package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/systemone"
)

// checkReading is the test's reading, the same one the judge tests pin
// at: the reading docs/clef.md counts agreement at. It is a test's
// reading, not the setting; T3.9 sets the check's three lines from
// measurement.
var checkReading = Lines{ChangeMade: readingAt, SamePerson: readingAt, CriterionMet: readingAt}

// checkOriginal and checkRender are the two pictures the stub tests run
// with: two distinct byte strings, so a request that swapped or doubled
// them cannot pass by accident.
var (
	checkOriginal = systemone.Image{ContentType: "image/jpeg", Bytes: []byte("original-bytes")}
	checkRender   = systemone.Image{ContentType: "image/jpeg", Bytes: []byte("render-bytes")}
)

// loadVocabulary reads the one look vocabulary from the occasions data.
func loadVocabulary(t *testing.T) *domain.Vocabulary {
	t.Helper()
	loaded, err := domain.Load(filepath.Join(repoRoot(t), "testdata", "occasions"))
	if err != nil {
		t.Fatalf("load testdata/occasions: %v", err)
	}
	vocab := loaded.Vocabulary
	return &vocab
}

// testCheckOccasion is the judge test's two-criterion occasion with the
// fix that serves its first criterion. The wording mirrors the data's
// shape; the values are the test's.
func testCheckOccasion() *domain.Occasion {
	occ := testOccasion()
	occ.Fixes = []domain.Fix{{
		ID:         "fix-a",
		OptIn:      true,
		Changes:    "Fixes a.",
		Criteria:   []string{"a"},
		Vocabulary: []domain.VocabularyChange{{Attribute: "a", To: "x"}},
	}}
	return occ
}

// testVocabulary is a two-attribute vocabulary in the data's shape: every
// attribute asks one question and offers none and can't tell among its
// options.
func testVocabulary() *domain.Vocabulary {
	return &domain.Vocabulary{
		Kind: "vocabulary",
		ID:   "look",
		Attributes: []domain.LookAttribute{
			{ID: "a", Question: "What is a?", Options: []string{"none", "x", "y", "can't tell"}},
			{ID: "b", Question: "What is b?", Options: []string{"none", "p", "q", "can't tell"}},
		},
	}
}

// floatPtr is one yes-probability for an answer.
func floatPtr(v float64) *float64 { return &v }

// checkAnswers builds a check response: one yes-probability per gate and
// one winning option per named attribute.
func checkAnswers(change, same, met float64, look map[string]string) systemone.Response {
	answers := map[string]systemone.Answer{
		changeMadeID:   {Type: systemone.TypeNoul, Noul: floatPtr(change)},
		samePersonID:   {Type: systemone.TypeNoul, Noul: floatPtr(same)},
		criterionMetID: {Type: systemone.TypeNoul, Noul: floatPtr(met)},
	}
	for attr, value := range look {
		answers[lookIDPrefix+attr] = systemone.Answer{Type: systemone.TypeChoice, Choice: value}
	}
	return systemone.Response{Model: systemone.ModelClef, Answers: answers}
}

// checkCall runs one Check over the constructed world with a capture
// around the given client, so a test can assert on the request and the
// outcome at once.
func checkCall(t *testing.T, client systemone.SystemOne, lines Lines) (*captureClient, Outcome, error) {
	t.Helper()
	occ := testCheckOccasion()
	capture := &captureClient{inner: client}
	got, err := Check(context.Background(), capture, systemone.ModelClef, checkOriginal, checkRender,
		occ, &occ.Fixes[0], "a", "Fixes a.", testVocabulary(), lines)
	return capture, got, err
}

// checkCase is one recorded check prepared for a replayed call: the
// exchange id, the model, both images read from the files the recording
// names, the occasion, and the one fix on it that serves the recorded
// criterion.
type checkCase struct {
	exchange  string
	model     string
	original  systemone.Image
	render    systemone.Image
	occ       *domain.Occasion
	fix       *domain.Fix
	requested string
}

