package eval

// The cached demo stories are derived data (T3.12). Every measured field
// of a story - the flags, the biggest issue, the pick, the runner-up,
// each pass's fix id, and a shown render's three check facts, its look
// answers and its shown flag - is a reading of the committed recordings
// at the shipped [judge] and [check] lines. DeriveSampleCaches computes
// them from the same sources the product reads:
//
//   - testdata/clef: the judge and check recordings, replayed through
//     the real pipeline.Judge and pipeline.Check, so no threshold rule
//     is restated here (the pipeline owns it) and nothing is
//     re-thresholded (the shipped lines are loaded, never retyped);
//   - dresscode.toml.example: the shipped [judge] and [check] lines,
//     read through config.Load exactly as the product reads them - a
//     hand-copied line here would repeat the bug T3.9e found;
//   - testdata/eval/ground-truth.json: the miss lists the derived flags
//     are cross-checked against;
//   - testdata/api/look.json: the contract's recorded example, which is
//     what sample-family's own source states its pair from;
//   - testdata/occasions: the criteria, the fix menu and the vocabulary.
//
// What stays authored is the half a recording cannot speak: the name,
// the occasion, the variant, the selfie, the source prose, the order and
// labels of the attempts, the pictures a pass names, and which
// recording a shown render stands on. Derivation rewrites the measured
// values inside the committed bytes - it splices each value into the
// span it occupies and touches nothing else - so a regenerated file
// moves exactly the bytes the measurement moved, and the formatting of
// an untouched field cannot drift.
//
// Nothing here dials, spends or asks: no key, no network, no live call.
// The assert mode (samples_cache_test.go) runs in the ordinary gate;
// rewriting the files is the named opt-in, following the precedent of
// DRESSCODE_RERECORD.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nrynss/dresscode/internal/config"
	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/pipeline"
	"github.com/nrynss/dresscode/internal/systemone"
)

// RunnerUp is the rule the date, festival and funeral stories state in
// their own source for the runner-up no recording pairs: the fix of the
// lowest-read flagged criterion the pick does not address and that some
// fix on the menu addresses. A flagged criterion no fix on the menu
// serves (funeral's expression-still and light-plain) is skipped, for a
// runner-up that cannot render is no runner-up; a criterion the pick
// already answers is skipped too. The read that decides is the
// recording's own noul, so a threshold edit that reorders the flags
// reorders the runner-up. A story with no pick and no flag names no
// runner-up, and the empty fix says so. The fix that renders a criterion
// is the first to declare it, in the menu's own order.
//
// This is the rule's one home (T3.12): the generator that derives the
// caches and the honesty test that re-executes clause (e) call this one
// function, so the two can never drift into two readings of one story.
func RunnerUp(occ *domain.Occasion, nouls map[string]float64, flagged map[string]bool, pickFix string) string {
	picked := menuFix(occ, pickFix)
	serves := func(criterionID string) bool {
		if picked == nil {
			return false
		}
		for _, id := range picked.Criteria {
			if id == criterionID {
				return true
			}
		}
		return false
	}
	theFix := func(criterionID string) string {
		for i := range occ.Fixes {
			for _, id := range occ.Fixes[i].Criteria {
				if id == criterionID {
					return occ.Fixes[i].ID
				}
			}
		}
		return ""
	}
	bestFix, bestNoul := "", 0.0
	for _, c := range occ.Criteria {
		if !flagged[c.ID] || serves(c.ID) {
			continue
		}
		noul, read := nouls[c.ID]
		if !read {
			continue
		}
		fid := theFix(c.ID)
		if fid == "" {
			continue
		}
		if bestFix == "" || noul < bestNoul {
			bestFix, bestNoul = fid, noul
		}
	}
	return bestFix
}

// menuFix finds a fix on the occasion's own menu, by id.
func menuFix(occ *domain.Occasion, id string) *domain.Fix {
	if id == "" {
		return nil
	}
	for i := range occ.Fixes {
		if occ.Fixes[i].ID == id {
			return &occ.Fixes[i]
		}
	}
	return nil
}

// settingsStandinPrefix marks the values this generator injects for the
// secret names in config.EnvVars(), so the environment of the machine
// running the gate is never read. The text is never a key, and an error
// that carried one would be leaking it, so the loader checks its own
// error text before returning it, and the keyring stub must never be
// consulted at all.
const settingsStandinPrefix = "eval-samples-cache-standin-"

