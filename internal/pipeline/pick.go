package pipeline

// Pick is this file: one Clef request that chooses a fix, on text only.
// Judge hands it the flags; measure (T3.3) and render (T3.4) consume its
// choice. No probability leaves this file: they rank the fixes here and
// die here, before any user or any downstream stage sees one.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/systemone"
)

// pickQuestionID is the question id of the pick's one choice question.
const pickQuestionID = "fix"

// noneID is the id of the option that declines every fix. It is the id
// the occasions' data already uses for nothing-is-wrong on the
// biggest-issue question, and the loader guarantees every occasion's
// biggest-issue options carry it.
const noneID = "none"

// pickQuestion is the one sentence this file authors. Everything else in
// the request is the data's own wording, copied verbatim: the criteria's
// suggestions, the fixes' changes, and the data's label for none. The
// occasions' data has no field for a pick question, so the connective
// sentence lives here, named and in one place. A change to it invalidates
// pick recordings only, and none exist yet (docs/clef.md recorded no
// pick call).
const pickQuestion = "Which one fix should be applied to this look? Choose none if no option fits."

// Recall is the pick's view of the account's style memory, the shape the
// API's recall answers with: the sentence in the user's own history, and
// the fix ids it names. It is not the Lambo hit list (memory.Recall);
// T3.8 reads Lambo and builds this. A nil *Recall is history off, and
// then nothing of the memory reaches the request, because there is no
// argument to carry it in.
type Recall struct {
	// Line is the memory sentence word for word, the one the second
	// visit shows. Empty when the account has no memory of this occasion.
	Line string
	// Kept names the fixes the user kept, by id.
	Kept []string
	// Dismissed names the fixes the user dismissed, by id.
	Dismissed []string
}

// empty reports whether the recall carries nothing at all: no sentence
// and no fix ids. An empty recall is no recall in the request, so an
// account with no memory of the occasion asks like a first visit.
func (r *Recall) empty() bool {
	return r == nil || (r.Line == "" && len(r.Kept) == 0 && len(r.Dismissed) == 0)
}

// Choice is the pick's output: the chosen fix and the runner-up, each
// the occasion's own menu entry carried whole, so measure reads which
// analysis the fix needs and render reads its YouCam task without a
// second lookup. Both are nil when no fix is chosen: the model answered
// none, or there was nothing to ask. A look with no fix stands on its
// suggestions, so a nil Fix is an outcome, not a failure. RunnerUp is
// nil when there is no second fix to fall back on.
//
// Because the fields point into the candidate set, a Choice in memory
// is a Choice from the menu: no off-menu fix value can be represented
// in it.
type Choice struct {
	Fix      *domain.Fix
	RunnerUp *domain.Fix
}

// pickState is the whole state of a pick request. The request is built
// from this struct, so nothing else can reach Clef in the state: a
// photo has no field, an analysis value outside domain.Allowlist has no
// field, and a probability has no field to travel in. The images of the
// request are left nil: Pick takes no image argument, so there is
// nothing to put there.
type pickState struct {
	Occasion string `json:"occasion"`
	Name     string `json:"name"`
	// Flags are the criteria the look missed, in the judgement's order,
	// worded as the data words them for the user. The judgement's
	// biggest issue is not sent: product.md's table lists flagged
	// criteria for this step, and the flags already carry every
	// criterion the issue could name.
	Flags []flagState `json:"flags"`
	// Attributes are the allowlisted analysis values the cached
	// analysis already holds, keyed by the allowlist's own JSON names.
	// A value the analysis has not produced is absent, never invented.
	Attributes map[string]string `json:"attributes,omitempty"`
	// Recall is the account's memory of this occasion, word for word,
	// when history is on. Absent when history is off or the memory is
	// empty.
	Recall *recallState `json:"recall,omitempty"`
}

// flagState is one flagged criterion as the pick request carries it: the
// id and the suggestion the data gives the user. Both are copied from
// the domain.Criterion the judgement carried; nothing is composed.
type flagState struct {
	ID         string `json:"id"`
	Suggestion string `json:"suggestion"`
}

