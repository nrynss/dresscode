package pipeline

// The run tests prove the two done-when clauses at the level of the
// whole look. The stage events are pinned shape for shape against the
// recorded API fixtures (testdata/api/job-events.json and its
// alternates), the shop frame against the recorded one with the
// server's media join left out, the fallback paths are pinned event for
// event, and a look killed mid-render is restarted the way a restart
// really happens - a second job runner over the same store, whose open
// interrupts the dead attempt and resumes the kind - and finishes
// having created the one YouCam task.
//
// No test dials a provider and no unit moves outside the fake ledger.
// The renders and the analysis replay the recorded YouCam flows through
// the live client against a local wire that serves the recorded replies;
// Clef is a stub that answers the three calls the way the pipeline's
// own tests do, because no pick recording exists (docs/clef.md) and the
// run is the sequencer, not the reader.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/systemone"
	"github.com/nrynss/dresscode/internal/youcam"
	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/job"
	jobsql "github.com/nrynss/keel/job/sqlitestore"
	"github.com/nrynss/keel/mediastore"
	"github.com/nrynss/keel/providertask"
	"github.com/nrynss/keel/providertask/sqlitestore"
	"github.com/nrynss/keel/sqlite"
	"github.com/nrynss/keel/stream"
)

// runToken stands in for the API key. It is never a real key.
const runToken = "run-test-key-nothing-real"

// v04Selfie and its product are the recordings the earring story rides:
// the committed V04 sample and the gold jhumkas photo.
const (
	v04Selfie  = "earring-upload-src"
	v04Product = "earring-upload-ref"
	lipSelfie  = "lip-red-upload-src"
)

// runLine is the look-in-words line the stub's vocabulary answers join
// into: the vocabulary's own order, none and can't tell left out, the
// jhumkas option's display phrase embedding the colour winner. This is
// the fixtures' "gold jhumkas" - T3.5's flagged gap, closed by the
// phrase table the look now composes from.
const runLine = "gold jhumkas, red, mandarin, tied-back, black, flat, festive-top"

// lookStub answers the three Clef calls a look makes, in kind: the
// judge's noul probabilities and biggest issue, the pick's choice, and
// the check's gates and vocabulary. Every ask is captured, so a test
// can count the calls a look actually made.
type lookStub struct {
	mu     sync.Mutex
	judge  map[string]float64 // criterion id -> yes probability
	issue  string
	picks  []pickAnswer  // one per pick call, in order
	checks []checkAnswer // one per check call, in order
	look   map[string]string
	asks   []systemone.Request
}

type pickAnswer struct {
	choice string
	probs  map[string]float64
}

type checkAnswer struct{ change, same, met float64 }

func (s *lookStub) Ask(_ context.Context, req systemone.Request) (systemone.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asks = append(s.asks, req)
	answers := map[string]systemone.Answer{}
	noul := func(v float64) systemone.Answer {
		return systemone.Answer{Type: systemone.TypeNoul, Noul: &v}
	}
	switch {
	case hasQuestion(req, biggestIssueID):
		for id := range req.Questions {
			if id == biggestIssueID {
				continue
			}
			p, ok := s.judge[id]
			if !ok {
				p = 0.9
			}
			answers[id] = noul(p)
		}
		answers[biggestIssueID] = systemone.Answer{Type: systemone.TypeChoice, Choice: s.issue}
	case hasQuestion(req, pickQuestionID):
		if len(s.picks) == 0 {
			return systemone.Response{}, errors.New("lookStub: the pick answers ran dry")
		}
		a := s.picks[0]
		s.picks = s.picks[1:]
		answers[pickQuestionID] = systemone.Answer{Type: systemone.TypeChoice, Choice: a.choice, Probabilities: a.probs}
	case hasQuestion(req, changeMadeID):
		if len(s.checks) == 0 {
			return systemone.Response{}, errors.New("lookStub: the check answers ran dry")
		}
		a := s.checks[0]
		s.checks = s.checks[1:]
		answers[changeMadeID] = noul(a.change)
		answers[samePersonID] = noul(a.same)
		answers[criterionMetID] = noul(a.met)
		for id, v := range s.look {
			answers[lookIDPrefix+id] = systemone.Answer{Type: systemone.TypeChoice, Choice: v}
		}
	default:
		return systemone.Response{}, errors.New("lookStub: a request this stub does not know")
	}
	return systemone.Response{Model: systemone.ModelClef, Answers: answers}, nil
}

func hasQuestion(req systemone.Request, id string) bool {
	_, ok := req.Questions[id]
	return ok
}

// askCount counts the calls of one kind the stub saw.
func (s *lookStub) askCount(t *testing.T, markerID string) int {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, req := range s.asks {
		if hasQuestion(req, markerID) {
			n++
		}
	}
	return n
}

// lastPickState decodes the latest pick request's state, so a test reads
// the attributes the run carried into the pick.
func (s *lookStub) lastPickState(t *testing.T) pickState {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.asks) - 1; i >= 0; i-- {
		if hasQuestion(s.asks[i], pickQuestionID) {
			var state pickState
			if err := json.Unmarshal(s.asks[i].State, &state); err != nil {
				t.Fatalf("pick state: %v", err)
			}
			return state
		}
	}
	t.Fatal("the stub saw no pick request")
	return pickState{}
}

// eventLog collects the progress events a run published, in order.
type eventLog struct {
	mu  sync.Mutex
	got []job.Progress
}

func (l *eventLog) publish(p job.Progress) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.got = append(l.got, p)
}

func (l *eventLog) all() []job.Progress {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]job.Progress(nil), l.got...)
}

// detail decodes one event's detail payload.
func detail(t *testing.T, p job.Progress) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(p.Detail, &m); err != nil {
		t.Fatalf("%s event detail: %v", p.Stage, err)
	}
	return m
}

// runBackend is a mediastore backend that keeps the bytes it is given,
// so a test can read a kept render back the way the check does.
type runBackend struct {
	mu    sync.Mutex
	blobs map[string][]byte
}

func (b *runBackend) Open(_ context.Context, id string) (mediastore.BlobReader, error) {
	b.mu.Lock()
	data, ok := b.blobs[id]
	b.mu.Unlock()
	if !ok {
		return nil, mediastore.ErrNotFound
	}
	return nopCloserReader{bytes.NewReader(data)}, nil
}

func (b *runBackend) Write(_ context.Context, id string, src io.Reader) (int64, error) {
	data, err := io.ReadAll(src)
	if err != nil {
		return 0, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.blobs[id]; ok {
		// The write is refused for an id the backend already holds, the
		// store's own contract: a repeat of a kept render must never
		// touch the bytes that stand.
		return 0, mediastore.ErrAlreadyExists
	}
	b.blobs[id] = data
	return int64(len(data)), nil
}

func (b *runBackend) Delete(_ context.Context, id string) error {
	b.mu.Lock()
	delete(b.blobs, id)
	b.mu.Unlock()
	return nil
}

func (b *runBackend) List(_ context.Context) ([]mediastore.Object, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]mediastore.Object, 0, len(b.blobs))
	for id, data := range b.blobs {
		out = append(out, mediastore.Object{ID: id, SizeBytes: int64(len(data))})
	}
	return out, nil
}

// count is how many pictures the backend holds.
func (b *runBackend) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.blobs)
}

// runPictures resolves the ids a look names: the kept selfie, a brought
// product, a catalog product, and a kept render, which it reads out of
// the mediastore the runner wrote.
type runPictures struct {
	mu    sync.Mutex
	named map[string]systemone.Image
	media *mediastore.Store
}

