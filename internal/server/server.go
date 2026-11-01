// Package server is Dresscode's HTTP server.
//
// One handler serves the API and the built app. A first visit creates a
// guest user and a server-side session through Keel identity. The cookie
// carries only the signed session id, and every request resolves that
// cookie against the rows, so a revoked session does not survive into the
// next request. Keel gate stands in front of every route that spends.
// Open alone mounts the session, the gate and the intake; Compose is the
// constructor the binary boots, and it mounts the rest: the look routes,
// the occasions, the samples, sign-in, and deletion. Intake mounts
// uploads, photos, products, links, and media.
package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

	"github.com/nrynss/dresscode/internal/config"
	"github.com/nrynss/dresscode/internal/memory"
	"github.com/nrynss/dresscode/internal/store"
	"github.com/nrynss/dresscode/internal/systemone"
	"github.com/nrynss/dresscode/internal/youcam"
	"github.com/nrynss/keel/cost"
	coststore "github.com/nrynss/keel/cost/sqlitestore"
	"github.com/nrynss/keel/gate"
	"github.com/nrynss/keel/identity"
	identitysql "github.com/nrynss/keel/identity/sqlitestore"
	"github.com/nrynss/keel/job"
	jobsql "github.com/nrynss/keel/job/sqlitestore"
	"github.com/nrynss/keel/mediastore"
	mediasql "github.com/nrynss/keel/mediastore/sqlitestore"
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
	// jobs is the job rows on this server's handle, for Drain and the
	// leftover read. Open leaves it nil; Compose opens it.
	jobs job.Store
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

// now is the clock Open runs on.
func (cfg Config) now() func() time.Time {
	if cfg.Now == nil {
		return time.Now
	}
	return cfg.Now
}

// logOrDiscard is the logger Open runs on.
func (cfg Config) logOrDiscard() *slog.Logger {
	if cfg.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return cfg.Log
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
//
// Close does not stop the job runners: Keel's runner has no Close, and
// cancelling a look would record `cancelled`, which is terminal, where a
// restart's `interrupted` is recoverable. Stop taking requests and call
// Drain first, so the looks in flight reach their terminal; a look still
// running under Close leaves an unfinished row, and the next Open's
// recovery interrupts it and resumes it from its snapshot.
func (s *Server) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.closeIntake()
	err := s.db.Close()
	s.db = nil
	return err
}

// ---------------------------------------------------------------------------
// The composition

// Wiring is everything the composed server needs that is not the
// database: the settings file, and the clients that reach the world. The
// callers build the clients; Compose opens the stores on the one
// database handle and mounts every route the binary answers. Nothing
// here dials at construction: every client's constructor only checks its
// configuration.
type Wiring struct {
	// Settings is the loaded settings file. The budgets own the spend
	// ceilings, the judge and check sections ride every look, and the
	// paths section names the two data directories the mounts read:
	// [paths] occasions and [paths] samples. Nil is refused.
	Settings *config.Settings

	// Clef builds the model client over the dollar books' meter. The
	// books open on the server's own database handle before this runs,
	// so the Workers AI client production builds here meters every ask
	// into the same store `dresscode budget` reads. The server closes
	// nothing the factory builds. Nil is refused.
	Clef func(dollars *cost.Meter) (systemone.SystemOne, error)

	// YouCam is the try-on client the look routes render and analyse
	// through. Nil is refused.
	YouCam youcam.YouCam

	// Memory is the Lambo client: style memory for the look routes,
	// and the account's session the deletion mount erases. Nil is
	// refused.
	Memory memory.Memory

	// Mail sends sign-in codes. Nil is refused: the sign-in routes are
	// part of the composition.
	Mail identity.Sender

	// CodeKey verifies sign-in and deletion codes. Empty is refused.
	// Compose copies it and never logs it.
	CodeKey []byte

	// Now is the clock for sessions, books and look rows. Nil means
	// time.Now. One clock rides every mount and both books, so a test
	// that moves time moves all of them together.
	Now func() time.Time

	// Log receives faults. Nil discards them.
	Log *slog.Logger
}

