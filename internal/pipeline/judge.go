// Package pipeline runs one look, one stage per file in the order
// product.md names: judge, pick, measure, render, check, shop, remember.
// Each stage is one call to a model seam or a client, and each test pins
// its boundary: what the model received and what it must never receive.
//
// Judge is this file: one Clef request that reads the photo against the
// occasion's criteria. Pick (T3.2) consumes its flags; nothing downstream
// consumes a probability.
package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/systemone"
)

// biggestIssueID is the question id of the judge's one choice question,
// the id the recordings carry.
const biggestIssueID = "biggest_issue"

// judgeState is the whole state of a judge request: the occasion's id and
// its name. The request is built from this struct, so nothing else can
// reach Clef in the state: a YouCam analysis, a score, or a user
// identifier has no field to travel in. The wording of every question is
// copied from the occasion's criteria, never composed here.
type judgeState struct {
	Occasion string `json:"occasion"`
	Name     string `json:"name"`
}

// Thresholds turn Clef's probabilities into flags. A criterion reads as
// met when its yes-probability is at least its threshold; under it, the
// criterion is flagged. The values come from the settings file, and every
// value there today is a placeholder until T3.9 sets them from
// measurement. Judge takes them as an argument, so a guessed number
// cannot silently ship inside this package.
type Thresholds struct {
	// Met is the met line for a criterion with no override.
	Met float64
	// PerCriterion names a met line per criterion id, one the evaluation
	// chose from that criterion's curve. A key that is not a criterion on
	// the occasion is an error, so a stale setting fails loudly instead
	// of quietly stopping to apply.
	PerCriterion map[string]float64
}

func (t Thresholds) validate(criteria []domain.Criterion) error {
	if !between(t.Met) {
		return fmt.Errorf("judge: met threshold %v is not between 0 and 1", t.Met)
	}
	ids := make(map[string]struct{}, len(criteria))
	for _, c := range criteria {
		ids[c.ID] = struct{}{}
	}
	for id, th := range t.PerCriterion {
		if _, ok := ids[id]; !ok {
			return fmt.Errorf("judge: threshold names criterion %q, which is not on this occasion", id)
		}
		if !between(th) {
			return fmt.Errorf("judge: threshold for %q is %v, not between 0 and 1", id, th)
		}
	}
	return nil
}

// between reports whether v sits in [0, 1]. The conjunction, not a pair
// of disjunctions, is what catches NaN: every comparison on NaN refuses.
func between(v float64) bool {
	return v >= 0 && v <= 1
}

func (t Thresholds) forCriterion(id string) float64 {
	if th, ok := t.PerCriterion[id]; ok {
		return th
	}
	return t.Met
}

// Issue is the judge's biggest-issue answer: the option Clef picked, in
// the occasion's own wording. ID is one of the occasion's criterion ids,
// or "none", or "cant-tell". The label is the option's label from the
// data, never composed.
type Issue struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Judgement is the judge's output: the criteria the look misses, each
// carried with the user-facing wording the data gives it, and the
// biggest issue. It carries no probability: probabilities steer this
// decision and die here, before any user or any downstream stage sees
// one.
type Judgement struct {
	Flags []domain.Criterion `json:"flags"`
	Issue Issue              `json:"issue"`
}

