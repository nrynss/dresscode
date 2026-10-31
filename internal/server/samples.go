// Samples is this file: the five sample looks a judge opens for free.
// GET /api/samples lists them; POST /api/samples/{id}/looks opens one.
// An open spends nothing: the sample's selfie is registered into the
// judge's own mediastore, the analysis rows its fix measures pre-exist
// in the analysis cache, the render its story showed is already a
// mediastore blob, and the look's job is a playback of the recorded
// stage state - the flags, the pick, the measure answer, the render the
// check kept, the shop list - so the judge's stream, verdict, renders
// and shop answer exactly as the recorded fixtures do, with no model
// ask, no YouCam call and no unit settled. Nothing here dials.
//
// The route is deliberately not a spending route: it takes no gate
// token, no judge-lease seat and no budget reservation, so a judge who
// has exhausted every allowance can still open a sample, and a judge
// who has opened all five still has the whole lease for a fresh selfie
// through POST /api/looks, which keeps its landed path untouched.
//
// The cached state is not a shipped database. Each sample's story is a
// recorded fixture under the samples directory (cached/*.json), and the
// rows are derived from it here, at open, under the judge's own user id
// - the id every landed look route scopes its reads by - so account
// deletion, look deletion and the user-scoped reads all treat an opened
// sample like any other look. A playback job is a look-kind job with no
// snapshot: the resume hook refuses a look with no snapshot, so a
// restart interrupts a playback that had not finished and never runs
// the paid pipeline in its place.
//
// The cache is evidence, not a wish list. Every value a story carries
// comes either from a recorded provider reply or from data the
// repository already owns - the sample set's ground truth (T0.4), the
// occasions and each fix's declared vocabulary (T0.3), the judge lines
// (T3.9) - and the file names which, in its own "source" member, because
// a number whose source nobody can point at is a number that was made
// up. loadSample turns that into rules it can check: the flags are the
// variant's miss list, the selfie is that variant's picture for its
// occasion, an analysis row names the picture the provider actually read
// and must be the same person, a shown render carries the check and only
// the look answers its fix declares changing, and a discarded render
// carries no check at all, since none was recorded for it. What the
// recordings do not tell, the story does not say.

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/pipeline"
	"github.com/nrynss/dresscode/internal/systemone"
	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/job"
	"github.com/nrynss/keel/mediastore"
	"github.com/nrynss/keel/wire"
)

const (
	routeSamples     = "GET /api/samples"
	routeSampleStart = "POST /api/samples/{id}/looks"

	// sampleCacheKind names the recorded story files. A file of another
	// kind is a mount error, the way a foreign snapshot version is.
	sampleCacheKind = "sample-cache"
)

// sampleIDs are the five samples, in the order the list answers. The
// recorded list example (testdata/api/samples.json) names them in this
// order, and each has one recorded story file beside the sample people.
var sampleIDs = []string{
	"sample-date",
	"sample-family",
	"sample-interview",
	"sample-festival",
	"sample-funeral",
}

// SamplesConfig is the wiring MountSamples adds on top of a look mount.
// Nothing here dials.
type SamplesConfig struct {
	// SamplesDir is the samples data directory: the sample people and,
	// under cached/, the recorded story each open derives. Empty is
	// refused.
	SamplesDir string

	// Now is the clock for look rows. Nil means time.Now.
	Now func() time.Time

	// Log receives faults. Nil uses the server's logger.
	Log *slog.Logger
}

// sampleCheck is one recorded render check: the three facts, and the
// look answers the check read, in the vocabulary's own order.
type sampleCheck struct {
	ChangeMade   bool                       `json:"change_made"`
	SamePerson   bool                       `json:"same_person"`
	CriterionMet bool                       `json:"criterion_met"`
	Attributes   []domain.AnsweredAttribute `json:"attributes"`
}