// recallState is the memory as the request carries it: the sentence and
// the fix ids, word for word from the caller's Recall. The dismissed ids
// travel here so the model knows the preference in its own words, but
// the enforcement of a dismissal is code, not wording: candidates
// excludes a dismissed fix before the request exists.
type recallState struct {
	Line      string   `json:"line,omitempty"`
	Kept      []string `json:"kept,omitempty"`
	Dismissed []string `json:"dismissed,omitempty"`
}

// Pick is the pipeline's choice of fix. One Clef request carries the
// flagged criteria, the occasion, the user's opt-in menu narrowed to the
// fixes still open, the allowlisted attributes the cached analysis
// holds, and the recall when history is on. It never carries the photo:
// the function takes no image, and the request it builds has no image
// field set.
//
// The candidate set is the menu, not a filter on the answer. A fix can
// come back only by being in it: the request offers exactly the ticked
// fixes minus the dismissed ones, and an answer naming anything else is
// refused. A dismissed fix is never offered at all - not first and not
// runner-up (the doc comment on candidates says why).
//
// Two asks are refused before any call, because Clef answers a vacuous
// question confidently (docs/clef.md measured that on an imageless
// judge): nothing is flagged, so nothing needs fixing; or nothing is a
// candidate, because the user ticked nothing or dismissed everything
// they ticked. Both return an empty Choice with no error and no request;
// the look then stands on its suggestions.
func Pick(ctx context.Context, client systemone.SystemOne, occ *domain.Occasion, j Judgement, optin []string, attrs domain.Allowlist, recall *Recall) (Choice, error) {
	if occ == nil {
		return Choice{}, errors.New("pick: no occasion")
	}
	if len(j.Flags) == 0 {
		return Choice{}, nil
	}
	menu := candidates(occ, optin, recall)
	if len(menu) == 0 {
		return Choice{}, nil
	}
	req, err := pickRequest(occ, j, attrs, recall, menu)
	if err != nil {
		return Choice{}, err
	}
	resp, err := client.Ask(ctx, req)
	if err != nil {
		return Choice{}, fmt.Errorf("pick: %w", err)
	}
	return pickRead(menu, resp)
}

// candidates narrows the occasion's menu to the user's ticks and drops
// every dismissed fix. The order is the menu's own, which is the
// tie-break for the runner-up.
//
// A dismissed fix is out entirely: not offered first, not offered as
// runner-up. product.md's second-visit moment says Dresscode "skips the
// red lip they dismissed", and the critical-path story in the dev-diary
// says "offers the jhumkas first, never the red lip". The fallback chain
// renders the runner-up whenever the first check fails, so a dismissal
// that only barred first place would put the dismissed fix on the
// user's face one hop later - the exact thing they said no to. api.md
// states the minimum ("a dismissed fix is not offered first"); the story
// states this stronger rule, and the stronger rule is the one built
// here. The exclusion happens before the request, so the model is never
// offered the dismissed fix; read refuses the id anyway, so a model
// echoing the state's dismissed list cannot get it returned either.
//
// A ticked or dismissed id that names no fix on this occasion is a
// no-op: it cannot enter or remove a candidate, so stale memory ids are
// tolerated rather than fatal.
func candidates(occ *domain.Occasion, optin []string, recall *Recall) []domain.Fix {
	ticked := make(map[string]struct{}, len(optin))
	for _, id := range optin {
		ticked[id] = struct{}{}
	}
	dismissed := map[string]struct{}{}
	if recall != nil {
		for _, id := range recall.Dismissed {
			dismissed[id] = struct{}{}
		}
	}
	out := make([]domain.Fix, 0, len(occ.Fixes))
	for _, fix := range occ.Fixes {
		if _, ok := ticked[fix.ID]; !ok {
			continue
		}
		if _, ok := dismissed[fix.ID]; ok {
			continue
		}
		out = append(out, fix)
	}
	return out
}

