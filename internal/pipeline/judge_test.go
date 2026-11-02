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

// expected is every sample's outcome at readingAt, generated from the
// recorded probabilities in the shipped corpus by running the real Judge
// over every judge recording (docs/clef.md's tables carry the same read,
// cell by cell). On many cells the read is not the README's ground truth:
// at 0.5, V01's, V02's, V09's, V15's, V17's and P09's beards; V02's light;
// V04's and P03's expressions; V08's and P11's beards; P11's garment;
// V09's, V11's, V12's and P05's collars; V11's and P05's interview
// expressions — nineteen cells — sit on the other side of the line. Those
// cells are the read the judge turns out; T3.9 owns moving the lines, not
// this table.
//
// The issue half of each row is the biggest issue Judge reports for that
// sample, in the pinned choice id, not prose: Judge asks one
// biggest_issue choice question whose options are the criterion ids
// (judge.go's request), and Issue's label is the option's own wording
// from the occasion data (judge.go's read, which errors on a choice
// outside the options). The id is gated (judge.go's gateIssue): a choice
// naming a criterion this look's flags do not hold is reported as none,
// and none and cant-tell pass through whatever the flags say. At
// readingAt the two coincide on all 37 recordings - every recorded choice
// names a criterion the 0.5 line does flag, or is none - so T3.11's gate
// moved no row of this table, and the ids below stay the model's own
// pinned choices. The recorder refuses a choice that is not one of the
// request's option ids before it is ever stored (systemone/recorder.go),
// so no model sentence can reach the label field, and the label assertion
// can only fail when the id itself does. A fresh recording therefore moves
// this table's issue ids as a stale measurement, never as the service's
// wording arriving in a user's line.
var expected = map[string]outcome{
	// date
	"V01": {flags: []string{"date-beard-even"}, issue: "date-beard-even"},
	"V02": {flags: []string{"date-collar-open", "date-garment-easy", "date-beard-even", "date-expression-engaged"}, issue: "date-beard-even"},
	"V03": {flags: []string{"date-hair-clear", "date-lip-clear"}, issue: "date-lip-clear"},
	"V21": {flags: []string{"date-beard-even"}, issue: "date-beard-even"},
	"P01": {issue: "none"},
	"P02": {flags: []string{"date-collar-open", "date-garment-easy", "date-hair-clear", "date-lip-clear"}, issue: "date-lip-clear"},
	"P09": {flags: []string{"date-beard-even"}, issue: "date-beard-even"},
	"P10": {flags: []string{"date-beard-even", "date-expression-engaged", "date-light-see"}, issue: "date-beard-even"},
	// family function
	"V04": {flags: []string{"family-earring-festive", "family-lip-colour", "family-expression-present"}, issue: "family-earring-festive"},
	"V05": {issue: "none"},
	"V06": {flags: []string{"family-hair-finished", "family-expression-present"}, issue: "family-hair-finished"},
	"V07": {flags: []string{"family-earring-festive", "family-lip-colour"}, issue: "none"},
	"V08": {flags: []string{"family-earring-festive", "family-lip-colour", "family-garment-festive", "family-expression-present", "family-light-even"}, issue: "family-garment-festive"},
	"P03": {flags: []string{"family-expression-present"}, issue: "none"},
	"P04": {flags: []string{"family-earring-festive", "family-lip-colour", "family-garment-festive", "family-hair-finished", "family-expression-present", "family-light-even"}, issue: "family-garment-festive"},
	"P11": {flags: []string{"family-earring-festive", "family-lip-colour", "family-garment-festive", "family-beard-even"}, issue: "family-beard-even"},
	"P12": {flags: []string{"family-earring-festive", "family-lip-colour", "family-garment-festive", "family-beard-even", "family-expression-present"}, issue: "family-beard-even"},
	// interview
	"V09": {flags: []string{"interview-collar-buttoned", "interview-beard-short"}, issue: "interview-beard-short"},
	"V10": {flags: []string{"interview-collar-buttoned", "interview-garment-plain", "interview-beard-short", "interview-expression-calm", "interview-light-plain"}, issue: "interview-garment-plain"},
	"V11": {flags: []string{"interview-collar-buttoned", "interview-hair-close", "lip-quiet", "interview-expression-calm"}, issue: "lip-quiet"},
	"V12": {flags: []string{"interview-collar-buttoned"}, issue: "none"},
	"P05": {flags: []string{"interview-collar-buttoned", "interview-hair-close", "interview-expression-calm"}, issue: "interview-hair-close"},
	"P13": {flags: []string{"interview-collar-buttoned", "interview-garment-plain", "interview-beard-short", "interview-expression-calm", "interview-light-plain"}, issue: "interview-garment-plain"},
	// festival
	"V13": {issue: "none"},
	"V14": {flags: []string{"festival-garment-festive", "festival-earring", "festival-necklace", "festival-lip", "festival-hair", "festival-expression", "festival-light"}, issue: "festival-hair"},
	"V15": {flags: []string{"festival-earring", "festival-necklace", "festival-lip", "festival-beard"}, issue: "festival-beard"},
	"V16": {flags: []string{"festival-garment-festive", "festival-earring", "festival-necklace", "festival-lip", "festival-beard", "festival-light"}, issue: "festival-garment-festive"},
	"P06": {flags: []string{"festival-hair", "festival-expression"}, issue: "festival-expression"},
	"P14": {flags: []string{"festival-earring", "festival-necklace", "festival-lip", "festival-beard"}, issue: "festival-beard"},
	// funeral
	"V17": {flags: []string{"funeral-beard-close"}, issue: "funeral-beard-close"},
	"V18": {flags: []string{"funeral-garment-dark", "funeral-collar-plain", "funeral-beard-close", "funeral-expression-still", "funeral-light-plain"}, issue: "funeral-garment-dark"},
	"V19": {issue: "none"},
	"V20": {flags: []string{"funeral-garment-dark", "funeral-collar-plain", "funeral-jewellery-quiet", "lip-quiet", "funeral-hair-plain", "funeral-expression-still", "funeral-light-plain"}, issue: "funeral-expression-still"},
	"P07": {issue: "none"},
	"P08": {flags: []string{"funeral-collar-plain", "funeral-jewellery-quiet", "lip-quiet", "funeral-hair-plain"}, issue: "funeral-jewellery-quiet"},
	"P15": {flags: []string{"funeral-beard-close"}, issue: "funeral-beard-close"},
	"P16": {flags: []string{"funeral-beard-close", "funeral-expression-still", "funeral-light-plain"}, issue: "funeral-expression-still"},
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

// TestJudgeIssueGatedOnTheFlags is the biggest issue's gate in the three
// shapes it has to get right, each on its own lines of the two-criterion
// occasion, and named in the report:
//
//   - a choice naming a criterion this look does not flag becomes the
//     occasion's own none (the misattribution the re-recorded corpus hits
//     on the interview sample);
//   - a choice naming a criterion this look does flag survives, label and
//     all, which is what four of the five demo stories already are;
//   - the two options that name no criterion - cant-tell and none -
//     survive on a look with no flags at all, because the gate suppresses
//     a misattributed criterion and never the model's uncertainty.
//
// A suppression is not an error: every case here judges clean.
func TestJudgeIssueGatedOnTheFlags(t *testing.T) {
	t.Parallel()
	occ := testOccasion()
	photo := systemone.Image{ContentType: "image/jpeg", Bytes: []byte{0xff, 0xd8, 0xff, 0xe0}}

	for _, tc := range []struct {
		name       string
		nouls      map[string]float64
		choice     string
		flags      []string
		issue      string
		label      string
		suppressed bool
	}{
		{
			// a flags, b reads met: the choice names b, which this
			// look does not flag. Neither the empty nor the
			// half-full flag list matters here - the choice
			// contradicts the flags either way.
			name:   "a criterion the flags do not hold becomes none",
			nouls:  map[string]float64{"a": 0.1, "b": 0.9},
			choice: "b", flags: []string{"a"}, issue: "none", label: "none",
			suppressed: true,
		},
		{
			// The same on a look with nothing flagged, the demo
			// story's shape: nothing to single out, so nothing is
			// singled out.
			name:   "a criterion the look flags nothing of becomes none",
			nouls:  map[string]float64{"a": 0.9, "b": 0.9},
			choice: "b", flags: nil, issue: "none", label: "none",
			suppressed: true,
		},
		{
			name:   "a criterion the flags do hold survives",
			nouls:  map[string]float64{"a": 0.1, "b": 0.9},
			choice: "a", flags: []string{"a"}, issue: "a", label: "the a",
		},
		{
			name:   "one of several flags survives in its own wording",
			nouls:  map[string]float64{"a": 0.1, "b": 0.1},
			choice: "b", flags: []string{"a", "b"}, issue: "b", label: "the b",
		},
		{
			// judge_test.go's existing pin, restated beside the gate:
			// a refusal to pick is not a claim about a criterion, so
			// the flags being empty does not touch it.
			name:   "can't tell survives a look with no flags",
			nouls:  map[string]float64{"a": 0.9, "b": 0.9},
			choice: "cant-tell", flags: nil, issue: "cant-tell", label: "can't tell",
		},
		{
			name:   "none survives a look with no flags",
			nouls:  map[string]float64{"a": 0.9, "b": 0.9},
			choice: "none", flags: nil, issue: "none", label: "none",
		},
		{
			name:   "none survives a look that does flag",
			nouls:  map[string]float64{"a": 0.1, "b": 0.1},
			choice: "none", flags: []string{"a", "b"}, issue: "none", label: "none",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &stubClient{resp: noulAnswers(tc.nouls, tc.choice)}
			got, err := Judge(context.Background(), client, photo, occ, Thresholds{Met: readingAt})
			if err != nil {
				t.Fatalf("judge: %v", err)
			}
			if ids := flagIDs(got); !slices.Equal(ids, tc.flags) {
				t.Fatalf("flags %v, want %v", ids, tc.flags)
			}
			if got.Issue.ID != tc.issue {
				t.Errorf("issue %q, want %q (the flags hold %v, the choice named %q)",
					got.Issue.ID, tc.issue, tc.flags, tc.choice)
			}
			if got.Issue.Label != tc.label {
				t.Errorf("issue label %q, want the occasion's own %q", got.Issue.Label, tc.label)
			}
			// The suppressed rows must not read back as the model's
			// choice: the whole point is that a misattributed
			// criterion never reaches the app.
			if tc.suppressed && got.Issue.ID == tc.choice {
				t.Errorf("the choice %q passed through unconditioned", tc.choice)
			}
		})
	}
}

