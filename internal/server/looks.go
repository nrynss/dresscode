// Looks is this file: the look routes of the T1.5 contract, and the
// wiring that starts one. A start validates the request, refuses past
// the runtime flags, the judge session's caps and the unit budget
// before anything is spent, recalls the account's style memory when
// history is on, and hands the look to the pipeline as one Keel job.
// The rest of the routes read: the job state and its event stream, the
// verdict, the renders, the shop list, the keep / dismiss / bought
// verdict, and the history switch with its recall.
//
// The wiring honours the seams the landed pipeline notes deferred here.
// config.Judge and config.Check map onto pipeline.Thresholds and
// pipeline.Lines per look: the judge refuses an override for a criterion
// off the occasion, so the settings' per_criterion map is projected onto
// the look's occasion the way internal/eval.thresholdsFor does - for a
// fresh start and for a resumed attempt alike, because the resume hook
// below is this file's, built over pipeline.Run. A failed recall is a
// first visit: logged, and the look starts with no recall, so a Lambo
// fault never fails a start. A failed remember never fails a verdict:
// the row is stored first, and the answer carries remembered false. The
// shop frame and the shop path carry the contract's media_id and url:
// the pipeline names products, and this file joins them to the store's
// pictures when it serves.
//
// Two identities share one id here on purpose: the look's id is the job
// id the runner minted for it. The look row is the look; the job is its
// engine. The resume hook reads the snapshot under that one id, so a
// restarted look costs no Lambo call and no repeated paid step.

package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nrynss/dresscode/internal/config"
	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/memory"
	"github.com/nrynss/dresscode/internal/pipeline"
	"github.com/nrynss/dresscode/internal/systemone"
	"github.com/nrynss/dresscode/internal/youcam"
	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/flag"
	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/identity"
	"github.com/nrynss/keel/job"
	jobsql "github.com/nrynss/keel/job/sqlitestore"
	"github.com/nrynss/keel/mediastore"
	"github.com/nrynss/keel/providertask"
	ptsql "github.com/nrynss/keel/providertask/sqlitestore"
	"github.com/nrynss/keel/sqlite"
	"github.com/nrynss/keel/stream"
	"github.com/nrynss/keel/wire"
)

// The look routes and the history routes. RouteLook, the gated start,
// is declared in server.go; the job read and its stream are declared in
// delete.go and shared with the deletion mount.
const (
	routeLookList    = "GET /api/looks"
	routeLookOne     = "GET /api/looks/{id}"
	routeLookRenders = "GET /api/looks/{id}/renders"
	routeLookShop    = "GET /api/looks/{id}/shop"
	routeLookVerdict = "PUT /api/looks/{id}/verdict"
	routeHistory     = "GET /api/history"
	routeHistoryPut  = "PUT /api/history"
	routeRecall      = "GET /api/history/recall"
)

// The wire codes and phrases this file answers with. The messages are
// the contract fixtures'.
const (
	codeLeaseRefused  = "lease_refused"
	msgLeaseRefused   = "This session cannot start another fresh look."
	codeBudgetRefused = "budget_refused"
	msgBudgetRefused  = "budget refused"
	codeNoFace        = "no_face_in_photo"
	msgNoFace         = "no face in photo"
	msgLookFailed     = "The look could not be finished."
	capFreshSelfies   = "fresh_selfies"
	capRenders        = "renders"
	budgetYouCam      = "youcam"
	historyOffNotice  = "Style history is off. Turn it on to remember, in words only, the occasion, the fixes you keep or dismiss, and the products. No photo is kept for this. It stays until you delete it."
	historyOnNotice   = "Style history is on. Dresscode remembers, in words only, the occasion, the fixes you keep or dismiss, and the products. No photo is kept for this. It stays until you delete it."
)

// rendersOffFlag is the renders switch. The render stage reads it through
// pipeline.Deps.RendersPaused on every attempt: a paused render is a
// refusal, not a failed look, so the suggestion stands and shop still
// runs. The start does not read it - the contract says renders_paused
// does not refuse a start.
var rendersOffFlag = flag.Bool{
	Name:    config.FlagRendersOff,
	Default: false,
	Help:    "Refuse a YouCam render.",
}

// lookAttempts mirrors the pipeline kind's attempt cap. The pipeline's
// own is unexported; the two must move together, and the resume kind
// below must never exceed what the paid keys can resume.
const lookAttempts = 2

// LooksConfig is the wiring MountLooks adds on top of Open. The clients
// and the budget belong to the caller; the stores, the job runner and
// the routes belong to this file. Nothing here dials.
type LooksConfig struct {
	// OccasionsDir is the occasions data directory, the one
	// domain.Load reads: the five occasions, the look vocabulary, and
	// the catalog. Empty is refused.
	OccasionsDir string

	// Settings is the loaded settings file. The judge and check lines
	// and the lease caps are read from it on every look; the budgets
	// and the flags sections belong to the caller's wiring. Nil is
	// refused.
	Settings *config.Settings

	// Clef asks the model. The judge, the pick and the check are its
	// three calls. Nil is refused.
	Clef systemone.SystemOne

	// YouCam is the try-on client. The runner is built here, over the
	// intake's mediastore, so a render lands where the media route and
	// the shop join read it. Nil is refused.
	YouCam youcam.YouCam

	// Meter settles the runner's units. Nil is refused.
	Meter *cost.Meter

	// UnitsLeft is the unit budget's remaining pool, read before a look
	// starts. Nil is refused. A remaining below the look's estimate
	// refuses the start before any paid call.
	UnitsLeft func() (cost.Price, error)

	// Memory is the style-memory seam. Nil is refused: with history on
	// the routes call it, and a nil would turn every verdict into a
	// silent miss.
	Memory memory.Memory

	// CatalogMedia names the store picture a catalog product wears.
	// The catalog is not user data; its photos are seeded into
	// mediastore beside the database, and this seam says which picture
	// belongs to which product id. Nil means no catalog product carries
	// a picture, which leaves every render that composites one unable
	// to run; production passes a real lookup.
	CatalogMedia func(ctx context.Context, productID string) (string, bool)

	// Now is the clock for look rows. Nil means time.Now.
	Now func() time.Time

	// Log receives faults. Nil uses the server's logger.
	Log *slog.Logger
}

// lookRuntime is everything the look routes read. One server holds one.
type lookRuntime struct {
	srv     *Server
	log     *slog.Logger
	now     func() time.Time
	broker  *stream.Broker
	jobs    *job.Runner
	snaps   snapshots
	cam     *youcam.Runner
	media   *mediastore.Store
	flags   flag.Store
	clef    systemone.SystemOne
	mem     memory.Memory
	units   func() (cost.Price, error)
	catalog func(ctx context.Context, productID string) (string, bool)
	cfg     *config.Settings

	loaded   domain.Loaded
	occasion map[string]*domain.Occasion
}

var (
	looksMu sync.Mutex
	looksBy = map[*Server]*lookRuntime{}
)

