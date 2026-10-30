package youcam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"

	"sync"
	"testing"
	"time"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/mediastore"
	"github.com/nrynss/keel/providertask"
	"github.com/nrynss/keel/providertask/sqlitestore"
	"github.com/nrynss/keel/sqlite"
)

// testKey stands in for the API key in every client test. It is never a
// real key and never appears in an error the tests accept.
const testKey = "youcam-test-api-key"

// repoRoot is the repository root, from this file's own location.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func recordingsDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "testdata", "youcam")
}

func openRecordings(t *testing.T) *Replay {
	t.Helper()
	replay, err := Open(recordingsDir(t))
	if err != nil {
		t.Fatal(err)
	}
	return replay
}

// recordedFile rebuilds the File an upload recording answers, bytes in line.
func recordedFile(t *testing.T, rep *Replay, key string) File {
	t.Helper()
	rec, err := rep.Reply(key)
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	file, ok := decodeAs[File](rec.Request)
	if !ok {
		t.Fatalf("%s: recording is not an upload", key)
	}
	return file
}

// recordedTask rebuilds the Task a create recording answers.
func recordedTask(t *testing.T, rep *Replay, key string) Task {
	t.Helper()
	rec, err := rep.Reply(key)
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	task, ok := decodeAs[Task](rec.Request)
	if !ok {
		t.Fatalf("%s: recording is not a create", key)
	}
	return task
}

func recordedCreated(t *testing.T, rep *Replay, key string) Created {
	t.Helper()
	rec, err := rep.Reply(key)
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	created, ok := decodeAs[Created](rec.Response)
	if !ok {
		t.Fatalf("%s: recording is not a created task", key)
	}
	return created
}

func recordedSlot(t *testing.T, rep *Replay, key string) Slot {
	t.Helper()
	rec, err := rep.Reply(key)
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	slot, ok := decodeAs[Slot](rec.Response)
	if !ok {
		t.Fatalf("%s: recording is not an upload", key)
	}
	return slot
}

func recordedStatus(t *testing.T, rep *Replay, key string) Status {
	t.Helper()
	rec, err := rep.Reply(key)
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	status, ok := decodeAs[Status](rec.Response)
	if !ok {
		t.Fatalf("%s: recording is not a poll", key)
	}
	return status
}

// recordedResult carries the recorded result bytes, in line since Open.
func recordedResult(t *testing.T, rep *Replay, key string) Result {
	t.Helper()
	rec, err := rep.Reply(key)
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	result, ok := decodeAs[Result](rec.Response)
	if !ok {
		t.Fatalf("%s: recording is not a result", key)
	}
	return result
}

