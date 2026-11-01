package server

// The look routes replay the recorded family-function story: the V04
// selfie through the recorded earring render, a Clef stub answering the
// three model calls the way the pipeline's own rig does, and a memory
// fake holding the story's facts. Every route's response is compared
// against its recorded example in testdata/api, member for member, with
// the values a replay cannot carry named where they differ:
//
//   - The ids the server mints for itself: the look, the job, the selfie,
//     the render and the catalog picture. The wiring makes the look id
//     and the job id one id (the look row is the look, the job is its
//     engine), so both fixture ids rename to it.
//   - The suggestion story's runner-up. The fixture names the festive
//     top, whose cloth task composites a product photo; one look may name
//     one catalog product, so the replay's runner-up is the red lip,
//     which paints and photographs nothing. The frames are pinned by
//     shape and the honest values.
//   - The heartbeat comment frame, which a replay cannot carry: the
//     frames are compared without comment lines. The error example
//     records the error frame alone, so the stages that ran before the
//     fault are pinned by name and the example is compared against the
//     terminal frame.
//
// Nothing dials and no unit moves: the YouCam client is the typed
// replay of testdata/youcam, Clef is the stub, and the budget is the
// rig's own.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/dresscode/internal/config"
	"github.com/nrynss/dresscode/internal/memory"
	"github.com/nrynss/dresscode/internal/pipeline"
	"github.com/nrynss/dresscode/internal/privacy"
	"github.com/nrynss/dresscode/internal/systemone"
	"github.com/nrynss/dresscode/internal/youcam"
	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/gate"
	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/job"
	jobsql "github.com/nrynss/keel/job/sqlitestore"
	"github.com/nrynss/keel/mediastore"
	"github.com/nrynss/keel/stream"
)

// ---------------------------------------------------------------------------
// The rig

// storyAnswers is one story the stub tells: the judge's readings, the
// biggest issue, the pick and its runner-up probabilities, the check
// gate readings in call order, and the look the check reads back.
type storyAnswers struct {
	judge     map[string]float64
	issue     string
	pick      string
	pickProbs map[string]float64
	checks    []checkAnswer3
	look      map[string]string
}

// noneLook is every vocabulary attribute the story does not name, set to
// the data's own "none": the check leaves those out of the attributes and
// out of the words.
func noneLook(named map[string]string) map[string]string {
	look := map[string]string{}
	for _, attr := range []string{
		"jewellery", "jewellery_colour", "necklace", "necklace_colour",
		"lip_colour", "collar", "hair", "hair_colour", "beard",
		"expression", "garment",
	} {
		look[attr] = "none"
	}
	for attr, value := range named {
		look[attr] = value
	}
	return look
}

// earringAnswers is the story the fixtures record: two flags, the
// jhumkas picked with the festive top behind it, the render shown.
func earringAnswers() storyAnswers {
	return storyAnswers{
		judge: map[string]float64{
			"family-earring-festive": 0.03,
			"family-lip-colour":      0.2,
		},
		issue:     "family-earring-festive",
		pick:      "family-gold-jhumkas",
		pickProbs: map[string]float64{"family-festive-top": 0.35},
		checks:    []checkAnswer3{{change: 0.99, same: 0.99, met: 0.99}},
		look:      noneLook(map[string]string{"jewellery": "jhumkas", "jewellery_colour": "gold"}),
	}
}

// suggestionAnswers is the same judge and pick with both checks
// discarded: the suggestion stream's story. The runner-up is the red
// lip, whose render photographs nothing, because the fixture's runner-up
// (the festive top) composites a product photo the one basket candidate
// cannot name for two fixes.
func suggestionAnswers() storyAnswers {
	a := earringAnswers()
	a.pickProbs = map[string]float64{"family-red-lip": 0.4}
	a.checks = []checkAnswer3{
		{change: 0.0001, same: 0.99, met: 0.99},
		{change: 0.0001, same: 0.99, met: 0.99},
	}
	a.look = noneLook(nil)
	return a
}

// smileAnswers is the template-only story: the expression flagged, the
// closed-mouth smile picked and shown, nothing to photograph. Its selfie
// is the recorded P12 sample, the one the smile recording uploads.
func smileAnswers() storyAnswers {
	return storyAnswers{
		judge:     map[string]float64{"family-expression-present": 0.03},
		issue:     "family-expression-present",
		pick:      "family-closed-smile",
		pickProbs: map[string]float64{},
		checks:    []checkAnswer3{{change: 0.99, same: 0.99, met: 0.99}},
		look:      noneLook(map[string]string{"expression": "closed-mouth-smile"}),
	}
}

// checkAnswer3 is one check call's three gate readings.
type checkAnswer3 struct{ change, same, met float64 }

type pickAnswer3 struct {
	choice string
	probs  map[string]float64
}

// clefStub answers the three Clef calls a look makes, in kind. Asks of a
// kind can be parked, so a test holds a look at a known stage. The park
// waits on the ask's own context, so an abandoned look ends instead of
// hanging the gate.
type clefStub struct {
	mu        sync.Mutex
	judge     map[string]float64
	issue     string
	picks     []pickAnswer3
	checks    []checkAnswer3
	look      map[string]string
	asked     map[string]int
	parkJudge chan struct{}
	parkPick  chan struct{}
	parkCheck chan struct{}
}

func newClefStub(a storyAnswers) *clefStub {
	return &clefStub{
		judge:  a.judge,
		issue:  a.issue,
		picks:  []pickAnswer3{{choice: a.pick, probs: a.pickProbs}},
		checks: append([]checkAnswer3(nil), a.checks...),
		look:   a.look,
		asked:  map[string]int{},
	}
}

func (s *clefStub) Ask(ctx context.Context, req systemone.Request) (systemone.Response, error) {
	s.mu.Lock()
	s.asked[askedKind(req)]++
	park := s.parkForLocked(req)
	s.mu.Unlock()
	if park != nil {
		select {
		case <-park:
		case <-ctx.Done():
			return systemone.Response{}, ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	answers := map[string]systemone.Answer{}
	noul := func(v float64) systemone.Answer {
		return systemone.Answer{Type: systemone.TypeNoul, Noul: &v}
	}
	switch {
	case hasQ(req, "biggest_issue"):
		for qid := range req.Questions {
			if qid == "biggest_issue" {
				continue
			}
			p, ok := s.judge[qid]
			if !ok {
				p = 0.9
			}
			answers[qid] = noul(p)
		}
		answers["biggest_issue"] = systemone.Answer{Type: systemone.TypeChoice, Choice: s.issue}
	case hasQ(req, "fix"):
		if len(s.picks) == 0 {
			return systemone.Response{}, errors.New("clefStub: the pick answers ran dry")
		}
		a := s.picks[0]
		s.picks = s.picks[1:]
		answers["fix"] = systemone.Answer{Type: systemone.TypeChoice, Choice: a.choice, Probabilities: a.probs}
	case hasQ(req, "change_made"):
		if len(s.checks) == 0 {
			return systemone.Response{}, errors.New("clefStub: the check answers ran dry")
		}
		a := s.checks[0]
		s.checks = s.checks[1:]
		answers["change_made"] = noul(a.change)
		answers["same_person"] = noul(a.same)
		answers["criterion_met"] = noul(a.met)
		for qid, v := range s.look {
			answers["look."+qid] = systemone.Answer{Type: systemone.TypeChoice, Choice: v}
		}
	default:
		return systemone.Response{}, errors.New("clefStub: a request this stub does not know")
	}
	return systemone.Response{Model: systemone.ModelClef, Answers: answers}, nil
}

func (s *clefStub) parkForLocked(req systemone.Request) chan struct{} {
	switch askedKind(req) {
	case "judge":
		return s.parkJudge
	case "pick":
		return s.parkPick
	case "check":
		return s.parkCheck
	}
	return nil
}

func askedKind(req systemone.Request) string {
	switch {
	case hasQ(req, "biggest_issue"):
		return "judge"
	case hasQ(req, "fix"):
		return "pick"
	case hasQ(req, "change_made"):
		return "check"
	}
	return "unknown"
}

func hasQ(req systemone.Request, qid string) bool {
	_, ok := req.Questions[qid]
	return ok
}

func (s *clefStub) count(kind string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asked[kind]
}

// countedYouCam counts the calls the replay serves, so a resume proves
// it created no second task, and can park the first poll: the moment a
// dead process leaves behind, and the moment a job state read catches a
// look mid-render.
type countedYouCam struct {
	youcam.YouCam
	mu      sync.Mutex
	creates int
	uploads int
	polls   int
	fetches int
	entered chan struct{}
	hold    chan struct{}
	once    sync.Once
}

func (c *countedYouCam) Upload(ctx context.Context, file youcam.File) (youcam.Slot, error) {
	c.mu.Lock()
	c.uploads++
	c.mu.Unlock()
	return c.YouCam.Upload(ctx, file)
}

func (c *countedYouCam) Create(ctx context.Context, task youcam.Task) (youcam.Created, error) {
	c.mu.Lock()
	c.creates++
	c.mu.Unlock()
	return c.YouCam.Create(ctx, task)
}

func (c *countedYouCam) Poll(ctx context.Context, ref youcam.PollRef) (youcam.Status, error) {
	c.mu.Lock()
	c.polls++
	first := c.polls == 1
	c.mu.Unlock()
	if first && c.hold != nil {
		c.once.Do(func() { close(c.entered) })
		select {
		case <-c.hold:
		case <-ctx.Done():
			return youcam.Status{}, ctx.Err()
		}
	}
	return c.YouCam.Poll(ctx, ref)
}

func (c *countedYouCam) Fetch(ctx context.Context, ref youcam.ResultRef) (youcam.Result, error) {
	c.mu.Lock()
	c.fetches++
	c.mu.Unlock()
	return c.YouCam.Fetch(ctx, ref)
}

func (c *countedYouCam) count() (creates, uploads, polls, fetches int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.creates, c.uploads, c.polls, c.fetches
}

// fakeMemory is the style-memory seam. It holds one occasion's hits, so
// the recall composes the story's sentence and another occasion is a
// first visit, and counts its reads and writes.
type fakeMemory struct {
	mu           sync.Mutex
	byOccasion   map[string][]memory.Hit
	recalls      int
	recallsFor   []string
	writes       int
	concepts     []memory.Concept
	failRecall   bool
	failRemember bool
}

func (f *fakeMemory) Remember(_ context.Context, _ string, concepts []memory.Concept) (memory.Remembered, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	if f.failRemember {
		return memory.Remembered{}, errors.New("fakeMemory: the write was not confirmed")
	}
	f.concepts = append(f.concepts, concepts...)
	return memory.Remembered{Receipt: "receipt", State: "applied"}, nil
}

func (f *fakeMemory) Recall(_ context.Context, _, occasion string) (memory.Recall, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recalls++
	f.recallsFor = append(f.recallsFor, occasion)
	if f.failRecall {
		return memory.Recall{}, errors.New("fakeMemory: the recall refused")
	}
	return memory.Recall{Context: occasion, Hits: f.byOccasion[occasion]}, nil
}

func (f *fakeMemory) Erase(_ context.Context, session string) (memory.EraseResult, error) {
	return memory.EraseResult{Session: session, AlreadyAbsent: true}, nil
}

func (f *fakeMemory) count() (recalls, writes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recalls, f.writes
}

func (f *fakeMemory) written() []memory.Concept {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]memory.Concept(nil), f.concepts...)
}

