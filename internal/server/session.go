package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/identity"
	"github.com/nrynss/keel/wire"
)

// sessionBody is the boot response. It carries the user id and the kind,
// and never the session id, the cookie, or an address.
type sessionBody struct {
	UserID string `json:"user_id"`
	Kind   string `json:"kind"`
}

// serveSession answers the boot call. The middleware has already resolved
// the cookie or minted a guest.
func (s *Server) serveSession(w http.ResponseWriter, r *http.Request) {
	user, ok := identity.UserFromContext(r.Context())
	if !ok || !id.Valid(user.ID) || (user.Kind != identity.KindGuest && user.Kind != identity.KindOwner) {
		_ = wire.WriteError(w, http.StatusInternalServerError, wire.CodeInternal, "The request could not be finished.", nil)
		return
	}
	if err := writeJSON(w, http.StatusOK, sessionBody{UserID: user.ID, Kind: user.Kind}); err != nil {
		s.log.Warn("server: write session", "error", err)
	}
}

// Revoke ends a server-side session. The next request that carries it
// does not resolve that user, because the request reads the row.
func (s *Server) Revoke(ctx context.Context, sessionID string) error {
	if s == nil || s.identity == nil {
		return fmt.Errorf("server: revoke: nil server")
	}
	return s.identity.Revoke(ctx, sessionID)
}

func writeJSON(w http.ResponseWriter, status int, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, err = w.Write(append(body, '\n'))
	return err
}

func writeNotFound(w http.ResponseWriter) {
	_ = wire.WriteError(w, http.StatusNotFound, wire.CodeNotFound, "No such resource exists.", nil)
}
