package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nrynss/dresscode/internal/config"
	"github.com/nrynss/keel/cost"
)

// openOpsServer opens a server whose database path the ops commands can
// work on, the way a second process would.
func openOpsServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	app := filepath.Join(dir, "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "index.html"), []byte(appIndex), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "dresscode.db")
	srv, err := Open(t.Context(), Config{
		DBPath:     dbPath,
		AppDir:     app,
		SigningKey: []byte("test-session-signing-key-32b!!!!"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv, dbPath
}

// testOpsSettings is a small split: ten units, four held for judges,
// six for everything else; one dollar a day.
func testOpsSettings() *config.Settings {
	return &config.Settings{
		Budgets: config.Budgets{
			YouCam:    config.YouCamBudget{Units: 10, JudgingUnits: 4},
			WorkersAI: config.WorkersAIBudget{DollarsPerDay: 1},
		},
	}
}

// settleCall runs one paid call that measures price and settles it, so
// a test can move the books the way a real provider call moves them.
func settleCall(t *testing.T, m *cost.Meter, price cost.Price, ref string) error {
	t.Helper()
	_, err := m.Call(t.Context(), price, "youcam.test", ref, func(ctx context.Context) (cost.Usage, error) {
		return cost.Usage{Price: price, Measured: true}, nil
	})
	return err
}

func TestFlagFlipTakesEffectOnTheNextRequest(t *testing.T) {
	srv, dbPath := openOpsServer(t)
	h := srv.Handler()
	userID, cookie := bootUser(t, h)
	ctx := t.Context()

	// The open position: a selfie uploads and becomes a photo.
	body := jpegWithEXIF(t, 80, 40, 6, "ops flip")
	first := uploadBytes(t, h, cookie, userID, groupSelfie, "image/jpeg", body)
	postPhoto(t, h, cookie, first)

	// The flip goes through the command's own mechanism: a second
	// database handle on the same file, one row, no restart.
	if err := SetFlag(ctx, dbPath, config.FlagFreshUploadsOff, true); err != nil {
		t.Fatal(err)
	}
	states, err := Flags(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var flipped *FlagState
	for i := range states {
		if states[i].Name == config.FlagFreshUploadsOff {
			flipped = &states[i]
		}
	}
	if flipped == nil || !flipped.On || flipped.Changed.IsZero() {
		t.Fatalf("fresh_uploads_off after the flip: %+v", flipped)
	}

	// The next requests are refused with the contract's shape: a selfie
	// open, and a photo made from an upload that was already complete.
	paused := postJSON(t, h, "/api/uploads", cookie, map[string]string{"content_type": "image/jpeg", "group": "selfie"})
	assertErr(t, paused, 403, codeUploadsPaused, msgUploadsPaused)
	assertErr(t, postJSON(t, h, "/api/photos", cookie, map[string]string{"upload_id": first}), 403, codeUploadsPaused, msgUploadsPaused)

	// A product open is not a fresh selfie; it still proceeds.
	product := postJSON(t, h, "/api/uploads", cookie, map[string]string{"content_type": "image/jpeg", "group": "product"})
	if product.Code != 201 {
		t.Fatalf("product open while uploads are paused: %d %s", product.Code, product.Body.Bytes())
	}

	// Flipped back, the next request works.
	if err := SetFlag(ctx, dbPath, config.FlagFreshUploadsOff, false); err != nil {
		t.Fatal(err)
	}
	second := uploadBytes(t, h, cookie, userID, groupSelfie, "image/jpeg", body)
	postPhoto(t, h, cookie, second)
}

func TestSamplesOnlyFlipWinsOverFreshUploads(t *testing.T) {
	srv, dbPath := openOpsServer(t)
	h := srv.Handler()
	_, cookie := bootUser(t, h)
	ctx := t.Context()

	if err := SetFlag(ctx, dbPath, config.FlagSamplesOnly, true); err != nil {
		t.Fatal(err)
	}
	// samples_only refuses every fresh intake path.
	assertErr(t, postJSON(t, h, "/api/photos", cookie, map[string]string{"upload_id": "0123456789abcdef0123456789abcdef"}), 403, codeSamplesOnly, msgSamplesOnly)
	assertErr(t, postJSON(t, h, "/api/uploads", cookie, map[string]string{"content_type": "image/jpeg", "group": "product"}), 403, codeSamplesOnly, msgSamplesOnly)
	assertErr(t, postJSON(t, h, "/api/products/link", cookie, map[string]string{"url": "https://example.com/item"}), 403, codeSamplesOnly, msgSamplesOnly)

	// With both switches on, samples_only wins.
	if err := SetFlag(ctx, dbPath, config.FlagFreshUploadsOff, true); err != nil {
		t.Fatal(err)
	}
	assertErr(t, postJSON(t, h, "/api/uploads", cookie, map[string]string{"content_type": "image/jpeg", "group": "selfie"}), 403, codeSamplesOnly, msgSamplesOnly)

	// Flipped off, the open position returns.
	for _, name := range []string{config.FlagSamplesOnly, config.FlagFreshUploadsOff} {
		if err := SetFlag(ctx, dbPath, name, false); err != nil {
			t.Fatal(err)
		}
	}
	states, err := Flags(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range states {
		if state.On {
			t.Fatalf("%s still on: %+v", state.Name, state)
		}
	}
	open := postJSON(t, h, "/api/uploads", cookie, map[string]string{"content_type": "image/jpeg", "group": "selfie"})
	if open.Code != 201 {
		t.Fatalf("selfie open after the flips are off: %d %s", open.Code, open.Body.Bytes())
	}
}

func TestSetFlagRefusesAnUnknownName(t *testing.T) {
	_, dbPath := openOpsServer(t)
	err := SetFlag(t.Context(), dbPath, "renders_of", true)
	if err == nil {
		t.Fatal("an unknown name flipped nothing and complained of nothing")
	}
	for _, want := range []string{"unknown flag", config.FlagRendersOff} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
	// The miss wrote nothing: the real switch still reads off.
	states, err := Flags(t.Context(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 3 {
		t.Fatalf("flags %+v", states)
	}
	for _, state := range states {
		if state.On {
			t.Fatalf("%s on after a refused flip", state.Name)
		}
	}
}

func TestFlagsListTheThreeSwitches(t *testing.T) {
	_, dbPath := openOpsServer(t)
	states, err := Flags(t.Context(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{config.FlagFreshUploadsOff, config.FlagRendersOff, config.FlagSamplesOnly}
	if len(states) != len(want) {
		t.Fatalf("flags %+v, want %v", states, want)
	}
	for i, state := range states {
		if state.Name != want[i] {
			t.Fatalf("flag %d is %s, want %s", i, state.Name, want[i])
		}
		if state.On || state.Help == "" || !state.Changed.IsZero() {
			t.Fatalf("unset switch %+v reads as set", state)
		}
	}
}

func TestBudgetReportAndTheJudgingShareHolds(t *testing.T) {
	_, dbPath := openOpsServer(t)
	settings := testOpsSettings()
	ctx := t.Context()

	// The books open beside the server's own namespaces, and a fresh
	// pool reports the file's ceilings with nothing spent.
	rep, err := Budget(ctx, dbPath, settings)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Units.Ceiling != 10 || rep.Units.Spent != 0 || rep.Units.Left != 10 {
		t.Fatalf("units report %+v, want ceiling 10 spent 0 left 10", rep.Units)
	}
	if rep.Units.Judging != (ShareReport{Ceiling: 4, Spent: 0, Left: 4}) {
		t.Fatalf("judging report %+v", rep.Units.Judging)
	}
	if rep.Units.Development != (ShareReport{Ceiling: 6, Spent: 0, Left: 6}) {
		t.Fatalf("development report %+v", rep.Units.Development)
	}
	if rep.Dollars != (DollarsReport{PerDay: cost.Dollar, Spent: 0, Left: cost.Dollar}) {
		t.Fatalf("dollars report %+v", rep.Dollars)
	}

	spend, err := OpenSpend(ctx, dbPath, settings)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = spend.Close() }()

	// The start's pre-read is the development share's headroom.
	left, err := spend.UnitsLeft(ctx)
	if err != nil || left != 6 {
		t.Fatalf("units left %d, %v; want 6", left, err)
	}

	// Development spends its whole share; the pool now holds only the
	// judging units.
	dev, err := spend.Development(ctx)
	if err != nil {
		t.Fatal(err)
	}
	devMeter, err := cost.NewContextMeter(dev, spend.Ledger())
	if err != nil {
		t.Fatal(err)
	}
	if err := settleCall(t, devMeter, 6, "dev-1"); err != nil {
		t.Fatalf("development spends its six: %v", err)
	}

	// The judging share is refused to development: the seventh unit is
	// the budget's own typed refusal, the one the render stage carries
	// as budget_refused.
	err = settleCall(t, devMeter, 1, "dev-2")
	if !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("development reaching for the judging share: %v, want cost.ErrOverBudget", err)
	}

	// A judge lease draws its own share.
	jud, err := spend.Judging(ctx)
	if err != nil {
		t.Fatal(err)
	}
	judMeter, err := cost.NewContextMeter(jud, spend.Ledger())
	if err != nil {
		t.Fatal(err)
	}
	if err := settleCall(t, judMeter, 4, "judging-1"); err != nil {
		t.Fatalf("the judging share pays for a judge lease: %v", err)
	}
	judLeft, err := spend.Units().Remaining(ctx, config.OwnerJudging)
	if err != nil || judLeft != 0 {
		t.Fatalf("judging left %d, %v; want 0", judLeft, err)
	}

	// And when the share is gone, it is gone for its own owner too.
	err = settleCall(t, judMeter, 1, "judging-2")
	if !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("judging past its share: %v, want cost.ErrOverBudget", err)
	}

	final, err := spend.Report(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if final.Units != (UnitsReport{
		Ceiling: 10, Spent: 10, Left: 0,
		Judging:     ShareReport{Ceiling: 4, Spent: 4, Left: 0},
		Development: ShareReport{Ceiling: 6, Spent: 6, Left: 0},
	}) {
		t.Fatalf("final units report %+v", final.Units)
	}
	if final.Dollars.Spent != 0 {
		t.Fatalf("dollars moved in a units test: %+v", final.Dollars)
	}
}

func TestDollarBooksBoundOneUTCDay(t *testing.T) {
	_, dbPath := openOpsServer(t)
	settings := testOpsSettings()
	ctx := t.Context()

	spend, err := OpenSpend(ctx, dbPath, settings)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = spend.Close() }()

	meter, err := cost.NewContextMeter(spend.Dollars().Account(), spend.Dollars())
	if err != nil {
		t.Fatal(err)
	}
	if err := settleCall(t, meter, cost.Dollar/4, "workers-1"); err != nil {
		t.Fatalf("a quarter day of dollars: %v", err)
	}
	rep, err := spend.Report(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Dollars != (DollarsReport{PerDay: cost.Dollar, Spent: cost.Dollar / 4, Left: cost.Dollar * 3 / 4}) {
		t.Fatalf("dollars report %+v", rep.Dollars)
	}
	err = settleCall(t, meter, cost.Dollar, "workers-2")
	if !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("past the day's dollars: %v, want cost.ErrOverBudget", err)
	}
}

func TestOpenSpendRefusesABrokenSplit(t *testing.T) {
	_, dbPath := openOpsServer(t)
	broken := &config.Settings{Budgets: config.Budgets{
		YouCam: config.YouCamBudget{Units: 10, JudgingUnits: 11},
	}}
	if _, err := OpenSpend(t.Context(), dbPath, broken); err == nil {
		t.Fatal("a judging share bigger than the pool opened")
	}
	empty := &config.Settings{Budgets: config.Budgets{
		YouCam: config.YouCamBudget{Units: 10, JudgingUnits: 4},
	}}
	if _, err := OpenSpend(t.Context(), dbPath, empty); err == nil {
		t.Fatal("a dollar ceiling of zero opened")
	}
}

func TestOpsRefuseAMissingDatabase(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.db")
	if _, err := Flags(t.Context(), missing); err == nil {
		t.Fatal("flags opened a missing database")
	}
	if err := SetFlag(t.Context(), missing, config.FlagRendersOff, true); err == nil {
		t.Fatal("setflag opened a missing database")
	}
	settings := testOpsSettings()
	if _, err := Budget(t.Context(), missing, settings); err == nil {
		t.Fatal("budget opened a missing database")
	}
}
