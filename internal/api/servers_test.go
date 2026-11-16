package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

const serversPath = "/api/v1/servers"

// listServers gets the server list. Tests of other packages share the
// database, so the list holds their servers too: tests look for their own.
func (e testEnv) listServers(t *testing.T) []map[string]any {
	t.Helper()
	w := e.get(serversPath)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q: %v", w.Body, err)
	}
	return got
}

func TestListServers(t *testing.T) {
	e := newTestEnv(t)
	// created in this order, so ids are ascending
	v4 := e.server(t, "10.66.66.1/24", "")
	dual := e.server(t, "10.77.77.1/24", "fd42::1/64")

	var ids []int64
	for _, s := range e.listServers(t) {
		id, ok := s["id"].(float64)
		if !ok {
			t.Fatalf("id = %#v, want a number", s["id"])
		}
		ids = append(ids, int64(id))
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] >= ids[i] {
			t.Fatalf("servers are not ordered by id: %d before %d", ids[i-1], ids[i])
		}
	}
	for _, id := range []int64{v4.ID, dual.ID} {
		if !slices.Contains(ids, id) {
			t.Errorf("server %d is not listed", id)
		}
	}
}

// The fields are compared with a list, so a new field in the response
// has to be added here on purpose.
func TestListServersFields(t *testing.T) {
	e := newTestEnv(t)
	e.server(t, "10.66.66.1/24", "fd42::1/64")

	allowed := map[string]bool{"id": true}
	for _, s := range e.listServers(t) {
		for field := range s {
			if !allowed[field] {
				t.Errorf("unexpected field %q in the response", field)
			}
		}
	}
}

func TestListServersNoSecrets(t *testing.T) {
	e := newTestEnv(t)
	srv := e.server(t, "10.66.66.1/24", "")
	p, err := srv.WireGuard()
	if err != nil {
		t.Fatal(err)
	}

	body := e.get(serversPath).Body.String()
	if strings.Contains(body, p.PrivateKey.Base64()) {
		t.Error("response contains the private key")
	}
	// encoded differently, a key would still show up under a field name
	for _, s := range []string{"key", "Key", "Protocol", "protocol"} {
		if strings.Contains(body, s) {
			t.Errorf("response contains %q", s)
		}
	}
}

func TestListServersRequiresToken(t *testing.T) {
	h := New(nil, testToken, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, serversPath, nil))
	wantError(t, w, http.StatusUnauthorized, "unauthorized")
}

// A database error reaches the log, not the caller.
func TestListServersUnavailable(t *testing.T) {
	logs := captureLog(t)
	h := New(unreachableApp(t), testToken, nil)

	r := httptest.NewRequest(http.MethodGet, serversPath, nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	wantError(t, w, http.StatusInternalServerError, "internal error")
	if strings.Contains(w.Body.String(), "store:") {
		t.Errorf("response reveals the error: %s", w.Body)
	}
	if !strings.Contains(logs.String(), "app: list servers: store: list servers: ") {
		t.Errorf("the error is not logged:\n%s", logs)
	}
}
