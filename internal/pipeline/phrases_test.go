// The LookLine tests pin the phrase table's composition: the fixture's
// English derived from the data, the determinism the same-look-same-line
// rule needs, the spoken-for rule that keeps a colour from standing in
// the line twice, and the refusals that name a broken table.

package pipeline

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nrynss/dresscode/internal/domain"
)

// fixtureAttributes is the shown render's check attributes exactly as
// testdata/api/look.json records them: the jhumkas and their colour,
// nothing else answered with more than none.
var fixtureAttributes = []domain.AnsweredAttribute{
	{ID: "jewellery", Value: "jhumkas"},
	{ID: "jewellery_colour", Value: "gold"},
}

// TestTheLookLineDerivesTheFixtureEnglish pins the phrase table against
// the API fixture's own byte value. The fixture's words - "gold
// jhumkas", the colour composed onto the item - was the string no
// mechanical join could derive (T3.5's flagged finding); the table's
// jhumkas phrase now derives it, and this is the proof, on the loaded
// data, with no entry but the seed.
func TestTheLookLineDerivesTheFixtureEnglish(t *testing.T) {
	t.Parallel()
	vocab := loadVocabulary(t)

	line, err := LookLine(vocab, fixtureAttributes)
	if err != nil {
		t.Fatalf("look line: %v", err)
	}
	if line != "gold jhumkas" {
		t.Fatalf("line %q, want the fixture's %q", line, "gold jhumkas")
	}

	// A look that says nothing - no winners, or none and can't tell -
	// is the contract's empty line.
	empty, err := LookLine(vocab, nil)
	if err != nil {
		t.Fatalf("empty look line: %v", err)
	}
	if empty != "" {
		t.Fatalf("line %q, want the empty line", empty)
	}
	silent := []domain.AnsweredAttribute{
		{ID: "jewellery", Value: "none"},
		{ID: "collar", Value: "can't tell"},
	}
	silentLine, err := LookLine(vocab, silent)
	if err != nil {
		t.Fatalf("silent look line: %v", err)
	}
	if silentLine != "" {
		t.Fatalf("line %q, want the empty line", silentLine)
	}
}

// TestTheLookLineIsDeterministic runs one answer set through the line
// twice, and in two different orders, and demands byte-identical lines:
// the line follows the vocabulary's order, never the caller's.
func TestTheLookLineIsDeterministic(t *testing.T) {
	t.Parallel()
	vocab := loadVocabulary(t)
	answers := []domain.AnsweredAttribute{
		{ID: "jewellery", Value: "jhumkas"},
		{ID: "jewellery_colour", Value: "gold"},
		{ID: "lip_colour", Value: "red"},
		{ID: "collar", Value: "mandarin"},
		{ID: "hair", Value: "tied-back"},
		{ID: "hair_colour", Value: "black"},
		{ID: "expression", Value: "flat"},
		{ID: "garment", Value: "festive-top"},
	}
	first, err := LookLine(vocab, answers)
	if err != nil {
		t.Fatalf("look line: %v", err)
	}
	second, err := LookLine(vocab, answers)
	if err != nil {
		t.Fatalf("look line: %v", err)
	}
	if first != second {
		t.Fatalf("identical answers joined into two lines: %q and %q", first, second)
	}

	// The same winners, in reverse: the line must not follow the order
	// the answers arrived in.
	reversed := make([]domain.AnsweredAttribute, len(answers))
	for i, a := range answers {
		reversed[len(answers)-1-i] = a
	}
	third, err := LookLine(vocab, reversed)
	if err != nil {
		t.Fatalf("look line: %v", err)
	}
	if third != first {
		t.Fatalf("reversed answers gave %q, want %q", third, first)
	}
}

