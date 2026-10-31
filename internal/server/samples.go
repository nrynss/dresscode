// Samples is this file: the five sample looks a judge opens for free.
// GET /api/samples lists them; POST /api/samples/{id}/looks opens one.
// An open spends nothing: the sample's selfie is registered into the
// judge's own mediastore - a repeat open reuses the picture that judge
// already holds rather than writing the same bytes again - the render its
// story showed is already a mediastore blob, and the look is the story its
// stages recorded - the flags, the pick, the measure answer, the render the
// check kept, the shop list - so the judge's stream, verdict, renders and
// shop answer exactly as the recorded fixtures do, with no model ask, no
// YouCam call and no unit settled. Nothing here dials.
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
// - the id every landed look route scopes its reads by - so the
// user-scoped reads and account deletion treat an opened sample like any
// other look. One thing that is NOT true of a single look's deletion, and
// it is said here rather than left to a comment nobody reads: an open
// registers no renders row (only the paid path writes one), and the
// privacy engine builds a look deletion's blob list from renders alone
// (internal/privacy/jobs.go, inspectLook), so deleting one sample look
// removes its looks row and zero bytes. Its pictures go with the selfie,
// through photo deletion by selfie_id, or with the account, which sweeps
// the owner's media. Writing the renders row here would make the old
// sentence true and was tried on paper before being refused: the reuse
// above hands one judge's two opens of one story the SAME render blob,
// and look deletion erases by id with no check for a second live look
// naming it, so erasing one would erase the other's picture. Reference
// counting that erasure is internal/privacy's to write, not this file's.
// Disclosed beside N8 in dev-diary/PHASE-4-server.md.
//
// The job is born finished. The contract freezes a sample's job read as
// "already done" (docs/api.md), and the shipped app reads a look exactly
// once: the render screen POSTs, navigates, asks for the look and never
// asks again - it has no stream follower of its own, only the verdict
// screen does - so a look that answered running would sit there pending
// forever with nothing left to poll. An open therefore does not start a
// playback on the looks runner at all. It plays the story into a buffer,
// writes the job row straight into the same job store the runner uses,
// terminal and carrying the finished look, and then publishes the
// buffered frames and the terminal on the job's topic itself. That
// mechanism was chosen over a wait because it makes both halves of the
// contract true at once: the read is done the instant the 202 is, with
// nothing to poll, and a subscriber that is listening still receives the
// story's frames. It also takes no runner lease - a sample open claims
// none of the look kind's slots and queues behind no paid look - and it
// leaves a restart nothing unfinished, so a playback can never be
// resumed into the paid pipeline. The frames are Keel's own progress and
// terminal payloads on the looks broker, so a subscriber that arrives
// after the story ends is answered with the retained terminal alone,
// exactly as it would be for a look that ran.
//
// The cache is evidence, not a wish list. Every value a story carries
// comes either from a recorded provider reply or from data the
// repository already owns - the sample set's ground truth (T0.4), the
// occasions and each fix's declared vocabulary (T0.3), the judge lines
// (T3.9) - and the file names which, in its own "source" member, because
// a number whose source nobody can point at is a number that was made
// up. loadSample turns that into rules it can check: the flags are the
// variant's miss list, the selfie is that variant's picture for its
// occasion, a cached reading names the picture the provider read and
// that picture must be the story's own selfie, since the analysis cache
// is keyed by photo hash, a shown render carries the check that showed
// it and names the recording of that check, and a discarded render
// carries no check at all - though it may still name the picture it
// made, as evidence of the pass, because a render that was made and then
// discarded is a recorded render no check ever showed. A picture is read
// from two roots only, the samples directory and the recorded YouCam
// results beside it; a story that names anything else is refused, not
// resolved. What the recordings do not tell, the story does not say:
// festival's gold chain is a genuine recorded try-on of its own selfie,
// it is named in that story, and it is not shown, because no render check
// of the festival occasion was ever recorded.

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
	jobsql "github.com/nrynss/keel/job/sqlitestore"
	"github.com/nrynss/keel/mediastore"
	"github.com/nrynss/keel/stream"
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
	// refused. The directory carries a shape as well as a content: a
	// story names a recorded provider result as
	// "../youcam/results/<name>.jpg", so the mount reads a second root,
	// the sibling <SamplesDir>/../youcam/results, and a samples
	// directory without that sibling refuses the mount - family's shown
	// render and festival's discarded pass both live there.
	SamplesDir string

	// Now is the clock for look rows. Nil means time.Now.
	Now func() time.Time

	// Log receives faults. Nil uses the server's logger.
	Log *slog.Logger
}

