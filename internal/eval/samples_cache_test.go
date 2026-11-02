package eval

// The cached demo stories are derived data (T3.12): the flags, the
// issue, the pick, the runner-up, the pass fixes and a shown render's
// check facts are readings of the committed recordings at the shipped
// lines, produced by samples_cache.go. The assert mode runs in every
// ordinary gate - go test ./... proves the caches still regenerate from
// the recordings with no opt-in and no key - and rewriting the files is
// the named opt-in, the way the paid re-record is: the generator never
// touches data on its own.
//
// When the assert mode fails, the recordings or the shipped lines moved
// under the cache. That is a measurement, not a formatting accident:
// the failure names every file whose bytes disagree with the
// derivation, and the regeneration is one deliberate command away.

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// The opt-in, named after the DRESSCODE_RERECORD precedent: the same
// shape, the same value, and the same rule that anything else refuses.
const (
	regenerateCachesEnv   = "DRESSCODE_REGENERATE_CACHES"
	regenerateCachesValue = "yes"
)

func TestTheSampleCachesAreGeneratedData(t *testing.T) {
	root := repoRoot(t)
	derived, err := DeriveSampleCaches(root)
	if err != nil {
		t.Fatalf("derive the caches: %v", err)
	}
	if len(derived) != 5 {
		t.Fatalf("%d stories derived, want the five shipped ones", len(derived))
	}
	names := make([]string, 0, len(derived))
	for name := range derived {
		names = append(names, name)
	}
	sort.Strings(names)
	var moved []string
	for _, name := range names {
		path := filepath.Join(root, "testdata", "samples", "cached", name)
		committed, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !bytes.Equal(committed, derived[name]) {
			moved = append(moved, name)
		}
	}
	if len(moved) != 0 {
		t.Fatalf("the cached stories are derived data and %d of them are not what the recordings say: %v\n"+
			"a flag, an issue, a pick, a runner-up or a check fact moved under the cache. "+
			"If the new reading is the measurement, regenerate the data:\n"+
			"\t%s=%s go test ./internal/eval -run TestTheSampleCachesRegenerate",
			len(moved), moved, regenerateCachesEnv, regenerateCachesValue)
	}
}

// TestTheSampleCachesRegenerate is the named opt-in: it rewrites
// testdata/samples/cached from the recordings, then re-derives over the
// freshly written files and refuses unless the second derivation returns
// exactly what it wrote - a generator that is not idempotent is not a
// generator, it is a second author.
func TestTheSampleCachesRegenerate(t *testing.T) {
	if os.Getenv(regenerateCachesEnv) != regenerateCachesValue {
		t.Skipf("regeneration writes data: set %s=%s to rewrite testdata/samples/cached from the recordings",
			regenerateCachesEnv, regenerateCachesValue)
	}
	root := repoRoot(t)
	derived, err := DeriveSampleCaches(root)
	if err != nil {
		t.Fatalf("derive the caches: %v", err)
	}
	names := make([]string, 0, len(derived))
	for name := range derived {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(root, "testdata", "samples", "cached", name)
		if err := os.WriteFile(path, derived[name], 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("regenerated %s", path)
	}
	second, err := DeriveSampleCaches(root)
	if err != nil {
		t.Fatalf("derive the caches over the written files: %v", err)
	}
	for _, name := range names {
		if !bytes.Equal(second[name], derived[name]) {
			t.Errorf("%s: the derivation is not idempotent - writing it and reading it back disagree, "+
				"so the emitted bytes are not what the derivation of a derived file produces", name)
		}
	}
}
