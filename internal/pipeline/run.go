// Run is this file: one look as one Keel job. The stages the landed
// files each own - judge, pick, measure, render, check, shop - run here
// in the order docs/api.md freezes: judge, pick, measure, render, check,
// and, when the first check is not shown, render and check once more for
// the runner-up, then shop, which always runs last. Each stage publishes
// its event on the job's stream in the contract's shape, and nothing
// else rides an event: a probability, an analysis value, and a discarded
// render's media id have no field to travel in.
//
// A look that is interrupted resumes under the job kind below. The
// resume machinery is Keel's: the kind is idempotent, the replacement
// attempt rebuilds the look's inputs from the snapshot store, and the
// paid steps are keyed - Measure by analysis/<user>/<hash>/<task>,
// Render by render/<user>/<hash>/<fix> - so a restarted look repeats
// nothing the provider already charged.

package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/systemone"
	"github.com/nrynss/dresscode/internal/youcam"
	"github.com/nrynss/keel/job"
)

// KindName is the job kind a look runs under. Register it in the
// runner's kinds before the runner opens, or a restart cannot resume one.
const KindName = "look"

// lookAttempts is the attempt cap of the kind: the first attempt and one
// replacement. A rerun repeats nothing the provider charged, because the
// paid steps are keyed, but a look that fails twice in a row should stop
// and be retried by the user rather than loop.
const lookAttempts = 2

// snapshotVersion is the version the resume hook refuses anything but.
const snapshotVersion = 1

// The stage names and the two attempt names. They are the contract's
// spellings; a stream only speaks them.
const (
	stageJudge   = "judge"
	stagePick    = "pick"
	stageMeasure = "measure"
	stageRender  = "render"
	stageCheck   = "check"
	stageShop    = "shop"

	attemptFix      = "fix"
	attemptRunnerUp = "runner_up"
)

// The wire codes a look's failure and refusal carry. The budget refusal
// is not a failure: the suggestion stands and the look finishes. The
// message is the phrase the contract's error fixture carries.
const (
	codeBudgetRefused    = "budget_refused"
	messageBudgetRefused = "budget refused"
	codeNoFaceInPhoto    = "no_face_in_photo"
)

// Pictures reads a kept picture back by its id: the selfie a look was
// taken from, a product photo the user brought, a catalog product, and a
// render the runner copied into mediastore. The check reads the render
// back on every look, and a resumed look reads its selfie and products
// back, because the bytes of a look live in the store, not in the job
// record. The wiring resolves ids against mediastore and the catalog;
// nothing here dials.
type Pictures interface {
	Picture(ctx context.Context, user, id string) (systemone.Image, error)
}

// Snapshot is the durable input of one look job: everything a replacement
// attempt needs to rebuild the request, with no bytes in it. The picture
// ids name the kept copies; a resumed attempt reads their bytes back
// through Pictures. It travels through the Snapshots store, keyed by the
// job's root id.
type Snapshot struct {
	Version  int      `json:"version"`
	User     string   `json:"user"`
	Hash     string   `json:"hash"`
	Selfie   string   `json:"selfie"`
	Occasion string   `json:"occasion"`
	OptIn    []string `json:"opt_in,omitempty"`
	Recall   *Recall  `json:"recall,omitempty"`
	Brought  string   `json:"brought,omitempty"`
	Basket   string   `json:"basket,omitempty"`
}

// valid reports whether the snapshot carries a rebuildable look.
func (s Snapshot) valid() error {
	if s.Version != snapshotVersion {
		return fmt.Errorf("snapshot version %d, want %d", s.Version, snapshotVersion)
	}
	if s.User == "" {
		return errors.New("no user")
	}
	if s.Hash == "" {
		return errors.New("no photo hash")
	}
	if s.Selfie == "" {
		return errors.New("no kept selfie")
	}
	if s.Occasion == "" {
		return errors.New("no occasion")
	}
	return nil
}