// sampleCheck is one recorded render check: the check recording the
// shown render stands on, the three facts it read, and the look answers
// the check gave, in the vocabulary's own order. The recording is named
// because three booleans with no recording behind them are three
// booleans nobody can check, and the facts are only facts because a
// model read a pair of pictures and gave them.
type sampleCheck struct {
	Recording    string                     `json:"recording"`
	ChangeMade   bool                       `json:"change_made"`
	SamePerson   bool                       `json:"same_person"`
	CriterionMet bool                       `json:"criterion_met"`
	Attributes   []domain.AnsweredAttribute `json:"attributes"`
}

// sampleAttempt is one recorded render-and-check pass. A shown attempt
// carries the recorded render image and the check that kept it; a
// discarded one carries no check. A discard holds no check values on
// purpose: the sample set records no check of a discarded render for
// these stories, and a byte the check never answered cannot be written
// into the cache and read back as if it had.
//
// A discard may still name the picture it made. The picture is evidence
// of the pass, not a render the judge is shown: it rides no frame, is
// registered in no mediastore and appears on no screen, and the pipeline
// never lands it, because a render the check did not keep is discarded.
// Naming it is what lets a story say "this try-on is real and this check
// was never recorded for it" instead of having to pretend one of the two.
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
// provider actually read. The read must be of this story's own selfie:
// the analysis cache is keyed by photo hash, so a reading of any other
// picture is not a cache hit here, however close the person, and a
// sample cannot measure to make it one. A story whose fix measures a task
// the sample set never read of this selfie therefore names no row at all,
// and says so in its source.
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
	rt *lookRuntime
	// jobs is the job store, the same table the looks runner records
	// into. An open writes its playback's terminal row here directly -
	// not through the runner, which would take a lease and run the job -
	// and the landed look routes read the row back through the runner's
	// own store.
	jobs job.Store
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
// after MountLooks: an open answers through the landed look routes, and
// its story is played onto the looks broker, so the look mount is a
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
	// A second handle on the jobs table, the way the deletion mount opens
	// its own: one table, one schema, and each mount reads what it needs.
	// The look routes reach these rows through the runner's handle, whose
	// store is the same table.
	jobs, err := jobsql.Open(ctx, jobsql.Config{DB: s.db})
	if err != nil {
		return fmt.Errorf("server: samples: %w", err)
	}
	sm := &sampleRuntime{rt: rt, jobs: jobs, log: log, now: now, list: list, byID: byID}
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

	// The analysis cache: a row is one the story stands on only when the
	// provider read this picture. A fix that names no analysis names no
	// row, and a fix that names one the recordings never read of this
	// selfie holds no row either - the measure stage then says skipped,
	// because the playback holds no reading and ran no measurement. The
	// rows that are there must be the fix's own, once each, with a value.
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
		// The same person is not the same photo, and the cache is keyed
		// by photo hash: a reading of a sister picture is a miss for this
		// one, so it is not a row this story may hold.
		if a.ReadFrom != st.SelfiePath {
			return nil, fmt.Errorf("%s: analysis %s read %s, which is not this sample's own picture, so the cache holds no reading of it",
				sid, a.Task, a.ReadFrom)
		}
	}

	// The attempts: the pick's first pass, then the runner-up's when
	// there is one. A shown attempt carries its recorded render, the check
	// that showed it and the look that vocabulary offers; a discarded one
	// carries no check, and at most the picture it made.
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
		if at.Shown && at.Image == "" {
			return nil, fmt.Errorf("%s: attempt %d: a render the check showed carries its image", sid, i)
		}
		if !at.Shown {
			// A discard keeps nothing: no media id, no words and no
			// check. The contract's discarded frame is {"fix","shown":
			// false} and the look body carries no render at all, so a
			// discard with check values would be recording an answer no
			// check ever gave. Its picture may still be named, as the
			// evidence that the pass ran and what it made; it is read
			// here so a story cannot name a picture that is not there,
			// and it is kept nowhere else - no frame names it, no
			// mediastore holds it, no screen shows it.
			if at.Check != nil {
				return nil, fmt.Errorf("%s: attempt %d: a discarded render names no check", sid, i)
			}
			if at.Image != "" {
				if _, err := readSampleImage(dir, at.Image); err != nil {
					return nil, fmt.Errorf("%s: attempt %d: %w", sid, i, err)
				}
			}
			continue
		}
		img, err := readSampleImage(dir, at.Image)
		if err != nil {
			return nil, fmt.Errorf("%s: attempt %d: %w", sid, i, err)
		}
		at.image = img
		if at.Check == nil {
			return nil, fmt.Errorf("%s: attempt %d: a shown render carries the check that showed it", sid, i)
		}
		// The check behind a shown render is a recording, and the story
		// names which one: three booleans with no recording behind them
		// are three booleans nobody can check. The name is one file in the
		// recorded checks' own directory, and the honesty test reads it.
		if at.Check.Recording == "" || strings.ContainsAny(at.Check.Recording, `/\\`) {
			return nil, fmt.Errorf("%s: attempt %d: the check names no recording in the check directory", sid, i)
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

// sampleRoots are the two places a cached story may read a picture from,
// and the only two: the samples directory, which holds the sample people,
// and the recorded YouCam results kept beside it, which hold the renders
// the recorded flows saved. The cache is data on disk rather than user
// input, but a path in it is still a path, and one that walked out of both
// roots would have the server open anything its own user can read. The
// recorded results are a root because honest evidence lives there:
// family's jhumkas and festival's chain are YouCam results, named as such
// with the one "../youcam/results/" shape, and no other member of any
// story reaches outside the samples directory.
func sampleRoots(dir string) []string {
	root := filepath.Clean(dir)
	return []string{root, filepath.Clean(filepath.Join(root, "..", "youcam", "results"))}
}

// contained reports whether a cleaned path is one of the roots or lives
// under one, with the separator included so a sibling directory named
// "samples-old" is not read as inside "samples".
func contained(roots []string, path string) bool {
	for _, root := range roots {
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// readSampleImage reads one recorded image, its path relative to the
// samples directory, and refuses a path that leaves the sample roots.
// The join is cleaned before it is judged, so a "../" chain is contained
// by what it resolves to and not by how it is spelled; the file the
// server actually opens is resolved again, because a link can lead out of
// the roots with a path that stays inside them on paper.
func readSampleImage(dir, rel string) (systemone.Image, error) {
	if rel == "" {
		return systemone.Image{}, fmt.Errorf("no picture is named")
	}
	path := filepath.FromSlash(rel)
	if filepath.IsAbs(path) {
		return systemone.Image{}, fmt.Errorf("%q is an absolute path, outside the sample roots", rel)
	}
	roots := sampleRoots(dir)
	full := filepath.Join(roots[0], path)
	if !contained(roots, full) {
		return systemone.Image{}, fmt.Errorf("%q is outside the sample roots", rel)
	}
	target, err := filepath.EvalSymlinks(full)
	if err != nil {
		return systemone.Image{}, missingSamplePicture(roots, rel, err)
	}
	if !contained(resolvedRoots(roots), target) {
		return systemone.Image{}, fmt.Errorf("%q leads out of the sample roots", rel)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		return systemone.Image{}, missingSamplePicture(roots, rel, err)
	}
	if len(raw) == 0 {
		return systemone.Image{}, fmt.Errorf("%s is empty", rel)
	}
	return systemone.Image{ContentType: "image/jpeg", Bytes: raw}, nil
}

// missingSamplePicture is a story's picture a read refused, said with the
// two roots a story may read from. It names them because the commonest
// reason a picture is not there is the mount's layout, not the story: a
// directory handed to SamplesConfig without the recorded results beside it
// fails here, over family's shown render, and a bare "no such file or
// directory" for one jpeg path does not tell the caller that the sibling
// root was missing. The layout rule belongs in the fault that proves it.
func missingSamplePicture(roots []string, rel string, err error) error {
	return fmt.Errorf("%q is not a picture this story can read: a story reads from two roots only, the sample people under %s and the recorded provider results under %s: %w",
		rel, roots[0], roots[1], err)
}

// resolvedRoots is each root with the links it is made of followed, so a
// target under a linked samples directory is still read as inside it. A
// root that cannot be resolved is judged as it is spelled.
func resolvedRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		real, err := filepath.EvalSymlinks(root)
		if err != nil {
			out = append(out, root)
			continue
		}
		out = append(out, real)
	}
	return out
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
// judge's user id, the story landed as a job that is finished as it is
// written, 202 with the one id the look and its job share. The route takes
// no gate token, no lease seat and no budget reservation: a sample costs
// nothing, and the judge's allowances are waiting for their own selfie.
// Nor does it take the look kind's slot or queue behind a paid look: the
// job is written, not run.
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
	// story recorded, the owner the reads require. Only the picture a
	// discarded pass left behind is not registered: it is evidence, not a
	// render, and no frame and no screen names it.
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
	// stage would otherwise run. A story holds a row only when the
	// recording read this very picture, which is why the measure stage can
	// say cached for the rows it has and skipped for the reads it has not.
	for _, a := range st.Analysis {
		if err := sm.rt.Keep(ctx, user.ID, hash, a.Task, domain.Allowlist{FaceShape: a.FaceShape}); err != nil {
			sm.log.Error("server: samples: the analysis cache", "err", err.Error())
			writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
			return
		}
	}

	// The look's job: the recorded story, played into a buffer and landed
	// as a terminal row of the look kind, so the job read behind the 202 is
	// already done and the stream still carries the stages.
	jobID, err := id.New()
	if err != nil {
		sm.log.Error("server: samples: the job id", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	data, frames, err := sm.story(st, renders)
	if err != nil {
		sm.log.Error("server: samples: the story", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if err := sm.land(ctx, jobID, data, frames); err != nil {
		sm.log.Error("server: samples: land the story", "job_id", jobID, "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}

	// The look row, the id the answer names. It lands after the job row,
	// the order pipeline.Start keeps. A failure here leaves a finished job
	// row that names no look: every look route scopes its reads by the
	// looks table and the job dispatcher answers only what it can see, so
	// the orphan row is unreadable, nothing was spent, and the answer hands
	// back nothing.
	if _, err := sm.rt.srv.db.Writer().ExecContext(ctx,
		`INSERT OR IGNORE INTO looks (id, user_id, selfie_id, photo_hash, occasion, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		jobID, user.ID, selfieID, hash, st.Occasion, sm.now().Unix()); err != nil {
		sm.log.Error("server: samples: keep the look", "job_id", jobID, "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if err := writeJSON(w, http.StatusAccepted, lookStartedBody{LookID: jobID, JobID: jobID}); err != nil {
		sm.log.Warn("server: samples: start", "err", err.Error())
	}
}

// story is the recorded playback into a buffer instead of onto a live
// topic: the stage frames a subscriber would be played, in order, and the
// finished look the job's data carries. It is the same function a runner
// would have been handed; nothing in it reads a client or a store, so
// nothing in it can spend.
func (sm *sampleRuntime) story(st *sampleState, renders []string) ([]byte, []job.Progress, error) {
	frames := make([]job.Progress, 0, 8)
	data, err := sm.playback(func(p job.Progress) { frames = append(frames, p) }, st, renders)
	if err != nil {
		return nil, nil, err
	}
	return data, frames, nil
}

// land writes one story as the job it finished being, then plays its
// frames onto the job's topic and closes the topic with the terminal.
//
// The record is the shape Keel's runner leaves behind for a look that
// finished on its own: the look kind, so the job dispatcher hands its reads
// to the look runtime like any other look; attempt one, its own root, so
// the look routes' attempt walk finds one finished attempt and no
// replacement; the last stage as the progress snapshot, so the job read
// names the stage the story reached; the look as the data, so the verdict
// is ready the moment the row is; and no error. The row is written before
// the frames are published, so a read never sees a story mid-flight, and
// the row is never anything but terminal, so a restart's recovery finds
// nothing to interrupt and the resume hook is never offered a playback.
func (sm *sampleRuntime) land(ctx context.Context, jobID string, data []byte, frames []job.Progress) error {
	rec := job.Record{
		ID:        jobID,
		Kind:      pipeline.KindName,
		Status:    job.StatusDone,
		Attempt:   1,
		RootID:    jobID,
		Data:      data,
		UpdatedAt: sm.now(),
	}
	if len(frames) > 0 {
		rec.Progress = frames[len(frames)-1]
	}
	if err := sm.jobs.Create(ctx, rec); err != nil {
		return fmt.Errorf("samples: land the story: %w", err)
	}
	sm.publish(jobID, frames)
	return nil
}

// publish plays one story onto its job topic, in the payloads Keel's
// runner publishes: one progress event per stage, then the done terminal
// the broker retains for the subscriber that arrives after the story ended.
func (sm *sampleRuntime) publish(jobID string, frames []job.Progress) {
	for _, p := range frames {
		sm.rt.broker.Publish(job.Topic(jobID), stream.Event{
			Name: wire.EventProgress,
			Data: wire.ProgressEvent{
				JobID:   jobID,
				Stage:   p.Stage,
				Current: p.Current,
				Total:   p.Total,
				Detail:  p.Detail,
			},
		})
	}
	sm.rt.broker.Publish(job.Topic(jobID), stream.Event{
		Name:     string(job.StatusDone),
		Data:     wire.StatusEvent{JobID: jobID, Status: string(job.StatusDone)},
		Terminal: true,
	})
}

// keep writes one recorded image into the judge's own mediastore, the
// same store the intake and the runner write, private to its owner.
//
// A repeat open reuses the picture the owner already holds instead of
// writing it again. A story's bytes are fixed data, so a second open of
// the same sample asks for the same pictures, and a copy of a picture
// this owner already has is a second blob, a second row and a second
// write of the same bytes for one story: the five recorded stories are
// 1,649,539 bytes of pictures between them, and without the reuse every
// repeat open pays that again, with no gate token, no lease seat and no
// budget reservation on this path to bound it. A picture the search does
// not settle mints, which is what an open did before and is never wrong,
// only less frugal.
func (sm *sampleRuntime) keep(ctx context.Context, owner, group string, img systemone.Image) (string, error) {
	want := sha256.Sum256(img.Bytes)
	if mediaID, ok := sm.held(ctx, owner, group, int64(len(img.Bytes)), want); ok {
		return mediaID, nil
	}
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

// keepSearch is how many same-sized pictures of one owner and group a keep
// reads before it writes its own. A group holds the judge's own uploads
// beside a story's pictures, and the shortlist is a byte count rather than
// an identity, so reading a whole library to avoid one duplicate costs
// more than the duplicate. The newest rows come first, which is where a
// story's own earlier copy is - a reuse adds no row, so nothing newer
// lands in the group between two opens of one story - and the read stops
// at the search. Past it a keep writes.
const keepSearch = 8

// held is the media id this owner already holds for exactly these bytes in
// exactly this group, if the store holds one. The mediastore's index is a
// table of the same SQLite file the look routes read, and it carries an
// owner, a group and a byte count and no digest - the Store lists blobs to
// deletion through its own index handle, not through a lookup by content -
// so the rows give the shortlist and the store's own read gives the
// identity: a candidate comes back only once its bytes hash to the story's.
// Nothing is written here, and a search that finds nothing or fails is the
// landed write rather than an error.
func (sm *sampleRuntime) held(ctx context.Context, owner, group string, size int64, want [sha256.Size]byte) (string, bool) {
	rows, err := sm.rt.srv.db.Reader().QueryContext(ctx,
		`SELECT id FROM media WHERE owner = ? AND media_group = ? AND size_bytes = ?
		 ORDER BY created_at DESC, id DESC LIMIT ?`,
		owner, group, size, keepSearch)
	if err != nil {
		sm.log.Warn("server: samples: the shortlist", "err", err.Error())
		return "", false
	}
	defer rows.Close()
	for rows.Next() {
		var mediaID string
		if err := rows.Scan(&mediaID); err != nil {
			sm.log.Warn("server: samples: the shortlist row", "err", err.Error())
			return "", false
		}
		if mediaID == "" {
			continue
		}
		rc, _, err := sm.rt.media.Open(ctx, mediaID)
		if err != nil {
			continue
		}
		h := sha256.New()
		copied, copyErr := io.Copy(h, io.LimitReader(rc, size))
		rc.Close()
		if copyErr != nil || copied != size {
			continue
		}
		if bytes.Equal(h.Sum(nil), want[:]) {
			return mediaID, true
		}
	}
	if err := rows.Err(); err != nil {
		sm.log.Warn("server: samples: the shortlist", "err", err.Error())
	}
	return "", false
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

	// Measure. The cache rows were derived before the job was written, so
	// the frame answers with the read the playback actually holds: cached
	// when it holds one, skipped when it holds none - never false, because
	// a false here would be a promise to measure, and measuring spends.
	// The contract ties skipped to a fix that names no analysis; one story
	// deviates from that wording and says so in its own source, because its
	// fix names an analysis no recording ever read of its picture. Saying
	// cached would claim a read that never happened, and saying false would
	// spend to make it true, so the frame says what the playback does: it
	// measured nothing.
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
	// discarded, the runner-up's. A discarded pass streams the contract's
	// discard frame - {"fix","shown":false} and nothing else: no media id
	// the phone could show, no words the check never said - even when the
	// story names the picture that pass made, because naming a picture is
	// not showing one.
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
