// Phrases are this file: the display wording the occasions data carries,
// and the two readings that turn it into English. One is a phrase
// template - an option's display words, which may embed another
// attribute's winner ("{jewellery_colour} jhumkas" reads "gold
// jhumkas"). The other is a fix's words for the memory sentences - the
// table's entry when one exists, the id's own name when it does not.
// Both are pure functions of data already loaded; neither reads a model,
// a score, or an identifier on a user's screen.

package domain

import (
	"fmt"
	"strings"
)

// Phrase returns the display phrase the vocabulary carries for one
// option of one attribute, and whether it carries one. An option with no
// entry is spoken as its own bare label; the caller falls back.
func (v *Vocabulary) Phrase(attr, option string) (string, bool) {
	if v.Phrases == nil {
		return "", false
	}
	byOption, ok := v.Phrases[attr]
	if !ok {
		return "", false
	}
	phrase, ok := byOption[option]
	return phrase, ok
}

// phrasePart is one piece of a parsed template: a literal run of text,
// or a placeholder naming the attribute whose winner it embeds.
type phrasePart struct {
	// Text is the literal text, when this part is not a placeholder.
	Text string
	// Ref is the referenced attribute id, when this part is a
	// placeholder. Text is empty then.
	Ref string
}

// parsePhrase splits a template into literals and references. The
// grammar: `{` opens a placeholder, `}` closes it, the content between
// them is an attribute id, and every other character is literal. An
// unclosed `{`, an empty or brace-bearing placeholder, and a stray `}`
// with no opener are malformed - the loader refuses them at load, so a
// template that loads always parses here.
func parsePhrase(template string) ([]phrasePart, error) {
	var parts []phrasePart
	var literal strings.Builder
	for i := 0; i < len(template); {
		c := template[i]
		switch c {
		case '{':
			end := strings.IndexByte(template[i:], '}')
			if end < 0 {
				return nil, fmt.Errorf("template %q has a { with no closing }", template)
			}
			ref := template[i+1 : i+end]
			if ref == "" {
				return nil, fmt.Errorf("template %q has an empty {} placeholder", template)
			}
			if strings.ContainsAny(ref, "{}") {
				return nil, fmt.Errorf("template %q has braces inside a placeholder", template)
			}
			if literal.Len() > 0 {
				parts = append(parts, phrasePart{Text: literal.String()})
				literal.Reset()
			}
			parts = append(parts, phrasePart{Ref: ref})
			i += end + 1
		case '}':
			return nil, fmt.Errorf("template %q has a } with no opening {", template)
		default:
			literal.WriteByte(c)
			i++
		}
	}
	if literal.Len() > 0 {
		parts = append(parts, phrasePart{Text: literal.String()})
	}
	return parts, nil
}

// RenderPhrase renders one display phrase template. The template's
// `{attribute}` placeholders name the attribute whose winner the phrase
// embeds; resolve answers for one attribute with the winner's own label,
// and false when the attribute says nothing in this answer set - none,
// can't tell, or not answered. An unresolved placeholder vanishes and
// the remaining words re-join on single spaces, so "{jewellery_colour}
// jhumkas" with no colour reads "jhumkas", never " jhumkas" or "none
// jhumkas". The placeholders speak the winner's own label; they do not
// recurse into the referenced option's own phrase.
func RenderPhrase(template string, resolve func(attribute string) (string, bool)) (string, error) {
	parts, err := parsePhrase(template)
	if err != nil {
		return "", err
	}
	var words []string
	for _, part := range parts {
		if part.Ref == "" {
			words = append(words, strings.Fields(part.Text)...)
			continue
		}
		value, ok := resolve(part.Ref)
		if !ok {
			continue
		}
		words = append(words, strings.Fields(value)...)
	}
	return strings.Join(words, " "), nil
}

// FixWords is the fix in words the memory sentences keep: the display
// table's entry when the fix carries one, else the id's own name with
// the occasion's prefix cut and the hyphens spaced - family-gold-jhumkas
// on family-function reads "gold jhumkas". The occasion's prefix is its
// id's first dash segment, the convention the shipped data gives every
// fix. A fix with no entry whose id carries no occasion prefix has no
// words this format can carry, and the error names it: the sentence
// never carries an id.
func (o *Occasion) FixWords(f *Fix) (string, error) {
	if f.Display != nil && f.Display.Words != "" {
		return f.Display.Words, nil
	}
	prefix, _, _ := strings.Cut(o.ID, "-")
	words, ok := strings.CutPrefix(f.ID, prefix+"-")
	if !ok || words == "" {
		return "", fmt.Errorf("fix %q carries no %q prefix to word it by", f.ID, prefix)
	}
	return strings.ReplaceAll(words, "-", " "), nil
}
