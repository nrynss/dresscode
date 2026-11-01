// Check is this file: one Clef request that reads a render against the
// photo it came from. Judge, pick, measure and render ran before it; the
// fallback chain (T3.6) reads its outcome. No probability leaves this
// file: the three probabilities become the check's facts here, and the
// choice answers become the look in words.

package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/systemone"
)

// The three gate question ids and the look questions' prefix. They are
// the ids the recorded check requests carry (docs/clef.md), and a
// recording replays only for the same ids.
const (
	changeMadeID   = "change_made"
	samePersonID   = "same_person"
	criterionMetID = "criterion_met"
	lookIDPrefix   = "look."
)

// The three gate questions' wording, and the sentence that tells Clef
// which image is which. All four are the recorded probe's own wording
// (docs/clef.md): this task's final wording is the recorded one, because
// a wording change invalidates every check recording and none can be
// remade offline. A new wording is a re-recording decision, and these
// constants are where it lands.
const (
	changeMadeQuestion = "Was the requested change made in the render?"
	samePersonQuestion = "Is the person in the render the same person as in the original, with the face shape and skin texture unchanged?"
	imagesLine         = "The first image is the original photo. The second image is the render. Answer every question about the render."
)

// The two look-vocabulary options that say nothing about the look. An
// attribute answered with either is left out of the check's attributes
// and out of the look-in-words line, as the API contract states.
const (
	noneOption     = "none"
	cantTellOption = "can't tell"
)

// Lines turn the check's three probabilities into its three facts. A
// probability at or above its line is yes; under it, no. Every value is
// a placeholder until T3.9 sets them from measurement, so they are an
// argument, not a constant in this package, and a value outside [0, 1]
// - NaN included - is refused before any call.
type Lines struct {
	// ChangeMade is the line for "was the requested change made".
	ChangeMade float64
	// SamePerson is the line for "is it the same person".
	SamePerson float64
	// CriterionMet is the line for the criterion the render was meant
	// to serve.
	CriterionMet float64
}

func (l Lines) validate() error {
	for _, line := range []struct {
		name  string
		value float64
	}{
		{"change made", l.ChangeMade},
		{"same person", l.SamePerson},
		{"criterion met", l.CriterionMet},
	} {
		if !between(line.value) {
			return fmt.Errorf("check: %s line %v is not between 0 and 1", line.name, line.value)
		}
	}
	return nil
}

// Outcome is the check's output: Clef's facts about the render, and the
// look in words. It carries no probability and no score - there is
// nothing here a fallback could rank one render against another with.
//
// Shown is the discard rule, and it is the rule by construction: the
// method reads the change-made and same-person facts and nothing else,
// so no met score, no attribute and no wording can outrank a failing
// gate. product.md: a render that fails either of the first two is
// discarded, never shown. A criterion the render honestly failed does
// not discard it - the change was made and the person is the same, and
// the criterion's suggestion stands either way.
type Outcome struct {
	Check domain.Check `json:"check"`
	Words string       `json:"words"`
}

// Shown reports whether the render survives the check. This is the whole
// decision; the fallback chain (T3.6) reads it and falls back when it is
// false.
func (o Outcome) Shown() bool {
	return o.Check.ChangeMade && o.Check.SamePerson
}

// checkState is the whole state of a check request. The request is built
// from this struct, so nothing else can reach Clef in the state: a YouCam
// analysis, a probability, a product photo and a user identifier have no
// field to travel in. The two images travel as images, in the recorded
// order: the original first, the render second - the order the state's
// own sentence names, and the order the recordings pin.
type checkState struct {
	Occasion string `json:"occasion"`
	Name     string `json:"name"`
	// Images is the recorded sentence that names the two images' roles.
	Images string `json:"images"`
	// RequestedChange is what the render was asked to do, in the
	// words the caller rendered with. The check does not compose it:
	// the words belong to the render attempt, and T3.6 passes the
	// fix's own changes for it.
	RequestedChange string `json:"requested_change"`
	// Criterion is the one criterion this render was meant to serve.
	Criterion string `json:"criterion"`
}