// MountLooks attaches the look routes, the history routes, and the job
// read and stream for look jobs. Call it before the process listens.
// The job pair is shared with MountDeletion and dispatched by the job's
// kind (jobs.go), so either mount may come first. A second call refuses.
//
// The mount replaces the spending route's admission answer with the
// start handler, so the gate stands in front of a real look.
func (s *Server) MountLooks(ctx context.Context, cfg LooksConfig) error {
	if s == nil || s.db == nil || s.mux == nil {
		return fmt.Errorf("server: looks: nil server")
	}
	if ctx == nil {
		return fmt.Errorf("server: looks: nil context")
	}
	if cfg.OccasionsDir == "" {
		return fmt.Errorf("server: looks: no occasions directory")
	}
	if cfg.Settings == nil {
		return fmt.Errorf("server: looks: no settings")
	}
	if cfg.Clef == nil || cfg.YouCam == nil || cfg.Meter == nil {
		return fmt.Errorf("server: looks: clef, youcam and meter are required")
	}
	if cfg.UnitsLeft == nil || cfg.Memory == nil {
		return fmt.Errorf("server: looks: units and memory are required")
	}
	looksMu.Lock()
	defer looksMu.Unlock()
	if _, ok := looksBy[s]; ok {
		return fmt.Errorf("server: looks: already mounted")
	}
	log := cfg.Log
	if log == nil {
		log = s.log
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	loaded, err := domain.Load(cfg.OccasionsDir)
	if err != nil {
		return fmt.Errorf("server: looks: %w", err)
	}
	if len(loaded.Occasions) == 0 || len(loaded.Vocabulary.Attributes) == 0 || len(loaded.Products) == 0 {
		return fmt.Errorf("server: looks: the occasions data is incomplete")
	}
	if err := checkLookSettings(cfg.Settings); err != nil {
		return fmt.Errorf("server: looks: %w", err)
	}
	in := s.intakeOf()
	if in == nil || in.media == nil {
		return fmt.Errorf("server: looks: the intake is not mounted")
	}
	if err := migrateLooks(ctx, s.db); err != nil {
		return fmt.Errorf("server: looks: %w", err)
	}
	tasks, err := ptsql.Open(ctx, ptsql.Config{DB: s.db})
	if err != nil {
		return fmt.Errorf("server: looks: %w", err)
	}
	cam, err := youcam.NewRunner(cfg.YouCam, youcam.RunnerConfig{
		Store: tasks,
		Meter: cfg.Meter,
		Media: in.media,
		Config: providertask.Config{
			PollBase:    50 * time.Millisecond,
			PollCeiling: 2 * time.Second,
		},
	})
	if err != nil {
		return fmt.Errorf("server: looks: %w", err)
	}
	rt := &lookRuntime{
		srv:      s,
		log:      log,
		now:      now,
		broker:   stream.New(stream.Config{}),
		snaps:    snapshots{db: s.db, now: now},
		cam:      cam,
		media:    in.media,
		flags:    in.flags,
		clef:     cfg.Clef,
		mem:      cfg.Memory,
		units:    cfg.UnitsLeft,
		catalog:  cfg.CatalogMedia,
		cfg:      cfg.Settings,
		loaded:   loaded,
		occasion: make(map[string]*domain.Occasion, len(loaded.Occasions)),
	}
	for i := range loaded.Occasions {
		occ := &loaded.Occasions[i]
		rt.occasion[occ.ID] = occ
	}
	jobStore, err := jobsql.Open(ctx, jobsql.Config{DB: s.db})
	if err != nil {
		return fmt.Errorf("server: looks: %w", err)
	}
	kinds := map[string]job.Kind{pipeline.KindName: lookKind(rt)}
	runner, err := job.Open(ctx, job.Config{
		Broker: rt.broker,
		Store:  kindStore{Store: jobStore, kinds: kinds},
		Kinds:  kinds,
		Log:    log,
		Now:    now,
	})
	if err != nil {
		return fmt.Errorf("server: looks: %w", err)
	}
	rt.jobs = runner

	s.mux.Handle(routeLookList, http.HandlerFunc(rt.serveLookList))
	s.mux.Handle(routeLookOne, http.HandlerFunc(rt.serveLook))
	s.mux.Handle(routeLookRenders, http.HandlerFunc(rt.serveRenders))
	s.mux.Handle(routeLookShop, http.HandlerFunc(rt.serveShop))
	s.mux.Handle(routeLookVerdict, http.HandlerFunc(rt.serveVerdict))
	s.mux.Handle(routeHistory, http.HandlerFunc(rt.serveHistoryGet))
	s.mux.Handle(routeHistoryPut, http.HandlerFunc(rt.serveHistoryPut))
	s.mux.Handle(routeRecall, http.HandlerFunc(rt.serveRecall))
	s.mountJobRoutes()
	if err := s.SetSpendHandler(RouteLook, http.HandlerFunc(rt.serveStart)); err != nil {
		return fmt.Errorf("server: looks: %w", err)
	}
	looksBy[s] = rt
	return nil
}

func (s *Server) looksOf() *lookRuntime {
	looksMu.Lock()
	defer looksMu.Unlock()
	return looksBy[s]
}

// ---------------------------------------------------------------------------
// The kind and the dependencies

// lookKind is the job kind a look resumes under. It mirrors the
// pipeline's own Kind - idempotent, one replacement attempt, the
// snapshot rebuilt into the request - and differs in one wiring duty the
// pipeline's shape cannot carry: the thresholds are projected onto the
// resumed look's occasion, because the judge refuses an override for a
// criterion off the occasion and one settings map names overrides for
// several. The projection is internal/eval.thresholdsFor's.
func lookKind(rt *lookRuntime) job.Kind {
	return job.Kind{
		Idempotent:  true,
		MaxAttempts: lookAttempts,
		Resume: func(rec job.Record) (job.Func, error) {
			snap, ok, err := rt.snaps.Snapshot(context.Background(), rec.RootID)
			if err != nil {
				return nil, fmt.Errorf("looks: resume %s: %w", rec.ID, err)
			}
			if !ok {
				return nil, fmt.Errorf("looks: resume %s: no snapshot for look %s", rec.ID, rec.RootID)
			}
			if err := snapshotUsable(snap); err != nil {
				return nil, fmt.Errorf("looks: resume %s: %w", rec.ID, err)
			}
			req, err := rt.requestFromSnapshot(snap)
			if err != nil {
				return nil, fmt.Errorf("looks: resume %s: %w", rec.ID, err)
			}
			deps := rt.depsFor(req.Occasion)
			return func(ctx context.Context, progress func(job.Progress)) ([]byte, error) {
				look, err := pipeline.Run(ctx, deps, req, progress)
				if err != nil {
					return nil, err
				}
				return json.Marshal(look)
			}, nil
		},
	}
}

// depsFor builds the dependencies of one look on one occasion. The
// settings' lines ride per look: the judge's per_criterion overrides are
// projected onto this occasion, and the check's three lines and model
// are the file's own. The renders switch is read at the render stage,
// not here, so a flip mid-look holds the attempt that has not run yet.
func (rt *lookRuntime) depsFor(occasionID string) pipeline.Deps {
	occ := rt.occasion[occasionID]
	deps := pipeline.Deps{
		Clef:          rt.clef,
		Runner:        rt.cam,
		Cache:         rt,
		Vocab:         &rt.loaded.Vocabulary,
		Lines:         checkLines(rt.cfg.Check),
		CheckModel:    rt.cfg.Check.Model,
		Occasions:     func(id string) (*domain.Occasion, bool) { o, ok := rt.occasion[id]; return o, ok },
		Pictures:      rt,
		Catalog:       rt.loaded.Products,
		ProductRecord: rt.productRecord,
		RendersPaused: rt.rendersPaused,
	}
	if occ != nil {
		deps.Thresholds = thresholdsFor(occ, rt.cfg.Judge)
	}
	return deps
}

// rendersPaused reads the operator's renders switch. A read that fails
// pauses: the switch is a cost control, and a unit spent against a
// switch nobody could read is the worse mistake. The fault is logged.
func (rt *lookRuntime) rendersPaused(ctx context.Context) bool {
	on, _, err := rt.flags.Bool(ctx, rendersOffFlag)
	if err != nil {
		rt.log.Error("server: looks: the renders flag", "err", err.Error())
		return true
	}
	return on
}

// checkLines maps the settings' check section onto the pipeline's lines.
func checkLines(c config.Check) pipeline.Lines {
	return pipeline.Lines{
		ChangeMade:   c.ChangeMade,
		SamePerson:   c.SamePerson,
		CriterionMet: c.CriterionMet,
	}
}

// thresholdsFor projects the settings' judge lines onto one occasion.
// An override for a criterion another occasion carries is left out
// here, so the judge's own stale-setting refusal stays the guard it is.
func thresholdsFor(occ *domain.Occasion, j config.Judge) pipeline.Thresholds {
	per := make(map[string]float64, len(j.PerCriterion))
	for _, c := range occ.Criteria {
		if v, ok := j.PerCriterion[c.ID]; ok {
			per[c.ID] = v
		}
	}
	return pipeline.Thresholds{Met: j.Met, PerCriterion: per}
}

// checkLookSettings refuses a settings value the look routes cannot
// serve: the load has already validated the file, and this names the
// three lines the routes read before anything else.
func checkLookSettings(cfg *config.Settings) error {
	if cfg.Lease.Judge.FreshSelfies < 1 || cfg.Lease.Judge.Renders < 1 {
		return fmt.Errorf("looks: lease.judge: fresh selfies %d, renders %d",
			cfg.Lease.Judge.FreshSelfies, cfg.Lease.Judge.Renders)
	}
	if cfg.Judge.Met < 0 || cfg.Judge.Met > 1 || cfg.Check.Model == "" {
		return errors.New("looks: the judge and check lines are not set")
	}
	return nil
}

// snapshotUsable refuses a snapshot no look can be rebuilt from, the
// pipeline's own refusal carried onto the wiring side.
func snapshotUsable(snap pipeline.Snapshot) error {
	if snap.Version != 1 {
		return fmt.Errorf("snapshot version %d, want 1", snap.Version)
	}
	for name, value := range map[string]string{
		"user": snap.User, "hash": snap.Hash,
		"selfie": snap.Selfie, "occasion": snap.Occasion,
	} {
		if value == "" {
			return fmt.Errorf("no %s", name)
		}
	}
	return nil
}

// requestFromSnapshot rebuilds one look's request from its snapshot.
// The recall rides in the snapshot, so a resumed look costs no Lambo
// call; the pictures are read back by id at the stages that need them.
func (rt *lookRuntime) requestFromSnapshot(snap pipeline.Snapshot) (pipeline.Request, error) {
	req := pipeline.Request{
		Photo:    pipeline.Photo{User: snap.User, Hash: snap.Hash},
		Selfie:   snap.Selfie,
		Occasion: snap.Occasion,
		OptIn:    snap.OptIn,
		Recall:   snap.Recall,
	}
	if snap.Brought != "" {
		req.Brought = &pipeline.Product{ID: snap.Brought}
	}
	if snap.Basket != "" {
		req.Basket = &pipeline.Product{ID: snap.Basket}
	}
	return req, nil
}

// ---------------------------------------------------------------------------
// The start route

type lookStartedBody struct {
	LookID string `json:"look_id"`
	JobID  string `json:"job_id"`
}

// serveStart is the spending route's handler. The gate has already
// admitted the request; every refusal from here on takes no gate token.
func (rt *lookRuntime) serveStart(w http.ResponseWriter, r *http.Request) {
	if !rt.srv.haveUser(w, r) {
		return
	}
	if rt.srv.refuseFlag(w, r, samplesOnlyFlag, http.StatusForbidden, codeSamplesOnly, msgSamplesOnly) {
		return
	}
	if rt.srv.refuseFlag(w, r, freshUploadsOffFlag, http.StatusForbidden, codeUploadsPaused, msgUploadsPaused) {
		return
	}
	fields, ok := rt.srv.readObject(w, r, "occasion", "selfie_id", "product_ids")
	if !ok {
		return
	}
	occasion, ok := rt.srv.requiredText(w, fields, "occasion")
	if !ok {
		return
	}
	occ, known := rt.occasion[strings.TrimSpace(occasion)]
	if !known {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "occasion", Reason: "unknown occasion"})
		return
	}
	selfieID, ok := rt.srv.requiredID(w, fields, "selfie_id")
	if !ok {
		return
	}
	broughtID, basketID, ok := rt.readProductIDs(w, r, fields)
	if !ok {
		return
	}
	rt.startLook(w, r, occ, selfieID, broughtID, basketID)
}

