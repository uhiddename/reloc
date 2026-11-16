// Package api is the JSON HTTP API of relocd.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/uhiddename/reloc/internal/app"
)

const (
	// readyTimeout bounds the database ping in /readyz; keep it below the
	// probe's timeoutSeconds, or the probe gives up first and the ping's
	// error never reaches the log
	readyTimeout = time.Second
	// ApplyTimeout limits one apply, including how long a hung host can hold
	// the server's lock. relocd waits longer than this on shutdown.
	ApplyTimeout = 15 * time.Second
)

type server struct {
	app *app.App
}

// New returns the handler of relocd. Paths under /api/ need the token as
// "Authorization: Bearer <token>"; /livez and /readyz are open for probes;
// every other path goes to ui, the browser UI, which is open too: its pages
// hold no data, they fetch it from the API. A nil ui leaves those paths to
// a 404.
//
// New panics on an empty token: the header "Bearer " would then be accepted.
func New(a *app.App, token string, ui http.Handler) http.Handler {
	if token == "" {
		panic("api: empty token")
	}
	s := &server{app: a}

	root := http.NewServeMux()
	root.HandleFunc("GET /livez", s.livez)
	root.HandleFunc("GET /readyz", s.readyz)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/servers", s.listServers)
	mux.HandleFunc("GET /api/v1/servers/{id}/clients", s.listClients)
	mux.HandleFunc("POST /api/v1/servers/{id}/clients", s.createClient)
	mux.HandleFunc("POST /api/v1/servers/{id}/apply", s.applyServer)
	mux.HandleFunc("PATCH /api/v1/clients/{id}", s.updateClient)
	mux.HandleFunc("DELETE /api/v1/clients/{id}", s.deleteClient)
	mux.HandleFunc("GET /api/v1/clients/{id}/conf", s.clientConf)

	root.Handle("/api/", logRequests(requireToken(token, mux)))
	if ui != nil {
		root.Handle("/", logRequests(ui))
	}

	return recoverPanics(root)
}

type statusResponse struct {
	Status string `json:"status"`
}

func (s *server) livez(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (s *server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
	defer cancel()
	if err := s.app.Ping(ctx); err != nil {
		slog.Warn("not ready", "err", err)
		writeJSON(
			w, http.StatusServiceUnavailable,
			statusResponse{Status: "unavailable"},
		)
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

type applyResponse struct {
	Action string `json:"action"`
}

func (s *server) applyServer(w http.ResponseWriter, r *http.Request) {
	serverID, err := pathID(r, "server")
	if err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), ApplyTimeout)
	defer cancel()
	action, err := s.app.ApplyServer(ctx, serverID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, applyResponse{Action: action.String()})
}