// lookEnv is one assembled look server.
type lookEnv struct {
	t       *testing.T
	srv     *Server
	handler http.Handler
	clef    *clefStub
	cam     *countedYouCam
	mem     *fakeMemory
	catalog map[string]string
	blobs   *ownedBlobs
	budget  *cost.Budget
	media   *mediastore.Store
	occDir  string
}

// testSettings carries the measured lines of T3.9 and the lease caps.
func testSettings() *config.Settings {
	return &config.Settings{
		Lease: config.Lease{Judge: config.JudgeLease{FreshSelfies: 5, Renders: 10}},
		Judge: config.Judge{
			Met: 0.628,
			PerCriterion: map[string]float64{
				"family-expression-present": 0.5466,
				"family-garment-festive":    0.2394,
				"interview-collar-buttoned": 0.11395,
				"interview-expression-calm": 0.36925,
			},
		},
		Check: config.Check{
			ChangeMade:   0.50085,
			SamePerson:   0.48355,
			CriterionMet: 0.50315,
			Model:        "clef",
		},
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// newLookEnv opens one server with the look routes mounted. The story
// answers decide what the stub says; the units decide what the budget
// admits. The spending gate is opened wide: these tests start looks to
// read routes, not to measure the gate, which server_test.go pins.
func newLookEnv(t *testing.T, answers storyAnswers, units cost.Price, storyHits []memory.Hit) *lookEnv {
	t.Helper()
	env, cfg := openLookEnv(t, answers, units, storyHits)
	if err := env.srv.MountLooks(t.Context(), cfg); err != nil {
		t.Fatalf("mount looks: %v", err)
	}
	if err := env.srv.MountLooks(t.Context(), cfg); err == nil {
		t.Fatal("a second mount succeeded")
	}
	return env
}

// openLookEnv builds the server and the look config without mounting, so
// a test can choose the order the mounts come in.
func openLookEnv(t *testing.T, answers storyAnswers, units cost.Price, storyHits []memory.Hit) (*lookEnv, LooksConfig) {
	t.Helper()
	dir := t.TempDir()
	app := filepath.Join(dir, "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "index.html"), []byte(appIndex), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := Open(t.Context(), Config{
		DBPath:        filepath.Join(dir, "dresscode.db"),
		AppDir:        app,
		SigningKey:    []byte("test-session-signing-key-32b!!!!"),
		Log:           slog.New(slog.DiscardHandler),
		LookPerClient: gate.Limit{Burst: 1000, Every: time.Hour},
		LookGlobal:    gate.Limit{Burst: 1000, Every: time.Hour},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	rep, err := youcam.Open(filepath.Join(repoRoot(t), "testdata", "youcam"))
	if err != nil {
		t.Fatalf("open the replay: %v", err)
	}
	cam := &countedYouCam{YouCam: rep}
	budget, err := cost.NewBudgetIn(youcam.UnitsDenomination, units)
	if err != nil {
		t.Fatal(err)
	}
	meter, err := cost.NewMeter(budget, cost.NewLedger())
	if err != nil {
		t.Fatal(err)
	}
	mem := &fakeMemory{byOccasion: map[string][]memory.Hit{}}
	if len(storyHits) > 0 {
		mem.byOccasion["family function"] = storyHits
	}
	env := &lookEnv{
		t:       t,
		srv:     srv,
		handler: srv.Handler(),
		clef:    newClefStub(answers),
		cam:     cam,
		mem:     mem,
		catalog: map[string]string{},
		blobs:   &ownedBlobs{owners: map[string]string{}},
		budget:  budget,
		media:   srv.intakeOf().media,
		occDir:  filepath.Join(repoRoot(t), "testdata", "occasions"),
	}
	cfg := LooksConfig{
		OccasionsDir: env.occDir,
		Settings:     testSettings(),
		Clef:         env.clef,
		YouCam:       cam,
		Meter:        meter,
		UnitsLeft:    budget.Remaining,
		Memory:       mem,
		CatalogMedia: func(_ context.Context, productID string) (string, bool) {
			seeded, ok := env.catalog[productID]
			return seeded, ok
		},
	}
	return env, cfg
}

// mountDeletion attaches the deletion routes beside the looks, with the
// fakes the privacy engine needs. Nothing dials.
func (e *lookEnv) mountDeletion(t *testing.T) {
	t.Helper()
	if err := e.srv.MountDeletion(t.Context(), DeletionConfig{
		SigningKey: []byte("test-session-signing-key-32b!!!!"),
		CodeKey:    []byte("test-deletion-code-key-32b!!!!!!"),
		Blobs:      e.blobs,
		Memory:     e.mem,
	}); err != nil {
		t.Fatalf("mount deletion: %v", err)
	}
	e.handler = e.srv.Handler()
}

// ownedBlobs is the mediastore seam the erase reads: the blobs a test
// names, by owner. Nothing dials, and the erase deletes from this map.
type ownedBlobs struct {
	mu     sync.Mutex
	owners map[string]string
}

func (b *ownedBlobs) add(id, owner string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.owners[id] = owner
}

func (b *ownedBlobs) Blobs(context.Context) ([]mediastore.Blob, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]mediastore.Blob, 0, len(b.owners))
	for id, owner := range b.owners {
		out = append(out, mediastore.Blob{ID: id, Owner: owner})
	}
	return out, nil
}

func (b *ownedBlobs) Delete(_ context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.owners, id)
	return nil
}

// gateRender parks the first poll until the returned release is called.
// The entered channel closes when the look reaches that poll.
func (e *lookEnv) gateRender() (entered chan struct{}, release func()) {
	e.cam.entered = make(chan struct{})
	e.cam.hold = make(chan struct{})
	hold := e.cam.hold
	var once sync.Once
	release = func() { once.Do(func() { close(hold) }) }
	e.t.Cleanup(release)
	return e.cam.entered, release
}

// seed puts one picture into the intake's mediastore under an id.
func (e *lookEnv) seed(t *testing.T, owner, group string, img systemone.Image) string {
	t.Helper()
	mediaID, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.media.PersistWithID(t.Context(), mediaID, strings.NewReader(string(img.Bytes)), mediastore.Put{
		ContentType: img.ContentType,
		Owner:       owner,
		Group:       group,
		Visibility:  mediastore.Private,
	}); err != nil {
		t.Fatalf("seed %s: %v", mediaID, err)
	}
	return mediaID
}

// recordedImage reads one recorded upload's own file, the bytes the
// typed replay matches that upload by.
func recordedImage(t *testing.T, key string) systemone.Image {
	t.Helper()
	rep, err := youcam.Open(filepath.Join(repoRoot(t), "testdata", "youcam"))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := rep.Reply(key)
	if err != nil {
		t.Fatal(err)
	}
	var file youcam.File
	if err := json.Unmarshal(rec.Request, &file); err != nil {
		t.Fatalf("%s is not an upload recording: %v", key, err)
	}
	if len(file.Bytes) == 0 || file.ContentType == "" {
		t.Fatalf("%s carries no bytes or no content type", key)
	}
	return systemone.Image{ContentType: file.ContentType, Bytes: file.Bytes}
}

// storySeeds seeds the V04 selfie and the catalog product photo, and
// names the jhumkas' seeded id for the media join.
func (e *lookEnv) storySeeds(t *testing.T, user string) (selfieID string) {
	t.Helper()
	selfieID = e.seed(t, user, "selfie", recordedImage(t, "earring-upload-src"))
	e.catalog["gold-jhumkas"] = e.seed(t, user, "product", recordedImage(t, "earring-upload-ref"))
	return selfieID
}

// smileSeeds seeds the P12 sample the smile recording uploads. The
// template-only fix photographs nothing, so no catalog picture is needed.
func (e *lookEnv) smileSeeds(t *testing.T, user string) string {
	t.Helper()
	return e.seed(t, user, "selfie", recordedImage(t, "smile-upload-src"))
}

func (e *lookEnv) optIn(t *testing.T, user, occasion string, fixes ...string) {
	t.Helper()
	for _, fix := range fixes {
		if _, err := e.srv.db.Writer().ExecContext(t.Context(),
			`INSERT INTO opt_ins (user_id, occasion, fix_id) VALUES (?, ?, ?)`, user, occasion, fix); err != nil {
			t.Fatalf("opt in %s: %v", fix, err)
		}
	}
}

func (e *lookEnv) history(t *testing.T, user string, on bool) {
	t.Helper()
	if _, err := e.srv.db.Writer().ExecContext(t.Context(),
		`INSERT INTO history_switch (user_id, enabled) VALUES (?, ?)
		 ON CONFLICT (user_id) DO UPDATE SET enabled = excluded.enabled`,
		user, b2i(on)); err != nil {
		t.Fatalf("history: %v", err)
	}
}

