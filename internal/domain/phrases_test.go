// The phrase tests pin the domain's display wording: the template
// grammar RenderPhrase and the loader share, the fix-in-words rule the
// memory sentences keep, and the loader's refusals for a malformed
// table - every one a wiring bug the load names, never papers over.

package domain

import (
	"strings"
	"testing"
)

// TestRenderPhrase pins the template grammar: a placeholder names the
// attribute whose winner it embeds, an attribute that says nothing
// vanishes with its placeholder, and the words re-join on single spaces
// so no placeholder leaves a stray gap behind.
func TestRenderPhrase(t *testing.T) {
	t.Parallel()
	answers := map[string]string{
		"jewellery":        "jhumkas",
		"jewellery_colour": "gold",
	}
	resolve := func(attr string) (string, bool) {
		v, ok := answers[attr]
		return v, ok
	}

	said, err := RenderPhrase("{jewellery_colour} jhumkas", resolve)
	if err != nil || said != "gold jhumkas" {
		t.Fatalf("phrase %q (err %v), want %q", said, err, "gold jhumkas")
	}

	// A literal phrase is itself.
	said, err = RenderPhrase("red lip", resolve)
	if err != nil || said != "red lip" {
		t.Fatalf("phrase %q (err %v), want %q", said, err, "red lip")
	}

	// Two placeholders, both answered.
	said, err = RenderPhrase("{jewellery} with {jewellery_colour}", resolve)
	if err != nil || said != "jhumkas with gold" {
		t.Fatalf("phrase %q (err %v), want %q", said, err, "jhumkas with gold")
	}

	// The colour answered nothing: the placeholder vanishes, the gap
	// closes, and the noun stands alone.
	said, err = RenderPhrase("{jewellery_colour} jhumkas", func(string) (string, bool) { return "", false })
	if err != nil || said != "jhumkas" {
		t.Fatalf("phrase %q (err %v), want %q", said, err, "jhumkas")
	}

	// The same, with the placeholder mid-sentence.
	said, err = RenderPhrase("a {jewellery_colour} jhumka set", func(string) (string, bool) { return "", false })
	if err != nil || said != "a jhumka set" {
		t.Fatalf("phrase %q (err %v), want %q", said, err, "a jhumka set")
	}

	for _, broken := range []struct{ name, template, reason string }{
		{"unclosed", "{jewellery_colour jhumkas", "no closing }"},
		{"empty placeholder", "{} jhumkas", "empty {} placeholder"},
		{"stray close", "jhumkas}", "no opening {"},
		{"nested brace", "{jewellery_{colour}}", "braces inside a placeholder"},
	} {
		_, err := RenderPhrase(broken.template, resolve)
		if err == nil {
			t.Fatalf("%s: template %q rendered", broken.name, broken.template)
		}
		if !strings.Contains(err.Error(), broken.reason) {
			t.Fatalf("%s: error %v, want it to name %q", broken.name, err, broken.reason)
		}
	}
}

// TestFixWords pins the fix in words the memory sentences keep: the
// display table's entry when the fix carries one, the id's own name when
// it does not, and the refusal for a fix the format cannot word. The
// display name is the shop entry's wording and never the sentence's: a
// fix with a name and no words keeps its id-derived sentence.
func TestFixWords(t *testing.T) {
	t.Parallel()
	occ := &Occasion{
		ID:   "family-function",
		Name: "Family function",
		Fixes: []Fix{
			{ID: "family-gold-jhumkas"},
			{ID: "family-closed-smile", Display: &FixDisplay{Name: "Closed-mouth smile"}},
			{ID: "family-red-lip", Display: &FixDisplay{Words: "bright red lip"}},
			{ID: "stray-fix"},
		},
	}

	// No entry: the id's own name, prefix cut, hyphens spaced.
	words, err := occ.FixWords(&occ.Fixes[0])
	if err != nil || words != "gold jhumkas" {
		t.Fatalf("words %q (err %v), want %q", words, err, "gold jhumkas")
	}

	// A display name alone does not move the sentence.
	words, err = occ.FixWords(&occ.Fixes[1])
	if err != nil || words != "closed smile" {
		t.Fatalf("words %q (err %v), want %q", words, err, "closed smile")
	}

	// The table's entry wins when it exists.
	words, err = occ.FixWords(&occ.Fixes[2])
	if err != nil || words != "bright red lip" {
		t.Fatalf("words %q (err %v), want the table's %q", words, err, "bright red lip")
	}

	// No entry and no occasion prefix: no words this format can carry.
	if _, err := occ.FixWords(&occ.Fixes[3]); err == nil || !strings.Contains(err.Error(), "stray-fix") {
		t.Fatalf("error %v, want the unwordable fix refused by name", err)
	}
}

