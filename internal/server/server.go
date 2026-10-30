// Package server is Dresscode's HTTP server.
//
// One handler serves the API and the built app. A first visit creates a
// guest user and a server-side session through Keel identity. The cookie
// carries only the signed session id, and every request resolves that
// cookie against the rows, so a revoked session does not survive into the
// next request. Keel gate stands in front of every route that spends.
// Sign-in, deletion, and the look handler are not mounted here.
// Intake mounts uploads, photos, products, links, and media.
package server

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nrynss/dresscode/internal/store"
	"github.com/nrynss/keel/gate"
	"github.com/nrynss/keel/identity"
	identitysql "github.com/nrynss/keel/identity/sqlitestore"
	"github.com/nrynss/keel/sqlite"
)

const (
	routeSession = "GET /api/session"

	// RouteLook is the fresh-look route, the one the API puts the
	// spending gate on. SetSpendHandler installs the handler behind
	// that gate. Until it does, an admission answers not_found.
	RouteLook = "POST /api/looks"
)

// Defaults for the fresh-look gate. They bound requests, not credits.
// One client may start eight looks at once, then one every ten minutes.
// The process may start forty at once, then one every minute.
const (
	defaultLookPerClientBurst = 8
	defaultLookPerClientEvery = 10 * time.Minute
	defaultLookGlobalBurst    = 40
	defaultLookGlobalEvery    = time.Minute
)

// Config configures Open. The zero value is not usable: the database
// path, the built app, and the signing key are required.
type Config struct {
	// DBPath is the SQLite file. Open creates it and its parent
	// directory. The store schema and the identity schema share it.
	DBPath string

	// AppDir is the built static app. It must be a directory that
	// contains index.html. Open does not build the app.
	AppDir string

	// SigningKey signs the session cookie. The caller loads the bytes
	// from its secret source. Empty is refused. Open copies the slice,
	// and the server never logs it.
	SigningKey []byte

	// Now is the clock for sessions and the spending gate. Nil means
	// time.Now.
	Now func() time.Time

	// Log receives faults. Nil discards them.
	Log *slog.Logger

	// LookPerClient is the per-client bucket on a fresh look. The zero
	// value means the default. A limit with a non-positive burst or
	// interval fails Open, because an unenforced gate must not pass.
	LookPerClient gate.Limit

	// LookGlobal is the process-wide bucket on a fresh look. The zero
	// value means the default. It is the ceiling a forged client key
	// cannot raise.
	LookGlobal gate.Limit
}

// Server is the API and the built app. Create it with Open and release
// it with Close. A Server is safe for concurrent requests. Close is not
// safe against requests still running.
type Server struct {
	db       *sqlite.DB
	identity *identity.Service
	mux      *http.ServeMux
	app      fs.FS
	log      *slog.Logger
	spend    map[string]*spendSlot
	handler  http.Handler
}

// spendSlot is the handler behind one gated route. The gate calls it
// only after the request is admitted, so a refusal never reaches it.
type spendSlot struct {
	mu sync.RWMutex
	h  http.Handler
}

func (s *spendSlot) set(h http.Handler) {
	s.mu.Lock()
	s.h = h
	s.mu.Unlock()
}

func (s *spendSlot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	h := s.h
	s.mu.RUnlock()
	if h == nil {
		writeNotFound(w)
		return
	}
	h.ServeHTTP(w, r)
}

// Open migrates the store and identity schemas, then builds the handler.
// It does not listen.
func Open(ctx context.Context, cfg Config) (*Server, error) {
	if ctx == nil {
		return nil, fmt.Errorf("server: open: nil context")
	}
	if err := cfg.check(); err != nil {
		return nil, err
	}
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	key := make([]byte, len(cfg.SigningKey))
	copy(key, cfg.SigningKey)

	db, err := sqlite.Open(ctx, sqlite.Config{Path: cfg.DBPath, Logger: log, Now: now})
	if err != nil {
		return nil, fmt.Errorf("server: open: %w", err)
	}
	closeDB := true
	defer func() {
		if closeDB {
			_ = db.Close()
		}
	}()

	// The store schema lands first. A fresh file migrates in that
	// order, and the store does not create identity's tables.
	if err := store.Migrate(ctx, db); err != nil {
		return nil, fmt.Errorf("server: open: %w", err)
	}
	idStore, err := identitysql.Open(ctx, identitysql.Config{DB: db})
	if err != nil {
		return nil, fmt.Errorf("server: open: %w", err)
	}
	// Code sign-in and account deletion stay unwired. The sender, the
	// code key, and the eraser belong to those routes, not this one.
	svc, err := identity.New(identity.Config{
		Store:      idStore,
		SigningKey: key,
		Now:        now,
		Log:        log,
	})
	if err != nil {
		return nil, fmt.Errorf("server: open: %w", err)
	}
	g, err := gate.New(gate.Config{Now: now, Log: log})
	if err != nil {
		return nil, fmt.Errorf("server: open: %w", err)
	}

	s := &Server{
		db:       db,
		identity: svc,
		mux:      http.NewServeMux(),
		app:      os.DirFS(cfg.AppDir),
		log:      log,
		spend:    make(map[string]*spendSlot),
	}
	if err := s.mount(ctx, g, cfg); err != nil {
		return nil, err
	}
	s.handler = svc.Middleware(s.mux)
	closeDB = false
	return s, nil
}