// start posts the recorded start request and decodes the two ids. The
// recorder rides out so the start's own shape can be pinned.
func (e *lookEnv) start(t *testing.T, cookie *http.Cookie, selfieID string, productIDs ...string) (string, string, *httptest.ResponseRecorder) {
	t.Helper()
	body := map[string]any{
		"occasion":  "family-function",
		"selfie_id": selfieID,
	}
	if productIDs != nil {
		body["product_ids"] = productIDs
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rec := do(t, e.handler, http.MethodPost, "/api/looks", cookie, strings.NewReader(string(raw)), headerJSON)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start status %d body %s", rec.Code, rec.Body.String())
	}
	var started struct {
		LookID string `json:"look_id"`
		JobID  string `json:"job_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
		t.Fatalf("start body %s: %v", rec.Body.String(), err)
	}
	if !id.Valid(started.LookID) || !id.Valid(started.JobID) {
		t.Fatalf("start ids %q %q", started.LookID, started.JobID)
	}
	if started.LookID != started.JobID {
		t.Fatalf("the look id %s and the job id %s name one look", started.LookID, started.JobID)
	}
	return started.LookID, started.JobID, rec
}

// park holds one kind of Clef ask until the returned release is called,
// so a test reads a look at a known stage. The release is also a cleanup:
// an abandoned park ends with the test instead of hanging the gate.
func (e *lookEnv) park(kind string) (release func()) {
	e.t.Helper()
	ch := make(chan struct{})
	switch kind {
	case "judge":
		e.clef.parkJudge = ch
	case "pick":
		e.clef.parkPick = ch
	case "check":
		e.clef.parkCheck = ch
	default:
		e.t.Fatalf("park: unknown ask kind %q", kind)
	}
	var once sync.Once
	release = func() { once.Do(func() { close(ch) }) }
	e.t.Cleanup(release)
	return release
}

// waitFor polls a condition until it holds, so a test reads a stage the
// look has actually reached rather than one it might reach.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("never %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// jobState reads one job state, following next_id when the held attempt
// was replaced. It returns the id that answered and its body.
func (e *lookEnv) jobState(t *testing.T, cookie *http.Cookie, jobID string) (string, map[string]any) {
	t.Helper()
	rec := do(t, e.handler, http.MethodGet, "/api/jobs/"+jobID, cookie, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("job read %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("job body %s: %v", rec.Body.String(), err)
	}
	return jobID, body
}

// waitDone polls the job read until the look finishes, following the
// replacement attempt a resume names. It returns the id that finished.
func (e *lookEnv) waitDone(t *testing.T, cookie *http.Cookie, jobID string) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		held, body := e.jobState(t, cookie, jobID)
		status, _ := body["status"].(string)
		switch status {
		case "done":
			return held
		case "error", "cancelled":
			t.Fatalf("job ended %s: %v", status, body)
		}
		if next, ok := body["next_id"].(string); ok && next != "" {
			jobID = next
		} else if status == "interrupted" {
			t.Fatalf("job interrupted with no replacement: %v", body)
		}
		if time.Now().After(deadline) {
			t.Fatalf("job stayed %s: %v", status, body)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitError polls the job read until the look fails. A failed look is not
// resumed, so the held id is the one that answers.
func (e *lookEnv) waitError(t *testing.T, cookie *http.Cookie, jobID string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		_, body := e.jobState(t, cookie, jobID)
		switch status, _ := body["status"].(string); status {
		case "error":
			return body
		case "done", "cancelled":
			t.Fatalf("job ended %s, want error: %v", status, body)
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never failed: %v", body)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (e *lookEnv) get(t *testing.T, cookie *http.Cookie, path string) *httptest.ResponseRecorder {
	t.Helper()
	return do(t, e.handler, http.MethodGet, path, cookie, nil, nil)
}

func (e *lookEnv) put(t *testing.T, cookie *http.Cookie, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return do(t, e.handler, http.MethodPut, path, cookie, strings.NewReader(body), headerJSON)
}

var headerJSON = http.Header{"Content-Type": []string{"application/json"}}

// storyHits is the recorded second visit: the jhumkas kept, the red lip
// dismissed, in the words the memory file writes and parses.
func storyHits() []memory.Hit {
	return []memory.Hit{
		{NodeID: "n1", Content: "Family function kept the gold jhumkas.", ConceptType: memory.ConceptObservation},
		{NodeID: "n2", Content: "Family function dismissed the red lip.", ConceptType: memory.ConceptObservation},
	}
}

// ---------------------------------------------------------------------------
// The recorded examples

// apiFixture is one recorded example in testdata/api: the request it
// answers, the status and headers it carries, and the body. A stream
// fixture's body is the frame text as a JSON string.
type apiFixture struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
	Request json.RawMessage   `json:"request"`
}

func fixture(t *testing.T, name string) apiFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "api", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var f apiFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return f
}

// The ids the fixtures name for one look. The wiring mints its own, and
// makes the look id and the job id one, so every fixture id for this
// look renames to the id the server answered with.
var (
	fixtureLookIDs = []string{
		"100c0000000000000000000000000001",
		"100c0000000000000000000000000002",
		"100c0000000000000000000000000003",
	}
	fixtureJobIDs = []string{
		"10b00000000000000000000000000001",
		"10b00000000000000000000000000002",
		"10b0000000000000000000000000000e",
		"10b0000000000000000000000000000c",
		"10b0000000000000000000000000000d",
		"5a391e00000000000000000000000002",
	}
	fixtureSelfieID  = "5e1f1e00000000000000000000000001"
	fixtureRenderID  = "2e4de200000000000000000000000001"
	fixtureCatalogID = "c0a10600000000000000000000000001"
)

// renames maps the fixture's values onto this replay's: the minted ids
// and the media urls that follow them. renderID is empty when no render
// was shown.
func (e *lookEnv) renames(lookID, selfieID, renderID string) map[string]string {
	r := map[string]string{
		fixtureSelfieID:  selfieID,
		fixtureCatalogID: e.catalog["gold-jhumkas"],
	}
	r["/media/"+fixtureSelfieID] = "/media/" + selfieID
	r["/media/"+fixtureCatalogID] = "/media/" + e.catalog["gold-jhumkas"]
	for _, fid := range fixtureLookIDs {
		r[fid] = lookID
	}
	for _, fid := range fixtureJobIDs {
		r[fid] = lookID
	}
	if renderID != "" {
		r[fixtureRenderID] = renderID
		r["/media/"+fixtureRenderID] = "/media/" + renderID
	}
	return r
}

// renameTree substitutes whole string values, so a suggestion that
// contains a renamed phrase is untouched: only the value the fixture
// names as its own is the one a replay cannot mint.
func renameTree(v any, renames map[string]string) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = renameTree(val, renames)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = renameTree(val, renames)
		}
		return out
	case string:
		if to, ok := renames[x]; ok {
			return to
		}
	}
	return v
}

// compare pins one response against its recorded example: the status,
// the headers the contract names, and the body member for member.
func compare(t *testing.T, name string, rec *httptest.ResponseRecorder, renames map[string]string) {
	t.Helper()
	f := fixture(t, name)
	if rec.Code != f.Status {
		t.Fatalf("%s: status %d body %s, want %d", name, rec.Code, rec.Body.String(), f.Status)
	}
	for key, want := range f.Headers {
		if got := rec.Header().Get(key); got != want {
			t.Fatalf("%s: header %s %q, want %q", name, key, got, want)
		}
	}
	var want, got any
	if err := json.Unmarshal(f.Body, &want); err != nil {
		t.Fatalf("%s: fixture body: %v", name, err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("%s: body %s: %v", name, rec.Body.String(), err)
	}
	want = renameTree(want, renames)
	if !reflect.DeepEqual(want, got) {
		wj, _ := json.Marshal(want)
		gj, _ := json.Marshal(got)
		t.Fatalf("%s: body\n got %s\nwant %s", name, gj, wj)
	}
}

// lookFrame is one event frame read from a stream. Comment frames carry
// no event and no data and are dropped: a heartbeat is the broker's own
// clock, which a replay cannot carry.
type lookFrame struct {
	Name string
	ID   string
	Data json.RawMessage
}

// parseFrames reads to the terminal frame and stops there. The broker
// holds the response open after it, so a reader that waits for the end
// of the body waits for its own deadline.
func parseFrames(r io.Reader) []lookFrame {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var out []lookFrame
	var name, id, data string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if name != "" || data != "" {
				out = append(out, lookFrame{Name: name, ID: id, Data: json.RawMessage(data)})
				switch name {
				case "done", "error", "cancelled", "interrupted":
					return out
				}
			}
			name, id, data = "", "", ""
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "id: "):
			id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		}
	}
	return out
}

// readFrames reads one job's events to the terminal frame, live. Keel
// answers a subscriber that arrives after the terminal frame with that
// frame alone, and a Last-Event-ID of 0 is no cursor at all, so the
// subscription opens while the look is held at its first ask and the
// release lets it run: the frames are read as they are published, the
// order the app reads them in. The stream's own headers are the
// contract's on every job, so they are pinned here; a comment frame is
// dropped, because a heartbeat is the broker's clock.
func (e *lookEnv) readFrames(t *testing.T, cookie *http.Cookie, jobID string, release func()) ([]lookFrame, http.Header, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ts := httptest.NewServer(e.handler)
	defer ts.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/jobs/"+jobID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" ||
		resp.Header.Get("Cache-Control") != "no-store" ||
		resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("stream headers %v", resp.Header)
	}
	// The headers are written after the subscription is registered, so
	// the look can run now without losing its first frame.
	if release != nil {
		release()
	}
	frames := parseFrames(resp.Body)
	cancel()
	if len(frames) == 0 {
		t.Fatal("the stream carried no frame")
	}
	switch last := frames[len(frames)-1].Name; last {
	case "done", "error", "cancelled", "interrupted":
	default:
		t.Fatalf("the stream ended on %q, not a terminal frame: %v", last, frames)
	}
	return frames, resp.Header.Clone(), resp.StatusCode
}

// compareFrames pins one job's events against the recorded example: the
// status and headers, the frame names in order, and every payload member
// for member. Frame ids must increase; their values are the broker's,
// not the contract's. The frames are read before the look finishes and
// compared after, because the renames carry the render id the finished
// look names.
func compareFrames(t *testing.T, name string, frames []lookFrame, header http.Header, status int, renames map[string]string) {
	t.Helper()
	f := fixture(t, name)
	if status != f.Status {
		t.Fatalf("%s: stream status %d, want %d", name, status, f.Status)
	}
	for key, want := range f.Headers {
		if got := header.Get(key); got != want {
			t.Fatalf("%s: stream header %s %q, want %q", name, key, got, want)
		}
	}
	var text string
	if err := json.Unmarshal(f.Body, &text); err != nil {
		t.Fatalf("%s: fixture body is not frame text: %v", name, err)
	}
	want := parseFrames(strings.NewReader(text))
	got := frames
	if len(got) != len(want) {
		t.Fatalf("%s: %d frames, want %d\n got %v\nwant %v", name, len(got), len(want), got, want)
	}
	last := 0
	for i := range want {
		if got[i].Name != want[i].Name {
			t.Fatalf("%s: frame %d is %q, want %q", name, i, got[i].Name, want[i].Name)
		}
		n := 0
		if _, err := fmt.Sscanf(got[i].ID, "%d", &n); err != nil || n <= last {
			t.Fatalf("%s: frame %d id %q does not increase from %d", name, i, got[i].ID, last)
		}
		last = n
		var w, g any
		if err := json.Unmarshal(want[i].Data, &w); err != nil {
			t.Fatalf("%s: fixture frame %d: %v", name, i, err)
		}
		if err := json.Unmarshal(got[i].Data, &g); err != nil {
			t.Fatalf("%s: frame %d %s: %v", name, i, got[i].Data, err)
		}
		w = renameTree(w, renames)
		if !reflect.DeepEqual(w, g) {
			wj, _ := json.Marshal(w)
			gj, _ := json.Marshal(g)
			t.Fatalf("%s: frame %d\n got %s\nwant %s", name, i, gj, wj)
		}
	}
}

// frameStages names each frame the way the progress view reads it: the
// stage on a progress frame, the event name on a terminal one.
func frameStages(t *testing.T, frames []lookFrame) []string {
	t.Helper()
	out := make([]string, 0, len(frames))
	for _, f := range frames {
		if f.Name != "progress" {
			out = append(out, f.Name)
			continue
		}
		var payload struct {
			Stage string `json:"stage"`
		}
		if err := json.Unmarshal(f.Data, &payload); err != nil {
			t.Fatalf("progress frame %s: %v", f.Data, err)
		}
		out = append(out, payload.Stage)
	}
	return out
}

// stageFrame returns the detail of the first frame on one stage.
func stageFrame(t *testing.T, frames []lookFrame, stage string) map[string]any {
	t.Helper()
	for _, f := range frames {
		if f.Name != "progress" {
			continue
		}
		var payload struct {
			Stage  string          `json:"stage"`
			Detail json.RawMessage `json:"detail"`
		}
		if err := json.Unmarshal(f.Data, &payload); err != nil {
			t.Fatalf("progress frame %s: %v", f.Data, err)
		}
		if payload.Stage != stage {
			continue
		}
		var detail map[string]any
		if err := json.Unmarshal(payload.Detail, &detail); err != nil {
			t.Fatalf("%s detail %s: %v", stage, payload.Detail, err)
		}
		return detail
	}
	t.Fatalf("no %s frame in %v", stage, frames)
	return nil
}

// shownRenderID reads the media id the look ended on, the one the
// renames carry into every other route's answer.
func shownRenderID(t *testing.T, body map[string]any) string {
	t.Helper()
	shown, _ := body["shown_render"].(map[string]any)
	if shown == nil {
		return ""
	}
	mediaID, _ := shown["media_id"].(string)
	if !id.Valid(mediaID) {
		t.Fatalf("shown render media id %q", mediaID)
	}
	return mediaID
}

// bodyOf decodes one response body as generic JSON.
func bodyOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %s: %v", rec.Body.String(), err)
	}
	return body
}

// ---------------------------------------------------------------------------
// The routes, on the recorded story

// TestTheEarringStoryAnswersEveryLookRoute is the Done-when clause: every
// look route of T1.5 answers its recorded example's shape on a replay.
// History is on and the account remembers the second visit, so the pick
// frame carries the recall sentence and the verdicts are remembered.
func TestTheEarringStoryAnswersEveryLookRoute(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, storyHits())
	user, cookie := bootUser(t, env.handler)
	selfieID := env.storySeeds(t, user)
	env.optIn(t, user, "family-function", "family-gold-jhumkas", "family-festive-top")
	env.history(t, user, true)

	release := env.park("judge")
	lookID, jobID, rec := env.start(t, cookie, selfieID)
	compare(t, "look-start", rec, env.renames(lookID, selfieID, ""))
	frames, header, status := env.readFrames(t, cookie, jobID, release)
	env.waitDone(t, cookie, jobID)

	// The verdict read names the render the other routes carry.
	lookRec := env.get(t, cookie, "/api/looks/"+lookID)
	renderID := shownRenderID(t, bodyOf(t, lookRec))
	r := env.renames(lookID, selfieID, renderID)
	compare(t, "look", lookRec, r)
	compare(t, "job-done", env.get(t, cookie, "/api/jobs/"+jobID), r)
	compare(t, "looks", env.get(t, cookie, "/api/looks?occasion=family-function"), r)
	compare(t, "renders", env.get(t, cookie, "/api/looks/"+lookID+"/renders"), r)
	compare(t, "shop", env.get(t, cookie, "/api/looks/"+lookID+"/shop"), r)
	compare(t, "history-put", env.get(t, cookie, "/api/history"), r)
	compare(t, "recall", env.get(t, cookie, "/api/history/recall?occasion=family-function"), r)
	compare(t, "recall-empty", env.get(t, cookie, "/api/history/recall?occasion=date"), r)
	compareFrames(t, "job-events", frames, header, status, r)

	// The three verdicts, each stored first and then remembered in the
	// words the memory file writes.
	for _, v := range []struct{ fixture, verdict string }{
		{"verdict", "kept"},
		{"verdict-dismissed", "dismissed"},
		{"verdict-bought", "bought"},
	} {
		rec := env.put(t, cookie, "/api/looks/"+lookID+"/verdict", `{"verdict":"`+v.verdict+`"}`)
		compare(t, v.fixture, rec, r)
	}
	want := []string{
		"Family function kept the gold jhumkas.",
		"Family function dismissed the gold jhumkas.",
		"Family function bought the gold jhumkas.",
	}
	concepts := env.mem.written()
	if len(concepts) != len(want) {
		t.Fatalf("%d concepts, want %d: %+v", len(concepts), len(want), concepts)
	}
	for i, c := range concepts {
		if c.Content != want[i] || c.ConceptType != memory.ConceptObservation {
			t.Fatalf("concept %d %+v, want %q as an observation", i, c, want[i])
		}
	}

	// One render created, two files uploaded (the selfie and the catalog
	// picture), one unit settled and nothing left reserved.
	creates, uploads, _, _ := env.cam.count()
	if creates != 1 || uploads != 2 {
		t.Fatalf("creates %d uploads %d, want one task and two files", creates, uploads)
	}
	if env.budget.Spent() != 1 || env.budget.Reserved() != 0 {
		t.Fatalf("spent %d reserved %d, want the one-unit render settled", env.budget.Spent(), env.budget.Reserved())
	}
	for _, kind := range []string{"judge", "pick", "check"} {
		if n := env.clef.count(kind); n != 1 {
			t.Fatalf("%s asked %d times, want once", kind, n)
		}
	}
	if recalls, writes := env.mem.count(); recalls == 0 || writes != 3 {
		t.Fatalf("recalls %d writes %d, want a recall and three verdicts", recalls, writes)
	}
}

// TestAPendingLookIsTheIdsOnly holds the judge, so the verdict read has
// no outcome to carry: the ids only, and the list agrees.
func TestAPendingLookIsTheIdsOnly(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	user, cookie := bootUser(t, env.handler)
	selfieID := env.storySeeds(t, user)
	env.optIn(t, user, "family-function", "family-gold-jhumkas", "family-festive-top")
	release := env.park("judge")

	lookID, jobID, _ := env.start(t, cookie, selfieID)
	waitFor(t, "the judge was asked", func() bool { return env.clef.count("judge") == 1 })
	compare(t, "look-pending", env.get(t, cookie, "/api/looks/"+lookID), env.renames(lookID, selfieID, ""))

	list := bodyOf(t, env.get(t, cookie, "/api/looks?occasion=family-function"))
	items, _ := list["looks"].([]any)
	if len(items) != 1 {
		t.Fatalf("list %v, want the one pending look", list)
	}
	item := items[0].(map[string]any)
	if item["ready"] != false || item["words"] != "" || item["fix"] != "" || item["verdict"] != "" {
		t.Fatalf("pending item %v, want no outcome yet", item)
	}

	release()
	env.waitDone(t, cookie, jobID)
	lookRec := env.get(t, cookie, "/api/looks/"+lookID)
	compare(t, "look", lookRec, env.renames(lookID, selfieID, shownRenderID(t, bodyOf(t, lookRec))))
}

// TestTheJobStateWhileTheCheckRuns reads the job state at the render
// stage: the render frame is published when the render lands, so the
// check is the ask that holds the look there.
func TestTheJobStateWhileTheCheckRuns(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	user, cookie := bootUser(t, env.handler)
	selfieID := env.storySeeds(t, user)
	env.optIn(t, user, "family-function", "family-gold-jhumkas", "family-festive-top")
	release := env.park("check")

	lookID, jobID, _ := env.start(t, cookie, selfieID)
	waitFor(t, "the check was asked", func() bool { return env.clef.count("check") == 1 })
	compare(t, "job", env.get(t, cookie, "/api/jobs/"+jobID), env.renames(lookID, selfieID, ""))
	release()
	env.waitDone(t, cookie, jobID)
	lookRec := env.get(t, cookie, "/api/looks/"+lookID)
	compare(t, "job-done", env.get(t, cookie, "/api/jobs/"+jobID),
		env.renames(lookID, selfieID, shownRenderID(t, bodyOf(t, lookRec))))
}

// TestTheSuggestionStoryDiscardsBothRenders is the second stream fixture:
// both checks discard, the runner-up renders and is discarded too, no
// media id is ever published for a discarded render, and the look stands
// on its suggestions while shop still runs.
func TestTheSuggestionStoryDiscardsBothRenders(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, suggestionAnswers(), 8, nil)
	user, cookie := bootUser(t, env.handler)
	selfieID := env.storySeeds(t, user)
	// The red lip is the runner-up this replay can render: the festive
	// top composites a product photo, and one look names one candidate.
	env.optIn(t, user, "family-function", "family-gold-jhumkas", "family-red-lip")

	release := env.park("judge")
	lookID, jobID, _ := env.start(t, cookie, selfieID)
	frames, header, status := env.readFrames(t, cookie, jobID, release)
	env.waitDone(t, cookie, jobID)
	r := env.renames(lookID, selfieID, "")
	r["family-festive-top"] = "family-red-lip"

	compare(t, "look-suggestion", env.get(t, cookie, "/api/looks/"+lookID), r)
	compare(t, "renders-none", env.get(t, cookie, "/api/looks/"+lookID+"/renders"), r)
	compare(t, "shop-none", env.get(t, cookie, "/api/looks/"+lookID+"/shop"), r)
	compare(t, "job-done", env.get(t, cookie, "/api/jobs/"+jobID), r)
	compareFrames(t, "job-events-suggestion", frames, header, status, r)

	creates, _, _, _ := env.cam.count()
	if creates != 2 {
		t.Fatalf("creates %d, want the fix and the runner-up", creates)
	}
	if env.budget.Spent() != 2 || env.budget.Reserved() != 0 {
		t.Fatalf("spent %d reserved %d, want one settle per render", env.budget.Spent(), env.budget.Reserved())
	}
	if n := env.clef.count("check"); n != 2 {
		t.Fatalf("check asked %d times, want once per render", n)
	}
	// History is off: the verdict is stored, nothing is remembered, and
	// the memory was never read.
	rec := env.put(t, cookie, "/api/looks/"+lookID+"/verdict", `{"verdict":"kept"}`)
	body := bodyOf(t, rec)
	if rec.Code != http.StatusOK || body["verdict"] != "kept" || body["remembered"] != false {
		t.Fatalf("verdict %d %v, want kept and remembered false", rec.Code, body)
	}
	if recalls, writes := env.mem.count(); recalls != 0 || writes != 0 {
		t.Fatalf("recalls %d writes %d, want no memory touched with history off", recalls, writes)
	}
}

// TestTheSmileStoryShopsASimulation is the template-only shop fixture: a
// fix with nothing to photograph lists the suggestion itself, under the
// basket's own category and no picture, named by the fix's display entry
// in the occasions data.
func TestTheSmileStoryShopsASimulation(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, smileAnswers(), 8, nil)
	user, cookie := bootUser(t, env.handler)
	selfieID := env.smileSeeds(t, user)
	env.optIn(t, user, "family-function", "family-closed-smile")

	release := env.park("judge")
	lookID, jobID, _ := env.start(t, cookie, selfieID)
	frames, _, _ := env.readFrames(t, cookie, jobID, release)
	env.waitDone(t, cookie, jobID)
	lookRec := env.get(t, cookie, "/api/looks/"+lookID)
	renderID := shownRenderID(t, bodyOf(t, lookRec))
	r := env.renames(lookID, selfieID, renderID)
	compare(t, "shop-simulation", env.get(t, cookie, "/api/looks/"+lookID+"/shop"), r)

	shop := stageFrame(t, frames, "shop")
	products, _ := shop["products"].([]any)
	if len(products) != 1 {
		t.Fatalf("shop frame %v, want the one simulation", shop)
	}
	entry := products[0].(map[string]any)
	if _, hasMedia := entry["media_id"]; hasMedia {
		t.Fatalf("a simulation carries a picture: %v", entry)
	}
	if entry["fidelity"] != "visual simulation" || entry["id"] != "family-closed-smile" {
		t.Fatalf("simulation entry %v", entry)
	}
	// The shop stage and the shop path return the same array.
	path := bodyOf(t, env.get(t, cookie, "/api/looks/"+lookID+"/shop"))
	if !reflect.DeepEqual(path["products"], shop["products"]) {
		t.Fatalf("the shop stage %v and the shop path %v disagree", shop["products"], path["products"])
	}
	if creates, _, _, _ := env.cam.count(); creates != 1 {
		t.Fatalf("creates %d, want the one smile task", creates)
	}
}

// TestANoFacePhotoFailsTheJob is the error fixture: the provider refuses
// the photo, the job ends error with the contract's code and phrase, the
// stream carries the envelope twice as Keel writes it, and there is no
// verdict. The stage is measure, which the example was corrected to:
// the judge is a Clef ask with no face detector, so the refusal lands in
// the render, and the pipeline publishes the render frame only once the
// render lands. The last stage published is the one the state names.
func TestANoFacePhotoFailsTheJob(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, smileAnswers(), 8, nil)
	user, cookie := bootUser(t, env.handler)
	// The recorded no-face upload is the ai-smile task fed a product
	// photograph: no face, and no person.
	selfieID := env.seed(t, user, "selfie", recordedImage(t, "err-no-face-upload-src"))
	env.optIn(t, user, "family-function", "family-closed-smile")

	release := env.park("judge")
	lookID, jobID, _ := env.start(t, cookie, selfieID)
	frames, header, status := env.readFrames(t, cookie, jobID, release)
	state := env.waitError(t, cookie, jobID)
	r := env.renames(lookID, selfieID, "")
	rec := env.get(t, cookie, "/api/jobs/"+jobID)
	compare(t, "job-error", rec, r)
	if state["status"] != "error" {
		t.Fatalf("state %v, want error", state)
	}
	// The example records the error frame alone. A live stream carries
	// the stages that ran before the fault, so they are pinned by name
	// and the example is compared against the terminal frame.
	if got, want := frameStages(t, frames), []string{"judge", "pick", "measure", "error"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("the failed look's frames %v, want %v", got, want)
	}
	compareFrames(t, "job-events-error", frames[len(frames)-1:], header, status, r)

	// The look row exists - the start was accepted - but no outcome ever
	// landed, so the verdict read stays the ids only.
	lookRec := env.get(t, cookie, "/api/looks/"+lookID)
	compare(t, "look-pending", lookRec, env.renames(lookID, selfieID, ""))
	if _, writes := env.mem.count(); writes != 0 {
		t.Fatalf("writes %d, want nothing remembered of a failed look", writes)
	}
	if env.budget.Reserved() != 0 {
		t.Fatalf("reserved %d, want the refused render's hold released", env.budget.Reserved())
	}
	if n := env.clef.count("check"); n != 0 {
		t.Fatalf("check asked %d times, want none: there is no render to read", n)
	}
}

// TestRendersPausedRefusesTheRenderNotTheLook is the operator's switch:
// the start still answers 202, the render stage carries the refusal's
// code and phrase, no unit is quoted, no check is asked, the suggestion
// stands, shop runs and done arrives.
func TestRendersPausedRefusesTheRenderNotTheLook(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	user, cookie := bootUser(t, env.handler)
	selfieID := env.storySeeds(t, user)
	env.optIn(t, user, "family-function", "family-gold-jhumkas", "family-festive-top")
	if err := env.srv.setIntakeFlag(t.Context(), rendersOffFlag, true); err != nil {
		t.Fatal(err)
	}

	release := env.park("judge")
	lookID, jobID, rec := env.start(t, cookie, selfieID)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start %d, want the switch not to refuse a start", rec.Code)
	}
	frames, _, _ := env.readFrames(t, cookie, jobID, release)
	env.waitDone(t, cookie, jobID)

	render := stageFrame(t, frames, "render")
	paused := fixture(t, "errors/renders-paused")
	var want struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(paused.Body, &want); err != nil {
		t.Fatal(err)
	}
	if render["code"] != want.Error.Code || render["message"] != want.Error.Message {
		t.Fatalf("render detail %v, want the refusal %q %q", render, want.Error.Code, want.Error.Message)
	}
	if _, hasMedia := render["media_id"]; hasMedia {
		t.Fatalf("a refused render carries a media id: %v", render)
	}
	for _, f := range frames {
		if f.Name == "progress" && strings.Contains(string(f.Data), `"stage":"check"`) {
			t.Fatalf("a refused render was checked: %s", f.Data)
		}
	}
	if frames[len(frames)-1].Name != "done" {
		t.Fatalf("last frame %q, want done", frames[len(frames)-1].Name)
	}

	r := env.renames(lookID, selfieID, "")
	lookRec := env.get(t, cookie, "/api/looks/"+lookID)
	compare(t, "look-suggestion", lookRec, r)
	compare(t, "renders-none", env.get(t, cookie, "/api/looks/"+lookID+"/renders"), r)
	compare(t, "shop-none", env.get(t, cookie, "/api/looks/"+lookID+"/shop"), r)

	if creates, uploads, _, _ := env.cam.count(); creates != 0 || uploads != 0 {
		t.Fatalf("creates %d uploads %d, want nothing called while paused", creates, uploads)
	}
	if env.budget.Spent() != 0 || env.budget.Reserved() != 0 {
		t.Fatalf("spent %d reserved %d, want empty books", env.budget.Spent(), env.budget.Reserved())
	}
	if n := env.clef.count("check"); n != 0 {
		t.Fatalf("check asked %d times, want none", n)
	}
}

// TestAFailedRememberStillStoresTheVerdict: the row is written first, so
// a memory that refuses leaves the verdict standing and answers
// remembered false.
func TestAFailedRememberStillStoresTheVerdict(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	user, cookie := bootUser(t, env.handler)
	selfieID := env.storySeeds(t, user)
	env.optIn(t, user, "family-function", "family-gold-jhumkas", "family-festive-top")
	env.history(t, user, true)
	env.mem.failRemember = true

	lookID, jobID, _ := env.start(t, cookie, selfieID)
	env.waitDone(t, cookie, jobID)
	rec := env.put(t, cookie, "/api/looks/"+lookID+"/verdict", `{"verdict":"kept"}`)
	body := bodyOf(t, rec)
	if rec.Code != http.StatusOK || body["remembered"] != false {
		t.Fatalf("verdict %d %v, want 200 and remembered false", rec.Code, body)
	}
	stored := bodyOf(t, env.get(t, cookie, "/api/looks/"+lookID))
	if stored["verdict"] != "kept" {
		t.Fatalf("stored verdict %v, want kept", stored["verdict"])
	}
}

// TestASecondVisitSkipsTheDismissedFix: the recall rides in the pick
// frame, and the dismissed fix is not a candidate at all - not first and
// not runner-up - even though the user ticked it.
func TestASecondVisitSkipsTheDismissedFix(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, storyHits())
	user, cookie := bootUser(t, env.handler)
	selfieID := env.storySeeds(t, user)
	env.optIn(t, user, "family-function", "family-gold-jhumkas", "family-red-lip")
	env.history(t, user, true)
	// The model would name the red lip behind the jhumkas; the menu never
	// offers it, so the answer cannot carry it.
	env.clef.picks[0].probs = map[string]float64{"family-red-lip": 0.4}

	release := env.park("judge")
	lookID, jobID, _ := env.start(t, cookie, selfieID)
	frames, _, _ := env.readFrames(t, cookie, jobID, release)
	env.waitDone(t, cookie, jobID)
	pick := stageFrame(t, frames, "pick")
	if pick["recall"] != "Last family function you kept the gold jhumkas and dismissed the red lip." {
		t.Fatalf("pick recall %v, want the memory sentence", pick["recall"])
	}
	if pick["runner_up"] != "" {
		t.Fatalf("runner_up %v, want none: the only other tick is dismissed", pick["runner_up"])
	}
	lookRec := env.get(t, cookie, "/api/looks/"+lookID)
	renderID := shownRenderID(t, bodyOf(t, lookRec))
	r := env.renames(lookID, selfieID, renderID)
	r["family-festive-top"] = ""
	compare(t, "look", lookRec, r)
	if creates, _, _, _ := env.cam.count(); creates != 1 {
		t.Fatalf("creates %d, want the jhumkas alone", creates)
	}
}

// TestAFailedRecallIsAFirstVisit: a Lambo fault never fails a start. The
// look runs with no recall and the pick frame carries none.
func TestAFailedRecallIsAFirstVisit(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, storyHits())
	user, cookie := bootUser(t, env.handler)
	selfieID := env.storySeeds(t, user)
	env.optIn(t, user, "family-function", "family-gold-jhumkas", "family-festive-top")
	env.history(t, user, true)
	env.mem.failRecall = true

	release := env.park("judge")
	_, jobID, rec := env.start(t, cookie, selfieID)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start %d %s, want a recall fault not to fail it", rec.Code, rec.Body.String())
	}
	frames, _, _ := env.readFrames(t, cookie, jobID, release)
	env.waitDone(t, cookie, jobID)
	pick := stageFrame(t, frames, "pick")
	if _, hasRecall := pick["recall"]; hasRecall {
		t.Fatalf("pick frame carries a recall after a failed read: %v", pick)
	}
	if pick["runner_up"] != "family-festive-top" {
		t.Fatalf("runner_up %v, want the menu's own second fix", pick["runner_up"])
	}
	// The recall route answers a first visit too, not an error.
	compare(t, "recall-empty", env.get(t, cookie, "/api/history/recall?occasion=family-function"),
		map[string]string{"date": "family-function"})
}

// TestHistoryOnAndOff: the switch reads off until it is set, the notice
// is the contract's own sentence on each side, and turning it on does not
// read the memory until a recall asks.
func TestHistoryOnAndOff(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	_, cookie := bootUser(t, env.handler)
	compare(t, "history", env.get(t, cookie, "/api/history"), nil)
	compare(t, "recall-off", env.get(t, cookie, "/api/history/recall?occasion=family-function"), nil)
	if recalls, _ := env.mem.count(); recalls != 0 {
		t.Fatalf("recalls %d, want none with history off", recalls)
	}
	rec := env.put(t, cookie, "/api/history", `{"enabled":true}`)
	compare(t, "history-put", rec, nil)
	compare(t, "history-put", env.get(t, cookie, "/api/history"), nil)
	compare(t, "recall-empty", env.get(t, cookie, "/api/history/recall?occasion=family-function"),
		map[string]string{"date": "family-function"})
	off := env.put(t, cookie, "/api/history", `{"enabled":false}`)
	compare(t, "history", off, nil)
	bad := env.put(t, cookie, "/api/history", `{"enabled":"yes"}`)
	assertErr(t, bad, http.StatusBadRequest, codeInvalid, msgInvalid)
}

// TestTheStartRefusals: every refusal the contract names, each before a
// paid call and each with its recorded envelope.
func TestTheStartRefusals(t *testing.T) {
	t.Parallel()

	t.Run("samples only wins over uploads paused", func(t *testing.T) {
		t.Parallel()
		env := newLookEnv(t, earringAnswers(), 8, nil)
		user, cookie := bootUser(t, env.handler)
		selfieID := env.storySeeds(t, user)
		env.optIn(t, user, "family-function", "family-gold-jhumkas")
		body := map[string]any{"occasion": "family-function", "selfie_id": selfieID}
		if err := env.srv.setIntakeFlag(t.Context(), freshUploadsOffFlag, true); err != nil {
			t.Fatal(err)
		}
		compare(t, "errors/uploads-paused", postJSON(t, env.handler, "/api/looks", cookie, body), nil)
		if err := env.srv.setIntakeFlag(t.Context(), samplesOnlyFlag, true); err != nil {
			t.Fatal(err)
		}
		compare(t, "errors/samples-only", postJSON(t, env.handler, "/api/looks", cookie, body), nil)
		assertNothingStarted(t, env)
	})

	t.Run("the budget refuses before a seat is taken", func(t *testing.T) {
		t.Parallel()
		env := newLookEnv(t, earringAnswers(), 0, nil)
		user, cookie := bootUser(t, env.handler)
		selfieID := env.storySeeds(t, user)
		env.optIn(t, user, "family-function", "family-gold-jhumkas", "family-festive-top")
		body := map[string]any{"occasion": "family-function", "selfie_id": selfieID}
		compare(t, "errors/budget-refused", postJSON(t, env.handler, "/api/looks", cookie, body), nil)
		assertNothingStarted(t, env)
		// The lease is taken after the budget, so a refusal here burns no
		// judge capacity: the session holds no counters at all.
		var sessions int
		if err := env.srv.db.Reader().QueryRowContext(t.Context(),
			`SELECT count(*) FROM judge_sessions`).Scan(&sessions); err != nil {
			t.Fatal(err)
		}
		if sessions != 0 {
			t.Fatalf("judge_sessions holds %d rows after a budget refusal", sessions)
		}
	})

	t.Run("the fresh selfie cap", func(t *testing.T) {
		t.Parallel()
		env := newLookEnv(t, earringAnswers(), 8, nil)
		env.srv.looksOf().cfg.Lease.Judge.FreshSelfies = 1
		user, cookie := bootUser(t, env.handler)
		selfieID := env.storySeeds(t, user)
		env.optIn(t, user, "family-function", "family-gold-jhumkas")
		_, jobID, _ := env.start(t, cookie, selfieID)
		env.waitDone(t, cookie, jobID)
		body := map[string]any{"occasion": "family-function", "selfie_id": selfieID}
		compare(t, "errors/lease-refused", postJSON(t, env.handler, "/api/looks", cookie, body), nil)
		if creates, _, _, _ := env.cam.count(); creates != 1 {
			t.Fatalf("creates %d, want the one accepted look's task", creates)
		}
	})

	t.Run("the render cap", func(t *testing.T) {
		t.Parallel()
		env := newLookEnv(t, earringAnswers(), 8, nil)
		env.srv.looksOf().cfg.Lease.Judge.Renders = 1
		user, cookie := bootUser(t, env.handler)
		selfieID := env.storySeeds(t, user)
		env.optIn(t, user, "family-function", "family-gold-jhumkas")
		body := map[string]any{"occasion": "family-function", "selfie_id": selfieID}
		rec := postJSON(t, env.handler, "/api/looks", cookie, body)
		compare(t, "errors/lease-refused", rec, map[string]string{"fresh_selfies": "renders"})
		assertNothingStarted(t, env)
	})

	t.Run("the request is validated", func(t *testing.T) {
		t.Parallel()
		env := newLookEnv(t, earringAnswers(), 8, nil)
		user, cookie := bootUser(t, env.handler)
		selfieID := env.storySeeds(t, user)
		env.optIn(t, user, "family-function", "family-gold-jhumkas")
		productPhoto := env.seed(t, user, "product", recordedImage(t, "earring-upload-ref"))

		// The recorded invalid-request example is the unknown occasion.
		rec := postJSON(t, env.handler, "/api/looks", cookie, map[string]any{
			"occasion": "wedding", "selfie_id": selfieID,
		})
		compare(t, "errors/invalid-request", rec, nil)

		for _, body := range []map[string]any{
			{"selfie_id": selfieID},
			{"occasion": "family-function"},
			{"occasion": "family-function", "selfie_id": "not-an-id"},
			{"occasion": "family-function", "selfie_id": selfieID, "product_ids": "gold-jhumkas"},
			{"occasion": "family-function", "selfie_id": selfieID, "product_ids": []string{"gold-jhumkas", "lip-red"}},
			{"occasion": "family-function", "selfie_id": selfieID, "product_ids": []string{productPhoto, productPhoto}},
		} {
			assertErr(t, postJSON(t, env.handler, "/api/looks", cookie, body), http.StatusBadRequest, codeInvalid, msgInvalid)
		}
		// A product upload is not a selfie.
		assertErr(t, postJSON(t, env.handler, "/api/looks", cookie, map[string]any{
			"occasion": "family-function", "selfie_id": productPhoto,
		}), http.StatusBadRequest, codeInvalid, msgInvalid)
		// A selfie this user does not hold, and one that never existed.
		other, _ := bootUser(t, env.handler)
		otherSelfie := env.seed(t, other, "selfie", recordedImage(t, "earring-upload-src"))
		for _, sid := range []string{otherSelfie, strings.Repeat("ab", 16)} {
			compare(t, "errors/not-found", postJSON(t, env.handler, "/api/looks", cookie, map[string]any{
				"occasion": "family-function", "selfie_id": sid,
			}), nil)
		}
		assertNothingStarted(t, env)
	})

	t.Run("another account's look is not found", func(t *testing.T) {
		t.Parallel()
		env := newLookEnv(t, earringAnswers(), 8, nil)
		user, cookie := bootUser(t, env.handler)
		selfieID := env.storySeeds(t, user)
		env.optIn(t, user, "family-function", "family-gold-jhumkas", "family-festive-top")
		lookID, jobID, _ := env.start(t, cookie, selfieID)
		env.waitDone(t, cookie, jobID)

		_, stranger := bootUser(t, env.handler)
		for _, path := range []string{
			"/api/looks/" + lookID,
			"/api/looks/" + lookID + "/renders",
			"/api/looks/" + lookID + "/shop",
			"/api/jobs/" + jobID,
			"/api/jobs/" + jobID + "/events",
		} {
			compare(t, "errors/not-found", env.get(t, stranger, path), nil)
		}
		compare(t, "errors/not-found", env.put(t, stranger, "/api/looks/"+lookID+"/verdict", `{"verdict":"kept"}`), nil)
		// The holder still reads it, and the verdict is untouched.
		body := bodyOf(t, env.get(t, cookie, "/api/looks/"+lookID))
		if body["verdict"] != "" {
			t.Fatalf("verdict %v, want a stranger's put not to store one", body["verdict"])
		}
	})
}

// assertNothingStarted pins that a refusal spent nothing: no task, no
// upload, no unit, and no look row.
func assertNothingStarted(t *testing.T, env *lookEnv) {
	t.Helper()
	if creates, uploads, _, _ := env.cam.count(); creates != 0 || uploads != 0 {
		t.Fatalf("creates %d uploads %d, want a refusal to call nothing", creates, uploads)
	}
	if env.budget.Spent() != 0 || env.budget.Reserved() != 0 {
		t.Fatalf("spent %d reserved %d, want empty books", env.budget.Spent(), env.budget.Reserved())
	}
	var looks int
	if err := env.srv.db.Reader().QueryRowContext(t.Context(), `SELECT count(*) FROM looks`).Scan(&looks); err != nil {
		t.Fatal(err)
	}
	if looks != 0 {
		t.Fatalf("looks holds %d rows after a refusal", looks)
	}
}

// TestAKilledLookResumesUnderTheSameId is the wiring's resume: a second
// runner opens over the same store, the dead attempt is interrupted, and
// the replacement finishes the look from the snapshot. The paid task is
// keyed, so it is created once; the recall rode in the snapshot, so the
// memory is read once; and the id the app holds names the replacement.
func TestAKilledLookResumesUnderTheSameId(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, storyHits())
	user, cookie := bootUser(t, env.handler)
	selfieID := env.storySeeds(t, user)
	env.optIn(t, user, "family-function", "family-gold-jhumkas", "family-festive-top")
	env.history(t, user, true)
	// The replacement runs the model calls again: they are deterministic
	// and cost neurons the budget does not meter.
	env.clef.picks = append(env.clef.picks, env.clef.picks[0])

	entered, _ := env.gateRender()
	lookID, jobID, _ := env.start(t, cookie, selfieID)
	select {
	case <-entered:
	case <-time.After(20 * time.Second):
		t.Fatal("the render never reached its first poll")
	}

	// The restart: a second runner over the same job store, the same
	// broker and the same kind, then the dead attempt lets go.
	rt := env.srv.looksOf()
	jobStore, err := jobsql.Open(t.Context(), jobsql.Config{DB: env.srv.db})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := job.Open(t.Context(), job.Config{
		Broker: rt.broker,
		Store:  jobStore,
		Kinds:  map[string]job.Kind{pipeline.KindName: lookKind(rt)},
		Log:    slog.New(slog.DiscardHandler),
	}); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if err := rt.jobs.Cancel(jobID); err != nil {
		t.Fatalf("cancel the dead attempt: %v", err)
	}

	// The held id reports the replacement while it runs, and names it.
	waitFor(t, "the replacement was named", func() bool {
		_, body := env.jobState(t, cookie, jobID)
		next, _ := body["next_id"].(string)
		return id.Valid(next) && next != jobID
	})
	finished := env.waitDone(t, cookie, jobID)
	if finished == jobID {
		t.Fatal("the held attempt finished; the replacement should have")
	}
	lookRec := env.get(t, cookie, "/api/looks/"+lookID)
	renderID := shownRenderID(t, bodyOf(t, lookRec))
	compare(t, "look", lookRec, env.renames(lookID, selfieID, renderID))

	// The replacement's state names itself as the job and the dead
	// attempt as the root. The fixture carries one id for both, so the
	// example is compared with the two named apart.
	stateRec := env.get(t, cookie, "/api/jobs/"+finished)
	if stateRec.Code != http.StatusOK {
		t.Fatalf("the replacement's state %d %s", stateRec.Code, stateRec.Body.String())
	}
	var want map[string]any
	if err := json.Unmarshal(fixture(t, "job-done").Body, &want); err != nil {
		t.Fatal(err)
	}
	want["job_id"] = finished
	want["root_id"] = jobID
	if got := bodyOf(t, stateRec); !reflect.DeepEqual(want, got) {
		t.Fatalf("job-done after a resume\n got %v\nwant %v", got, want)
	}

	if creates, _, _, _ := env.cam.count(); creates != 1 {
		t.Fatalf("creates %d, want the keyed task created once", creates)
	}
	if env.budget.Spent() != 1 || env.budget.Reserved() != 0 {
		t.Fatalf("spent %d reserved %d, want one settle and no hold", env.budget.Spent(), env.budget.Reserved())
	}
	if recalls, _ := env.mem.count(); recalls != 1 {
		t.Fatalf("recalls %d, want the snapshot's recall and no second read", recalls)
	}
	if n := env.clef.count("check"); n != 1 {
		t.Fatalf("check asked %d times, want once: the render resumed, it did not run twice", n)
	}
}

// TestTheSnapshotIsTheUsersAndGoesWithTheLook: the resume snapshot is
// the user's row, it carries no photograph, and the look's deletion
// cascade takes it with it.
func TestTheSnapshotIsTheUsersAndGoesWithTheLook(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, storyHits())
	user, cookie := bootUser(t, env.handler)
	selfieID := env.storySeeds(t, user)
	env.optIn(t, user, "family-function", "family-gold-jhumkas", "family-festive-top")
	env.history(t, user, true)
	lookID, jobID, _ := env.start(t, cookie, selfieID)
	env.waitDone(t, cookie, jobID)

	var owner, snapshot string
	if err := env.srv.db.Reader().QueryRowContext(t.Context(),
		`SELECT user_id, snapshot FROM look_snapshots WHERE root_id = ?`, lookID).Scan(&owner, &snapshot); err != nil {
		t.Fatalf("the snapshot: %v", err)
	}
	if owner != user {
		t.Fatalf("snapshot owner %q, want %q", owner, user)
	}
	if strings.Contains(snapshot, selfieID) == false {
		t.Fatalf("snapshot %s names no selfie id", snapshot)
	}
	// The selfie travels by id, never by bytes: nothing of the photograph
	// is in the row a resumed look rebuilds from.
	if strings.Contains(snapshot, "base64") || len(snapshot) > 8*1024 {
		t.Fatalf("snapshot carries %d bytes of payload, want ids and words only", len(snapshot))
	}
	if !strings.Contains(snapshot, "Last family function you kept the gold jhumkas and dismissed the red lip.") {
		t.Fatalf("snapshot %s carries no recall sentence", snapshot)
	}
	if _, err := env.srv.db.Writer().ExecContext(t.Context(), `DELETE FROM looks WHERE id = ?`, lookID); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := env.srv.db.Reader().QueryRowContext(t.Context(),
		`SELECT count(*) FROM look_snapshots WHERE root_id = ?`, lookID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("%d snapshots outlived the look", left)
	}
}

// TestBothMountsShareTheJobRoutes: the job read and the job stream are one
// pair of paths, and looks and deletion both answer them. Either mount
// may come first, the pair is registered once, and each job is served by
// the runtime that owns its kind - the look's stream through this file's
// rewrites, the deletion's through its own broker.
func TestBothMountsShareTheJobRoutes(t *testing.T) {
	t.Parallel()
	for _, order := range []string{"looks first", "deletion first"} {
		t.Run(order, func(t *testing.T) {
			t.Parallel()
			env, cfg := openLookEnv(t, earringAnswers(), 8, nil)
			switch order {
			case "looks first":
				if err := env.srv.MountLooks(t.Context(), cfg); err != nil {
					t.Fatalf("mount looks: %v", err)
				}
				env.mountDeletion(t)
			default:
				env.mountDeletion(t)
				if err := env.srv.MountLooks(t.Context(), cfg); err != nil {
					t.Fatalf("mount looks: %v", err)
				}
			}
			user, cookie := bootUser(t, env.handler)
			selfieID := env.storySeeds(t, user)
			env.optIn(t, user, "family-function", "family-gold-jhumkas")

			// A second photo, so the deletion erases nothing the look is
			// reading while it runs.
			spare := env.seed(t, user, "selfie", recordedImage(t, "earring-upload-ref"))
			env.blobs.add(spare, user)
			rec := do(t, env.handler, http.MethodPost, "/api/photos/"+spare+"/deletion", cookie, nil, nil)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("photo deletion %d %s", rec.Code, rec.Body.String())
			}
			var deleted struct {
				JobID string `json:"job_id"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &deleted); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the deletion finished", func() bool {
				body := bodyOf(t, env.get(t, cookie, "/api/jobs/"+deleted.JobID))
				return body["status"] == "done"
			})
			delState := bodyOf(t, env.get(t, cookie, "/api/jobs/"+deleted.JobID))
			// The photo deletion's last stage is the erase it finished.
			if delState["stage"] != "erase" || delState["job_id"] != deleted.JobID {
				t.Fatalf("deletion state %v, want the deletion runtime's own", delState)
			}
			// A late subscriber to a finished deletion gets the retained
			// terminal, from the deletion broker: the look runtime holds no
			// such topic.
			delFrames, _, _ := env.readFrames(t, cookie, deleted.JobID, nil)
			if len(delFrames) != 1 || delFrames[0].Name != "done" {
				t.Fatalf("deletion frames %v, want the retained terminal alone", delFrames)
			}

			release := env.park("judge")
			lookID, jobID, _ := env.start(t, cookie, selfieID)
			frames, _, _ := env.readFrames(t, cookie, jobID, release)
			env.waitDone(t, cookie, jobID)
			if last := frames[len(frames)-1].Name; last != "done" {
				t.Fatalf("the look's stream ended on %q", last)
			}
			// The shop frame carries the media join, which only the look
			// runtime's writer adds: the dispatch reached this file.
			shop := stageFrame(t, frames, "shop")
			products, _ := shop["products"].([]any)
			if len(products) == 0 {
				t.Fatalf("shop frame %v carries no products", shop)
			}
			entry := products[0].(map[string]any)
			if entry["media_id"] == "" || entry["url"] == "" {
				t.Fatalf("shop entry %v carries no media join", entry)
			}
			lookState := bodyOf(t, env.get(t, cookie, "/api/jobs/"+jobID))
			if lookState["root_id"] != lookID || lookState["status"] != "done" {
				t.Fatalf("look state %v", lookState)
			}
			// An id neither runtime owns is not_found, not a panic and not
			// the other runtime's answer.
			unknown := env.get(t, cookie, "/api/jobs/"+mustID(t))
			if unknown.Code != http.StatusNotFound {
				t.Fatalf("an unknown job answered %d", unknown.Code)
			}
		})
	}
}