// checkSample reads one check recording the way its probe built it: the
// model, the state's occasion, requested change and criterion, and the
// two image files - the original first, the render second.
func checkSample(t *testing.T, clefDir, file string, occasions map[string]*domain.Occasion) checkCase {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	var env struct {
		Exchange string `json:"exchange"`
		Request  struct {
			Body struct {
				Model string `json:"model"`
				State struct {
					Occasion        string `json:"occasion"`
					RequestedChange string `json:"requested_change"`
					Criterion       string `json:"criterion"`
				} `json:"state"`
				Images []struct {
					ContentType string `json:"content_type"`
					File        string `json:"file"`
				} `json:"images"`
			} `json:"body"`
		} `json:"request"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("%s: %v", filepath.Base(file), err)
	}
	body := env.Request.Body
	if len(body.Images) != 2 {
		t.Fatalf("%s: %d images, want the original and the render", filepath.Base(file), len(body.Images))
	}
	read := func(i int) systemone.Image {
		raw, err := os.ReadFile(filepath.Join(clefDir, filepath.FromSlash(body.Images[i].File)))
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(file), err)
		}
		return systemone.Image{ContentType: body.Images[i].ContentType, Bytes: raw}
	}
	occ, ok := occasions[body.State.Occasion]
	if !ok {
		t.Fatalf("%s: state names unknown occasion %q", filepath.Base(file), body.State.Occasion)
	}
	var fixes []domain.Fix
	for _, fix := range occ.Fixes {
		for _, id := range fix.Criteria {
			if id == body.State.Criterion {
				fixes = append(fixes, fix)
			}
		}
	}
	if len(fixes) != 1 {
		t.Fatalf("%s: %d fixes on %q address criterion %q, want one",
			filepath.Base(file), len(fixes), occ.ID, body.State.Criterion)
	}
	return checkCase{
		exchange:  env.Exchange,
		model:     body.Model,
		original:  read(0),
		render:    read(1),
		occ:       occ,
		fix:       &fixes[0],
		requested: body.State.RequestedChange,
	}
}

// runRecordedCheck runs one recorded check through the real Check.
func runRecordedCheck(t *testing.T, clefDir string, replay *systemone.Replay, occasions map[string]*domain.Occasion, vocab *domain.Vocabulary, name string) Outcome {
	t.Helper()
	s := checkSample(t, clefDir, filepath.Join(clefDir, name+".json"), occasions)
	got, err := Check(context.Background(), replay, s.model, s.original, s.render,
		s.occ, s.fix, s.fix.Criteria[0], s.requested, vocab, checkReading)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return got
}

// expectedChecks reads off the recorded answers in docs/clef.md's check
// tables. The woman-white pair is the known bad render T0.4 and T0.6 gave
// the check: the original's "render" is a different person entirely - the
// change missing and the person changed at once - recorded on both models
// because both models must reject it. The V05 pair is the honest render:
// the same look with the jhumkas and the red lip added. Both models read
// the same eleven look answers on the honest pair, so their lines join
// byte-identically: the same look described the same way is the
// vocabulary's whole point.
var expectedChecks = map[string]struct {
	shown             bool
	change, same, met bool
	words             string
	attributes        int
}{
	"check-clef-V04-to-V05": {
		shown: true, change: true, same: true, met: true,
		// The phrase table's line: the jhumkas option's display phrase
		// embeds the colour winner, and the colour does not also stand
		// alone. This is the fixture's "gold jhumkas" (testdata/api/
		// look.json), now derived from the table.
		words:      "gold jhumkas, red, mandarin, tied-back, black, flat, festive-top",
		attributes: 8,
	},
	"check-clef-flash-V04-to-V05": {
		shown: true, change: true, same: true, met: true,
		words:      "gold jhumkas, red, mandarin, tied-back, black, flat, festive-top",
		attributes: 8,
	},
	"check-clef-V04-to-woman-white": {
		shown: false, change: false, same: false, met: false,
		words:      "nude, open, loose, red, flat, plain-shirt",
		attributes: 6,
	},
	"check-clef-flash-V04-to-woman-white": {
		shown: false, change: false, same: false, met: false,
		words:      "nude, buttoned, loose, red, flat, plain-shirt",
		attributes: 6,
	},
}

// checkRecordings is every check recording on the seam, in replay order.
var checkRecordings = []string{
	"check-clef-V04-to-V05",
	"check-clef-flash-V04-to-V05",
	"check-clef-V04-to-woman-white",
	"check-clef-flash-V04-to-woman-white",
}

// TestCheckReplaysEveryRecording runs the check on all four recorded
// calls and asserts each outcome. The request Check builds must equal the
// recorded request every time - which is also the two-image pin: the
// recorded body carries the original first and the render second, so a
// swapped or single-image request cannot equal it.
func TestCheckReplaysEveryRecording(t *testing.T) {
	t.Parallel()
	clefDir := filepath.Join(repoRoot(t), "testdata", "clef")
	replay := openRecordings(t)
	occasions := loadOccasions(t)
	vocab := loadVocabulary(t)

	for _, name := range checkRecordings {
		t.Run(name, func(t *testing.T) {
			if _, ok := expectedChecks[name]; !ok {
				t.Fatalf("recording %q has no expected outcome", name)
			}
			s := checkSample(t, clefDir, filepath.Join(clefDir, name+".json"), occasions)
			want := expectedChecks[name]
			capture := &captureClient{inner: replay}
			got, err := Check(context.Background(), capture, s.model, s.original, s.render,
				s.occ, s.fix, s.fix.Criteria[0], s.requested, vocab, checkReading)
			if err != nil {
				t.Fatalf("%s: %v", s.exchange, err)
			}
			if len(capture.seen) != 1 {
				t.Fatalf("%s: %d requests went out, want one", s.exchange, len(capture.seen))
			}
			recorded, err := replay.Reply(s.exchange)
			if err != nil {
				t.Fatalf("%s: %v", s.exchange, err)
			}
			assertSameRequest(t, capture.seen[0], recordedRequest(t, recorded), s.exchange)

			if got.Shown() != want.shown {
				t.Errorf("%s: shown %v, want %v", s.exchange, got.Shown(), want.shown)
			}
			if got.Check.ChangeMade != want.change || got.Check.SamePerson != want.same || got.Check.CriterionMet != want.met {
				t.Errorf("%s: facts (change %v, same %v, met %v), want (%v, %v, %v)",
					s.exchange, got.Check.ChangeMade, got.Check.SamePerson, got.Check.CriterionMet,
					want.change, want.same, want.met)
			}
			if len(got.Check.Attributes) != want.attributes {
				t.Errorf("%s: %d attributes, want %d", s.exchange, len(got.Check.Attributes), want.attributes)
			}
			if got.Words != want.words {
				t.Errorf("%s: words %q, want %q", s.exchange, got.Words, want.words)
			}
		})
	}

	// The known bad renders: each is discarded, on the model that read it.
	for _, name := range checkRecordings {
		if got := runRecordedCheck(t, clefDir, replay, occasions, vocab, name); got.Shown() != expectedChecks[name].shown {
			t.Errorf("%s: shown %v, want %v", name, got.Shown(), expectedChecks[name].shown)
		}
	}

	// The line is identical for identical answers across recordings: both
	// models read the honest render's look the same way, so their lines
	// come out byte-identical.
	first := runRecordedCheck(t, clefDir, replay, occasions, vocab, "check-clef-V04-to-V05")
	second := runRecordedCheck(t, clefDir, replay, occasions, vocab, "check-clef-flash-V04-to-V05")
	if first.Words != second.Words {
		t.Errorf("the two honest-render lines differ: %q and %q", first.Words, second.Words)
	}
}

// TestCheckRequestCarriesTheTwoImagesAndNothingElse is the boundary pin.
// The check is the one step that sends two pictures, and that is all it
// sends: the original and the render it was given, in that order, and no
// third image. The state is a five-field struct, so a YouCam analysis, a
// probability, a product photo or a user identifier has no field to
// travel in, and this file imports the System One seam and the domain,
// never the YouCam client, so no analysis can even be named here. Every
// question is the recorded wording or the data's own; nothing is
// composed.
func TestCheckRequestCarriesTheTwoImagesAndNothingElse(t *testing.T) {
	t.Parallel()
	occ := testCheckOccasion()
	vocab := testVocabulary()
	capture := &captureClient{inner: &stubClient{resp: checkAnswers(0.9, 0.9, 0.9,
		map[string]string{"a": "x", "b": "none"})}}
	_, err := Check(context.Background(), capture, systemone.ModelClef, checkOriginal, checkRender,
		occ, &occ.Fixes[0], "a", "Fixes a.", vocab, checkReading)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(capture.seen) != 1 {
		t.Fatalf("%d requests went out, want one", len(capture.seen))
	}
	req := capture.seen[0]

	if len(req.Images) != 2 {
		t.Fatalf("%d images in the request, want the original and the render", len(req.Images))
	}
	if req.Images[0].ContentType != checkOriginal.ContentType || !bytes.Equal(req.Images[0].Bytes, checkOriginal.Bytes) {
		t.Fatal("the request's first image is not the original photo")
	}
	if req.Images[1].ContentType != checkRender.ContentType || !bytes.Equal(req.Images[1].Bytes, checkRender.Bytes) {
		t.Fatal("the request's second image is not the render")
	}

	var state checkState
	dec := json.NewDecoder(strings.NewReader(string(req.State)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&state); err != nil {
		t.Fatalf("state does not decode as the check state: %v", err)
	}
	if dec.More() {
		t.Fatal("the state carries more than the check state's five fields")
	}
	if state.Occasion != occ.ID || state.Name != occ.Name {
		t.Errorf("state occasion and name %+v, want the occasion's id and name", state)
	}
	if state.Images != imagesLine {
		t.Errorf("state images line %q, want the recorded sentence", state.Images)
	}
	if state.RequestedChange != "Fixes a." || state.Criterion != "a" {
		t.Errorf("state change and criterion %+v, want the requested change and the criterion", state)
	}

	if len(req.Questions) != 3+len(vocab.Attributes) {
		t.Fatalf("%d questions, want the three gates plus %d look attributes",
			len(req.Questions), len(vocab.Attributes))
	}
	gates := map[string]string{
		changeMadeID:   changeMadeQuestion,
		samePersonID:   samePersonQuestion,
		criterionMetID: occasionCriterion(t, occ, "a").Clef,
	}
	for id, want := range gates {
		q, ok := req.Questions[id]
		if !ok {
			t.Fatalf("no %s question", id)
		}
		if q.Type != systemone.TypeNoul {
			t.Errorf("%s is %q, want noul", id, q.Type)
		}
		if q.Instructions != want {
			t.Errorf("%s is worded %q, want the recorded wording", id, q.Instructions)
		}
		if len(q.Criteria) != 0 {
			t.Errorf("%s carries criteria", id)
		}
	}
	for _, attr := range vocab.Attributes {
		q, ok := req.Questions[lookIDPrefix+attr.ID]
		if !ok {
			t.Fatalf("no question for attribute %q", attr.ID)
		}
		if q.Type != systemone.TypeChoice {
			t.Errorf("%s%s is %q, want choice", lookIDPrefix, attr.ID, q.Type)
		}
		if q.Instructions != attr.Question {
			t.Errorf("%s%s is not worded as the vocabulary words it", lookIDPrefix, attr.ID)
		}
		var options map[string]string
		if err := json.Unmarshal(q.Criteria, &options); err != nil {
			t.Fatalf("%s%s options: %v", lookIDPrefix, attr.ID, err)
		}
		if len(options) != len(attr.Options) {
			t.Fatalf("%s%s has %d options, want %d", lookIDPrefix, attr.ID, len(options), len(attr.Options))
		}
		for _, o := range attr.Options {
			if options[o] != o {
				t.Errorf("%s%s option %q is %q, want the option worded as itself", lookIDPrefix, attr.ID, o, options[o])
			}
		}
	}
}

// occasionCriterion is the test's lookup for one criterion's wording.
func occasionCriterion(t *testing.T, occ *domain.Occasion, id string) domain.Criterion {
	t.Helper()
	for _, c := range occ.Criteria {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("occasion %q has no criterion %q", occ.ID, id)
	return domain.Criterion{}
}

// TestCheckDiscardsWhateverElseItScored pins the discard rule by
// construction: only a failing change-made or a failing same-person
// discards, and no other fact - a met criterion at probability one, any
// attribute answer - can keep a render whose gate failed or lose one
// whose gates passed.
func TestCheckDiscardsWhateverElseItScored(t *testing.T) {
	t.Parallel()

	t.Run("a change the render skipped discards a perfect score", func(t *testing.T) {
		client := &stubClient{resp: checkAnswers(0.0001, 1.0, 1.0, map[string]string{"a": "x", "b": "none"})}
		_, got, err := checkCall(t, client, checkReading)
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		if got.Shown() {
			t.Fatal("a render that skipped the change was shown")
		}
		if got.Check.ChangeMade {
			t.Error("change-made reads true under its line")
		}
	})

	t.Run("a changed person discards a perfect score", func(t *testing.T) {
		client := &stubClient{resp: checkAnswers(1.0, 0.0001, 1.0, map[string]string{"a": "x", "b": "none"})}
		_, got, err := checkCall(t, client, checkReading)
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		if got.Shown() {
			t.Fatal("a render of a different person was shown")
		}
		if got.Check.SamePerson {
			t.Error("same-person reads true under its line")
		}
	})

	t.Run("a criterion the render honestly failed does not discard it", func(t *testing.T) {
		client := &stubClient{resp: checkAnswers(1.0, 1.0, 0.0001, map[string]string{"a": "none", "b": "none"})}
		_, got, err := checkCall(t, client, checkReading)
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		if !got.Shown() {
			t.Fatal("an honest render was discarded for its failed criterion")
		}
		if got.Check.CriterionMet {
			t.Error("criterion-met reads true under its line")
		}
	})

	t.Run("at the line is met", func(t *testing.T) {
		client := &stubClient{resp: checkAnswers(readingAt, readingAt, readingAt, map[string]string{"a": "x", "b": "p"})}
		_, got, err := checkCall(t, client, checkReading)
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		if !got.Shown() {
			t.Error("probabilities at the lines read as failed")
		}
	})

	t.Run("each line moves only its own gate", func(t *testing.T) {
		client := &stubClient{resp: checkAnswers(0.7, 0.7, 0.7, map[string]string{"a": "x", "b": "p"})}
		_, got, err := checkCall(t, client, Lines{ChangeMade: 0.9, SamePerson: 0.5, CriterionMet: 0.5})
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		if got.Shown() {
			t.Error("change-made at 0.7 passed a 0.9 line")
		}
		_, got, err = checkCall(t, client, Lines{ChangeMade: 0.5, SamePerson: 0.5, CriterionMet: 0.9})
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		if !got.Shown() {
			t.Error("a failing criterion-met line discarded an honest render")
		}
		if got.Check.CriterionMet {
			t.Error("criterion-met at 0.7 passed a 0.9 line")
		}
	})
}

// TestTheLookLineIsIdenticalForIdenticalAnswers pins the look-in-words
// line: the same answers always join into the byte-identical line, in the
// vocabulary's own order, with none and can't tell left out - and
// different answers say something else.
func TestTheLookLineIsIdenticalForIdenticalAnswers(t *testing.T) {
	t.Parallel()
	vocab := loadVocabulary(t)
	occ := testCheckOccasion()

	// The winners are named in scrambled order on purpose: the line must
	// follow the vocabulary's order, not the answer map's iteration
	// order, or this pin would flake. Jewellery is the vocabulary's
	// first attribute, lip colour its fifth, hair its seventh.
	look := map[string]string{
		"hair":             "tied-back",
		"lip_colour":       "red",
		"jewellery":        "jhumkas",
		"beard":            "none",
		"garment":          "festive-top",
		"collar":           "can't tell",
		"hair_colour":      "black",
		"necklace":         "none",
		"expression":       "flat",
		"jewellery_colour": "gold",
		"necklace_colour":  "none",
	}
	run := func() Outcome {
		capture := &captureClient{inner: &stubClient{resp: checkAnswers(0.9, 0.9, 0.9, look)}}
		got, err := Check(context.Background(), capture, systemone.ModelClef, checkOriginal, checkRender,
			occ, &occ.Fixes[0], "a", "Fixes a.", vocab, checkReading)
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		if len(capture.seen) != 1 {
			t.Fatalf("%d requests went out, want one", len(capture.seen))
		}
		return got
	}
	first, second := run(), run()
	if first.Words != second.Words {
		t.Fatalf("identical answers joined into two lines: %q and %q", first.Words, second.Words)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("identical answers gave two outcomes: %+v and %+v", first, second)
	}
	want := "gold jhumkas, red, tied-back, black, flat, festive-top"
	if first.Words != want {
		t.Errorf("words %q, want %q - the vocabulary's order with none and can't tell left out, the colour spoken inside the jhumkas phrase", first.Words, want)
	}

	// Different answers, different line.
	look["lip_colour"] = "nude"
	if changed := run(); changed.Words == first.Words {
		t.Error("different answers gave the same line")
	}

	// None and can't tell are left out, and a look they empty is an
	// empty line.
	empty := map[string]string{}
	for _, attr := range vocab.Attributes {
		empty[attr.ID] = "none"
	}
	empty["collar"] = "can't tell"
	capture := &captureClient{inner: &stubClient{resp: checkAnswers(0.9, 0.9, 0.9, empty)}}
	got, err := Check(context.Background(), capture, systemone.ModelClef, checkOriginal, checkRender,
		occ, &occ.Fixes[0], "a", "Fixes a.", vocab, checkReading)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if got.Words != "" {
		t.Errorf("words %q, want the empty line", got.Words)
	}
	if len(got.Check.Attributes) != 0 {
		t.Errorf("%d attributes survived, want none", len(got.Check.Attributes))
	}
}

// TestCheckLinesRefused: every line must sit in [0, 1]; NaN refuses
// because every comparison on it refuses.
func TestCheckLinesRefused(t *testing.T) {
	t.Parallel()
	bad := []struct {
		name  string
		lines Lines
	}{
		{"change made under zero", Lines{ChangeMade: -0.1, SamePerson: readingAt, CriterionMet: readingAt}},
		{"change made over one", Lines{ChangeMade: 1.1, SamePerson: readingAt, CriterionMet: readingAt}},
		{"change made not a number", Lines{ChangeMade: math.NaN(), SamePerson: readingAt, CriterionMet: readingAt}},
		{"same person under zero", Lines{ChangeMade: readingAt, SamePerson: -1, CriterionMet: readingAt}},
		{"same person not a number", Lines{ChangeMade: readingAt, SamePerson: math.NaN(), CriterionMet: readingAt}},
		{"criterion met over one", Lines{ChangeMade: readingAt, SamePerson: readingAt, CriterionMet: 2}},
		{"criterion met not a number", Lines{ChangeMade: readingAt, SamePerson: readingAt, CriterionMet: math.NaN()}},
	}
	for _, tc := range bad {
		t.Run(tc.name+" is refused", func(t *testing.T) {
			client := &stubClient{resp: checkAnswers(0.9, 0.9, 0.9, nil)}
			capture, _, err := checkCall(t, client, tc.lines)
			if err == nil {
				t.Fatal("check accepted an out-of-range line")
			}
			if n := len(capture.seen); n != 0 {
				t.Fatalf("%d requests went out, want the refusal before any call", n)
			}
		})
	}
}

// TestCheckRefusals pins every refusal that happens before any call, and
// the reads that refuse a broken answer. Clef answers a vacuous or
// imageless check confidently (docs/clef.md), so an unusable check must
// never reach it.
func TestCheckRefusals(t *testing.T) {
	t.Parallel()
	occ := testCheckOccasion()
	fix := &occ.Fixes[0]
	vocab := testVocabulary()
	good := checkAnswers(0.9, 0.9, 0.9, map[string]string{"a": "x", "b": "none"})

	t.Run("no original is refused before any call", func(t *testing.T) {
		capture := &captureClient{inner: &stubClient{resp: good}}
		_, err := Check(context.Background(), capture, systemone.ModelClef,
			systemone.Image{ContentType: "image/jpeg"}, checkRender, occ, fix, "a", "Fixes a.", vocab, checkReading)
		if n := len(capture.seen); n != 0 {
			t.Fatalf("%d requests went out, want the refusal before any call", n)
		}
		if err == nil || !strings.Contains(err.Error(), "original") {
			t.Fatalf("error %v, want it to name the original", err)
		}
	})

	t.Run("no render is refused before any call", func(t *testing.T) {
		capture := &captureClient{inner: &stubClient{resp: good}}
		_, err := Check(context.Background(), capture, systemone.ModelClef,
			checkOriginal, systemone.Image{ContentType: "image/jpeg"}, occ, fix, "a", "Fixes a.", vocab, checkReading)
		if n := len(capture.seen); n != 0 {
			t.Fatalf("%d requests went out, want the refusal before any call", n)
		}
		if err == nil || !strings.Contains(err.Error(), "render") {
			t.Fatalf("error %v, want it to name the render", err)
		}
	})

	t.Run("an empty requested change is refused before any call", func(t *testing.T) {
		capture := &captureClient{inner: &stubClient{resp: good}}
		_, err := Check(context.Background(), capture, systemone.ModelClef,
			checkOriginal, checkRender, occ, fix, "a", "   ", vocab, checkReading)
		if n := len(capture.seen); n != 0 {
			t.Fatalf("%d requests went out, want the refusal before any call", n)
		}
		if err == nil || !strings.Contains(err.Error(), "requested change") {
			t.Fatalf("error %v, want it to name the requested change", err)
		}
	})

	t.Run("a criterion off the occasion is refused before any call", func(t *testing.T) {
		capture := &captureClient{inner: &stubClient{resp: good}}
		_, err := Check(context.Background(), capture, systemone.ModelClef,
			checkOriginal, checkRender, occ, fix, "no-such-criterion", "Fixes a.", vocab, checkReading)
		if n := len(capture.seen); n != 0 {
			t.Fatalf("%d requests went out, want the refusal before any call", n)
		}
		if err == nil || !strings.Contains(err.Error(), "no-such-criterion") {
			t.Fatalf("error %v, want it to name the criterion", err)
		}
	})

	t.Run("a criterion the fix does not address is refused before any call", func(t *testing.T) {
		capture := &captureClient{inner: &stubClient{resp: good}}
		_, err := Check(context.Background(), capture, systemone.ModelClef,
			checkOriginal, checkRender, occ, fix, "b", "Fixes a.", vocab, checkReading)
		if n := len(capture.seen); n != 0 {
			t.Fatalf("%d requests went out, want the refusal before any call", n)
		}
		if err == nil || !strings.Contains(err.Error(), "fix-a") || !strings.Contains(err.Error(), "b") {
			t.Fatalf("error %v, want it to name the fix and the criterion", err)
		}
	})

	t.Run("no occasion, fix, vocabulary, or model is refused before any call", func(t *testing.T) {
		capture := &captureClient{inner: &stubClient{resp: good}}
		cases := []struct {
			name string
			want string
			run  func() error
		}{
			{"no occasion", "no occasion", func() error {
				_, err := Check(context.Background(), capture, systemone.ModelClef, checkOriginal, checkRender, nil, fix, "a", "Fixes a.", vocab, checkReading)
				return err
			}},
			{"no fix", "no fix", func() error {
				_, err := Check(context.Background(), capture, systemone.ModelClef, checkOriginal, checkRender, occ, nil, "a", "Fixes a.", vocab, checkReading)
				return err
			}},
			{"no vocabulary", "no vocabulary", func() error {
				_, err := Check(context.Background(), capture, systemone.ModelClef, checkOriginal, checkRender, occ, fix, "a", "Fixes a.", nil, checkReading)
				return err
			}},
			{"no model", "no model", func() error {
				_, err := Check(context.Background(), capture, "", checkOriginal, checkRender, occ, fix, "a", "Fixes a.", vocab, checkReading)
				return err
			}},
		}
		for _, tc := range cases {
			if err := tc.run(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s: error %v, want it to name %q", tc.name, err, tc.want)
			}
		}
		if n := len(capture.seen); n != 0 {
			t.Fatalf("%d requests went out, want the refusals before any call", n)
		}
	})

	t.Run("an empty vocabulary is refused before any call", func(t *testing.T) {
		capture := &captureClient{inner: &stubClient{resp: good}}
		_, err := Check(context.Background(), capture, systemone.ModelClef,
			checkOriginal, checkRender, occ, fix, "a", "Fixes a.", &domain.Vocabulary{}, checkReading)
		if n := len(capture.seen); n != 0 {
			t.Fatalf("%d requests went out, want the refusal before any call", n)
		}
		if err == nil || !strings.Contains(err.Error(), "no attributes") {
			t.Fatalf("error %v, want it to name the empty vocabulary", err)
		}
	})

	t.Run("a missing gate answer is refused", func(t *testing.T) {
		client := &stubClient{resp: checkAnswers(0.9, 0.9, 0.9, nil)}
		delete(client.resp.Answers, samePersonID)
		_, _, err := checkCall(t, client, checkReading)
		if err == nil || !strings.Contains(err.Error(), samePersonID) {
			t.Fatalf("error %v, want it to name %s", err, samePersonID)
		}
	})

	t.Run("a gate answer that is not a yes probability is refused", func(t *testing.T) {
		client := &stubClient{resp: checkAnswers(0.9, 0.9, 0.9, nil)}
		client.resp.Answers[changeMadeID] = systemone.Answer{Type: systemone.TypeChoice, Choice: "yes"}
		_, _, err := checkCall(t, client, checkReading)
		if err == nil || !strings.Contains(err.Error(), changeMadeID) {
			t.Fatalf("error %v, want it to name %s", err, changeMadeID)
		}
	})

	t.Run("a missing attribute answer is refused", func(t *testing.T) {
		client := &stubClient{resp: checkAnswers(0.9, 0.9, 0.9, map[string]string{"a": "x"})}
		delete(client.resp.Answers, lookIDPrefix+"b")
		_, _, err := checkCall(t, client, checkReading)
		if err == nil || !strings.Contains(err.Error(), lookIDPrefix+"b") {
			t.Fatalf("error %v, want it to name %sb", err, lookIDPrefix)
		}
	})

	t.Run("an attribute answer that is not a choice is refused", func(t *testing.T) {
		client := &stubClient{resp: checkAnswers(0.9, 0.9, 0.9, nil)}
		client.resp.Answers[lookIDPrefix+"a"] = systemone.Answer{Type: systemone.TypeNoul, Noul: floatPtr(0.5)}
		_, _, err := checkCall(t, client, checkReading)
		if err == nil || !strings.Contains(err.Error(), lookIDPrefix+"a") {
			t.Fatalf("error %v, want it to name %sa", err, lookIDPrefix)
		}
	})

	t.Run("an option off the attribute is refused", func(t *testing.T) {
		client := &stubClient{resp: checkAnswers(0.9, 0.9, 0.9, map[string]string{"a": "the moon", "b": "none"})}
		_, _, err := checkCall(t, client, checkReading)
		if err == nil || !strings.Contains(err.Error(), "the moon") {
			t.Fatalf("error %v, want it to name the option", err)
		}
	})

	t.Run("a seam error carries the stage's name", func(t *testing.T) {
		client := &failingClient{err: errors.New("no recording")}
		_, _, err := checkCall(t, client, checkReading)
		if err == nil || !strings.HasPrefix(err.Error(), "check: ") {
			t.Fatalf("error %v, want it to name the check stage", err)
		}
	})
}
