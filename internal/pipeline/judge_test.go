package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/systemone"
)

// readingAt is the reading the replay table pins the flags at. It is the
// reading docs/clef.md counts agreement at, so its tables of the recorded
// probabilities are the source of every expected outcome below. It is a
// test's reading, not the setting: the settings file owns the thresholds,
// and T3.9 sets them from measurement.
const readingAt = 0.5

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func openRecordings(t *testing.T) *systemone.Replay {
	t.Helper()
	replay, err := systemone.Open(filepath.Join(repoRoot(t), "testdata", "clef"))
	if err != nil {
		t.Fatalf("open testdata/clef: %v", err)
	}
	return replay
}

func loadOccasions(t *testing.T) map[string]*domain.Occasion {
	t.Helper()
	loaded, err := domain.Load(filepath.Join(repoRoot(t), "testdata", "occasions"))
	if err != nil {
		t.Fatalf("load testdata/occasions: %v", err)
	}
	occasions := make(map[string]*domain.Occasion, len(loaded.Occasions))
	for i := range loaded.Occasions {
		occ := loaded.Occasions[i]
		occasions[occ.ID] = &occ
	}
	return occasions
}

// captureClient records every request that passes through it, so a test
// can pin what the judge sent the model.
type captureClient struct {
	inner systemone.SystemOne

	mu   sync.Mutex
	seen []systemone.Request
}

func (c *captureClient) Ask(ctx context.Context, req systemone.Request) (systemone.Response, error) {
	c.mu.Lock()
	c.seen = append(c.seen, req)
	c.mu.Unlock()
	return c.inner.Ask(ctx, req)
}

// sampleCase is one judge recording prepared for a replayed look.
type sampleCase struct {
	exchange string
	variant  string
	occ      *domain.Occasion
	photo    systemone.Image
}

