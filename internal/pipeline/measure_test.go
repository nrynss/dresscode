package pipeline

// The measure tests replay the recorded face-attr-analysis flow through
// the T2.2 runner and the live client, and pin the two spend boundaries
// the done-when names: a second look on the same photo makes zero calls,
// and a fix that needs no analysis makes zero calls. No test dials the
// provider, and no unit is spent: the wire is an httptest server serving
// the recorded replies, the key is a fake, and the meter's budget is the
// test's own.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/systemone"
	"github.com/nrynss/dresscode/internal/youcam"
	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/mediastore"
	"github.com/nrynss/keel/providertask"
	"github.com/nrynss/keel/providertask/sqlitestore"
	"github.com/nrynss/keel/sqlite"
	"github.com/nrynss/keel/throttle"
)

// measureToken stands in for the API key. It is never a real key.
const measureToken = "measure-test-key-nothing-real"

// analysisTaskName is the one analysis task on the menu.
const analysisTaskName = "face-attr-analysis"

// faceAttrUpload is the typed upload recording the analysis flow replays.
const faceAttrUpload = "face-attr-upload-src"

// openYoucam opens the recorded YouCam exchanges.
func openYoucam(t *testing.T) *youcam.Replay {
	t.Helper()
	replay, err := youcam.Open(filepath.Join(repoRoot(t), "testdata", "youcam"))
	if err != nil {
		t.Fatalf("open testdata/youcam: %v", err)
	}
	return replay
}

// recordedPhoto rebuilds the normalised photo the analysis flow was
// recorded against. The typed upload recording carries the file's bytes
// inline since Open, so the photo here is the committed sample itself.
func recordedPhoto(t *testing.T, rep *youcam.Replay) systemone.Image {
	t.Helper()
	rec, err := rep.Reply(faceAttrUpload)
	if err != nil {
		t.Fatal(err)
	}
	var file youcam.File
	if err := json.Unmarshal(rec.Request, &file); err != nil {
		t.Fatalf("the upload recording is not a File: %v", err)
	}
	if len(file.Bytes) == 0 || file.ContentType == "" {
		t.Fatal("the upload recording carries no bytes or no content type")
	}
	return systemone.Image{ContentType: file.ContentType, Bytes: file.Bytes}
}

// recordedFileID is the file id the recorded upload answered with. The
// recorded create names it, so the slot reply must hand it out for the
// built payload to match the recorded create byte for byte.
func recordedFileID(t *testing.T, rep *youcam.Replay) string {
	t.Helper()
	rec, err := rep.Reply(faceAttrUpload)
	if err != nil {
		t.Fatal(err)
	}
	var slot youcam.Slot
	if err := json.Unmarshal(rec.Response, &slot); err != nil {
		t.Fatalf("the upload recording is not a Slot: %v", err)
	}
	return slot.FileID
}

// wireReplay is the offline YouCam these tests face: an httptest server
// that answers each queued upload slot with the recorded file id, accepts
// the bytes the PUT carries, and serves every API call from the recorded
// http-* exchanges by method, path and body. A call the recordings do not
// hold answers 500, so a stage that makes one fails the test loudly. No
// request leaves the process and no signed URL is dialed.
type wireReplay struct {
	rep *youcam.Replay

	mu    sync.Mutex
	slots []youcam.Slot

	srv  *httptest.Server
	base string
}

func newWireReplay(t *testing.T, rep *youcam.Replay) *wireReplay {
	t.Helper()
	w := &wireReplay{rep: rep}
	w.srv = httptest.NewServer(w)
	t.Cleanup(w.srv.Close)
	w.base = w.srv.URL
	return w
}

// enqueue queues one slot reply. The slot spends no units, and this
// flow's slot body was never recorded (docs/youcam.md keeps one slot per
// distinct request), so the reply is built around the recorded file id.
func (w *wireReplay) enqueue(slot youcam.Slot) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.slots = append(w.slots, slot)
}

