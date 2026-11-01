// Memory is this file: the pipeline's style-memory stage. With history
// on, a look leaves two traces in the account's Lambo session. Before a
// fix is picked, the stage recalls for the occasion and hands the pick
// (T3.2) the sentence and the fix ids; after the user keeps, dismisses
// or buys, the stage remembers the verdict as one short sentence per
// fact. With history off the wiring calls neither half, and nothing is
// read or written.
//
// The stage is two functions, not a run stage. The contract's stage list
// is frozen (docs/api.md: judge, pick, measure, render, check, shop), so
// the write publishes no stream event and the read adds none: the recall
// travels to the phone on the pick frame's own `recall` field, which the
// run already carries when the request holds it, and the write's outcome
// surfaces on the verdict response's `remembered` field, which the
// contract already defines (api.md: true only when history is on and the
// memory write landed; a memory failure still returns 200). The run
// itself never touches Lambo: it receives the built recall in the
// request and passes it to Pick untouched, and the snapshot round-trips
// it through a restart, so a resumed look costs no Lambo call and a
// Lambo fault cannot fail a resume. The call sites are the wiring that
// starts a look and the wiring that stores a verdict, both the server's.
//
// The text is the product's plain voice, and it is the one the probe
// recorded and the fixtures speak: the occasion's name, the verdict, and
// the fix in words - "Family function kept the gold jhumkas." The words
// for a fix come from the occasions data's display table when the fix
// carries an entry, and from its id's own name otherwise
// (family-gold-jhumkas keeps "gold jhumkas"), so the sentence carries no
// id, no score and no analysis value, and the same fix always reads the
// same way. docs/lambo.md measured what recall returns: the nearest
// concepts of the session, unfiltered by occasion, their text verbatim,
// a score that ranks but does not match. So the recall half reads the
// concept text - the format is this file's, written and parsed by the
// same functions - and keeps only the facts of the occasion asked
// about.

package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/memory"
)

// Fact is one verdict the memory keeps: the fix the verdict is about,
// and the verdict itself. The occasion travels beside the facts in
// Remember, once, because every fact of a write is on one occasion.
// The fix the verdict is about is the one the look ended on - the shown
// fix when a render was shown, else the picked fix, the rule the shop
// stage states - because the verdict is the user's answer to what they
// saw.
type Fact struct {
	Fix     *domain.Fix
	Verdict domain.Verdict
}

// Remember writes the verdicts into the account's session, one concept
// per fact, in one Lambo write. The verdict handler calls it with the
// one verdict that just landed; a write that carries several facts sends
// them together, which is the shape the recorded exchange holds. The
// call returns only what the memory client confirms: a derive is not a
// write until its receipt reports applied (docs/lambo.md), and the
// client waits on that receipt before it returns - a nil error here is a
// confirmed write, and any error is a look that was not remembered. The
// verdict is already stored when this runs; the caller answers the
// contract's `remembered` with err == nil and the look stands either
// way. Memory is auxiliary: nothing here fails a look.
//
// Refused before any call: no client, no session, no occasion, no fact,
// a fact without a fix, an empty or unknown verdict, and a fix whose id
// does not carry its occasion's prefix - the last is a naming the data
// broke, named loudly rather than remembered wrongly.
func Remember(ctx context.Context, client memory.Memory, session string, occ *domain.Occasion, facts ...Fact) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if client == nil {
		return errors.New("memory: no memory client")
	}
	if session == "" {
		return errors.New("memory: no session")
	}
	if occ == nil {
		return errors.New("memory: no occasion")
	}
	if len(facts) == 0 {
		return errors.New("memory: no verdict to remember")
	}
	concepts := make([]memory.Concept, 0, len(facts))
	for _, fact := range facts {
		concept, err := factConcept(occ, fact)
		if err != nil {
			return err
		}
		concepts = append(concepts, concept)
	}
	if _, err := client.Remember(ctx, session, concepts); err != nil {
		return fmt.Errorf("memory: remember %s: %w", occ.ID, err)
	}
	return nil
}

