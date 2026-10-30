package server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nrynss/dresscode/internal/store"
	"github.com/nrynss/keel/identity"
	identitysql "github.com/nrynss/keel/identity/sqlitestore"
)

// Code sign-in paths. Keel serves both. Dresscode does not add a Google
// route, and it does not accept choice merge.
const (
	pathSignInCode   = "/api/session/code"
	pathSignInVerify = "/api/session/code/verify"
)

// signInMounted records servers that already registered the code routes.
// A second registration panics the mux, so a second call returns an
// error. MountSignIn runs once, before the process listens.
var (
	signInMu      sync.Mutex
	signInMounted = map[*Server]struct{}{}
)

// SignInConfig configures MountSignIn. Mail and CodeKey turn on Keel's
// code sign-in. SigningKey is the key Open used for the session cookie:
// verify signs the rotated cookie with it, and the middleware Open built
// checks that signature.
type SignInConfig struct {
	// Mail sends one sign-in code. Production passes the Resend client
	// from internal/mail. Tests pass the recorder. Nil is refused.
	// Keel's send ceilings run before Mail is called.
	Mail identity.Sender

	// CodeKey signs the code and address hashes Keel stores. Empty is
	// refused. MountSignIn copies it and does not log it.
	CodeKey []byte

	// SigningKey is the session key passed to Open. Empty is refused.
	// A different key leaves the rotated cookie unverifiable, so the
	// next request would mint a new guest.
	SigningKey []byte

	// Now is the clock for code expiry and the send ceilings. Nil means
	// time.Now. A test that moves time passes the same function here
	// and to Open.
	Now func() time.Time

	// Log receives sign-in faults. Nil uses the server log. The code
	// and the keys are not written.
	Log *slog.Logger
}

// MountSignIn serves POST /api/session/code and POST /api/session/code/verify
// through Keel identity. Open leaves those routes unmounted, and the
// identity service it built has no sender and no code key. Keel does not
// set either later, so this builds a second service on the same database.
// The session middleware stays the one Open built.
//
// A new address upgrades the guest in place, so a look row keeps its
// user id. An address that already has an account, when this guest has
// a look, returns guest_data_conflict and leaves the code live. choice
// switch then moves the device onto that account. The look stays on the
// guest. Nothing is copied onto the account, and the account's looks
// are not overwritten. Any other choice, including merge, is
// invalid_request.
func (s *Server) MountSignIn(ctx context.Context, cfg SignInConfig) error {
	if s == nil || s.db == nil || s.mux == nil || s.identity == nil {
		return fmt.Errorf("server: sign-in: nil server")
	}
	if ctx == nil {
		return fmt.Errorf("server: sign-in: nil context")
	}
	if cfg.Mail == nil {
		return fmt.Errorf("server: sign-in: mail sender is nil")
	}
	if len(cfg.CodeKey) == 0 {
		return fmt.Errorf("server: sign-in: code key is empty")
	}
	if len(cfg.SigningKey) == 0 {
		return fmt.Errorf("server: sign-in: signing key is empty")
	}

	signInMu.Lock()
	defer signInMu.Unlock()
	if _, ok := signInMounted[s]; ok {
		return fmt.Errorf("server: sign-in: already mounted")
	}
	svc, err := newSignIn(ctx, s, cfg)
	if err != nil {
		return err
	}
	handler := svc.CodeSignInHandler(pathSignInCode, pathSignInVerify)
	s.mux.Handle("POST "+pathSignInCode, handler)
	s.mux.Handle("POST "+pathSignInVerify, handler)
	signInMounted[s] = struct{}{}
	return nil
}

// newSignIn builds the code-sign-in service on the server's database.
// The store schema is already applied. Opening the identity store again
// records nothing new.
func newSignIn(ctx context.Context, s *Server, cfg SignInConfig) (*identity.Service, error) {
	idStore, err := identitysql.Open(ctx, identitysql.Config{DB: s.db})
	if err != nil {
		return nil, fmt.Errorf("server: sign-in: %w", err)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	log := cfg.Log
	if log == nil {
		log = s.log
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	key := make([]byte, len(cfg.SigningKey))
	copy(key, cfg.SigningKey)
	codeKey := make([]byte, len(cfg.CodeKey))
	copy(codeKey, cfg.CodeKey)
	svc, err := identity.New(identity.Config{
		Store:      idStore,
		SigningKey: key,
		CodeKey:    codeKey,
		Mail:       cfg.Mail,
		GuestData:  s.guestHasLooks,
		Now:        now,
		Log:        log,
	})
	if err != nil {
		return nil, fmt.Errorf("server: sign-in: %w", err)
	}
	return svc, nil
}

// guestHasLooks reports whether userID owns a look. Keel asks before it
// moves a device onto an existing account. A look is the conflict. No
// look lets the device join that account. This does not move rows:
// switch leaves them on the guest.
func (s *Server) guestHasLooks(ctx context.Context, userID string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("server: sign-in: nil server")
	}
	var n int
	err := s.db.Reader().QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM `+store.TableLooks+` WHERE `+store.UserIDColumn+` = ?)`,
		userID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("server: sign-in: look rows: %w", err)
	}
	return n != 0, nil
}