// recordedWireBody is the response body of an http-* recording.
func recordedWireBody(t *testing.T, rep *Replay, key string) []byte {
	t.Helper()
	rec, err := rep.Reply(key)
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	var envelope struct {
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(rec.Response, &envelope); err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	return envelope.Body
}

// flowCase is one recorded task flow: the uploads it made, and the create,
// poll and fetch that followed. A flow with no typed upload (the second
// run of one picture, the never-PUT file) carries a pinned payload: its
// create body names file ids no recording can upload again.
type flowCase struct {
	name    string
	task    string
	uploads []string
	create  string
	poll    string
	fetch   string
	units   cost.Price
}

var flows = []flowCase{
	{name: "smile", task: "ai-smile", uploads: []string{"smile-upload-src"}, create: "smile-create", poll: "smile-poll", fetch: "smile-fetch", units: 1},
	{name: "lip-red", task: "makeup-vto", uploads: []string{"lip-red-upload-src"}, create: "lip-red-create", poll: "lip-red-poll", fetch: "lip-red-fetch", units: 1},
	{name: "lip-default-smooth", task: "makeup-vto", create: "lip-red-default-smooth-create", poll: "lip-red-default-smooth-poll", fetch: "lip-red-default-smooth-fetch", units: 1},
	{name: "earring", task: "2d-vto/earring", uploads: []string{"earring-upload-src", "earring-upload-ref"}, create: "earring-create", poll: "earring-poll", fetch: "earring-fetch", units: 1},
	{name: "necklace", task: "2d-vto/necklace", uploads: []string{"necklace-upload-src", "necklace-upload-ref"}, create: "necklace-create", poll: "necklace-poll", fetch: "necklace-fetch", units: 1},
	{name: "necklace-auto", task: "2d-vto/necklace", create: "necklace-auto-create", poll: "necklace-auto-poll", fetch: "necklace-auto-fetch", units: 1},
	{name: "cloth", task: "cloth-v4", uploads: []string{"cloth-upload-src", "cloth-upload-ref"}, create: "cloth-create", poll: "cloth-poll", fetch: "cloth-fetch", units: 2},
	{name: "hair-updo", task: "hair-transfer", uploads: []string{"hair-updo-upload-src"}, create: "hair-updo-create", poll: "hair-updo-poll", fetch: "hair-updo-fetch", units: 2},
	{name: "hair-ponytail", task: "hair-transfer", create: "hair-ponytail-create", poll: "hair-ponytail-poll", fetch: "hair-ponytail-fetch", units: 2},
	{name: "beard", task: "beard-style", uploads: []string{"beard-upload-src"}, create: "beard-create", poll: "beard-poll", fetch: "beard-fetch", units: 2},
	{name: "face-attr", task: "face-attr-analysis", uploads: []string{"face-attr-upload-src"}, create: "face-attr-create", poll: "face-attr-poll", units: 10},
}

// stepClient counts what the runner asked its client for, can script poll
// answers ahead of the recordings, and can stand in for the fetch leg.
// Everything else is delegated.
type stepClient struct {
	YouCam

	fetch func(ctx context.Context, ref ResultRef) (Result, error)

	mu      sync.Mutex
	steps   []func() (Status, error)
	next    int
	uploads int
	creates int
	polls   int
	fetches int
}

func newStepClient(rep *Replay) *stepClient {
	return &stepClient{YouCam: rep}
}

func (s *stepClient) counts() (uploads, creates, polls, fetches int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.uploads, s.creates, s.polls, s.fetches
}

func (s *stepClient) Upload(ctx context.Context, file File) (Slot, error) {
	s.mu.Lock()
	s.uploads++
	s.mu.Unlock()
	return s.YouCam.Upload(ctx, file)
}

func (s *stepClient) Create(ctx context.Context, task Task) (Created, error) {
	s.mu.Lock()
	s.creates++
	s.mu.Unlock()
	return s.YouCam.Create(ctx, task)
}

func (s *stepClient) Poll(ctx context.Context, ref PollRef) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	s.mu.Lock()
	s.polls++
	var step func() (Status, error)
	if s.next < len(s.steps) {
		step = s.steps[s.next]
		s.next++
	}
	s.mu.Unlock()
	if step != nil {
		return step()
	}
	return s.YouCam.Poll(ctx, ref)
}

func (s *stepClient) Fetch(ctx context.Context, ref ResultRef) (Result, error) {
	s.mu.Lock()
	s.fetches++
	standIn := s.fetch
	s.mu.Unlock()
	if standIn != nil {
		return standIn(ctx, ref)
	}
	return s.YouCam.Fetch(ctx, ref)
}

// mediaFakes is the in-memory mediastore the tests use, the shape the
// server's delete tests fake their index with, plus the bytes behind it.
type mediaFakes struct {
	store  *mediastore.Store
	index  *memIndex
	blocks *memBackend
}

func newMedia(t *testing.T) mediaFakes {
	t.Helper()
	fakes := mediaFakes{index: &memIndex{rows: map[string]mediastore.Blob{}}, blocks: &memBackend{blobs: map[string][]byte{}}}
	store, err := mediastore.Open(t.Context(), mediastore.Config{Index: fakes.index, Backend: fakes.blocks, Log: discardLog()})
	if err != nil {
		t.Fatal(err)
	}
	fakes.store = store
	return fakes
}