// RecallFor asks the account's session what it remembers of one
// occasion, and builds the recall the pick takes (pipeline.Recall): the
// sentence the second visit shows, and the fix ids the sentence names.
// The look-start wiring calls it when history is on and puts the result
// in the request; on an error it starts the look with no recall and the
// pick never sees one - a failed recall reads as a first visit, never as
// a failed look. An erased session comes back typed (memory.ErrErased):
// it is not an empty history, and the wiring, not this file, decides
// what carrying on without memory means.
//
// The query is the occasion's name in lower case - the words a concept
// carries, so the session's nearest concepts include the occasion's own
// facts. The session returns whatever is nearest; it does not filter to
// the occasion (docs/lambo.md), so every hit is read and only the facts
// this file's own format names, on this occasion, are kept. A fact whose
// words no fix of the occasion carries - a fix renamed or gone - is left
// out of the sentence and the ids alike, so the two always agree. A fix
// both kept and dismissed across visits reads as dismissed: the cost of
// wrongly offering again is the thing the user said no to, and the cost
// of wrongly hiding is one quieter menu. A bought fix counts as kept for
// the pick's ranking, and the sentence says bought.
//
// An account with no memory of the occasion - no parseable fact - gets
// an empty, non-nil recall: the pick asks like a first visit, and the
// recall route answers the contract's empty line and empty ids.
func RecallFor(ctx context.Context, client memory.Memory, session string, occ *domain.Occasion) (*Recall, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("memory: no memory client")
	}
	if session == "" {
		return nil, errors.New("memory: no session")
	}
	if occ == nil {
		return nil, errors.New("memory: no occasion")
	}
	got, err := client.Recall(ctx, session, strings.ToLower(occ.Name))
	if err != nil {
		return nil, fmt.Errorf("memory: recall for %s: %w", occ.ID, err)
	}

	// One verdict per fix, the strongest winning. The scan order of the
	// hits is the session's rank, not time, so a winner is a rule about
	// verdicts, never about arrival.
	rank := map[domain.Verdict]int{
		domain.VerdictKept:      1,
		domain.VerdictBought:    2,
		domain.VerdictDismissed: 3,
	}
	byFix := map[string]domain.Verdict{}
	for _, hit := range got.Hits {
		fact, ok := parseFact(occ, hit.Content)
		if !ok {
			continue
		}
		prev, seen := byFix[fact.Fix.ID]
		if !seen || rank[fact.Verdict] > rank[prev] {
			byFix[fact.Fix.ID] = fact.Verdict
		}
	}

	var kept, bought, dismissed []string
	for i := range occ.Fixes {
		verdict, ok := byFix[occ.Fixes[i].ID]
		if !ok {
			continue
		}
		switch verdict {
		case domain.VerdictKept:
			kept = append(kept, occ.Fixes[i].ID)
		case domain.VerdictBought:
			bought = append(bought, occ.Fixes[i].ID)
		case domain.VerdictDismissed:
			dismissed = append(dismissed, occ.Fixes[i].ID)
		}
	}
	out := &Recall{
		Line:      memoryLine(occ, kept, bought, dismissed),
		Kept:      append(kept, bought...),
		Dismissed: dismissed,
	}
	return out, nil
}

// fixWords is the fix in words, through the occasions data's display
// table: the fix's own display words when one is recorded, else the
// id-derived name the table's fallback produces - the occasion's prefix
// cut and the hyphens spaced, family-gold-jhumkas reading "gold
// jhumkas". The wording the user reviews lives in the data (domain's
// FixWords); this wrapper only keeps the stage's own name on the error.
func fixWords(occ *domain.Occasion, fix *domain.Fix) (string, error) {
	words, err := occ.FixWords(fix)
	if err != nil {
		return "", fmt.Errorf("memory: %w", err)
	}
	return words, nil
}