// Snapshots keeps one look job's inputs where a restart can find them.
// It is the seam between this file and the account's own durable state:
// the wiring implements it over the look it wrote when the job started.
// Start keeps the snapshot under the job id the runner returns, so a
// look killed after its first stage event resumes; a look killed in the
// sliver before that names nothing in the record and is not resumable -
// the resume hook refuses it, Keel leaves the record interrupted and
// never retries it - so the retry is the app or the user starting a new
// look. Nothing pays twice even then: a sliver look had reached no paid
// step, and a new look's keys resume whatever the provider had already
// charged.
type Snapshots interface {
	Keep(ctx context.Context, root string, snap Snapshot) error
	Snapshot(ctx context.Context, root string) (Snapshot, bool, error)
}

// Deps is everything a look needs besides its request. One value serves
// every attempt of the job, first run and resumed alike.
type Deps struct {
	// Clef asks the model. The judge, the pick, and the check are its
	// three calls.
	Clef systemone.SystemOne
	// Runner carries the paid YouCam work. The measure and render
	// stages run through it.
	Runner *youcam.Runner
	// Cache is the analysis cache, measure.Cache.
	Cache Cache
	// Vocab is the look vocabulary the check asks.
	Vocab *domain.Vocabulary
	// Thresholds and Lines are the judge's and the check's settings
	// lines, config.Judge and config.Check mapped.
	Thresholds Thresholds
	Lines      Lines
	// CheckModel is the model the check runs on, the settings' check
	// model.
	CheckModel string
	// Occasions resolves the occasion an id names. A resumed look
	// carries an id, not an occasion.
	Occasions func(id string) (*domain.Occasion, bool)
	// Pictures reads the kept pictures a look names.
	Pictures Pictures
	// Catalog is the occasions directory's catalog.json as domain.Load
	// read it, the same load the occasion itself came from. Shop labels
	// the list against it. An empty catalog is refused: a look would
	// render first and then be held hostage at the last step, shopping
	// against nothing.
	Catalog []domain.Product
	// ProductRecord reads a product the user brought back as its intake
	// record - the domain.Product the server kept when the item was
	// added, its name and category and origin. Only the shop stage's
	// finish reads it, and only when the fix being shopped composites
	// product photos and the user brought an item; nothing else invokes
	// it. The record, not the request's shell, is what shops: a resumed
	// attempt carries only the id.
	ProductRecord func(ctx context.Context, user, id string) (*domain.Product, error)
}

func (d Deps) validate() error {
	if d.Clef == nil {
		return errors.New("run: no clef client")
	}
	if d.Runner == nil {
		return errors.New("run: no youcam runner")
	}
	if d.Cache == nil {
		return errors.New("run: no analysis cache")
	}
	if d.Vocab == nil {
		return errors.New("run: no look vocabulary")
	}
	if len(d.Vocab.Attributes) == 0 {
		return errors.New("run: the look vocabulary has no attributes")
	}
	if d.Occasions == nil {
		return errors.New("run: no occasions")
	}
	if d.Pictures == nil {
		return errors.New("run: no pictures")
	}
	if d.ProductRecord == nil {
		return errors.New("run: no product record")
	}
	if d.CheckModel == "" {
		return errors.New("run: no check model")
	}
	if len(d.Catalog) == 0 {
		return errors.New("run: no catalog")
	}
	return nil
}

// Request is one look: the photo it reads, the occasion it checks
// against, the fixes the user opened, the memory they allowed, and the
// products a render may composite. The selfie's bytes travel in the
// Photo; the kept selfie's id travels beside them, because a resumed
// attempt rebuilds the job from ids.
type Request struct {
	// Photo is the normalised selfie: its owner, its hash, its bytes.
	Photo Photo
	// Selfie is the mediastore id of the kept copy. Start refuses a
	// request without one, because a look that cannot name its selfie
	// cannot be rebuilt after a restart.
	Selfie string
	// Occasion is the occasion's id.
	Occasion string
	// OptIn is the fixes the user ticked, by id.
	OptIn []string
	// Recall is the account's memory of this occasion. A nil is history
	// off.
	Recall *Recall
	// Brought is the product the user brought, with its picture.
	Brought *Product
	// Basket is the occasion's candidate, with its picture.
	Basket *Product
}

