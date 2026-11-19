package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uhiddename/reloc/internal/app"
	"github.com/uhiddename/reloc/internal/config"
	"github.com/uhiddename/reloc/internal/inet"
	"github.com/uhiddename/reloc/internal/store"
	"github.com/uhiddename/reloc/internal/vpn"
	"github.com/uhiddename/reloc/internal/wg"
	"github.com/uhiddename/reloc/migrations"
	"golang.org/x/crypto/ssh"
)

const testToken = "test-token"

type testEnv struct {
	handler http.Handler
	app     *app.App
	st      *store.Store
	db      *pgxpool.Pool
}

// newTestEnv connects to the database from the environment, see package
// config, and applies migrations. Tests that use it are skipped when
// RELOC_PGDSN is not set; any other configuration error fails them.
func newTestEnv(t *testing.T) testEnv {
	t.Helper()
	if os.Getenv("RELOC_PGDSN") == "" {
		t.Skip("RELOC_PGDSN is not set")
	}
	ctx := context.Background()

	cfg, err := config.LoadPostgres()
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 3
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)

	dir, err := fs.Sub(migrations.Postgres, "postgres")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db, dir); err != nil {
		t.Fatal(err)
	}
	st := store.New(db)
	a := app.New(st)
	return testEnv{handler: New(a, testToken, nil, nil), app: a, st: st, db: db}
}

var testHostSeq atomic.Int64

// server creates a host and a server with the given networks (empty string
// means no network of that family). Everything is deleted when the test ends.
func (e testEnv) server(t *testing.T, ipv4, ipv6 string) vpn.Server {
	t.Helper()
	ctx := context.Background()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostKey, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	h, err := e.st.CreateHost(ctx, vpn.Host{
		Name: fmt.Sprintf("api/%s-%d", t.Name(), testHostSeq.Add(1)),
		SSH: vpn.SSH{
			Addr:    inet.MustEndpointFromAddr(netip.MustParseAddr("192.0.2.1"), 22),
			User:    "root",
			HostKey: hostKey,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := e.st.CreateServer(ctx, vpn.Server{
		HostID:        h.ID,
		Transport:     vpn.UDP,
		InterfaceName: "wg0",
		ListenPort:    51820,
		Endpoint:      inet.MustEndpointFromAddr(netip.MustParseAddr("198.51.100.7"), 51820),
		IPv4:          testNetwork(ipv4),
		IPv6:          testNetwork(ipv6),
		Protocol:      vpn.WireGuardServer{PrivateKey: wg.Genkey()},
	})
	// registered before the error check, so the host is removed either way
	t.Cleanup(func() {
		for _, q := range []struct {
			sql string
			id  int64
		}{
			{"delete from vpn_clients where server_id = $1", srv.ID},
			{"delete from vpn_servers where id = $1", srv.ID},
			{"delete from hosts where id = $1", h.ID},
		} {
			if _, err := e.db.Exec(context.Background(), q.sql, q.id); err != nil {
				t.Error(err)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func testNetwork(s string) *vpn.Network {
	if s == "" {
		return nil
	}
	p := netip.MustParsePrefix(s)
	return &vpn.Network{Prefix: p.Masked(), Addr: p.Addr()}
}

func (e testEnv) get(path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, r)
	return w
}

func wantError(t *testing.T, w *httptest.ResponseRecorder, status int, msg string) {
	t.Helper()
	if w.Code != status {
		t.Errorf("status = %d, want %d; body: %s", w.Code, status, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got errorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q: %v", w.Body, err)
	}
	if got.Error != msg {
		t.Errorf("error = %q, want %q", got.Error, msg)
	}
}

func TestNewEmptyToken(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("New() with an empty token did not panic")
		}
	}()
	New(nil, "", nil, nil)
}

// fakeUI stands in for web.Handler: it answers with a status no route of the
// API uses and echoes the path, so a test sees who served a request and with
// which path.
var fakeUI = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusTeapot)
	_, _ = io.WriteString(w, r.URL.Path)
})

// Who serves what: the API under /api/, behind the token; the probes on their
// exact paths; the UI everything else, whatever the method (it refuses the
// methods it does not serve itself).
func TestRoutes(t *testing.T) {
	h := New(nil, testToken, nil, fakeUI)
	const (
		api   = "api"
		ui    = "ui"
		livez = "livez"
	)
	for _, tt := range []struct{ method, path, want string }{
		{http.MethodGet, "/", ui},
		{http.MethodGet, "/servers/7", ui},
		{http.MethodHead, "/servers/7", ui},
		{http.MethodGet, "/assets/index-3f9a1c.js", ui},
		{http.MethodPost, "/servers/7", ui},
		// near the API, but not under /api/
		{http.MethodGet, "/apix", ui},
		{http.MethodGet, "/v1/servers/7/clients", ui},
		// near the probes, but not their exact paths and methods
		{http.MethodPost, "/livez", ui},
		{http.MethodDelete, "/readyz", ui},
		{http.MethodGet, "/livez/", ui},
		{http.MethodGet, "/LIVEZ", ui},
		{http.MethodGet, "/healthz", ui},

		{http.MethodGet, "/api/", api},
		{http.MethodGet, "/api/v1/servers/7/clients", api},
		{http.MethodPost, "/api/v1/servers/7/apply", api},
		// unknown API paths need the token too: without it, nobody can probe
		// which routes exist
		{http.MethodGet, "/api/v1/unknown", api},
		{http.MethodGet, "/api/v2/servers", api},

		{http.MethodGet, "/livez", livez},
		{http.MethodHead, "/livez", livez},
	} {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			logs := captureLog(t)
			w := probe(h, tt.method, tt.path)
			switch tt.want {
			case api:
				wantError(t, w, http.StatusUnauthorized, "unauthorized")
			case ui:
				if w.Code != http.StatusTeapot {
					t.Fatalf("code = %d, want the UI's %d; body: %s", w.Code, http.StatusTeapot, w.Body)
				}
				if tt.method != http.MethodHead && w.Body.String() != tt.path {
					t.Errorf("the UI got path %q, want %q", w.Body, tt.path)
				}
				// UI requests are logged like API ones
				for _, s := range []string{"path=" + tt.path, "status=418"} {
					if !strings.Contains(logs.String(), s) {
						t.Errorf("log does not mention %q:\n%s", s, logs)
					}
				}
			case livez:
				if w.Code != http.StatusOK {
					t.Errorf("code = %d, want 200; body: %s", w.Code, w.Body)
				}
			}
		})
	}
}

// Without a UI the API and the probes work as before, and the other paths are
// a plain 404, not a 401: there is nothing behind them to protect.
func TestRoutesWithoutUI(t *testing.T) {
	h := New(nil, testToken, nil, nil)
	for _, tt := range []struct {
		method, path string
		code         int
	}{
		{http.MethodGet, "/", http.StatusNotFound},
		{http.MethodGet, "/servers/7", http.StatusNotFound},
		{http.MethodGet, "/livez/", http.StatusNotFound},
		// the probe's path, another method: ServeMux answers itself
		{http.MethodPost, "/livez", http.StatusMethodNotAllowed},
		{http.MethodGet, "/livez", http.StatusOK},
		{http.MethodGet, "/api/v1/servers/7/clients", http.StatusUnauthorized},
	} {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			captureLog(t)
			if w := probe(h, tt.method, tt.path); w.Code != tt.code {
				t.Errorf("code = %d, want %d; body: %s", w.Code, tt.code, w.Body)
			}
		})
	}
}

