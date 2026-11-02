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
//
// A criterion id here is a criterion this look's own Flags hold: read
// gates the model's choice on those flags, the rule gateIssue states. An
// ID naming a criterion the flags do not carry never arrives, and on a
// look with nothing flagged Issue.ID is the occasion's own none.
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
// alone, and the judge does not import the YouCam client.
//
// A criterion with no photo to judge cannot be asked at all: Clef answers
// a judge request that lost its image, and the answer is not one to trust
// (docs/clef.md), so an empty photo is refused before any call.
//
// The flags come back in the occasion's criteria order. Their wording is
// the occasion's, so the app renders a suggestion and never a score. The
// biggest issue is the model's choice gated on those flags: gateIssue
// states the rule and why a suppressed choice is not an error.
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
// the model's own choice, gated on those flags by gateIssue: what reaches
// the app is the choice only where this look's flags support it.
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
			// The read site, and the one thing a later reader must not
			// infer from it: where this line hands back none or
			// can't tell in place of a criterion, the service did NOT
			// say so - it named a criterion this look does not flag, and
			// gateIssue suppressed that misattribution. The flags are the
			// truth and the choice is a hint; a suppression is not an
			// error, and nothing here records which one it was.
			gated, err := gateIssue(occ, judgement.Flags, o)
			if err != nil {
				return Judgement{}, err
			}
			judgement.Issue = gated
			return judgement, nil
		}
	}
	return Judgement{}, fmt.Errorf("judge: the biggest issue %q is not one of this occasion's options", answer.Choice)
}

// gateIssue is the biggest issue's gate: the model's choice is reported
// when this look's flags support it, and suppressed when they do not.
//
// It keeps the choice in two cases. The first is a criterion the look
// actually flags: the app has a suggestion to hang on it, and the pick
// downstream reads the same flags. The second is an option that names no
// criterion at all, which on this data means the occasion's own none and
// cant-tell (internal/domain/load.go's validateBiggest asks for the criteria plus
// those two and refuses any other set). Neither is a claim about a
// criterion, so neither can contradict the flags: a model that will not
// pick has said so, and that says something the app should show. The gate
// suppresses a misattributed criterion, never uncertainty.
//
// It suppresses in the case the corpus actually hits: a choice naming a
// specific criterion this look does not flag. A user whose look meets the
// dress code would otherwise be told its beard is the biggest issue, with
// nothing flagged and no fix to offer for it - the re-recorded
// testdata/clef/judge-V09-interview.json answers interview-beard-short
// while that criterion's own reading sits above its line, so the flags are
// empty and the choice names a criterion none of them is. The choice and
// the flags are two reads of one probability set, and the flags are the
// one the thresholds set and the pick and the suggestions follow; the
// choice that contradicts them is the misattribution. So it is reported
// as the occasion's own none, which is what the user is being told: this
// look has nothing to single out.
//
// The none is the occasion's own option, id and label together, never a
// string composed here - the same way pick.go takes its decline label
// from the data (noneLabel). An occasion whose options carried no none is
// an error rather than a silent fallback: with no none to report, keeping
// the misattributed criterion would tell the user something the flags
// refuse, and the loader guarantees none is there, so this cannot be
// reached by any occasion that loads.
func gateIssue(occ *domain.Occasion, flags []domain.Criterion, chosen domain.IssueOption) (Issue, error) {
	if !namesCriterion(occ, chosen.ID) || namesFlagged(flags, chosen.ID) {
		return Issue{ID: chosen.ID, Label: chosen.Label}, nil
	}
	for _, o := range occ.BiggestIssue.Options {
		if o.ID == noneID {
			return Issue{ID: o.ID, Label: o.Label}, nil
		}
	}
	return Issue{}, fmt.Errorf("judge: occasion %q has no %q option to report a suppressed issue with", occ.ID, noneID)
}

// namesCriterion reports whether an option id is one of the occasion's
// criteria. The two options that are not - none and can't tell - are the
// ones the gate lets through whatever the flags say: they claim nothing
// about a criterion, so they cannot misattribute one. Their ids are not
// re-tested here. An id reaches this function only after the model's answer
// matched one of this occasion's own options, and the loader refuses an
// occasion whose options are anything other than its criteria plus that
// pair, so a criterion can never be named none or can't tell - and a wire
// value that is not one of the options is refused before the gate is
// reached, not passed to it.
func namesCriterion(occ *domain.Occasion, id string) bool {
	for _, c := range occ.Criteria {
		if c.ID == id {
			return true
		}
	}
	return false
}

// namesFlagged reports whether id is among the criteria this look flags.
func namesFlagged(flags []domain.Criterion, id string) bool {
	for _, f := range flags {
		if f.ID == id {
			return true
		}
	}
	return false
}