// Compose boots the server the binary runs: one database handle, every
// Keel store on it, and every route the API contract names mounted and
// answering before the first listen. It calls Open, opens the two spend
// books on the same handle, and mounts, in the order the constructors
// demand: the look routes (the intake must be there first, and the
// occasions, the samples and the job read hang off the look mount), then
// the occasions, then the samples, then sign-in, then deletion, whose
// identity middleware is the one the server serves under.
//
// The spend wiring is T4.7's seam landed: the units books are the
// `spend_youcam` namespace with the settings' lifetime ceiling and its
// judging split, the dollars books are `spend_workers_ai` with the
// settings' daily ceiling, and both are rewritten from the settings on
// every open, so the books and the settings file agree. Every spend the
// look routes make belongs to a session that took a judge lease, so the
// YouCam runner meters into the judging share. No production path spends
// a YouCam unit outside a judge lease - a sample open spends nothing by
// construction, and no other route reaches YouCam - so no development
// meter is built; the next one reserves against the development share
// through the books' keyed budget, the account `Spend.Development` hands
// out, not a meter of its own. The look start's pre-read is the
// development share's remaining headroom, the seam LooksConfig.UnitsLeft
// was built for.
//
// A mount that fails fails Compose: the server is closed and the error
// comes back, because a server that cannot mount what it claims is a
// bug, not a warning.
func Compose(ctx context.Context, cfg Config, wiring Wiring) (*Server, error) {
	if ctx == nil {
		return nil, fmt.Errorf("server: compose: nil context")
	}
	if wiring.Settings == nil {
		return nil, fmt.Errorf("server: compose: no settings")
	}
	if wiring.Settings.Paths.Occasions == "" {
		return nil, fmt.Errorf("server: compose: no [paths] occasions in the settings")
	}
	if wiring.Settings.Paths.Samples == "" {
		return nil, fmt.Errorf("server: compose: no [paths] samples in the settings")
	}
	if wiring.Clef == nil {
		return nil, fmt.Errorf("server: compose: no clef builder")
	}
	if wiring.YouCam == nil {
		return nil, fmt.Errorf("server: compose: no youcam client")
	}
	if wiring.Memory == nil {
		return nil, fmt.Errorf("server: compose: no memory client")
	}
	if wiring.Mail == nil {
		return nil, fmt.Errorf("server: compose: no mail sender")
	}
	if len(wiring.CodeKey) == 0 {
		return nil, fmt.Errorf("server: compose: the code key is empty")
	}

	// The wiring's clock is the whole server's: Open rides it with the
	// books and every mount, so a test that moves Wiring.Now moves the
	// sessions, the books and the look rows together, which is the
	// contract the field declares.
	now := wiring.Now
	if now == nil {
		now = cfg.now()
	}
	cfg.Now = now

	srv, err := Open(ctx, cfg)
	if err != nil {
		return nil, err
	}
	live := false
	defer func() {
		if !live {
			_ = srv.Close()
		}
	}()

	books, err := openBooks(ctx, srv.db, wiring.Settings, now, cfg.logOrDiscard())
	if err != nil {
		return nil, fmt.Errorf("server: compose: %w", err)
	}

	dollarsMeter, err := cost.NewContextMeter(books.dollars.Account(), books.dollars)
	if err != nil {
		return nil, fmt.Errorf("server: compose: the dollars meter: %w", err)
	}
	clef, err := wiring.Clef(dollarsMeter)
	if err != nil {
		return nil, fmt.Errorf("server: compose: %w", err)
	}

	index, err := mediasql.Open(ctx, mediasql.Config{DB: srv.db})
	if err != nil {
		return nil, fmt.Errorf("server: compose: %w", err)
	}
	media := srv.intakeOf().media
	if media == nil {
		return nil, fmt.Errorf("server: compose: the intake is not mounted")
	}
	catalog, err := seedCatalog(ctx, media, index, wiring.Settings.Paths.Samples)
	if err != nil {
		return nil, fmt.Errorf("server: compose: %w", err)
	}

	log := cfg.logOrDiscard()
	if err := srv.MountLooks(ctx, LooksConfig{
		OccasionsDir: wiring.Settings.Paths.Occasions,
		Settings:     wiring.Settings,
		Clef:         clef,
		YouCam:       wiring.YouCam,
		Meter:        books.judging,
		UnitsLeft: func() (cost.Price, error) {
			return books.keyed.Remaining(context.Background(), config.OwnerDevelopment)
		},
		Memory:       wiring.Memory,
		CatalogMedia: catalog.lookup,
		Now:          now,
		Log:          log,
	}); err != nil {
		return nil, fmt.Errorf("server: compose: %w", err)
	}
	if err := srv.MountOccasions(); err != nil {
		return nil, fmt.Errorf("server: compose: %w", err)
	}
	if err := srv.MountSamples(ctx, SamplesConfig{
		SamplesDir: wiring.Settings.Paths.Samples,
		Now:        now,
		Log:        log,
	}); err != nil {
		return nil, fmt.Errorf("server: compose: %w", err)
	}
	if err := srv.MountSignIn(ctx, SignInConfig{
		Mail:       wiring.Mail,
		CodeKey:    wiring.CodeKey,
		SigningKey: cfg.SigningKey,
		Now:        now,
		Log:        log,
	}); err != nil {
		return nil, fmt.Errorf("server: compose: %w", err)
	}
	if err := srv.MountDeletion(ctx, DeletionConfig{
		SigningKey: cfg.SigningKey,
		CodeKey:    wiring.CodeKey,
		Mail:       wiring.Mail,
		Blobs:      blobStore{media: media, index: index},
		Memory:     wiring.Memory,
		Now:        now,
		Log:        log,
	}); err != nil {
		return nil, fmt.Errorf("server: compose: %w", err)
	}

	jobs, err := jobsql.Open(ctx, jobsql.Config{DB: srv.db})
	if err != nil {
		return nil, fmt.Errorf("server: compose: %w", err)
	}
	srv.jobs = jobs

	live = true
	return srv, nil
}