// snapshot is the durable form of the request.
func (r Request) snapshot() Snapshot {
	snap := Snapshot{
		Version:  snapshotVersion,
		User:     r.Photo.User,
		Hash:     r.Photo.Hash,
		Selfie:   r.Selfie,
		Occasion: r.Occasion,
		OptIn:    r.OptIn,
	}
	if !r.Recall.empty() {
		snap.Recall = r.Recall
	}
	if r.Brought != nil {
		snap.Brought = r.Brought.ID
	}
	if r.Basket != nil {
		snap.Basket = r.Basket.ID
	}
	return snap
}

// validate refuses a request that names no look. The byte-level checks
// are Run's, because a resumed attempt reads its bytes back first.
func (r Request) validate() error {
	if r.Photo.User == "" {
		return errors.New("run: no user")
	}
	if r.Photo.Hash == "" {
		return errors.New("run: no photo hash")
	}
	if r.Selfie == "" {
		return errors.New("run: no kept selfie to resume a look from")
	}
	if r.Occasion == "" {
		return errors.New("run: no occasion")
	}
	return nil
}

// Look is what one run finished with: the judge's flags and biggest
// issue, the pick's two fixes, and the render that was shown, with its
// check and the look in words, and the shop list the look ends on. It is
// the job's result data, the value the server persists as the look's
// outcome. A look with no shown render stands on its suggestions, and
// SuggestionOnly says so.
type Look struct {
	Flags          []domain.Criterion `json:"flags"`
	Issue          Issue              `json:"issue"`
	FixID          string             `json:"fix_id,omitempty"`
	RunnerUpID     string             `json:"runner_up_id,omitempty"`
	Shown          *domain.Render     `json:"shown,omitempty"`
	Words          string             `json:"words,omitempty"`
	SuggestionOnly bool               `json:"suggestion_only"`
	// Products is the shop list, the same array the shop stage
	// published. The entries are Shop's own - id, category, name,
	// fidelity, origin, no media: the contract's media_id and url are
	// the server's join, added when the frame and the shop path are
	// served.
	Products []domain.Product `json:"products,omitempty"`
}

// Start runs the look as a job and returns the job id at once. The
// snapshot is kept as soon as the runner has minted the id, before the
// stages are read by anyone but the job itself, so a restart finds a
// rebuildable record for every look that got far enough to publish.
// The keep runs under the caller's context with the cancellation cut
// (context.WithoutCancel), so a cancel in the sliver between StartKind
// and the keep cannot strand the running look without its record; the
// job's drive and this call's return still honour the caller's cancel.
// A keep that fails does not stop the job that already started: the id
// comes back beside the error, and the look simply will not resume.
func Start(ctx context.Context, runner *job.Runner, deps Deps, snaps Snapshots, req Request) (string, error) {
	if runner == nil {
		return "", errors.New("run: no job runner")
	}
	if snaps == nil {
		return "", errors.New("run: no snapshots")
	}
	if err := deps.validate(); err != nil {
		return "", err
	}
	if _, ok := deps.Occasions(req.Occasion); !ok {
		return "", fmt.Errorf("run: no occasion %q", req.Occasion)
	}
	if err := req.validate(); err != nil {
		return "", err
	}
	id, err := runner.StartKind(ctx, KindName, func(ctx context.Context, progress func(job.Progress)) ([]byte, error) {
		return runJob(ctx, deps, req, progress)
	})
	if err != nil {
		return "", err
	}
	if err := snaps.Keep(context.WithoutCancel(ctx), id, req.snapshot()); err != nil {
		return id, fmt.Errorf("run: keep the look's snapshot for %s: %w", id, err)
	}
	return id, nil
}