// sampleAttempt is one recorded render-and-check pass. A shown attempt
// carries the recorded render image and the check that kept it; a
// discarded one names neither. A discard holds no check values on
// purpose: the sample set records no check of a discarded render for
// these stories, and a byte the check never answered cannot be written
// into the cache and read back as if it had.
type sampleAttempt struct {
	Fix     string       `json:"fix"`
	Attempt string       `json:"attempt"`
	Shown   bool         `json:"shown"`
	Image   string       `json:"image"`
	Check   *sampleCheck `json:"check"`

	image systemone.Image
}

// sampleAnalysis is one cached YouCam read the story stands on: the
// task, the one value the seam decodes for it, and the photo the
// provider actually read. That photo is never the sample's own unless
// the recording says so: a sample cannot measure, so a reading it carries
// is one a real call made of some picture, and the file names which.
type sampleAnalysis struct {
	Task      string `json:"task"`
	FaceShape string `json:"face_shape"`
	ReadFrom  string `json:"read_from"`
}

// sampleState is one sample's recorded story, loaded and validated at
// mount: the person, the judge's flags and biggest issue, the pick, the
// analysis rows the story's cache holds, and the render attempts. The
// image bytes are read at mount, so a recording that goes missing fails
// the mount instead of a judge's open.
type sampleState struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Occasion string `json:"occasion"`
	Variant  string `json:"variant"`
	Source   string `json:"source"`

	SelfiePath string `json:"selfie"`
	Judge      struct {
		Flags []string `json:"flags"`
		Issue string   `json:"issue"`
	} `json:"judge"`
	Pick struct {
		Fix      string `json:"fix"`
		RunnerUp string `json:"runner_up"`
	} `json:"pick"`
	Analysis []sampleAnalysis `json:"analysis"`
	Attempts []sampleAttempt  `json:"attempts"`

	occ    *domain.Occasion
	selfie systemone.Image
	// flags are the missed criteria, in the occasion's own order, the
	// order a real judge answers in.
	flags []domain.Criterion
}

// sampleRuntime is everything the sample routes read. One server holds
// one.
type sampleRuntime struct {
	rt   *lookRuntime
	log  *slog.Logger
	now  func() time.Time
	list []*sampleState
	byID map[string]*sampleState
}

var (
	samplesMu sync.Mutex
	samplesBy = map[*Server]*sampleRuntime{}
)