// readProductIDs reads the request's product list. One id may be the
// user's own item and one may be a catalog product; a second of either
// kind is a value this path does not honour, because the look renders
// one brought product against one basket candidate.
func (rt *lookRuntime) readProductIDs(w http.ResponseWriter, r *http.Request, fields map[string]json.RawMessage) (brought, basket string, ok bool) {
	raw, named := fields["product_ids"]
	if !named || string(raw) == "null" {
		return "", "", true
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "product_ids", Reason: "must be a list of ids"})
		return "", "", false
	}
	user, _ := identity.UserFromContext(r.Context())
	for _, raw := range ids {
		one := strings.TrimSpace(raw)
		if !id.Valid(one) {
			writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "product_ids", Reason: "must be a list of ids"})
			return "", "", false
		}
		_, err := rt.userProduct(r.Context(), user.ID, one)
		if err != nil && !errors.Is(err, errNoUserProduct) {
			writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
			return "", "", false
		}
		switch {
		case errors.Is(err, errNoUserProduct):
			if _, isCatalog := rt.catalogProduct(one); !isCatalog {
				writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "product_ids", Reason: "names no product this user or catalog holds"})
				return "", "", false
			}
			if basket != "" {
				writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "product_ids", Reason: "names more than one catalog product"})
				return "", "", false
			}
			basket = one
		default:
			if brought != "" {
				writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "product_ids", Reason: "names more than one of this user's products"})
				return "", "", false
			}
			brought = one
		}
	}
	return brought, basket, true
}

