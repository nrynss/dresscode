package server

// The shipped judge and check lines are settings-file data, and this
// package scores its recordings against a hand-written copy of them
// (testSettings in looks_test.go). The copy is how three demo stories came
// to look broken in T3.9e: the paid re-record moved every line in
// dresscode.toml.example and the copy stayed on the pre-re-record values,
// silently, because nothing compared the two. This file is that comparison.
//
// It is an equality pin, not a second table of numbers. The values
// themselves are pinned to the evaluation where they are chosen, in
// internal/config/load_test.go and internal/eval; what is asserted here is
// the claim the demo's honesty proof quietly depends on, that the tests run
// on the lines the product ships. Both directions are checked, so a copy
// that gains an override drifts as loudly as one that loses it, and every
// disagreement is named in one run.
//
// The load is the zero-key, zero-network, zero-real-keyring shape
// internal/config's TestExampleLoads already proves: every secret name in
// config.EnvVars() gets a stand-in value from a LookupEnv closure, so the
// environment of the machine running the gate is never read, and the
// keyring source is a stub that fails the test if it is consulted at all.
// Nothing here prints a resolved secret: the error text is checked for a
// stand-in value before it is shown, and the only fields ever formatted are
// the judge lines, the check lines and the variable names.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nrynss/dresscode/internal/config"
)

// standinPrefix marks the values injected for secret names. Its text is
// never a key, and an error that carried one would be leaking it.
const standinPrefix = "server-settings-pin-standin-"

// TestShippedSettingsLinesMatchTheToml fails the moment testSettings'
// judge or check lines disagree with dresscode.toml.example.
func TestShippedSettingsLinesMatchTheToml(t *testing.T) {
	t.Parallel()
	shipped := loadShippedSettings(t)
	mirror := testSettings()

	if mirror.Judge.Met != shipped.Judge.Met {
		t.Errorf("testSettings judge met = %v, dresscode.toml.example ships met = %v",
			mirror.Judge.Met, shipped.Judge.Met)
	}

	for _, id := range overrideIDs(mirror.Judge.PerCriterion) {
		line, ok := shipped.Judge.PerCriterion[id]
		if !ok {
			t.Errorf("testSettings overrides %s at %v; the shipped file has no such override",
				id, mirror.Judge.PerCriterion[id])
			continue
		}
		if mirror.Judge.PerCriterion[id] != line {
			t.Errorf("testSettings overrides %s at %v, the shipped file overrides it at %v",
				id, mirror.Judge.PerCriterion[id], line)
		}
	}
	for _, id := range overrideIDs(shipped.Judge.PerCriterion) {
		if _, ok := mirror.Judge.PerCriterion[id]; !ok {
			t.Errorf("the shipped file overrides %s at %v; testSettings has no such override",
				id, shipped.Judge.PerCriterion[id])
		}
	}

	if mirror.Check.ChangeMade != shipped.Check.ChangeMade {
		t.Errorf("testSettings check change_made = %v, the shipped file ships %v",
			mirror.Check.ChangeMade, shipped.Check.ChangeMade)
	}
	if mirror.Check.SamePerson != shipped.Check.SamePerson {
		t.Errorf("testSettings check same_person = %v, the shipped file ships %v",
			mirror.Check.SamePerson, shipped.Check.SamePerson)
	}
	if mirror.Check.CriterionMet != shipped.Check.CriterionMet {
		t.Errorf("testSettings check criterion_met = %v, the shipped file ships %v",
			mirror.Check.CriterionMet, shipped.Check.CriterionMet)
	}
	if mirror.Check.Model != shipped.Check.Model {
		t.Errorf("testSettings runs the check on %q, the shipped file names %q",
			mirror.Check.Model, shipped.Check.Model)
	}
}

// loadShippedSettings loads dresscode.toml.example with stand-ins for every
// secret name and a keyring stub that must never be reached, and returns
// what the file says. It resolves no key, reads no environment variable of
// the host and makes no call.
func loadShippedSettings(t *testing.T) config.Settings {
	t.Helper()
	path := filepath.Join(repoRoot(t), "dresscode.toml.example")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the shipped settings file: %v", err)
	}

	values := make(map[string]string, len(config.EnvVars()))
	for i, name := range config.EnvVars() {
		values[name] = fmt.Sprintf("%s%02d", standinPrefix, i+1)
	}
	asked := make([]string, 0, len(values))

	got, _, err := config.Load(t.Context(), config.Options{
		Path: path,
		LookupEnv: func(name string) (string, bool) {
			v, ok := values[name]
			return v, ok
		},
		Keyring: func(_ context.Context, variable string) (string, error) {
			asked = append(asked, variable)
			return "", errors.New("absent")
		},
	})
	if err != nil {
		if strings.Contains(err.Error(), standinPrefix) {
			t.Fatal("config.Load returned an error carrying an injected stand-in value")
		}
		t.Fatalf("load the shipped settings file: %v", err)
	}
	if len(asked) != 0 {
		t.Fatalf("the keyring was consulted for %v while the stand-ins covered every name", asked)
	}
	return got
}

// overrideIDs sorts criterion ids so a diff reads the same way every run.
func overrideIDs(lines map[string]float64) []string {
	ids := make([]string, 0, len(lines))
	for id := range lines {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