// loadShippedSettings reads dresscode.toml.example through config.Load
// and returns what the file ships. The generator takes the shipped
// [judge] and [check] lines as data, the way the product does; it
// resolves no key, reads no environment variable of the host and makes
// no call.
func loadShippedSettings(root string) (config.Settings, error) {
	path := filepath.Join(root, "dresscode.toml.example")
	if _, err := os.Stat(path); err != nil {
		return config.Settings{}, fmt.Errorf("eval: the shipped settings file: %w", err)
	}
	values := make(map[string]string, len(config.EnvVars()))
	for i, name := range config.EnvVars() {
		values[name] = fmt.Sprintf("%s%02d", settingsStandinPrefix, i+1)
	}
	var asked []string
	got, _, err := config.Load(context.Background(), config.Options{
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
		if strings.Contains(err.Error(), settingsStandinPrefix) {
			return config.Settings{}, errors.New("eval: loading the shipped settings returned an error carrying an injected stand-in value")
		}
		return config.Settings{}, fmt.Errorf("eval: load the shipped settings: %w", err)
	}
	if len(asked) != 0 {
		return config.Settings{}, fmt.Errorf("eval: the keyring was consulted for %v while the stand-ins covered every name", asked)
	}
	return got, nil
}

// cacheKind is the member every cached story file carries; a file of
// another kind is not a story and the generator refuses the directory.
const cacheKind = "sample-cache"

// cacheStory is the authored frame of one cached story: only the members
// the derivation reads as inputs, never the measured values it produces.
// The variant and the occasion locate the recording; the selfie, the
// attempt order, the attempt labels, the pictures a pass names and the
// recording a shown render stands on are authored evidence the
// derivation proves the measured values belong to - a recording cannot
// say which of its passes the story chose to narrate.
type cacheStory struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Occasion string `json:"occasion"`
	Variant  string `json:"variant"`
	Selfie   string `json:"selfie"`
	Attempts []struct {
		Fix     string `json:"fix"`
		Attempt string `json:"attempt"`
		Image   string `json:"image"`
		Check   *struct {
			Recording string `json:"recording"`
		} `json:"check"`
	} `json:"attempts"`
}

// DeriveSampleCaches derives every cached story in
// <root>/testdata/samples/cached from the recordings, the shipped lines
// and the ground truth, and returns the derived bytes by file name. The
// committed bytes are inputs only for the authored frame named above:
// each measured value is spliced into the span the key holds, so an
// authored field keeps its exact bytes.
//
// A derivation that cannot say why a story is what it is returns an
// error naming the story: the generator invents no value to get past
// one, and writes nothing at all when it errors.
func DeriveSampleCaches(root string) (map[string][]byte, error) {
	settings, err := loadShippedSettings(root)
	if err != nil {
		return nil, err
	}
	loaded, err := domain.Load(filepath.Join(root, "testdata", "occasions"))
	if err != nil {
		return nil, fmt.Errorf("eval: load occasions: %w", err)
	}
	occasions := make(map[string]*domain.Occasion, len(loaded.Occasions))
	for i := range loaded.Occasions {
		occ := loaded.Occasions[i]
		occasions[occ.ID] = &occ
	}
	vocab := &loaded.Vocabulary
	gt, err := LoadGroundTruth(filepath.Join(root, "testdata", "eval", "ground-truth.json"))
	if err != nil {
		return nil, err
	}
	misses := make(map[string][]string, len(gt.Samples))
	for _, s := range gt.Samples {
		misses[s.ID] = s.Miss
	}
	familyFix, familyRunner, err := lookFixturePair(filepath.Join(root, "testdata", "api", "look.json"))
	if err != nil {
		return nil, err
	}
	clefDir := filepath.Join(root, "testdata", "clef")
	replay, err := systemone.Open(clefDir)
	if err != nil {
		return nil, fmt.Errorf("eval: open testdata/clef: %w", err)
	}
	checks, err := clefCheckRecordings(clefDir)
	if err != nil {
		return nil, err
	}

	files, err := filepath.Glob(filepath.Join(root, "testdata", "samples", "cached", "*.json"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("eval: no cached story files")
	}
	out := make(map[string][]byte, len(files))
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("eval: %s: %w", filepath.Base(file), err)
		}
		derived, err := deriveCacheFile(raw, filepath.Base(file), settings, occasions, vocab, misses,
			familyFix, familyRunner, replay, clefDir, checks)
		if err != nil {
			return nil, fmt.Errorf("eval: %s: %w", filepath.Base(file), err)
		}
		out[filepath.Base(file)] = derived
	}
	return out, nil
}