// request builds the one Clef request. Every byte of it is traceable:
// the state is pickState, the one question offers the candidates in the
// data's own changes wording plus the data's own label for none, and
// the images field stays nil because nothing in this file's arguments
// could fill it.
func pickRequest(occ *domain.Occasion, j Judgement, attrs domain.Allowlist, recall *Recall, menu []domain.Fix) (systemone.Request, error) {
	state := pickState{
		Occasion:   occ.ID,
		Name:       occ.Name,
		Flags:      make([]flagState, len(j.Flags)),
		Attributes: held(attrs),
	}
	for i, flag := range j.Flags {
		state.Flags[i] = flagState{ID: flag.ID, Suggestion: flag.Suggestion}
	}
	if !recall.empty() {
		state.Recall = &recallState{
			Line:      recall.Line,
			Kept:      recall.Kept,
			Dismissed: recall.Dismissed,
		}
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return systemone.Request{}, fmt.Errorf("pick: %w", err)
	}
	options := make(map[string]string, len(menu)+1)
	for _, fix := range menu {
		options[fix.ID] = fix.Changes
	}
	none, ok := noneLabel(occ)
	if !ok {
		return systemone.Request{}, fmt.Errorf("pick: occasion %q has no %q option to decline a fix with", occ.ID, noneID)
	}
	options[noneID] = none
	choice, err := json.Marshal(options)
	if err != nil {
		return systemone.Request{}, fmt.Errorf("pick: %w", err)
	}
	req := systemone.Request{
		Model: systemone.ModelClef,
		State: encoded,
		Questions: map[string]systemone.Question{
			pickQuestionID: {
				Type:         systemone.TypeChoice,
				Instructions: pickQuestion,
				Criteria:     choice,
			},
		},
	}
	if err := req.Validate(); err != nil {
		return systemone.Request{}, fmt.Errorf("pick: %w", err)
	}
	return req, nil
}

// held projects the allowlist onto the values the cached analysis
// already holds. The keys come from marshalling domain.Allowlist itself,
// so they are that type's own four JSON names and no fifth key has a
// place to come from; an empty value is an attribute the analysis has
// not produced, and it is left out rather than invented. Nothing held
// gives a nil map, which the state omits.
func held(attrs domain.Allowlist) map[string]string {
	raw, err := json.Marshal(attrs)
	if err != nil {
		// domain.Allowlist is four string fields; it cannot fail to
		// marshal. Refusing here would invent an error path the type
		// cannot take.
		return nil
	}
	var all map[string]string
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil
	}
	for key, value := range all {
		if value == "" {
			delete(all, key)
		}
	}
	return all
}

// noneLabel is the data's own label for the none option, taken from the
// occasion's biggest-issue wording so the decline option is worded by
// the data, not composed here.
func noneLabel(occ *domain.Occasion) (string, bool) {
	for _, o := range occ.BiggestIssue.Options {
		if o.ID == noneID {
			return o.Label, true
		}
	}
	return "", false
}

// read turns the answers into the choice. The chosen id must be one of
// the candidates or none; anything else - an unticked fix, a dismissed
// one, an id that was never on this occasion's menu - is refused, so
// Choice returns a menu entry or an error and never an off-menu fix. The
// runner-up is the best-probability candidate under the model's own
// answer, menu order breaking ties; the probabilities rank this one
// decision and go no further.
func pickRead(menu []domain.Fix, resp systemone.Response) (Choice, error) {
	answer, ok := resp.Answers[pickQuestionID]
	if !ok {
		return Choice{}, errors.New("pick: the model did not answer the pick")
	}
	if answer.Type != systemone.TypeChoice || answer.Choice == "" {
		return Choice{}, errors.New("pick: the pick is not a choice")
	}
	if answer.Choice == noneID {
		return Choice{}, nil
	}
	chosen := -1
	for i, fix := range menu {
		if fix.ID == answer.Choice {
			chosen = i
			break
		}
	}
	if chosen < 0 {
		return Choice{}, fmt.Errorf("pick: the chosen fix %q is not one of the offered options", answer.Choice)
	}
	fix := menu[chosen]
	out := Choice{Fix: &fix}
	runner, runnerP := -1, 0.0
	for i, other := range menu {
		if i == chosen {
			continue
		}
		p, ok := answer.Probabilities[other.ID]
		if !ok {
			continue
		}
		if runner < 0 || p > runnerP {
			runner, runnerP = i, p
		}
	}
	if runner >= 0 {
		second := menu[runner]
		out.RunnerUp = &second
	}
	return out, nil
}