// sample reads one judge recording: its exchange id, the occasion its
// state names, and the photo its one image references by file.
func sample(t *testing.T, clefDir, file string, occasions map[string]*domain.Occasion) sampleCase {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	var env struct {
		Exchange string          `json:"exchange"`
		Request  json.RawMessage `json:"request"`
		Response json.RawMessage `json:"response"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("%s: %v", filepath.Base(file), err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("%s: trailing JSON", filepath.Base(file))
	}
	var wire struct {
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(env.Request, &wire); err != nil {
		t.Fatalf("%s: %v", filepath.Base(file), err)
	}
	var body struct {
		Model string `json:"model"`
		State struct {
			Occasion string `json:"occasion"`
			Name     string `json:"name"`
		} `json:"state"`
		Images []struct {
			ContentType string `json:"content_type"`
			File        string `json:"file"`
		} `json:"images"`
	}
	if err := json.Unmarshal(wire.Body, &body); err != nil {
		t.Fatalf("%s: %v", filepath.Base(file), err)
	}
	if body.Model != systemone.ModelClef {
		t.Fatalf("%s: model %q, want clef", filepath.Base(file), body.Model)
	}
	if len(body.Images) != 1 {
		t.Fatalf("%s: %d images, want the one photo", filepath.Base(file), len(body.Images))
	}
	photo, err := os.ReadFile(filepath.Join(clefDir, filepath.FromSlash(body.Images[0].File)))
	if err != nil {
		t.Fatalf("%s: %v", filepath.Base(file), err)
	}
	occ, ok := occasions[body.State.Occasion]
	if !ok {
		t.Fatalf("%s: state names unknown occasion %q", filepath.Base(file), body.State.Occasion)
	}
	parts := strings.Split(env.Exchange, "-")
	if len(parts) < 3 {
		t.Fatalf("%s: exchange %q does not name its variant", filepath.Base(file), env.Exchange)
	}
	return sampleCase{
		exchange: env.Exchange,
		variant:  parts[1],
		occ:      occ,
		photo:    systemone.Image{ContentType: body.Images[0].ContentType, Bytes: photo},
	}
}

// outcome is one variant's expected judgement at readingAt: the flagged
// criterion ids in the occasion's criteria order, and the biggest issue.
type outcome struct {
	flags []string
	issue string
}

// expected is every sample's outcome at readingAt, read off the recorded
// probabilities in docs/clef.md's judge tables. On a few cells the read
// is not the README's ground truth: at 0.5, V02's light, V21's and P09's
// beards, V06's and P12's expressions, V08's light and beard, P11's
// garment, V09's and V12's collars, P13's beard, V16's light and P16's
// beard sit on the other side of the line. Those cells are the read the
// judge turns out; T3.9 owns moving the lines, not this table.
var expected = map[string]outcome{
	// date
	"V01": {issue: "none"},
	"V02": {flags: []string{"date-collar-open", "date-garment-easy", "date-expression-engaged"}, issue: "date-expression-engaged"},
	"V03": {flags: []string{"date-hair-clear", "date-lip-clear"}, issue: "date-lip-clear"},
	"V21": {issue: "date-collar-open"},
	"P01": {issue: "none"},
	"P02": {flags: []string{"date-collar-open", "date-garment-easy", "date-hair-clear", "date-lip-clear"}, issue: "date-lip-clear"},
	"P09": {flags: []string{"date-beard-even"}, issue: "date-beard-even"},
	"P10": {flags: []string{"date-beard-even", "date-expression-engaged", "date-light-see"}, issue: "date-beard-even"},
	// family function
	"V04": {flags: []string{"family-earring-festive", "family-lip-colour"}, issue: "family-earring-festive"},
	"V05": {issue: "none"},
	"V06": {flags: []string{"family-hair-finished"}, issue: "family-hair-finished"},
	"V07": {flags: []string{"family-earring-festive", "family-lip-colour"}, issue: "none"},
	"V08": {flags: []string{"family-earring-festive", "family-lip-colour", "family-garment-festive", "family-expression-present"}, issue: "family-garment-festive"},
	"P03": {issue: "none"},
	"P04": {flags: []string{"family-earring-festive", "family-lip-colour", "family-garment-festive", "family-hair-finished", "family-expression-present", "family-light-even"}, issue: "family-garment-festive"},
	"P11": {flags: []string{"family-earring-festive", "family-lip-colour", "family-garment-festive"}, issue: "family-garment-festive"},
	"P12": {flags: []string{"family-earring-festive", "family-lip-colour", "family-garment-festive", "family-beard-even"}, issue: "family-garment-festive"},
	// interview
	"V09": {flags: []string{"interview-collar-buttoned"}, issue: "none"},
	"V10": {flags: []string{"interview-collar-buttoned", "interview-garment-plain", "interview-beard-short", "interview-expression-calm", "interview-light-plain"}, issue: "interview-garment-plain"},
	"V11": {flags: []string{"interview-hair-close", "lip-quiet"}, issue: "lip-quiet"},
	"V12": {flags: []string{"interview-collar-buttoned"}, issue: "none"},
	"P05": {flags: []string{"interview-hair-close"}, issue: "interview-hair-close"},
	"P13": {flags: []string{"interview-collar-buttoned", "interview-garment-plain", "interview-expression-calm", "interview-light-plain"}, issue: "interview-garment-plain"},
	// festival
	"V13": {issue: "none"},
	"V14": {flags: []string{"festival-garment-festive", "festival-earring", "festival-necklace", "festival-lip", "festival-hair", "festival-expression", "festival-light"}, issue: "festival-hair"},
	"V15": {flags: []string{"festival-earring", "festival-necklace", "festival-lip"}, issue: "none"},
	"V16": {flags: []string{"festival-garment-festive", "festival-earring", "festival-necklace", "festival-lip", "festival-beard"}, issue: "festival-garment-festive"},
	"P06": {flags: []string{"festival-hair", "festival-expression"}, issue: "festival-hair"},
	"P14": {flags: []string{"festival-earring", "festival-necklace", "festival-lip", "festival-beard"}, issue: "festival-beard"},
	// funeral
	"V17": {issue: "none"},
	"V18": {flags: []string{"funeral-garment-dark", "funeral-collar-plain", "funeral-beard-close", "funeral-expression-still", "funeral-light-plain"}, issue: "funeral-garment-dark"},
	"V19": {issue: "none"},
	"V20": {flags: []string{"funeral-garment-dark", "funeral-collar-plain", "funeral-jewellery-quiet", "lip-quiet", "funeral-hair-plain", "funeral-expression-still", "funeral-light-plain"}, issue: "funeral-garment-dark"},
	"P07": {issue: "none"},
	"P08": {flags: []string{"funeral-collar-plain", "funeral-jewellery-quiet", "lip-quiet", "funeral-hair-plain"}, issue: "funeral-jewellery-quiet"},
	"P15": {issue: "funeral-beard-close"},
	"P16": {flags: []string{"funeral-expression-still", "funeral-light-plain"}, issue: "funeral-expression-still"},
}

// TestJudgeReplaysEverySample runs the judge on all 37 recorded judge
// calls and asserts each sample's flags and biggest issue against the
// recorded outcome. The request judge builds must equal the recorded
// probe request every time; the probe carried the photo, the occasion,
// and the criteria, and no YouCam analysis (docs/clef.md).
func TestJudgeReplaysEverySample(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	clefDir := filepath.Join(root, "testdata", "clef")
	replay := openRecordings(t)
	occasions := loadOccasions(t)

	files, err := filepath.Glob(filepath.Join(clefDir, "judge-*.json"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) != len(expected) {
		t.Fatalf("%d judge recordings, want %d", len(files), len(expected))
	}
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		s := sample(t, clefDir, file, occasions)
		if seen[s.variant] {
			t.Fatalf("two recordings name variant %q", s.variant)
		}
		seen[s.variant] = true
		want, ok := expected[s.variant]
		if !ok {
			t.Fatalf("recording %s names variant %q, which the expected table has no row for", file, s.variant)
		}
		capture := &captureClient{inner: replay}
		got, err := Judge(context.Background(), capture, s.photo, s.occ, Thresholds{Met: readingAt})
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

		gotIDs := make([]string, len(got.Flags))
		for i, flag := range got.Flags {
			gotIDs[i] = flag.ID
			if flag.Suggestion != criterion(t, s.occ, flag.ID).Suggestion {
				t.Errorf("%s: flag %q does not carry the occasion's suggestion", s.exchange, flag.ID)
			}
		}
		if !slices.Equal(gotIDs, want.flags) {
			t.Errorf("%s: flags %v, want %v", s.exchange, gotIDs, want.flags)
		}
		if got.Issue.ID != want.issue {
			t.Errorf("%s: biggest issue %q, want %q", s.exchange, got.Issue.ID, want.issue)
		}
		if got.Issue.Label != issueLabel(t, s.occ, want.issue) {
			t.Errorf("%s: biggest issue label %q is not the occasion's wording", s.exchange, got.Issue.Label)
		}
	}
}

// TestJudgeRequestCarriesNoYouCamAnalysis is the boundary pin. The judge
// is given the photo and the occasion, and nothing else can get in: the
// state is a two-field struct, every question is the data's own wording,
// the photo is the only image, and the whole request equals the recorded
// probe request, which sent no YouCam analysis (docs/clef.md). This
// package imports the System One seam and the domain, and not the YouCam
// client, so no analysis can even be named here.
func TestJudgeRequestCarriesNoYouCamAnalysis(t *testing.T) {
	t.Parallel()
	clefDir := filepath.Join(repoRoot(t), "testdata", "clef")
	replay := openRecordings(t)
	occasions := loadOccasions(t)
	occ := occasions["family-function"]

	s := sample(t, clefDir, filepath.Join(clefDir, "judge-V04-family-function.json"), occasions)
	capture := &captureClient{inner: replay}
	if _, err := Judge(context.Background(), capture, s.photo, occ, Thresholds{Met: readingAt}); err != nil {
		t.Fatalf("judge: %v", err)
	}
	req := capture.seen[0]

	if len(req.Images) != 1 {
		t.Fatalf("%d images in the request, want the one photo", len(req.Images))
	}
	if req.Images[0].ContentType != s.photo.ContentType || !bytes.Equal(req.Images[0].Bytes, s.photo.Bytes) {
		t.Fatal("the request's only image is not the photo the judge was given")
	}

	// The state decodes as the occasion's id and name and nothing else;
	// a state key beyond the two fields fails this decode.
	var state judgeState
	dec := json.NewDecoder(bytes.NewReader(req.State))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&state); err != nil {
		t.Fatalf("state does not decode as occasion and name: %v", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		t.Fatal("the state carries more than the occasion's id and name")
	}
	if state.Occasion != occ.ID || state.Name != occ.Name {
		t.Fatalf("state %+v, want the occasion's id and name", state)
	}

	// One noul per criterion in the data's wording, one choice with the
	// occasion's options, and no other question.
	if len(req.Questions) != len(occ.Criteria)+1 {
		t.Fatalf("%d questions, want %d criteria plus the biggest issue", len(req.Questions), len(occ.Criteria))
	}
	for _, c := range occ.Criteria {
		q, ok := req.Questions[c.ID]
		if !ok {
			t.Fatalf("no question for criterion %q", c.ID)
		}
		if q.Type != systemone.TypeNoul {
			t.Fatalf("question %q is %q, want noul", c.ID, q.Type)
		}
		if q.Instructions != c.Clef {
			t.Fatalf("question %q is not worded as the data words it", c.ID)
		}
		if len(q.Criteria) != 0 {
			t.Fatalf("question %q carries criteria", c.ID)
		}
	}
	choice, ok := req.Questions[biggestIssueID]
	if !ok {
		t.Fatal("no biggest-issue question")
	}
	if choice.Type != systemone.TypeChoice {
		t.Fatalf("biggest issue is %q, want choice", choice.Type)
	}
	if choice.Instructions != occ.BiggestIssue.Question {
		t.Fatal("the biggest issue is not worded as the data words it")
	}
	var options map[string]string
	if err := json.Unmarshal(choice.Criteria, &options); err != nil {
		t.Fatalf("biggest-issue options: %v", err)
	}
	if len(options) != len(occ.BiggestIssue.Options) {
		t.Fatalf("%d biggest-issue options, want %d", len(options), len(occ.BiggestIssue.Options))
	}
	for _, o := range occ.BiggestIssue.Options {
		if options[o.ID] != o.Label {
			t.Fatalf("biggest-issue option %q is %q, want %q", o.ID, options[o.ID], o.Label)
		}
	}

	// The whole request equals the recorded probe request, which carried
	// no YouCam analysis.
	recorded, err := replay.Reply(s.exchange)
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	assertSameRequest(t, req, recordedRequest(t, recorded), s.exchange)
}

func criterion(t *testing.T, occ *domain.Occasion, id string) domain.Criterion {
	t.Helper()
	for _, c := range occ.Criteria {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("occasion %q has no criterion %q", occ.ID, id)
	return domain.Criterion{}
}

func issueLabel(t *testing.T, occ *domain.Occasion, id string) string {
	t.Helper()
	for _, o := range occ.BiggestIssue.Options {
		if o.ID == id {
			return o.Label
		}
	}
	t.Fatalf("occasion %q has no biggest-issue option %q", occ.ID, id)
	return ""
}

func recordedRequest(t *testing.T, rec systemone.Recording) json.RawMessage {
	t.Helper()
	var wire struct {
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(rec.Request, &wire); err != nil {
		t.Fatalf("recorded request: %v", err)
	}
	return wire.Body
}

// assertSameRequest checks the request the judge built against a recorded
// request body, one part at a time so a mismatch names itself instead of
// printing a megabyte of base64.
func assertSameRequest(t *testing.T, got systemone.Request, want json.RawMessage, name string) {
	t.Helper()
	var body struct {
		Model     string                        `json:"model"`
		State     json.RawMessage               `json:"state"`
		Questions map[string]systemone.Question `json:"questions"`
		Images    []systemone.Image             `json:"images"`
	}
	if err := json.Unmarshal(want, &body); err != nil {
		t.Fatalf("%s: recorded body: %v", name, err)
	}
	if got.Model != body.Model {
		t.Errorf("%s: model %q, want %q", name, got.Model, body.Model)
	}
	if !bytes.Equal(canonical(t, got.State), canonical(t, body.State)) {
		t.Errorf("%s: the state is not the recorded state", name)
	}
	if !bytes.Equal(canonical(t, got.Questions), canonical(t, body.Questions)) {
		t.Errorf("%s: the questions are not the recorded questions", name)
	}
	if len(got.Images) != len(body.Images) {
		t.Fatalf("%s: %d images, want %d", name, len(got.Images), len(body.Images))
	}
	for i := range body.Images {
		if got.Images[i].ContentType != body.Images[i].ContentType {
			t.Errorf("%s: image %d content type %q, want %q", name, i, got.Images[i].ContentType, body.Images[i].ContentType)
		}
		if !bytes.Equal(got.Images[i].Bytes, body.Images[i].Bytes) {
			t.Errorf("%s: image %d is not the recorded photo", name, i)
		}
	}
}

// canonical marshals v and normalises the JSON, so two encodings of one
// request compare equal.
func canonical(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		t.Fatalf("decode: %v", err)
	}
	out, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return out
}

// stubClient answers every call with one canned response. It stands in
// for the seam in the threshold and refusal tests, which need answers,
// not recordings.
type stubClient struct {
	resp systemone.Response
}

func (s *stubClient) Ask(ctx context.Context, req systemone.Request) (systemone.Response, error) {
	return s.resp, nil
}

// noulAnswers builds a judge response with one yes-probability per
// criterion id and the named biggest issue.
func noulAnswers(probabilities map[string]float64, issue string) systemone.Response {
	answers := make(map[string]systemone.Answer, len(probabilities)+1)
	for id, p := range probabilities {
		p := p
		answers[id] = systemone.Answer{Type: systemone.TypeNoul, Noul: &p}
	}
	answers[biggestIssueID] = systemone.Answer{Type: systemone.TypeChoice, Choice: issue}
	return systemone.Response{Model: systemone.ModelClef, Answers: answers}
}

// testOccasion is a two-criterion occasion for the threshold and refusal
// tests. Its wording mirrors the data's shape; the values are the test's.
func testOccasion() *domain.Occasion {
	return &domain.Occasion{
		Kind: "occasion",
		ID:   "test",
		Name: "Test",
		Criteria: []domain.Criterion{
			{ID: "a", Kind: domain.KindClothing, Clef: "Is a as the dress code asks?", Suggestion: "A is not as it asks; try fixing a."},
			{ID: "b", Kind: domain.KindGrooming, Clef: "Is b as the dress code asks?", Suggestion: "B is not as it asks; try fixing b."},
		},
		BiggestIssue: domain.BiggestIssue{
			Question: "Which missed part of this dress code is the biggest issue?",
			Options: []domain.IssueOption{
				{ID: "a", Label: "the a"},
				{ID: "b", Label: "the b"},
				{ID: "none", Label: "none"},
				{ID: "cant-tell", Label: "can't tell"},
			},
		},
	}
}

func TestJudgeThresholds(t *testing.T) {
	t.Parallel()
	occ := testOccasion()
	photo := systemone.Image{ContentType: "image/jpeg", Bytes: []byte{0xff, 0xd8, 0xff, 0xe0}}

	t.Run("at the line is met, under it is flagged", func(t *testing.T) {
		client := &stubClient{resp: noulAnswers(map[string]float64{"a": readingAt, "b": 0.4999}, "b")}
		got, err := Judge(context.Background(), client, photo, occ, Thresholds{Met: readingAt})
		if err != nil {
			t.Fatalf("judge: %v", err)
		}
		if ids := flagIDs(got); !slices.Equal(ids, []string{"b"}) {
			t.Errorf("flags %v, want [b]", ids)
		}
	})

	t.Run("a per-criterion line moves only that criterion", func(t *testing.T) {
		client := &stubClient{resp: noulAnswers(map[string]float64{"a": 0.85, "b": 0.4}, "none")}
		th := Thresholds{Met: 0.9, PerCriterion: map[string]float64{"b": 0.3}}
		got, err := Judge(context.Background(), client, photo, occ, th)
		if err != nil {
			t.Fatalf("judge: %v", err)
		}
		if ids := flagIDs(got); !slices.Equal(ids, []string{"a"}) {
			t.Errorf("flags %v, want [a]", ids)
		}
	})

	t.Run("flags keep the occasion's order and wording", func(t *testing.T) {
		client := &stubClient{resp: noulAnswers(map[string]float64{"a": 0.1, "b": 0.1}, "none")}
		got, err := Judge(context.Background(), client, photo, occ, Thresholds{Met: readingAt})
		if err != nil {
			t.Fatalf("judge: %v", err)
		}
		if len(got.Flags) != 2 {
			t.Fatalf("%d flags, want 2", len(got.Flags))
		}
		for i, flag := range got.Flags {
			if flag.ID != occ.Criteria[i].ID || flag.Suggestion != occ.Criteria[i].Suggestion {
				t.Errorf("flag %d is %+v, want the occasion's criterion in its order", i, flag)
			}
		}
	})

	t.Run("the biggest issue keeps the occasion's label", func(t *testing.T) {
		client := &stubClient{resp: noulAnswers(map[string]float64{"a": 0.9, "b": 0.9}, "cant-tell")}
		got, err := Judge(context.Background(), client, photo, occ, Thresholds{Met: readingAt})
		if err != nil {
			t.Fatalf("judge: %v", err)
		}
		if got.Issue.ID != "cant-tell" || got.Issue.Label != "can't tell" {
			t.Errorf("issue %+v, want cant-tell as the occasion words it", got.Issue)
		}
	})

	for name, th := range map[string]Thresholds{
		"met under zero":      {Met: -0.1},
		"met over one":        {Met: 1.1},
		"met not a number":    {Met: math.NaN()},
		"override under zero": {Met: readingAt, PerCriterion: map[string]float64{"a": -1}},
		"override over one":   {Met: readingAt, PerCriterion: map[string]float64{"a": 2}},
	} {
		t.Run(name+" is refused", func(t *testing.T) {
			client := &stubClient{resp: noulAnswers(map[string]float64{"a": 0.9, "b": 0.9}, "none")}
			if _, err := Judge(context.Background(), client, photo, occ, th); err == nil {
				t.Fatal("judge accepted an out-of-range threshold")
			}
		})
	}

	t.Run("an override for a criterion off the occasion is refused", func(t *testing.T) {
		client := &stubClient{resp: noulAnswers(map[string]float64{"a": 0.9, "b": 0.9}, "none")}
		th := Thresholds{Met: readingAt, PerCriterion: map[string]float64{"no-such-criterion": readingAt}}
		_, err := Judge(context.Background(), client, photo, occ, th)
		if err == nil || !strings.Contains(err.Error(), "no-such-criterion") {
			t.Fatalf("error %v, want it to name no-such-criterion", err)
		}
	})
}

func TestJudgeRefusals(t *testing.T) {
	t.Parallel()
	occ := testOccasion()
	photo := systemone.Image{ContentType: "image/jpeg", Bytes: []byte{0xff, 0xd8, 0xff, 0xe0}}

	t.Run("no photo is refused before any call", func(t *testing.T) {
		// Clef answers a judge request that lost its image, and the
		// answer is not one to trust (docs/clef.md). The capture pins
		// the order, not just the wording: a judge that asked the
		// model on an empty photo would leave a request here.
		capture := &captureClient{inner: &stubClient{resp: noulAnswers(map[string]float64{"a": 0.9, "b": 0.9}, "none")}}
		_, err := Judge(context.Background(), capture, systemone.Image{ContentType: "image/jpeg"}, occ, Thresholds{Met: readingAt})
		if n := len(capture.seen); n != 0 {
			t.Fatalf("%d requests went out, want the refusal before any call", n)
		}
		if err == nil || !strings.Contains(err.Error(), "photo") {
			t.Fatalf("error %v, want it to name the photo", err)
		}
	})

	t.Run("a missing criterion answer is refused", func(t *testing.T) {
		client := &stubClient{resp: noulAnswers(map[string]float64{"a": 0.9, "b": 0.9}, "none")}
		delete(client.resp.Answers, "b")
		_, err := Judge(context.Background(), client, photo, occ, Thresholds{Met: readingAt})
		if err == nil || !strings.Contains(err.Error(), "b") {
			t.Fatalf("error %v, want it to name criterion b", err)
		}
	})

	t.Run("an answer that is not a yes probability is refused", func(t *testing.T) {
		client := &stubClient{resp: noulAnswers(map[string]float64{"a": 0.9, "b": 0.9}, "none")}
		client.resp.Answers["b"] = systemone.Answer{Type: systemone.TypeChoice, Choice: "yes"}
		_, err := Judge(context.Background(), client, photo, occ, Thresholds{Met: readingAt})
		if err == nil || !strings.Contains(err.Error(), "b") {
			t.Fatalf("error %v, want it to name criterion b", err)
		}
	})

	t.Run("a missing biggest issue is refused", func(t *testing.T) {
		client := &stubClient{resp: noulAnswers(map[string]float64{"a": 0.9, "b": 0.9}, "none")}
		delete(client.resp.Answers, biggestIssueID)
		_, err := Judge(context.Background(), client, photo, occ, Thresholds{Met: readingAt})
		if err == nil {
			t.Fatal("judge accepted a response with no biggest issue")
		}
	})

	t.Run("an option off the occasion is refused", func(t *testing.T) {
		client := &stubClient{resp: noulAnswers(map[string]float64{"a": 0.9, "b": 0.9}, "the moon")}
		_, err := Judge(context.Background(), client, photo, occ, Thresholds{Met: readingAt})
		if err == nil || !strings.Contains(err.Error(), "the moon") {
			t.Fatalf("error %v, want it to name the option", err)
		}
	})

	t.Run("a seam error carries the stage's name", func(t *testing.T) {
		client := &failingClient{err: errors.New("no recording")}
		_, err := Judge(context.Background(), client, photo, occ, Thresholds{Met: readingAt})
		if err == nil || !strings.HasPrefix(err.Error(), "judge: ") {
			t.Fatalf("error %v, want it to name the judge stage", err)
		}
	})
}

func flagIDs(j Judgement) []string {
	ids := make([]string, len(j.Flags))
	for i, flag := range j.Flags {
		ids[i] = flag.ID
	}
	return ids
}

type failingClient struct{ err error }

func (f *failingClient) Ask(ctx context.Context, req systemone.Request) (systemone.Response, error) {
	return systemone.Response{}, f.err
}