// TestJudgeGatesTheRecordedInterviewIssue is the gate on the recording
// that turned the defect red: testdata/clef/judge-V09-interview.json, the
// interview sample behind sample-interview. Its biggest_issue choice is
// interview-beard-short, and the beard's own reading 0.2175 sits above its
// shipped line 0.18815, so a look read at a line every criterion clears
// flags nothing and the choice names a criterion none of the flags hold.
// Through the pipeline that is none; on the recording it stays
// interview-beard-short, because the recordings are evidence and this task
// edits none of them.
//
// The two thresholds are the proof the gate reads the flags and not the
// file: the same recording, the same choice, survives at a line that does
// flag the beard and is suppressed at one that does not. The shipped lines
// read this look flagless; internal/server/samples_test.go's clause (d) is
// where the real Judge runs over it at those lines.
func TestJudgeGatesTheRecordedInterviewIssue(t *testing.T) {
	t.Parallel()
	clefDir := filepath.Join(repoRoot(t), "testdata", "clef")
	replay := openRecordings(t)
	occasions := loadOccasions(t)
	occ := occasions["interview"]
	s := sample(t, clefDir, filepath.Join(clefDir, "judge-V09-interview.json"), occasions)

	recorded, err := replay.Reply(s.exchange)
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	// The raw choice, straight out of the recording: no threshold reads it.
	if raw := recordedChoice(t, recorded); raw != "interview-beard-short" {
		t.Fatalf("the recording chose %q, want the choice the defect rode on", raw)
	}

	// A line every criterion on this look clears, so the look flags
	// nothing - the shipped lines' shape for this sample. The beard's
	// reading is the lowest on it at 0.2175, so 0.2 leaves every criterion
	// met and no flag for the choice to name.
	got, err := Judge(context.Background(), replay, s.photo, occ, Thresholds{Met: 0.2})
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if n := len(got.Flags); n != 0 {
		t.Fatalf("flags %v, want a look with nothing flagged", flagIDs(got))
	}
	if got.Issue.ID != "none" {
		t.Errorf("issue %q through the pipeline, want none: the recording's choice names a criterion the flags do not hold",
			got.Issue.ID)
	}
	if got.Issue.Label != issueLabel(t, occ, "none") {
		t.Errorf("issue label %q is not the occasion's own wording for none", got.Issue.Label)
	}

	// At readingAt the beard does flag, and the very same choice is the
	// look's own honest issue: the gate kept it.
	flagged, err := Judge(context.Background(), replay, s.photo, occ, Thresholds{Met: readingAt})
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if ids := flagIDs(flagged); !slices.Equal(ids, []string{"interview-collar-buttoned", "interview-beard-short"}) {
		t.Fatalf("flags %v, want the collar and the beard at %v", ids, readingAt)
	}
	if flagged.Issue.ID != "interview-beard-short" {
		t.Errorf("issue %q, want the choice kept: it names a criterion this look flags", flagged.Issue.ID)
	}
	if flagged.Issue.Label != issueLabel(t, occ, "interview-beard-short") {
		t.Errorf("issue label %q is not the occasion's own wording", flagged.Issue.Label)
	}
}