func (cfg Config) check() error {
	if cfg.DBPath == "" {
		return fmt.Errorf("server: open: database path is empty")
	}
	if len(cfg.SigningKey) == 0 {
		return fmt.Errorf("server: open: signing key is empty")
	}
	dir, err := os.Stat(cfg.AppDir)
	if err != nil {
		return fmt.Errorf("server: open: app dir: %w", err)
	}
	if !dir.IsDir() {
		return fmt.Errorf("server: open: app dir %s is not a directory", cfg.AppDir)
	}
	index, err := os.Stat(filepath.Join(cfg.AppDir, "index.html"))
	if err != nil {
		return fmt.Errorf("server: open: app index: %w", err)
	}
	if !index.Mode().IsRegular() {
		return fmt.Errorf("server: open: app index is not a file")
	}
	return nil
}

func (s *Server) mount(ctx context.Context, g *gate.Gate, cfg Config) error {
	s.mux.Handle(routeSession, http.HandlerFunc(s.serveSession))
	s.mux.Handle("/", http.HandlerFunc(s.serveApp))

	rule := gate.Rule{
		Name:      "look",
		PerClient: limitOr(cfg.LookPerClient, gate.Limit{Burst: defaultLookPerClientBurst, Every: defaultLookPerClientEvery}),
		Global:    limitOr(cfg.LookGlobal, gate.Limit{Burst: defaultLookGlobalBurst, Every: defaultLookGlobalEvery}),
	}
	slot := &spendSlot{}
	protected, err := g.Protect(rule, slot)
	if err != nil {
		return fmt.Errorf("server: open: %w", err)
	}
	s.spend[RouteLook] = slot
	s.mux.Handle(RouteLook, protected)
	return s.mountIntake(ctx, cfg)
}

func limitOr(got, fallback gate.Limit) gate.Limit {
	if got == (gate.Limit{}) {
		return fallback
	}
	return got
}

// Handler is the API and the app. Every request runs through identity
// before the route, so the cookie is resolved against the rows first.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// SetSpendHandler installs h behind the gate for pattern. Pattern must
// be a route the server already mounted as a spending route. Nil
// restores the admission answer, which is not_found. The call does not
// implement the route.
func (s *Server) SetSpendHandler(pattern string, h http.Handler) error {
	if s == nil {
		return fmt.Errorf("server: nil server")
	}
	slot := s.spend[pattern]
	if slot == nil {
		return fmt.Errorf("server: %s is not a spending route", pattern)
	}
	slot.set(h)
	return nil
}

// Close releases the database. A second call does nothing.
func (s *Server) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.closeIntake()
	err := s.db.Close()
	s.db = nil
	return err
}

// serveApp serves the built app. A client-side route has no file, so an
// unknown GET falls back to index.html. API and media paths are not the
// app: an API miss is the wire envelope, and a media miss is the plain
// text the contract names for a blob this session may not read.
func (s *Server) serveApp(w http.ResponseWriter, r *http.Request) {
	p := path.Clean("/" + r.URL.Path)
	switch {
	case under(p, "/api"):
		writeNotFound(w)
		return
	case under(p, "/media"):
		w.Header().Set("Cache-Control", "private, no-store")
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	rel := strings.TrimPrefix(p, "/")
	if rel == "" || rel == "index.html" {
		s.serveIndex(w, r)
		return
	}
	if !fs.ValidPath(rel) {
		http.NotFound(w, r)
		return
	}
	info, err := fs.Stat(s.app, rel)
	if err != nil || info.IsDir() {
		s.serveIndex(w, r)
		return
	}
	http.ServeFileFS(w, r, s.app, rel)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	body, err := fs.ReadFile(s.app, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

func under(p, root string) bool {
	return p == root || strings.HasPrefix(p, root+"/")
}