// MountSamples attaches the sample list and the sample start. Call it
// after MountLooks: an open starts its playback on the looks runner and
// answers through the landed look routes, so the look mount is a
// requirement, not an order of convenience. A second call refuses.
func (s *Server) MountSamples(ctx context.Context, cfg SamplesConfig) error {
	if s == nil || s.db == nil || s.mux == nil {
		return fmt.Errorf("server: samples: nil server")
	}
	if ctx == nil {
		return fmt.Errorf("server: samples: nil context")
	}
	if cfg.SamplesDir == "" {
		return fmt.Errorf("server: samples: no samples directory")
	}
	samplesMu.Lock()
	defer samplesMu.Unlock()
	if _, ok := samplesBy[s]; ok {
		return fmt.Errorf("server: samples: already mounted")
	}
	rt := s.looksOf()
	if rt == nil {
		return fmt.Errorf("server: samples: the look routes are not mounted")
	}
	log := cfg.Log
	if log == nil {
		log = s.log
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	list := make([]*sampleState, 0, len(sampleIDs))
	byID := make(map[string]*sampleState, len(sampleIDs))
	for _, sid := range sampleIDs {
		st, err := loadSample(cfg.SamplesDir, sid, rt)
		if err != nil {
			return fmt.Errorf("server: samples: %w", err)
		}
		list = append(list, st)
		byID[sid] = st
	}
	sm := &sampleRuntime{rt: rt, log: log, now: now, list: list, byID: byID}
	s.mux.Handle(routeSamples, http.HandlerFunc(sm.serveList))
	s.mux.Handle(routeSampleStart, http.HandlerFunc(sm.serveStart))
	samplesBy[s] = sm
	return nil
}

func (s *Server) samplesOf() *sampleRuntime {
	samplesMu.Lock()
	defer samplesMu.Unlock()
	return samplesBy[s]
}

// ---------------------------------------------------------------------------
// The recorded stories

// loadSample reads and validates one sample's recorded story. Every rule
// here is a coherence rule the pipeline's own shape gives for free: the
// flags are the occasion's criteria, the pick addresses what flagged,
// the attempts are the pick's two passes, the cache holds exactly what
// the picked fix measures, and the vocabulary offers every look answer.
// The rules that came in with the task are joined by the ones that keep
// the cache honest, and they read the same way: the story is the variant
// it names, a picture it holds belongs to the person it names, a reading
// it carries names the picture that was read, a shown render carries the
// check and only the look its fix declares changing, and a discarded one
// carries nothing at all.
func loadSample(dir, sid string, rt *lookRuntime) (*sampleState, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "cached", sid+".json"))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", sid, err)
	}
	var st sampleState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("%s: %w", sid, err)
	}
	if st.Kind != sampleCacheKind {
		return nil, fmt.Errorf("%s: kind %q, want %q", sid, st.Kind, sampleCacheKind)
	}
	if st.ID != sid {
		return nil, fmt.Errorf("%s: the file names %q", sid, st.ID)
	}
	// The provenance is part of the record, not a comment on it: a story
	// that names no source is a story whose numbers nobody can check.
	if strings.TrimSpace(st.Source) == "" {
		return nil, fmt.Errorf("%s: no source", sid)
	}
	occ, known := rt.occasion[st.Occasion]
	if !known {
		return nil, fmt.Errorf("%s: no occasion %q", sid, st.Occasion)
	}
	st.occ = occ
	if st.Name != occ.Name {
		return nil, fmt.Errorf("%s: name %q, want the occasion's own %q", sid, st.Name, occ.Name)
	}
	if st.SelfiePath == "" {
		return nil, fmt.Errorf("%s: no sample person", sid)
	}
	// The variant is the sample set's own id for the person, and the
	// recorded files carry it: the selfie must be that variant's picture
	// for this occasion, or the story is not the one its miss list is
	// about.
	if st.Variant == "" {
		return nil, fmt.Errorf("%s: no variant", sid)
	}
	if want := st.Variant + "-" + st.Occasion + ".jpg"; filepath.Base(st.SelfiePath) != want {
		return nil, fmt.Errorf("%s: the selfie is %q, want %s", sid, st.SelfiePath, want)
	}
	img, err := readSampleImage(dir, st.SelfiePath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", sid, err)
	}
	st.selfie = img

	// The flags: the occasion's own criteria, reordered into the
	// occasion's order, which is the order a real judge answers in.
	flagged := make(map[string]bool, len(st.Judge.Flags))
	for _, fid := range st.Judge.Flags {
		if criterionByID(occ, fid) == nil {
			return nil, fmt.Errorf("%s: flag %q is not a criterion of %s", sid, fid, occ.ID)
		}
		if flagged[fid] {
			return nil, fmt.Errorf("%s: flag %q twice", sid, fid)
		}
		flagged[fid] = true
	}
	for i := range occ.Criteria {
		if flagged[occ.Criteria[i].ID] {
			st.flags = append(st.flags, occ.Criteria[i])
		}
	}
	if len(st.flags) != len(st.Judge.Flags) {
		return nil, fmt.Errorf("%s: %d flags, want %d", sid, len(st.flags), len(st.Judge.Flags))
	}

	// The biggest issue: one of the occasion's own options.
	if !issueOffered(occ, st.Judge.Issue) {
		return nil, fmt.Errorf("%s: issue %q is not one of the occasion's options", sid, st.Judge.Issue)
	}

	// The pick: a fix on the menu when something flagged, nothing
	// otherwise, and the pick addresses a criterion that flagged - the
	// fix's why is the issue's suggestion. The runner-up is the model's
	// second choice and answers to the menu alone: the recorded story's
	// own runner-up is the festive top, a fix for a criterion that was
	// met.
	if (st.Pick.Fix == "") != (len(st.flags) == 0) {
		return nil, fmt.Errorf("%s: a pick needs a flag and a flag a pick", sid)
	}
	if st.Pick.Fix != "" {
		if fixByID(occ, st.Pick.Fix) == nil {
			return nil, fmt.Errorf("%s: pick %q is not a fix of %s", sid, st.Pick.Fix, occ.ID)
		}
		if !addresses(occ, st.Pick.Fix, flagged) {
			return nil, fmt.Errorf("%s: pick %q addresses nothing that flagged", sid, st.Pick.Fix)
		}
	}
	if st.Pick.RunnerUp != "" {
		if st.Pick.RunnerUp == st.Pick.Fix {
			return nil, fmt.Errorf("%s: the runner-up is the pick", sid)
		}
		if fixByID(occ, st.Pick.RunnerUp) == nil {
			return nil, fmt.Errorf("%s: runner-up %q is not a fix of %s", sid, st.Pick.RunnerUp, occ.ID)
		}
	}

	// The analysis cache: exactly what the picked fix measures, with a
	// value for each read. A fix that names no analysis names no row -
	// and a fix that names one gets every row, because a playback that
	// claimed cached:false would have to run the measurement to make it
	// true, and this path never spends.
	fix := fixByID(occ, st.Pick.Fix)
	named := map[string]bool{}
	if fix != nil {
		for _, a := range fix.Analysis {
			named[a.Task] = true
		}
	}
	seen := map[string]bool{}
	for _, a := range st.Analysis {
		if !named[a.Task] {
			return nil, fmt.Errorf("%s: analysis %s is not one the pick measures", sid, a.Task)
		}
		if seen[a.Task] {
			return nil, fmt.Errorf("%s: analysis %s twice", sid, a.Task)
		}
		seen[a.Task] = true
		if a.FaceShape == "" {
			return nil, fmt.Errorf("%s: analysis %s holds no reading", sid, a.Task)
		}
		// Whose face was read. The row is a provider's answer, so the
		// picture it answers for is named; a reading of someone else's
		// face dressed as this sample's is the one invention this file
		// cannot allow. The sample set keeps one person per directory,
		// so the same directory is the same person.
		if a.ReadFrom == "" {
			return nil, fmt.Errorf("%s: analysis %s names no picture it read", sid, a.Task)
		}
		if _, err := readSampleImage(dir, a.ReadFrom); err != nil {
			return nil, fmt.Errorf("%s: analysis %s: %w", sid, a.Task, err)
		}
		if filepath.Dir(a.ReadFrom) != filepath.Dir(st.SelfiePath) {
			return nil, fmt.Errorf("%s: analysis %s read %s, another person's face",
				sid, a.Task, a.ReadFrom)
		}
	}
	if len(seen) != len(named) {
		return nil, fmt.Errorf("%s: the cache holds %d of the pick's %d analyses", sid, len(seen), len(named))
	}

	// The attempts: the pick's first pass, then the runner-up's when
	// there is one. A shown attempt carries its recorded render and a
	// look the vocabulary offers; a discarded one names no image.
	want := []struct{ fix, name string }{{st.Pick.Fix, "fix"}, {st.Pick.RunnerUp, "runner_up"}}
	if len(st.Attempts) > 0 && st.Pick.Fix == "" {
		return nil, fmt.Errorf("%s: attempts without a pick", sid)
	}
	if len(st.Attempts) > 2 {
		return nil, fmt.Errorf("%s: %d attempts, want at most two", sid, len(st.Attempts))
	}
	for i := range st.Attempts {
		at := &st.Attempts[i]
		if i >= len(want) || at.Fix != want[i].fix || at.Attempt != want[i].name {
			return nil, fmt.Errorf("%s: attempt %d is %s/%s, want %s/%s",
				sid, i, at.Fix, at.Attempt, want[i].fix, want[i].name)
		}
		if at.Shown == (at.Image == "") {
			return nil, fmt.Errorf("%s: attempt %d: a shown render carries its image and a discarded one names none", sid, i)
		}
		if !at.Shown {
			// A discard keeps nothing: no media id, no words and no
			// check. The contract's discarded frame is {"fix","shown":
			// false} and the look body carries no render at all, so a
			// discard with check values would be recording an answer no
			// check ever gave.
			if at.Check != nil {
				return nil, fmt.Errorf("%s: attempt %d: a discarded render names no check", sid, i)
			}
			continue
		}
		img, err := readSampleImage(dir, at.Image)
		if err != nil {
			return nil, fmt.Errorf("%s: attempt %d: %w", sid, i, err)
		}
		at.image = img
		if at.Check == nil {
			return nil, fmt.Errorf("%s: attempt %d: a shown render carries its check", sid, i)
		}
		// The pipeline's own discard rule, read back as a loading rule:
		// a render is shown when the change was made and it is the same
		// person. A shown attempt whose check says otherwise is a story
		// that contradicts the stage that showed it.
		if !at.Check.ChangeMade || !at.Check.SamePerson {
			return nil, fmt.Errorf("%s: attempt %d: a shown render passed the change and the person", sid, i)
		}
		if len(at.Check.Attributes) == 0 {
			return nil, fmt.Errorf("%s: attempt %d: a shown render names no look", sid, i)
		}
		// The look the check answers is the look the fix changes: the
		// same attributes the occasion's own data declares, the values
		// the recording read. The values may differ from the declared
		// ones - the evaluation's known hair/tied-back tension is one
		// such difference - but no other attribute may appear, because
		// no other attribute is what this render was made to change.
		wanted := map[string]bool{}
		for _, change := range st.fixOf().Vocabulary {
			wanted[change.Attribute] = true
		}
		got := map[string]bool{}
		for _, attr := range at.Check.Attributes {
			if err := vocabularyOffers(&rt.loaded.Vocabulary, attr.ID, attr.Value); err != nil {
				return nil, fmt.Errorf("%s: attempt %d: %w", sid, i, err)
			}
			if !wanted[attr.ID] {
				return nil, fmt.Errorf("%s: attempt %d: the look answers %q, which fix %s does not change",
					sid, i, attr.ID, at.Fix)
			}
			if got[attr.ID] {
				return nil, fmt.Errorf("%s: attempt %d: the look answers %q twice", sid, i, attr.ID)
			}
			got[attr.ID] = true
		}
		if len(got) != len(wanted) {
			return nil, fmt.Errorf("%s: attempt %d: the look answers %d of fix %s's %d attributes",
				sid, i, len(got), at.Fix, len(wanted))
		}
	}
	return &st, nil
}