// books is the server's own spend wiring: the two cost namespaces the
// ops commands read, opened on the server's one database handle, with
// the settings file's ceilings rewritten onto them at open. Every field
// has a reader in Compose: keyed is the look start's pre-read, dollars
// is the meter the Clef factory takes, and judging is the meter the
// YouCam runner settles into.
type books struct {
	// keyed is the two-owner split over the units pool. Its development
	// share is the read the look start makes before it takes a judge
	// seat (LooksConfig.UnitsLeft). No meter is built over the share
	// here, because no production path spends a unit outside a judge
	// lease; the next one takes the account `Spend.Development` hands
	// out, not a meter of its own.
	keyed *coststore.KeyedBudget
	// dollars is the Workers AI books. Compose builds the context meter
	// the Clef factory takes over its account.
	dollars *coststore.Store
	// judging meters the look routes' spend into the judging share. It
	// is LooksConfig.Meter, the YouCam runner's own.
	judging *cost.Meter
}

// openBooks opens the two spend namespaces on db. The settings file owns
// the numbers: the ceilings and the judging split are rewritten on every
// open, so the books a running server enforces are the books the
// operator's file describes, and `dresscode budget` reading the same
// namespaces through a second handle reads what this server settles.
func openBooks(ctx context.Context, db *sqlite.DB, settings *config.Settings, now func() time.Time, log *slog.Logger) (*books, error) {
	units := settings.Budgets.YouCam
	if err := checkUnitsBudget(units); err != nil {
		return nil, err
	}
	dollarLimit, err := settings.Budgets.WorkersAI.Limit()
	if err != nil {
		return nil, err
	}
	unitStore, err := coststore.Open(ctx, coststore.Config{
		DB:           db,
		Namespace:    NamespaceUnits,
		Limit:        cost.Price(units.Units),
		Period:       units.Period(),
		Denomination: units.Denomination(),
		Now:          now,
		Logger:       log,
	})
	if err != nil {
		return nil, err
	}
	keyed := coststore.NewKeyedBudget(unitStore)
	for _, owner := range []struct {
		name  string
		limit cost.Price
	}{
		{name: config.OwnerJudging, limit: cost.Price(units.JudgingUnits)},
		{name: config.OwnerDevelopment, limit: cost.Price(units.DevelopmentUnits())},
	} {
		if err := keyed.SetLimit(ctx, owner.name, owner.limit); err != nil {
			return nil, fmt.Errorf("the %s share: %w", owner.name, err)
		}
	}
	dollarStore, err := coststore.Open(ctx, coststore.Config{
		DB:        db,
		Namespace: NamespaceDollars,
		Limit:     dollarLimit,
		Period:    settings.Budgets.WorkersAI.Period(),
		Now:       now,
		Logger:    log,
	})
	if err != nil {
		return nil, err
	}
	judgingAccount, err := keyed.Owner(ctx, config.OwnerJudging)
	if err != nil {
		return nil, err
	}
	judging, err := cost.NewContextMeter(judgingAccount, unitStore)
	if err != nil {
		return nil, err
	}
	return &books{
		keyed:   keyed,
		dollars: dollarStore,
		judging: judging,
	}, nil
}

// catalogOwner owns the catalog's pictures. It is not a user id and is
// never one, so an account erasure that sweeps blobs by owner can never
// take a catalog picture, and the catalog is not user data.
const catalogOwner = "catalog"

// catalogContentType is the store type a catalog picture file carries,
// keyed by its extension. A file of another kind is not a picture and is
// skipped, not failed: the directory may carry notes beside the
// products.
var catalogContentType = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
}

