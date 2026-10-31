package pipeline

// The render tests replay the recorded YouCam flows through the T2.2
// runner over the typed recordings, and pin the three things the
// done-when names: every fix kind renders on its recordings, the product
// rule picks the brought product before the basket's, and a budget
// refusal spends nothing while the suggestion stands. No test dials the
// provider and no unit is spent: the only units that move are the fake
// ledger's.
//
// The proof is byte-exact by construction. The typed replay looks a
// create up by the canonical JSON of its payload, so a Render that
// succeeds against the recordings built the recorded body - a wrong
// field, a missing zero on skin_smooth, a guessed necklace coordinate
// would all miss and fail the test as a Missing recording.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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
)

// renderReplay is the one open of testdata/youcam the render tests
// share. Open hashes every recording's inline image bytes and costs
// seconds under -race; the recordings are read-only once open, so one
// open serves every test in this file.
var (
	renderReplayOnce sync.Once
	renderReplay     *youcam.Replay
	renderReplayErr  error
)

func openRenderReplay(t *testing.T) *youcam.Replay {
	t.Helper()
	renderReplayOnce.Do(func() {
		renderReplay, renderReplayErr = youcam.Open(filepath.Join(repoRoot(t), "testdata", "youcam"))
	})
	if renderReplayErr != nil {
		t.Fatalf("open testdata/youcam: %v", renderReplayErr)
	}
	return renderReplay
}