// Check reads one render against the photo it came from. One Clef
// request carries both images - the original, then the render - the
// occasion's id and name, the requested change, the one criterion this
// render was meant to serve, the three gate questions, and one choice
// per look attribute in the vocabulary's own wording.
//
// The criterion is the caller's to name, because the data gives three
// fixes two criteria each and the recorded shape checks one: Check
// refuses a criterion the occasion does not have and one the fix does
// not address, so a render can only ever be checked against a criterion
// it was picked for. The requested change is the caller's words too:
// the probe's recorded check names a two-fix edit, and this step states
// what was asked of the renderer, never a sentence it composed.
//
// Both images are required before any call. Clef answers a check that
// lost an image instead of failing (the judge's lesson, docs/clef.md),
// and an answer about a missing render is not one to trust.
//
// The model is the caller's choice: the recorded check ran on clef and
// on clef-flash, and the evaluation (T3.9) owns picking one. Both
// replay.
func Check(ctx context.Context, client systemone.SystemOne, model string, original, render systemone.Image, occ *domain.Occasion, fix *domain.Fix, criterionID, requested string, vocab *domain.Vocabulary, lines Lines) (Outcome, error) {
	if occ == nil {
		return Outcome{}, errors.New("check: no occasion")
	}
	if fix == nil {
		return Outcome{}, errors.New("check: no fix")
	}
	if err := lines.validate(); err != nil {
		return Outcome{}, err
	}
	if model == "" {
		return Outcome{}, errors.New("check: no model")
	}
	if len(original.Bytes) == 0 {
		return Outcome{}, errors.New("check: the original photo is empty")
	}
	if len(render.Bytes) == 0 {
		return Outcome{}, errors.New("check: the render is empty")
	}
	criterion, ok := criterionByID(occ, criterionID)
	if !ok {
		return Outcome{}, fmt.Errorf("check: occasion %q has no criterion %q", occ.ID, criterionID)
	}
	if !fixAddresses(fix, criterionID) {
		return Outcome{}, fmt.Errorf("check: fix %q does not address criterion %q", fix.ID, criterionID)
	}
	if strings.TrimSpace(requested) == "" {
		return Outcome{}, errors.New("check: the requested change is empty")
	}
	if vocab == nil {
		return Outcome{}, errors.New("check: no vocabulary")
	}
	if len(vocab.Attributes) == 0 {
		return Outcome{}, errors.New("check: the vocabulary has no attributes")
	}
	req, err := checkRequest(model, original, render, occ, criterion, requested, vocab)
	if err != nil {
		return Outcome{}, err
	}
	resp, err := client.Ask(ctx, req)
	if err != nil {
		return Outcome{}, fmt.Errorf("check: %w", err)
	}
	return checkRead(resp, vocab, lines)
}

// criterionByID finds the occasion's own wording for the criterion, so
// the question is the data's sentence and never a copy that could drift.
func criterionByID(occ *domain.Occasion, id string) (domain.Criterion, bool) {
	for _, c := range occ.Criteria {
		if c.ID == id {
			return c, true
		}
	}
	return domain.Criterion{}, false
}

func fixAddresses(fix *domain.Fix, id string) bool {
	for _, c := range fix.Criteria {
		if c == id {
			return true
		}
	}
	return false
}