// blobBytes reads one stored blob back, or fails when it is absent.
func (f mediaFakes) blobBytes(t *testing.T, id string) []byte {
	t.Helper()
	f.index.mu.Lock()
	_, ok := f.index.rows[id]
	f.index.mu.Unlock()
	if !ok {
		t.Fatalf("blob %s is not in the index", id)
	}
	reader, err := f.blocks.Open(t.Context(), id)
	if err != nil {
		t.Fatalf("blob %s: %v", id, err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (f mediaFakes) count() int {
	f.index.mu.Lock()
	defer f.index.mu.Unlock()
	return len(f.index.rows)
}

type memIndex struct {
	mu   sync.Mutex
	rows map[string]mediastore.Blob
}

func (m *memIndex) Create(ctx context.Context, b mediastore.Blob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[b.ID]; ok {
		return mediastore.ErrAlreadyExists
	}
	m.rows[b.ID] = b
	return nil
}

func (m *memIndex) Get(ctx context.Context, id string) (mediastore.Blob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.rows[id]
	if !ok {
		return mediastore.Blob{}, mediastore.ErrNotFound
	}
	return b, nil
}

func (m *memIndex) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[id]; !ok {
		return mediastore.ErrNotFound
	}
	delete(m.rows, id)
	return nil
}

func (m *memIndex) DeleteGroup(ctx context.Context, group string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	found := false
	for id, b := range m.rows {
		if b.Group == group {
			found = true
			delete(m.rows, id)
		}
	}
	if !found {
		return mediastore.ErrNotFound
	}
	return nil
}

func (m *memIndex) Groups(ctx context.Context) ([]mediastore.Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	byGroup := map[string]mediastore.Group{}
	for _, b := range m.rows {
		g := byGroup[b.Group]
		g.ID = b.Group
		g.Blobs = append(g.Blobs, b)
		byGroup[b.Group] = g
	}
	out := make([]mediastore.Group, 0, len(byGroup))
	for _, g := range byGroup {
		out = append(out, g)
	}
	return out, nil
}

func (m *memIndex) Blobs(ctx context.Context) ([]mediastore.Blob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]mediastore.Blob, 0, len(m.rows))
	for _, b := range m.rows {
		out = append(out, b)
	}
	return out, nil
}

type memBackend struct {
	mu    sync.Mutex
	blobs map[string][]byte
}

func (m *memBackend) Open(ctx context.Context, id string) (mediastore.BlobReader, error) {
	m.mu.Lock()
	data, ok := m.blobs[id]
	m.mu.Unlock()
	if !ok {
		return nil, mediastore.ErrNotFound
	}
	return nopCloser{bytes.NewReader(data)}, nil
}

func (m *memBackend) Write(ctx context.Context, id string, src io.Reader) (int64, error) {
	data, err := io.ReadAll(src)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.blobs[id]; ok {
		return 0, mediastore.ErrAlreadyExists
	}
	m.blobs[id] = data
	return int64(len(data)), nil
}

func (m *memBackend) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.blobs[id]; !ok {
		return mediastore.ErrNotFound
	}
	delete(m.blobs, id)
	return nil
}

func (m *memBackend) List(ctx context.Context) ([]mediastore.Object, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]mediastore.Object, 0, len(m.blobs))
	for id, data := range m.blobs {
		out = append(out, mediastore.Object{ID: id, SizeBytes: int64(len(data))})
	}
	return out, nil
}

type nopCloser struct{ io.ReadSeeker }

func (nopCloser) Close() error { return nil }

func discardLog() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// newUnitsMeter is a meter over a budget of limit youcam units and the
// ledger every charge lands in.
func newUnitsMeter(t *testing.T, limit cost.Price) (*cost.Meter, *cost.Budget, *cost.Ledger) {
	t.Helper()
	budget, err := cost.NewBudgetIn(UnitsDenomination, limit)
	if err != nil {
		t.Fatal(err)
	}
	ledger := cost.NewLedger()
	meter, err := cost.NewMeter(budget, ledger)
	if err != nil {
		t.Fatal(err)
	}
	return meter, budget, ledger
}