// TestTheLoadedPhraseTable pins the shipped data's table: the seeded
// entry reads back, unseeded options fall through to false, and the
// loaded fixes word themselves by the fallback the memory sentences
// already speak.
func TestTheLoadedPhraseTable(t *testing.T) {
	t.Parallel()
	loaded := loadReal(t)

	phrase, ok := loaded.Vocabulary.Phrase("jewellery", "jhumkas")
	if !ok || phrase != "{jewellery_colour} jhumkas" {
		t.Fatalf("phrase %q (found %v), want the seeded template", phrase, ok)
	}
	if _, ok := loaded.Vocabulary.Phrase("jewellery", "studs"); ok {
		t.Fatal("an unseeded option reported a phrase")
	}
	if _, ok := loaded.Vocabulary.Phrase("garment", "festive-top"); ok {
		t.Fatal("an unseeded attribute reported a phrase")
	}

	family := occasion(t, loaded, "family-function")
	jhumkas := fix(t, loaded, "family-function", "family-gold-jhumkas")
	words, err := family.FixWords(&jhumkas)
	if err != nil || words != "gold jhumkas" {
		t.Fatalf("words %q (err %v), want the recorded sentence's %q", words, err, "gold jhumkas")
	}
	red := fix(t, loaded, "family-function", "family-red-lip")
	words, err = family.FixWords(&red)
	if err != nil || words != "red lip" {
		t.Fatalf("words %q (err %v), want the recorded sentence's %q", words, err, "red lip")
	}
	smile := fix(t, loaded, "family-function", "family-closed-smile")
	if smile.Display == nil || smile.Display.Name != "Closed-mouth smile" {
		t.Fatalf("smile display %+v, want the fixture's display name", smile.Display)
	}
	words, err = family.FixWords(&smile)
	if err != nil || words != "closed smile" {
		t.Fatalf("words %q (err %v), want the id-derived %q - the name is the shop entry's, not the sentence's", words, err, "closed smile")
	}
}