// TestACancelledLookAnswersItsExample is the cancelled pair: a look stopped
// while it runs keeps its last published stage, and the stream carries the
// stages that ran and then the cancelled terminal.
func TestACancelledLookAnswersItsExample(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	user, cookie := bootUser(t, env.handler)
	selfieID := env.storySeeds(t, user)
	env.optIn(t, user, "family-function", "family-gold-jhumkas")
	holdJudge := env.park("judge")
	env.park("pick")
	lookID, jobID, _ := env.start(t, cookie, selfieID)

	// The subscription opens while the judge is held, so the judge frame
	// is read live; the pick is held so nothing else can publish between
	// that frame and the cancel.
	stop := func() {
		holdJudge()
		waitFor(t, "the judge finished", func() bool {
			return bodyOf(t, env.get(t, cookie, "/api/jobs/"+jobID))["stage"] == "judge"
		})
		if err := env.srv.looksOf().jobs.Cancel(jobID); err != nil {
			t.Errorf("cancel: %v", err)
		}
	}
	frames, header, status := env.readFrames(t, cookie, jobID, stop)
	r := env.renames(lookID, selfieID, "")
	compare(t, "job-cancelled", env.get(t, cookie, "/api/jobs/"+jobID), r)
	compareFrames(t, "job-events-cancelled", frames, header, status, r)
}