// fixOf is the fix the story's look ended on, which is what its shown
// render and its check belong to: the shown attempt's own fix when the
// story shows one, the picked fix otherwise.
func (st *sampleState) fixOf() *domain.Fix {
	for i := range st.Attempts {
		if st.Attempts[i].Shown {
			return fixByID(st.occ, st.Attempts[i].Fix)
		}
	}
	return fixByID(st.occ, st.Pick.Fix)
}

// readSampleImage reads one recorded image, paths relative to the
// samples directory. The recordings are data, not user input; the paths
// name the sample people and the recorded results.
func readSampleImage(dir, rel string) (systemone.Image, error) {
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return systemone.Image{}, err
	}
	if len(raw) == 0 {
		return systemone.Image{}, fmt.Errorf("%s is empty", rel)
	}
	return systemone.Image{ContentType: "image/jpeg", Bytes: raw}, nil
}

func issueOffered(occ *domain.Occasion, id string) bool {
	for _, o := range occ.BiggestIssue.Options {
		if o.ID == id {
			return true
		}
	}
	return false
}

func addresses(occ *domain.Occasion, fixID string, flagged map[string]bool) bool {
	fix := fixByID(occ, fixID)
	if fix == nil {
		return false
	}
	for _, cid := range fix.Criteria {
		if flagged[cid] {
			return true
		}
	}
	return false
}

