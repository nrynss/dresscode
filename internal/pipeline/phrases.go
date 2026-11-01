// Phrases are this file: the look-in-words line, composed from the
// occasions data's display-phrase table. The check reads the winners off
// the render; this file turns them into the line. It is a pure function
// of the answer set and the loaded table - no model, no score, no id,
// no number - so the same look always says the same words, wherever the
// line is composed.

package pipeline

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nrynss/dresscode/internal/domain"
)

// LookLine composes the look-in-words line from one answer set: each
// attribute's winning option, in the vocabulary's own order, none and
// can't tell left out, joined with ", ". The winners are the data's
// option labels; a winner the vocabulary does not offer is refused by
// name, not passed on.
//
// The wording is the vocabulary's phrase table (testdata/occasions/
// vocabulary.json): an option with a display phrase speaks it, an option
// without one speaks its bare label, the join the line has always made.
// A phrase may carry `{attribute}` placeholders that embed another
// attribute's winner from the same answer set - the jewellery option
// `jhumkas` speaks "{jewellery_colour} jhumkas", which reads "gold
// jhumkas". A placeholder whose attribute says nothing in this answer
// set - none, can't tell, or unanswered - vanishes, so the same phrase
// reads "jhumkas" when the colour is absent.
//
// An attribute whose winner a phrase embeds is spoken for: it does not
// also stand in the line on its own, or the same fact would appear twice
// ("gold jhumkas, gold"). A colour whose item said nothing is embedded by
// nothing, and keeps its bare label - the line describes the picture it
// was given, never more.
//
// Errors name the wiring that broke: a nil vocabulary, an off-list
// winner, or a malformed phrase - the last can only reach this function
// through a vocabulary that never went through domain.Load, which
// validates every template at load.
func LookLine(vocab *domain.Vocabulary, attrs []domain.AnsweredAttribute) (string, error) {
	if vocab == nil {
		return "", errors.New("words: no vocabulary")
	}
	answered := make(map[string]string, len(attrs))
	for _, a := range attrs {
		answered[a.ID] = a.Value
	}
	for _, attr := range vocab.Attributes {
		value, ok := answered[attr.ID]
		if !ok || silentOption(value) {
			continue
		}
		if !offers(attr.Options, value) {
			return "", fmt.Errorf("words: %s answered %q, which is not one of the attribute's options", attr.ID, value)
		}
	}

	// Spoken-for: the attributes whose winners the phrases embed. The
	// render's own resolve records every placeholder that answered with
	// a real winner, so consumption follows the templates the table
	// actually renders, in whatever order the attributes sit.
	spoken := make(map[string]bool, len(attrs))
	for _, attr := range vocab.Attributes {
		value, ok := answered[attr.ID]
		if !ok || silentOption(value) {
			continue
		}
		template, ok := vocab.Phrase(attr.ID, value)
		if !ok {
			continue
		}
		if _, err := domain.RenderPhrase(template, func(ref string) (string, bool) {
			winner, ok := answered[ref]
			if !ok || silentOption(winner) {
				return "", false
			}
			spoken[ref] = true
			return winner, true
		}); err != nil {
			return "", fmt.Errorf("words: phrase for %s %q: %w", attr.ID, value, err)
		}
	}

	line := make([]string, 0, len(attrs))
	for _, attr := range vocab.Attributes {
		value, ok := answered[attr.ID]
		if !ok || silentOption(value) || spoken[attr.ID] {
			continue
		}
		if template, ok := vocab.Phrase(attr.ID, value); ok {
			said, err := domain.RenderPhrase(template, func(ref string) (string, bool) {
				winner, ok := answered[ref]
				return winner, ok && !silentOption(winner)
			})
			if err != nil {
				return "", fmt.Errorf("words: phrase for %s %q: %w", attr.ID, value, err)
			}
			value = said
		}
		line = append(line, value)
	}
	return strings.Join(line, ", "), nil
}

// silentOption reports whether an answer says nothing about the look:
// none and can't tell are left out of the attributes and of the line,
// as the API contract states.
func silentOption(value string) bool {
	return value == noneOption || value == cantTellOption
}