// TestAnInterruptedLookAnswersItsExample is the interrupted pair: the
// attempt a restart left behind keeps the interrupted terminal and its
// last published stage. The restart's kind is at its attempt limit, so no
// replacement is named - the state the example records. A live subscriber
// sees the terminal because the restart publishes on the same broker; a
// phone reconnecting after a real restart reads it as the retained
// terminal, the way the deletion stream above does.
func TestAnInterruptedLookAnswersItsExample(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	user, cookie := bootUser(t, env.handler)
	selfieID := env.storySeeds(t, user)
	env.optIn(t, user, "family-function", "family-gold-jhumkas")
	holdJudge := env.park("judge")
	env.park("pick")
	lookID, jobID, _ := env.start(t, cookie, selfieID)

	rt := env.srv.looksOf()
	die := func() {
		holdJudge()
		waitFor(t, "the judge finished", func() bool {
			return bodyOf(t, env.get(t, cookie, "/api/jobs/"+jobID))["stage"] == "judge"
		})
		jobStore, err := jobsql.Open(t.Context(), jobsql.Config{DB: env.srv.db})
		if err != nil {
			t.Errorf("job store: %v", err)
			return
		}
		spent := lookKind(rt)
		spent.MaxAttempts = 1
		kinds := map[string]job.Kind{pipeline.KindName: spent}
		if _, err := job.Open(t.Context(), job.Config{
			Broker: rt.broker,
			Store:  kindStore{Store: jobStore, kinds: kinds},
			Kinds:  kinds,
			Log:    slog.New(slog.DiscardHandler),
		}); err != nil {
			t.Errorf("restart: %v", err)
		}
	}
	frames, header, status := env.readFrames(t, cookie, jobID, die)
	r := env.renames(lookID, selfieID, "")
	compare(t, "job-interrupted", env.get(t, cookie, "/api/jobs/"+jobID), r)
	compareFrames(t, "job-events-interrupted", frames, header, status, r)
	// The dead attempt is still on its goroutine, held at the pick. Let it
	// go now the examples are read.
	if err := rt.jobs.Cancel(jobID); err != nil && !errors.Is(err, job.ErrUnknownJob) {
		t.Fatalf("cancel the dead attempt: %v", err)
	}
}