// vocabularyOffers refuses a look answer the vocabulary does not offer,
// the check's own rule carried onto the loading side.
func vocabularyOffers(vocab *domain.Vocabulary, attrID, value string) error {
	for _, attr := range vocab.Attributes {
		if attr.ID != attrID {
			continue
		}
		for _, option := range attr.Options {
			if option == value {
				return nil
			}
		}
		return fmt.Errorf("look answer %q is not one of %s's options", value, attrID)
	}
	return fmt.Errorf("look answer names %q, which the vocabulary does not ask", attrID)
}

// ---------------------------------------------------------------------------
// The routes

// sampleItem is one sample on the list, the recorded example's shape.
type sampleItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Occasion string `json:"occasion"`
}

type sampleListBody struct {
	Samples []sampleItem `json:"samples"`
}

// serveList answers the samples a judge can open. No image bytes ride
// the list; the person is the open's business.
func (sm *sampleRuntime) serveList(w http.ResponseWriter, r *http.Request) {
	out := make([]sampleItem, 0, len(sm.list))
	for _, st := range sm.list {
		out = append(out, sampleItem{ID: st.ID, Name: st.Name, Occasion: st.Occasion})
	}
	if err := writeJSON(w, http.StatusOK, sampleListBody{Samples: out}); err != nil {
		sm.log.Warn("server: samples: list", "err", err.Error())
	}
}

