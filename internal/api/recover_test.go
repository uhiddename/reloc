package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// panicBeforeWrite is a named function so that its name shows up in the
// logged stack.
func panicBeforeWrite(w http.ResponseWriter, r *http.Request) {
	panic("boom")
}

// serveRecovered runs h behind recoverPanics and returns what the panic
// left after it: the value recoverPanics let through, if any.
func serveRecovered(h http.HandlerFunc, w http.ResponseWriter, r *http.Request) (escaped any) {
	defer func() { escaped = recover() }()
	recoverPanics(h).ServeHTTP(w, r)
	return nil
}

func TestRecoverPanics(t *testing.T) {
	logs := captureLog(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/servers/7/apply?token=secret", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)

	if v := serveRecovered(panicBeforeWrite, w, r); v != nil {
		t.Fatalf("panic %v escaped", v)
	}

	// the client gets the same answer as for any unknown error
	wantError(t, w, http.StatusInternalServerError, "internal error")

	got := logs.String()
	for _, want := range []string{
		"level=ERROR",
		"method=POST",
		"path=/api/v1/servers/7/apply",
		"boom",
		// the stack points at the code that panicked
		"api.panicBeforeWrite",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("log does not mention %q:\n%s", want, got)
		}
	}
	// like logRequests: no headers, no query string
	for _, secret := range []string{testToken, "token=secret"} {
		if strings.Contains(got, secret) {
			t.Errorf("log leaks %q:\n%s", secret, got)
		}
	}
}

// Once the status line is out, a 500 can no longer be sent. The handler must
// abort the response instead, so the client sees a broken response rather
// than a cut one that looks complete.
func TestRecoverPanicsAfterWrite(t *testing.T) {
	tests := []struct {
		name  string
		write func(w http.ResponseWriter)
	}{
		{"after WriteHeader", func(w http.ResponseWriter) { w.WriteHeader(http.StatusOK) }},
		// Write sends an implicit 200
		{"after Write", func(w http.ResponseWriter) { _, _ = io.WriteString(w, `{"partial":`) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureLog(t)
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/api/v1/servers/7/clients", nil)

			v := serveRecovered(func(w http.ResponseWriter, r *http.Request) {
				tt.write(w)
				panic("boom")
			}, w, r)

			if v != http.ErrAbortHandler {
				t.Errorf("escaped panic = %v, want http.ErrAbortHandler", v)
			}
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want the 200 already sent", w.Code)
			}
			if strings.Contains(w.Body.String(), "internal error") {
				t.Errorf("an error body was appended to the response: %s", w.Body)
			}
			// the original panic is still logged before the abort
			if !strings.Contains(logs.String(), "boom") {
				t.Errorf("the panic is not logged:\n%s", logs)
			}
		})
	}
}

// http.ErrAbortHandler is how a handler aborts a response on purpose:
// net/http expects it and stays silent, so recoverPanics must too.
func TestRecoverPanicsAbortHandler(t *testing.T) {
	logs := captureLog(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/servers/7/clients", nil)

	v := serveRecovered(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}, w, r)

	if v != http.ErrAbortHandler {
		t.Errorf("escaped panic = %v, want http.ErrAbortHandler", v)
	}
	if logs.Len() != 0 {
		t.Errorf("a deliberate abort is logged:\n%s", logs)
	}
	if w.Body.Len() != 0 {
		t.Errorf("body = %q, want nothing written", w.Body)
	}
}

func TestRecoverPanicsNoPanic(t *testing.T) {
	logs := captureLog(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/livez", nil)

	v := serveRecovered(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusTeapot, statusResponse{Status: "ok"})
	}, w, r)

	if v != nil {
		t.Fatalf("panic %v escaped", v)
	}
	wantStatus(t, w, http.StatusTeapot, "ok")
	if logs.Len() != 0 {
		t.Errorf("a normal request is logged:\n%s", logs)
	}
}

// The handler from New is behind recoverPanics: a nil App makes the readyz
// handler panic, and the probe still gets a JSON 500.
func TestNewRecoversPanics(t *testing.T) {
	logs := captureLog(t)
	h := New(nil, testToken)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	wantError(t, w, http.StatusInternalServerError, "internal error")
	if !strings.Contains(logs.String(), "path=/readyz") {
		t.Errorf("the panic is not logged:\n%s", logs)
	}
}

// Over a real connection: the client gets a 500, not a dropped connection,
// and the server keeps serving.
func TestRecoverPanicsOverHTTP(t *testing.T) {
	captureLog(t)
	srv := httptest.NewServer(recoverPanics(http.HandlerFunc(panicBeforeWrite)))
	t.Cleanup(srv.Close)

	for range 2 {
		resp, err := http.Get(srv.URL)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", resp.StatusCode)
		}
	}
}