// TestARestartRecoversOnlyItsOwnKinds: the two mounts share one jobs
// table, and a runner's recovery interrupts every unfinished row it is
// shown but resumes only its own kinds. Shown the other mount's rows it
// would interrupt a job it cannot resume and leave it there, so each
// runner reads its own kinds' unfinished rows and no others.
func TestARestartRecoversOnlyItsOwnKinds(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	ctx := t.Context()
	seedRunning := func(kind string) string {
		t.Helper()
		jobID := mustID(t)
		if _, err := env.srv.db.Writer().ExecContext(ctx,
			`INSERT INTO jobs (id, kind, status, attempt, parent_id, root_id, progress, updated_at)
			 VALUES (?, ?, 'running', 1, '', ?, '', 1)`,
			jobID, kind, jobID); err != nil {
			t.Fatalf("seed %s: %v", kind, err)
		}
		return jobID
	}
	statusOf := func(jobID string) string {
		t.Helper()
		var status string
		if err := env.srv.db.Reader().QueryRowContext(ctx,
			`SELECT status FROM jobs WHERE id = ?`, jobID).Scan(&status); err != nil {
			t.Fatalf("status of %s: %v", jobID, err)
		}
		return status
	}
	restart := func(kinds map[string]job.Kind) {
		t.Helper()
		store, err := jobsql.Open(ctx, jobsql.Config{DB: env.srv.db})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := job.Open(ctx, job.Config{
			Broker: stream.New(stream.Config{}),
			Store:  kindStore{Store: store, kinds: kinds},
			Kinds:  kinds,
			Log:    slog.New(slog.DiscardHandler),
			Now:    time.Now,
		}); err != nil {
			t.Fatalf("restart: %v", err)
		}
	}

	look := seedRunning(pipeline.KindName)
	deletion := seedRunning(privacy.KindName)
	restart(map[string]job.Kind{pipeline.KindName: {}})
	if got := statusOf(look); got != "interrupted" {
		t.Fatalf("the look row is %s after its own restart, want interrupted", got)
	}
	if got := statusOf(deletion); got != "running" {
		t.Fatalf("the look restart left the deletion %s, want running: it cannot resume it", got)
	}

	look = seedRunning(pipeline.KindName)
	deletion = seedRunning(privacy.KindName)
	restart(map[string]job.Kind{privacy.KindName: {}})
	if got := statusOf(deletion); got != "interrupted" {
		t.Fatalf("the deletion row is %s after its own restart, want interrupted", got)
	}
	if got := statusOf(look); got != "running" {
		t.Fatalf("the deletion restart left the look %s, want running: it cannot resume it", got)
	}
}