// serveStart opens one sample: the recorded state derived under this
// judge's user id, the playback job started, 202 with the one id the
// look and its job share. The route takes no gate token, no lease seat
// and no budget reservation: a sample costs nothing, and the judge's
// allowances are waiting for their own selfie.
func (sm *sampleRuntime) serveStart(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r)
	if !ok {
		return
	}
	st, known := sm.byID[r.PathValue("id")]
	if !known {
		writeNotFound(w)
		return
	}
	ctx := r.Context()

	// The sample's selfie, registered under this judge: the bytes the
	// story recorded, the owner the reads require.
	selfieID, err := sm.keep(ctx, user.ID, groupSelfie, st.selfie)
	if err != nil {
		sm.log.Error("server: samples: the selfie", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	digest := sha256.Sum256(st.selfie.Bytes)
	hash := hex.EncodeToString(digest[:])

	// The render the story showed, already a blob in the group one
	// user's renders of one photo join - the unit the render eviction
	// and the deletions read.
	renders := make([]string, len(st.Attempts))
	for i := range st.Attempts {
		if !st.Attempts[i].Shown {
			continue
		}
		mediaID, err := sm.keep(ctx, user.ID, renderGroup(user.ID, hash), st.Attempts[i].image)
		if err != nil {
			sm.log.Error("server: samples: the render", "err", err.Error())
			writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
			return
		}
		renders[i] = mediaID
	}

	// The analysis the story stands on, into the cache under this
	// judge and this photo: the reads the pick carries and the measure
	// stage would otherwise run.
	for _, a := range st.Analysis {
		if err := sm.rt.Keep(ctx, user.ID, hash, a.Task, domain.Allowlist{FaceShape: a.FaceShape}); err != nil {
			sm.log.Error("server: samples: the analysis cache", "err", err.Error())
			writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
			return
		}
	}

	// The look's job: the recorded stages, played back on the looks
	// runner under the look kind, so the job routes dispatch it to the
	// look runtime like any other look.
	jobID, err := sm.rt.jobs.StartKind(ctx, pipeline.KindName, func(ctx context.Context, progress func(job.Progress)) ([]byte, error) {
		return sm.playback(progress, st, renders)
	})
	if err != nil {
		sm.log.Error("server: samples: start", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}

	// The look row, the id the answer names. It lands after the job
	// minted the id, the order pipeline.Start keeps, and a failure here
	// cancels the job and hands back nothing: no row, no readable look,
	// and the playback stops before its first frame matters.
	if _, err := sm.rt.srv.db.Writer().ExecContext(ctx,
		`INSERT OR IGNORE INTO looks (id, user_id, selfie_id, photo_hash, occasion, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		jobID, user.ID, selfieID, hash, st.Occasion, sm.now().Unix()); err != nil {
		sm.log.Error("server: samples: keep the look", "job_id", jobID, "err", err.Error())
		sm.cancel(jobID)
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if err := writeJSON(w, http.StatusAccepted, lookStartedBody{LookID: jobID, JobID: jobID}); err != nil {
		sm.log.Warn("server: samples: start", "err", err.Error())
	}
}

// cancel stops a look whose row never landed, the way a failed start
// does. A playback this small has usually finished already; Cancel
// answers ErrUnknownJob for it and that is no fault.
func (sm *sampleRuntime) cancel(jobID string) {
	if err := sm.rt.jobs.Cancel(jobID); err != nil && !errors.Is(err, job.ErrUnknownJob) {
		sm.log.Error("server: samples: cancel the unrecorded look", "job_id", jobID, "err", err.Error())
	}
}

// keep writes one recorded image into the judge's own mediastore, the
// same store the intake and the runner write, private to its owner.
func (sm *sampleRuntime) keep(ctx context.Context, owner, group string, img systemone.Image) (string, error) {
	mediaID, err := id.New()
	if err != nil {
		return "", fmt.Errorf("samples: mint the media id: %w", err)
	}
	if err := sm.rt.media.PersistWithID(ctx, mediaID, bytes.NewReader(img.Bytes), mediastore.Put{
		ContentType: img.ContentType,
		Owner:       owner,
		Group:       group,
		Visibility:  mediastore.Private,
	}); err != nil {
		return "", fmt.Errorf("samples: keep %s: %w", mediaID, err)
	}
	return mediaID, nil
}

// renderGroup is the mediastore group one user's renders of one photo
// join, the pipeline's own unit. The group is not exported there; the
// shape is its contract, and the deletions read the group.
func renderGroup(user, photoHash string) string {
	return "render/" + user + "/" + photoHash
}

// ---------------------------------------------------------------------------
// The playback

// The stage frames a look publishes, in the pipeline's own JSON shapes.
// The pipeline's payloads are its own; these are the contract's shapes
// for the same members, and a playback may only differ in values, never
// in shape.
type sampleJudgeFrame struct {
	Flags []sampleFlag `json:"flags"`
}

type sampleFlag struct {
	ID         string `json:"id"`
	Suggestion string `json:"suggestion"`
}

type samplePickFrame struct {
	Fix      string `json:"fix"`
	RunnerUp string `json:"runner_up"`
}

type sampleMeasureFrame struct {
	Skipped *bool `json:"skipped,omitempty"`
	Cached  *bool `json:"cached,omitempty"`
}

type sampleRenderFrame struct {
	Fix     string `json:"fix"`
	Attempt string `json:"attempt"`
}

type sampleCheckFrame struct {
	Fix     string `json:"fix"`
	Shown   bool   `json:"shown"`
	MediaID string `json:"media_id,omitempty"`
	Words   string `json:"words,omitempty"`
}

type sampleShopFrame struct {
	Products []domain.Product `json:"products"`
}

// playback is one sample's recorded run: the stages in the contract's
// order and the finished look as the job's data. It reads no client and
// no store, so nothing here can spend, and a test can pin it whole.
func (sm *sampleRuntime) playback(progress func(job.Progress), st *sampleState, renders []string) ([]byte, error) {
	occ := st.occ

	// Judge. The flags are the occasion's own wordings, in its order.
	flags := make([]sampleFlag, 0, len(st.flags))
	for _, c := range st.flags {
		flags = append(flags, sampleFlag{ID: c.ID, Suggestion: c.Suggestion})
	}
	emitSample(progress, "judge", sampleJudgeFrame{Flags: flags})

	// Pick. A sample carries no memory: the judge's history is about
	// their own looks, and the story's pick is the recorded one.
	emitSample(progress, "pick", samplePickFrame{Fix: st.Pick.Fix, RunnerUp: st.Pick.RunnerUp})

	// Measure. The cache rows were derived before the job started, so
	// the answer is the read's own: skipped when the fix measures
	// nothing, cached when it does - never false, because a false here
	// would be a promise to measure, and measuring spends.
	if st.Pick.Fix != "" {
		var frame sampleMeasureFrame
		if len(st.Analysis) == 0 {
			skipped := true
			frame.Skipped = &skipped
		} else {
			cached := true
			frame.Cached = &cached
		}
		emitSample(progress, "measure", frame)
	}

	// Render and check, the pick's pass and, when the first was
	// discarded, the runner-up's. A discarded render names no media id,
	// so the phone cannot show it.
	shown := -1
	for i := range st.Attempts {
		at := &st.Attempts[i]
		emitSample(progress, "render", sampleRenderFrame{Fix: at.Fix, Attempt: at.Attempt})
		frame := sampleCheckFrame{Fix: at.Fix, Shown: at.Shown}
		if at.Shown {
			frame.MediaID = renders[i]
			frame.Words = wordsOf(&sm.rt.loaded.Vocabulary, at.Check.Attributes)
			shown = i
		}
		emitSample(progress, "check", frame)
	}

	// Shop. The list is Shop's own, computed by the pipeline's function
	// from the occasion, the fix the look ended on and the catalog -
	// the same function a paid look ends on. The look ends on the shown
	// fix when one is shown, else the picked fix; nothing was picked,
	// the list is empty.
	shopFix := st.Pick.Fix
	if shown >= 0 {
		shopFix = st.Attempts[shown].Fix
	}
	var shopFor *domain.Fix
	if shopFix != "" {
		shopFor = fixByID(occ, shopFix)
	}
	products, err := pipeline.Shop(occ, shopFor, nil, sm.rt.loaded.Products, nil)
	if err != nil {
		return nil, fmt.Errorf("samples: shop %s: %w", st.ID, err)
	}
	emitSample(progress, "shop", sampleShopFrame{Products: products})

	look := pipeline.Look{
		Flags:      st.flags,
		Issue:      issueOf(occ, st.Judge.Issue),
		FixID:      st.Pick.Fix,
		RunnerUpID: st.Pick.RunnerUp,
		Products:   products,
	}
	if shown >= 0 {
		at := &st.Attempts[shown]
		check := at.Check
		look.Shown = &domain.Render{
			MediaID: renders[shown],
			FixID:   at.Fix,
			Check: &domain.Check{
				ChangeMade:   check.ChangeMade,
				SamePerson:   check.SamePerson,
				CriterionMet: check.CriterionMet,
				Attributes:   check.Attributes,
			},
		}
		look.Words = wordsOf(&sm.rt.loaded.Vocabulary, at.Check.Attributes)
	} else {
		look.SuggestionOnly = true
	}
	return json.Marshal(look)
}

// emitSample publishes one stage frame, the pipeline's emit with this
// file's payloads.
func emitSample(progress func(job.Progress), stage string, detail any) {
	if progress == nil {
		return
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return
	}
	progress(job.Progress{Stage: stage, Detail: raw})
}

// issueOf resolves the biggest issue's label from the occasion's own
// options, the way the judge reads it back.
func issueOf(occ *domain.Occasion, id string) pipeline.Issue {
	for _, o := range occ.BiggestIssue.Options {
		if o.ID == id {
			return pipeline.Issue{ID: o.ID, Label: o.Label}
		}
	}
	return pipeline.Issue{ID: id}
}

// wordsOf joins the look answers into the line the check composes: the
// vocabulary's own order, the offered values only, none and can't tell
// left out - the rule that makes the same look always say the same
// words. The loader has already refused an answer the vocabulary does
// not offer.
func wordsOf(vocab *domain.Vocabulary, attrs []domain.AnsweredAttribute) string {
	answered := make(map[string]string, len(attrs))
	for _, a := range attrs {
		answered[a.ID] = a.Value
	}
	words := make([]string, 0, len(attrs))
	for _, attr := range vocab.Attributes {
		value, ok := answered[attr.ID]
		if !ok || value == "none" || value == "can't tell" {
			continue
		}
		words = append(words, value)
	}
	return strings.Join(words, ", ")
}
