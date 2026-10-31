package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/systemone"
)

// storyLine is the recall sentence the API contract records for the
// second visit (testdata/api/recall.json).
const storyLine = "Last family function you kept the gold jhumkas and dismissed the red lip."

// storyRecall is the second visit's memory: the kept jhumkas, the
// dismissed red lip, the recorded sentence.
func storyRecall() *Recall {
	return &Recall{
		Line:      storyLine,
		Kept:      []string{"family-gold-jhumkas"},
		Dismissed: []string{"family-red-lip"},
	}
}

// storyJudgement is the first visit's judgement, the flags the story's
// look misses, carried whole from the occasion's own criteria.
func storyJudgement(t *testing.T, occ *domain.Occasion) Judgement {
	t.Helper()
	return Judgement{
		Flags: []domain.Criterion{
			criterion(t, occ, "family-earring-festive"),
			criterion(t, occ, "family-lip-colour"),
		},
		Issue: Issue{ID: "family-earring-festive", Label: "the earring"},
	}
}

// allFixIDs is the occasion's whole menu, in the menu's order.
func allFixIDs(occ *domain.Occasion) []string {
	ids := make([]string, len(occ.Fixes))
	for i, fix := range occ.Fixes {
		ids[i] = fix.ID
	}
	return ids
}

// choiceAnswer builds a pick response: one chosen option and the
// probabilities the choice carries.
func choiceAnswer(choice string, probabilities map[string]float64) systemone.Response {
	return systemone.Response{
		Model: systemone.ModelClef,
		Answers: map[string]systemone.Answer{
			pickQuestionID: {Type: systemone.TypeChoice, Choice: choice, Probabilities: probabilities},
		},
	}
}

// pickCall runs one pick against a capture wrapped around a stub that
// answers resp, so every test sees the request that went out and the
// zero requests that did not.
func pickCall(t *testing.T, occ *domain.Occasion, j Judgement, optin []string, attrs domain.Allowlist, recall *Recall, resp systemone.Response) (*captureClient, Choice, error) {
	t.Helper()
	capture := &captureClient{inner: &stubClient{resp: resp}}
	got, err := Pick(context.Background(), capture, occ, j, optin, attrs, recall)
	return capture, got, err
}