// TestTheLoaderRefusesBrokenPhrases runs the phrase table's refusals:
// every malformed table, unknown name, and silent wording is refused at
// load, naming the file. The shipped data loads; each break fails it.
func TestTheLoaderRefusesBrokenPhrases(t *testing.T) {
	t.Parallel()

	t.Run("phrase names an unknown attribute", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editVocabulary(t, dir, func(v *Vocabulary) {
			v.Phrases = map[string]map[string]string{
				"necklace": {"chain": "chain"},
			}
			delete(v.Phrases, "necklace")
			v.Phrases["scarf"] = map[string]string{"silk": "silk scarf"}
		})
		assertFileError(t, dir, "vocabulary.json", `phrase table names attribute "scarf", which the vocabulary does not have`)
	})

	t.Run("phrase offers an option the attribute does not have", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editVocabulary(t, dir, func(v *Vocabulary) {
			v.Phrases["jewellery"] = map[string]string{"choker": "choker"}
		})
		assertFileError(t, dir, "vocabulary.json", `"choker" for attribute "jewellery", which is not one of its options`)
	})

	t.Run("phrase for none is refused", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editVocabulary(t, dir, func(v *Vocabulary) {
			v.Phrases["jewellery"] = map[string]string{"none": "no earring"}
		})
		assertFileError(t, dir, "vocabulary.json", `which the line never speaks`)
	})

	t.Run("blank phrase is refused", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editVocabulary(t, dir, func(v *Vocabulary) {
			v.Phrases["jewellery"] = map[string]string{"jhumkas": "   "}
		})
		assertFileError(t, dir, "vocabulary.json", `phrase for jewellery "jhumkas" is blank`)
	})

	t.Run("unclosed placeholder is refused", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editVocabulary(t, dir, func(v *Vocabulary) {
			v.Phrases["jewellery"] = map[string]string{"jhumkas": "{jewellery_colour jhumkas"}
		})
		assertFileError(t, dir, "vocabulary.json", `no closing }`)
	})

	t.Run("empty placeholder is refused", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editVocabulary(t, dir, func(v *Vocabulary) {
			v.Phrases["jewellery"] = map[string]string{"jhumkas": "{} jhumkas"}
		})
		assertFileError(t, dir, "vocabulary.json", "empty {} placeholder")
	})

	t.Run("stray close brace is refused", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editVocabulary(t, dir, func(v *Vocabulary) {
			v.Phrases["jewellery"] = map[string]string{"jhumkas": "jhumkas}"}
		})
		assertFileError(t, dir, "vocabulary.json", "no opening {")
	})

	t.Run("self-reference is refused", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editVocabulary(t, dir, func(v *Vocabulary) {
			v.Phrases["jewellery"] = map[string]string{"jhumkas": "{jewellery} jhumkas"}
		})
		assertFileError(t, dir, "vocabulary.json", "references its own attribute")
	})

	t.Run("unknown reference is refused", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editVocabulary(t, dir, func(v *Vocabulary) {
			v.Phrases["jewellery"] = map[string]string{"jhumkas": "{scarf_colour} jhumkas"}
		})
		assertFileError(t, dir, "vocabulary.json", `references attribute "scarf_colour", which the vocabulary does not have`)
	})

	t.Run("a phrase of pure placeholders is refused", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editVocabulary(t, dir, func(v *Vocabulary) {
			// The shipped seed stays in the table beside the broken
			// entry: the refusal must name the placeholder-only template
			// and leave the one with literal text alone.
			v.Phrases["jewellery"] = map[string]string{
				"jhumkas": "{jewellery_colour} jhumkas",
				"studs":   "{jewellery_colour}",
			}
		})
		assertFileError(t, dir, "vocabulary.json", `phrase for jewellery "studs" is all placeholders`)
	})

	t.Run("a phrase with literal text keeps loading", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editVocabulary(t, dir, func(v *Vocabulary) {
			// A placeholder plus literal words is exactly the shape the
			// shipped seed has: it loads, the pure-placeholder refusal
			// does not bite a template the line can always speak.
			v.Phrases["necklace"] = map[string]string{"chain": "{necklace_colour} chain"}
		})
		if _, err := Load(dir); err != nil {
			t.Fatalf("load: %v", err)
		}
	})

	t.Run("display that names nothing is refused", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "family-function.json", func(o *Occasion) {
			fixPtr(t, o, "family-closed-smile").Display = &FixDisplay{}
		})
		assertFileError(t, dir, "family-function.json", `has a display that names nothing`)
	})

	t.Run("two fixes reading the same words are refused", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "family-function.json", func(o *Occasion) {
			fixPtr(t, o, "family-red-lip").Display = &FixDisplay{Words: "gold jhumkas"}
		})
		assertFileError(t, dir, "family-function.json", `both read "gold jhumkas" - the memory could not tell the sentences apart`)
	})

	t.Run("an entry colliding with an id's words is refused", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "date.json", func(o *Occasion) {
			fixPtr(t, o, "date-nude-lip").Display = &FixDisplay{Words: "open collar"}
		})
		assertFileError(t, dir, "date.json", `both read "open collar" - the memory could not tell the sentences apart`)
	})
}