// TestJudgeGateRefusesAnOccasionWithNoNoneOption closes the gate's one
// loose end. A suppressed choice is reported as the occasion's own none,
// id and label taken from its option list, never composed here - so an
// occasion whose options carried no none has nothing honest to report, and
// the judge says so instead of inventing a string no occasion's wording
// carries or quietly keeping the misattribution. The loader guarantees
// every occasion that loads has its none (domain.load.go's
// validateBiggest), so no real dress code reaches this; the rule is what
// would happen if one did.
func TestJudgeGateRefusesAnOccasionWithNoNoneOption(t *testing.T) {
	t.Parallel()
	photo := systemone.Image{ContentType: "image/jpeg", Bytes: []byte{0xff, 0xd8, 0xff, 0xe0}}
	occ := testOccasion()
	withoutNone := *occ
	withoutNone.BiggestIssue = domain.BiggestIssue{
		Question: occ.BiggestIssue.Question,
		Options: []domain.IssueOption{
			{ID: "a", Label: "the a"},
			{ID: "b", Label: "the b"},
			{ID: "cant-tell", Label: "can't tell"},
		},
	}

	t.Run("a choice that needs suppressing is an error", func(t *testing.T) {
		client := &stubClient{resp: noulAnswers(map[string]float64{"a": 0.9, "b": 0.9}, "b")}
		_, err := Judge(context.Background(), client, photo, &withoutNone, Thresholds{Met: readingAt})
		if err == nil || !strings.Contains(err.Error(), `"none"`) {
			t.Fatalf("error %v, want it to name the none option the occasion does not carry", err)
		}
	})

	t.Run("a choice that needs no suppression still reads", func(t *testing.T) {
		// The refusal is about the fallback, not about the occasion's
		// shape: a flagged criterion and can't tell are both reported
		// straight from this occasion's own options.
		client := &stubClient{resp: noulAnswers(map[string]float64{"a": 0.1, "b": 0.9}, "a")}
		got, err := Judge(context.Background(), client, photo, &withoutNone, Thresholds{Met: readingAt})
		if err != nil {
			t.Fatalf("judge: %v", err)
		}
		if got.Issue.ID != "a" || got.Issue.Label != "the a" {
			t.Errorf("issue %+v, want the flagged criterion kept", got.Issue)
		}
		tell := &stubClient{resp: noulAnswers(map[string]float64{"a": 0.9, "b": 0.9}, "cant-tell")}
		again, err := Judge(context.Background(), tell, photo, &withoutNone, Thresholds{Met: readingAt})
		if err != nil {
			t.Fatalf("judge: %v", err)
		}
		if again.Issue.ID != "cant-tell" {
			t.Errorf("issue %q, want can't tell kept on a look with no flags", again.Issue.ID)
		}
	})
}