// TestPickRequestCarriesNoImage is the first half of the boundary pin.
// The pick is given flags, an occasion, ticks, an allowlist, and a
// recall, and no image exists in that list: the request the model
// receives has no images field at all, the state decodes strictly as
// the four pick fields, and the one question offers exactly the
// candidate fixes plus the data's none, each in the data's own wording.
func TestPickRequestCarriesNoImage(t *testing.T) {
	t.Parallel()
	occ := loadOccasions(t)["family-function"]
	j := storyJudgement(t, occ)
	optin := allFixIDs(occ)
	attrs := domain.Allowlist{FaceShape: "Heart-shaped"}
	recall := storyRecall()

	capture, got, err := pickCall(t, occ, j, optin, attrs, recall,
		choiceAnswer("family-gold-jhumkas", map[string]float64{"family-gold-jhumkas": 0.6}))
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if len(capture.seen) != 1 {
		t.Fatalf("%d requests went out, want one", len(capture.seen))
	}
	req := capture.seen[0]
	if got.Fix == nil || got.Fix.ID != "family-gold-jhumkas" {
		t.Fatalf("fix %+v, want the kept jhumkas", got.Fix)
	}

	// No image crosses. Not an empty list: no field at all on the wire.
	if len(req.Images) != 0 {
		t.Fatalf("%d images in the request, want none", len(req.Images))
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(canonical(t, req), &wire); err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, ok := wire["images"]; ok {
		t.Fatal(`the request carries an "images" key`)
	}

	// The state is the four pick fields and nothing else; a state key
	// beyond them fails this decode.
	var state pickState
	dec := json.NewDecoder(strings.NewReader(string(req.State)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&state); err != nil {
		t.Fatalf("state does not decode as the pick state: %v", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		t.Fatal("the state carries more than the pick state")
	}
	if state.Occasion != occ.ID || state.Name != occ.Name {
		t.Fatalf("state %+v, want the occasion's id and name", state)
	}

	// The flags are the judgement's, in its order, with the data's own
	// suggestions.
	if len(state.Flags) != len(j.Flags) {
		t.Fatalf("%d flags in the state, want %d", len(state.Flags), len(j.Flags))
	}
	for i, flag := range state.Flags {
		if flag.ID != j.Flags[i].ID || flag.Suggestion != j.Flags[i].Suggestion {
			t.Fatalf("flag %d is %+v, want the judgement's criterion worded as the data words it", i, flag)
		}
	}

	// The attributes name exactly what the allowlist holds.
	if !mapsEqual(state.Attributes, map[string]string{"face_shape": "Heart-shaped"}) {
		t.Fatalf("attributes %v, want only the held face shape", state.Attributes)
	}

	// The recall travels word for word.
	if state.Recall == nil {
		t.Fatal("the state carries no recall")
	}
	if state.Recall.Line != storyLine {
		t.Fatalf("recall line %q, want the recorded sentence verbatim", state.Recall.Line)
	}
	if !slices.Equal(state.Recall.Kept, recall.Kept) || !slices.Equal(state.Recall.Dismissed, recall.Dismissed) {
		t.Fatalf("recall ids %+v, want the kept and dismissed fix ids", state.Recall)
	}

	// One question: the pick choice over the candidates plus none. The
	// dismissed red lip is not among them.
	if len(req.Questions) != 1 {
		t.Fatalf("%d questions, want the one pick", len(req.Questions))
	}
	q, ok := req.Questions[pickQuestionID]
	if !ok {
		t.Fatalf("no %q question", pickQuestionID)
	}
	if q.Type != systemone.TypeChoice {
		t.Fatalf("pick is %q, want choice", q.Type)
	}
	if q.Instructions != pickQuestion {
		t.Fatal("the pick question is not the question this file authors")
	}
	var options map[string]string
	if err := json.Unmarshal(q.Criteria, &options); err != nil {
		t.Fatalf("pick options: %v", err)
	}
	none, _ := noneLabel(occ)
	want := map[string]string{noneID: none}
	for _, fix := range occ.Fixes {
		if fix.ID == "family-red-lip" {
			continue // dismissed by the recall; never offered
		}
		want[fix.ID] = fix.Changes
	}
	if !mapsEqual(options, want) {
		t.Fatalf("options %v, want the candidates in the data's wording plus none", options)
	}
}

// TestPickAllowlistGate is the second half of the boundary pin: no
// attribute outside the allowlist reaches the request. The only door
// attributes come through is domain.Allowlist, a four-field type whose
// decode refuses any other field; pick forwards exactly the fields that
// type holds, and only the ones with a value, keyed by the type's own
// JSON names.
func TestPickAllowlistGate(t *testing.T) {
	t.Parallel()
	occ := loadOccasions(t)["family-function"]
	j := storyJudgement(t, occ)
	optin := allFixIDs(occ)

	t.Run("only the held fields cross", func(t *testing.T) {
		attrs := domain.Allowlist{FaceShape: "Oval", HairLength: "long"}
		capture, _, err := pickCall(t, occ, j, optin, attrs, nil,
			choiceAnswer("family-gold-jhumkas", nil))
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		var state pickState
		if err := json.Unmarshal(capture.seen[0].State, &state); err != nil {
			t.Fatalf("state: %v", err)
		}
		want := map[string]string{"face_shape": "Oval", "hair_length": "long"}
		if !mapsEqual(state.Attributes, want) {
			t.Fatalf("attributes %v, want %v", state.Attributes, want)
		}
	})

	t.Run("nothing held means no attributes at all", func(t *testing.T) {
		capture, _, err := pickCall(t, occ, j, optin, domain.Allowlist{}, nil,
			choiceAnswer("family-gold-jhumkas", nil))
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		var wire map[string]json.RawMessage
		if err := json.Unmarshal(capture.seen[0].State, &wire); err != nil {
			t.Fatalf("state: %v", err)
		}
		if _, ok := wire["attributes"]; ok {
			t.Fatal(`the state carries an "attributes" key with nothing held`)
		}
	})

	t.Run("the keys are the allowlist's own names", func(t *testing.T) {
		full := domain.Allowlist{FaceShape: "oval", HairType: "wavy", HairLength: "long", ColourTones: "warm"}
		have := held(full)
		want := map[string]string{
			"face_shape":   "oval",
			"hair_type":    "wavy",
			"hair_length":  "long",
			"colour_tones": "warm",
		}
		if !mapsEqual(have, want) {
			t.Fatalf("held %v, want the allowlist's four fields", have)
		}
		typ := reflect.TypeOf(domain.Allowlist{})
		if typ.NumField() != 4 {
			t.Fatalf("the allowlist grew to %d fields; the pick state's door grew with it, so name it here", typ.NumField())
		}
	})

	t.Run("the type refuses everything else", func(t *testing.T) {
		for _, raw := range []string{
			`{"photo":"pixels"}`,
			`{"face_shape":"oval","age":"30"}`,
			`{"face_shape":"oval","gender":"x"}`,
			`{"face_shape":"oval","skin_tone":"warm"}`,
		} {
			var got domain.Allowlist
			if err := json.Unmarshal([]byte(raw), &got); err == nil {
				t.Fatalf("%s was accepted; an attribute outside the allowlist must not travel", raw)
			}
		}
	})
}

// TestPickReturnsOnlyMenuFixes is the opt-in pin: a fix outside the
// opt-in menu can never be returned. The candidate set is built before
// the request, so every fix the user ticked can come back (carried
// whole, in the data's own wording), and every fix they did not tick -
// and every id that was never a fix - is refused as an answer.
func TestPickReturnsOnlyMenuFixes(t *testing.T) {
	t.Parallel()
	occasions := loadOccasions(t)
	ids := make([]string, 0, len(occasions))
	for id := range occasions {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	for _, id := range ids {
		occ := occasions[id]
		j := Judgement{Flags: []domain.Criterion{occ.Criteria[0]}}
		optin := allFixIDs(occ)

		t.Run(id, func(t *testing.T) {
			// Every ticked fix can come back, whole.
			for _, fix := range occ.Fixes {
				capture, got, err := pickCall(t, occ, j, optin, domain.Allowlist{}, nil,
					choiceAnswer(fix.ID, map[string]float64{fix.ID: 0.5}))
				if err != nil {
					t.Fatalf("pick %s: %v", fix.ID, err)
				}
				if len(capture.seen) != 1 {
					t.Fatalf("pick %s: %d requests, want one", fix.ID, len(capture.seen))
				}
				if got.Fix == nil || got.Fix.ID != fix.ID {
					t.Fatalf("fix %+v, want %s", got.Fix, fix.ID)
				}
				if got.Fix.Changes != fix.Changes {
					t.Fatalf("fix %s is worded %q, want the menu's own %q", fix.ID, got.Fix.Changes, fix.Changes)
				}
				if got.RunnerUp != nil {
					t.Fatalf("runner-up %s with no second probability, want none", got.RunnerUp.ID)
				}
			}

			// An unticked fix can never come back, however hard the
			// model answers it.
			dropped := occ.Fixes[0]
			_, got, err := pickCall(t, occ, j, optin[1:], domain.Allowlist{}, nil,
				choiceAnswer(dropped.ID, nil))
			if err == nil || !strings.Contains(err.Error(), dropped.ID) {
				t.Fatalf("error %v, want the unticked fix %s refused", err, dropped.ID)
			}
			if got.Fix != nil || got.RunnerUp != nil {
				t.Fatalf("pick %+v came back for an unticked fix", got)
			}

			// An id that was never a fix cannot come back either.
			_, got, err = pickCall(t, occ, j, optin, domain.Allowlist{}, nil,
				choiceAnswer("no-such-fix", nil))
			if err == nil || !strings.Contains(err.Error(), "no-such-fix") {
				t.Fatalf("error %v, want the unknown fix refused", err)
			}
			if got.Fix != nil || got.RunnerUp != nil {
				t.Fatalf("pick %+v came back for an unknown fix", got)
			}

			// The none option declines every fix.
			none, _ := noneLabel(occ)
			_, got, err = pickCall(t, occ, j, optin, domain.Allowlist{}, nil,
				choiceAnswer(noneID, nil))
			if err != nil {
				t.Fatalf("decline %q: %v", none, err)
			}
			if got.Fix != nil || got.RunnerUp != nil {
				t.Fatalf("pick %+v came back for none", got)
			}
		})
	}
}

// TestPickRecallDismissalIsNeverOffered pins the second visit. The
// dismissed red lip is not in the offered options at all, cannot come
// back when the model answers it anyway, and cannot slip in as the
// runner-up through the probabilities: the jhumkas come first and the
// red lip never.
func TestPickRecallDismissalIsNeverOffered(t *testing.T) {
	t.Parallel()
	occ := loadOccasions(t)["family-function"]
	j := storyJudgement(t, occ)
	optin := allFixIDs(occ)
	recall := storyRecall()

	t.Run("the dismissed fix is not among the offered options", func(t *testing.T) {
		capture, _, err := pickCall(t, occ, j, optin, domain.Allowlist{}, recall,
			choiceAnswer("family-gold-jhumkas", nil))
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		var options map[string]string
		q := capture.seen[0].Questions[pickQuestionID]
		if err := json.Unmarshal(q.Criteria, &options); err != nil {
			t.Fatalf("options: %v", err)
		}
		if _, ok := options["family-red-lip"]; ok {
			t.Fatal("the dismissed red lip was offered")
		}
		if len(options) != len(occ.Fixes)-1+1 {
			t.Fatalf("%d options, want the five open fixes plus none", len(options))
		}
	})

	t.Run("the dismissed fix cannot come back as the answer", func(t *testing.T) {
		_, got, err := pickCall(t, occ, j, optin, domain.Allowlist{}, recall,
			choiceAnswer("family-red-lip", nil))
		if err == nil || !strings.Contains(err.Error(), "family-red-lip") {
			t.Fatalf("error %v, want the dismissed fix refused", err)
		}
		if got.Fix != nil || got.RunnerUp != nil {
			t.Fatalf("pick %+v came back for a dismissed fix", got)
		}
	})

	t.Run("the dismissed fix cannot come back as the runner-up", func(t *testing.T) {
		_, got, err := pickCall(t, occ, j, optin, domain.Allowlist{}, recall,
			choiceAnswer("family-gold-jhumkas", map[string]float64{
				"family-gold-jhumkas": 0.6,
				"family-red-lip":      0.3,
				"family-festive-top":  0.1,
			}))
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		if got.Fix == nil || got.Fix.ID != "family-gold-jhumkas" {
			t.Fatalf("fix %+v, want the kept jhumkas first", got.Fix)
		}
		if got.RunnerUp == nil || got.RunnerUp.ID != "family-festive-top" {
			t.Fatalf("runner-up %+v, want the festive top; the dismissed red lip scored higher and must still never come", got.RunnerUp)
		}
	})

	t.Run("dismissing everything leaves nothing to ask", func(t *testing.T) {
		allOut := &Recall{Dismissed: allFixIDs(occ)}
		capture, got, err := pickCall(t, occ, j, optin, domain.Allowlist{}, allOut,
			choiceAnswer("family-gold-jhumkas", nil))
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		if n := len(capture.seen); n != 0 {
			t.Fatalf("%d requests went out, want the empty menu refused before any call", n)
		}
		if got.Fix != nil || got.RunnerUp != nil {
			t.Fatalf("pick %+v came back from an empty menu", got)
		}
	})

	t.Run("a stale dismissed id changes nothing", func(t *testing.T) {
		stale := &Recall{Line: storyLine, Dismissed: []string{"no-such-fix"}}
		capture, got, err := pickCall(t, occ, j, optin, domain.Allowlist{}, stale,
			choiceAnswer("family-red-lip", nil))
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		if len(capture.seen) != 1 {
			t.Fatalf("%d requests, want one", len(capture.seen))
		}
		if got.Fix == nil || got.Fix.ID != "family-red-lip" {
			t.Fatalf("fix %+v, want the red lip still open when nothing dismissed it", got.Fix)
		}
	})
}

// TestPickAsksOnlyWhenThereIsSomethingToPick pins the two vacuous asks:
// nothing flagged needs no fix, and nothing ticked offers no choice.
// Clef answers a vacuous question confidently (docs/clef.md), so both
// are refused before any request, and the capture proves the ordering.
func TestPickAsksOnlyWhenThereIsSomethingToPick(t *testing.T) {
	t.Parallel()
	occ := loadOccasions(t)["family-function"]
	attrs := domain.Allowlist{FaceShape: "Heart-shaped"}

	t.Run("nothing flagged asks nothing", func(t *testing.T) {
		capture, got, err := pickCall(t, occ, Judgement{}, allFixIDs(occ), attrs, storyRecall(),
			choiceAnswer("family-gold-jhumkas", nil))
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		if n := len(capture.seen); n != 0 {
			t.Fatalf("%d requests went out with no flag to fix", n)
		}
		if got.Fix != nil || got.RunnerUp != nil {
			t.Fatalf("pick %+v came back with nothing flagged", got)
		}
	})

	t.Run("nothing ticked asks nothing", func(t *testing.T) {
		capture, got, err := pickCall(t, occ, storyJudgement(t, occ), nil, attrs, nil,
			choiceAnswer("family-gold-jhumkas", nil))
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		if n := len(capture.seen); n != 0 {
			t.Fatalf("%d requests went out with nothing ticked", n)
		}
		if got.Fix != nil || got.RunnerUp != nil {
			t.Fatalf("pick %+v came back with an empty menu", got)
		}
	})

	t.Run("an empty recall asks like a first visit", func(t *testing.T) {
		capture, _, err := pickCall(t, occ, storyJudgement(t, occ), allFixIDs(occ), attrs, &Recall{},
			choiceAnswer("family-gold-jhumkas", nil))
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		var state pickState
		if err := json.Unmarshal(capture.seen[0].State, &state); err != nil {
			t.Fatalf("state: %v", err)
		}
		if state.Recall != nil {
			t.Fatalf("the state carries %+v, want no recall for an empty memory", state.Recall)
		}
	})
}

// TestPickRunnerUp pins where the runner-up comes from: the
// best-probability candidate under the model's own answer, in the
// menu's order on a tie, and none when there is no second fix.
func TestPickRunnerUp(t *testing.T) {
	t.Parallel()
	occ := loadOccasions(t)["family-function"]
	j := storyJudgement(t, occ)
	three := []string{"family-gold-jhumkas", "family-red-lip", "family-festive-top"}

	t.Run("the second probability is the runner-up", func(t *testing.T) {
		_, got, err := pickCall(t, occ, j, three, domain.Allowlist{}, nil,
			choiceAnswer("family-gold-jhumkas", map[string]float64{
				"family-gold-jhumkas": 0.5,
				"family-festive-top":  0.3,
				"family-red-lip":      0.1,
				noneID:                0.1,
			}))
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		if got.RunnerUp == nil || got.RunnerUp.ID != "family-festive-top" {
			t.Fatalf("runner-up %+v, want the festive top", got.RunnerUp)
		}
	})

	t.Run("a tie goes to the menu's order", func(t *testing.T) {
		_, got, err := pickCall(t, occ, j, three, domain.Allowlist{}, nil,
			choiceAnswer("family-gold-jhumkas", map[string]float64{
				"family-gold-jhumkas": 0.4,
				"family-red-lip":      0.3,
				"family-festive-top":  0.3,
			}))
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		if got.RunnerUp == nil || got.RunnerUp.ID != "family-red-lip" {
			t.Fatalf("runner-up %+v, want the red lip, earlier on the menu", got.RunnerUp)
		}
	})

	t.Run("no probabilities name no runner-up", func(t *testing.T) {
		_, got, err := pickCall(t, occ, j, three, domain.Allowlist{}, nil,
			choiceAnswer("family-gold-jhumkas", nil))
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		if got.RunnerUp != nil {
			t.Fatalf("runner-up %s from no probabilities", got.RunnerUp.ID)
		}
	})

	t.Run("a one-fix menu has no runner-up", func(t *testing.T) {
		_, got, err := pickCall(t, occ, j, []string{"family-gold-jhumkas"}, domain.Allowlist{}, nil,
			choiceAnswer("family-gold-jhumkas", map[string]float64{"family-gold-jhumkas": 0.9}))
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		if got.Fix == nil || got.Fix.ID != "family-gold-jhumkas" {
			t.Fatalf("fix %+v, want the jhumkas", got.Fix)
		}
		if got.RunnerUp != nil {
			t.Fatalf("runner-up %s on a one-fix menu", got.RunnerUp.ID)
		}
	})
}

// TestPickReplayKeysTheRequest pins pick onto the existing replay
// machinery. T0.6 recorded no pick call (docs/clef.md: the fix-pick call
// is not in its figures), so the replay answers the typed Missing and
// not a validation error: the request is well formed and hashes through
// the same key a future recording will be stored under. Two identical
// picks hash to one key, so a recording made against this construction
// will serve it.
func TestPickReplayKeysTheRequest(t *testing.T) {
	t.Parallel()
	replay := openRecordings(t)
	occ := loadOccasions(t)["family-function"]
	j := storyJudgement(t, occ)

	key := func() string {
		capture := &captureClient{inner: replay}
		_, err := Pick(context.Background(), capture, occ, j, allFixIDs(occ),
			domain.Allowlist{FaceShape: "Heart-shaped"}, storyRecall())
		var missing *systemone.Missing
		if !errors.As(err, &missing) {
			t.Fatalf("error %v, want the replay's typed Missing", err)
		}
		return missing.Key
	}
	first := key()
	if first != key() {
		t.Fatal("two identical picks hashed to two keys; a recording could not serve them")
	}
}

// TestPickRefusals pins the error paths: no occasion, a seam failure
// that names this stage, and answers that are not a choice.
func TestPickRefusals(t *testing.T) {
	t.Parallel()
	occ := loadOccasions(t)["family-function"]
	j := storyJudgement(t, occ)

	t.Run("no occasion is refused", func(t *testing.T) {
		_, _, err := pickCall(t, nil, j, allFixIDs(occ), domain.Allowlist{}, nil,
			choiceAnswer("family-gold-jhumkas", nil))
		if err == nil || !strings.Contains(err.Error(), "no occasion") {
			t.Fatalf("error %v, want it to name the missing occasion", err)
		}
	})

	t.Run("a seam error carries the stage's name", func(t *testing.T) {
		_, err := Pick(context.Background(), &failingClient{err: errors.New("no recording")},
			occ, j, allFixIDs(occ), domain.Allowlist{}, nil)
		if err == nil || !strings.HasPrefix(err.Error(), "pick: ") {
			t.Fatalf("error %v, want it to name the pick stage", err)
		}
	})

	t.Run("a missing answer is refused", func(t *testing.T) {
		_, _, err := pickCall(t, occ, j, allFixIDs(occ), domain.Allowlist{}, nil,
			systemone.Response{Model: systemone.ModelClef, Answers: map[string]systemone.Answer{}})
		if err == nil || !strings.Contains(err.Error(), "did not answer") {
			t.Fatalf("error %v, want it to name the missing answer", err)
		}
	})

	t.Run("an answer that is not a choice is refused", func(t *testing.T) {
		p := 0.5
		_, _, err := pickCall(t, occ, j, allFixIDs(occ), domain.Allowlist{}, nil,
			systemone.Response{Model: systemone.ModelClef, Answers: map[string]systemone.Answer{
				pickQuestionID: {Type: systemone.TypeNoul, Noul: &p},
			}})
		if err == nil || !strings.Contains(err.Error(), "not a choice") {
			t.Fatalf("error %v, want it to refuse a non-choice", err)
		}
	})
}

// mapsEqual compares two string maps, so a mismatch names the whole map
// instead of one key.
func mapsEqual(got, want map[string]string) bool {
	return reflect.DeepEqual(got, want)
}
