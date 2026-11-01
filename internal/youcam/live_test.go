//go:build live

package youcam

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/mediastore"
	mediasql "github.com/nrynss/keel/mediastore/sqlitestore"
	"github.com/nrynss/keel/providertask"
	"github.com/nrynss/keel/providertask/sqlitestore"
	"github.com/nrynss/keel/sqlite"
)

// liveJobKey is the idempotency key the live render runs under.
const liveJobKey = "t22b-live-lip-red"

// TestLiveYouCamRendersLipRed is the project's first live call to YouCam.
// Charter of T2.2b: one upload slot, which costs nothing, then one render
// task on a sample photo, within a 20-unit budget. The render is lip red
// (makeup-vto, the rate card's cheapest line at 1 unit) on the committed AI
// sample woman V04, run through the T2.2 Runner over Keel providertask, so
// the evidence covers the resume machinery, the mediastore keep and the
// fresh-link re-poll, not just the raw client. The balance at the v1 credit
// endpoint is read before and after; the read is free.
//
// It runs only with `go test -tags live ./internal/youcam/` and
// YOUCAM_API_KEY in the environment; it skips when the name is absent and
// the gate never builds this file. Run it once: every run creates one paid
// task. The meter is funded with exactly the rate-card line, so nothing
// past one task can ever leave the process, and a budget refusal spends
// nothing at all.
func TestLiveYouCamRendersLipRed(t *testing.T) {
	key := os.Getenv("YOUCAM_API_KEY")
	if key == "" {
		t.Skip("YOUCAM_API_KEY is missing; the live YouCam test runs only with the key in the environment")
	}
	client, err := NewClient(ClientConfig{Token: key})
	if err != nil {
		t.Fatal(scrubLiveKey(key, err.Error()))
	}
	wire := &liveWire{YouCam: client, key: key, t: t}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	photo := livePhoto(t, filepath.Join(repoRoot(t), "testdata", "samples", "woman", "V04-family-function.jpg"))

	// The free leg first. A slot is the call AGENTS.md names as the
	// nothing-spent proof of a key.
	before := liveBalance(ctx, wire, client)
	slotStart := time.Now()
	slot, err := wire.Upload(ctx, File{Task: "makeup-vto", ContentType: "image/jpeg", Bytes: photo})
	if err != nil {
		t.Fatalf("upload slot: %s", scrubLiveKey(key, err.Error()))
	}
	if slot.FileID == "" {
		t.Fatal("the slot named no file id")
	}
	liveLog(wire, "upload slot leg done in %d ms (free)", time.Since(slotStart).Milliseconds())
	mid := liveBalance(ctx, wire, client)
	if mid != before {
		t.Errorf("FINDING: the balance moved %d -> %d across the free slot leg", before, mid)
	}

	// The paid leg: one render, metered with exactly the rate-card line.
	units, ok := UnitsFor("makeup-vto")
	if !ok || units != 1 {
		t.Fatalf("the rate card quotes %d/%v for makeup-vto, want 1", units, ok)
	}
	budget, err := cost.NewBudgetIn(UnitsDenomination, units)
	if err != nil {
		t.Fatal(err)
	}
	ledger := cost.NewLedger()
	meter, err := cost.NewMeter(budget, ledger)
	if err != nil {
		t.Fatal(err)
	}

	// The runner's own wiring, production-shaped: one sqlite file carries
	// the providertask claims and the mediastore index, the blobs land on
	// a directory beside it.
	dir := t.TempDir()
	db, err := sqlite.Open(ctx, sqlite.Config{
		Path:   filepath.Join(dir, "live.db"),
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := sqlitestore.Open(ctx, sqlitestore.Config{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	mediaIndex, err := mediasql.Open(ctx, mediasql.Config{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	media, err := mediastore.Open(ctx, mediastore.Config{
		Dir:   filepath.Join(dir, "media"),
		Index: mediaIndex,
		Log:   slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewRunner(wire, RunnerConfig{
		Store: store,
		Meter: meter,
		Media: media,
		Config: providertask.Config{
			PollBase:    2 * time.Second,
			PollCeiling: 4 * time.Second,
			Deadline:    2 * time.Minute,
			Log:         slog.New(slog.DiscardHandler),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	job := Job{
		Key:   liveJobKey,
		Task:  "makeup-vto",
		Owner: "live-t22b",
		Group: "live-t22b",
		Files: []File{{Task: "makeup-vto", ContentType: "image/jpeg", Bytes: photo}},
		Payload: func(ids []string) (json.RawMessage, error) {
			if len(ids) != 1 {
				return nil, fmt.Errorf("live: %d uploaded file ids, want 1", len(ids))
			}
			return json.Marshal(liveLipBody{
				SrcFileID: ids[0],
				Version:   "1.0",
				Effects: []any{
					liveSmoothEffect{Category: "skin_smooth"},
					liveLipEffect{
						Category: "lip_color",
						Shape:    liveLipShape{Name: "original"},
						Palettes: []liveLipPalette{{Color: "#B0172F", Texture: "matte", ColorIntensity: 70}},
						Style:    liveLipStyle{Type: "full"},
					},
				},
			})
		},
	}

	renderStart := time.Now()
	out, err := runner.Run(ctx, job)
	if err != nil {
		t.Fatalf("render: %s", scrubLiveKey(key, err.Error()))
	}
	liveLog(wire, "render leg done in %d ms task_id=%s media_id=%s face_shape=%q",
		time.Since(renderStart).Milliseconds(), out.TaskID, out.MediaID, out.FaceShape)

	// The kept bytes: the keep copied the result the moment it was in hand,
	// the content type sniffed from the bytes (a PNG result arrives as
	// binary/octet-stream under a .jpg name).
	reader, blob, err := media.Open(ctx, out.MediaID)
	if err != nil {
		t.Fatalf("the kept blob %s: %v", out.MediaID, err)
	}
	kept, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	liveLog(wire, "kept blob content_type=%s size=%d sniffed=%s",
		blob.ContentType, blob.SizeBytes, http.DetectContentType(kept))
	if len(kept) == 0 || !strings.HasPrefix(http.DetectContentType(kept), "image/") {
		t.Fatalf("the kept bytes are not a picture: %d bytes, sniffed %s", len(kept), http.DetectContentType(kept))
	}
	if blob.SizeBytes != int64(len(kept)) {
		t.Errorf("blob row says %d bytes, read back %d", blob.SizeBytes, len(kept))
	}

	// The books: one settle, the rate-card line, on this key.
	if spent := budget.Spent(); spent != units {
		t.Errorf("the books spent %d, want the rate-card line %d", spent, units)
	}
	charges := ledger.Charges()
	if len(charges) != 1 {
		t.Fatalf("charges %d, want the one settle", len(charges))
	}
	charge := charges[0]
	if charge.Kind != "youcam.makeup-vto" || charge.Ref != liveJobKey ||
		charge.UnitPrice != units || charge.Denomination != UnitsDenomination {
		t.Errorf("charge %+v, want youcam.makeup-vto of %d units on %s", charge, units, liveJobKey)
	}
	liveLog(wire, "settled kind=%s ref=%s units=%d", charge.Kind, charge.Ref, charge.UnitPrice)

	// The claim: the resume machinery recorded the task and finished it.
	claim, err := store.Get(ctx, liveJobKey)
	if err != nil {
		t.Fatal(err)
	}
	if claim.State != providertask.StateSucceeded || claim.TaskID != out.TaskID {
		t.Errorf("claim state=%s task_id=%s, want a succeeded %s", claim.State, claim.TaskID, out.TaskID)
	}

	// The calls the run made: one create is the whole point of the
	// idempotency key; the two uploads are the charter's free slot leg and
	// the job's own upload inside the create.
	wire.mu.Lock()
	uploads, creates, polls, fetches := wire.uploads, wire.creates, wire.polls, wire.fetches
	wire.mu.Unlock()
	liveLog(wire, "calls: uploads=%d creates=%d polls=%d fetches=%d", uploads, creates, polls, fetches)
	if creates != 1 {
		t.Errorf("creates %d, want exactly one", creates)
	}

	// The wire-side units evidence: the balance after names what the
	// provider actually took.
	after := liveBalance(ctx, wire, client)
	if want := before - int(units); after != want {
		t.Errorf("FINDING: balance after %d, want %d (before %d minus the %d-unit render): the units story does not line up",
			after, want, before, units)
	}
}

// livePhoto reads one committed AI sample fixture and proves it decodes
// above the engine's 320x320 minimum. No real person's photo ever goes to
// the provider (AGENTS.md).
func livePhoto(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if cfg.Width < 320 || cfg.Height < 320 {
		t.Fatalf("%s is %dx%d, under the engine's 320x320 minimum", path, cfg.Width, cfg.Height)
	}
	t.Logf("photo %s %dx%d %d bytes (an AI sample person)", filepath.Base(path), cfg.Width, cfg.Height, len(raw))
	return raw
}

// creditPool is one pool of the v1 credit endpoint's reply. The endpoint
// answers to the v2 key and reads free (docs/youcam.md).
type creditPool struct {
	Type   string  `json:"type"`
	Amount float64 `json:"amount"`
	Expiry float64 `json:"expiry"`
}

// liveBalance reads the balance once, logs each pool, and returns the
// total. The read spends nothing, so it is safe before, between and after
// the paid legs.
func liveBalance(ctx context.Context, w *liveWire, c *Client) int {
	w.t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base.String()+"/s2s/v1.0/client/credit", nil)
	if err != nil {
		w.t.Fatal(err)
	}
	req.Header.Set("Accept", "application/json")
	// The recorded credit GET carried both headers; a bare GET drew 415 on
	// the credit history (docs/youcam.md).
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		w.t.Fatal(scrubLiveKey(c.token, err.Error()))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		w.t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		w.t.Fatalf("the balance read answered %d: %s", resp.StatusCode, scrubLiveKey(c.token, string(body)))
	}
	var reply struct {
		Status  int          `json:"status"`
		Results []creditPool `json:"results"`
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		w.t.Fatalf("the balance reply is not json: %s", scrubLiveKey(c.token, string(body)))
	}
	total := 0
	for _, pool := range reply.Results {
		units := int(math.Round(pool.Amount))
		total += units
		liveLog(w, "balance pool type=%s amount=%d expiry=%s", pool.Type, units,
			time.UnixMilli(int64(pool.Expiry)).UTC().Format("2006-01-02"))
	}
	liveLog(w, "balance total=%d", total)
	return total
}

// liveWire wraps the live client to write one transcript line per call the
// run makes. It changes no behaviour. A signed url is logged without its
// query, because the query carries the grant and the recordings keep only
// scheme, host and path.
type liveWire struct {
	YouCam
	key string
	t   *testing.T

	mu      sync.Mutex
	uploads int
	creates int
	polls   int
	fetches int
}

func liveLog(w *liveWire, format string, args ...any) {
	w.t.Helper()
	w.t.Log(scrubLiveKey(w.key, fmt.Sprintf(format, args...)))
}

func scrubLiveKey(key, text string) string {
	if key == "" {
		return text
	}
	return strings.ReplaceAll(text, key, "redacted")
}

// briefLiveURL strips the query string off a signed url.
func briefLiveURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "(url)"
	}
	return u.Scheme + "://" + u.Host + u.Path
}

func (w *liveWire) Upload(ctx context.Context, file File) (Slot, error) {
	w.mu.Lock()
	w.uploads++
	n := w.uploads
	w.mu.Unlock()
	start := time.Now()
	slot, err := w.YouCam.Upload(ctx, file)
	if err != nil {
		liveLog(w, "upload #%d task=%s bytes=%d failed after %d ms: %s",
			n, file.Task, len(file.Bytes), time.Since(start).Milliseconds(), scrubLiveKey(w.key, err.Error()))
		return Slot{}, err
	}
	liveLog(w, "upload #%d task=%s bytes=%d file_name=%s -> file_id=%s put=%s (%d ms)",
		n, file.Task, len(file.Bytes), fileName(file), slot.FileID, briefLiveURL(slot.URL),
		time.Since(start).Milliseconds())
	return slot, nil
}

func (w *liveWire) Create(ctx context.Context, task Task) (Created, error) {
	w.mu.Lock()
	w.creates++
	n := w.creates
	w.mu.Unlock()
	start := time.Now()
	created, err := w.YouCam.Create(ctx, task)
	if err != nil {
		liveLog(w, "create #%d %s failed after %d ms: %s",
			n, task.Name, time.Since(start).Milliseconds(), scrubLiveKey(w.key, err.Error()))
		return Created{}, err
	}
	liveLog(w, "create #%d POST /s2s/%s/task/%s payload=%s -> task_id=%s (%d ms)",
		n, versionFor(task.Name), task.Name, task.Payload, created.TaskID,
		time.Since(start).Milliseconds())
	return created, nil
}

func (w *liveWire) Poll(ctx context.Context, ref PollRef) (Status, error) {
	w.mu.Lock()
	w.polls++
	n := w.polls
	w.mu.Unlock()
	start := time.Now()
	st, err := w.YouCam.Poll(ctx, ref)
	if err != nil {
		liveLog(w, "poll #%d failed after %d ms: %s",
			n, time.Since(start).Milliseconds(), scrubLiveKey(w.key, err.Error()))
		return Status{}, err
	}
	extra := ""
	if st.ResultURL != "" {
		extra = " url=" + briefLiveURL(st.ResultURL)
	}
	liveLog(w, "poll #%d GET /s2s/%s/task/%s/%s -> %s%s (%d ms)",
		n, versionFor(ref.Task), ref.Task, ref.TaskID, st.Status, extra,
		time.Since(start).Milliseconds())
	return st, nil
}

func (w *liveWire) Fetch(ctx context.Context, ref ResultRef) (Result, error) {
	w.mu.Lock()
	w.fetches++
	n := w.fetches
	w.mu.Unlock()
	start := time.Now()
	res, err := w.YouCam.Fetch(ctx, ref)
	if err != nil {
		liveLog(w, "fetch #%d failed after %d ms: %s",
			n, time.Since(start).Milliseconds(), scrubLiveKey(w.key, err.Error()))
		return Result{}, err
	}
	liveLog(w, "fetch #%d GET %s -> %d bytes (%d ms)",
		n, briefLiveURL(ref.URL), len(res.Bytes), time.Since(start).Milliseconds())
	return res, nil
}

// liveLipBody is the recorded lip-red makeup body, docs/youcam.md's request
// table: skin_smooth sent at zero (an omitted one defaults to 50), the lip
// shape original, one matte palette at intensity 70, full style.
type liveLipBody struct {
	SrcFileID string `json:"src_file_id"`
	Version   string `json:"version"`
	Effects   []any  `json:"effects"`
}

type liveSmoothEffect struct {
	Category                 string `json:"category"`
	SkinSmoothStrength       int    `json:"skinSmoothStrength"`
	SkinSmoothColorIntensity int    `json:"skinSmoothColorIntensity"`
}

type liveLipShape struct {
	Name string `json:"name"`
}

type liveLipPalette struct {
	Color          string `json:"color"`
	Texture        string `json:"texture"`
	ColorIntensity int    `json:"colorIntensity"`
}

type liveLipStyle struct {
	Type string `json:"type"`
}

type liveLipEffect struct {
	Category string           `json:"category"`
	Shape    liveLipShape     `json:"shape"`
	Palettes []liveLipPalette `json:"palettes"`
	Style    liveLipStyle     `json:"style"`
}