// seededCatalog maps the catalog product ids to the store pictures the
// shop join and the render's picture seam read. Published only after
// every picture is seeded, so the lookups that read it need no lock.
type seededCatalog struct {
	byProduct map[string]string
}

// lookup is the LooksConfig.CatalogMedia seam.
func (c *seededCatalog) lookup(_ context.Context, productID string) (string, bool) {
	id, ok := c.byProduct[productID]
	return id, ok
}

// catalogBlobID is the store id a product's picture is seeded under. It
// is derived from the product id, so a restart seeds nothing a second
// time and two boots over one store hand out the same picture.
func catalogBlobID(productID string) string {
	sum := sha256.Sum256([]byte("dresscode-catalog/" + productID))
	return hex.EncodeToString(sum[:])[:32]
}

// seedCatalog copies the catalog products' pictures into the server's
// mediastore, one blob per product, public, because a shop list names
// the picture for every caller and the media route authorizes private
// blobs against their owner. The pictures come from
// <SamplesDir>/products, the layout the sample set already holds: the
// file name without its extension is the product id. A picture already
// in the store under its derived id is adopted, not rewritten, so a
// restart over the same database pays no bytes and mints no second id.
func seedCatalog(ctx context.Context, media *mediastore.Store, index *mediasql.Store, samplesDir string) (*seededCatalog, error) {
	dir := filepath.Join(samplesDir, "products")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("the catalog pictures: %w", err)
	}
	seeded := &seededCatalog{byProduct: make(map[string]string, len(entries))}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		contentType, ok := catalogContentType[ext]
		if !ok {
			continue
		}
		productID := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		blobID := catalogBlobID(productID)
		if _, err := index.Get(ctx, blobID); err == nil {
			seeded.byProduct[productID] = blobID
			continue
		} else if !errors.Is(err, mediastore.ErrNotFound) {
			return nil, fmt.Errorf("the catalog picture for %s: %w", productID, err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("the catalog picture for %s: %w", productID, err)
		}
		if err := media.PersistWithID(ctx, blobID, bytes.NewReader(raw), mediastore.Put{
			Owner:       catalogOwner,
			Group:       "catalog",
			ContentType: contentType,
			Visibility:  mediastore.Public,
		}); err != nil {
			return nil, fmt.Errorf("the catalog picture for %s: %w", productID, err)
		}
		seeded.byProduct[productID] = blobID
	}
	return seeded, nil
}

// blobStore is the deletion mount's mediastore seam: the index handle
// lists the rows, and the store's Delete takes the row and then the
// bytes. *mediastore.Store alone does not list, so the composition opens
// the index beside the intake's store.
type blobStore struct {
	media *mediastore.Store
	index *mediasql.Store
}

func (b blobStore) Blobs(ctx context.Context) ([]mediastore.Blob, error) {
	return b.index.Blobs(ctx)
}

func (b blobStore) Delete(ctx context.Context, blobID string) error {
	return b.media.Delete(ctx, blobID)
}

// Drain waits for the jobs the runners are still working to go terminal:
// the looks mid-render, the deletions mid-erase, all kinds on one store.
// It is the shutdown posture's second step, after the listener stops
// taking requests: the render that has started is not a request any
// more, it is a runner goroutine, and the demo must not strand it
// mid-render. A caller whose context ends first gets the context error
// back and closes anyway; the rows still unfinished stay unfinished, and
// the restart interrupts them and resumes them from their snapshots.
func (s *Server) Drain(ctx context.Context, every time.Duration) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("server: drain: closed server")
	}
	if s.jobs == nil {
		return fmt.Errorf("server: drain: the job rows are not open")
	}
	if every <= 0 {
		every = 250 * time.Millisecond
	}
	for {
		left, err := s.jobs.Unfinished(ctx)
		if err != nil {
			return fmt.Errorf("server: drain: %w", err)
		}
		if len(left) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(every):
		}
	}
}

// Leftover names the jobs the drain is still waiting for: the unfinished
// rows on the one jobs store, all kinds together, by id. It is the read
// the shutdown posture's timeout report makes, so a process that runs
// out of its grace names what it leaves for the next boot to interrupt
// and resume from their snapshots.
func (s *Server) Leftover(ctx context.Context) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("server: leftover: closed server")
	}
	if s.jobs == nil {
		return nil, fmt.Errorf("server: leftover: the job rows are not open")
	}
	left, err := s.jobs.Unfinished(ctx)
	if err != nil {
		return nil, fmt.Errorf("server: leftover: %w", err)
	}
	ids := make([]string, 0, len(left))
	for _, rec := range left {
		ids = append(ids, rec.ID)
	}
	return ids, nil
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