func (w *wireReplay) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxMeasureBody+1))
	if err != nil {
		http.Error(rw, "body", http.StatusBadRequest)
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/s2s/v2.0/file":
		w.mu.Lock()
		have := len(w.slots) > 0
		var slot youcam.Slot
		if have {
			slot = w.slots[0]
			w.slots = w.slots[1:]
		}
		w.mu.Unlock()
		if !have {
			http.Error(rw, "no queued slot", http.StatusInternalServerError)
			return
		}
		w.slotReply(rw, slot)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/put/"):
		rw.WriteHeader(http.StatusOK)
	default:
		rec, err := w.rep.Serve(r.Method, r.URL.Path, body)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		var envelope struct {
			Status int             `json:"status"`
			Body   json.RawMessage `json:"body"`
		}
		if err := json.Unmarshal(rec.Response, &envelope); err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		if envelope.Status == 0 {
			envelope.Status = http.StatusOK
		}
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(envelope.Status)
		_, _ = rw.Write(envelope.Body)
	}
}

// slotPut is the one upload request a slot reply names.
type slotPut struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

// slotFileReply is one file of the slot reply the harness builds.
type slotFileReply struct {
	FileID   string    `json:"file_id"`
	Requests []slotPut `json:"requests"`
}

// slotReply answers the File API the way the recorded slots do, with the
// one PUT pointed back at this server so the bytes never leave.
func (w *wireReplay) slotReply(rw http.ResponseWriter, slot youcam.Slot) {
	reply := struct {
		Status int `json:"status"`
		Data   struct {
			Files []slotFileReply `json:"files"`
		} `json:"data"`
	}{Status: http.StatusOK}
	reply.Data.Files = append(reply.Data.Files, slotFileReply{
		FileID:   slot.FileID,
		Requests: []slotPut{{Method: http.MethodPut, URL: w.base + "/put/uploaded"}},
	})
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(rw).Encode(reply)
}

// maxMeasureBody bounds one request body the harness reads. No analysis
// payload is more than a file id and a few feature names.
const maxMeasureBody = 1 << 20

// countingClient counts every call the runner makes of its client and
// keeps every create body, so a test pins what was asked and that nothing
// else was.
type countingClient struct {
	youcam.YouCam

	mu          sync.Mutex
	uploads     int
	creates     int
	polls       int
	fetches     int
	createTasks []string
	createBody  []json.RawMessage
}

func (c *countingClient) Upload(ctx context.Context, file youcam.File) (youcam.Slot, error) {
	c.mu.Lock()
	c.uploads++
	c.mu.Unlock()
	return c.YouCam.Upload(ctx, file)
}

func (c *countingClient) Create(ctx context.Context, task youcam.Task) (youcam.Created, error) {
	c.mu.Lock()
	c.creates++
	c.createTasks = append(c.createTasks, task.Name)
	c.createBody = append(c.createBody, task.Payload)
	c.mu.Unlock()
	return c.YouCam.Create(ctx, task)
}

func (c *countingClient) Poll(ctx context.Context, ref youcam.PollRef) (youcam.Status, error) {
	c.mu.Lock()
	c.polls++
	c.mu.Unlock()
	return c.YouCam.Poll(ctx, ref)
}

func (c *countingClient) Fetch(ctx context.Context, ref youcam.ResultRef) (youcam.Result, error) {
	c.mu.Lock()
	c.fetches++
	c.mu.Unlock()
	return c.YouCam.Fetch(ctx, ref)
}

// counts is the call tally, and the create bodies under a lock.
func (c *countingClient) tally() (uploads, creates, polls, fetches int, bodies []json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.uploads, c.creates, c.polls, c.fetches, append([]json.RawMessage(nil), c.createBody...)
}

// memCache is the analysis cache in memory, the analysis_cache table's
// shape: one row per user, photo hash and task, the four allowlist
// columns, nothing else.
type memCache struct {
	mu   sync.Mutex
	rows map[string]domain.Allowlist
}

func newMemCache() *memCache {
	return &memCache{rows: map[string]domain.Allowlist{}}
}

func cacheKey(user, hash, task string) string {
	return user + "\x00" + hash + "\x00" + task
}

func (c *memCache) Held(_ context.Context, user, photoHash, task string) (domain.Allowlist, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	row, ok := c.rows[cacheKey(user, photoHash, task)]
	return row, ok, nil
}

func (c *memCache) Keep(_ context.Context, user, photoHash, task string, values domain.Allowlist) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rows[cacheKey(user, photoHash, task)] = values
	return nil
}

func (c *memCache) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.rows)
}

// memMedia is the in-memory mediastore the rig opens over, the shape the
// T2.2 tests fake their index with. The analysis flow keeps nothing, so
// the store is here because the runner refuses a build without one.
type memMedia struct {
	index  *memMediaIndex
	blocks *memMediaBackend
	store  *mediastore.Store
}