// Kind is the job kind a look runs under: idempotent, one replacement
// attempt, and a resume hook that rebuilds the request from the
// snapshot store. The rebuilt attempt runs the whole chain again - the
// model calls are deterministic and the paid steps are keyed - and
// publishes its events on its own attempt's topic.
func Kind(deps Deps, snaps Snapshots) job.Kind {
	return job.Kind{
		Idempotent:  true,
		MaxAttempts: lookAttempts,
		Resume: func(rec job.Record) (job.Func, error) {
			snap, ok, err := snaps.Snapshot(context.Background(), rec.RootID)
			if err != nil {
				return nil, fmt.Errorf("run: resume %s: %w", rec.ID, err)
			}
			if !ok {
				return nil, fmt.Errorf("run: resume %s: no snapshot for look %s", rec.ID, rec.RootID)
			}
			if err := snap.valid(); err != nil {
				return nil, fmt.Errorf("run: resume %s: %w", rec.ID, err)
			}
			if _, ok := deps.Occasions(snap.Occasion); !ok {
				return nil, fmt.Errorf("run: resume %s: no occasion %q", rec.ID, snap.Occasion)
			}
			req := Request{
				Photo:    Photo{User: snap.User, Hash: snap.Hash},
				Selfie:   snap.Selfie,
				Occasion: snap.Occasion,
				OptIn:    snap.OptIn,
				Recall:   snap.Recall,
			}
			if snap.Brought != "" {
				req.Brought = &Product{ID: snap.Brought}
			}
			if snap.Basket != "" {
				req.Basket = &Product{ID: snap.Basket}
			}
			return func(ctx context.Context, progress func(job.Progress)) ([]byte, error) {
				return runJob(ctx, deps, req, progress)
			}, nil
		},
	}
}

// runJob is the job's function: the chain, then the result as the job's
// data bytes.
func runJob(ctx context.Context, deps Deps, req Request, progress func(job.Progress)) ([]byte, error) {
	look, err := Run(ctx, deps, req, progress)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(look)
	if err != nil {
		return nil, fmt.Errorf("run: %w", err)
	}
	return data, nil
}

// Run carries one look through its stages and returns what finished.
// Every stage publishes its event, and the events carry what the
// contract's table says and nothing more. The look fails only when a
// stage cannot honestly finish; a render the budget refuses is not such
// a failure - the suggestion stands and the look completes. Every clean
// path ends at the shop stage: finish runs Shop for the fix the look
// ends on - the shown fix when one is shown, else the picked fix - and
// publishes the products last, so `done` follows shop.
func Run(ctx context.Context, deps Deps, req Request, progress func(job.Progress)) (Look, error) {
	if err := deps.validate(); err != nil {
		return Look{}, err
	}
	occ, ok := deps.Occasions(req.Occasion)
	if !ok {
		return Look{}, fmt.Errorf("run: no occasion %q", req.Occasion)
	}
	if req.Photo.User == "" {
		return Look{}, errors.New("run: no user")
	}
	if req.Photo.Hash == "" {
		return Look{}, errors.New("run: no photo hash")
	}
	selfie := req.Photo.Image
	if len(selfie.Bytes) == 0 {
		if req.Selfie == "" {
			return Look{}, errors.New("run: no selfie bytes and no kept selfie")
		}
		img, err := deps.Pictures.Picture(ctx, req.Photo.User, req.Selfie)
		if err != nil {
			return Look{}, fmt.Errorf("run: the selfie %s: %w", req.Selfie, err)
		}
		selfie = img
	}
	photo := Photo{User: req.Photo.User, Hash: req.Photo.Hash, Image: selfie}

	// Judge. The flags are the look's reasons; the event carries them
	// in the occasion's own wording.
	j, err := Judge(ctx, deps.Clef, photo.Image, occ, deps.Thresholds)
	if err != nil {
		return Look{}, err
	}
	flags := make([]judgeFlag, len(j.Flags))
	for i, c := range j.Flags {
		flags[i] = judgeFlag{ID: c.ID, Suggestion: c.Suggestion}
	}
	emit(progress, stageJudge, judgeEvent{Flags: flags})

	// Pick. The attributes the cached analysis already holds ride along,
	// so a second look on a measured photo picks with its face read.
	attrs, err := pickAttrs(ctx, deps.Cache, photo)
	if err != nil {
		return Look{}, err
	}
	choice, err := Pick(ctx, deps.Clef, occ, j, req.OptIn, attrs, req.Recall)
	if err != nil {
		return Look{}, err
	}
	pick := pickEvent{}
	if choice.Fix != nil {
		pick.Fix = choice.Fix.ID
	}
	if choice.RunnerUp != nil {
		pick.RunnerUp = choice.RunnerUp.ID
	}
	if !req.Recall.empty() {
		pick.Recall = req.Recall.Line
	}
	emit(progress, stagePick, pick)

	look := Look{
		Flags:      j.Flags,
		Issue:      j.Issue,
		FixID:      pick.Fix,
		RunnerUpID: pick.RunnerUp,
	}
	if choice.Fix == nil {
		// Nothing was flagged, or nothing was a candidate: the look
		// stands on its suggestions, and there is no fix to measure,
		// render, check or shop.
		look.SuggestionOnly = true
		return finish(ctx, deps, req, occ, nil, look, progress)
	}

	// Measure. Only the chosen fix's analyses run, once per user, photo
	// and task.
	cached, err := cachedAlready(ctx, deps.Cache, photo, choice.Fix)
	if err != nil {
		return Look{}, err
	}
	if _, err := Measure(ctx, deps.Runner, deps.Cache, photo, choice.Fix); err != nil {
		return Look{}, job.Failure{Code: noFaceCode(err), Err: fmt.Errorf("measure: fix %q: %w", choice.Fix.ID, err)}
	}
	if len(choice.Fix.Analysis) == 0 {
		skipped := true
		emit(progress, stageMeasure, measureEvent{Skipped: &skipped})
	} else {
		emit(progress, stageMeasure, measureEvent{Cached: &cached})
	}

	// Render and check the fix. If the check discards it, the runner-up
	// renders and is checked once. After that the suggestion stands. A
	// refused render is not a failed check: the chain ends there, with
	// the refusal's event and the suggestion.
	flagged := criterionIDs(j.Flags)
	shown, words, refused, rerr := attempt(ctx, deps, photo, occ, choice.Fix, req, flagged, attemptFix, progress)
	if rerr != nil {
		return look, rerr
	}
	if shown != nil {
		shown.FixID = choice.Fix.ID
		look.Shown = shown
		look.Words = words
		return finish(ctx, deps, req, occ, choice.Fix, look, progress)
	}
	if refused {
		look.SuggestionOnly = true
		return finish(ctx, deps, req, occ, choice.Fix, look, progress)
	}

	if choice.RunnerUp == nil {
		look.SuggestionOnly = true
		return finish(ctx, deps, req, occ, choice.Fix, look, progress)
	}
	shown, words, refused, rerr = attempt(ctx, deps, photo, occ, choice.RunnerUp, req, flagged, attemptRunnerUp, progress)
	if rerr != nil {
		return look, rerr
	}
	if shown != nil {
		shown.FixID = choice.RunnerUp.ID
		look.Shown = shown
		look.Words = words
		return finish(ctx, deps, req, occ, choice.RunnerUp, look, progress)
	}
	look.SuggestionOnly = true
	return finish(ctx, deps, req, occ, choice.Fix, look, progress)
}