// Judge reads one photo against one occasion. One Clef request carries
// the photo, the occasion's id and name, one noul question per criterion
// in the data's wording, and the biggest-issue choice. It never receives
// a YouCam analysis: the request is built from the photo and the occasion
// alone, and this package does not import the YouCam client.
//
// A criterion with no photo to judge cannot be asked at all: Clef answers
// a judge request that lost its image, and the answer is not one to trust
// (docs/clef.md), so an empty photo is refused before any call.
//
// The flags come back in the occasion's criteria order. Their wording is
// the occasion's, so the app renders a suggestion and never a score.
func Judge(ctx context.Context, client systemone.SystemOne, photo systemone.Image, occ *domain.Occasion, th Thresholds) (Judgement, error) {
	if occ == nil {
		return Judgement{}, errors.New("judge: no occasion")
	}
	if err := th.validate(occ.Criteria); err != nil {
		return Judgement{}, err
	}
	if len(photo.Bytes) == 0 {
		return Judgement{}, errors.New("judge: the photo is empty")
	}
	if len(occ.Criteria) == 0 {
		return Judgement{}, fmt.Errorf("judge: occasion %q has no criteria", occ.ID)
	}
	req, err := request(photo, occ)
	if err != nil {
		return Judgement{}, err
	}
	resp, err := client.Ask(ctx, req)
	if err != nil {
		return Judgement{}, fmt.Errorf("judge: %w", err)
	}
	return read(occ, resp, th)
}

// request builds the one Clef request from the photo and the occasion.
// Every byte of it is traceable to those two: the state is judgeState,
// the questions are the criteria's own wording plus the biggest-issue
// choice, and the photo is the only image.
func request(photo systemone.Image, occ *domain.Occasion) (systemone.Request, error) {
	state, err := json.Marshal(judgeState{Occasion: occ.ID, Name: occ.Name})
	if err != nil {
		return systemone.Request{}, fmt.Errorf("judge: %w", err)
	}
	options := make(map[string]string, len(occ.BiggestIssue.Options))
	for _, o := range occ.BiggestIssue.Options {
		options[o.ID] = o.Label
	}
	choice, err := json.Marshal(options)
	if err != nil {
		return systemone.Request{}, fmt.Errorf("judge: %w", err)
	}
	questions := make(map[string]systemone.Question, len(occ.Criteria)+1)
	for _, c := range occ.Criteria {
		questions[c.ID] = systemone.Question{Type: systemone.TypeNoul, Instructions: c.Clef}
	}
	questions[biggestIssueID] = systemone.Question{
		Type:         systemone.TypeChoice,
		Instructions: occ.BiggestIssue.Question,
		Criteria:     choice,
	}
	req := systemone.Request{
		Model:     systemone.ModelClef,
		Images:    []systemone.Image{photo},
		State:     state,
		Questions: questions,
	}
	if err := req.Validate(); err != nil {
		return systemone.Request{}, fmt.Errorf("judge: %w", err)
	}
	return req, nil
}

// read turns the answers into a judgement. A criterion flags when its
// yes-probability sits under its threshold; the flagged criterion is
// carried whole, so its suggestion travels with it. The biggest issue is
// carried as the occasion words the chosen option.
func read(occ *domain.Occasion, resp systemone.Response, th Thresholds) (Judgement, error) {
	var judgement Judgement
	for _, c := range occ.Criteria {
		answer, ok := resp.Answers[c.ID]
		if !ok {
			return Judgement{}, fmt.Errorf("judge: the model did not answer criterion %q", c.ID)
		}
		if answer.Type != systemone.TypeNoul || answer.Noul == nil {
			return Judgement{}, fmt.Errorf("judge: answer %q is not a yes probability", c.ID)
		}
		if *answer.Noul < th.forCriterion(c.ID) {
			judgement.Flags = append(judgement.Flags, c)
		}
	}
	answer, ok := resp.Answers[biggestIssueID]
	if !ok {
		return Judgement{}, errors.New("judge: the model did not answer the biggest issue")
	}
	if answer.Type != systemone.TypeChoice || answer.Choice == "" {
		return Judgement{}, errors.New("judge: the biggest issue is not a choice")
	}
	for _, o := range occ.BiggestIssue.Options {
		if o.ID == answer.Choice {
			judgement.Issue = Issue{ID: o.ID, Label: o.Label}
			return judgement, nil
		}
	}
	return Judgement{}, fmt.Errorf("judge: the biggest issue %q is not one of this occasion's options", answer.Choice)
}