// recordedChoice is the biggest_issue choice a recording answered, read
// out of its own response body: what the model said, before any line or
// any gate. The answer is looked for at the two nestings a recording
// carries it at, the bare result and the result inside the HTTP body, so
// this reads the file as stored rather than through the seam.
func recordedChoice(t *testing.T, rec systemone.Recording) string {
	t.Helper()
	var env struct {
		Result json.RawMessage `json:"result"`
		Body   struct {
			Result json.RawMessage `json:"result"`
		} `json:"body"`
	}
	if err := json.Unmarshal(rec.Response, &env); err != nil {
		t.Fatalf("recorded response: %v", err)
	}
	raw := env.Result
	if len(raw) == 0 {
		raw = env.Body.Result
	}
	var result struct {
		Answers map[string]struct {
			Type   systemone.Kind `json:"type"`
			Choice string         `json:"choice"`
		} `json:"answers"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || len(result.Answers) == 0 {
		t.Fatalf("recorded response carries no answers: %v", err)
	}
	one, ok := result.Answers[biggestIssueID]
	if !ok {
		t.Fatal("the recording answers no biggest_issue")
	}
	if one.Type != systemone.TypeChoice || one.Choice == "" {
		t.Fatalf("the recorded biggest issue is %q, not a choice", one.Type)
	}
	return one.Choice
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