// finish is the shop stage's wiring, every clean path out of Run's last
// step: shop always runs after whatever the chain showed, `done`
// follows it, and the refusal arm shops as the contract says - the
// suggestion stands, shop still runs.
//
// The which-fix-shops rule: shop for the shown fix when one is shown,
// else the picked fix - the look is what is on the face. The runner-up
// shown therefore shops the runner-up; every path that ends without a
// render on the face - the pick declined, the render was refused, the
// check discarded, and no runner-up there was - shops the pick's fix.
// The fix argument is that decision, made by the caller at each return.
//
// When the fix's render composites a product photo and the user brought
// an item, the item is resolved to its intake record before Shop sees
// it, so the entry carries the record's own name and category and not
// the request's id-only shell - the shape a resumed attempt carries.
// Nothing is brought, nothing is resolved.
//
// The list is Shop's, published as the shop stage's frame and carried
// on the look. This wiring adds no media: the contract's media_id and
// url on a shop entry are the server's join - no seam here names a
// product photo's media id, and an id nothing registered is never
// invented - so the frame carries Shop's entries as they are and the
// server enriches it when it serves the stream and the shop path.
//
// A Shop refusal is the wiring bug its doc names - two loads mixed, a
// record gone - named loudly: the look fails, it is not silently
// stripped of its products.
func finish(ctx context.Context, deps Deps, req Request, occ *domain.Occasion, fix *domain.Fix, look Look, progress func(job.Progress)) (Look, error) {
	var brought *domain.Product
	if fix != nil && needProduct(fix) && req.Brought != nil {
		record, err := deps.ProductRecord(ctx, req.Photo.User, req.Brought.ID)
		if err != nil {
			return look, fmt.Errorf("run: the brought product %s: %w", req.Brought.ID, err)
		}
		brought = record
	}
	products, err := Shop(occ, fix, brought, deps.Catalog, req.Recall)
	if err != nil {
		return look, fmt.Errorf("run: shop: %w", err)
	}
	look.Products = products
	emit(progress, stageShop, shopEvent{Products: products})
	return look, nil
}