func (p *runPictures) Picture(ctx context.Context, _, id string) (systemone.Image, error) {
	p.mu.Lock()
	img, ok := p.named[id]
	p.mu.Unlock()
	if ok {
		return img, nil
	}
	rc, blob, err := p.media.Open(ctx, id)
	if err != nil {
		return systemone.Image{}, fmt.Errorf("picture %s: %w", id, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return systemone.Image{}, err
	}
	return systemone.Image{ContentType: blob.ContentType, Bytes: data}, nil
}

// memSnaps is the snapshot store in memory, keyed by root id, the shape
// the wiring implements over the look row it wrote.
type memSnaps struct {
	mu   sync.Mutex
	kept map[string]Snapshot
}

func newMemSnaps() *memSnaps {
	return &memSnaps{kept: map[string]Snapshot{}}
}

func (s *memSnaps) Keep(_ context.Context, root string, snap Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kept[root] = snap
	return nil
}

func (s *memSnaps) Snapshot(_ context.Context, root string) (Snapshot, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.kept[root]
	return snap, ok, nil
}

// runRig is one assembled look: the wire that serves the recorded YouCam
// replies, the runner over it, a mediastore the test can read, the cache,
// the Clef stub, and the event log. Nothing dials and no unit moves
// outside the rig's own budget.
type runRig struct {
	wire   *wireReplay
	client *youcam.Client
	runner *youcam.Runner
	budget *cost.Budget
	ledger *cost.Ledger
	media  *mediastore.Store
	blobs  *runBackend
	pics   *runPictures
	cache  *memCache
	clef   *lookStub
	events *eventLog
}

// newRunRig assembles the rig with a unit budget of units and a queue of
// canned upload slots, one per upload the run will make, in order. The
// slots hand out the ids the recorded creates name, so every built body
// is the recorded body.
func newRunRig(t *testing.T, rep *youcam.Replay, units cost.Price, slots ...string) *runRig {
	t.Helper()
	wire := newWireReplay(t, rep)
	for _, id := range slots {
		wire.enqueue(youcam.Slot{FileID: id})
	}
	client, err := youcam.NewClient(youcam.ClientConfig{
		BaseURL:  wire.base,
		Token:    runToken,
		Throttle: throttlePace(),
	})
	if err != nil {
		t.Fatal(err)
	}
	budget, err := cost.NewBudgetIn(youcam.UnitsDenomination, units)
	if err != nil {
		t.Fatal(err)
	}
	ledger := cost.NewLedger()
	meter, err := cost.NewMeter(budget, ledger)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(t.Context(), sqlite.Config{
		Path:   filepath.Join(t.TempDir(), "run-tasks.db"),
		Logger: quietLog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tasks, err := sqlitestore.Open(t.Context(), sqlitestore.Config{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	blobs := &runBackend{blobs: map[string][]byte{}}
	media, err := mediastore.Open(t.Context(), mediastore.Config{
		Index:   &memMediaIndex{rows: map[string]mediastore.Blob{}},
		Backend: blobs,
		Log:     quietLog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := youcam.NewRunner(newResultReplay(client, wire.base, rep), youcam.RunnerConfig{
		Store: tasks,
		Meter: meter,
		Media: media,
		Config: providertask.Config{
			PollBase:    time.Millisecond,
			PollCeiling: 2 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	pics := &runPictures{named: map[string]systemone.Image{}, media: media}
	return &runRig{
		wire:   wire,
		client: client,
		runner: runner,
		budget: budget,
		ledger: ledger,
		media:  media,
		blobs:  blobs,
		pics:   pics,
		cache:  newMemCache(),
		clef:   &lookStub{},
		events: &eventLog{},
	}
}

// fetchKeys maps the YouCam task a poll names onto the typed fetch
// recording that keeps its result picture.
var fetchKeys = map[string]string{
	"2d-vto/earring":  "earring-fetch",
	"2d-vto/necklace": "necklace-auto-fetch",
	"cloth-v4":        "cloth-fetch",
	"makeup-vto":      "lip-red-fetch",
	"hair-transfer":   "hair-updo-fetch",
	"beard-style":     "beard-fetch",
	"ai-smile":        "smile-fetch",
}

// resultReplay is the signed-URL leg of the wire. A recorded result link
// is a live S3 URL signed for two hours and recorded with its signature
// cut, so the harness cannot answer it where it stands: docs/youcam.md
// keeps the two signed-URL legs (PUT upload, GET result) out of the wire
// recordings for exactly that reason. The PUT leg the slot reply already
// points back here; this wrapper rewrites the poll's result link onto
// the harness and serves the fetch from the typed fetch recording. The
// picture that lands in mediastore is the recorded picture.
type resultReplay struct {
	youcam.YouCam

	base string
	rep  *youcam.Replay
}

func newResultReplay(inner youcam.YouCam, base string, rep *youcam.Replay) *resultReplay {
	return &resultReplay{YouCam: inner, base: base, rep: rep}
}

func (r *resultReplay) Poll(ctx context.Context, ref youcam.PollRef) (youcam.Status, error) {
	st, err := r.YouCam.Poll(ctx, ref)
	if err != nil {
		return st, err
	}
	if st.ResultURL != "" {
		if key, ok := fetchKeys[ref.Task]; ok {
			st.ResultURL = r.base + "/result/" + key
		}
	}
	return st, nil
}

func (r *resultReplay) Fetch(ctx context.Context, ref youcam.ResultRef) (youcam.Result, error) {
	if key, ok := strings.CutPrefix(ref.URL, r.base+"/result/"); ok {
		rec, err := r.rep.Reply(key)
		if err != nil {
			return youcam.Result{}, err
		}
		var res youcam.Result
		if err := json.Unmarshal(rec.Response, &res); err != nil {
			return youcam.Result{}, err
		}
		return youcam.Result{TaskID: ref.TaskID, Bytes: res.Bytes}, nil
	}
	return r.YouCam.Fetch(ctx, ref)
}

// recordedSlotID is the file id one recorded upload answered with. The
// recorded create names it, so the run's slot replies must hand it out
// for the built body to be the recorded body.
func recordedSlotID(t *testing.T, rep *youcam.Replay, key string) string {
	t.Helper()
	rec, err := rep.Reply(key)
	if err != nil {
		t.Fatal(err)
	}
	var slot youcam.Slot
	if err := json.Unmarshal(rec.Response, &slot); err != nil {
		t.Fatalf("%s is not an upload reply: %v", key, err)
	}
	if slot.FileID == "" {
		t.Fatalf("%s names no file id", key)
	}
	return slot.FileID
}

// earringStory is the family-function look the fixtures record: the
// V04 selfie, the jhumkas in the basket, the jhumkas fix picked first
// and the red lip behind it.
type earringStory struct {
	rig    *runRig
	occ    *domain.Occasion
	photo  systemone.Image
	req    Request
	deps   Deps
	occSrc occasionSource
}

type occasionSource map[string]*domain.Occasion

func (s occasionSource) resolve(id string) (*domain.Occasion, bool) {
	occ, ok := s[id]
	return occ, ok
}

// newEarringStory builds the rig and the request for the earring look.
// The check answers and the pick answers are the caller's, because each
// test tells its own story.
func newEarringStory(t *testing.T, rep *youcam.Replay, units cost.Price, checks []checkAnswer, recall *Recall, slotIDs ...string) *earringStory {
	t.Helper()
	occasions := loadOccasions(t)
	occ := occasions["family-function"]
	rig := newRunRig(t, rep, units, slotIDs...)
	photo := recordedImage(t, rep, v04Selfie)
	product := recordedImage(t, rep, v04Product)
	story := &earringStory{
		rig:    rig,
		occ:    occ,
		photo:  photo,
		occSrc: occasionSource(occasions),
	}
	story.req = Request{
		Photo:    Photo{User: "user-1", Hash: "hash-v04", Image: photo},
		Selfie:   "selfie-1",
		Occasion: "family-function",
		OptIn:    []string{"family-gold-jhumkas", "family-red-lip"},
		Recall:   recall,
		Basket:   &Product{ID: "gold-jhumkas", Image: product},
	}
	story.deps = Deps{
		Clef:          rig.clef,
		Runner:        rig.runner,
		Cache:         rig.cache,
		Vocab:         loadVocabulary(t),
		Thresholds:    Thresholds{Met: readingAt},
		Lines:         checkReading,
		CheckModel:    systemone.ModelClef,
		Occasions:     story.occSrc.resolve,
		Pictures:      rig.pics,
		Catalog:       loadCatalog(t),
		ProductRecord: noIntakeRecords,
	}
	rig.pics.named["selfie-1"] = photo
	rig.clef.judge = map[string]float64{
		"family-earring-festive": 0.03,
		"family-lip-colour":      0.2,
	}
	rig.clef.issue = "family-earring-festive"
	rig.clef.look = map[string]string{
		"jewellery":        "jhumkas",
		"jewellery_colour": "gold",
		"necklace":         "none",
		"necklace_colour":  "none",
		"lip_colour":       "red",
		"collar":           "mandarin",
		"hair":             "tied-back",
		"hair_colour":      "black",
		"beard":            "none",
		"expression":       "flat",
		"garment":          "festive-top",
	}
	rig.clef.checks = checks
	return story
}

// contractFrame is one frame of a recorded stream: its event name and
// its data payload.
type contractFrame struct {
	name string
	data map[string]any
}

// readContract parses one recorded stream fixture into its frames.
func readContract(t *testing.T, name string) []contractFrame {
	t.Helper()
	path := filepath.Join(repoRoot(t), "testdata", "api", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var doc struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var frames []contractFrame
	for _, block := range strings.Split(doc.Body, "\n\n") {
		var (
			event string
			line  string
		)
		for _, field := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(field, "event: "):
				event = strings.TrimPrefix(field, "event: ")
			case strings.HasPrefix(field, "data: "):
				line = strings.TrimPrefix(field, "data: ")
			}
		}
		if event == "" || line == "" {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(line), &data); err != nil {
			t.Fatalf("%s: %s frame: %v", name, event, err)
		}
		frames = append(frames, contractFrame{name: event, data: data})
	}
	return frames
}

// frameOf returns the contract's first progress frame of a stage.
func frameOf(t *testing.T, frames []contractFrame, stage string) map[string]any {
	t.Helper()
	for _, f := range frames {
		if f.name == "progress" && f.data["stage"] == stage {
			sub, ok := f.data["detail"].(map[string]any)
			if !ok {
				t.Fatalf("%s frame carries no detail object", stage)
			}
			return sub
		}
	}
	t.Fatalf("the contract names no %s stage", stage)
	return nil
}

// assertShape pins one published detail against the contract frame: the
// same keys, and the same value kinds where the contract speaks booleans.
func assertShape(t *testing.T, stage string, want map[string]any, got map[string]any) {
	t.Helper()
	wantKeys := make([]string, 0, len(want))
	for k := range want {
		wantKeys = append(wantKeys, k)
	}
	gotKeys := make([]string, 0, len(got))
	for k := range got {
		gotKeys = append(gotKeys, k)
	}
	if !reflect.DeepEqual(sorted(wantKeys), sorted(gotKeys)) {
		t.Fatalf("%s detail carries %v, want the contract's %v", stage, sorted(gotKeys), sorted(wantKeys))
	}
	for _, k := range wantKeys {
		wv, gv := want[k], got[k]
		if wb, ok := wv.(bool); ok {
			gb, ok := gv.(bool)
			if !ok || gb != wb {
				t.Fatalf("%s detail %s is %v, want the contract's %v", stage, k, gv, wb)
			}
		}
	}
}

// sorted returns the slice sorted, for a stable comparison.
func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// noIntakeRecords is the ProductRecord a story with nothing brought
// carries. The wiring reads an intake record only when the fix being
// shopped composites product photos and the user brought an item, so a
// run that reaches for one here is a wiring bug, and the read fails the
// look loudly - the silent drop the review closed runs both ways.
func noIntakeRecords(_ context.Context, _, id string) (*domain.Product, error) {
	return nil, fmt.Errorf("noIntakeRecords: an intake record was read for %q", id)
}

// assertShopFrame pins one published shop event against a recorded
// fixture's shop frame: the same single products key, and each entry the
// recorded entry minus the server's media join. The contract's media_id
// and url on a shop entry are the server's - no seam here names a
// product photo's media id, and the wiring does not invent one - so the
// frame carries Shop's entries as they are and the server enriches it
// when it serves the stream (T4.4's wiring). The keys and values the
// pipeline owns are pinned exactly against the recording. The decoded
// entries come back for the caller's own reads.
func assertShopFrame(t *testing.T, fixtureName string, p job.Progress) []domain.Product {
	t.Helper()
	fixture := frameOf(t, readContract(t, fixtureName), stageShop)
	assertShape(t, stageShop, fixture, detail(t, p))
	var got shopEvent
	if err := json.Unmarshal(p.Detail, &got); err != nil {
		t.Fatalf("%s event detail: %v", stageShop, err)
	}
	recorded, ok := fixture["products"].([]any)
	if !ok {
		t.Fatalf("the %s frame carries no products array", stageShop)
	}
	want := make([]map[string]any, 0, len(recorded))
	for i, entry := range recorded {
		m, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("the %s frame's product %d is not an object", stageShop, i)
		}
		delete(m, "media_id")
		delete(m, "url")
		want = append(want, m)
	}
	raw, err := json.Marshal(got.Products)
	if err != nil {
		t.Fatal(err)
	}
	var shaped []map[string]any
	if err := json.Unmarshal(raw, &shaped); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, shaped) {
		t.Fatalf("%s products %v, want the recorded frame's %v without the server's media join", stageShop, shaped, want)
	}
	return got.Products
}

// stagesOf reads the stage names off a run's events, in order.
func stagesOf(t *testing.T, events *eventLog) []string {
	t.Helper()
	var out []string
	for _, p := range events.all() {
		if p.Stage == "" {
			t.Fatal("an event carried no stage")
		}
		out = append(out, p.Stage)
	}
	return out
}

// TestTheLookRunsTheContractStages is the happy story, pinned against
// the recorded stream: judge, pick, measure, render, check, shop, each
// detail the contract's shape - the flags worded as the data words them,
// the two fix ids, the skipped measure, the render attempt, and the
// shown render with its media id and the look in words, then the shop
// frame with the shown fix's product. The books carry the one render
// settle, and the look comes back shown.
func TestTheLookRunsTheContractStages(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	slots := []string{
		recordedSlotID(t, rep, v04Selfie),
		recordedSlotID(t, rep, v04Product),
	}
	shown := checkAnswer{change: 0.99, same: 0.99, met: 0.99}
	story := newEarringStory(t, rep, 1, []checkAnswer{shown}, nil, slots...)
	story.rig.clef.picks = []pickAnswer{{
		choice: "family-gold-jhumkas",
		probs:  map[string]float64{"family-red-lip": 0.4},
	}}

	look, err := Run(t.Context(), story.deps, story.req, story.rig.events.publish)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// The stage order is the contract's order, shop last.
	got := stagesOf(t, story.rig.events)
	want := []string{stageJudge, stagePick, stageMeasure, stageRender, stageCheck, stageShop}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stages %v, want %v", got, want)
	}
	events := story.rig.events.all()

	// judge: the flags carry the data's own suggestion, in order.
	judgeFixture := frameOf(t, readContract(t, "job-events.json"), stageJudge)
	assertShape(t, stageJudge, judgeFixture, detail(t, events[0]))
	var judgeGot judgeEvent
	if err := json.Unmarshal(events[0].Detail, &judgeGot); err != nil {
		t.Fatal(err)
	}
	if len(judgeGot.Flags) != 2 {
		t.Fatalf("%d flags, want the two misses", len(judgeGot.Flags))
	}
	for i, id := range []string{"family-earring-festive", "family-lip-colour"} {
		if judgeGot.Flags[i].ID != id || judgeGot.Flags[i].Suggestion != occasionCriterion(t, story.occ, id).Suggestion {
			t.Fatalf("flag %d is %+v, want %s worded as the data words it", i, judgeGot.Flags[i], id)
		}
	}

	// pick: the two fix ids, no recall where history is off. The shape
	// is the suggestion stream's pick, which carries history off too.
	pickFixture := frameOf(t, readContract(t, "job-events-suggestion.json"), stagePick)
	assertShape(t, stagePick, pickFixture, detail(t, events[1]))
	var pickGot pickEvent
	if err := json.Unmarshal(events[1].Detail, &pickGot); err != nil {
		t.Fatal(err)
	}
	if pickGot.Fix != "family-gold-jhumkas" || pickGot.RunnerUp != "family-red-lip" || pickGot.Recall != "" {
		t.Fatalf("pick %+v, want the jhumkas fix and the lip behind it", pickGot)
	}

	// measure: the fix names no analysis, so the event is the contract's
	// skipped frame, byte for byte.
	measureFixture := frameOf(t, readContract(t, "job-events.json"), stageMeasure)
	assertShape(t, stageMeasure, measureFixture, detail(t, events[2]))
	if string(events[2].Detail) != `{"skipped":true}` {
		t.Fatalf("measure detail %s, want the contract's skipped frame", events[2].Detail)
	}

	// render: the fix and the attempt, value for value the contract's.
	renderFixture := frameOf(t, readContract(t, "job-events.json"), stageRender)
	assertShape(t, stageRender, renderFixture, detail(t, events[3]))
	if string(events[3].Detail) != `{"fix":"family-gold-jhumkas","attempt":"fix"}` {
		t.Fatalf("render detail %s, want the contract's frame", events[3].Detail)
	}

	// check: the shown render carries its media id and the look in
	// words; a discarded render is never given a media id.
	checkFixture := frameOf(t, readContract(t, "job-events.json"), stageCheck)
	assertShape(t, stageCheck, checkFixture, detail(t, events[4]))
	var checkGot checkEvent
	if err := json.Unmarshal(events[4].Detail, &checkGot); err != nil {
		t.Fatal(err)
	}
	if !checkGot.Shown || checkGot.Fix != "family-gold-jhumkas" {
		t.Fatalf("check %+v, want the jhumkas render shown", checkGot)
	}

	// The look: shown, with the check carried and the words joined in
	// the vocabulary's order.
	if look.SuggestionOnly || look.Shown == nil {
		t.Fatalf("look %+v, want a shown render", look)
	}
	if look.Shown.FixID != "family-gold-jhumkas" || look.Shown.Check == nil {
		t.Fatalf("shown render %+v, want the jhumkas fix with its check", look.Shown)
	}
	if !look.Shown.Check.ChangeMade || !look.Shown.Check.SamePerson || !look.Shown.Check.CriterionMet {
		t.Fatalf("check facts %+v, want all three", look.Shown.Check)
	}
	if look.Words != runLine {
		t.Fatalf("words %q, want %q", look.Words, runLine)
	}
	if checkGot.MediaID != look.Shown.MediaID {
		t.Fatalf("the check event named media %q, want the shown render's %q", checkGot.MediaID, look.Shown.MediaID)
	}
	if len(look.Shown.MediaID) != 32 {
		t.Fatalf("media id %q is not an id-shaped name", look.Shown.MediaID)
	}

	// shop: the recorded frame 6, the entries the pipeline owns of it -
	// the shown fix's product, exact, the catalog's. The which-fix-shops
	// rule's shown arm: the jhumkas render is what is on the face, and
	// the jhumkas are what shops.
	products := assertShopFrame(t, "job-events.json", events[5])
	if len(products) != 1 || products[0].ID != "gold-jhumkas" || products[0].Fidelity != domain.ExactProduct {
		t.Fatalf("shop %+v, want the shown fix's gold-jhumkas exact", products)
	}
	if !reflect.DeepEqual(look.Products, products) {
		t.Fatalf("look products %+v, want the shop frame's %+v", look.Products, products)
	}

	// The books: one render, settled once, against the render key.
	if story.rig.budget.Spent() != 1 || story.rig.budget.Reserved() != 0 {
		t.Fatalf("budget spent %d reserved %d, want the one-unit render settled", story.rig.budget.Spent(), story.rig.budget.Reserved())
	}
	charges := story.rig.ledger.Charges()
	if len(charges) != 1 || charges[0].Ref != "render/user-1/hash-v04/family-gold-jhumkas/gold-jhumkas" {
		t.Fatalf("charges %+v, want the one settle on the render key", charges)
	}

	// The kept render is the recorded picture; the check read those
	// bytes back out of the store to send them.
	kept, err := story.rig.pics.Picture(t.Context(), "user-1", look.Shown.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	if want := recordedRenderBytes(t, rep, "earring-fetch"); !reflect.DeepEqual(kept.Bytes, want) {
		t.Fatalf("the check read %d bytes, want the kept render's %d", len(kept.Bytes), len(want))
	}

	// The model calls: one judge, one pick, one check. No analysis ran.
	if n := story.rig.clef.askCount(t, biggestIssueID); n != 1 {
		t.Fatalf("%d judge calls, want one", n)
	}
	if n := story.rig.clef.askCount(t, pickQuestionID); n != 1 {
		t.Fatalf("%d pick calls, want one", n)
	}
	if n := story.rig.clef.askCount(t, changeMadeID); n != 1 {
		t.Fatalf("%d check calls, want one", n)
	}
	if story.rig.cache.count() != 0 {
		t.Fatalf("%d cache rows, want none: the fix names no analysis", story.rig.cache.count())
	}
}

// TestThePickCarriesTheRecallWhenHistoryIsOn pins the pick event's
// history arm: the memory sentence rides the pick event only when the
// look was given a memory, word for word.
func TestThePickCarriesTheRecallWhenHistoryIsOn(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	slots := []string{
		recordedSlotID(t, rep, v04Selfie),
		recordedSlotID(t, rep, v04Product),
	}
	const line = "Last family function you kept the gold jhumkas and dismissed the red lip."
	shown := checkAnswer{change: 0.99, same: 0.99, met: 0.99}
	story := newEarringStory(t, rep, 1, []checkAnswer{shown}, &Recall{
		Line:      line,
		Kept:      []string{"family-gold-jhumkas"},
		Dismissed: []string{"family-red-lip"},
	}, slots...)
	story.rig.clef.picks = []pickAnswer{{
		choice: "family-gold-jhumkas",
		probs:  map[string]float64{"family-red-lip": 0.4},
	}}

	if _, err := Run(t.Context(), story.deps, story.req, story.rig.events.publish); err != nil {
		t.Fatalf("run: %v", err)
	}
	events := story.rig.events.all()

	// With history on, the pick detail's keys are the contract's recall
	// arm: fix, runner_up, recall.
	pickFixture := frameOf(t, readContract(t, "job-events.json"), stagePick)
	assertShape(t, stagePick, pickFixture, detail(t, events[1]))
	var pickGot pickEvent
	if err := json.Unmarshal(events[1].Detail, &pickGot); err != nil {
		t.Fatal(err)
	}
	if pickGot.Recall != line {
		t.Fatalf("recall %q, want the memory sentence word for word", pickGot.Recall)
	}

	// The pick request carried the memory too, so the model knew the
	// preference in its own words.
	state := story.rig.clef.lastPickState(t)
	if state.Recall == nil || state.Recall.Line != line {
		t.Fatalf("pick state recall %+v, want the sentence", state.Recall)
	}
}

// TestBothChecksFailAndTheSuggestionStands is the fallback's last arm:
// the first check discards, the runner-up renders once and is checked
// once, and that discards too. The stream then reads exactly like the
// recorded suggestion story - shop included, which names the picked
// fix's product, nothing being on the face - the look carries no render,
// and each of the two renders settled its own quote on its own key.
func TestBothChecksFailAndTheSuggestionStands(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	slots := []string{
		recordedSlotID(t, rep, v04Selfie),
		recordedSlotID(t, rep, v04Product),
		recordedSlotID(t, rep, lipSelfie),
	}
	discard := checkAnswer{change: 0.0001, same: 0.99, met: 0.99}
	story := newEarringStory(t, rep, 2, []checkAnswer{discard, discard}, nil, slots...)
	story.rig.clef.picks = []pickAnswer{{
		choice: "family-gold-jhumkas",
		probs:  map[string]float64{"family-red-lip": 0.4},
	}}

	look, err := Run(t.Context(), story.deps, story.req, story.rig.events.publish)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// The stage order is the contract's fallback order: the render and
	// check stages once more for the runner-up, then shop, which runs
	// after whatever the chain showed.
	got := stagesOf(t, story.rig.events)
	want := []string{stageJudge, stagePick, stageMeasure, stageRender, stageCheck, stageRender, stageCheck, stageShop}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stages %v, want %v", got, want)
	}

	// Every frame matches the recorded suggestion stream, which is this
	// story with the fixtures' runner-up.
	frames := readContract(t, "job-events-suggestion.json")
	wantFrames := []string{stageJudge, stagePick, stageMeasure, stageRender, stageCheck, stageRender, stageCheck}
	for i, stage := range wantFrames {
		assertShape(t, stage, frameOf(t, frames, stage), detail(t, story.rig.events.all()[i]))
	}
	var attempts [2]renderEvent
	for i := range attempts {
		if err := json.Unmarshal(story.rig.events.all()[3+i*2].Detail, &attempts[i]); err != nil {
			t.Fatal(err)
		}
	}
	if attempts[0].Fix != "family-gold-jhumkas" || attempts[0].Attempt != attemptFix {
		t.Fatalf("first render event %+v, want the fix attempt", attempts[0])
	}
	if attempts[1].Fix != "family-red-lip" || attempts[1].Attempt != attemptRunnerUp {
		t.Fatalf("second render event %+v, want the runner-up attempt", attempts[1])
	}
	var checks [2]checkEvent
	for i := range checks {
		if err := json.Unmarshal(story.rig.events.all()[4+i*2].Detail, &checks[i]); err != nil {
			t.Fatal(err)
		}
		if checks[i].Shown || checks[i].MediaID != "" || checks[i].Words != "" {
			t.Fatalf("check event %+v, want a discarded render with no media id and no words", checks[i])
		}
	}
	if checks[0].Fix != "family-gold-jhumkas" || checks[1].Fix != "family-red-lip" {
		t.Fatalf("check fixes %q and %q, want each render checked against its own fix", checks[0].Fix, checks[1].Fix)
	}

	// shop: the recorded suggestion stream's own frame. Nothing was
	// shown, so the picked fix shops - the fixture names gold-jhumkas,
	// not the runner-up's top-festive - the which-fix-shops rule's word
	// in the data's own voice.
	products := assertShopFrame(t, "job-events-suggestion.json", story.rig.events.all()[7])
	if len(products) != 1 || products[0].ID != "gold-jhumkas" {
		t.Fatalf("shop %+v, want the picked fix's gold-jhumkas", products)
	}
	if !reflect.DeepEqual(look.Products, products) {
		t.Fatalf("look products %+v, want the shop frame's %+v", look.Products, products)
	}

	// The look stands on its suggestions alone.
	if !look.SuggestionOnly || look.Shown != nil || look.Words != "" {
		t.Fatalf("look %+v, want the suggestion-only look", look)
	}
	if look.FixID != "family-gold-jhumkas" || look.RunnerUpID != "family-red-lip" {
		t.Fatalf("look fixes %q and %q, want the pick's two", look.FixID, look.RunnerUpID)
	}

	// The two renders each settled their own quote on their own key: the
	// runner-up is a different fix and a different task at the provider.
	if story.rig.budget.Spent() != 2 || story.rig.budget.Reserved() != 0 {
		t.Fatalf("budget spent %d reserved %d, want one settle per render", story.rig.budget.Spent(), story.rig.budget.Reserved())
	}
	charges := story.rig.ledger.Charges()
	if len(charges) != 2 {
		t.Fatalf("charges %+v, want two settles", charges)
	}
	if charges[0].Ref != "render/user-1/hash-v04/family-gold-jhumkas/gold-jhumkas" {
		t.Fatalf("first charge on %q, want the fix's own key", charges[0].Ref)
	}
	if charges[1].Ref != "render/user-1/hash-v04/family-red-lip" {
		t.Fatalf("second charge on %q, want the runner-up's own key", charges[1].Ref)
	}
	if charges[0].UnitPrice != 1 || charges[1].UnitPrice != 1 {
		t.Fatalf("quotes %d and %d, want one rate-card line per render", charges[0].UnitPrice, charges[1].UnitPrice)
	}
}

// TestTheRunnerUpIsShownWhenItPasses is the fallback's middle arm: the
// first check discards, the runner-up's check shows, and the look then
// talks about the runner-up's render while keeping the first pick as the
// fix. It is also the which-fix-shops rule's shown arm, pinned: the shop
// frame names the runner-up's fix - the lip is what is on the face - and
// the user's brought item stays off the lip's list, its intake record
// unread, the lip compositing no product photo.
func TestTheRunnerUpIsShownWhenItPasses(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	slots := []string{
		recordedSlotID(t, rep, v04Selfie),
		recordedSlotID(t, rep, v04Product),
		recordedSlotID(t, rep, lipSelfie),
	}
	discard := checkAnswer{change: 0.0001, same: 0.99, met: 0.99}
	shown := checkAnswer{change: 0.99, same: 0.99, met: 0.99}
	story := newEarringStory(t, rep, 2, []checkAnswer{discard, shown}, nil, slots...)
	story.rig.clef.picks = []pickAnswer{{
		choice: "family-gold-jhumkas",
		probs:  map[string]float64{"family-red-lip": 0.4},
	}}
	// Something brought: the fix's render composites it, but the fix's
	// render is discarded and the shown runner-up is a paint flow, so
	// the shop stage never shops for it. The story's ProductRecord
	// fails the look if the wiring reads one anyway.
	story.req.Brought = &Product{ID: "b20d0c70000000000000000000000001", Image: recordedImage(t, rep, v04Product)}

	look, err := Run(t.Context(), story.deps, story.req, story.rig.events.publish)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	events := story.rig.events.all()
	if got, want := stagesOf(t, story.rig.events), []string{stageJudge, stagePick, stageMeasure, stageRender, stageCheck, stageRender, stageCheck, stageShop}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stages %v, want %v", got, want)
	}

	var checks [2]checkEvent
	for i := range checks {
		if err := json.Unmarshal(events[4+i*2].Detail, &checks[i]); err != nil {
			t.Fatal(err)
		}
	}
	if checks[0].Shown {
		t.Fatalf("first check %+v, want the discarded render", checks[0])
	}
	if !checks[1].Shown || checks[1].Fix != "family-red-lip" {
		t.Fatalf("second check %+v, want the runner-up's render shown", checks[1])
	}
	if checks[1].MediaID == checks[0].MediaID && checks[1].MediaID == "" {
		t.Fatal("the shown check named no media id")
	}

	if look.Shown == nil || look.Shown.FixID != "family-red-lip" {
		t.Fatalf("shown render %+v, want the runner-up's", look.Shown)
	}
	if look.FixID != "family-gold-jhumkas" {
		t.Fatalf("fix %q, want the first pick to stay the look's fix", look.FixID)
	}
	if look.Words != runLine {
		t.Fatalf("words %q, want the shown render's line", look.Words)
	}
	if checks[1].MediaID != look.Shown.MediaID {
		t.Fatalf("the check event named %q, want the shown render's %q", checks[1].MediaID, look.Shown.MediaID)
	}

	// The shown picture is the lip render's, read back from the store.
	kept, err := story.rig.pics.Picture(t.Context(), "user-1", look.Shown.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	if want := recordedRenderBytes(t, rep, "lip-red-fetch"); !reflect.DeepEqual(kept.Bytes, want) {
		t.Fatalf("the shown render is %d bytes, want the lip render's %d", len(kept.Bytes), len(want))
	}

	// The which-fix-shops rule, shown arm: the shop frame names the
	// shown runner-up's own product - the lip the face wears, approximate
	// match - not the jhumkas fix the pick chose first, and not the item
	// the user brought, the lip compositing no product photo. This story
	// matches no recorded stream, so the want is the data's own entry;
	// the recorded frame's shape is pinned where a story does match one.
	var shop shopEvent
	if err := json.Unmarshal(events[7].Detail, &shop); err != nil {
		t.Fatal(err)
	}
	wantLip := []domain.Product{{
		ID:       "lip-red",
		Category: "lip-colour",
		Name:     "Red lip colour",
		Fidelity: domain.ApproximateMatch,
		Origin:   domain.OriginCatalog,
	}}
	if !reflect.DeepEqual(shop.Products, wantLip) {
		t.Fatalf("shop %+v, want the shown runner-up's %v", shop.Products, wantLip)
	}
	if !reflect.DeepEqual(look.Products, shop.Products) {
		t.Fatalf("look products %+v, want the shop frame's %+v", look.Products, shop.Products)
	}
}

// TestTheBroughtItemIsShoppedFromItsIntakeRecord pins the shop wiring's
// record read: when the fix the look ends on composites product photos
// and the user brought an item, finish resolves that item to its intake
// record before Shop sees it, so her own entry carries the record's name
// and category - not the empty shell the request's picture-only product
// would give - and ranks first, exact, hers, the catalog's jhumkas
// behind it. The read is the one the wiring makes: once, for the
// brought id, the user named with it.
func TestTheBroughtItemIsShoppedFromItsIntakeRecord(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	slots := []string{
		recordedSlotID(t, rep, v04Selfie),
		recordedSlotID(t, rep, v04Product),
	}
	shown := checkAnswer{change: 0.99, same: 0.99, met: 0.99}
	story := newEarringStory(t, rep, 1, []checkAnswer{shown}, nil, slots...)
	story.rig.clef.picks = []pickAnswer{{
		choice: "family-gold-jhumkas",
		probs:  map[string]float64{"family-red-lip": 0.4},
	}}

	const ownID = "b20d0c70000000000000000000000001"
	// The intake record the server kept when the item was brought: the
	// id, its name and category, exact product, origin user - the shape
	// api.md's intake row freezes.
	record := &domain.Product{
		ID:       ownID,
		Category: "earrings",
		Name:     "Her own jhumkas",
		Fidelity: domain.ExactProduct,
		Origin:   domain.OriginUser,
	}
	var reads []string
	var readMu sync.Mutex
	story.deps.ProductRecord = func(_ context.Context, user, id string) (*domain.Product, error) {
		readMu.Lock()
		defer readMu.Unlock()
		reads = append(reads, user+"/"+id)
		return record, nil
	}
	// The request carries the item as the render needs it - id and
	// picture, no words. The record is where the shop's words come from.
	story.req.Brought = &Product{ID: ownID, Image: recordedImage(t, rep, v04Product)}

	look, err := Run(t.Context(), story.deps, story.req, story.rig.events.publish)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	events := story.rig.events.all()
	if got, want := stagesOf(t, story.rig.events), []string{stageJudge, stagePick, stageMeasure, stageRender, stageCheck, stageShop}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stages %v, want %v", got, want)
	}

	// One record read, for the brought id, the user named with it.
	readMu.Lock()
	gotReads := append([]string(nil), reads...)
	readMu.Unlock()
	if len(gotReads) != 1 || gotReads[0] != "user-1/"+ownID {
		t.Fatalf("intake record reads %v, want one read of the brought id", gotReads)
	}

	// Her item first, exact, hers, wearing the record's own name and
	// category; the catalog's jhumkas behind it, exact, the catalog's.
	// The frame's shape is the recorded shop frame's.
	assertShape(t, stageShop, frameOf(t, readContract(t, "job-events.json"), stageShop), detail(t, events[5]))
	wantProducts := []domain.Product{
		{ID: ownID, Category: "earrings", Name: "Her own jhumkas", Fidelity: domain.ExactProduct, Origin: domain.OriginUser},
		{ID: "gold-jhumkas", Category: "earrings", Name: "Gold jhumkas", Fidelity: domain.ExactProduct, Origin: domain.OriginCatalog},
	}
	var shop shopEvent
	if err := json.Unmarshal(events[5].Detail, &shop); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(shop.Products, wantProducts) {
		t.Fatalf("shop %+v, want the record's own item first and the catalog's %v", shop.Products, wantProducts)
	}
	if !reflect.DeepEqual(look.Products, shop.Products) {
		t.Fatalf("look products %+v, want the shop frame's %+v", look.Products, shop.Products)
	}
}

// TestABudgetRefusalTakesTheSuggestionAlone pins the refusal arm: the
// render the budget refused publishes its code and its message and no
// media id, the runner-up is not tried, and the look finishes with the
// suggestion and empty books. Shop still runs - the contract's word for
// the refusal arm - and it shops the picked fix.
func TestABudgetRefusalTakesTheSuggestionAlone(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	story := newEarringStory(t, rep, 0, nil, nil) // no slots: nothing may upload
	story.rig.clef.picks = []pickAnswer{{
		choice: "family-gold-jhumkas",
		probs:  map[string]float64{"family-red-lip": 0.4},
	}}

	look, err := Run(t.Context(), story.deps, story.req, story.rig.events.publish)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// The stream ends at the refused render and the shop after it:
	// judge, pick, measure, render, shop - no check of a render that
	// never was, and no runner-up.
	got := stagesOf(t, story.rig.events)
	want := []string{stageJudge, stagePick, stageMeasure, stageRender, stageShop}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stages %v, want %v", got, want)
	}
	var renderGot renderEvent
	if err := json.Unmarshal(story.rig.events.all()[3].Detail, &renderGot); err != nil {
		t.Fatal(err)
	}
	if renderGot.Fix != "family-gold-jhumkas" || renderGot.Attempt != attemptFix {
		t.Fatalf("render event %+v, want the refused fix attempt", renderGot)
	}
	if renderGot.Code != codeBudgetRefused || renderGot.Message != messageBudgetRefused {
		t.Fatalf("refusal %q %q, want %s with the contract's message", renderGot.Code, renderGot.Message, codeBudgetRefused)
	}

	// The refusal shops the picked fix: nothing was shown, and the pick
	// is what the suggestion talks about.
	products := assertShopFrame(t, "job-events.json", story.rig.events.all()[4])
	if len(products) != 1 || products[0].ID != "gold-jhumkas" {
		t.Fatalf("shop %+v, want the picked fix's gold-jhumkas", products)
	}
	if !reflect.DeepEqual(look.Products, products) {
		t.Fatalf("look products %+v, want the shop frame's %+v", look.Products, products)
	}

	if !look.SuggestionOnly || look.Shown != nil {
		t.Fatalf("look %+v, want the suggestion standing", look)
	}
	if look.FixID != "family-gold-jhumkas" || look.RunnerUpID != "family-red-lip" {
		t.Fatalf("look fixes %q and %q, want the pick's two", look.FixID, look.RunnerUpID)
	}

	// Zero calls, zero units: the refusal quotes before anything runs,
	// and the suggestion is exactly what it was.
	if story.rig.budget.Spent() != 0 || story.rig.budget.Reserved() != 0 || len(story.rig.ledger.Charges()) != 0 {
		t.Fatalf("spent %d reserved %d, want empty books", story.rig.budget.Spent(), story.rig.budget.Reserved())
	}
	if story.rig.clef.askCount(t, changeMadeID) != 0 {
		t.Fatal("the check asked about a render that never was")
	}
}

// TestThePickDecliningEndsAtTheSuggestion pins the suggestion-only arm
// the fixtures do not record: a judge that flags and a pick whose answer
// is none. The stream is the contract's judge frame and pick frame, and
// then the shop stage - the contract's order runs it after every arm -
// whose frame is the empty products array, a declined pick having no fix
// to shop for. No measure, render or check frame after a declined pick,
// the look stands on its suggestions with no fix named, and the provider
// is never reached: no upload, no create, no poll, no fetch, zero spend.
// The one pick ask pins the decline to the model's answer, not to an
// empty menu.
func TestThePickDecliningEndsAtTheSuggestion(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	occasions := loadOccasions(t)
	photo := recordedImage(t, rep, v04Selfie)

	// The rig is hand-built, the resume proof's way, so the runner's
	// YouCam sits behind the tallying gate - a gate with no entered
	// channel, which counts without parking. The wire queues no slot and
	// the budget holds no unit, so a single provider call would fail the
	// run loudly before the tally could say otherwise.
	wire := newWireReplay(t, rep)
	client, err := youcam.NewClient(youcam.ClientConfig{
		BaseURL:  wire.base,
		Token:    runToken,
		Throttle: throttlePace(),
	})
	if err != nil {
		t.Fatal(err)
	}
	gated := &gatedYoucam{YouCam: newResultReplay(client, wire.base, rep)}
	budget, err := cost.NewBudgetIn(youcam.UnitsDenomination, 0)
	if err != nil {
		t.Fatal(err)
	}
	ledger := cost.NewLedger()
	meter, err := cost.NewMeter(budget, ledger)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(t.Context(), sqlite.Config{
		Path:   filepath.Join(t.TempDir(), "decline-tasks.db"),
		Logger: quietLog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tasks, err := sqlitestore.Open(t.Context(), sqlitestore.Config{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	blobs := &runBackend{blobs: map[string][]byte{}}
	media, err := mediastore.Open(t.Context(), mediastore.Config{
		Index:   &memMediaIndex{rows: map[string]mediastore.Blob{}},
		Backend: blobs,
		Log:     quietLog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := youcam.NewRunner(gated, youcam.RunnerConfig{
		Store: tasks,
		Meter: meter,
		Media: media,
		Config: providertask.Config{
			PollBase:    time.Millisecond,
			PollCeiling: 2 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	pics := &runPictures{named: map[string]systemone.Image{"selfie-1": photo}, media: media}
	clef := &lookStub{
		judge: map[string]float64{"family-earring-festive": 0.03, "family-lip-colour": 0.2},
		issue: "family-earring-festive",
		picks: []pickAnswer{{choice: "none"}},
	}
	src := occasionSource(occasions)
	deps := Deps{
		Clef:          clef,
		Runner:        runner,
		Cache:         newMemCache(),
		Vocab:         loadVocabulary(t),
		Thresholds:    Thresholds{Met: readingAt},
		Lines:         checkReading,
		CheckModel:    systemone.ModelClef,
		Occasions:     src.resolve,
		Pictures:      pics,
		Catalog:       loadCatalog(t),
		ProductRecord: noIntakeRecords,
	}
	req := Request{
		Photo:    Photo{User: "user-1", Hash: "hash-v04", Image: photo},
		Selfie:   "selfie-1",
		Occasion: "family-function",
		OptIn:    []string{"family-gold-jhumkas", "family-red-lip"},
	}
	events := &eventLog{}

	look, err := Run(t.Context(), deps, req, events.publish)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// The stream is judge, pick, shop: the two frames the contract holds
	// before a fix is measured, and the shop stage that runs after every
	// arm - empty here, nothing having been picked to shop for.
	got := stagesOf(t, events)
	want := []string{stageJudge, stagePick, stageShop}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stages %v, want %v", got, want)
	}
	published := events.all()
	judgeFixture := frameOf(t, readContract(t, "job-events.json"), stageJudge)
	assertShape(t, stageJudge, judgeFixture, detail(t, published[0]))
	pickFixture := frameOf(t, readContract(t, "job-events-suggestion.json"), stagePick)
	assertShape(t, stagePick, pickFixture, detail(t, published[1]))
	var pickGot pickEvent
	if err := json.Unmarshal(published[1].Detail, &pickGot); err != nil {
		t.Fatal(err)
	}
	if pickGot.Fix != "" || pickGot.RunnerUp != "" || pickGot.Recall != "" {
		t.Fatalf("pick %+v, want the decline's two empty ids and no recall", pickGot)
	}

	// The shop stage ran, and a declined pick shops nothing: the frame
	// is the empty products array, byte for byte - the shorter list, the
	// honest one, nothing invented to fill it.
	if published[2].Stage != stageShop {
		t.Fatalf("stage %q, want %q after the declined pick", published[2].Stage, stageShop)
	}
	if string(published[2].Detail) != `{"products":[]}` {
		t.Fatalf("shop detail %s, want the empty products array", published[2].Detail)
	}
	if len(look.Products) != 0 {
		t.Fatalf("look products %+v, want none after a declined pick", look.Products)
	}

	// The look stands on its suggestions: the flags and the issue stay,
	// and no fix, runner-up, render or words came out of a declined pick.
	if !look.SuggestionOnly || look.Shown != nil || look.Words != "" || look.FixID != "" || look.RunnerUpID != "" {
		t.Fatalf("look %+v, want the suggestion-only look", look)
	}
	if len(look.Flags) != 2 {
		t.Fatalf("%d flags, want the two misses the look stands on", len(look.Flags))
	}

	// The provider saw nothing at all.
	uploads, creates, polls, fetches := gated.tally()
	if len(uploads) != 0 || creates != 0 || polls != 0 || fetches != 0 {
		t.Fatalf("provider calls (%d uploads, %d creates, %d polls, %d fetches), want none", len(uploads), creates, polls, fetches)
	}
	if budget.Spent() != 0 || budget.Reserved() != 0 || len(ledger.Charges()) != 0 {
		t.Fatalf("spent %d reserved %d with %d charges, want empty books", budget.Spent(), budget.Reserved(), len(ledger.Charges()))
	}

	// The decline was the model's answer: one judge, one pick, no check.
	if n := clef.askCount(t, biggestIssueID); n != 1 {
		t.Fatalf("%d judge calls, want one", n)
	}
	if n := clef.askCount(t, pickQuestionID); n != 1 {
		t.Fatalf("%d pick calls, want the one the decline answered", n)
	}
	if n := clef.askCount(t, changeMadeID); n != 0 {
		t.Fatal("the check asked about a fix that was never picked")
	}
}

// TestTheMeasureStageSaysWhatTheAnalysisDid rides the hairstyle story,
// where the fix names Face Attributes: the first look runs the analysis
// and says cached false, the second look on the same photo reads the
// cache, says cached true, carries the face shape into the pick request,
// and spends nothing the second time. The renders behind the two looks
// are the one recorded task, collected twice.
func TestTheMeasureStageSaysWhatTheAnalysisDid(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	slots := []string{
		recordedSlotID(t, rep, faceAttrUpload),
		recordedSlotID(t, rep, "hair-updo-upload-src"),
	}
	occasions := loadOccasions(t)
	photo := recordedImage(t, rep, "hair-updo-upload-src")
	rig := newRunRig(t, rep, 12, slots...)
	rig.pics.named["selfie-1"] = photo
	rig.clef.judge = map[string]float64{"family-hair-finished": 0.03}
	rig.clef.issue = "family-hair-finished"
	rig.clef.picks = []pickAnswer{
		{choice: "family-tie-hair"},
		{choice: "family-tie-hair"},
	}
	rig.clef.look = map[string]string{
		"jewellery":        "none",
		"jewellery_colour": "none",
		"necklace":         "none",
		"necklace_colour":  "none",
		"lip_colour":       "none",
		"collar":           "mandarin",
		"hair":             "tied-back",
		"hair_colour":      "black",
		"beard":            "none",
		"expression":       "flat",
		"garment":          "festive-top",
	}
	rig.clef.checks = []checkAnswer{
		{change: 0.99, same: 0.99, met: 0.99},
		{change: 0.99, same: 0.99, met: 0.99},
	}
	src := occasionSource(occasions)
	deps := Deps{
		Clef:          rig.clef,
		Runner:        rig.runner,
		Cache:         rig.cache,
		Vocab:         loadVocabulary(t),
		Thresholds:    Thresholds{Met: readingAt},
		Lines:         checkReading,
		CheckModel:    systemone.ModelClef,
		Occasions:     src.resolve,
		Pictures:      rig.pics,
		Catalog:       loadCatalog(t),
		ProductRecord: noIntakeRecords,
	}
	req := Request{
		Photo:    Photo{User: "user-1", Hash: "hash-v06", Image: photo},
		Selfie:   "selfie-1",
		Occasion: "family-function",
		OptIn:    []string{"family-tie-hair"},
	}

	first, err := Run(t.Context(), deps, req, rig.events.publish)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	events := rig.events.all()
	want := []string{stageJudge, stagePick, stageMeasure, stageRender, stageCheck, stageShop}
	if got := stagesOf(t, rig.events); !reflect.DeepEqual(got, want) {
		t.Fatalf("stages %v, want %v", got, want)
	}
	if string(events[2].Detail) != `{"cached":false}` {
		t.Fatalf("measure detail %s, want the contract's cached-false frame", events[2].Detail)
	}
	// The shown fix has nothing to photograph: the shop frame is the one
	// suggestion, the fix under the basket's hair category.
	if want := `{"products":[{"id":"family-tie-hair","category":"hair","name":"Ties the hair back.","fidelity":"visual simulation","origin":"catalog"}]}`; string(events[5].Detail) != want {
		t.Fatalf("shop detail %s, want the hair suggestion", events[5].Detail)
	}
	if first.Shown == nil || first.Shown.FixID != "family-tie-hair" {
		t.Fatalf("look %+v, want the tied hair shown", first)
	}
	if len(first.Products) != 1 || first.Products[0].ID != "family-tie-hair" {
		t.Fatalf("look products %+v, want the hair suggestion", first.Products)
	}
	if rig.cache.count() != 1 {
		t.Fatalf("%d cache rows, want the one analysis row", rig.cache.count())
	}
	if held, ok, err := rig.cache.Held(t.Context(), "user-1", "hash-v06", analysisTaskName); err != nil || !ok || held.FaceShape != "Oval" {
		t.Fatalf("held row (%v, %v, %+v), want the recorded Oval", err, ok, held)
	}

	// The second look: the same photo, the cache answers the analysis,
	// the render collects the recorded task, and the pick carries the
	// face shape it measured.
	second, err := Run(t.Context(), deps, req, rig.events.publish)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	events = rig.events.all()
	gotStages := stagesOf(t, rig.events)
	wantStages := []string{
		stageJudge, stagePick, stageMeasure, stageRender, stageCheck, stageShop,
		stageJudge, stagePick, stageMeasure, stageRender, stageCheck, stageShop,
	}
	if !reflect.DeepEqual(gotStages, wantStages) {
		t.Fatalf("stages %v, want two runs of %v", gotStages, wantStages)
	}
	if string(events[8].Detail) != `{"cached":true}` {
		t.Fatalf("second measure detail %s, want the contract's cached-true frame", events[8].Detail)
	}
	state := rig.clef.lastPickState(t)
	if state.Attributes["face_shape"] != "Oval" {
		t.Fatalf("pick attributes %v, want the cached face shape on the picker allowlist", state.Attributes)
	}
	if second.Shown == nil || second.Shown.FixID != "family-tie-hair" {
		t.Fatalf("second look %+v, want the tied hair shown again", second)
	}

	// The books: the ten-unit analysis and the two-unit render, settled
	// once each, whatever the repeat collected.
	if rig.budget.Spent() != 12 || rig.budget.Reserved() != 0 {
		t.Fatalf("budget spent %d reserved %d, want the two settles only", rig.budget.Spent(), rig.budget.Reserved())
	}
	charges := rig.ledger.Charges()
	if len(charges) != 2 {
		t.Fatalf("charges %+v, want the analysis and the render, settled once each", charges)
	}
	refs := map[string]bool{}
	for _, c := range charges {
		refs[c.Ref] = true
	}
	if !refs["analysis/user-1/hash-v06/"+analysisTaskName] || !refs["render/user-1/hash-v06/family-tie-hair"] {
		t.Fatalf("charges %+v, want the analysis key and the render key", charges)
	}
	if rig.blobs.count() != 1 {
		t.Fatalf("mediastore keeps %d renders, want the one recorded task's copy", rig.blobs.count())
	}
}

// TestAKilledLookResumesAndNeverPaysTwice is the resume proof. The look
// runs as a real job under the real kind; its render blocks mid-task on
// the first poll, the way a process leaves one when it dies. A second
// runner opens over the same store - the restart - and its open
// interrupts the dead attempt and starts the replacement from the kind's
// resume hook and the snapshot. The first attempt is then cancelled, its
// lease lands back in the store, and the replacement takes the recorded
// task over: one create, one settle, the same picture, and a done look.
func TestAKilledLookResumesAndNeverPaysTwice(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	occasions := loadOccasions(t)
	photo := recordedImage(t, rep, v04Selfie)
	product := recordedImage(t, rep, v04Product)

	// The wire, the stores and the books are shared; the two youcam
	// runners are not, because two processes do not share a coordinator.
	wire := newWireReplay(t, rep)
	wire.enqueue(youcam.Slot{FileID: recordedSlotID(t, rep, v04Selfie)})
	wire.enqueue(youcam.Slot{FileID: recordedSlotID(t, rep, v04Product)})
	client, err := youcam.NewClient(youcam.ClientConfig{
		BaseURL:  wire.base,
		Token:    runToken,
		Throttle: throttlePace(),
	})
	if err != nil {
		t.Fatal(err)
	}
	gated := &gatedYoucam{YouCam: newResultReplay(client, wire.base, rep), entered: make(chan struct{})}

	budget, err := cost.NewBudgetIn(youcam.UnitsDenomination, 2)
	if err != nil {
		t.Fatal(err)
	}
	ledger := cost.NewLedger()
	meter, err := cost.NewMeter(budget, ledger)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(t.Context(), sqlite.Config{
		Path:   filepath.Join(t.TempDir(), "resume.db"),
		Logger: quietLog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tasks, err := sqlitestore.Open(t.Context(), sqlitestore.Config{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	jobStore, err := jobsql.Open(t.Context(), jobsql.Config{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	blobs := &runBackend{blobs: map[string][]byte{}}
	media, err := mediastore.Open(t.Context(), mediastore.Config{
		Index:   &memMediaIndex{rows: map[string]mediastore.Blob{}},
		Backend: blobs,
		Log:     quietLog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	tune := providertask.Config{
		PollBase:    time.Millisecond,
		PollCeiling: 2 * time.Millisecond,
	}
	runnerA, err := youcam.NewRunner(gated, youcam.RunnerConfig{Store: tasks, Meter: meter, Media: media, Config: tune})
	if err != nil {
		t.Fatal(err)
	}
	runnerB, err := youcam.NewRunner(gated, youcam.RunnerConfig{Store: tasks, Meter: meter, Media: media, Config: tune})
	if err != nil {
		t.Fatal(err)
	}
	pics := &runPictures{named: map[string]systemone.Image{
		"selfie-1":     photo,
		"gold-jhumkas": product,
	}, media: media}
	cache := newMemCache()
	clef := &lookStub{
		judge: map[string]float64{"family-earring-festive": 0.03, "family-lip-colour": 0.2},
		issue: "family-earring-festive",
		look: map[string]string{
			"jewellery": "jhumkas", "jewellery_colour": "gold", "necklace": "none",
			"necklace_colour": "none", "lip_colour": "none", "collar": "mandarin",
			"hair": "tied-back", "hair_colour": "black", "beard": "none",
			"expression": "flat", "garment": "festive-top",
		},
	}
	// Two judge asks, two pick asks, one check: the replacement runs the
	// model calls again, because they are deterministic and cost neurons
	// the budget does not meter.
	clef.picks = []pickAnswer{
		{choice: "family-gold-jhumkas", probs: map[string]float64{"family-red-lip": 0.4}},
		{choice: "family-gold-jhumkas", probs: map[string]float64{"family-red-lip": 0.4}},
	}
	clef.checks = []checkAnswer{{change: 0.99, same: 0.99, met: 0.99}}
	src := occasionSource(occasions)
	snaps := newMemSnaps()

	newDeps := func(runner *youcam.Runner) Deps {
		return Deps{
			Clef:          clef,
			Runner:        runner,
			Cache:         cache,
			Vocab:         loadVocabulary(t),
			Thresholds:    Thresholds{Met: readingAt},
			Lines:         checkReading,
			CheckModel:    systemone.ModelClef,
			Occasions:     src.resolve,
			Pictures:      pics,
			Catalog:       loadCatalog(t),
			ProductRecord: noIntakeRecords,
		}
	}
	broker := stream.New(stream.Config{})
	jr1, err := job.Open(t.Context(), job.Config{
		Broker: broker,
		Store:  jobStore,
		Kinds:  map[string]job.Kind{KindName: Kind(newDeps(runnerA), snaps)},
		Log:    quietLog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := Request{
		Photo:    Photo{User: "user-1", Hash: "hash-v04", Image: photo},
		Selfie:   "selfie-1",
		Occasion: "family-function",
		OptIn:    []string{"family-gold-jhumkas", "family-red-lip"},
		Basket:   &Product{ID: "gold-jhumkas", Image: product},
	}
	jobID, err := Start(t.Context(), jr1, newDeps(runnerA), snaps, req)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// The snapshot travelled before the look paid anything.
	snap, ok, err := snaps.Snapshot(t.Context(), jobID)
	if err != nil || !ok {
		t.Fatalf("snapshot (%v, %v), want the look's inputs kept at start", ok, err)
	}
	if snap.User != "user-1" || snap.Occasion != "family-function" || snap.Selfie != "selfie-1" || snap.Basket != "gold-jhumkas" {
		t.Fatalf("snapshot %+v, want the request's ids", snap)
	}

	// The look renders, and the first poll of the recorded task parks -
	// the moment a dead process leaves behind.
	select {
	case <-gated.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the render never reached its first poll")
	}

	// The restart: a second runner opens over the same store, interrupts
	// the dead attempt, and resumes the kind from the snapshot.
	jr2, err := job.Open(t.Context(), job.Config{
		Broker: broker,
		Store:  jobStore,
		Kinds:  map[string]job.Kind{KindName: Kind(newDeps(runnerB), snaps)},
		Log:    quietLog(),
	})
	if err != nil {
		t.Fatalf("restart: %v", err)
	}

	// The dead attempt lets go of the task: its context ends, its lease
	// is released, and the waiter takes the recorded task over.
	if err := jr1.Cancel(jobID); err != nil {
		t.Fatalf("cancel the dead attempt: %v", err)
	}

	// The replacement finishes the look.
	root := jobID
	attempt2 := waitAttempt(t, jr2, root)
	if attempt2.Status != job.StatusDone {
		t.Fatalf("replacement status %s (%v), want done", attempt2.Status, attempt2.Err)
	}
	var look Look
	if err := json.Unmarshal(attempt2.Data, &look); err != nil {
		t.Fatalf("replacement data: %v", err)
	}
	if look.Shown == nil || look.Shown.FixID != "family-gold-jhumkas" {
		t.Fatalf("replacement look %+v, want the jhumkas render shown", look)
	}
	if look.SuggestionOnly || look.Words == "" {
		t.Fatalf("replacement look %+v, want the words", look)
	}
	// The replacement shops too: the shown fix's product rides the look
	// the restart finished, resume and all.
	if len(look.Products) != 1 || look.Products[0].ID != "gold-jhumkas" || look.Products[0].Fidelity != domain.ExactProduct {
		t.Fatalf("replacement products %+v, want the shown fix's gold-jhumkas exact", look.Products)
	}

	// The chain: exactly one attempt replacement, and the record of the
	// first attempt is not running.
	attempts, err := jr2.Attempts(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 {
		t.Fatalf("%d attempts, want the dead one and its replacement", len(attempts))
	}
	for _, rec := range attempts {
		if rec.Attempt == 1 && rec.Status == job.StatusRunning {
			t.Fatal("the dead attempt is still marked running")
		}
	}

	// The proof: the provider saw one create. The replacement collected
	// the recorded task - its poll, its picture, the same mediastore id -
	// and settled the one-unit line once.
	uploads, creates, polls, fetches := gated.tally()
	if creates != 1 {
		t.Fatalf("creates %d, want the one recorded task, never a second", creates)
	}
	if len(uploads) != 2 {
		t.Fatalf("uploads %d, want the dead attempt's selfie and product only", len(uploads))
	}
	if polls < 2 || fetches < 1 {
		t.Fatalf("polls %d fetches %d, want the collected task's poll and copy", polls, fetches)
	}
	if budget.Spent() != 1 || budget.Reserved() != 0 {
		t.Fatalf("budget spent %d reserved %d, want the one render settled once", budget.Spent(), budget.Reserved())
	}
	charges := ledger.Charges()
	if len(charges) != 1 || charges[0].Ref != "render/user-1/hash-v04/family-gold-jhumkas/gold-jhumkas" {
		t.Fatalf("charges %+v, want the one settle on the render key", charges)
	}
	kept, err := pics.Picture(t.Context(), "user-1", look.Shown.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	if want := recordedRenderBytes(t, rep, "earring-fetch"); !reflect.DeepEqual(kept.Bytes, want) {
		t.Fatalf("the kept render is %d bytes, want the recorded %d", len(kept.Bytes), len(want))
	}
}

// gatedYoucam is the process boundary: every call passes through, but
// the first poll parks until its context ends or the test releases it,
// the way a task in flight does when its process dies. A gate built
// with no entered channel never parks - it is a tally alone, for a run
// that must reach the provider not at all.
type gatedYoucam struct {
	youcam.YouCam

	mu      sync.Mutex
	uploads []youcam.File
	creates int
	polls   int
	fetches int
	entered chan struct{}
}

func (g *gatedYoucam) Upload(ctx context.Context, file youcam.File) (youcam.Slot, error) {
	g.mu.Lock()
	g.uploads = append(g.uploads, file)
	g.mu.Unlock()
	return g.YouCam.Upload(ctx, file)
}

func (g *gatedYoucam) Create(ctx context.Context, task youcam.Task) (youcam.Created, error) {
	g.mu.Lock()
	g.creates++
	g.mu.Unlock()
	return g.YouCam.Create(ctx, task)
}

func (g *gatedYoucam) Poll(ctx context.Context, ref youcam.PollRef) (youcam.Status, error) {
	g.mu.Lock()
	g.polls++
	first := g.polls == 1
	park := first && g.entered != nil
	g.mu.Unlock()
	if park {
		close(g.entered)
		select {
		case <-ctx.Done():
			return youcam.Status{}, ctx.Err()
		case <-time.After(30 * time.Second):
			return g.YouCam.Poll(ctx, ref)
		}
	}
	return g.YouCam.Poll(ctx, ref)
}

func (g *gatedYoucam) Fetch(ctx context.Context, ref youcam.ResultRef) (youcam.Result, error) {
	g.mu.Lock()
	g.fetches++
	g.mu.Unlock()
	return g.YouCam.Fetch(ctx, ref)
}

func (g *gatedYoucam) tally() (uploads []youcam.File, creates, polls, fetches int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]youcam.File(nil), g.uploads...), g.creates, g.polls, g.fetches
}

// waitAttempt waits for the replacement attempt of a logical job to
// reach a terminal state and returns its record.
func waitAttempt(t *testing.T, runner *job.Runner, root string) job.Record {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		attempts, err := runner.Attempts(t.Context(), root)
		if err != nil {
			t.Fatal(err)
		}
		for _, rec := range attempts {
			if rec.Attempt >= 2 && rec.Status != job.StatusRunning && rec.Status != job.StatusQueued {
				return rec
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the replacement attempt never finished")
	return job.Record{}
}

// TestRunRefusesAnUnusableLook pins the refusals that come before any
// call: a dep missing, an occasion unknown, a look with no photo, a look
// that names no kept selfie. Not one ask reaches the model.
func TestRunRefusesAnUnusableLook(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	occasions := loadOccasions(t)
	photo := recordedImage(t, rep, v04Selfie)
	rig := newRunRig(t, rep, 5)
	rig.pics.named["selfie-1"] = photo
	rig.clef.judge = map[string]float64{}
	rig.clef.issue = "none"
	rig.clef.picks = []pickAnswer{{choice: "none"}}
	rig.clef.look = map[string]string{}
	rig.clef.checks = []checkAnswer{{change: 0.9, same: 0.9, met: 0.9}}
	src := occasionSource(occasions)
	whole := Deps{
		Clef:          rig.clef,
		Runner:        rig.runner,
		Cache:         rig.cache,
		Vocab:         loadVocabulary(t),
		Thresholds:    Thresholds{Met: readingAt},
		Lines:         checkReading,
		CheckModel:    systemone.ModelClef,
		Occasions:     src.resolve,
		Pictures:      rig.pics,
		Catalog:       loadCatalog(t),
		ProductRecord: noIntakeRecords,
	}
	req := Request{
		Photo:    Photo{User: "user-1", Hash: "hash-v04", Image: photo},
		Selfie:   "selfie-1",
		Occasion: "family-function",
		OptIn:    []string{"family-gold-jhumkas"},
	}
	broken := func(mutate func(*Deps, *Request)) func() error {
		return func() error {
			deps, r := whole, req
			mutate(&deps, &r)
			_, err := Run(t.Context(), deps, r, nil)
			return err
		}
	}
	cases := []struct {
		name string
		want string
		call func() error
	}{
		{"no clef client", "no clef client", broken(func(d *Deps, _ *Request) { d.Clef = nil })},
		{"no youcam runner", "no youcam runner", broken(func(d *Deps, _ *Request) { d.Runner = nil })},
		{"no analysis cache", "no analysis cache", broken(func(d *Deps, _ *Request) { d.Cache = nil })},
		{"no vocabulary", "no look vocabulary", broken(func(d *Deps, _ *Request) { d.Vocab = nil })},
		{"no occasions", "no occasions", broken(func(d *Deps, _ *Request) { d.Occasions = nil })},
		{"no pictures", "no pictures", broken(func(d *Deps, _ *Request) { d.Pictures = nil })},
		{"no product record", "no product record", broken(func(d *Deps, _ *Request) { d.ProductRecord = nil })},
		{"no check model", "no check model", broken(func(d *Deps, _ *Request) { d.CheckModel = "" })},
		{"no catalog", "no catalog", broken(func(d *Deps, _ *Request) { d.Catalog = nil })},
		{"an unknown occasion", `no occasion "no-such"`, broken(func(_ *Deps, r *Request) { r.Occasion = "no-such" })},
		{"no user", "no user", broken(func(_ *Deps, r *Request) { r.Photo.User = "" })},
		{"no hash", "no photo hash", broken(func(_ *Deps, r *Request) { r.Photo.Hash = "" })},
		{"no selfie anywhere", "no selfie bytes and no kept selfie", broken(func(_ *Deps, r *Request) {
			r.Photo.Image = systemone.Image{}
			r.Selfie = ""
		})},
		{"a selfie the store does not hold", "the selfie ghost-1", broken(func(_ *Deps, r *Request) {
			r.Photo.Image = systemone.Image{}
			r.Selfie = "ghost-1"
		})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.call(); err == nil {
				t.Fatal("an unusable look ran")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v, want it to name %q", err, tc.want)
			}
		})
	}
	// Every refusal landed before the model: the rig's stub saw nothing.
	if n := rig.clef.askCount(t, biggestIssueID); n != 0 {
		t.Fatalf("%d judge calls, want the refusals before any call", n)
	}
}

// TestKindRefusesARecordItCannotRebuild pins the resume hook's refusals:
// a record whose root names no snapshot, a snapshot of another version,
// a snapshot with a hole in it, and a snapshot naming an occasion the
// wiring does not hold are all refused before a replacement starts.
func TestKindRefusesARecordItCannotRebuild(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	rig := newRunRig(t, rep, 5)
	occasions := loadOccasions(t)
	src := occasionSource(occasions)
	deps := Deps{
		Clef:          rig.clef,
		Runner:        rig.runner,
		Cache:         rig.cache,
		Vocab:         loadVocabulary(t),
		Thresholds:    Thresholds{Met: readingAt},
		Lines:         checkReading,
		CheckModel:    systemone.ModelClef,
		Occasions:     src.resolve,
		Pictures:      rig.pics,
		Catalog:       loadCatalog(t),
		ProductRecord: noIntakeRecords,
	}
	snaps := newMemSnaps()
	kind := Kind(deps, snaps)

	build := func(id, root string) job.Record {
		return job.Record{ID: id, Kind: KindName, RootID: root, Attempt: 2, Status: job.StatusQueued}
	}
	t.Run("no snapshot", func(t *testing.T) {
		if _, err := kind.Resume(build("job-2", "job-ghost")); err == nil {
			t.Fatal("a record with no snapshot was rebuilt")
		}
	})
	t.Run("a snapshot of another version", func(t *testing.T) {
		if err := snaps.Keep(t.Context(), "job-old", Snapshot{Version: 99, User: "user-1"}); err != nil {
			t.Fatal(err)
		}
		if _, err := kind.Resume(build("job-2", "job-old")); err == nil {
			t.Fatal("a foreign snapshot was rebuilt")
		}
	})
	t.Run("a snapshot with a hole", func(t *testing.T) {
		if err := snaps.Keep(t.Context(), "job-hole", Snapshot{Version: snapshotVersion, User: "user-1", Hash: "hash"}); err != nil {
			t.Fatal(err)
		}
		if _, err := kind.Resume(build("job-2", "job-hole")); err == nil {
			t.Fatal("an incomplete snapshot was rebuilt")
		}
	})
	t.Run("an occasion the wiring does not hold", func(t *testing.T) {
		if err := snaps.Keep(t.Context(), "job-occ", Snapshot{
			Version: snapshotVersion, User: "user-1", Hash: "hash",
			Selfie: "selfie-1", Occasion: "no-such",
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := kind.Resume(build("job-2", "job-occ")); err == nil {
			t.Fatal("an unknown occasion was rebuilt")
		}
	})
	t.Run("a rebuildable record builds work", func(t *testing.T) {
		if err := snaps.Keep(t.Context(), "job-ok", Request{
			Photo:    Photo{User: "user-1", Hash: "hash-v04"},
			Selfie:   "selfie-1",
			Occasion: "family-function",
		}.snapshot()); err != nil {
			t.Fatal(err)
		}
		fn, err := kind.Resume(build("job-2", "job-ok"))
		if err != nil {
			t.Fatalf("resume: %v", err)
		}
		if fn == nil {
			t.Fatal("the rebuildable record built no work")
		}
	})
}

// TestStartRefusesALookItCannotName pins the start-side refusals: no
// runner, no snapshots, an unknown occasion, and a look that names no
// kept selfie never start a job.
func TestStartRefusesALookItCannotName(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	occasions := loadOccasions(t)
	src := occasionSource(occasions)
	photo := recordedImage(t, rep, v04Selfie)
	rig := newRunRig(t, rep, 5)
	deps := Deps{
		Clef:          rig.clef,
		Runner:        rig.runner,
		Cache:         rig.cache,
		Vocab:         loadVocabulary(t),
		Thresholds:    Thresholds{Met: readingAt},
		Lines:         checkReading,
		CheckModel:    systemone.ModelClef,
		Occasions:     src.resolve,
		Pictures:      rig.pics,
		Catalog:       loadCatalog(t),
		ProductRecord: noIntakeRecords,
	}
	rig.pics.named["selfie-1"] = photo
	req := Request{
		Photo:    Photo{User: "user-1", Hash: "hash-v04", Image: photo},
		Selfie:   "selfie-1",
		Occasion: "family-function",
		OptIn:    []string{"family-gold-jhumkas"},
	}

	if _, err := Start(t.Context(), nil, deps, newMemSnaps(), req); err == nil {
		t.Fatal("a look started with no runner")
	}
	broker := stream.New(stream.Config{})
	db, err := sqlite.Open(t.Context(), sqlite.Config{
		Path:   filepath.Join(t.TempDir(), "start-jobs.db"),
		Logger: quietLog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	jobStore, err := jobsql.Open(t.Context(), jobsql.Config{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := job.Open(t.Context(), job.Config{
		Broker: broker,
		Store:  jobStore,
		Kinds:  map[string]job.Kind{KindName: Kind(deps, newMemSnaps())},
		Log:    quietLog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Start(t.Context(), runner, deps, nil, req); err == nil {
		t.Fatal("a look started with no snapshots")
	}
	if _, err := Start(t.Context(), runner, deps, newMemSnaps(), Request{
		Photo: req.Photo, Selfie: req.Selfie, Occasion: "no-such",
	}); err == nil || !strings.Contains(err.Error(), `no occasion "no-such"`) {
		t.Fatalf("err %v, want the unknown occasion refused", err)
	}
	if _, err := Start(t.Context(), runner, deps, newMemSnaps(), Request{
		Photo: req.Photo, Occasion: req.Occasion,
	}); err == nil || !strings.Contains(err.Error(), "no kept selfie") {
		t.Fatalf("err %v, want the look with no kept selfie refused", err)
	}
}

// parkSnaps is the snapshot store with the sliver made wide enough to
// stand in: its keep parks until the test releases it, and the context
// it was handed stays readable while it parks. A keep whose handed
// context is already done is refused, the way a store that honours its
// context refuses one - which is exactly what a cancel in the window
// between StartKind and the keep used to cost the look.
type parkSnaps struct {
	mu      sync.Mutex
	kept    map[string]Snapshot
	handed  context.Context
	entered chan struct{}
	release chan struct{}
}

func (s *parkSnaps) Keep(ctx context.Context, root string, snap Snapshot) error {
	s.mu.Lock()
	s.handed = ctx
	s.mu.Unlock()
	close(s.entered)
	<-s.release
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.kept == nil {
		s.kept = map[string]Snapshot{}
	}
	s.kept[root] = snap
	return nil
}

func (s *parkSnaps) Snapshot(_ context.Context, root string) (Snapshot, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.kept[root]
	return snap, ok, nil
}

// handedCtx is the context the last keep was handed.
func (s *parkSnaps) handedCtx() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handed
}

// TestStartKeepsTheSnapshotPastACancel pins the sliver's hardening: the
// keep runs under the caller's context with the cancellation cut, so a
// cancel that lands between StartKind and the keep cannot leave a
// running look whose record names nothing. The window is staged
// deterministically at the keep itself - the one statement the window
// holds: the probe's keep parks, the test cancels the caller's context
// while Start stands inside it, and the context the keep was handed must
// still be alive. Released, the keep lands and Start returns the id with
// no error; the snapshot is queryable and the kind's resume hook rebuilds
// the look from it, which is the record a restart's replacement takes
// over. The rig cannot cancel between two statements of Start, so the
// window is stood in, not raced past.
func TestStartKeepsTheSnapshotPastACancel(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	occasions := loadOccasions(t)
	photo := recordedImage(t, rep, v04Selfie)
	rig := newRunRig(t, rep, 5)
	rig.pics.named["selfie-1"] = photo
	rig.clef.judge = map[string]float64{"family-earring-festive": 0.03}
	rig.clef.issue = "family-earring-festive"
	rig.clef.picks = []pickAnswer{{choice: "none"}}
	src := occasionSource(occasions)
	deps := Deps{
		Clef:          rig.clef,
		Runner:        rig.runner,
		Cache:         rig.cache,
		Vocab:         loadVocabulary(t),
		Thresholds:    Thresholds{Met: readingAt},
		Lines:         checkReading,
		CheckModel:    systemone.ModelClef,
		Occasions:     src.resolve,
		Pictures:      rig.pics,
		Catalog:       loadCatalog(t),
		ProductRecord: noIntakeRecords,
	}
	req := Request{
		Photo:    Photo{User: "user-1", Hash: "hash-v04", Image: photo},
		Selfie:   "selfie-1",
		Occasion: "family-function",
		OptIn:    []string{"family-gold-jhumkas"},
	}
	snaps := &parkSnaps{entered: make(chan struct{}), release: make(chan struct{})}

	broker := stream.New(stream.Config{})
	db, err := sqlite.Open(t.Context(), sqlite.Config{
		Path:   filepath.Join(t.TempDir(), "cancel-jobs.db"),
		Logger: quietLog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	jobStore, err := jobsql.Open(t.Context(), jobsql.Config{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	jr, err := job.Open(t.Context(), job.Config{
		Broker: broker,
		Store:  jobStore,
		Kinds:  map[string]job.Kind{KindName: Kind(deps, snaps)},
		Log:    quietLog(),
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan error, 1)
	var id string
	go func() {
		var err error
		id, err = Start(ctx, jr, deps, snaps, req)
		started <- err
	}()
	select {
	case <-snaps.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the keep never started")
	}

	// The caller gives up in the window. The job's drive dies with this
	// cancel; only the cut keeps the keep alive.
	cancel()

	if err := snaps.handedCtx().Err(); err != nil {
		t.Fatalf("the keep was handed a context already done (%v), want the cancellation cut", err)
	}

	close(snaps.release)
	if err := <-started; err != nil {
		t.Fatalf("start: %v", err)
	}
	if id == "" {
		t.Fatal("start returned no job id")
	}

	// The record a restart finds: the snapshot is queryable and the kind
	// rebuilds the look's work from it.
	snap, ok, err := snaps.Snapshot(t.Context(), id)
	if err != nil || !ok {
		t.Fatalf("snapshot (%v, %v), want the look's inputs kept past the cancel", ok, err)
	}
	if err := snap.valid(); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	fn, err := Kind(deps, snaps).Resume(job.Record{ID: id + "-attempt-2", Kind: KindName, RootID: id, Attempt: 2, Status: job.StatusQueued})
	if err != nil || fn == nil {
		t.Fatalf("resume (%v, %v), want the look rebuilt and resumable", fn, err)
	}
}