// checkRequest builds the one Clef request. Every byte of it is
// traceable: the state is checkState, the three gates are the recorded
// wordings, the criterion question is the occasion's own sentence, and
// the look questions are the vocabulary's own questions and options. The
// images are the original first and the render second.
func checkRequest(model string, original, render systemone.Image, occ *domain.Occasion, criterion domain.Criterion, requested string, vocab *domain.Vocabulary) (systemone.Request, error) {
	state, err := json.Marshal(checkState{
		Occasion:        occ.ID,
		Name:            occ.Name,
		Images:          imagesLine,
		RequestedChange: requested,
		Criterion:       criterion.ID,
	})
	if err != nil {
		return systemone.Request{}, fmt.Errorf("check: %w", err)
	}
	questions := make(map[string]systemone.Question, 3+len(vocab.Attributes))
	questions[changeMadeID] = systemone.Question{Type: systemone.TypeNoul, Instructions: changeMadeQuestion}
	questions[samePersonID] = systemone.Question{Type: systemone.TypeNoul, Instructions: samePersonQuestion}
	questions[criterionMetID] = systemone.Question{Type: systemone.TypeNoul, Instructions: criterion.Clef}
	for _, attr := range vocab.Attributes {
		options := make(map[string]string, len(attr.Options))
		for _, o := range attr.Options {
			options[o] = o
		}
		choice, err := json.Marshal(options)
		if err != nil {
			return systemone.Request{}, fmt.Errorf("check: %w", err)
		}
		questions[lookIDPrefix+attr.ID] = systemone.Question{
			Type:         systemone.TypeChoice,
			Instructions: attr.Question,
			Criteria:     choice,
		}
	}
	req := systemone.Request{
		Model:     model,
		Images:    []systemone.Image{original, render},
		State:     state,
		Questions: questions,
	}
	if err := req.Validate(); err != nil {
		return systemone.Request{}, fmt.Errorf("check: %w", err)
	}
	return req, nil
}

// checkRead turns the answers into the outcome. The three gates become
// facts at their lines. Each attribute's winning option must be one the
// vocabulary offers - a value off the list is refused, not passed on -
// and the attributes keep the vocabulary's own order. The line composes
// from the same answers through the phrase table (LookLine), a pure
// function of the winners and the loaded data, so identical answers
// always join into the identical line.
func checkRead(resp systemone.Response, vocab *domain.Vocabulary, lines Lines) (Outcome, error) {
	var outcome Outcome
	change, err := gateAnswer(resp, changeMadeID)
	if err != nil {
		return Outcome{}, err
	}
	same, err := gateAnswer(resp, samePersonID)
	if err != nil {
		return Outcome{}, err
	}
	met, err := gateAnswer(resp, criterionMetID)
	if err != nil {
		return Outcome{}, err
	}
	outcome.Check.ChangeMade = change >= lines.ChangeMade
	outcome.Check.SamePerson = same >= lines.SamePerson
	outcome.Check.CriterionMet = met >= lines.CriterionMet

	for _, attr := range vocab.Attributes {
		answer, ok := resp.Answers[lookIDPrefix+attr.ID]
		if !ok {
			return Outcome{}, fmt.Errorf("check: the model did not answer %s%s", lookIDPrefix, attr.ID)
		}
		if answer.Type != systemone.TypeChoice || answer.Choice == "" {
			return Outcome{}, fmt.Errorf("check: %s%s is not a choice", lookIDPrefix, attr.ID)
		}
		if !offers(attr.Options, answer.Choice) {
			return Outcome{}, fmt.Errorf("check: %s%s answered %q, which is not one of the attribute's options", lookIDPrefix, attr.ID, answer.Choice)
		}
		if answer.Choice == noneOption || answer.Choice == cantTellOption {
			continue
		}
		outcome.Check.Attributes = append(outcome.Check.Attributes, domain.AnsweredAttribute{ID: attr.ID, Value: answer.Choice})
	}
	outcome.Words, err = LookLine(vocab, outcome.Check.Attributes)
	if err != nil {
		return Outcome{}, fmt.Errorf("check: %w", err)
	}
	return outcome, nil
}

// gateAnswer reads one gate's yes-probability, refusing a missing,
// mistyped or empty answer by name.
func gateAnswer(resp systemone.Response, id string) (float64, error) {
	answer, ok := resp.Answers[id]
	if !ok {
		return 0, fmt.Errorf("check: the model did not answer %s", id)
	}
	if answer.Type != systemone.TypeNoul || answer.Noul == nil {
		return 0, fmt.Errorf("check: %s is not a yes probability", id)
	}
	return *answer.Noul, nil
}

// offers reports whether the vocabulary attribute offers the value.
func offers(options []string, want string) bool {
	for _, o := range options {
		if o == want {
			return true
		}
	}
	return false
}