// attempt is one render-and-check pass for one fix. The render runs
// under its own key - the runner-up is a different fix and a different
// task at the provider, and it quotes the budget on its own. A budget
// refusal ends the pass with a refusal event and no render, and the
// refused return says the chain ends here: the runner-up's trigger is a
// failed check, not a refused budget, because the budget that refused
// this render refuses the next. Any other render fault, and a check
// that cannot finish, fail the look. A discarded render is not an
// error: the pass ends with no render and no refusal, and the caller
// falls back.
func attempt(ctx context.Context, deps Deps, photo Photo, occ *domain.Occasion, fix *domain.Fix, req Request, flagged map[string]bool, name string, progress func(job.Progress)) (*domain.Render, string, bool, error) {
	brought, basket := req.Brought, req.Basket
	if needProduct(fix) {
		var err error
		if brought, err = resolveProduct(ctx, deps.Pictures, photo.User, brought, "the brought product"); err != nil {
			return nil, "", false, err
		}
		if basket, err = resolveProduct(ctx, deps.Pictures, photo.User, basket, "the basket product"); err != nil {
			return nil, "", false, err
		}
	}
	rendered, err := Render(ctx, deps.Runner, photo, fix, brought, basket)
	if err != nil {
		var refused *Refused
		if errors.As(err, &refused) {
			emit(progress, stageRender, renderEvent{
				Fix:     fix.ID,
				Attempt: name,
				Code:    codeBudgetRefused,
				Message: messageBudgetRefused,
			})
			return nil, "", true, nil
		}
		return nil, "", false, job.Failure{Code: noFaceCode(err), Err: fmt.Errorf("render: fix %q: %w", fix.ID, err)}
	}
	emit(progress, stageRender, renderEvent{Fix: fix.ID, Attempt: name})

	// The check reads the render back from the store the runner kept it
	// in, the same read the app's media route serves.
	picture, err := deps.Pictures.Picture(ctx, photo.User, rendered.MediaID)
	if err != nil {
		return nil, "", false, fmt.Errorf("run: the render %s: %w", rendered.MediaID, err)
	}
	criterion := checkCriterion(fix, flagged)
	outcome, err := Check(ctx, deps.Clef, deps.CheckModel, photo.Image, picture, occ, fix, criterion, fix.Changes, deps.Vocab, deps.Lines)
	if err != nil {
		return nil, "", false, fmt.Errorf("run: check fix %q: %w", fix.ID, err)
	}
	event := checkEvent{Fix: fix.ID, Shown: outcome.Shown()}
	if outcome.Shown() {
		event.MediaID = rendered.MediaID
		event.Words = outcome.Words
	}
	emit(progress, stageCheck, event)
	if !outcome.Shown() {
		return nil, "", false, nil
	}
	return &domain.Render{MediaID: rendered.MediaID, Check: &outcome.Check}, outcome.Words, false, nil
}

// checkCriterion names the one criterion a render was checked against.
// A fix that answers two criteria - the three shirt fixes - is checked
// against the one that flagged; when nothing it addresses flagged, its
// first criterion is the one the render was meant to serve.
func checkCriterion(fix *domain.Fix, flagged map[string]bool) string {
	for _, id := range fix.Criteria {
		if flagged[id] {
			return id
		}
	}
	if len(fix.Criteria) > 0 {
		return fix.Criteria[0]
	}
	return ""
}

// criterionIDs collects the judgement's flag ids.
func criterionIDs(flags []domain.Criterion) map[string]bool {
	out := make(map[string]bool, len(flags))
	for _, c := range flags {
		out[c.ID] = true
	}
	return out
}