// TestTheLookLineSpeaksTheTable pins the table's rules on the loaded
// data and on synthetic tables that generalise them.
func TestTheLookLineSpeaksTheTable(t *testing.T) {
	t.Parallel()
	vocab := loadVocabulary(t)

	t.Run("the embedded colour does not also stand alone", func(t *testing.T) {
		line, err := LookLine(vocab, fixtureAttributes)
		if err != nil {
			t.Fatalf("look line: %v", err)
		}
		if line != "gold jhumkas" {
			t.Fatalf("line %q, want the colour spoken inside the phrase only", line)
		}
	})

	t.Run("an orphan colour keeps its bare label", func(t *testing.T) {
		// The flash drift case T3.5 recorded: the item reads none, the
		// colour answers alone. Nothing embeds it, so nothing consumes
		// it - the line describes the picture it was given.
		line, err := LookLine(vocab, []domain.AnsweredAttribute{{ID: "jewellery_colour", Value: "gold"}})
		if err != nil {
			t.Fatalf("look line: %v", err)
		}
		if line != "gold" {
			t.Fatalf("line %q, want the bare colour", line)
		}
	})

	t.Run("a placeholder whose attribute says nothing vanishes", func(t *testing.T) {
		// The colour is none: not answered here, because none and
		// can't tell never reach the winners. The jhumkas phrase loses
		// its placeholder and keeps its noun.
		line, err := LookLine(vocab, []domain.AnsweredAttribute{{ID: "jewellery", Value: "jhumkas"}})
		if err != nil {
			t.Fatalf("look line: %v", err)
		}
		if line != "jhumkas" {
			t.Fatalf("line %q, want the bare item", line)
		}
	})

	t.Run("options without entries keep their bare labels", func(t *testing.T) {
		line, err := LookLine(vocab, []domain.AnsweredAttribute{
			{ID: "lip_colour", Value: "red"},
			{ID: "collar", Value: "mandarin"},
		})
		if err != nil {
			t.Fatalf("look line: %v", err)
		}
		if line != "red, mandarin" {
			t.Fatalf("line %q, want the bare-label join", line)
		}
	})

	t.Run("a synthetic table composes the same way", func(t *testing.T) {
		// The seed carries one phrase; the mechanism is general. A
		// table that words the necklace the way the jhumkas are worded
		// must behave exactly like the loaded one, including a
		// reference to an attribute that sits later in the vocabulary.
		synth := &domain.Vocabulary{
			Attributes: []domain.LookAttribute{
				{ID: "necklace", Options: []string{"none", "chain", "can't tell"}},
				{ID: "necklace_colour", Options: []string{"none", "gold", "silver", "can't tell"}},
			},
			Phrases: map[string]map[string]string{
				"necklace": {"chain": "{necklace_colour} chain"},
			},
		}
		line, err := LookLine(synth, []domain.AnsweredAttribute{
			{ID: "necklace", Value: "chain"},
			{ID: "necklace_colour", Value: "silver"},
		})
		if err != nil {
			t.Fatalf("look line: %v", err)
		}
		if line != "silver chain" {
			t.Fatalf("line %q, want the colour spoken inside the phrase only", line)
		}
		uncoloured, err := LookLine(synth, []domain.AnsweredAttribute{{ID: "necklace", Value: "chain"}})
		if err != nil {
			t.Fatalf("look line: %v", err)
		}
		if uncoloured != "chain" {
			t.Fatalf("line %q, want the bare item", uncoloured)
		}
	})
}

// TestTheLookLineRefuses pins the refusals: a nil vocabulary, a winner
// off its attribute's list, and a malformed phrase - the last reachable
// only through a vocabulary that never went through domain.Load, which
// refuses it at load; composed here it fails loudly, never silently.
func TestTheLookLineRefuses(t *testing.T) {
	t.Parallel()
	vocab := loadVocabulary(t)

	if _, err := LookLine(nil, fixtureAttributes); err == nil {
		t.Fatal("a nil vocabulary composed a line")
	}

	if _, err := LookLine(vocab, []domain.AnsweredAttribute{{ID: "jewellery", Value: "choker"}}); err == nil {
		t.Fatal("an off-list winner composed a line")
	} else if want := "not one of the attribute's options"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error %v, want it to name %q", err, want)
	}

	broken := &domain.Vocabulary{
		Attributes: []domain.LookAttribute{
			{ID: "lip_colour", Options: []string{"none", "red", "can't tell"}},
		},
		Phrases: map[string]map[string]string{
			"lip_colour": {"red": "{unclosed lip"},
		},
	}
	if _, err := LookLine(broken, []domain.AnsweredAttribute{{ID: "lip_colour", Value: "red"}}); err == nil {
		t.Fatal("a malformed phrase composed a line")
	} else if want := "no closing }"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error %v, want it to name %q", err, want)
	}
}

// TestTheDemoAnswersAreTheFixtureAttributes keeps the fixture pin honest:
// the attributes the fixture records are the ones this file composes
// with, so a fixture edit that changes the demo's answers shows up here.
func TestTheDemoAnswersAreTheFixtureAttributes(t *testing.T) {
	t.Parallel()
	vocab := loadVocabulary(t)
	offered := make(map[string]map[string]bool, len(vocab.Attributes))
	for _, attr := range vocab.Attributes {
		offered[attr.ID] = map[string]bool{}
		for _, o := range attr.Options {
			offered[attr.ID][o] = true
		}
	}
	for _, a := range fixtureAttributes {
		if !offered[a.ID][a.Value] {
			t.Fatalf("fixture attribute %s answered %q, which the vocabulary does not offer", a.ID, a.Value)
		}
	}
	if !reflect.DeepEqual(fixtureAttributes, []domain.AnsweredAttribute{
		{ID: "jewellery", Value: "jhumkas"},
		{ID: "jewellery_colour", Value: "gold"},
	}) {
		t.Fatalf("fixture attributes %+v", fixtureAttributes)
	}
}