// lookFixturePair reads the pair sample-family's own source names: the
// fix id and the runner-up of the contract's recorded example.
func lookFixturePair(path string) (string, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("eval: %s: %w", path, err)
	}
	var f struct {
		Body struct {
			Fix struct {
				ID string `json:"id"`
			} `json:"fix"`
			RunnerUp string `json:"runner_up"`
		} `json:"body"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return "", "", fmt.Errorf("eval: %s: %w", path, err)
	}
	if f.Body.Fix.ID == "" || f.Body.RunnerUp == "" {
		return "", "", fmt.Errorf("eval: %s: the recorded example names no fix pair", path)
	}
	return f.Body.Fix.ID, f.Body.RunnerUp, nil
}

// clefCheckRecordings indexes every check recording by its exchange id,
// the name a shown attempt carries.
func clefCheckRecordings(clefDir string) (map[string]recordingFile, error) {
	names, err := filepath.Glob(filepath.Join(clefDir, "check-*.json"))
	if err != nil {
		return nil, err
	}
	out := make(map[string]recordingFile, len(names))
	for _, name := range names {
		rec, err := readRecording(name)
		if err != nil {
			return nil, err
		}
		out[rec.Exchange] = rec
	}
	return out, nil
}

// checkFields is one check recording's authored request frame: the
// occasion it read, the criterion it was asked about, the change it was
// asked about in its own words, and the sample-directory path of the
// original picture - the selfie the answers are about.
type checkFields struct {
	occasion  string
	criterion string
	requested string
	original  string
}

func readCheckFields(rec recordingFile) (checkFields, error) {
	fields, err := stateFields(rec.Request.Body.State, "occasion", "criterion", "requested_change")
	if err != nil {
		return checkFields{}, err
	}
	cf := checkFields{occasion: fields[0], criterion: fields[1], requested: fields[2]}
	if len(rec.Request.Body.Images) > 0 {
		cf.original = strings.TrimPrefix(rec.Request.Body.Images[0].File, "../samples/")
	}
	return cf, nil
}

// deriveCacheFile derives one story's measured values and splices them
// into the committed bytes.
func deriveCacheFile(raw []byte, name string, settings config.Settings, occasions map[string]*domain.Occasion,
	vocab *domain.Vocabulary, misses map[string][]string, familyFix, familyRunner string,
	replay *systemone.Replay, clefDir string, checks map[string]recordingFile) ([]byte, error) {
	sid := strings.TrimSuffix(name, ".json")
	var story cacheStory
	if err := json.Unmarshal(raw, &story); err != nil {
		return nil, err
	}
	if story.Kind != cacheKind {
		return nil, fmt.Errorf("kind %q, want %q", story.Kind, cacheKind)
	}
	if story.ID != sid {
		return nil, fmt.Errorf("the file names %q", story.ID)
	}
	occ, ok := occasions[story.Occasion]
	if !ok {
		return nil, fmt.Errorf("no occasion %q", story.Occasion)
	}

	// The judge reading: the story's own recording, replayed through the
	// real pipeline at the shipped lines. The capture carries the nouls
	// the rule reads; the flags and the issue are the pipeline's own.
	rec, err := readRecording(filepath.Join(clefDir, "judge-"+story.Variant+"-"+story.Occasion+".json"))
	if err != nil {
		return nil, err
	}
	if len(rec.Request.Body.Images) != 1 {
		return nil, fmt.Errorf("judge-%s-%s carries %d request images, want the one photo",
			story.Variant, story.Occasion, len(rec.Request.Body.Images))
	}
	photo, err := readImage(clefDir, rec.Request.Body.Images[0])
	if err != nil {
		return nil, err
	}
	capture := &captureClient{inner: replay}
	th := thresholdsFor(occ, JudgeSettings{Met: settings.Judge.Met, PerCriterion: settings.Judge.PerCriterion})
	judgement, err := pipeline.Judge(context.Background(), capture, photo, occ, th)
	if err != nil {
		return nil, fmt.Errorf("the judge over the recording: %w", err)
	}
	flags := make([]string, 0, len(judgement.Flags))
	flagged := make(map[string]bool, len(judgement.Flags))
	for _, c := range judgement.Flags {
		flags = append(flags, c.ID)
		flagged[c.ID] = true
	}
	nouls := make(map[string]float64, len(occ.Criteria))
	for _, c := range occ.Criteria {
		answer, ok := capture.resp.Answers[c.ID]
		if !ok || answer.Type != systemone.TypeNoul || answer.Noul == nil {
			return nil, fmt.Errorf("the recording answers no yes-probability for %q", c.ID)
		}
		nouls[c.ID] = *answer.Noul
	}

	// The ground truth is the cross-check the ruling keeps: the flags the
	// recording reads at the shipped lines must be the variant's own miss
	// list, item for item, or the measurement and the transcription
	// disagree and no story may be written from either.
	if strings.Join(flags, ",") != strings.Join(misses[story.Variant], ",") {
		return nil, fmt.Errorf("%s flags %v, the ground truth's miss list says %v",
			story.Variant, flags, misses[story.Variant])
	}

	// The pick and the runner-up. A look that flagged nothing picks
	// nothing. sample-family names the contract's recorded example, the
	// way its own source states; the others read the menu and the rule.
	issue := judgement.Issue.ID
	if issue == "cant-tell" {
		return nil, fmt.Errorf("the recording answered can't tell, which no shipped story states")
	}
	pickFix, runnerUp := "", ""
	if len(flags) > 0 {
		if sid == "sample-family" {
			pickFix, runnerUp = familyFix, familyRunner
		} else {
			if issue == "none" {
				return nil, fmt.Errorf("the flags are %v but the gated issue is none", flags)
			}
			pick := fixFor(occ, issue)
			if pick == nil {
				return nil, fmt.Errorf("the issue %q is served by no single fix on the menu", issue)
			}
			pickFix = pick.ID
			runnerUp = RunnerUp(occ, nouls, flagged, pickFix)
		}
		if runnerUp == pickFix {
			return nil, fmt.Errorf("the runner-up reads out as the pick %q", runnerUp)
		}
	}

	spans, err := spansOf(raw)
	if err != nil {
		return nil, err
	}
	var edits []edit
	replace := func(path string, value []byte) error {
		s, ok := spans[path]
		if !ok {
			return fmt.Errorf("the file holds no %s", path)
		}
		edits = append(edits, edit{span: s, value: value})
		return nil
	}
	flagSpan, ok := spans["judge.flags"]
	if !ok {
		return nil, errors.New("the file holds no judge.flags")
	}
	if err := replace("judge.flags", arrayValue(flags, lineIndent(raw, flagSpan.begin), "flags")); err != nil {
		return nil, err
	}
	if err := replace("judge.issue", jsonString(issue)); err != nil {
		return nil, err
	}
	if err := replace("pick.fix", jsonString(pickFix)); err != nil {
		return nil, err
	}
	if err := replace("pick.runner_up", jsonString(runnerUp)); err != nil {
		return nil, err
	}

	// The attempts: the pick's first pass, then the runner-up's when
	// there is one - the loader's own prefix rule, read here as the
	// derivation's, so a pass fix is always the pick it belongs to. The
	// analysis rows are authored and stay untouched: every shipped story
	// holds none, because a row must name a recorded read of this
	// story's own picture and no recording read any sample selfie here.
	if len(story.Attempts) > 2 {
		return nil, fmt.Errorf("%d attempts, want at most two", len(story.Attempts))
	}
	if len(story.Attempts) > 0 && pickFix == "" {
		return nil, errors.New("attempts without a pick")
	}
	wants := [2]struct{ fix, label string }{{pickFix, "fix"}, {runnerUp, "runner_up"}}
	for i := range story.Attempts {
		at := story.Attempts[i]
		if at.Attempt != wants[i].label {
			return nil, fmt.Errorf("attempt %d is labelled %q, want %q", i, at.Attempt, wants[i].label)
		}
		fix := menuFix(occ, wants[i].fix)
		if err := replace(fmt.Sprintf("attempts[%d].fix", i), jsonString(wants[i].fix)); err != nil {
			return nil, err
		}
		shownPath := fmt.Sprintf("attempts[%d].shown", i)
		if at.Check == nil {
			// No check block: the pass showed nothing, and nothing
			// recorded could have shown it - the rule clause (g)'s tail
			// reads, here as a generation refusal.
			if err := replace(shownPath, jsonBool(false)); err != nil {
				return nil, err
			}
			if err := refuseShownElsewhere(occ, vocab, story, settings, replay, clefDir, checks, i); err != nil {
				return nil, err
			}
			continue
		}
		if fix == nil {
			return nil, fmt.Errorf("attempt %d names no fix on the menu", i)
		}
		rcNameSpan, ok := spans[fmt.Sprintf("attempts[%d].check.recording", i)]
		if !ok {
			return nil, fmt.Errorf("attempt %d's check names no recording member", i)
		}
		var recordingName string
		if err := json.Unmarshal(raw[rcNameSpan.begin:rcNameSpan.end], &recordingName); err != nil {
			return nil, fmt.Errorf("attempt %d's recording name: %w", i, err)
		}
		rc, ok := checks[recordingName]
		if !ok {
			return nil, fmt.Errorf("attempt %d stands on %q, which is not a recorded render check", i, recordingName)
		}
		cf, err := readCheckFields(rc)
		if err != nil {
			return nil, fmt.Errorf("attempt %d: %s: %w", i, recordingName, err)
		}
		if cf.original != story.Selfie {
			return nil, fmt.Errorf("attempt %d stands on %s, which read %q as the original, not this story's %q",
				i, recordingName, cf.original, story.Selfie)
		}
		outcome, err := replayCheck(rc, fix, occ, vocab, settings, replay, clefDir)
		if err != nil {
			return nil, fmt.Errorf("attempt %d: %w", i, err)
		}
		if !outcome.Shown() {
			return nil, fmt.Errorf("attempt %d carries a check block, but %s reads the pass as a discard", i, recordingName)
		}
		if at.Image == "" {
			return nil, fmt.Errorf("attempt %d is shown by %s and names no picture", i, recordingName)
		}
		if err := replace(shownPath, jsonBool(outcome.Shown())); err != nil {
			return nil, err
		}
		gates := []struct {
			id    string
			value bool
		}{
			{"change_made", outcome.Check.ChangeMade},
			{"same_person", outcome.Check.SamePerson},
			{"criterion_met", outcome.Check.CriterionMet},
		}
		for _, g := range gates {
			if err := replace(fmt.Sprintf("attempts[%d].check.%s", i, g.id), jsonBool(g.value)); err != nil {
				return nil, err
			}
		}
		// The look in the cache is the fix's declared attributes, in the
		// fix's own order, each carrying the value this recording read.
		// An attribute the fix declares and the reading has no answer
		// for is a story the check cannot tell, and the generator says
		// so instead of writing a value the model never gave.
		attribs := make([]domain.AnsweredAttribute, 0, len(fix.Vocabulary))
		for _, change := range fix.Vocabulary {
			value, ok := attributeRead(outcome.Check.Attributes, change.Attribute)
			if !ok {
				return nil, fmt.Errorf("%s read no look.%s, which fix %s declares",
					recordingName, change.Attribute, fix.ID)
			}
			attribs = append(attribs, domain.AnsweredAttribute{ID: change.Attribute, Value: value})
		}
		attrSpan, ok := spans[fmt.Sprintf("attempts[%d].check.attributes", i)]
		if !ok {
			return nil, fmt.Errorf("attempt %d's check holds no attributes", i)
		}
		if err := replace(fmt.Sprintf("attempts[%d].check.attributes", i),
			attributeValue(attribs, lineIndent(raw, attrSpan.begin))); err != nil {
			return nil, err
		}
	}

	return splice(raw, edits), nil
}

// attributeRead finds one attribute's read value in a check outcome.
func attributeRead(attrs []domain.AnsweredAttribute, id string) (string, bool) {
	for _, a := range attrs {
		if a.ID == id {
			return a.Value, true
		}
	}
	return "", false
}

// replayCheck runs the real pipeline.Check over one recorded check at
// the shipped lines, on the model and with the words the recording
// itself carries. The replay serves only the identical request, so a
// read here is also the proof that the check builds this recording byte
// for byte.
func replayCheck(rc recordingFile, fix *domain.Fix, occ *domain.Occasion, vocab *domain.Vocabulary,
	settings config.Settings, replay *systemone.Replay, clefDir string) (pipeline.Outcome, error) {
	if rc.Request.Body.Model != settings.Check.Model {
		return pipeline.Outcome{}, fmt.Errorf("%s ran on %q, while the shipped check model is %q",
			rc.Exchange, rc.Request.Body.Model, settings.Check.Model)
	}
	cf, err := readCheckFields(rc)
	if err != nil {
		return pipeline.Outcome{}, err
	}
	if cf.occasion != occ.ID {
		return pipeline.Outcome{}, fmt.Errorf("%s read the %s occasion, not %s", rc.Exchange, cf.occasion, occ.ID)
	}
	if fix == nil {
		return pipeline.Outcome{}, errors.New("the attempt names no fix on the menu")
	}
	if len(rc.Request.Body.Images) != 2 {
		return pipeline.Outcome{}, fmt.Errorf("%s carries %d images, want the original and the render",
			rc.Exchange, len(rc.Request.Body.Images))
	}
	original, err := readImage(clefDir, rc.Request.Body.Images[0])
	if err != nil {
		return pipeline.Outcome{}, err
	}
	render, err := readImage(clefDir, rc.Request.Body.Images[1])
	if err != nil {
		return pipeline.Outcome{}, err
	}
	return pipeline.Check(context.Background(), replay, rc.Request.Body.Model, original, render,
		occ, fix, cf.criterion, cf.requested, vocab, pipeline.Lines{
			ChangeMade:   settings.Check.ChangeMade,
			SamePerson:   settings.Check.SamePerson,
			CriterionMet: settings.Check.CriterionMet,
		})
}

// refuseShownElsewhere proves a pass without a check block could not
// have shown: no recorded check of this story's occasion, this story's
// own selfie and the shipped model reads as shown at the shipped lines.
// The sample set records four render checks, all of them family's V04,
// so for the other four stories the sweep finds nothing to answer with,
// which is exactly why they show no render.
func refuseShownElsewhere(occ *domain.Occasion, vocab *domain.Vocabulary, story cacheStory,
	settings config.Settings, replay *systemone.Replay, clefDir string,
	checks map[string]recordingFile, attempt int) error {
	names := make([]string, 0, len(checks))
	for name := range checks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		rc := checks[name]
		if rc.Request.Body.Model != settings.Check.Model {
			continue
		}
		cf, err := readCheckFields(rc)
		if err != nil {
			return err
		}
		if cf.occasion != story.Occasion || cf.original != story.Selfie {
			continue
		}
		fix := fixFor(occ, cf.criterion)
		if fix == nil {
			return fmt.Errorf("%s checks %q, a criterion no single fix serves", rc.Exchange, cf.criterion)
		}
		outcome, err := replayCheck(rc, fix, occ, vocab, settings, replay, clefDir)
		if err != nil {
			return err
		}
		if outcome.Shown() {
			return fmt.Errorf("attempt %d names no check, but %s reads a render of this story as shown",
				attempt, rc.Exchange)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// The span splicer: derived values into committed bytes.

// span is one value's byte range [begin, end) in a JSON file.
type span struct{ begin, end int64 }

// edit is one value to write over one span.
type edit struct {
	span  span
	value []byte
}

// spansOf indexes every value of a JSON document by path:
// "judge.flags", "attempts[1].fix", "attempts[0].check.attributes". The
// root object's own path is "".
func spansOf(data []byte) (map[string]span, error) {
	spans := map[string]span{}
	dec := json.NewDecoder(bytes.NewReader(data))
	var walk func(path string) (span, error)
	walk = func(path string) (span, error) {
		before := dec.InputOffset()
		t, err := dec.Token()
		if err != nil {
			return span{}, err
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				for dec.More() {
					kt, err := dec.Token()
					if err != nil {
						return span{}, err
					}
					key, ok := kt.(string)
					if !ok {
						return span{}, errors.New("an object key is not a string")
					}
					if _, err := walk(joinPath(path, key)); err != nil {
						return span{}, err
					}
				}
				if _, err := dec.Token(); err != nil { // the closing brace
					return span{}, err
				}
			case '[':
				i := 0
				for dec.More() {
					if _, err := walk(fmt.Sprintf("%s[%d]", path, i)); err != nil {
						return span{}, err
					}
					i++
				}
				if _, err := dec.Token(); err != nil { // the closing bracket
					return span{}, err
				}
			default:
				return span{}, fmt.Errorf("unexpected closing delimiter %v", delim)
			}
			s := span{valueBegin(data, before, dec.InputOffset()), dec.InputOffset()}
			spans[path] = s
			return s, nil
		}
		s := span{valueBegin(data, before, dec.InputOffset()), dec.InputOffset()}
		spans[path] = s
		return s, nil
	}
	if _, err := walk(""); err != nil {
		return nil, err
	}
	return spans, nil
}

// valueBegin is where a token's text starts: the first byte after the
// separators the scanner ate on the way to it. A JSON value never begins
// with whitespace, a comma or a colon, so trimming exactly those lands
// on the value's own first byte.
func valueBegin(data []byte, before, end int64) int64 {
	i := before
	for i < end {
		switch data[i] {
		case ' ', '\t', '\n', '\r', ',', ':':
			i++
			continue
		}
		break
	}
	return i
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// lineIndent counts the line's leading spaces from a value's begin: the
// key's own indent, which is the indent every re-emitted form keeps.
// Every value this generator rewrites begins on its key's line.
func lineIndent(data []byte, at int64) int {
	line := int64(bytes.LastIndexByte(data[:at], '\n') + 1)
	n := 0
	for line+int64(n) < at && data[line+int64(n)] == ' ' {
		n++
	}
	return n
}

// splice applies the edits right to left, so every untouched byte stays
// exactly where the committed file had it.
func splice(data []byte, edits []edit) []byte {
	sort.Slice(edits, func(i, j int) bool { return edits[i].span.begin > edits[j].span.begin })
	out := append([]byte(nil), data...)
	for _, e := range edits {
		replaced := append([]byte(nil), out[:e.span.begin]...)
		replaced = append(replaced, e.value...)
		replaced = append(replaced, out[e.span.end:]...)
		out = replaced
	}
	return out
}

// jsonString quotes one value. encoding/json marshals every Go string -
// invalid UTF-8 is replaced, never refused - so no error can arrive
// here from a data id.
func jsonString(v string) []byte {
	raw, _ := json.Marshal(v)
	return raw
}

func jsonBool(v bool) []byte {
	raw, _ := json.Marshal(v)
	return raw
}

// inlineBudget is the column a flags line may reach before the array
// breaks at the bracket, the way the committed files already do. The
// committed date, family and interview stories sit inside it, the
// festival and funeral ones outside, and the derivation reproduces both
// forms byte for byte.
const inlineBudget = 72

// arrayValue renders a string array in the committed files' own two
// forms: inline when the key's line stays inside the budget, one item
// per line otherwise.
func arrayValue(items []string, keyIndent int, keyLabel string) []byte {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = string(jsonString(item))
	}
	inline := "[" + strings.Join(quoted, ", ") + "]"
	if keyIndent+len(`"`+keyLabel+`": `)+len(inline) <= inlineBudget {
		return []byte(inline)
	}
	var b bytes.Buffer
	b.WriteString("[\n")
	for i, q := range quoted {
		b.WriteString(strings.Repeat(" ", keyIndent+2))
		b.WriteString(q)
		if i < len(quoted)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString(strings.Repeat(" ", keyIndent))
	b.WriteString("]")
	return b.Bytes()
}

// attributeValue renders a shown check's attributes the way the
// committed family file holds them: one inline object per line, the
// array closed at the key's own indent.
func attributeValue(items []domain.AnsweredAttribute, keyIndent int) []byte {
	var b bytes.Buffer
	b.WriteString("[\n")
	for i, a := range items {
		b.WriteString(strings.Repeat(" ", keyIndent+2))
		b.WriteString(`{"id": `)
		b.Write(jsonString(a.ID))
		b.WriteString(`, "value": `)
		b.Write(jsonString(a.Value))
		b.WriteString("}")
		if i < len(items)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString(strings.Repeat(" ", keyIndent))
	b.WriteString("]")
	return b.Bytes()
}