// TestAStartThatCannotRecordTheLookSpendsNothing: the keep writes the look
// row and its snapshot together, and when it fails there is no look to
// read and none to resume. The job is cancelled so no paid step runs for
// it, and the seat it took is handed back, because the start delivered
// nothing.
func TestAStartThatCannotRecordTheLookSpendsNothing(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	user, cookie := bootUser(t, env.handler)
	selfieID := env.storySeeds(t, user)
	env.optIn(t, user, "family-function", "family-gold-jhumkas")
	release := env.park("judge")
	if _, err := env.srv.db.Writer().ExecContext(t.Context(), `DROP TABLE look_snapshots`); err != nil {
		t.Fatal(err)
	}

	body := fmt.Sprintf(`{"occasion":"family-function","selfie_id":%q}`, selfieID)
	rec := do(t, env.handler, http.MethodPost, "/api/looks", cookie, strings.NewReader(body), headerJSON)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("start %d %s, want the internal refusal", rec.Code, rec.Body.String())
	}
	if got := bodyOf(t, rec)["error"].(map[string]any)["code"]; got != "internal_error" {
		t.Fatalf("start error code %v", got)
	}
	release()

	var jobID, status string
	waitFor(t, "the unrecorded look was cancelled", func() bool {
		err := env.srv.db.Reader().QueryRowContext(t.Context(),
			`SELECT id, status FROM jobs WHERE kind = ?`, pipeline.KindName).Scan(&jobID, &status)
		return err == nil && status == "cancelled"
	})
	var looks, fresh, renders int
	if err := env.srv.db.Reader().QueryRowContext(t.Context(),
		`SELECT count(*) FROM looks WHERE user_id = ?`, user).Scan(&looks); err != nil {
		t.Fatal(err)
	}
	if looks != 0 {
		t.Fatalf("%d look rows landed for a start that refused", looks)
	}
	var session string
	if err := env.srv.db.Reader().QueryRowContext(t.Context(),
		`SELECT id FROM sessions WHERE user_id = ?`, user).Scan(&session); err != nil {
		t.Fatal(err)
	}
	if err := env.srv.db.Reader().QueryRowContext(t.Context(),
		`SELECT fresh_selfies, renders FROM judge_sessions WHERE session_id = ?`, session).Scan(&fresh, &renders); err != nil {
		t.Fatal(err)
	}
	if fresh != 0 || renders != 0 {
		t.Fatalf("the seat was not handed back: fresh %d renders %d", fresh, renders)
	}
	if creates, _, _, _ := env.cam.count(); creates != 0 {
		t.Fatalf("creates %d, want no paid task for a look that was never recorded", creates)
	}
	if env.budget.Spent() != 0 || env.budget.Reserved() != 0 {
		t.Fatalf("spent %d reserved %d, want nothing", env.budget.Spent(), env.budget.Reserved())
	}
}