// recordedImage is the picture one typed upload recording names. Open
// has already read the file the recording points at, so this is the
// committed sample or product photo as the flow was recorded against.
func recordedImage(t *testing.T, rep *youcam.Replay, key string) systemone.Image {
	t.Helper()
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

// recordedCreateTask is the create recording one flow made.
func recordedCreateTask(t *testing.T, rep *youcam.Replay, key string) youcam.Task {
	t.Helper()
	rec, err := rep.Reply(key)
	if err != nil {
		t.Fatal(err)
	}
	var task youcam.Task
	if err := json.Unmarshal(rec.Request, &task); err != nil {
		t.Fatalf("%s is not a create recording: %v", key, err)
	}
	return task
}

// recordedRenderBytes is the result picture one typed fetch recording
// kept.
func recordedRenderBytes(t *testing.T, rep *youcam.Replay, key string) []byte {
	t.Helper()
	rec, err := rep.Reply(key)
	if err != nil {
		t.Fatal(err)
	}
	var result youcam.Result
	if err := json.Unmarshal(rec.Response, &result); err != nil {
		t.Fatalf("%s is not a fetch recording: %v", key, err)
	}
	if len(result.Bytes) == 0 {
		t.Fatalf("%s carries no picture", key)
	}
	return result.Bytes
}

// createFileIDs reads the file ids one recorded create body names, in
// the order the flow uploaded them: the selfie, then the product when
// the flow takes one. The slot leg of the recorded flows is unrecorded
// for a second flow on the same picture (docs/youcam.md), and a slot
// spends nothing, so these are the ids a replayed upload must hand out
// for the built body to match the recorded one.
func createFileIDs(t *testing.T, payload json.RawMessage) []string {
	t.Helper()
	var body struct {
		Src  string   `json:"src_file_id"`
		Refs []string `json:"ref_file_ids"`
		Ref  string   `json:"ref_file_id"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("create body: %v", err)
	}
	if body.Src == "" {
		t.Fatal("create body names no source file id")
	}
	ids := []string{body.Src}
	if body.Ref != "" {
		ids = append(ids, body.Ref)
	}
	if len(body.Refs) == 1 {
		ids = append(ids, body.Refs[0])
	}
	return ids
}

// canonicalForm is the JSON one raw body hashes to, keys sorted, so two
// encodings of one body compare equal.
func canonicalForm(t *testing.T, raw json.RawMessage) json.RawMessage {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("canonical form: %v", err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// renderClient captures every call the runner makes and can hand out
// canned slots, so a flow whose upload leg shares a recording with
// another flow (the engine-placed necklace, docs/youcam.md) still sends
// its recorded file ids. Everything else is delegated to the replay.
type renderClient struct {
	youcam.YouCam

	mu      sync.Mutex
	slots   []string
	uploads []youcam.File
	creates int
	polls   int
	fetches int
	bodies  []json.RawMessage
}

func newRenderClient(rep *youcam.Replay, slots ...string) *renderClient {
	return &renderClient{YouCam: rep, slots: slots}
}

func (c *renderClient) Upload(ctx context.Context, file youcam.File) (youcam.Slot, error) {
	c.mu.Lock()
	c.uploads = append(c.uploads, file)
	take := len(c.slots) > 0
	var id string
	if take {
		id = c.slots[0]
		c.slots = c.slots[1:]
	}
	c.mu.Unlock()
	if take {
		return youcam.Slot{FileID: id}, nil
	}
	return c.YouCam.Upload(ctx, file)
}

func (c *renderClient) Create(ctx context.Context, task youcam.Task) (youcam.Created, error) {
	c.mu.Lock()
	c.creates++
	c.bodies = append(c.bodies, task.Payload)
	c.mu.Unlock()
	return c.YouCam.Create(ctx, task)
}

func (c *renderClient) Poll(ctx context.Context, ref youcam.PollRef) (youcam.Status, error) {
	c.mu.Lock()
	c.polls++
	c.mu.Unlock()
	return c.YouCam.Poll(ctx, ref)
}

func (c *renderClient) Fetch(ctx context.Context, ref youcam.ResultRef) (youcam.Result, error) {
	c.mu.Lock()
	c.fetches++
	c.mu.Unlock()
	return c.YouCam.Fetch(ctx, ref)
}

func (c *renderClient) uploaded() []youcam.File {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]youcam.File(nil), c.uploads...)
}

// tally is the call tally: the uploads in order, the counts, and the
// create bodies under a lock.
func (c *renderClient) tally() (uploads []youcam.File, creates, polls, fetches int, bodies []json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]youcam.File(nil), c.uploads...), c.creates, c.polls, c.fetches, append([]json.RawMessage(nil), c.bodies...)
}

// renderBackend keeps the bytes a render kept, so the test reads the
// copy back out of the mediastore the runner wrote and compares it with
// the recorded result. The measure tests' backend throws the bytes
// away, because an analysis keeps none.
type renderBackend struct {
	mu    sync.Mutex
	blobs map[string][]byte
}

func (b *renderBackend) Open(_ context.Context, id string) (mediastore.BlobReader, error) {
	b.mu.Lock()
	data, ok := b.blobs[id]
	b.mu.Unlock()
	if !ok {
		return nil, mediastore.ErrNotFound
	}
	return nopCloserReader{bytes.NewReader(data)}, nil
}

func (b *renderBackend) Write(_ context.Context, id string, src io.Reader) (int64, error) {
	data, err := io.ReadAll(src)
	if err != nil {
		return 0, err
	}
	b.mu.Lock()
	b.blobs[id] = data
	b.mu.Unlock()
	return int64(len(data)), nil
}

func (b *renderBackend) Delete(_ context.Context, _ string) error { return nil }

func (b *renderBackend) List(_ context.Context) ([]mediastore.Object, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]mediastore.Object, 0, len(b.blobs))
	for id, data := range b.blobs {
		out = append(out, mediastore.Object{ID: id, SizeBytes: int64(len(data))})
	}
	return out, nil
}

func (b *renderBackend) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.blobs)
}

// renderRig is one assembled render run: the runner over the capturing
// client, the meter over its own unit budget, a mediastore that keeps
// what it is given, and a providertask store that can say whether a
// refused render left a claim behind. Nothing dialed.
type renderRig struct {
	runner *youcam.Runner
	client *renderClient
	budget *cost.Budget
	ledger *cost.Ledger
	media  *mediastore.Store
	blobs  *renderBackend
	tasks  *sqlitestore.Store
}

func newRenderRig(t *testing.T, rep *youcam.Replay, units cost.Price, slots ...string) *renderRig {
	t.Helper()
	client := newRenderClient(rep, slots...)
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
		Path:   filepath.Join(t.TempDir(), "render-tasks.db"),
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
	index := &memMediaIndex{rows: map[string]mediastore.Blob{}}
	blobs := &renderBackend{blobs: map[string][]byte{}}
	media, err := mediastore.Open(t.Context(), mediastore.Config{Index: index, Backend: blobs, Log: quietLog()})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := youcam.NewRunner(client, youcam.RunnerConfig{
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
	return &renderRig{
		runner: runner,
		client: client,
		budget: budget,
		ledger: ledger,
		media:  media,
		blobs:  blobs,
		tasks:  tasks,
	}
}

// keptRender reads one kept render back out of the rig's mediastore,
// the read path the app serves a render through.
func keptRender(t *testing.T, rig *renderRig, id string) ([]byte, mediastore.Blob) {
	t.Helper()
	rc, blob, err := rig.media.Open(t.Context(), id)
	if err != nil {
		t.Fatalf("open kept render %s: %v", id, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return data, blob
}

// renderPhoto is the photo one test renders: a fixed user and hash so
// the key pins read as literals.
func renderPhoto(image systemone.Image) Photo {
	return Photo{User: "user-1", Hash: "hash-v04", Image: image}
}

// renderFlowCase is one recorded flow: the fix it was recorded against,
// the photo and product it was recorded with, and the recordings its
// create, poll and fetch made.
type renderFlowCase struct {
	name     string
	occasion string
	fixID    string
	photo    string
	// product is the upload recording key of the product photo, empty
	// when the flow composites none.
	product   string
	productID string
	create    string
	fetch     string
	// slots names the flow whose recorded file ids the replayed upload
	// must hand out: the engine-placed necklace shares no upload
	// recording with its create (docs/youcam.md).
	slots string
	task  string
	units cost.Price
}

var renderFlowCases = []renderFlowCase{
	{name: "earring", occasion: "family-function", fixID: "family-gold-jhumkas", photo: "earring-upload-src", product: "earring-upload-ref", productID: "gold-jhumkas", create: "earring-create", fetch: "earring-fetch", task: "2d-vto/earring", units: 1},
	{name: "necklace", occasion: "festival", fixID: "festival-gold-chain", photo: "necklace-upload-src", product: "necklace-upload-ref", productID: "gold-chain", create: "necklace-auto-create", fetch: "necklace-auto-fetch", slots: "necklace-auto-create", task: "2d-vto/necklace", units: 1},
	{name: "garment swap", occasion: "family-function", fixID: "family-festive-top", photo: "cloth-upload-src", product: "cloth-upload-ref", productID: "top-festive", create: "cloth-create", fetch: "cloth-fetch", task: "cloth-v4", units: 2},
	{name: "red lip", occasion: "family-function", fixID: "family-red-lip", photo: "lip-red-upload-src", create: "lip-red-create", fetch: "lip-red-fetch", task: "makeup-vto", units: 1},
	{name: "tied hair", occasion: "family-function", fixID: "family-tie-hair", photo: "hair-updo-upload-src", create: "hair-updo-create", fetch: "hair-updo-fetch", task: "hair-transfer", units: 2},
	{name: "short beard", occasion: "family-function", fixID: "family-short-beard", photo: "beard-upload-src", create: "beard-create", fetch: "beard-fetch", task: "beard-style", units: 2},
	{name: "closed smile", occasion: "family-function", fixID: "family-closed-smile", photo: "smile-upload-src", create: "smile-create", fetch: "smile-fetch", task: "ai-smile", units: 1},
}

// TestEveryFixKindRendersOnItsRecordings is the success class of the
// done-when: each of the seven flows a fix menu names renders on the
// recordings it was made from - the body the fix builds is the recorded
// body, the picture comes back and is copied into mediastore at once,
// and the rate-card line settles once against the render key. The
// product a reference flow composites is the basket's, the rule's
// fallback arm.
func TestEveryFixKindRendersOnItsRecordings(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	for _, flow := range renderFlowCases {
		t.Run(flow.name, func(t *testing.T) {
			t.Parallel()
			occ := loadOccasions(t)[flow.occasion]
			fix := measureFix(t, occ, flow.fixID)
			var slots []string
			if flow.slots != "" {
				slots = createFileIDs(t, recordedCreateTask(t, rep, flow.slots).Payload)
			}
			rig := newRenderRig(t, rep, flow.units, slots...)
			photo := renderPhoto(recordedImage(t, rep, flow.photo))
			var basket *Product
			if flow.product != "" {
				basket = &Product{ID: flow.productID, Image: recordedImage(t, rep, flow.product)}
			}

			wantKey := "render/user-1/hash-v04/" + flow.fixID
			if basket != nil {
				wantKey += "/" + basket.ID
			}
			got, err := Render(t.Context(), rig.runner, photo, fix, nil, basket)
			if err != nil {
				t.Fatalf("%s: %v", flow.name, err)
			}
			if got.FixID != flow.fixID {
				t.Fatalf("render names fix %q, want %q", got.FixID, flow.fixID)
			}
			if got.Check != nil {
				t.Fatal("a render left this stage carrying a check; the check is T3.5's")
			}
			if len(got.MediaID) != 32 {
				t.Fatalf("media id %q is not an id-shaped name", got.MediaID)
			}

			// The kept copy is the recorded picture, under the content
			// type the bytes sniff as - never the provider's header or
			// name, which lie (docs/youcam.md).
			kept, blob := keptRender(t, rig, got.MediaID)
			if want := recordedRenderBytes(t, rep, flow.fetch); !bytes.Equal(kept, want) {
				t.Fatalf("kept %d bytes, want the recorded %d", len(kept), len(want))
			}
			// The recorded results are the T0.5 re-encodes - JPEG at
			// quality 92, whatever the provider sent - so the sniff reads
			// image/jpeg on every flow here. On the live wire the same
			// sniff is what names a PNG result honestly.
			if blob.ContentType != "image/jpeg" {
				t.Fatalf("kept content type %q, want the bytes' own image/jpeg", blob.ContentType)
			}
			if blob.Owner != photo.User || blob.Group != "render/"+photo.User+"/"+photo.Hash {
				t.Fatalf("kept blob owner %q group %q, want the user's render group", blob.Owner, blob.Group)
			}

			// The wire: the selfie uploaded first, the product second and
			// only when the flow composites one; one create whose body is
			// the recorded body; two polls (the verdict, then the fresh
			// link); one fetch.
			uploads := rig.client.uploaded()
			wantUploads := 1
			if flow.product != "" {
				wantUploads = 2
			}
			if len(uploads) != wantUploads {
				t.Fatalf("uploads %d, want %d", len(uploads), wantUploads)
			}
			if uploads[0].Task != flow.task || !bytes.Equal(uploads[0].Bytes, photo.Image.Bytes) {
				t.Fatalf("first upload is task %q with %d bytes, want the selfie under %s", uploads[0].Task, len(uploads[0].Bytes), flow.task)
			}
			if flow.product != "" {
				if !bytes.Equal(uploads[1].Bytes, basket.Image.Bytes) {
					t.Fatalf("second upload is %d bytes, want the basket product's %d", len(uploads[1].Bytes), len(basket.Image.Bytes))
				}
			}
			_, creates, polls, fetches, bodies := rig.client.tally()
			if creates != 1 || polls != 2 || fetches != 1 {
				t.Fatalf("creates %d polls %d fetches %d, want one create, two polls, one fetch", creates, polls, fetches)
			}
			if len(bodies) != 1 {
				t.Fatalf("create bodies %d, want the one render create", len(bodies))
			}
			recorded := recordedCreateTask(t, rep, flow.create).Payload
			if !bytes.Equal(canonicalForm(t, bodies[0]), canonicalForm(t, recorded)) {
				t.Fatalf("create body %s, want the recorded %s", bodies[0], recorded)
			}

			// The books: the rate-card line, settled once, against this
			// render's key.
			if rig.budget.Spent() != flow.units || rig.budget.Reserved() != 0 {
				t.Fatalf("budget spent %d reserved %d, want the %s-unit line settled", rig.budget.Spent(), rig.budget.Reserved(), flow.units)
			}
			charges := rig.ledger.Charges()
			if len(charges) != 1 {
				t.Fatalf("charges %d, want the one settle", len(charges))
			}
			if charges[0].Kind != "youcam."+flow.task || charges[0].Ref != wantKey || charges[0].UnitPrice != flow.units {
				t.Fatalf("charge %+v, want youcam.%s of %d units on %s", charges[0], flow.task, flow.units, wantKey)
			}
		})
	}
}

// TestTheBroughtProductRendersInsteadOfTheBasket is the priority arm of
// the product rule. The user brought their own product photo; the render
// composites that picture, not the basket's, and lands under a key of
// its own - so a later basket render never resumes it. The brought
// picture is the fixtures' other product photo: what the test pins is
// which bytes travel, not what they depict. The same look without a
// brought product then falls back to the basket's own entry.
func TestTheBroughtProductRendersInsteadOfTheBasket(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	refIDs := createFileIDs(t, recordedCreateTask(t, rep, "earring-create").Payload)
	// Both slots are canned, in upload order, so the built create names
	// the recorded ids and matches the recording: the selfie's id first,
	// then the one the brought product's upload answers with.
	rig := newRenderRig(t, rep, 2, refIDs...)
	occ := loadOccasions(t)["family-function"]
	fix := measureFix(t, occ, "family-gold-jhumkas")
	photo := renderPhoto(recordedImage(t, rep, "earring-upload-src"))
	basket := &Product{ID: "gold-jhumkas", Image: recordedImage(t, rep, "earring-upload-ref")}
	brought := &Product{ID: "my-own-jhumkas", Image: recordedImage(t, rep, "necklace-upload-ref")}

	got, err := Render(t.Context(), rig.runner, photo, fix, brought, basket)
	if err != nil {
		t.Fatal(err)
	}
	uploads := rig.client.uploaded()
	if len(uploads) != 2 {
		t.Fatalf("uploads %d, want the selfie and the brought product", len(uploads))
	}
	if !bytes.Equal(uploads[1].Bytes, brought.Image.Bytes) {
		t.Fatalf("the render composited %d other bytes, want the user's own picture (%d)", len(uploads[1].Bytes), len(brought.Image.Bytes))
	}
	if bytes.Equal(uploads[1].Bytes, basket.Image.Bytes) {
		t.Fatal("the render composited the basket product over the one the user brought")
	}
	kept, _ := keptRender(t, rig, got.MediaID)
	if want := recordedRenderBytes(t, rep, "earring-fetch"); !bytes.Equal(kept, want) {
		t.Fatalf("kept %d bytes, want the recorded %d", len(kept), len(want))
	}
	charges := rig.ledger.Charges()
	if len(charges) != 1 || charges[0].Ref != "render/user-1/hash-v04/family-gold-jhumkas/my-own-jhumkas" {
		t.Fatalf("charges %+v, want one settle on the brought product's key", charges)
	}

	// Without a brought product, the same fix renders from the basket's
	// entry, under the basket's key - a different task at the provider,
	// so neither render ever resumes the other's.
	if _, err := Render(t.Context(), rig.runner, photo, fix, nil, basket); err != nil {
		t.Fatal(err)
	}
	uploads = rig.client.uploaded()
	if len(uploads) != 4 {
		t.Fatalf("uploads %d after the basket render, want two per render", len(uploads))
	}
	if !bytes.Equal(uploads[3].Bytes, basket.Image.Bytes) {
		t.Fatalf("the fallback composited %d bytes, want the basket product's %d", len(uploads[3].Bytes), len(basket.Image.Bytes))
	}
	if rig.budget.Spent() != 2 || len(rig.ledger.Charges()) != 2 {
		t.Fatalf("spent %d over %d charges, want one settle per render", rig.budget.Spent(), len(rig.ledger.Charges()))
	}
	refs := map[string]bool{}
	for _, c := range rig.ledger.Charges() {
		refs[c.Ref] = true
	}
	if !refs["render/user-1/hash-v04/family-gold-jhumkas/my-own-jhumkas"] || !refs["render/user-1/hash-v04/family-gold-jhumkas/gold-jhumkas"] {
		t.Fatalf("charges %+v, want one key per product, never a shared one", rig.ledger.Charges())
	}
	if rig.blobs.count() != 2 {
		t.Fatalf("mediastore keeps %d renders, want one per product", rig.blobs.count())
	}
}

// TestARenderNeverPaysTwice is the idempotency class: the same user,
// photo, fix and product rendered twice runs the one recorded task. The
// second render uploads nothing and creates nothing - it collects the
// recorded outcome, reads a fresh link for the same picture, and lands
// it in mediastore under the same id - so the books carry exactly one
// settle no matter how many times a look restarts.
func TestARenderNeverPaysTwice(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	rig := newRenderRig(t, rep, 1)
	occ := loadOccasions(t)["family-function"]
	fix := measureFix(t, occ, "family-gold-jhumkas")
	photo := renderPhoto(recordedImage(t, rep, "earring-upload-src"))
	basket := &Product{ID: "gold-jhumkas", Image: recordedImage(t, rep, "earring-upload-ref")}

	first, err := Render(t.Context(), rig.runner, photo, fix, nil, basket)
	if err != nil {
		t.Fatal(err)
	}
	uploads, creates, polls, fetches, _ := rig.client.tally()
	if len(uploads) != 2 || creates != 1 || polls != 2 || fetches != 1 {
		t.Fatalf("uploads %d creates %d polls %d fetches %d, want the one recorded flow", len(uploads), creates, polls, fetches)
	}
	if rig.budget.Spent() != 1 || len(rig.ledger.Charges()) != 1 {
		t.Fatalf("spent %d over %d charges, want the one settle", rig.budget.Spent(), len(rig.ledger.Charges()))
	}

	second, err := Render(t.Context(), rig.runner, photo, fix, nil, basket)
	if err != nil {
		t.Fatal(err)
	}
	if second.MediaID != first.MediaID {
		t.Fatalf("second render kept %q, want the first render's %q", second.MediaID, first.MediaID)
	}
	uploads, creates, polls, fetches, _ = rig.client.tally()
	if len(uploads) != 2 || creates != 1 {
		t.Fatalf("uploads %d creates %d after the repeat, want no second upload and no second task", len(uploads), creates)
	}
	if polls != 3 || fetches != 2 {
		t.Fatalf("polls %d fetches %d after the repeat, want the collected outcome's one poll and copy", polls, fetches)
	}
	if rig.budget.Spent() != 1 || len(rig.ledger.Charges()) != 1 {
		t.Fatalf("spent %d over %d charges after the repeat, want the books untouched", rig.budget.Spent(), len(rig.ledger.Charges()))
	}
	kept, _ := keptRender(t, rig, second.MediaID)
	if want := recordedRenderBytes(t, rep, "earring-fetch"); !bytes.Equal(kept, want) {
		t.Fatalf("kept %d bytes, want the recorded %d", len(kept), len(want))
	}
}

// TestABudgetRefusalSpendsNothing is the budget class of the done-when.
// A budget under the rate-card line refuses the render before anything
// uploads: the typed refusal carries the quote and unwraps to the
// budget's own error, the books stay empty, mediastore keeps nothing, no
// providertask claim is left behind, and the suggestion the look was
// going to show - the criterion's own words, the suggestion-only look's
// content - is exactly what it was.
func TestABudgetRefusalSpendsNothing(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	rig := newRenderRig(t, rep, 0)
	occ := loadOccasions(t)["family-function"]
	fix := measureFix(t, occ, "family-gold-jhumkas")
	photo := renderPhoto(recordedImage(t, rep, "earring-upload-src"))
	basket := &Product{ID: "gold-jhumkas", Image: recordedImage(t, rep, "earring-upload-ref")}
	var suggestion string
	for _, id := range fix.Criteria {
		for _, c := range occ.Criteria {
			if c.ID == id {
				suggestion = c.Suggestion
			}
		}
	}
	if suggestion == "" {
		t.Fatal("the fix names no criterion with a suggestion; the story needs one")
	}

	got, err := Render(t.Context(), rig.runner, photo, fix, nil, basket)
	if err == nil {
		t.Fatal("a zero budget rendered")
	}
	refused, ok := err.(*Refused)
	if !ok {
		t.Fatalf("%v, want a typed *Refused", err)
	}
	if refused.FixID != "family-gold-jhumkas" || refused.Task != "2d-vto/earring" || refused.Units != 1 {
		t.Fatalf("refusal %+v, want the jhumkas fix, its task, and its one-unit quote", refused)
	}
	if !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("%v, want the budget's own refusal underneath", err)
	}
	if got != (domain.Render{}) {
		t.Fatalf("a refused render returned %+v, want nothing to show", got)
	}
	if suggestion != "The ear reads plain; try gold jhumkas." {
		t.Fatalf("suggestion %q moved", suggestion)
	}

	uploads, creates, polls, fetches, _ := rig.client.tally()
	if len(uploads) != 0 || creates != 0 || polls != 0 || fetches != 0 {
		t.Fatalf("uploads %d creates %d polls %d fetches %d, want a refusal before any call", len(uploads), creates, polls, fetches)
	}
	if rig.budget.Spent() != 0 || rig.budget.Reserved() != 0 || len(rig.ledger.Charges()) != 0 {
		t.Fatalf("spent %d reserved %d over %d charges, want empty books", rig.budget.Spent(), rig.budget.Reserved(), len(rig.ledger.Charges()))
	}
	if rig.blobs.count() != 0 {
		t.Fatal("a refused render kept a blob")
	}
	if _, err := rig.tasks.Get(t.Context(), "render/user-1/hash-v04/family-gold-jhumkas/gold-jhumkas"); !errors.Is(err, providertask.ErrUnknownKey) {
		t.Fatalf("claim %v, want the refused key to leave nothing behind", err)
	}
}

// nudeLipFixes are the lip fixes whose palette has no recorded render:
// the date, interview and funeral menus paint nude, and no nude render
// was ever made (docs/occasions.md). They are refused until one is
// recorded, and the refusal set must grow or shrink with the recordings
// rather than with this table's mood.
var nudeLipFixes = map[string]bool{
	"date-nude-lip":      true,
	"interview-nude-lip": true,
	"funeral-nude-lip":   true,
}

// TestEveryMenuFixMapsToItsFlow builds the render job of all twenty-eight
// fixes on the five menus, without running any of them. Every fix maps
// onto its data's own task, carries exactly the files that task takes,
// keys itself the same way a run would, and builds a body with the
// recorded shape: the template the fix names, the palette its vocabulary
// names, the garment category the recording used, skin_smooth at a
// present zero, and no necklace coordinate the pipeline never measured.
// The three nude lip fixes are the refused set, each naming what a
// recording would add.
func TestEveryMenuFixMapsToItsFlow(t *testing.T) {
	t.Parallel()
	ids := []string{"src-file-id", "ref-file-id"}
	planned := 0
	refused := 0
	covered := map[string]int{}
	for _, occ := range loadOccasions(t) {
		for i := range occ.Fixes {
			fix := &occ.Fixes[i]
			photo := Photo{User: "user-1", Hash: "hash", Image: systemone.Image{ContentType: "image/jpeg", Bytes: []byte("pixels")}}
			var basket *Product
			if len(fix.Products) > 0 {
				basket = &Product{ID: fix.Products[0], Image: photo.Image}
			}
			job, err := plan(photo, fix, nil, basket)
			if nudeLipFixes[fix.ID] {
				refused++
				if err == nil || !strings.Contains(err.Error(), "nude") || !strings.Contains(err.Error(), "no recorded render on this seam used") {
					t.Fatalf("%s: %v, want a refusal naming the nude palette no recording used", fix.ID, err)
				}
				continue
			}
			if err != nil {
				t.Fatalf("%s: %v", fix.ID, err)
			}
			planned++
			covered[job.Task]++

			// The mapping honours the data's own task, and the file plan
			// is the flow's: a second file only where the task composites
			// a product - never for the lip, whose product photo is a
			// shop photo the task never sees, and never for the template
			// and smile flows.
			if job.Task != fix.YouCam.Task {
				t.Fatalf("%s planned task %q, want the data's %q", fix.ID, job.Task, fix.YouCam.Task)
			}
			wantFiles := 1
			switch job.Task {
			case "2d-vto/earring", "2d-vto/necklace", "cloth-v4":
				wantFiles = 2
				if len(fix.Products) != 1 {
					t.Fatalf("%s composites a product and names %d", fix.ID, len(fix.Products))
				}
			default:
				if len(fix.Products) > 1 {
					t.Fatalf("%s names %d products; a render takes one", fix.ID, len(fix.Products))
				}
			}
			if len(job.Files) != wantFiles {
				t.Fatalf("%s plans %d files, want %d", fix.ID, len(job.Files), wantFiles)
			}
			for _, f := range job.Files {
				if f.Task != job.Task || f.ContentType != photo.Image.ContentType {
					t.Fatalf("%s plans a file under task %q type %q", fix.ID, f.Task, f.ContentType)
				}
			}

			// The key is the run's key, product segment and all, and the
			// result lands in the photo's render group.
			wantKey := "render/user-1/hash/" + fix.ID
			if wantFiles == 2 {
				wantKey += "/" + fix.Products[0]
			}
			if job.Key != wantKey {
				t.Fatalf("%s keys %q, want %q", fix.ID, job.Key, wantKey)
			}
			if job.Owner != "user-1" || job.Group != "render/user-1/hash" {
				t.Fatalf("%s keeps under owner %q group %q", fix.ID, job.Owner, job.Group)
			}

			// The body: the recorded shape, pinned field by field per
			// task. A body the recordings never saw fails here rather
			// than at the provider.
			body, err := job.Payload(ids)
			if err != nil {
				t.Fatalf("%s body: %v", fix.ID, err)
			}
			var m map[string]any
			if err := json.Unmarshal(body, &m); err != nil {
				t.Fatalf("%s body is not JSON: %v", fix.ID, err)
			}
			if m["src_file_id"] != ids[0] {
				t.Fatalf("%s body names source %v, want the uploaded id", fix.ID, m["src_file_id"])
			}
			switch job.Task {
			case "2d-vto/earring", "2d-vto/necklace":
				refs, ok := m["ref_file_ids"].([]any)
				if !ok || len(refs) != 1 || refs[0] != ids[1] {
					t.Fatalf("%s body names refs %v, want the one uploaded product id", fix.ID, m["ref_file_ids"])
				}
				info, ok := m["source_info"].(map[string]any)
				if !ok || info["name"] != ids[0] {
					t.Fatalf("%s body names source_info %v", fix.ID, m["source_info"])
				}
				objs, ok := m["object_infos"].([]any)
				if !ok || len(objs) != 1 {
					t.Fatalf("%s body names object_infos %v", fix.ID, m["object_infos"])
				}
				if obj, ok := objs[0].(map[string]any); !ok || obj["name"] != ids[1] {
					t.Fatalf("%s body names object %v", fix.ID, objs[0])
				}
				if _, ok := m["parameter"]; ok {
					t.Fatalf("%s body carries placement parameters the pipeline never measured", fix.ID)
				}
			case "cloth-v4":
				if m["ref_file_id"] != ids[1] {
					t.Fatalf("%s body names ref %v, want the uploaded product id", fix.ID, m["ref_file_id"])
				}
				if m["garment_category"] != clothCategory {
					t.Fatalf("%s body names category %v, want %q", fix.ID, m["garment_category"], clothCategory)
				}
				if _, ok := m["template_id"]; ok {
					t.Fatalf("%s body carries a template a cloth render never took", fix.ID)
				}
			case "hair-transfer":
				if m["template_id"] != fix.Template {
					t.Fatalf("%s body renders template %v, want the fix's %q", fix.ID, m["template_id"], fix.Template)
				}
				if m["hair_color"] != hairColorSrc {
					t.Fatalf("%s body sets hair colour %v, want %q, the user's own", fix.ID, m["hair_color"], hairColorSrc)
				}
				if _, ok := m["ref_file_id"]; ok {
					t.Fatalf("%s body carries a product a hair render never took", fix.ID)
				}
			case "beard-style":
				if m["template_id"] != fix.Template {
					t.Fatalf("%s body renders template %v, want the fix's %q", fix.ID, m["template_id"], fix.Template)
				}
				if _, ok := m["hair_color"]; ok {
					t.Fatalf("%s body sets a hair colour a beard render never took", fix.ID)
				}
			case "makeup-vto":
				if m["version"] != lipVersion {
					t.Fatalf("%s body names version %v", fix.ID, m["version"])
				}
				effects, ok := m["effects"].([]any)
				if !ok || len(effects) != 2 {
					t.Fatalf("%s body carries %v effects, want the smoothing zero and the one lip palette", fix.ID, m["effects"])
				}
				smooth, ok := effects[0].(map[string]any)
				if !ok || smooth["category"] != "skin_smooth" {
					t.Fatalf("%s body's first effect is %v, want skin_smooth", fix.ID, effects[0])
				}
				if v, ok := smooth["skinSmoothStrength"]; !ok || v != float64(0) {
					t.Fatalf("%s body's skinSmoothStrength is %v (present %v), want a present zero: omitted, the provider smooths at 50", fix.ID, v, ok)
				}
				if v, ok := smooth["skinSmoothColorIntensity"]; !ok || v != float64(0) {
					t.Fatalf("%s body's skinSmoothColorIntensity is %v (present %v), want a present zero", fix.ID, v, ok)
				}
				lip, ok := effects[1].(map[string]any)
				if !ok || lip["category"] != "lip_color" {
					t.Fatalf("%s body's second effect is %v, want lip_color", fix.ID, effects[1])
				}
				if strings.Contains(string(body), "morphology") {
					t.Fatalf("%s body carries a morphology a lip fix must not set", fix.ID)
				}
				value, err := vocabularyValue(fix, "lip_colour")
				if err != nil {
					t.Fatal(err)
				}
				want, ok := lipPalettes[value]
				if !ok {
					t.Fatalf("%s paints %q, which this test does not know", fix.ID, value)
				}
				shape, ok := lip["shape"].(map[string]any)
				if !ok || shape["name"] != lipShapeOriginal {
					t.Fatalf("%s body's lip shape is %v, want original", fix.ID, lip["shape"])
				}
				palettes, ok := lip["palettes"].([]any)
				if !ok || len(palettes) != 1 {
					t.Fatalf("%s body carries %v palettes, want one", fix.ID, lip["palettes"])
				}
				palette := palettes[0].(map[string]any)
				if palette["color"] != want {
					t.Fatalf("%s paints %v, want the recorded %q", fix.ID, palette["color"], want)
				}
				if palette["texture"] != lipTexture || palette["colorIntensity"] != float64(lipIntensity) {
					t.Fatalf("%s body's palette is %v, want %s matte at %d", fix.ID, palette, lipTexture, lipIntensity)
				}
				style, ok := lip["style"].(map[string]any)
				if !ok || style["type"] != lipStyleFull {
					t.Fatalf("%s body's lip style is %v, want full", fix.ID, lip["style"])
				}
			case "ai-smile":
				if m["expression_type"] != "closed_mouth_smile" {
					t.Fatalf("%s body sets expression %v, want the recorded closed_mouth_smile", fix.ID, m["expression_type"])
				}
				if _, ok := m["template_id"]; ok {
					t.Fatalf("%s body carries a template a smile never took", fix.ID)
				}
			}
		}
	}

	// The sweep covered the real menus: twenty-five fixes planned, the
	// three nude lips refused, and every task kind present - so a fix
	// added to the data that plans oddly cannot hide.
	if planned != 25 || refused != 3 {
		t.Fatalf("planned %d refused %d, want the menus' 25 and the 3 nude lips", planned, refused)
	}
	want := map[string]int{
		"cloth-v4":        5,
		"hair-transfer":   6,
		"beard-style":     5,
		"makeup-vto":      2,
		"2d-vto/earring":  2,
		"2d-vto/necklace": 1,
		"ai-smile":        4,
	}
	for task, n := range want {
		if covered[task] != n {
			t.Fatalf("the sweep planned %d %s fixes, want %d", covered[task], task, n)
		}
	}
}

// TestRenderRefusesAnUnusableCall pins the refusals that come before any
// work: no runner, no fix, no user, no hash, an empty photo, a photo
// with no content type, a task no flow builds, a reference render with
// no product anywhere, a product with no id or no picture, a template
// fix with no template, a lip with no colour value or an unrecorded one,
// and a smile no recording used. Every one is refused before a call, a
// unit or a byte.
func TestRenderRefusesAnUnusableCall(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	rig := newRenderRig(t, rep, 5)
	occ := loadOccasions(t)["family-function"]
	jhumkas := measureFix(t, occ, "family-gold-jhumkas")
	updo := measureFix(t, occ, "family-tie-hair")
	image := recordedImage(t, rep, "earring-upload-src")
	product := &Product{ID: "gold-jhumkas", Image: image}
	lipMagentaVocab := []domain.VocabularyChange{{Attribute: "lip_colour", To: "magenta"}}
	expressionVocab := []domain.VocabularyChange{{Attribute: "expression", To: "closed-mouth-smile"}}
	expressionWink := []domain.VocabularyChange{{Attribute: "expression", To: "wink"}}
	hairVocab := []domain.VocabularyChange{{Attribute: "hair", To: "bun"}}
	good := renderPhoto(image)
	ids := []string{"src-file-id", "ref-file-id"}
	cases := []struct {
		name string
		call func() error
	}{
		{"no runner", func() error {
			_, err := Render(t.Context(), nil, good, jhumkas, nil, product)
			return err
		}},
		{"no fix", func() error {
			_, err := Render(t.Context(), rig.runner, good, nil, nil, product)
			return err
		}},
		{"no user", func() error {
			_, err := Render(t.Context(), rig.runner, Photo{Hash: "hash-v04", Image: image}, jhumkas, nil, product)
			return err
		}},
		{"no hash", func() error {
			_, err := Render(t.Context(), rig.runner, Photo{User: "user-1", Image: image}, jhumkas, nil, product)
			return err
		}},
		{"empty photo", func() error {
			_, err := Render(t.Context(), rig.runner, Photo{User: "user-1", Hash: "hash-v04", Image: systemone.Image{ContentType: image.ContentType}}, jhumkas, nil, product)
			return err
		}},
		{"no content type", func() error {
			_, err := Render(t.Context(), rig.runner, Photo{User: "user-1", Hash: "hash-v04", Image: systemone.Image{Bytes: image.Bytes}}, jhumkas, nil, product)
			return err
		}},
		{"a task no flow builds", func() error {
			_, err := Render(t.Context(), rig.runner, good, &domain.Fix{ID: "constructed-strange", YouCam: domain.YouCam{Task: "no-such-task"}}, nil, product)
			return err
		}},
		{"no product anywhere", func() error {
			_, err := Render(t.Context(), rig.runner, good, jhumkas, nil, nil)
			return err
		}},
		{"a product with no id", func() error {
			_, err := Render(t.Context(), rig.runner, good, jhumkas, nil, &Product{Image: image})
			return err
		}},
		{"a product with no picture", func() error {
			_, err := Render(t.Context(), rig.runner, good, jhumkas, nil, &Product{ID: "gold-jhumkas", Image: systemone.Image{ContentType: image.ContentType}})
			return err
		}},
		{"a product with no content type", func() error {
			_, err := Render(t.Context(), rig.runner, good, jhumkas, nil, &Product{ID: "gold-jhumkas", Image: systemone.Image{Bytes: image.Bytes}})
			return err
		}},
		{"a hair fix with no template", func() error {
			_, err := Render(t.Context(), rig.runner, good, &domain.Fix{ID: "constructed-hair", YouCam: domain.YouCam{Task: "hair-transfer"}, Vocabulary: hairVocab}, nil, product)
			return err
		}},
		{"a lip fix with no colour value", func() error {
			_, err := Render(t.Context(), rig.runner, good, &domain.Fix{ID: "constructed-lip", YouCam: domain.YouCam{Task: "makeup-vto"}}, nil, product)
			return err
		}},
		{"a lip colour no recording used", func() error {
			_, err := Render(t.Context(), rig.runner, good, &domain.Fix{ID: "constructed-magenta", YouCam: domain.YouCam{Task: "makeup-vto"}, Vocabulary: lipMagentaVocab}, nil, product)
			return err
		}},
		{"an expression no recording used", func() error {
			_, err := Render(t.Context(), rig.runner, good, &domain.Fix{ID: "constructed-wink", YouCam: domain.YouCam{Task: "ai-smile"}, Vocabulary: expressionWink}, nil, product)
			return err
		}},
		{"a smile with no expression value", func() error {
			_, err := Render(t.Context(), rig.runner, good, &domain.Fix{ID: "constructed-smile", YouCam: domain.YouCam{Task: "ai-smile"}, Vocabulary: hairVocab}, nil, product)
			return err
		}},
	}

	// Two shapes the table must take without refusing: a smile that
	// carries a hair template and hair vocabulary takes neither - the
	// flow reads only its own expression value - and a hair fix carries
	// its analysis list for Measure untouched.
	smilingHair := &domain.Fix{ID: "constructed-smiling-hair", YouCam: domain.YouCam{Task: "ai-smile"}, Template: "female_casual_updo", Vocabulary: expressionVocab}
	job, err := plan(good, smilingHair, nil, product)
	if err != nil {
		t.Fatalf("a smile with someone else's vocabulary: %v", err)
	}
	if len(job.Files) != 1 {
		t.Fatalf("a smile planned %d files, want the selfie alone", len(job.Files))
	}
	body, err := job.Payload(ids)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "template") {
		t.Fatalf("a smile body read the fix's template: %s", body)
	}
	job, err = plan(good, updo, nil, product)
	if err != nil {
		t.Fatalf("the hair fix: %v", err)
	}
	if job.Task != "hair-transfer" || len(updo.Analysis) != 1 {
		t.Fatalf("hair plan task %q, fix analysis %d", job.Task, len(updo.Analysis))
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.call(); err == nil {
				t.Fatal("an unusable call was taken")
			}
		})
	}
	uploads, creates, polls, fetches, _ := rig.client.tally()
	if len(uploads) != 0 || creates != 0 || polls != 0 || fetches != 0 {
		t.Fatalf("uploads %d creates %d polls %d fetches %d, want no call from any refusal", len(uploads), creates, polls, fetches)
	}
	if rig.budget.Spent() != 0 || len(rig.ledger.Charges()) != 0 {
		t.Fatalf("spent %d over %d charges, want nothing settled", rig.budget.Spent(), len(rig.ledger.Charges()))
	}
	if rig.blobs.count() != 0 {
		t.Fatalf("mediastore keeps %d blobs, want none", rig.blobs.count())
	}
}