// newStore is a providertask store over a real Keel sqlite file, so the
// resume tests carry the restart promise the production store makes.
func newStore(t *testing.T) *sqlitestore.Store {
	t.Helper()
	db, err := sqlite.Open(t.Context(), sqlite.Config{
		Path:   filepath.Join(t.TempDir(), "tasks.db"),
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := sqlitestore.Open(t.Context(), sqlitestore.Config{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// newRunner builds the runner over client with its own meter, store and
// media fakes, paced so the poll loop waits milliseconds.
func newRunner(t *testing.T, client YouCam) (*Runner, *cost.Budget, *cost.Ledger, mediaFakes, *sqlitestore.Store) {
	t.Helper()
	meter, budget, ledger := newUnitsMeter(t, 10_000)
	fakes := newMedia(t)
	store := newStore(t)
	runner, err := NewRunner(client, RunnerConfig{
		Store: store,
		Meter: meter,
		Media: fakes.store,
		Config: providertask.Config{
			PollBase:    time.Millisecond,
			PollCeiling: 2 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return runner, budget, ledger, fakes, store
}

// flowJob builds the job one recorded flow runs under: the recorded files,
// the recorded payload, and the key that names the flow.
func flowJob(t *testing.T, flow flowCase, rep *Replay) Job {
	t.Helper()
	job := Job{Key: "look-" + flow.name, Task: flow.task, Owner: "user-1", Group: "look-" + flow.name}
	for _, key := range flow.uploads {
		job.Files = append(job.Files, recordedFile(t, rep, key))
	}
	task := recordedTask(t, rep, flow.create)
	job.Payload = func([]string) (json.RawMessage, error) {
		// The replay's uploads hand out the recorded file ids, so the
		// recorded payload is the payload this run creates with.
		return task.Payload, nil
	}
	return job
}

// TestRunnerCarriesEveryRecordedFlow replays every task flow T0.5
// recorded through the real runner over the typed recordings: upload,
// create, poll, fetch, keep, and the one metered charge. This is the
// success class of the done-when.
func TestRunnerCarriesEveryRecordedFlow(t *testing.T) {
	t.Parallel()
	rep := openRecordings(t)
	for _, flow := range flows {
		t.Run(flow.name, func(t *testing.T) {
			t.Parallel()
			client := newStepClient(rep)
			runner, budget, ledger, fakes, store := newRunner(t, client)
			job := flowJob(t, flow, rep)

			out, err := runner.Run(t.Context(), job)
			if err != nil {
				t.Fatalf("%s: %v", flow.name, err)
			}
			created := recordedCreated(t, rep, flow.create)
			if out.TaskID != created.TaskID {
				t.Fatalf("task id %q, want %q", out.TaskID, created.TaskID)
			}
			uploads, creates, polls, fetches := client.counts()
			if uploads != len(flow.uploads) || creates != 1 {
				t.Fatalf("uploads %d creates %d, want %d and 1", uploads, creates, len(flow.uploads))
			}
			// One status poll and one more for a fresh result link: polling
			// a finished task is free, and the runner takes the fresh link
			// every time, so a lapsed one is never handed on.
			if polls != 2 {
				t.Fatalf("polls %d, want 2", polls)
			}

			if flow.fetch == "" {
				// The analysis flow: the result is the face shape alone,
				// nothing is fetched and nothing is copied into mediastore.
				if fetches != 0 {
					t.Fatalf("analysis fetched %d times", fetches)
				}
				if out.MediaID != "" || fakes.count() != 0 {
					t.Fatalf("analysis kept a blob: %q, %d", out.MediaID, fakes.count())
				}
			} else {
				if fetches != 1 {
					t.Fatalf("fetches %d, want 1", fetches)
				}
				want := recordedResult(t, rep, flow.fetch)
				if out.MediaID == "" {
					t.Fatal("no media id")
				}
				if got := fakes.blobBytes(t, out.MediaID); !bytes.Equal(got, want.Bytes) {
					t.Fatalf("kept %d bytes, want the recorded %d", len(got), len(want.Bytes))
				}
				if len(out.MediaID) != 32 {
					t.Fatalf("media id %q is not an id-shaped name", out.MediaID)
				}
			}

			// The one charge: the rate-card line, in youcam units, under
			// the job's key.
			spent := budget.Spent()
			if spent != flow.units {
				t.Fatalf("budget spent %d, want %d units", spent, flow.units)
			}
			charges := ledger.Charges()
			if len(charges) != 1 {
				t.Fatalf("charges %d, want the one settle", len(charges))
			}
			charge := charges[0]
			if charge.Kind != "youcam."+flow.task || charge.Ref != job.Key ||
				charge.UnitPrice != flow.units || charge.Denomination != UnitsDenomination {
				t.Fatalf("charge %+v, want youcam.%s of %d units on %s", charge, flow.task, flow.units, job.Key)
			}
			if reserved := budget.Reserved(); reserved != 0 {
				t.Fatalf("budget holds %d after the run", reserved)
			}
			claim, err := store.Get(t.Context(), job.Key)
			if err != nil {
				t.Fatal(err)
			}
			if claim.State != providertask.StateSucceeded || claim.TaskID != created.TaskID {
				t.Fatalf("claim %+v, want a succeeded %s", claim, created.TaskID)
			}
		})
	}
}

// TestEngineErrorsAreTypedAndSpendNothing replays the four recorded engine
// failures: no face, a bad file, an image too small, and a file whose
// bytes never landed. Each ends the task typed, refunds what the provider
// charged (a net zero), and keeps nothing. This is the error class of the
// done-when.
func TestEngineErrorsAreTypedAndSpendNothing(t *testing.T) {
	t.Parallel()
	rep := openRecordings(t)
	cases := []struct {
		name   string
		upload string // empty when the recorded run uploaded nothing
		create string
		want   *Error
	}{
		{"no face", "err-no-face-upload-src", "err-no-face-create", &Error{Kind: KindNoFace, Detail: "error_no_face"}},
		{"bad file", "err-bad-file-upload-src", "err-bad-file-create", &Error{Kind: KindBadFile, Detail: "error_decode_image"}},
		{"too small", "err-too-small-upload-src", "err-too-small-create", &Error{Kind: KindBadFile, Detail: "error_below_min_image_size"}},
		{"file not in storage", "", "err-not-uploaded-create", &Error{Kind: KindExpired, Detail: "error_download_image"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := newStepClient(rep)
			runner, budget, ledger, fakes, store := newRunner(t, client)
			task := recordedTask(t, rep, tc.create)
			job := Job{
				Key:     "look-err",
				Task:    task.Name,
				Payload: func([]string) (json.RawMessage, error) { return task.Payload, nil },
			}
			if tc.upload != "" {
				job.Files = []File{recordedFile(t, rep, tc.upload)}
			}

			_, err := runner.Run(t.Context(), job)
			if !errors.Is(err, tc.want) {
				t.Fatalf("%v, want %s", err, tc.want.Kind)
			}
			var ye *Error
			if !errors.As(err, &ye) || ye.Detail != tc.want.Detail {
				t.Fatalf("detail %v, want %s", err, tc.want.Detail)
			}

			uploads, creates, polls, fetches := client.counts()
			if creates != 1 || polls != 1 || fetches != 0 {
				t.Fatalf("uploads %d creates %d polls %d fetches %d, want one create and one poll", uploads, creates, polls, fetches)
			}
			if budget.Spent() != 0 || len(ledger.Charges()) != 0 {
				t.Fatalf("spent %d over %d charges, want a refunded net zero", budget.Spent(), len(ledger.Charges()))
			}
			if fakes.count() != 0 {
				t.Fatal("a failed task kept a blob")
			}
			claim, err := store.Get(t.Context(), job.Key)
			if err != nil {
				t.Fatal(err)
			}
			if claim.State != providertask.StateFailed || claim.Code != tc.want.Detail {
				t.Fatalf("claim %+v, want failed with %s", claim, tc.want.Detail)
			}
		})
	}
}

// TestACreateRefusalSpendsNothingAndKeepsTheClaim runs the runner against
// the recorded InvalidParameters refusal (a file id that was never
// issued): the refusal is not retried, the reservation is freed, and
// nothing was kept. The claim stays, because the provider may have created
// a task even though the answer was lost, and a re-run of the same key
// refuses pending instead of creating again; Store.Release is the
// deliberate recovery, which stays the caller's call.
func TestACreateRefusalSpendsNothingAndKeepsTheClaim(t *testing.T) {
	t.Parallel()
	rep := openRecordings(t)
	h := newHarness(t, rep)
	h.steps = []wireStep{{
		status: 400,
		body:   recordedWireBody(t, rep, "http-err-unknown-file-id"),
	}}
	client := h.client(t)
	meter, budget, ledger := newUnitsMeter(t, 10_000)
	fakes := newMedia(t)
	store := newStore(t)
	runner, err := NewRunner(client, RunnerConfig{
		Store: store,
		Meter: meter,
		Media: fakes.store,
		Config: providertask.Config{
			PollBase:    time.Millisecond,
			PollCeiling: 2 * time.Millisecond,
			Deadline:    20 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	job := Job{
		Key:  "look-refused",
		Task: "ai-smile",
		Payload: func([]string) (json.RawMessage, error) {
			return json.RawMessage(`{"src_file_id":"not-a-file-id","expression_type":"closed_mouth_smile"}`), nil
		},
	}

	_, err = runner.Run(t.Context(), job)
	var refused *HTTPError
	if !errors.As(err, &refused) || refused.Code != 400 || refused.Name != "InvalidParameters" {
		t.Fatalf("%v, want the recorded 400 InvalidParameters", err)
	}
	if budget.Spent() != 0 || len(ledger.Charges()) != 0 {
		t.Fatalf("spent %d over %d charges, want nothing", budget.Spent(), len(ledger.Charges()))
	}
	if fakes.count() != 0 {
		t.Fatal("a refused create kept a blob")
	}
	if calls := h.apiCount(); calls != 1 { // the refused create; the job uploads nothing
		t.Fatalf("api calls %d, want the one refused create", calls)
	}
	claim, err := store.Get(t.Context(), job.Key)
	if err != nil {
		t.Fatal(err)
	}
	if claim.TaskID != "" || claim.State != providertask.StateRunning {
		t.Fatalf("claim %+v, want an empty running claim", claim)
	}

	// A re-run of the burned key waits instead of creating again, and no
	// new call leaves the process.
	if _, err := runner.Run(t.Context(), job); !errors.Is(err, providertask.ErrTaskPending) {
		t.Fatalf("%v, want the burned key refused pending", err)
	}
	if calls := h.apiCount(); calls != 1 {
		t.Fatalf("api calls %d after the pending refusal, want no new create", calls)
	}
}

// TestARestartMidPollResumesWithoutASecondCreate drives a first Run into
// the recorded task and cancels it while the task still runs, then runs
// the same key again: the second Run takes over the recorded task, never
// creates again, and the books carry exactly one charge. This is the
// restart class of the done-when.
func TestARestartMidPollResumesWithoutASecondCreate(t *testing.T) {
	t.Parallel()
	rep := openRecordings(t)
	flow := flows[0] // the smile flow
	client := newStepClient(rep)
	runner, budget, ledger, fakes, store := newRunner(t, client)
	job := flowJob(t, flow, rep)
	created := recordedCreated(t, rep, flow.create)

	ctx, cancel := context.WithCancel(context.Background())
	client.mu.Lock()
	client.steps = []func() (Status, error){func() (Status, error) {
		cancel() // the process dies mid-poll
		return Status{TaskID: created.TaskID, Status: StatusRunning}, nil
	}}
	client.mu.Unlock()

	_, firstErr := runner.Run(ctx, job)
	if !errors.Is(firstErr, context.Canceled) {
		t.Fatalf("%v, want the cancelled first run", firstErr)
	}
	if _, creates, _, _ := client.counts(); creates != 1 {
		t.Fatalf("creates %d after the first run", creates)
	}
	claim, err := store.Get(context.Background(), job.Key)
	if err != nil {
		t.Fatal(err)
	}
	if claim.TaskID != created.TaskID || claim.State != providertask.StateRunning {
		t.Fatalf("claim %+v, want the recorded running task", claim)
	}
	if budget.Spent() != 0 || len(ledger.Charges()) != 0 {
		t.Fatalf("spent %d over %d charges before any verdict", budget.Spent(), len(ledger.Charges()))
	}

	// The restart: the same key, a fresh process's worth of context.
	out, err := runner.Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if out.TaskID != created.TaskID {
		t.Fatalf("resumed task %q, want %q", out.TaskID, created.TaskID)
	}
	uploads, creates, polls, fetches := client.counts()
	if uploads != len(flow.uploads) || creates != 1 {
		t.Fatalf("uploads %d creates %d after the resume, want the first run's uploads and exactly one create", uploads, creates)
	}
	if polls != 3 || fetches != 1 {
		t.Fatalf("polls %d fetches %d, want the running poll, the verdict and the fresh link, then one fetch", polls, fetches)
	}
	want := recordedResult(t, rep, flow.fetch)
	if got := fakes.blobBytes(t, out.MediaID); !bytes.Equal(got, want.Bytes) {
		t.Fatalf("kept %d bytes, want the recorded %d", len(got), len(want.Bytes))
	}
	if budget.Spent() != flow.units || len(ledger.Charges()) != 1 {
		t.Fatalf("spent %d over %d charges, want one settle of %d", budget.Spent(), len(ledger.Charges()), flow.units)
	}
	if ledger.Charges()[0].Ref != job.Key {
		t.Fatalf("charge ref %q, want %q", ledger.Charges()[0].Ref, job.Key)
	}
}

// TestABudgetRefusalSpendsNothing runs a job against a budget smaller than
// the task's rate-card line: the reserve is refused before anything is
// uploaded, the empty claim is released, and a funded run of the same key
// succeeds afterwards. This is the budget class of the done-when.
func TestABudgetRefusalSpendsNothing(t *testing.T) {
	t.Parallel()
	rep := openRecordings(t)
	flow := flows[6] // cloth: two units
	client := newStepClient(rep)
	meter, budget, ledger := newUnitsMeter(t, 1) // one unit short of the quote
	fakes := newMedia(t)
	store := newStore(t)
	runner, err := NewRunner(client, RunnerConfig{
		Store:  store,
		Meter:  meter,
		Media:  fakes.store,
		Config: providertask.Config{PollBase: time.Millisecond, PollCeiling: 2 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	job := flowJob(t, flow, rep)

	_, err = runner.Run(t.Context(), job)
	if !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("%v, want the budget refusal", err)
	}
	uploads, creates, polls, fetches := client.counts()
	if uploads != 0 || creates != 0 || polls != 0 || fetches != 0 {
		t.Fatalf("uploads %d creates %d polls %d fetches %d, want nothing at all", uploads, creates, polls, fetches)
	}
	if budget.Spent() != 0 || budget.Reserved() != 0 || len(ledger.Charges()) != 0 {
		t.Fatalf("budget spent %d reserved %d, charges %d, want empty books", budget.Spent(), budget.Reserved(), len(ledger.Charges()))
	}
	if _, err := store.Get(t.Context(), job.Key); !errors.Is(err, providertask.ErrUnknownKey) {
		t.Fatalf("claim %v, want the empty claim released", err)
	}
	if fakes.count() != 0 {
		t.Fatal("a refused run kept a blob")
	}
}

// TestAnExpiredLinkIsRecoveredWithAFreshPoll replays the recorded result
// link that lived past its two hours: the fetch answers the recorded 403
// AccessDenied, the runner polls the finished task once more, which is
// free, and the fresh link fetches the same picture.
func TestAnExpiredLinkIsRecoveredWithAFreshPoll(t *testing.T) {
	t.Parallel()
	rep := openRecordings(t)
	flow := flows[0]
	client := newStepClient(rep)
	runner, budget, ledger, fakes, _ := newRunner(t, client)
	job := flowJob(t, flow, rep)

	// The wrapper answers the first fetch with what the recorded 403 maps
	// to: the recorded AccessDenied body is the typed ErrExpired, and the
	// client-level replay pins that mapping.
	expired := recordedWireBody(t, rep, "http-err-expired-result-link")
	if !bytes.Contains(expired, []byte("AccessDenied")) {
		t.Fatalf("the recorded refusal is not the expired grant: %s", expired)
	}
	var once sync.Once
	client.fetch = func(ctx context.Context, ref ResultRef) (Result, error) {
		var mapped error
		once.Do(func() { mapped = ErrExpired })
		if mapped != nil {
			return Result{}, mapped
		}
		return client.YouCam.Fetch(ctx, ref)
	}

	out, err := runner.Run(t.Context(), job)
	if err != nil {
		t.Fatal(err)
	}
	_, creates, polls, fetches := client.counts()
	if creates != 1 || polls != 3 || fetches != 2 {
		t.Fatalf("creates %d polls %d fetches %d, want the fresh-link poll and the second fetch", creates, polls, fetches)
	}
	want := recordedResult(t, rep, flow.fetch)
	if got := fakes.blobBytes(t, out.MediaID); !bytes.Equal(got, want.Bytes) {
		t.Fatalf("kept %d bytes, want the recorded %d", len(got), len(want.Bytes))
	}
	if budget.Spent() != 1 || len(ledger.Charges()) != 1 {
		t.Fatalf("spent %d over %d charges, want one settle", budget.Spent(), len(ledger.Charges()))
	}
}

// TestTheAnalysisCarriesFaceShapeOnly runs the analysis flow over the wire
// recordings and pins the contract change: the poll decodes faceshape and
// nothing else, because the seam carries face shape alone.
func TestTheAnalysisCarriesFaceShapeOnly(t *testing.T) {
	t.Parallel()
	rep := openRecordings(t)
	flow := flows[10] // face-attr
	h := newHarness(t, rep)
	for _, key := range flow.uploads {
		h.enqueueSlot(recordedSlot(t, rep, key))
	}
	client := h.client(t)

	st, err := client.Poll(t.Context(), PollRef{
		Task:   flow.task,
		TaskID: recordedCreated(t, rep, flow.create).TaskID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusSuccess {
		t.Fatalf("status %q", st.Status)
	}
	if st.FaceShape != "Oval" {
		t.Fatalf("face shape %q, want the recorded Oval", st.FaceShape)
	}
	if st.ResultURL != "" {
		t.Fatalf("the analysis named a result link %q", st.ResultURL)
	}
}

// TestTheRunnerFacesTheWire runs one render flow and the analysis flow
// through the runner and the live client over the recorded wire, so the
// client, the meter, the store and the keep all meet the recorded replies.
func TestTheRunnerFacesTheWire(t *testing.T) {
	t.Parallel()
	rep := openRecordings(t)
	t.Run("render", func(t *testing.T) {
		t.Parallel()
		flow := flows[0]
		h := newHarness(t, rep)
		h.enqueueSlot(recordedSlot(t, rep, flow.uploads[0]))
		runner, budget, ledger, fakes, _ := newRunner(t, h.client(t))
		out, err := runner.Run(t.Context(), flowJob(t, flow, rep))
		if err != nil {
			t.Fatal(err)
		}
		want := recordedResult(t, rep, flow.fetch)
		if got := fakes.blobBytes(t, out.MediaID); !bytes.Equal(got, want.Bytes) {
			t.Fatalf("kept %d bytes, want the recorded %d", len(got), len(want.Bytes))
		}
		if budget.Spent() != 1 || len(ledger.Charges()) != 1 {
			t.Fatalf("spent %d over %d charges", budget.Spent(), len(ledger.Charges()))
		}
	})
	t.Run("analysis", func(t *testing.T) {
		t.Parallel()
		flow := flows[10]
		h := newHarness(t, rep)
		h.enqueueSlot(recordedSlot(t, rep, flow.uploads[0]))
		runner, budget, ledger, fakes, _ := newRunner(t, h.client(t))
		out, err := runner.Run(t.Context(), flowJob(t, flow, rep))
		if err != nil {
			t.Fatal(err)
		}
		if out.FaceShape != "Oval" || out.MediaID != "" || fakes.count() != 0 {
			t.Fatalf("outcome %+v with %d blobs, want the face shape alone", out, fakes.count())
		}
		if budget.Spent() != 10 || len(ledger.Charges()) != 1 {
			t.Fatalf("spent %d over %d charges, want the 10-unit analysis", budget.Spent(), len(ledger.Charges()))
		}
	})
}

// TestRateCardAndKeepID pin the quote table and the derived blob id.
func TestRateCardAndKeepID(t *testing.T) {
	t.Parallel()
	for task, units := range map[string]cost.Price{
		"ai-smile": 1, "makeup-vto": 1, "2d-vto/earring": 1, "2d-vto/necklace": 1,
		"cloth-v4": 2, "hair-transfer": 2, "beard-style": 2, "face-attr-analysis": 10,
	} {
		if got, ok := UnitsFor(task); !ok || got != units {
			t.Fatalf("%s quotes %d/%v", task, got, ok)
		}
	}
	if _, ok := UnitsFor("no-such-task"); ok {
		t.Fatal("an unknown task quoted")
	}
	id := keepID("look-1")
	if len(id) != 32 {
		t.Fatalf("keep id %q is not 32 hex characters", id)
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Fatalf("keep id %q is not lowercase hex", id)
		}
	}
	if keepID("look-1") != id {
		t.Fatal("keep id is not derived from the key")
	}
	if keepID("look-2") == id {
		t.Fatal("two keys share a keep id")
	}
}

// TestNewRunnerRefusesAnUnmeteredBuild pins that a runner without its
// meter, store, media store or client never exists.
func TestNewRunnerRefusesAnUnmeteredBuild(t *testing.T) {
	t.Parallel()
	meter, _, _ := newUnitsMeter(t, 100)
	fakes := newMedia(t)
	store := newStore(t)
	if _, err := NewRunner(nil, RunnerConfig{Store: store, Meter: meter, Media: fakes.store}); err == nil {
		t.Fatal("nil client accepted")
	}
	rep := openRecordings(t)
	if _, err := NewRunner(rep, RunnerConfig{Meter: meter, Media: fakes.store}); err == nil {
		t.Fatal("nil store accepted")
	}
	if _, err := NewRunner(rep, RunnerConfig{Store: store, Media: fakes.store}); err == nil {
		t.Fatal("nil meter accepted")
	}
	if _, err := NewRunner(rep, RunnerConfig{Store: store, Meter: meter}); err == nil {
		t.Fatal("nil media store accepted")
	}
}