// TestTheStreamRewriteRefusesAShortWrite: a rewritten frame is consumed
// whole, so the writer must report the whole frame or an error. A short
// write with no error would truncate the frame and still claim it landed.
func TestTheStreamRewriteRefusesAShortWrite(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	w := &lookStreamWriter{
		ResponseWriter: &shortWriter{header: http.Header{}},
		rt:             env.srv.looksOf(),
		ctx:            t.Context(),
	}
	frame := []byte("event: error\nid: 1\ndata: {\"status\":\"error\",\"job_id\":\"x\",\"error\":{\"code\":\"no_face_in_photo\",\"message\":\"not the phrase\"}}\n\n")
	n, err := w.Write(frame)
	if !errors.Is(err, io.ErrShortWrite) || n != 0 {
		t.Fatalf("a short rewrite answered %d, %v, want 0 and io.ErrShortWrite", n, err)
	}
	// A frame it does not rewrite passes through, and the underlying
	// writer's own answer is the answer.
	ping := []byte(": ping\n\n")
	n, err = w.Write(ping)
	if err != nil || n != len(ping)-1 {
		t.Fatalf("a comment answered %d, %v, want the underlying writer's count", n, err)
	}
}

func mustID(t *testing.T) string {
	t.Helper()
	value, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

type shortWriter struct {
	header http.Header
}

func (w *shortWriter) Header() http.Header { return w.header }
func (w *shortWriter) WriteHeader(int)     {}
func (w *shortWriter) Write(p []byte) (int, error) {
	return len(p) - 1, nil
}

// TestTheLookListAnswersABoundedPage is the page pin on GET /api/looks,
// the T4.10 amendment to the P1-frozen list: the answer is a page, newest
// first, bounded by the optional limit - default 20, maximum 100 - so a
// phone with a long history never reads it whole. The looks are seeded
// straight into the store the list reads, oldest first, no job rows: a
// look with no job answers the list as not ready, which is all a page pin
// needs (the item shape is the contract's and is compared member for
// member in the recorded-route tests). Every bound is asserted: the
// default answers exactly the newest 20; an explicit limit takes exactly
// that many, newest first; the maximum answers 100; a value above it
// clamps to 100 rather than answering what was asked; zero is a whole
// number of at least zero and answers the empty page; and a non-numeric
// or negative value is invalid_request naming the field, because a
// silently corrected page is a page the caller cannot reason about.
func TestTheLookListAnswersABoundedPage(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	user, cookie := bootUser(t, env.handler)

	const seeded = 110
	ids := make([]string, 0, seeded)
	for i := 0; i < seeded; i++ {
		lookID := mustID(t)
		ids = append(ids, lookID)
		if _, err := env.srv.db.Writer().ExecContext(t.Context(),
			`INSERT INTO looks (id, user_id, selfie_id, photo_hash, occasion, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			lookID, user, "selfie-seed", "hash-seed", "family-function", int64(1_700_000_000+i)); err != nil {
			t.Fatal(err)
		}
	}

	list := func(query string) []map[string]any {
		t.Helper()
		rec := env.get(t, cookie, "/api/looks?occasion=family-function"+query)
		if rec.Code != http.StatusOK {
			t.Fatalf("the list answered %d: %s", rec.Code, rec.Body.String())
		}
		raw, _ := bodyOf(t, rec)["looks"].([]any)
		items := make([]map[string]any, 0, len(raw))
		for _, one := range raw {
			items = append(items, one.(map[string]any))
		}
		return items
	}
	newestFirst := func(items []map[string]any, from int) {
		t.Helper()
		for i, item := range items {
			if item["id"] != ids[from-i] {
				t.Fatalf("item %d is %v, want %s: the page reads newest first", i, item["id"], ids[from-i])
			}
		}
	}

	items := list("")
	if len(items) != lookListDefaultLimit {
		t.Fatalf("the default answered %d looks, want %d", len(items), lookListDefaultLimit)
	}
	newestFirst(items, seeded-1)

	items = list("&limit=3")
	if len(items) != 3 {
		t.Fatalf("limit=3 answered %d looks, want 3", len(items))
	}
	newestFirst(items, seeded-1)

	if items := list("&limit=100"); len(items) != lookListMaxLimit {
		t.Fatalf("limit=100 answered %d looks, want %d", len(items), lookListMaxLimit)
	}
	// The clamp: the seeded account holds more than the maximum, and a
	// request above it still answers at most the maximum - the page a
	// client cannot reason about is the one that grows with its own ask.
	if items := list("&limit=250"); len(items) != lookListMaxLimit {
		t.Fatalf("limit=250 answered %d looks, want the clamp at %d", len(items), lookListMaxLimit)
	}
	if items := list("&limit=0"); len(items) != 0 {
		t.Fatalf("limit=0 answered %d looks, want the empty page", len(items))
	}

	for _, bad := range []string{"abc", "-3"} {
		rec := env.get(t, cookie, "/api/looks?occasion=family-function&limit="+bad)
		assertErr(t, rec, http.StatusBadRequest, codeInvalid, msgInvalid)
		var body struct {
			Error struct {
				Detail struct {
					Field string `json:"field"`
				} `json:"detail"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Error.Detail.Field != "limit" {
			t.Fatalf("limit=%s refused with detail.field %q, want limit", bad, body.Error.Detail.Field)
		}
	}
}