func (e testEnv) post(path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, r)
	return w
}

func (e testEnv) do(method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, r)
	return w
}

func applyPath(serverID string) string {
	return "/api/v1/servers/" + serverID + "/apply"
}

func TestApplyServerBadID(t *testing.T) {
	h := New(nil, testToken, nil, nil)
	for _, id := range []string{"abc", "0", "-1", "1.5", "99999999999999999999"} {
		t.Run(id, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, applyPath(id), nil)
			r.Header.Set("Authorization", "Bearer "+testToken)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			wantError(t, w, http.StatusBadRequest, "server id must be a positive number")
		})
	}
}

func TestApplyServerRequiresToken(t *testing.T) {
	h := New(nil, testToken, nil, nil)
	r := httptest.NewRequest(http.MethodPost, applyPath("1"), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	wantError(t, w, http.StatusUnauthorized, "unauthorized")
}

func TestApplyServerNoKey(t *testing.T) {
	h := New(app.New(nil), testToken, nil, nil)
	r := httptest.NewRequest(http.MethodPost, applyPath("1"), nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	wantError(t, w, http.StatusServiceUnavailable, "ssh key is not configured")
}

func TestApplyServerNotFound(t *testing.T) {
	e := newTestEnv(t)
	e.handler = New(app.New(e.st).WithAuth(ssh.Password("unused")), testToken, nil, nil)

	w := e.do(http.MethodPost, applyPath(fmt.Sprint(int64(1<<62))), "")
	wantError(t, w, http.StatusNotFound, "not found")
}
