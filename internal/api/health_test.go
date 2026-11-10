package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uhiddename/reloc/internal/app"
	"github.com/uhiddename/reloc/internal/store"
)

// captureLog sends the default logger to a buffer until the test ends.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// probe sends a request without a token, as kubelet does.
func probe(h http.Handler, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

// wantStatus checks a probe response: the code and a body that holds only
// the status, so nothing else (an error, a host name) can leak through it.
func wantStatus(t *testing.T, w *httptest.ResponseRecorder, code int, status string) {
	t.Helper()
	if w.Code != code {
		t.Errorf("code = %d, want %d; body: %s", w.Code, code, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	dec := json.NewDecoder(bytes.NewReader(w.Body.Bytes()))
	dec.DisallowUnknownFields()
	var got statusResponse
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("body %q: %v", w.Body, err)
	}
	if got.Status != status {
		t.Errorf("status = %q, want %q", got.Status, status)
	}
	// a handler that writes twice leaves a second value after the first
	if _, err := dec.Token(); err != io.EOF {
		t.Errorf("body %q has more than one JSON value", w.Body)
	}
}

// unreachableApp returns an App whose database nobody listens on.
func unreachableApp(t *testing.T) *app.App {
	t.Helper()
	// pgxpool connects lazily, so New succeeds
	db, err := pgxpool.New(context.Background(), "postgres://reloc@127.0.0.1:1/reloc?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return app.New(store.New(db))
}

func TestLivez(t *testing.T) {
	logs := captureLog(t)
	// a nil app: liveness must not depend on anything, the database included
	h := New(nil, testToken)

	wantStatus(t, probe(h, http.MethodGet, "/livez"), http.StatusOK, "ok")
	if w := probe(h, http.MethodHead, "/livez"); w.Code != http.StatusOK {
		t.Errorf("HEAD code = %d, want %d", w.Code, http.StatusOK)
	}
	if logs.Len() != 0 {
		t.Errorf("probes are logged:\n%s", logs)
	}
}

func TestReadyz(t *testing.T) {
	e := newTestEnv(t)
	logs := captureLog(t)

	wantStatus(t, probe(e.handler, http.MethodGet, "/readyz"), http.StatusOK, "ok")
	if logs.Len() != 0 {
		t.Errorf("a successful probe is logged:\n%s", logs)
	}
}

func TestReadyzUnavailable(t *testing.T) {
	logs := captureLog(t)
	h := New(unreachableApp(t), testToken)

	wantStatus(t, probe(h, http.MethodGet, "/readyz"), http.StatusServiceUnavailable, "unavailable")

	// the reason goes to the log, not to the unauthenticated caller
	got := logs.String()
	for _, want := range []string{"level=WARN", "not ready", "store: ping:"} {
		if !strings.Contains(got, want) {
			t.Errorf("log does not mention %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "msg=request") {
		t.Errorf("the probe itself is logged as a request:\n%s", got)
	}
}

// A database that accepts connections but never answers must not hold the
// probe: readyz gives up after readyTimeout, before kubelet does.
func TestReadyzTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// hold the connection silent until the test ends
			t.Cleanup(func() { conn.Close() })
		}
	}()

	db, err := pgxpool.New(context.Background(), "postgres://reloc@"+ln.Addr().String()+"/reloc?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	logs := captureLog(t)
	h := New(app.New(store.New(db)), testToken)

	start := time.Now()
	w := probe(h, http.MethodGet, "/readyz")
	elapsed := time.Since(start)

	wantStatus(t, w, http.StatusServiceUnavailable, "unavailable")
	if elapsed > readyTimeout+time.Second {
		t.Errorf("readyz took %v, want about readyTimeout (%v)", elapsed, readyTimeout)
	}
	if !strings.Contains(logs.String(), "not ready") {
		t.Errorf("the timeout is not logged:\n%s", logs)
	}
}

func TestReadyzClosedPool(t *testing.T) {
	e := newTestEnv(t)
	captureLog(t)
	e.db.Close()

	wantStatus(t, probe(e.handler, http.MethodGet, "/readyz"), http.StatusServiceUnavailable, "unavailable")
}

// Only GET and HEAD on the exact paths are public; anything else goes to the
// API behind the token and is logged like any API request.
func TestProbesOtherwiseRequireToken(t *testing.T) {
	h := New(nil, testToken)
	for _, tt := range []struct{ method, path string }{
		{http.MethodPost, "/livez"},
		{http.MethodPost, "/readyz"},
		{http.MethodDelete, "/livez"},
		{http.MethodGet, "/livez/"},
		{http.MethodGet, "/readyz/"},
		{http.MethodGet, "/LIVEZ"},
		{http.MethodGet, "/healthz"},
	} {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			logs := captureLog(t)
			w := probe(h, tt.method, tt.path)
			wantError(t, w, http.StatusUnauthorized, "unauthorized")
			if !strings.Contains(logs.String(), "status=401") {
				t.Errorf("the request is not logged:\n%s", logs)
			}
		})
	}
}