func newMemMedia(t *testing.T) *mediastore.Store {
	t.Helper()
	fakes := memMedia{
		index:  &memMediaIndex{rows: map[string]mediastore.Blob{}},
		blocks: &memMediaBackend{blobs: map[string][]byte{}},
	}
	store, err := mediastore.Open(t.Context(), mediastore.Config{Index: fakes.index, Backend: fakes.blocks, Log: quietLog()})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

type memMediaIndex struct {
	mu   sync.Mutex
	rows map[string]mediastore.Blob
}

func (m *memMediaIndex) Create(_ context.Context, b mediastore.Blob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[b.ID]; ok {
		return mediastore.ErrAlreadyExists
	}
	m.rows[b.ID] = b
	return nil
}

func (m *memMediaIndex) Get(_ context.Context, id string) (mediastore.Blob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.rows[id]
	if !ok {
		return mediastore.Blob{}, mediastore.ErrNotFound
	}
	return b, nil
}

func (m *memMediaIndex) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[id]; !ok {
		return mediastore.ErrNotFound
	}
	delete(m.rows, id)
	return nil
}

func (m *memMediaIndex) DeleteGroup(_ context.Context, group string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, b := range m.rows {
		if b.Group == group {
			delete(m.rows, id)
		}
	}
	return nil
}

func (m *memMediaIndex) Groups(_ context.Context) ([]mediastore.Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []mediastore.Group
	seen := map[string]bool{}
	for _, b := range m.rows {
		if !seen[b.Group] {
			seen[b.Group] = true
			out = append(out, mediastore.Group{ID: b.Group, Blobs: []mediastore.Blob{b}})
		}
	}
	return out, nil
}

func (m *memMediaIndex) Blobs(_ context.Context) ([]mediastore.Blob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]mediastore.Blob, 0, len(m.rows))
	for _, b := range m.rows {
		out = append(out, b)
	}
	return out, nil
}

type memMediaBackend struct {
	mu    sync.Mutex
	blobs map[string][]byte
}

func (m *memMediaBackend) Open(_ context.Context, id string) (mediastore.BlobReader, error) {
	m.mu.Lock()
	data, ok := m.blobs[id]
	m.mu.Unlock()
	if !ok {
		return nil, mediastore.ErrNotFound
	}
	return nopCloserReader{bytes.NewReader(data)}, nil
}

// nopCloserReader is a blob reader over bytes already in hand.
type nopCloserReader struct{ *bytes.Reader }

func (nopCloserReader) Close() error { return nil }

func (m *memMediaBackend) Write(_ context.Context, _ string, src io.Reader) (int64, error) {
	data, err := io.ReadAll(src)
	if err != nil {
		return 0, err
	}
	return int64(len(data)), nil
}

func (m *memMediaBackend) Delete(_ context.Context, _ string) error { return nil }

func (m *memMediaBackend) List(_ context.Context) ([]mediastore.Object, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]mediastore.Object, 0, len(m.blobs))
	for id, data := range m.blobs {
		out = append(out, mediastore.Object{ID: id, SizeBytes: int64(len(data))})
	}
	return out, nil
}

func quietLog() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// measureRig is one assembled measure run: the runner over the counting
// client and the wire replay, the meter over its own unit budget, the
// in-memory cache, and nothing dialed.
type measureRig struct {
	runner   *youcam.Runner
	cache    *memCache
	counting *countingClient
	budget   *cost.Budget
	ledger   *cost.Ledger
}