// factConcept composes one concept for one verdict. The sentence is the
// probe's and the fixtures': "Family function kept the gold jhumkas."
// The occasion's name leads it so a later query on the name finds the
// fact (docs/lambo.md: put the occasion in the text), the verdict is the
// stored word for what the user did, and the fix is named in words, not
// ids. A look verdict is an observation (docs/lambo.md).
func factConcept(occ *domain.Occasion, fact Fact) (memory.Concept, error) {
	if occ == nil {
		return memory.Concept{}, errors.New("memory: no occasion")
	}
	if fact.Fix == nil {
		return memory.Concept{}, errors.New("memory: a verdict names no fix")
	}
	if fact.Verdict == "" || !fact.Verdict.Valid() {
		return memory.Concept{}, fmt.Errorf("memory: verdict %q is not one the memory keeps", string(fact.Verdict))
	}
	words, err := fixWords(occ, fact.Fix)
	if err != nil {
		return memory.Concept{}, err
	}
	content := occ.Name + " " + string(fact.Verdict) + " the " + words + "."
	return memory.Concept{Content: content, ConceptType: memory.ConceptObservation}, nil
}

// parseFact reads one hit's text as this file's own format and returns
// the fact it names on this occasion. Anything else - another
// occasion's fact, another sentence, a fix the occasion no longer
// carries - is not a fact here, and false is the answer. The words must
// name a fix exactly: no prefix match, no score, no guessing. The same
// table that words the sentences parses them, so a display-words entry
// and its fallback always invert.
func parseFact(occ *domain.Occasion, content string) (Fact, bool) {
	if occ == nil {
		return Fact{}, false
	}
	rest, ok := strings.CutPrefix(content, occ.Name+" ")
	if !ok {
		return Fact{}, false
	}
	verdict, tail, ok := strings.Cut(rest, " the ")
	if !ok {
		return Fact{}, false
	}
	words := strings.TrimSuffix(tail, ".")
	if words == "" || strings.ContainsAny(words, "\n") {
		return Fact{}, false
	}
	v := domain.Verdict(verdict)
	if v == "" || !v.Valid() {
		return Fact{}, false
	}
	for i := range occ.Fixes {
		fixWords, err := occ.FixWords(&occ.Fixes[i])
		if err != nil || fixWords != words {
			continue
		}
		return Fact{Fix: &occ.Fixes[i], Verdict: v}, true
	}
	return Fact{}, false
}

// memoryLine composes the sentence the second visit shows, from facts
// already mapped to this occasion's fixes: kept, then bought, then
// dismissed, each in the occasion's own menu order. It is the API
// contract's sentence - "Last family function you kept the gold jhumkas
// and dismissed the red lip." - and it is a pure function of the
// verdicts: the session returns its hits in rank order, the sentence
// does not follow it. No facts, no sentence: the empty line is the
// contract's empty-memory answer.
func memoryLine(occ *domain.Occasion, kept, bought, dismissed []string) string {
	var clauses []string
	for _, class := range []struct {
		verb string
		ids  []string
	}{
		{"kept", kept},
		{"bought", bought},
		{"dismissed", dismissed},
	} {
		if len(class.ids) == 0 {
			continue
		}
		words := make([]string, 0, len(class.ids))
		for _, id := range class.ids {
			fix := fixByID(occ, id)
			if fix == nil {
				// The ids came from facts already parsed against this
				// occasion; every one names a fix. This branch cannot run.
				continue
			}
			w, err := occ.FixWords(fix)
			if err != nil {
				// A fix the table and its id cannot word was refused
				// before any fact carried it. This branch cannot run.
				continue
			}
			words = append(words, "the "+w)
		}
		if len(words) == 0 {
			continue
		}
		clauses = append(clauses, class.verb+" "+strings.Join(words, " and "))
	}
	if len(clauses) == 0 {
		return ""
	}
	return "Last " + strings.ToLower(occ.Name) + " you " + strings.Join(clauses, " and ") + "."
}

// fixByID finds one of the occasion's fixes by id.
func fixByID(occ *domain.Occasion, id string) *domain.Fix {
	for i := range occ.Fixes {
		if occ.Fixes[i].ID == id {
			return &occ.Fixes[i]
		}
	}
	return nil
}
