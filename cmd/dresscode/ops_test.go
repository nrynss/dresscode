package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nrynss/dresscode/internal/config"
	"github.com/nrynss/dresscode/internal/server"
	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/sqlite"
)

// settingsFile writes a settings file with the example's own numbers
// and every secret named as an environment reference. No value is in
// the file; the test's lookup stands in for the environment.
func settingsFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dresscode.toml")
	var b strings.Builder
	b.WriteString(`
[budgets.youcam]
units = 1040
judging_units = 400

[budgets.workers_ai]
dollars_per_day = 1

[lease.judge]
fresh_selfies = 5
renders = 10

[flags]
fresh_uploads_off = false
renders_off = false
samples_only = false

[judge]
met = 0.628

[judge.per_criterion]
family-expression-present = 0.5466

[check]
change_made = 0.50085
same_person = 0.48355
criterion_met = 0.50315
model = "clef"
`)
	for _, name := range config.EnvVars() {
		fmt.Fprintf(&b, "\n[secrets.%s]\nsource = \"env\"\nvar = %q\n", strings.ToLower(name), name)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// standInEnv answers the ten key variables and nothing else, so the
// load resolves the references and no environment override invents a
// setting the file does not carry.
func standInEnv(name string) (string, bool) {
	for _, one := range config.EnvVars() {
		if name == one {
			return one + "-stand-in", true
		}
	}
	return "", false
}

// newDB creates the database file the commands work on, the way a
// server's first open leaves one behind.
func newDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dresscode.db")
	db, err := sqlite.Open(context.Background(), sqlite.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBudgetCommandPrintsSpentAndLeft(t *testing.T) {
	dbPath := newDB(t)
	configPath := settingsFile(t)

	var out, errOut bytes.Buffer
	if code := runBudget([]string{"-db", dbPath, "-config", configPath}, &out, &errOut, standInEnv); code != 0 {
		t.Fatalf("budget exit %d, stderr %s", code, errOut.String())
	}
	page := out.String()
	for _, want := range []string{
		"youcam units: spent 0  left 1040  ceiling 1040",
		"judging:     spent 0  left 400  ceiling 400",
		"development: spent 0  left 640  ceiling 640",
		"workers ai: spent $0.00  left $1.00  ceiling $1.00 a day",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("budget page is missing %q:\n%s", want, page)
		}
	}

	// The page is the books', not the file's: spend twelve units as
	// development, and the same command shows it.
	spend, err := server.OpenSpend(context.Background(), dbPath, &config.Settings{
		Budgets: config.Budgets{
			YouCam:    config.YouCamBudget{Units: 1040, JudgingUnits: 400},
			WorkersAI: config.WorkersAIBudget{DollarsPerDay: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	dev, err := spend.Development(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	meter, err := cost.NewContextMeter(dev, spend.Ledger())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := meter.Call(context.Background(), 12, "youcam.test", "t4.7", func(ctx context.Context) (cost.Usage, error) {
		return cost.Usage{Price: 12, Measured: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := spend.Close(); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if code := runBudget([]string{"-db", dbPath, "-config", configPath}, &out, &errOut, standInEnv); code != 0 {
		t.Fatalf("budget exit %d, stderr %s", code, errOut.String())
	}
	for _, want := range []string{
		"youcam units: spent 12  left 1028  ceiling 1040",
		"development: spent 12  left 628  ceiling 640",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("budget page is missing %q:\n%s", want, out.String())
		}
	}
}

func TestFlagCommandFlipsAndLists(t *testing.T) {
	dbPath := newDB(t)

	var out, errOut bytes.Buffer
	if code := runFlag([]string{"-db", dbPath}, &out, &errOut); code != 0 {
		t.Fatalf("flag exit %d, stderr %s", code, errOut.String())
	}
	page := out.String()
	for _, want := range []string{"fresh_uploads_off", "renders_off", "samples_only"} {
		if !strings.Contains(page, want) {
			t.Fatalf("flag page is missing %q:\n%s", want, page)
		}
	}
	// Nothing is set, and the change column is the dash a never-set
	// switch carries.
	if strings.Count(page, " off ") != 3 || strings.Contains(page, " on ") {
		t.Fatalf("flag page shows a set switch:\n%s", page)
	}

	out.Reset()
	if code := runFlag([]string{"-db", dbPath, "fresh_uploads_off", "on"}, &out, &errOut); code != 0 {
		t.Fatalf("flip exit %d, stderr %s", code, errOut.String())
	}
	if out.String() != "fresh_uploads_off on\n" {
		t.Fatalf("flip answer %q", out.String())
	}

	out.Reset()
	if code := runFlag([]string{"-db", dbPath}, &out, &errOut); code != 0 {
		t.Fatalf("flag exit %d, stderr %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "fresh_uploads_off  on ") {
		t.Fatalf("flag page after the flip:\n%s", out.String())
	}

	out.Reset()
	if code := runFlag([]string{"-db", dbPath, "fresh_uploads_off", "off"}, &out, &errOut); code != 0 {
		t.Fatalf("flip exit %d, stderr %s", code, errOut.String())
	}
	out.Reset()
	if code := runFlag([]string{"-db", dbPath}, &out, &errOut); code != 0 {
		t.Fatalf("flag exit %d, stderr %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "fresh_uploads_off  off ") {
		t.Fatalf("flag page after flipping back:\n%s", out.String())
	}
}

func TestFlagCommandRejectsBadInput(t *testing.T) {
	dbPath := newDB(t)

	var out, errOut bytes.Buffer
	if code := runFlag([]string{"-db", dbPath, "quiet_hours", "on"}, &out, &errOut); code != 1 {
		t.Fatalf("unknown flag exit %d, stderr %s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "unknown flag") {
		t.Fatalf("unknown flag stderr %q", errOut.String())
	}

	errOut.Reset()
	if code := runFlag([]string{"-db", dbPath, "renders_off", "maybe"}, &out, &errOut); code != 2 {
		t.Fatalf("bad value exit %d", code)
	}
	if !strings.Contains(errOut.String(), "on or off") {
		t.Fatalf("bad value stderr %q", errOut.String())
	}

	errOut.Reset()
	if code := runFlag([]string{"-db", dbPath, "renders_off"}, &out, &errOut); code != 2 {
		t.Fatalf("one argument exit %d", code)
	}
	if !strings.Contains(errOut.String(), "usage: dresscode flag") {
		t.Fatalf("one argument stderr %q", errOut.String())
	}

	errOut.Reset()
	if code := runFlag([]string{"-nope"}, &out, &errOut); code != 2 {
		t.Fatalf("bad flag exit %d", code)
	}
}

func TestBudgetCommandRejectsBadInput(t *testing.T) {
	configPath := settingsFile(t)

	var out, errOut bytes.Buffer
	if code := runBudget([]string{"-db", filepath.Join(t.TempDir(), "absent.db"), "-config", configPath}, &out, &errOut, standInEnv); code != 1 {
		t.Fatalf("missing database exit %d, stderr %s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "dresscode budget:") {
		t.Fatalf("missing database stderr %q", errOut.String())
	}

	errOut.Reset()
	if code := runBudget([]string{"-db", newDB(t), "-config", filepath.Join(t.TempDir(), "absent.toml")}, &out, &errOut, standInEnv); code != 1 {
		t.Fatalf("missing settings exit %d, stderr %s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "settings file") {
		t.Fatalf("missing settings stderr %q", errOut.String())
	}

	errOut.Reset()
	if code := runBudget([]string{"extra"}, &out, &errOut, standInEnv); code != 2 {
		t.Fatalf("stray argument exit %d", code)
	}
	if !strings.Contains(errOut.String(), "usage: dresscode budget") {
		t.Fatalf("stray argument stderr %q", errOut.String())
	}
}