// newMeasureRig assembles the rig with a unit budget of units, so a test
// that must prove a spend names its ceiling and a test that must prove a
// non-spend watches it stay whole.
func newMeasureRig(t *testing.T, rep *youcam.Replay, units cost.Price) (*measureRig, *wireReplay) {
	t.Helper()
	wire := newWireReplay(t, rep)
	wire.enqueue(youcam.Slot{FileID: recordedFileID(t, rep)})
	client, err := youcam.NewClient(youcam.ClientConfig{
		BaseURL:  wire.base,
		Token:    measureToken,
		Throttle: throttlePace(),
	})
	if err != nil {
		t.Fatal(err)
	}
	counting := &countingClient{YouCam: client}
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
		Path:   filepath.Join(t.TempDir(), "measure-tasks.db"),
		Logger: quietLog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := sqlitestore.Open(t.Context(), sqlitestore.Config{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := youcam.NewRunner(counting, youcam.RunnerConfig{
		Store: store,
		Meter: meter,
		Media: newMemMedia(t),
		Config: providertask.Config{
			PollBase:    time.Millisecond,
			PollCeiling: 2 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &measureRig{runner: runner, cache: newMemCache(), counting: counting, budget: budget, ledger: ledger}, wire
}

// throttlePace paces the client so a retry, if one ever came, waits
// milliseconds in a test.
func throttlePace() throttle.Config {
	return throttle.Config{Backoff: time.Millisecond, Max: 2 * time.Millisecond}
}

// measureFix is one menu entry carried whole, the shape the pick hands
// over.
func measureFix(t *testing.T, occ *domain.Occasion, id string) *domain.Fix {
	t.Helper()
	for i := range occ.Fixes {
		if occ.Fixes[i].ID == id {
			return &occ.Fixes[i]
		}
	}
	t.Fatalf("fix %q is not on %s's menu", id, occ.ID)
	return nil
}

// v06Photo is the photo every test reads: the committed sample the
// face-attr flow was recorded against.
func v06Photo(user, hash string, image systemone.Image) Photo {
	return Photo{User: user, Hash: hash, Image: image}
}

// TestAHairstyleAsksFaceAttributesOnce is the run-selection proof and the
// cache-hit proof. The hairstyle fix names Face Attributes; the first
// look runs it once - one upload, one create naming features
// ["faceShape"], two polls, no fetch - and settles ten units. The second
// look on the same photo reads the cache and makes zero calls of any
// kind, spends zero units, and returns the same values.
func TestAHairstyleAsksFaceAttributesOnce(t *testing.T) {
	t.Parallel()
	rep := openYoucam(t)
	rig, _ := newMeasureRig(t, rep, 10)
	occ := loadOccasions(t)["date"]
	fix := measureFix(t, occ, "date-tie-hair")
	photo := v06Photo("user-1", "hash-v06", recordedPhoto(t, rep))
	wantKey := analysisKey(photo.User, photo.Hash, analysisTaskName)

	first, err := Measure(t.Context(), rig.runner, rig.cache, photo, fix)
	if err != nil {
		t.Fatal(err)
	}
	if first != (domain.Allowlist{FaceShape: "Oval"}) {
		t.Fatalf("first look measured %+v, want the recorded Oval face shape and nothing else", first)
	}
	uploads, creates, polls, fetches, bodies := rig.counting.tally()
	if uploads != 1 || creates != 1 || polls != 2 || fetches != 0 {
		t.Fatalf("uploads %d creates %d polls %d fetches %d, want one upload, one create, two polls, no fetch", uploads, creates, polls, fetches)
	}
	var body struct {
		SrcFileID string   `json:"src_file_id"`
		Features  []string `json:"features"`
	}
	if len(bodies) != 1 {
		t.Fatalf("create bodies %d, want the one analysis create", len(bodies))
	}
	if err := json.Unmarshal(bodies[0], &body); err != nil {
		t.Fatal(err)
	}
	if body.SrcFileID != recordedFileID(t, rep) {
		t.Fatalf("the create names file id %q, want the recorded upload's", body.SrcFileID)
	}
	if strings.Join(body.Features, ",") != "faceShape" {
		t.Fatalf("the create asks features %v, want [faceShape] and nothing else", body.Features)
	}
	if rig.budget.Spent() != 10 || rig.budget.Reserved() != 0 {
		t.Fatalf("budget spent %d reserved %d, want the ten-unit analysis settled", rig.budget.Spent(), rig.budget.Reserved())
	}
	charges := rig.ledger.Charges()
	if len(charges) != 1 {
		t.Fatalf("charges %d, want the one settle", len(charges))
	}
	if charges[0].Kind != "youcam."+analysisTaskName || charges[0].Ref != wantKey || charges[0].UnitPrice != 10 {
		t.Fatalf("charge %+v, want youcam.%s of 10 units on %s", charges[0], analysisTaskName, wantKey)
	}
	if rig.cache.count() != 1 {
		t.Fatalf("cache rows %d, want the one analysis row", rig.cache.count())
	}
	if held, ok, err := rig.cache.Held(t.Context(), photo.User, photo.Hash, analysisTaskName); err != nil || !ok || held.FaceShape != "Oval" {
		t.Fatalf("held row (%v, %v, %+v), want the cached Oval", err, ok, held)
	}

	// The second look: the same photo, the same fix, zero calls, zero
	// units, the same values.
	second, err := Measure(t.Context(), rig.runner, rig.cache, photo, fix)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("second look measured %+v, want the first look's %+v", second, first)
	}
	if uploads, creates, polls, fetches, _ := rig.counting.tally(); uploads != 1 || creates != 1 || polls != 2 || fetches != 0 {
		t.Fatalf("second look moved the tally to uploads %d creates %d polls %d fetches %d, want it untouched", uploads, creates, polls, fetches)
	}
	if rig.budget.Spent() != 10 || rig.budget.Reserved() != 0 || len(rig.ledger.Charges()) != 1 {
		t.Fatalf("after the second look: spent %d reserved %d over %d charges, want the books untouched", rig.budget.Spent(), rig.budget.Reserved(), len(rig.ledger.Charges()))
	}
	if rig.cache.count() != 1 {
		t.Fatalf("cache rows %d after the second look, want the one row", rig.cache.count())
	}
}

// TestTheCacheIsScopedToTheUser runs the same photo hash under a second
// account: their first look runs the analysis again, because the cache is
// the analysis_cache table's own (user, photo_hash, task) key and two
// users do not share a face reading. The second look of that account then
// spends nothing too.
func TestTheCacheIsScopedToTheUser(t *testing.T) {
	t.Parallel()
	rep := openYoucam(t)
	rig, wire := newMeasureRig(t, rep, 20)
	// A second slot: the second user's first look uploads the photo again.
	wire.enqueue(youcam.Slot{FileID: recordedFileID(t, rep)})
	fix := measureFix(t, loadOccasions(t)["date"], "date-tie-hair")
	image := recordedPhoto(t, rep)

	first, err := Measure(t.Context(), rig.runner, rig.cache, v06Photo("user-1", "hash-v06", image), fix)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Measure(t.Context(), rig.runner, rig.cache, v06Photo("user-2", "hash-v06", image), fix)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("the second user measured %+v, want the same reading %+v", second, first)
	}
	if uploads, creates, polls, fetches, _ := rig.counting.tally(); uploads != 2 || creates != 2 || polls != 4 || fetches != 0 {
		t.Fatalf("uploads %d creates %d polls %d fetches %d, want one analysis per user", uploads, creates, polls, fetches)
	}
	if rig.budget.Spent() != 20 || len(rig.ledger.Charges()) != 2 {
		t.Fatalf("spent %d over %d charges, want one settle per user", rig.budget.Spent(), len(rig.ledger.Charges()))
	}
	wantKeys := map[string]bool{
		analysisKey("user-1", "hash-v06", analysisTaskName): true,
		analysisKey("user-2", "hash-v06", analysisTaskName): true,
	}
	for _, charge := range rig.ledger.Charges() {
		if !wantKeys[charge.Ref] {
			t.Fatalf("charge ref %q, want one of the two users' keys", charge.Ref)
		}
	}
	if rig.cache.count() != 2 {
		t.Fatalf("cache rows %d, want one per user", rig.cache.count())
	}

	third, err := Measure(t.Context(), rig.runner, rig.cache, v06Photo("user-2", "hash-v06", image), fix)
	if err != nil {
		t.Fatal(err)
	}
	if third != second {
		t.Fatalf("the second user's repeat measured %+v, want %+v", third, second)
	}
	if uploads, creates, polls, fetches, _ := rig.counting.tally(); uploads != 2 || creates != 2 || polls != 4 || fetches != 0 {
		t.Fatalf("the repeat moved the tally, want it untouched")
	}
	if rig.budget.Spent() != 20 {
		t.Fatalf("spent %d after the repeat, want 20", rig.budget.Spent())
	}
}

// TestAFixThatNeedsNoAnalysisCallsNothing is the nothing-to-measure
// proof. Every fix on the five menus whose analysis list is empty - the
// clothes, jewellery, lip, beard and smile fixes - measures to an empty
// allowlist with zero calls of any kind, zero units, zero charges and no
// cache row, whether or not a photo is even carried.
func TestAFixThatNeedsNoAnalysisCallsNothing(t *testing.T) {
	t.Parallel()
	rep := openYoucam(t)
	rig, _ := newMeasureRig(t, rep, 10)
	image := recordedPhoto(t, rep)

	var measured []string
	covered := map[string]int{}
	for _, occ := range loadOccasions(t) {
		for i := range occ.Fixes {
			fix := &occ.Fixes[i]
			if len(fix.Analysis) != 0 {
				measured = append(measured, fix.ID)
				continue
			}
			covered[fix.YouCam.Task]++
			photo := v06Photo("user-1", "hash-v06", image)
			got, err := Measure(t.Context(), rig.runner, rig.cache, photo, fix)
			if err != nil {
				t.Fatalf("%s: %v", fix.ID, err)
			}
			if got != (domain.Allowlist{}) {
				t.Fatalf("%s measured %+v, want an empty allowlist", fix.ID, got)
			}
		}
	}
	if uploads, creates, polls, fetches, _ := rig.counting.tally(); uploads != 0 || creates != 0 || polls != 0 || fetches != 0 {
		t.Fatalf("uploads %d creates %d polls %d fetches %d over the sweep, want no call at all", uploads, creates, polls, fetches)
	}
	if rig.budget.Spent() != 0 || rig.budget.Reserved() != 0 || len(rig.ledger.Charges()) != 0 {
		t.Fatalf("spent %d reserved %d over %d charges, want empty books", rig.budget.Spent(), rig.budget.Reserved(), len(rig.ledger.Charges()))
	}
	if rig.cache.count() != 0 {
		t.Fatalf("cache rows %d, want none", rig.cache.count())
	}
	for _, task := range []string{"cloth-v4", "2d-vto/earring", "2d-vto/necklace", "makeup-vto", "beard-style", "ai-smile"} {
		if covered[task] == 0 {
			t.Fatalf("the sweep never covered a %s fix; the zero-call claim must be about real fixes", task)
		}
	}

	// The same nothing-fix with an empty photo and no scope at all: still
	// no call, still an empty allowlist, still no error.
	blank, err := Measure(t.Context(), rig.runner, rig.cache, Photo{}, &domain.Fix{ID: "constructed-nothing"})
	if err != nil {
		t.Fatalf("a nothing-fix on an empty photo: %v", err)
	}
	if blank != (domain.Allowlist{}) {
		t.Fatalf("a nothing-fix measured %+v", blank)
	}
	if uploads, creates, polls, fetches, _ := rig.counting.tally(); uploads != 0 || creates != 0 || polls != 0 || fetches != 0 {
		t.Fatalf("the empty-photo call moved the tally, want it untouched")
	}

	// The data-side pin: the fixes that do name an analysis are exactly
	// the hair fixes, so the hairstyle is the only measuring kind on the
	// menu.
	if len(measured) == 0 {
		t.Fatal("no fix names an analysis; the data lost the hair fixes")
	}
	for _, id := range measured {
		fix := measureFixByName(t, loadOccasions(t), id)
		if fix.YouCam.Task != "hair-transfer" {
			t.Fatalf("%s measures for a %s fix", id, fix.YouCam.Task)
		}
	}
}

// measureFixByName finds one fix across the loaded occasions.
func measureFixByName(t *testing.T, occasions map[string]*domain.Occasion, id string) *domain.Fix {
	t.Helper()
	for _, occ := range occasions {
		for i := range occ.Fixes {
			if occ.Fixes[i].ID == id {
				return &occ.Fixes[i]
			}
		}
	}
	t.Fatalf("fix %q is on no loaded menu", id)
	return nil
}

// TestMeasureRefusesAnAnalysisWithNoRun pins the mapping's edges with
// fixes built in code, the shapes the loader never loads today. A fix
// that names colour tones for a shade is refused, naming the task,
// before any call: skin-tone-analysis has no run on this seam - it is
// off the menu (docs/occasions.md), off the rate card, and has no
// recorded reply to decode - and the refusal is where a future run gets
// added, never a silent skip. The same holds for the three-view
// hair-type-detection, and for Face Attributes asked about a field the
// allowlist does not carry: never agegender.
func TestMeasureRefusesAnAnalysisWithNoRun(t *testing.T) {
	t.Parallel()
	rep := openYoucam(t)
	rig, _ := newMeasureRig(t, rep, 10)
	photo := v06Photo("user-1", "hash-v06", recordedPhoto(t, rep))
	cases := []struct {
		name string
		fix  *domain.Fix
		want string
	}{
		{
			name: "a shade naming colour tones",
			fix: &domain.Fix{ID: "constructed-shade", Analysis: []domain.Analysis{{
				Task: "skin-tone-analysis", UseFields: []string{"colourTones"},
			}}},
			want: `names analysis "skin-tone-analysis", which no task on this seam runs`,
		},
		{
			name: "a hairstyle naming the three-view hair type",
			fix: &domain.Fix{ID: "constructed-hair-type", Analysis: []domain.Analysis{{
				Task: "hair-type-detection", UseFields: []string{"hairType"},
			}}},
			want: `names analysis "hair-type-detection", which no task on this seam runs`,
		},
		{
			name: "face attributes asked about age",
			fix: &domain.Fix{ID: "constructed-age", Analysis: []domain.Analysis{{
				Task: analysisTaskName, UseFields: []string{"age"},
			}}},
			want: `asks for "age", which is not on the allowlist`,
		},
		{
			name: "a field vetoed by never_use",
			fix: &domain.Fix{ID: "constructed-vetoed", Analysis: []domain.Analysis{{
				Task: analysisTaskName, UseFields: []string{"faceShape"}, NeverUse: []string{"faceShape"},
			}}},
			want: "names no field the pipeline may ask",
		},
		{
			name: "an analysis naming no field at all",
			fix: &domain.Fix{ID: "constructed-nofields", Analysis: []domain.Analysis{{
				Task: analysisTaskName,
			}}},
			want: "names no field the pipeline may ask",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Measure(t.Context(), rig.runner, rig.cache, photo, tc.fix)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%v, want an error naming %s", err, tc.want)
			}
			if uploads, creates, polls, fetches, _ := rig.counting.tally(); uploads != 0 || creates != 0 || polls != 0 || fetches != 0 {
				t.Fatalf("uploads %d creates %d polls %d fetches %d, want a refusal before any call", uploads, creates, polls, fetches)
			}
			if rig.budget.Spent() != 0 || len(rig.ledger.Charges()) != 0 {
				t.Fatalf("spent %d over %d charges, want nothing settled", rig.budget.Spent(), len(rig.ledger.Charges()))
			}
			if rig.cache.count() != 0 {
				t.Fatalf("cache rows %d, want none", rig.cache.count())
			}
		})
	}
}

// TestMeasureRefusesAnUnusableCall pins the refusals that come before
// any work: no runner, no cache, no fix, no user, no hash, an empty
// photo, a photo with no content type. A look that lost its bytes must
// fail loudly, never measure, judge or render.
func TestMeasureRefusesAnUnusableCall(t *testing.T) {
	t.Parallel()
	rep := openYoucam(t)
	rig, _ := newMeasureRig(t, rep, 10)
	fix := measureFix(t, loadOccasions(t)["date"], "date-tie-hair")
	image := recordedPhoto(t, rep)
	good := v06Photo("user-1", "hash-v06", image)
	cases := []struct {
		name string
		call func() error
	}{
		{"no runner", func() error {
			_, err := Measure(t.Context(), nil, rig.cache, good, fix)
			return err
		}},
		{"no cache", func() error {
			_, err := Measure(t.Context(), rig.runner, nil, good, fix)
			return err
		}},
		{"no fix", func() error {
			_, err := Measure(t.Context(), rig.runner, rig.cache, good, nil)
			return err
		}},
		{"no user", func() error {
			_, err := Measure(t.Context(), rig.runner, rig.cache, v06Photo("", "hash-v06", image), fix)
			return err
		}},
		{"no hash", func() error {
			_, err := Measure(t.Context(), rig.runner, rig.cache, v06Photo("user-1", "", image), fix)
			return err
		}},
		{"empty photo", func() error {
			_, err := Measure(t.Context(), rig.runner, rig.cache, v06Photo("user-1", "hash-v06", systemone.Image{ContentType: image.ContentType}), fix)
			return err
		}},
		{"no content type", func() error {
			_, err := Measure(t.Context(), rig.runner, rig.cache, v06Photo("user-1", "hash-v06", systemone.Image{Bytes: image.Bytes}), fix)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.call(); err == nil {
				t.Fatal("an unusable call was taken")
			}
		})
	}
	if uploads, creates, polls, fetches, _ := rig.counting.tally(); uploads != 0 || creates != 0 || polls != 0 || fetches != 0 {
		t.Fatalf("uploads %d creates %d polls %d fetches %d, want no call from any refusal", uploads, creates, polls, fetches)
	}
	if rig.cache.count() != 0 {
		t.Fatalf("cache rows %d, want none", rig.cache.count())
	}
}
