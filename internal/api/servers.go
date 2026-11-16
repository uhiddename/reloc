package api

import (
	"net/http"

	"github.com/uhiddename/reloc/internal/vpn"
)

// serverResponse is a server as the API shows it. Never encode vpn.Server
// directly: its protocol holds the WireGuard private key.
type serverResponse struct {
	ID int64 `json:"id"`
}

func newServerResponse(s vpn.Server) serverResponse {
	return serverResponse{
		ID: s.ID,
	}
}

func (s *server) listServers(w http.ResponseWriter, r *http.Request) {
	servers, err := s.app.ListServers(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	// make, not var: no servers must encode as [], not null
	out := make([]serverResponse, 0, len(servers))
	for _, s := range servers {
		out = append(out, newServerResponse(s))
	}
	writeJSON(w, http.StatusOK, out)
}