// judgeLease takes the judge session's seats for one look: one fresh
// selfie and the pair of renders a look may make, the settings' own
// arithmetic (the fresh-selfie cap is one per occasion; the render cap
// is a render and a runner-up for each). A session out of seats is
// refused with the cap that has none, and no seat is taken.
//
// The seats are the last thing a start takes, after the selfie, the
// products and the budget have all been read: a refusal above this line
// spends no judge capacity, so a user who mistypes an id does not burn
// the session's fresh looks.
func (rt *lookRuntime) judgeLease(w http.ResponseWriter, r *http.Request) bool {
	session, ok := identity.SessionFromContext(r.Context())
	if !ok || session.ID == "" {
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return false
	}
	freshCap := rt.cfg.Lease.Judge.FreshSelfies
	renderCap := rt.cfg.Lease.Judge.Renders
	if freshCap < 1 || renderCap < 1 {
		rt.log.Error("server: looks: the lease caps are unset")
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return false
	}
	ctx := r.Context()
	if _, err := rt.srv.db.Writer().ExecContext(ctx,
		`INSERT INTO judge_sessions (session_id, fresh_selfies, renders) VALUES (?, 0, 0)
		 ON CONFLICT (session_id) DO NOTHING`, session.ID); err != nil {
		rt.log.Error("server: looks: judge lease", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return false
	}
	taken, err := rt.srv.db.Writer().ExecContext(ctx,
		`UPDATE judge_sessions SET fresh_selfies = fresh_selfies + 1, renders = renders + 2
		 WHERE session_id = ? AND fresh_selfies + 1 <= ? AND renders + 2 <= ?`,
		session.ID, freshCap, renderCap)
	if err != nil {
		rt.log.Error("server: looks: judge lease", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return false
	}
	n, err := taken.RowsAffected()
	if err != nil {
		rt.log.Error("server: looks: judge lease", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return false
	}
	if n == 1 {
		return true
	}
	var held struct{ fresh, renders int }
	if err := rt.srv.db.Reader().QueryRowContext(ctx,
		`SELECT fresh_selfies, renders FROM judge_sessions WHERE session_id = ?`, session.ID,
	).Scan(&held.fresh, &held.renders); err != nil {
		rt.log.Error("server: looks: judge lease", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return false
	}
	cap := capFreshSelfies
	if held.fresh < freshCap {
		cap = capRenders
	}
	writeAPI(w, http.StatusConflict, codeLeaseRefused, msgLeaseRefused, map[string]string{"cap": cap})
	return false
}

// releaseJudgeSeat hands back the counters one start took. Only a start
// that delivered nothing calls it: the look was never recorded, so the
// session spent no seat. The lease is a count, so giving it back is the
// same arithmetic that took it.
func (rt *lookRuntime) releaseJudgeSeat(ctx context.Context, sessionID string) {
	if _, err := rt.srv.db.Writer().ExecContext(ctx,
		`UPDATE judge_sessions SET fresh_selfies = fresh_selfies - 1, renders = renders - 2
		 WHERE session_id = ?`, sessionID); err != nil {
		rt.log.Error("server: looks: hand back the judge seat", "err", err.Error())
	}
}

// budgetAdmits is the budget check: the look's estimate is the units its
// opted-in menu could spend, two renders and the heaviest analysis
// load. Nothing is held here; the runner's own quotes are the enforced
// reservation, and this check is the one that refuses before a paid
// call instead of mid-look.
func (rt *lookRuntime) budgetAdmits(w http.ResponseWriter, occ *domain.Occasion, optin []string) bool {
	left, err := rt.units()
	if err != nil {
		rt.log.Error("server: looks: units left", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return false
	}
	estimate := lookEstimate(occ, optin)
	if left < estimate {
		writeAPI(w, http.StatusConflict, codeBudgetRefused, msgBudgetRefused, map[string]string{"budget": budgetYouCam})
		return false
	}
	return true
}

// lookEstimate is the units one look on this occasion could spend: the
// two priciest render quotes of the opted-in menu, the fix's and the
// runner-up's, and the heaviest analysis load one fix names. A task off
// the rate card is priced at its analysis or skipped; the runner's own
// quote is the enforced truth and this is the start's pre-read.
func lookEstimate(occ *domain.Occasion, optin []string) cost.Price {
	ticked := make(map[string]bool, len(optin))
	for _, id := range optin {
		ticked[id] = true
	}
	var renders []cost.Price
	var heaviest cost.Price
	for i := range occ.Fixes {
		fix := &occ.Fixes[i]
		if !ticked[fix.ID] {
			continue
		}
		if quote, ok := youcam.UnitsFor(fix.YouCam.Task); ok {
			renders = append(renders, quote)
		}
		var load cost.Price
		for _, a := range fix.Analysis {
			if units, ok := youcam.UnitsFor(a.Task); ok {
				load += units
			}
		}
		if load > heaviest {
			heaviest = load
		}
	}
	sort.Slice(renders, func(i, j int) bool { return renders[i] > renders[j] })
	estimate := heaviest
	for i := 0; i < len(renders) && i < 2; i++ {
		estimate += renders[i]
	}
	return estimate
}

// startLook assembles the request, takes the judge's seats, recalls,
// starts the job and answers 202. The keep writes the look row and its
// snapshot together, so the id the answer names is readable the moment
// it is given. Every refusal before the start has spent nothing.
func (rt *lookRuntime) startLook(w http.ResponseWriter, r *http.Request, occ *domain.Occasion, selfieID, broughtID, basketID string) {
	user, _ := identity.UserFromContext(r.Context())
	ctx := r.Context()

	selfieImg, hash, ok := rt.readSelfie(w, ctx, user.ID, selfieID)
	if !ok {
		return
	}
	brought, ok := rt.readBrought(w, ctx, user.ID, broughtID)
	if !ok {
		return
	}
	basket, ok := rt.readBasket(w, occ, basketID)
	if !ok {
		return
	}
	optin, err := rt.optIns(ctx, user.ID, occ.ID)
	if err != nil {
		rt.log.Error("server: looks: opt-in", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if !rt.budgetAdmits(w, occ, optin) {
		return
	}
	if !rt.judgeLease(w, r) {
		return
	}
	recall := rt.recallForStart(ctx, user.ID, occ)

	deps := rt.depsFor(occ.ID)
	jobID, err := pipeline.Start(ctx, rt.jobs, deps, rt.snaps, pipeline.Request{
		Photo:    pipeline.Photo{User: user.ID, Hash: hash, Image: selfieImg},
		Selfie:   selfieID,
		Occasion: occ.ID,
		OptIn:    optin,
		Recall:   recall,
		Brought:  brought,
		Basket:   basket,
	})
	if jobID == "" {
		rt.log.Error("server: looks: start", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if err != nil {
		// Neither the look row nor the snapshot landed: there is no look
		// to read and none to resume. The job is cancelled so no paid step
		// runs for it, the seat is handed back because this start
		// delivered nothing, and the id is not given out, because every
		// route would answer not_found for it.
		rt.log.Error("server: looks: keep the look", "job_id", jobID, "err", err.Error())
		clean := context.WithoutCancel(ctx)
		if cerr := rt.jobs.Cancel(jobID); cerr != nil && !errors.Is(cerr, job.ErrUnknownJob) {
			rt.log.Error("server: looks: cancel the unrecorded look", "job_id", jobID, "err", cerr.Error())
		}
		if session, ok := identity.SessionFromContext(ctx); ok {
			rt.releaseJudgeSeat(clean, session.ID)
		}
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if err := writeJSON(w, http.StatusAccepted, lookStartedBody{LookID: jobID, JobID: jobID}); err != nil {
		rt.log.Warn("server: looks: start", "err", err.Error())
	}
}

// readSelfie reads the kept normalised selfie: this user's, a selfie
// upload, its bytes the judge and the hash read from.
func (rt *lookRuntime) readSelfie(w http.ResponseWriter, ctx context.Context, user, selfieID string) (systemone.Image, string, bool) {
	img, blob, err := rt.openBlob(ctx, selfieID)
	if err != nil {
		if errors.Is(err, mediastore.ErrNotFound) {
			writeNotFound(w)
			return systemone.Image{}, "", false
		}
		rt.log.Error("server: looks: selfie", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return systemone.Image{}, "", false
	}
	if blob.Owner != user {
		writeNotFound(w)
		return systemone.Image{}, "", false
	}
	if blob.Group != groupSelfie {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "selfie_id", Reason: "is not a selfie upload"})
		return systemone.Image{}, "", false
	}
	digest := sha256.Sum256(img.Bytes)
	return img, hex.EncodeToString(digest[:]), true
}

// readBrought reads the user's own item with its picture.
func (rt *lookRuntime) readBrought(w http.ResponseWriter, ctx context.Context, user, broughtID string) (*pipeline.Product, bool) {
	if broughtID == "" {
		return nil, true
	}
	if _, err := rt.userProduct(ctx, user, broughtID); err != nil {
		if errors.Is(err, errNoUserProduct) {
			writeNotFound(w)
			return nil, false
		}
		rt.log.Error("server: looks: the brought product", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return nil, false
	}
	img, _, err := rt.openBlob(ctx, broughtID)
	if err != nil {
		if errors.Is(err, mediastore.ErrNotFound) {
			writeNotFound(w)
			return nil, false
		}
		rt.log.Error("server: looks: the brought product", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return nil, false
	}
	return &pipeline.Product{ID: broughtID, Image: img}, true
}

// readBasket reads the catalog candidate the render composites: the one
// the request named, else the occasion's own first candidate, the order
// the data lists them in.
func (rt *lookRuntime) readBasket(w http.ResponseWriter, occ *domain.Occasion, basketID string) (*pipeline.Product, bool) {
	if basketID == "" {
		if len(occ.Basket.Products) == 0 {
			return nil, true
		}
		basketID = occ.Basket.Products[0]
	}
	if _, ok := rt.catalogProduct(basketID); !ok {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "product_ids", Reason: "names no product this user or catalog holds"})
		return nil, false
	}
	return &pipeline.Product{ID: basketID}, true
}

// recallForStart asks the account's memory what it remembers of the
// occasion, with history on. A failure is a first visit: logged, and
// the look starts with no recall. A nil Lambo never fails a start.
func (rt *lookRuntime) recallForStart(ctx context.Context, user string, occ *domain.Occasion) *pipeline.Recall {
	if !rt.historyOn(ctx, user) {
		return nil
	}
	session, err := memory.Session(user)
	if err != nil {
		rt.log.Error("server: looks: the memory session", "err", err.Error())
		return nil
	}
	recall, err := pipeline.RecallFor(ctx, rt.mem, session, occ)
	if err != nil {
		rt.log.Error("server: looks: recall", "user", user, "occasion", occ.ID, "err", err.Error())
		return nil
	}
	return recall
}

// ---------------------------------------------------------------------------
// Data the look routes read

var errNoUserProduct = errors.New("looks: no product of this user by that id")

// userProduct reads one of the user's intake products by id.
func (rt *lookRuntime) userProduct(ctx context.Context, user, productID string) (domain.Product, error) {
	var (
		category string
		name     string
		fidelity string
	)
	err := rt.srv.db.Reader().QueryRowContext(ctx,
		`SELECT category, name, fidelity FROM products WHERE id = ? AND user_id = ?`, productID, user,
	).Scan(&category, &name, &fidelity)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Product{}, errNoUserProduct
	}
	if err != nil {
		return domain.Product{}, err
	}
	return domain.Product{
		ID:       productID,
		Category: category,
		Name:     name,
		Fidelity: domain.Fidelity(fidelity),
		Origin:   domain.OriginUser,
	}, nil
}

// catalogProduct reads one catalog entry by id.
func (rt *lookRuntime) catalogProduct(productID string) (domain.Product, bool) {
	for _, p := range rt.loaded.Products {
		if p.ID == productID {
			return p, true
		}
	}
	return domain.Product{}, false
}

// optIns reads the user's ticked fixes for one occasion, in the order
// they were stored.
func (rt *lookRuntime) optIns(ctx context.Context, user, occasion string) ([]string, error) {
	rows, err := rt.srv.db.Reader().QueryContext(ctx,
		`SELECT fix_id FROM opt_ins WHERE user_id = ? AND occasion = ? ORDER BY rowid`, user, occasion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var fix string
		if err := rows.Scan(&fix); err != nil {
			return nil, err
		}
		out = append(out, fix)
	}
	return out, rows.Err()
}

// historyOn reads the style-history switch. No row means off.
func (rt *lookRuntime) historyOn(ctx context.Context, user string) bool {
	var enabled int
	err := rt.srv.db.Reader().QueryRowContext(ctx,
		`SELECT enabled FROM history_switch WHERE user_id = ?`, user).Scan(&enabled)
	if err != nil {
		return false
	}
	return enabled == 1
}

// openBlob reads one stored picture with its type.
func (rt *lookRuntime) openBlob(ctx context.Context, mediaID string) (systemone.Image, mediastore.Blob, error) {
	if !id.Valid(mediaID) {
		return systemone.Image{}, mediastore.Blob{}, mediastore.ErrNotFound
	}
	rc, blob, err := rt.media.Open(ctx, mediaID)
	if err != nil {
		return systemone.Image{}, mediastore.Blob{}, err
	}
	defer rc.Close()
	raw, err := io.ReadAll(io.LimitReader(rc, maxPhotoInput+1))
	if err != nil {
		return systemone.Image{}, mediastore.Blob{}, err
	}
	if len(raw) > maxPhotoInput {
		return systemone.Image{}, mediastore.Blob{}, fmt.Errorf("looks: %s is over the read cap", mediaID)
	}
	return systemone.Image{ContentType: blob.ContentType, Bytes: raw}, blob, nil
}

// productRecord is the pipeline's ProductRecord: one of the user's
// intake products, carried whole so the shop entry names the record's
// own words. Only the shop stage's finish invokes it.
func (rt *lookRuntime) productRecord(ctx context.Context, user, productID string) (*domain.Product, error) {
	record, err := rt.userProduct(ctx, user, productID)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// Picture is the pipeline's Pictures: the kept selfie, a product
// picture, and a kept render, read from the store the intake and the
// runner write. A blob this user does not own is not theirs to read, so
// it falls through to the catalog seam, which is the only picture a look
// may read that is not the user's own. A miss there is not_found.
func (rt *lookRuntime) Picture(ctx context.Context, user, mediaID string) (systemone.Image, error) {
	img, blob, err := rt.openBlob(ctx, mediaID)
	if err == nil && blob.Owner == user {
		return img, nil
	}
	if err != nil && !errors.Is(err, mediastore.ErrNotFound) {
		return systemone.Image{}, err
	}
	if rt.catalog == nil {
		return systemone.Image{}, mediastore.ErrNotFound
	}
	seeded, ok := rt.catalog(ctx, mediaID)
	if !ok {
		return systemone.Image{}, mediastore.ErrNotFound
	}
	img, _, err = rt.openBlob(ctx, seeded)
	return img, err
}

// Held is the analysis cache read: one earlier read of this task for
// this user's photo. The row existing is the cache; an empty field
// inside it is a read that yielded nothing, kept so a repeat never pays
// for nothing again.
func (rt *lookRuntime) Held(ctx context.Context, user, photoHash, task string) (domain.Allowlist, bool, error) {
	var (
		face   string
		hair   string
		length string
		tones  string
	)
	err := rt.srv.db.Reader().QueryRowContext(ctx,
		`SELECT face_shape, hair_type, hair_length, colour_tones FROM analysis_cache
		 WHERE user_id = ? AND photo_hash = ? AND task = ?`, user, photoHash, task,
	).Scan(&face, &hair, &length, &tones)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Allowlist{}, false, nil
	}
	if err != nil {
		return domain.Allowlist{}, false, err
	}
	return domain.Allowlist{
		FaceShape:   face,
		HairType:    hair,
		HairLength:  length,
		ColourTones: tones,
	}, true, nil
}

// Keep stores one analysis read under its cache key, the analysis_cache
// table's own shape.
func (rt *lookRuntime) Keep(ctx context.Context, user, photoHash, task string, values domain.Allowlist) error {
	_, err := rt.srv.db.Writer().ExecContext(ctx,
		`INSERT INTO analysis_cache (user_id, photo_hash, task, face_shape, hair_type, hair_length, colour_tones)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (user_id, photo_hash, task) DO UPDATE SET
		   face_shape = excluded.face_shape, hair_type = excluded.hair_type,
		   hair_length = excluded.hair_length, colour_tones = excluded.colour_tones`,
		user, photoHash, task, values.FaceShape, values.HairType, values.HairLength, values.ColourTones)
	if err != nil {
		return fmt.Errorf("looks: keep the analysis cache: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// The look's own durable state

// migrateLooks applies this file's schema: the judge session's counters.
// The snapshot a resumed look rebuilds from is the store's own table
// (store.TableLookSnapshots), not this namespace's, because it carries
// the user's recall sentence and deletion walks the store's list. What
// is left here is keyed by a session and holds two counts, no user data.
// It is one namespace on the shared file, the way every Keel store keeps
// its own.
func migrateLooks(ctx context.Context, db *sqlite.DB) error {
	schema, err := fs.Sub(lookMigrations, "migrations")
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := sqlite.Migrate(ctx, db, lookSchemaNamespace, schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// lookSchemaNamespace is the migration ledger this file owns. It shares
// the database with the store, the identity and the job namespaces
// without colliding, because each keeps its own ledger.
const lookSchemaNamespace = "lookserver"

//go:embed migrations/*.sql
var lookMigrations embed.FS

// snapshots is the pipeline's Snapshots seam over the store's
// look_snapshots table, keyed by the job's root id - which is the look's
// id too. The look row and its snapshot land in one transaction, because
// the runner mints the id inside Start: the row the snapshot's foreign
// key names cannot exist before the keep, so the keep writes both. A look
// that got this far is readable and resumable; a keep that failed leaves
// neither, and Start returns the id beside the error so the caller can
// say the look was not recorded.
type snapshots struct {
	db  *sqlite.DB
	now func() time.Time
}

func (s snapshots) Keep(ctx context.Context, root string, snap pipeline.Snapshot) error {
	raw, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("looks: the snapshot of %s: %w", root, err)
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO looks (id, user_id, selfie_id, photo_hash, occasion, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		root, snap.User, snap.Selfie, snap.Hash, snap.Occasion, s.now().Unix()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO look_snapshots (root_id, user_id, snapshot) VALUES (?, ?, ?)
		 ON CONFLICT (root_id) DO UPDATE SET snapshot = excluded.snapshot`,
		root, snap.User, string(raw)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s snapshots) Snapshot(ctx context.Context, root string) (pipeline.Snapshot, bool, error) {
	var raw string
	err := s.db.Reader().QueryRowContext(ctx,
		`SELECT snapshot FROM look_snapshots WHERE root_id = ?`, root).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return pipeline.Snapshot{}, false, nil
	}
	if err != nil {
		return pipeline.Snapshot{}, false, err
	}
	var snap pipeline.Snapshot
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		return pipeline.Snapshot{}, false, fmt.Errorf("looks: the snapshot of %s: %w", root, err)
	}
	return snap, true, nil
}

// lookRow is one row of the looks table.
type lookRow struct {
	ID        string
	User      string
	SelfieID  string
	Hash      string
	Occasion  string
	Fix       string
	RunnerUp  string
	Verdict   string
	CreatedAt int64
}

// readLookRow reads this user's look. A missing id and another's look
// are the same not_found the contract gives an id the caller may not
// see.
func (rt *lookRuntime) readLookRow(ctx context.Context, user, lookID string) (lookRow, bool, error) {
	if !id.Valid(lookID) {
		return lookRow{}, false, nil
	}
	var row lookRow
	err := rt.srv.db.Reader().QueryRowContext(ctx,
		`SELECT id, user_id, selfie_id, photo_hash, occasion, fix, runner_up, verdict, created_at
		 FROM looks WHERE id = ? AND user_id = ?`, lookID, user,
	).Scan(&row.ID, &row.User, &row.SelfieID, &row.Hash, &row.Occasion, &row.Fix, &row.RunnerUp, &row.Verdict, &row.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return lookRow{}, false, nil
	}
	if err != nil {
		return lookRow{}, false, err
	}
	return row, true, nil
}

// lookJob is the job behind a look: the attempt the id names, the
// newest attempt, and the outcome data when the look finished.
type lookJob struct {
	status job.Status
	data   []byte
	code   string
	next   string
	found  bool
}

// jobOf reads the job a look id names. The look's id is the job's root
// id, so the attempts under it are the look's; the newest attempt that
// finished carries the outcome. A resume that replaced an attempt
// reports itself as next while it is still queued or running.
func (rt *lookRuntime) jobOf(ctx context.Context, lookID string) (lookJob, error) {
	var out lookJob
	attempts, err := rt.jobs.Attempts(ctx, lookID)
	if err != nil || len(attempts) == 0 {
		return out, nil
	}
	out.found = true
	newest := attempts[0]
	var done job.Record
	for _, attempt := range attempts {
		if attempt.Attempt >= newest.Attempt {
			newest = attempt
		}
		if attempt.Status == job.StatusDone && attempt.RootID == lookID {
			done = attempt
		}
	}
	out.status = newest.Status
	if newest.ID != lookID {
		out.next = newest.ID
	}
	if newest.Status == job.StatusError {
		out.code = newest.Code
	}
	if done.Status == job.StatusDone {
		out.data = done.Data
	}
	return out, nil
}

// materialise writes a finished look's outcome into the store: the
// picked fixes onto the row, the flags, the render with its check, and
// the shop list the look showed. Every write is keyed and idempotent,
// so two readers materialise the same look into the same rows. A look
// that failed writes nothing.
func (rt *lookRuntime) materialise(ctx context.Context, row lookRow) error {
	job, err := rt.jobOf(ctx, row.ID)
	if err != nil {
		return err
	}
	if !job.found || job.data == nil {
		return nil
	}
	var look pipeline.Look
	if err := json.Unmarshal(job.data, &look); err != nil {
		return fmt.Errorf("looks: the outcome of %s: %w", row.ID, err)
	}
	if _, err := rt.srv.db.Writer().ExecContext(ctx,
		`UPDATE looks SET fix = ?, runner_up = ? WHERE id = ? AND fix = ''`,
		look.FixID, look.RunnerUpID, row.ID); err != nil {
		return err
	}
	occ := rt.occasion[row.Occasion]
	if occ == nil {
		return fmt.Errorf("looks: the look %s names occasion %q, which is not loaded", row.ID, row.Occasion)
	}
	for i, flag := range look.Flags {
		if _, err := rt.srv.db.Writer().ExecContext(ctx,
			`INSERT OR IGNORE INTO look_flags (look_id, user_id, position, criterion_id) VALUES (?, ?, ?, ?)`,
			row.ID, row.User, i, flag.ID); err != nil {
			return err
		}
	}
	if look.Shown != nil {
		if _, err := rt.srv.db.Writer().ExecContext(ctx,
			`INSERT OR IGNORE INTO renders (id, look_id, user_id, media_id, fix_id, position, discarded) VALUES (?, ?, ?, ?, ?, 0, 0)`,
			row.ID+"-render", row.ID, row.User, look.Shown.MediaID, look.Shown.FixID); err != nil {
			return err
		}
	}
	if look.Shown != nil && look.Shown.Check != nil {
		check := look.Shown.Check
		if _, err := rt.srv.db.Writer().ExecContext(ctx,
			`INSERT OR IGNORE INTO render_checks (render_id, look_id, user_id, change_made, same_person, criterion_met) VALUES (?, ?, ?, ?, ?, ?)`,
			row.ID+"-render", row.ID, row.User, b2i(check.ChangeMade), b2i(check.SamePerson), b2i(check.CriterionMet)); err != nil {
			return err
		}
		for _, attr := range check.Attributes {
			if _, err := rt.srv.db.Writer().ExecContext(ctx,
				`INSERT OR IGNORE INTO render_check_attributes (render_id, user_id, attribute_id, value) VALUES (?, ?, ?, ?)`,
				row.ID+"-render", row.User, attr.ID, attr.Value); err != nil {
				return err
			}
		}
	}
	for i, p := range look.Products {
		if _, err := rt.srv.db.Writer().ExecContext(ctx,
			`INSERT OR IGNORE INTO look_products (look_id, user_id, position, product_id, category, name, fidelity, origin) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			row.ID, row.User, i, p.ID, p.Category, p.Name, string(p.Fidelity), string(p.Origin)); err != nil {
			return err
		}
	}
	return nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// The verdict route

// flagBody is one missed criterion as the verdict carries it.
type flagBody struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Suggestion string `json:"suggestion"`
}

type fixBody struct {
	ID      string `json:"id"`
	Changes string `json:"changes"`
	Why     string `json:"why"`
}

type checkBody struct {
	ChangeMade   bool            `json:"change_made"`
	SamePerson   bool            `json:"same_person"`
	CriterionMet bool            `json:"criterion_met"`
	Attributes   []attributeBody `json:"attributes"`
}

type attributeBody struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

type renderBody struct {
	MediaID string     `json:"media_id"`
	URL     string     `json:"url"`
	FixID   string     `json:"fix_id"`
	Check   *checkBody `json:"check"`
}

// shopEntry is one product of a shop list, the pipeline's entry plus
// the server's media join. A visual simulation and a catalog product
// with no picture carry neither member.
type shopEntry struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Name     string `json:"name"`
	Fidelity string `json:"fidelity"`
	Origin   string `json:"origin"`
	MediaID  string `json:"media_id,omitempty"`
	URL      string `json:"url,omitempty"`
}

// lookBody is the verdict read, every member present even when empty:
// shown_render null when nothing was shown, words "" when the look said
// nothing, suggestion_only false when a render stood. Remembered is the
// verdict's own answer and rides only on the verdict route.
type lookBody struct {
	ID             string      `json:"id"`
	JobID          string      `json:"job_id"`
	Occasion       string      `json:"occasion"`
	SelfieID       string      `json:"selfie_id"`
	SelfieURL      string      `json:"selfie_url"`
	Ready          bool        `json:"ready"`
	Flags          []flagBody  `json:"flags"`
	Fix            *fixBody    `json:"fix"`
	RunnerUp       string      `json:"runner_up"`
	ShownRender    *renderBody `json:"shown_render"`
	SuggestionOnly bool        `json:"suggestion_only"`
	Products       []shopEntry `json:"products"`
	Verdict        string      `json:"verdict"`
	Words          string      `json:"words"`
	Remembered     *bool       `json:"remembered,omitempty"`
}

// lookPendingBody is the read of a look that has not finished: the ids
// only. There is no outcome to carry yet, and the contract does not
// answer an empty one.
type lookPendingBody struct {
	ID        string `json:"id"`
	JobID     string `json:"job_id"`
	Occasion  string `json:"occasion"`
	SelfieID  string `json:"selfie_id"`
	SelfieURL string `json:"selfie_url"`
	Ready     bool   `json:"ready"`
	Verdict   string `json:"verdict"`
}

func (b lookBody) MarshalJSON() ([]byte, error) {
	if !b.Ready {
		return json.Marshal(lookPendingBody{
			ID:        b.ID,
			JobID:     b.JobID,
			Occasion:  b.Occasion,
			SelfieID:  b.SelfieID,
			SelfieURL: b.SelfieURL,
			Ready:     false,
			Verdict:   b.Verdict,
		})
	}
	type verdict lookBody
	return json.Marshal(verdict(b))
}

func (rt *lookRuntime) serveLook(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r)
	if !ok {
		return
	}
	row, ok, err := rt.readLookRow(r.Context(), user.ID, r.PathValue("id"))
	if err != nil {
		rt.log.Error("server: looks: the look row", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if !ok {
		writeNotFound(w)
		return
	}
	body, ok := rt.verdictBody(w, r, row)
	if !ok {
		return
	}
	if err := writeJSON(w, http.StatusOK, body); err != nil {
		rt.log.Warn("server: looks: verdict", "err", err.Error())
	}
}

// verdictBody assembles the ready verdict, or the pending ids. A look
// whose job is done is materialised first, so the first read finds the
// store the same as every later one.
func (rt *lookRuntime) verdictBody(w http.ResponseWriter, r *http.Request, row lookRow) (lookBody, bool) {
	ctx := r.Context()
	if err := rt.materialise(ctx, row); err != nil {
		rt.log.Error("server: looks: materialise", "look_id", row.ID, "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return lookBody{}, false
	}
	if state, _, err := rt.readLookRow(ctx, row.User, row.ID); err == nil && state.ID == row.ID {
		row = state
	}
	out := lookBody{
		ID:        row.ID,
		JobID:     row.ID,
		Occasion:  row.Occasion,
		SelfieID:  row.SelfieID,
		SelfieURL: mediaURL(row.SelfieID),
		Verdict:   row.Verdict,
	}
	job, err := rt.jobOf(ctx, row.ID)
	if err != nil {
		rt.log.Error("server: looks: the job read", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return lookBody{}, false
	}
	if !job.found || job.data == nil {
		return out, true
	}
	var look pipeline.Look
	if err := json.Unmarshal(job.data, &look); err != nil {
		rt.log.Error("server: looks: the outcome", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return lookBody{}, false
	}
	occ := rt.occasion[row.Occasion]
	if occ == nil {
		rt.log.Error("server: looks: the look's occasion", "occasion", row.Occasion)
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return lookBody{}, false
	}
	out.Ready = true
	out.Flags = rt.flagsBody(occ, look.Flags)
	if fix := fixByID(occ, look.FixID); fix != nil {
		out.Fix = rt.fixBody(occ, fix, look.Flags)
	}
	out.RunnerUp = look.RunnerUpID
	out.SuggestionOnly = look.SuggestionOnly
	if look.Shown != nil {
		out.ShownRender = &renderBody{
			MediaID: look.Shown.MediaID,
			URL:     mediaURL(look.Shown.MediaID),
			FixID:   look.Shown.FixID,
			Check:   checkOf(look.Shown.Check),
		}
	}
	out.Products = rt.joinProducts(ctx, look.Products)
	out.Words = look.Words
	return out, true
}

// flagsBody carries the flagged criteria whole, in the judge's order,
// with the occasion's kind beside each.
func (rt *lookRuntime) flagsBody(occ *domain.Occasion, flags []domain.Criterion) []flagBody {
	out := make([]flagBody, 0, len(flags))
	for _, flag := range flags {
		body := flagBody{ID: flag.ID, Kind: string(flag.Kind), Suggestion: flag.Suggestion}
		for i := range occ.Criteria {
			if occ.Criteria[i].ID == flag.ID {
				body.Kind = string(occ.Criteria[i].Kind)
			}
		}
		out = append(out, body)
	}
	return out
}

// fixBody is the picked fix, with why the suggestion of the criterion
// the fix addresses - the one that flagged when a flag it addresses
// exists, else the fix's first.
func (rt *lookRuntime) fixBody(occ *domain.Occasion, fix *domain.Fix, flags []domain.Criterion) *fixBody {
	flagged := make(map[string]bool, len(flags))
	for _, flag := range flags {
		flagged[flag.ID] = true
	}
	why := ""
	for _, id := range fix.Criteria {
		if !flagged[id] {
			continue
		}
		if c := criterionByID(occ, id); c != nil {
			why = c.Suggestion
		}
		break
	}
	if why == "" && len(fix.Criteria) > 0 {
		if c := criterionByID(occ, fix.Criteria[0]); c != nil {
			why = c.Suggestion
		}
	}
	return &fixBody{ID: fix.ID, Changes: fix.Changes, Why: why}
}

func checkOf(check *domain.Check) *checkBody {
	if check == nil {
		return nil
	}
	out := &checkBody{
		ChangeMade:   check.ChangeMade,
		SamePerson:   check.SamePerson,
		CriterionMet: check.CriterionMet,
	}
	for _, attr := range check.Attributes {
		out.Attributes = append(out.Attributes, attributeBody{ID: attr.ID, Value: attr.Value})
	}
	return out
}

func fixByID(occ *domain.Occasion, id string) *domain.Fix {
	for i := range occ.Fixes {
		if occ.Fixes[i].ID == id {
			return &occ.Fixes[i]
		}
	}
	return nil
}

func criterionByID(occ *domain.Occasion, id string) *domain.Criterion {
	for i := range occ.Criteria {
		if occ.Criteria[i].ID == id {
			return &occ.Criteria[i]
		}
	}
	return nil
}

// joinProducts is the media join: the contract's media_id and url are
// the server's, added when a shop list is served. The user's own item
// wears its intake picture; a catalog product wears the seeded picture
// the CatalogMedia seam names. A product with neither - a visual
// simulation, and a catalog entry with no picture - carries none.
func (rt *lookRuntime) joinProducts(ctx context.Context, products []domain.Product) []shopEntry {
	out := make([]shopEntry, 0, len(products))
	for _, p := range products {
		entry := shopEntry{
			ID:       p.ID,
			Category: p.Category,
			Name:     p.Name,
			Fidelity: string(p.Fidelity),
			Origin:   string(p.Origin),
		}
		switch p.Origin {
		case domain.OriginUser:
			entry.MediaID = p.ID
			entry.URL = mediaURL(p.ID)
		case domain.OriginCatalog:
			if rt.catalog != nil {
				if seeded, ok := rt.catalog(ctx, p.ID); ok {
					entry.MediaID = seeded
					entry.URL = mediaURL(seeded)
				}
			}
		}
		out = append(out, entry)
	}
	return out
}

// ---------------------------------------------------------------------------
// The list, the renders, and the shop

type lookListItem struct {
	ID       string `json:"id"`
	Occasion string `json:"occasion"`
	Verdict  string `json:"verdict"`
	Ready    bool   `json:"ready"`
	Fix      string `json:"fix"`
	Words    string `json:"words"`
}

type lookListBody struct {
	Looks []lookListItem `json:"looks"`
}

// serveLookList answers the past looks of one occasion, newest first.
// The list is the user's looks, not style memory, and it is returned
// whether or not history is on.
func (rt *lookRuntime) serveLookList(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r)
	if !ok {
		return
	}
	occasion := strings.TrimSpace(r.URL.Query().Get("occasion"))
	if _, known := rt.occasion[occasion]; !known {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "occasion", Reason: "unknown occasion"})
		return
	}
	ctx := r.Context()
	rows, err := rt.srv.db.Reader().QueryContext(ctx,
		`SELECT id, occasion, verdict, fix FROM looks WHERE user_id = ? AND occasion = ? ORDER BY created_at DESC, id DESC`,
		user.ID, occasion)
	if err != nil {
		rt.log.Error("server: looks: the list", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	defer rows.Close()
	type listed struct {
		id, occasion, verdict, fix string
	}
	var found []listed
	for rows.Next() {
		var one listed
		if err := rows.Scan(&one.id, &one.occasion, &one.verdict, &one.fix); err != nil {
			rt.log.Error("server: looks: the list", "err", err.Error())
			writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
			return
		}
		found = append(found, one)
	}
	if err := rows.Err(); err != nil {
		rt.log.Error("server: looks: the list", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	out := make([]lookListItem, 0, len(found))
	for _, one := range found {
		row := lookRow{ID: one.id, Occasion: one.occasion, Verdict: one.verdict, Fix: one.fix}
		item := lookListItem{ID: row.ID, Occasion: row.Occasion, Verdict: row.Verdict}
		if err := rt.materialise(ctx, lookRow{ID: row.ID, User: user.ID, Occasion: row.Occasion}); err != nil {
			rt.log.Error("server: looks: materialise", "look_id", row.ID, "err", err.Error())
			writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
			return
		}
		job, err := rt.jobOf(ctx, row.ID)
		if err != nil {
			rt.log.Error("server: looks: the job read", "err", err.Error())
			writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
			return
		}
		if job.data != nil {
			var look pipeline.Look
			if err := json.Unmarshal(job.data, &look); err != nil {
				rt.log.Error("server: looks: the outcome", "err", err.Error())
				writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
				return
			}
			item.Ready = true
			item.Fix = look.FixID
			item.Words = look.Words
		}
		out = append(out, item)
	}
	if err := writeJSON(w, http.StatusOK, lookListBody{Looks: out}); err != nil {
		rt.log.Warn("server: looks: list", "err", err.Error())
	}
}

type renderListItem struct {
	MediaID string `json:"media_id"`
	URL     string `json:"url"`
	FixID   string `json:"fix_id"`
	Changes string `json:"changes"`
	Words   string `json:"words"`
}

type rendersBody struct {
	SelfieURL      string           `json:"selfie_url"`
	Shown          []renderListItem `json:"shown"`
	SuggestionOnly bool             `json:"suggestion_only"`
}

// serveRenders answers the renders the phone may display, in the order
// to offer them: the shown one, when there is one. A discarded render
// is absent, and the original is the selfie, one tap away, not inside
// shown.
func (rt *lookRuntime) serveRenders(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	row, ok, err := rt.readLookRow(ctx, user.ID, r.PathValue("id"))
	if err != nil {
		rt.log.Error("server: looks: the look row", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if !ok {
		writeNotFound(w)
		return
	}
	if err := rt.materialise(ctx, row); err != nil {
		rt.log.Error("server: looks: materialise", "look_id", row.ID, "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	out := rendersBody{SelfieURL: mediaURL(row.SelfieID), Shown: []renderListItem{}}
	job, err := rt.jobOf(ctx, row.ID)
	if err != nil {
		rt.log.Error("server: looks: the job read", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if job.data != nil {
		var look pipeline.Look
		if err := json.Unmarshal(job.data, &look); err != nil {
			rt.log.Error("server: looks: the outcome", "err", err.Error())
			writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
			return
		}
		out.SuggestionOnly = look.SuggestionOnly
		if look.Shown != nil {
			changes := ""
			if occ := rt.occasion[row.Occasion]; occ != nil {
				if fix := fixByID(occ, look.Shown.FixID); fix != nil {
					changes = fix.Changes
				}
			}
			out.Shown = append(out.Shown, renderListItem{
				MediaID: look.Shown.MediaID,
				URL:     mediaURL(look.Shown.MediaID),
				FixID:   look.Shown.FixID,
				Changes: changes,
				Words:   look.Words,
			})
		}
	}
	if err := writeJSON(w, http.StatusOK, out); err != nil {
		rt.log.Warn("server: looks: renders", "err", err.Error())
	}
}

type shopBody struct {
	Products []shopEntry `json:"products"`
}

// serveShop answers the shop list the look ended on, the same array the
// shop stage carries, with the media join added.
func (rt *lookRuntime) serveShop(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	row, ok, err := rt.readLookRow(ctx, user.ID, r.PathValue("id"))
	if err != nil {
		rt.log.Error("server: looks: the look row", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if !ok {
		writeNotFound(w)
		return
	}
	if err := rt.materialise(ctx, row); err != nil {
		rt.log.Error("server: looks: materialise", "look_id", row.ID, "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	out := shopBody{Products: []shopEntry{}}
	job, err := rt.jobOf(ctx, row.ID)
	if err != nil {
		rt.log.Error("server: looks: the job read", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if job.data != nil {
		var look pipeline.Look
		if err := json.Unmarshal(job.data, &look); err != nil {
			rt.log.Error("server: looks: the outcome", "err", err.Error())
			writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
			return
		}
		out.Products = rt.joinProducts(ctx, look.Products)
	}
	if err := writeJSON(w, http.StatusOK, out); err != nil {
		rt.log.Warn("server: looks: shop", "err", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Keep, dismiss, bought

// serveVerdict stores the user's decision, then remembers it. The
// verdict is stored before the memory write; a memory failure still
// returns 200 with remembered false, and history off reads and writes
// no memory at all.
func (rt *lookRuntime) serveVerdict(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r)
	if !ok {
		return
	}
	fields, ok := rt.srv.readObject(w, r, "verdict")
	if !ok {
		return
	}
	raw, ok := rt.srv.requiredText(w, fields, "verdict")
	if !ok {
		return
	}
	verdict := domain.Verdict(strings.TrimSpace(raw))
	switch verdict {
	case domain.VerdictKept, domain.VerdictDismissed, domain.VerdictBought:
	default:
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "verdict", Reason: "must be kept, dismissed or bought"})
		return
	}
	ctx := r.Context()
	row, ok, err := rt.readLookRow(ctx, user.ID, r.PathValue("id"))
	if err != nil {
		rt.log.Error("server: looks: the look row", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if !ok {
		writeNotFound(w)
		return
	}
	if _, err := rt.srv.db.Writer().ExecContext(ctx,
		`UPDATE looks SET verdict = ? WHERE id = ? AND user_id = ?`, string(verdict), row.ID, row.User); err != nil {
		rt.log.Error("server: looks: the verdict", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	row.Verdict = string(verdict)
	body, ok := rt.verdictBody(w, r, row)
	if !ok {
		return
	}
	remembered := rt.remember(ctx, row, verdict)
	body.Remembered = &remembered
	if err := writeJSON(w, http.StatusOK, body); err != nil {
		rt.log.Warn("server: looks: verdict", "err", err.Error())
	}
}

// remember writes one verdict fact into the account's Lambo session,
// with history on and a finished look to name the fix. The fix is the
// one the look ended on - the shown fix, else the picked fix, the rule
// the shop stage states - because the verdict is the user's answer to
// what they saw. Anything that cannot honestly write - history off, the
// look not finished, the fix unresolvable, Lambo refusing - is a
// remembered false, and the look stands.
func (rt *lookRuntime) remember(ctx context.Context, row lookRow, verdict domain.Verdict) bool {
	if !rt.historyOn(ctx, row.User) {
		return false
	}
	job, err := rt.jobOf(ctx, row.ID)
	if err != nil || !job.found || job.data == nil {
		if err != nil {
			rt.log.Error("server: looks: the job read", "err", err.Error())
		}
		return false
	}
	var look pipeline.Look
	if err := json.Unmarshal(job.data, &look); err != nil {
		rt.log.Error("server: looks: the outcome", "err", err.Error())
		return false
	}
	occ := rt.occasion[row.Occasion]
	if occ == nil {
		rt.log.Error("server: looks: the look's occasion", "occasion", row.Occasion)
		return false
	}
	fixID := look.FixID
	if look.Shown != nil && look.Shown.FixID != "" {
		fixID = look.Shown.FixID
	}
	fix := fixByID(occ, fixID)
	if fix == nil {
		rt.log.Error("server: looks: the verdict's fix", "fix", fixID)
		return false
	}
	session, err := memory.Session(row.User)
	if err != nil {
		rt.log.Error("server: looks: the memory session", "err", err.Error())
		return false
	}
	if err := pipeline.Remember(ctx, rt.mem, session, occ, pipeline.Fact{Fix: fix, Verdict: verdict}); err != nil {
		rt.log.Error("server: looks: remember", "look_id", row.ID, "err", err.Error())
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// History on and off, and the recall

type historyBody struct {
	Enabled bool   `json:"enabled"`
	Notice  string `json:"notice"`
}

func (rt *lookRuntime) serveHistoryGet(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r)
	if !ok {
		return
	}
	enabled := rt.historyOn(r.Context(), user.ID)
	body := historyBody{Enabled: enabled, Notice: historyOffNotice}
	if enabled {
		body.Notice = historyOnNotice
	}
	if err := writeJSON(w, http.StatusOK, body); err != nil {
		rt.log.Warn("server: looks: history", "err", err.Error())
	}
}

func (rt *lookRuntime) serveHistoryPut(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r)
	if !ok {
		return
	}
	fields, ok := rt.srv.readObject(w, r, "enabled")
	if !ok {
		return
	}
	raw, named := fields["enabled"]
	if !named {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "enabled", Reason: "is required"})
		return
	}
	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err != nil {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "enabled", Reason: "must be true or false"})
		return
	}
	ctx := r.Context()
	if _, err := rt.srv.db.Writer().ExecContext(ctx,
		`INSERT INTO history_switch (user_id, enabled) VALUES (?, ?)
		 ON CONFLICT (user_id) DO UPDATE SET enabled = excluded.enabled`,
		user.ID, b2i(enabled)); err != nil {
		rt.log.Error("server: looks: history", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	body := historyBody{Enabled: enabled, Notice: historyOffNotice}
	if enabled {
		body.Notice = historyOnNotice
	}
	if err := writeJSON(w, http.StatusOK, body); err != nil {
		rt.log.Warn("server: looks: history", "err", err.Error())
	}
}

// recallOffBody is history off: the bare enabled false. The account has
// no memory to name, so no occasion, line or ids ride with it.
type recallOffBody struct {
	Enabled bool `json:"enabled"`
}

// recallOnBody is history on. Every member is present even when empty:
// an occasion with no memory yet answers line "" and two empty lists,
// not a missing line.
type recallOnBody struct {
	Enabled   bool     `json:"enabled"`
	Occasion  string   `json:"occasion"`
	Line      string   `json:"line"`
	Kept      []string `json:"kept"`
	Dismissed []string `json:"dismissed"`
}

// serveRecall answers what the account's memory remembers of one
// occasion. History off is the bare enabled false, no line, no ids. A
// failed recall is a first visit here too: logged, and answered as an
// occasion with no memory yet.
func (rt *lookRuntime) serveRecall(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r)
	if !ok {
		return
	}
	occasion := strings.TrimSpace(r.URL.Query().Get("occasion"))
	occ, known := rt.occasion[occasion]
	if !known {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "occasion", Reason: "unknown occasion"})
		return
	}
	ctx := r.Context()
	if !rt.historyOn(ctx, user.ID) {
		if err := writeJSON(w, http.StatusOK, recallOffBody{Enabled: false}); err != nil {
			rt.log.Warn("server: looks: recall", "err", err.Error())
		}
		return
	}
	out := recallOnBody{Enabled: true, Occasion: occ.ID, Kept: []string{}, Dismissed: []string{}}
	session, err := memory.Session(user.ID)
	if err != nil {
		rt.log.Error("server: looks: the memory session", "err", err.Error())
		if err := writeJSON(w, http.StatusOK, out); err != nil {
			rt.log.Warn("server: looks: recall", "err", err.Error())
		}
		return
	}
	recall, err := pipeline.RecallFor(ctx, rt.mem, session, occ)
	if err != nil {
		rt.log.Error("server: looks: recall", "user", user.ID, "occasion", occ.ID, "err", err.Error())
		if err := writeJSON(w, http.StatusOK, out); err != nil {
			rt.log.Warn("server: looks: recall", "err", err.Error())
		}
		return
	}
	out.Line = recall.Line
	if len(recall.Kept) > 0 {
		out.Kept = recall.Kept
	}
	if len(recall.Dismissed) > 0 {
		out.Dismissed = recall.Dismissed
	}
	if err := writeJSON(w, http.StatusOK, out); err != nil {
		rt.log.Warn("server: looks: recall", "err", err.Error())
	}
}

// ---------------------------------------------------------------------------
// The job read and its stream

// serveJob is the job state read. A look job needs the session: its
// look row is the look's id, and a row this user does not hold is the
// same not_found as a missing one. A deletion job, when the deletion
// mount shares this process, is that mount's, and the request is
// handed to it whole.
func (rt *lookRuntime) serveJob(w http.ResponseWriter, r *http.Request) {
	view, ok := rt.lookJobVisible(w, r)
	if !ok {
		return
	}
	if err := writeJSON(w, http.StatusOK, lookJobStateOf(r.PathValue("id"), view)); err != nil {
		return
	}
}

// lookJobStateOf is the job state read with the look's wording. Keel's
// runner stores the fault's Go error, which is internal prose naming a
// fix id and a provider task; the contract answers the phrase for the
// code instead, and a code it does not name is the look's own fallback.
func lookJobStateOf(id string, view jobView) jobStateBody {
	body := jobStateOf(id, view)
	if body.Error != nil {
		body.Error.Message = lookJobMessage(body.Error.Code)
	}
	return body
}

// lookJobMessage is the phrase a look job's failure carries, by code.
func lookJobMessage(code string) string {
	if code == codeNoFace {
		return msgNoFace
	}
	return msgLookFailed
}

// serveJobEvents is the job's event stream. The frames are Keel's, with
// two rewrites on the way out: the shop frame's products are enriched
// with the media join, because the pipeline names products and the
// contract's media is the server's; and an error frame's message is the
// contract's phrase for its code, not the fault's Go error.
func (rt *lookRuntime) serveJobEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := rt.lookJobVisible(w, r); !ok {
		return
	}
	rt.broker.ServeTopic(&lookStreamWriter{ResponseWriter: w, rt: rt, ctx: r.Context()}, r, job.Topic(r.PathValue("id")))
}

// lookJobVisible reports the job an id may be read as. The dispatcher
// (jobs.go) has already matched the kind, so an id this runtime does not
// own is not_found here too.
func (rt *lookRuntime) lookJobVisible(w http.ResponseWriter, r *http.Request) (jobView, bool) {
	jobID := r.PathValue("id")
	if !id.Valid(jobID) {
		writeNotFound(w)
		return jobView{}, false
	}
	ctx := r.Context()
	user, ok := caller(w, r)
	if !ok {
		return jobView{}, false
	}
	attempts, err := rt.jobs.Attempts(ctx, jobID)
	if err == nil && len(attempts) > 0 {
		var self job.Record
		found := false
		newest := attempts[0]
		for _, attempt := range attempts {
			if attempt.ID == jobID {
				self = attempt
				found = true
			}
			if attempt.Attempt >= newest.Attempt {
				newest = attempt
			}
		}
		if found && self.Kind == pipeline.KindName {
			if _, ok, err := rt.readLookRow(ctx, user.ID, self.RootID); err != nil {
				rt.log.Error("server: looks: the look row", "err", err.Error())
				writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
				return jobView{}, false
			} else if !ok {
				writeNotFound(w)
				return jobView{}, false
			}
			view := jobView{rec: self}
			if newest.ID != self.ID {
				view.next = newest.ID
				switch newest.Status {
				case job.StatusQueued, job.StatusRunning:
					view.rec.Status = newest.Status
					view.rec.Progress = newest.Progress
				}
			}
			return view, true
		}
	}
	writeNotFound(w)
	return jobView{}, false
}

// ---------------------------------------------------------------------------
// The stream's two rewrites, on the way out

// lookStreamWriter serves a job stream through the broker and rewrites
// the two frame kinds the contract words differently from Keel: a
// progress frame on the shop stage carries the pipeline's products, and
// the media_id and url are this server's; an error frame carries the
// fault's Go error, and the message is the phrase for its code. Every
// other frame passes through byte for byte, and the writer stays a
// pass-through for anything it cannot parse - a rewrite must never hold
// a stream hostage.
type lookStreamWriter struct {
	http.ResponseWriter
	rt  *lookRuntime
	ctx context.Context
}

func (w *lookStreamWriter) Write(p []byte) (int, error) {
	for _, rewrite := range []func([]byte) ([]byte, bool){w.joinFrame, w.wordErrorFrame} {
		if out, ok := rewrite(p); ok {
			n, err := w.ResponseWriter.Write(out)
			if err != nil {
				return 0, err
			}
			if n != len(out) {
				return 0, io.ErrShortWrite
			}
			// The frame was consumed whole; the rewrite is its stand-in.
			return len(p), nil
		}
	}
	return w.ResponseWriter.Write(p)
}

// Flush passes a stream flush through, the broker writes one per frame.
func (w *lookStreamWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// frameData locates the JSON of an event frame: the offsets between its
// "data: " marker and the closing blank line. A comment frame, such as
// the heartbeat, is not a data frame and carries neither marker.
func frameData(p []byte) (int, int, bool) {
	text := string(p)
	if !strings.HasPrefix(text, "event: ") || !strings.Contains(text, "\ndata: ") {
		return 0, 0, false
	}
	start := strings.Index(text, "data: ") + len("data: ")
	end := strings.LastIndex(text, "\n\n")
	if end < start {
		return 0, 0, false
	}
	return start, end, true
}

// replaceData rebuilds a frame around new data JSON, leaving the event
// name, the id and the closing blank line as they were.
func replaceData(p []byte, start, end int, data []byte) []byte {
	var out strings.Builder
	out.Write(p[:start])
	out.Write(data)
	out.Write(p[end:])
	return []byte(out.String())
}

// joinFrame rewrites one SSE frame's shop stage when it parses and its
// products join. The frame is Keel's wire.ProgressEvent, so every member
// it carries rides out again: only detail.products is this server's.
func (w *lookStreamWriter) joinFrame(p []byte) ([]byte, bool) {
	start, end, ok := frameData(p)
	if !ok {
		return nil, false
	}
	var frame wire.ProgressEvent
	if err := json.Unmarshal(p[start:end], &frame); err != nil {
		return nil, false
	}
	if frame.Stage != "shop" {
		return nil, false
	}
	var detail struct {
		Products []domain.Product `json:"products"`
	}
	if err := json.Unmarshal(frame.Detail, &detail); err != nil {
		return nil, false
	}
	joined, err := json.Marshal(map[string]any{"products": w.rt.joinProducts(w.ctx, detail.Products)})
	if err != nil {
		return nil, false
	}
	frame.Detail = joined
	data, err := json.Marshal(frame)
	if err != nil {
		return nil, false
	}
	return replaceData(p, start, end, data), true
}

// wordErrorFrame rewrites an error frame's message. The envelope is
// double-wrapped, as Keel writes it: the event's error member is the
// same body a failed response carries, so one parser reads both.
func (w *lookStreamWriter) wordErrorFrame(p []byte) ([]byte, bool) {
	start, end, ok := frameData(p)
	if !ok {
		return nil, false
	}
	var frame wire.ErrorEvent
	if err := json.Unmarshal(p[start:end], &frame); err != nil || frame.Status != wire.EventError {
		return nil, false
	}
	var envelope struct {
		Error struct {
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Detail  json.RawMessage `json:"detail,omitempty"`
		} `json:"error"`
	}
	if err := json.Unmarshal(frame.Error, &envelope); err != nil {
		return nil, false
	}
	want := lookJobMessage(envelope.Error.Code)
	if envelope.Error.Message == want {
		return nil, false
	}
	envelope.Error.Message = want
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, false
	}
	frame.Error = body
	data, err := json.Marshal(frame)
	if err != nil {
		return nil, false
	}
	return replaceData(p, start, end, data), true
}