// needProduct reports whether the fix's render composites a product
// photo, so the run resolves the bytes only when the render needs them.
func needProduct(fix *domain.Fix) bool {
	flow, ok := renderFlows[fix.YouCam.Task]
	return ok && flow.product
}

// resolveProduct fills a product's picture from the store it was kept in
// when the request carries only the id, the shape a resumed attempt
// carries. A product with its picture is passed through untouched.
func resolveProduct(ctx context.Context, pics Pictures, user string, p *Product, what string) (*Product, error) {
	if p == nil || len(p.Image.Bytes) > 0 {
		return p, nil
	}
	img, err := pics.Picture(ctx, user, p.ID)
	if err != nil {
		return nil, fmt.Errorf("run: %s %s: %w", what, p.ID, err)
	}
	out := *p
	out.Image = img
	return &out, nil
}

// pickAttrs reads the values the cached analysis already holds for this
// photo, over every task the measure stage can run. A row that is not
// there yet is a first look; a row that is there contributes what it
// yielded. This is the read the pick request carries, and nothing else:
// the values go to the picker's allowlist and nowhere else.
func pickAttrs(ctx context.Context, cache Cache, photo Photo) (domain.Allowlist, error) {
	var out domain.Allowlist
	for task := range analysisRuns {
		held, ok, err := cache.Held(ctx, photo.User, photo.Hash, task)
		if err != nil {
			return domain.Allowlist{}, fmt.Errorf("run: the cached analysis of %s: %w", task, err)
		}
		if !ok {
			continue
		}
		for _, field := range allowlistFields {
			if v := field.get(held); v != "" {
				field.set(&out, v)
			}
		}
	}
	return out, nil
}

// cachedAlready reports whether every analysis the fix names is in the
// cache before the measure stage runs. The read is free; the answer
// only words the measure event.
func cachedAlready(ctx context.Context, cache Cache, photo Photo, fix *domain.Fix) (bool, error) {
	for _, a := range fix.Analysis {
		_, ok, err := cache.Held(ctx, photo.User, photo.Hash, a.Task)
		if err != nil {
			return false, fmt.Errorf("run: the cached analysis of %s: %w", a.Task, err)
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// noFaceCode names the wire code for a stage fault. A provider refusal
// of no face is the contract's no_face_in_photo; every other fault keeps
// the job_failed fallback by naming no code.
func noFaceCode(err error) string {
	if errors.Is(err, youcam.ErrNoFace) {
		return codeNoFaceInPhoto
	}
	return ""
}

// emit publishes one stage event. The detail is the event's whole
// payload; nothing else is attached.
func emit(progress func(job.Progress), stage string, detail any) {
	if progress == nil {
		return
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		// The event values are ids, flags, words and two booleans; they
		// cannot fail to marshal. A marshal that somehow failed would
		// carry no worse a cost than a missing progress frame.
		return
	}
	progress(job.Progress{Stage: stage, Detail: raw})
}

// The event payloads, in the contract's shapes (docs/api.md, the stage
// table). judge flags carry the user-facing suggestion; pick carries the
// two fix ids and the memory sentence when history is on; measure says
// the analysis was skipped or whether it was cached, never a value;
// render names the fix and the attempt, and a refusal names its code;
// check names the fix and whether the render was shown, and a shown
// render alone carries its media id and the look in words; shop carries
// the products, the same array as shop the look - Shop's entries, no
// media id and no url, the server's join added when it serves.
type shopEvent struct {
	Products []domain.Product `json:"products"`
}
type judgeEvent struct {
	Flags []judgeFlag `json:"flags"`
}

type judgeFlag struct {
	ID         string `json:"id"`
	Suggestion string `json:"suggestion"`
}

type pickEvent struct {
	Fix      string `json:"fix"`
	RunnerUp string `json:"runner_up"`
	Recall   string `json:"recall,omitempty"`
}

type measureEvent struct {
	Skipped *bool `json:"skipped,omitempty"`
	Cached  *bool `json:"cached,omitempty"`
}

type renderEvent struct {
	Fix     string `json:"fix"`
	Attempt string `json:"attempt"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type checkEvent struct {
	Fix     string `json:"fix"`
	Shown   bool   `json:"shown"`
	MediaID string `json:"media_id,omitempty"`
	Words   string `json:"words,omitempty"`
}
