package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestLoadOccasions(t *testing.T) {
	t.Parallel()
	loaded := loadReal(t)

	got := map[string]string{}
	for _, occ := range loaded.Occasions {
		got[occ.ID] = occ.Name
	}
	want := map[string]string{
		"date":            "Date",
		"family-function": "Family function",
		"festival":        "Festival",
		"funeral":         "Funeral",
		"interview":       "Interview",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("occasions %#v, want %#v", got, want)
	}

	fixes := 0
	for _, occ := range loaded.Occasions {
		for _, fix := range occ.Fixes {
			fixes++
			if fix.YouCam.Task == "" {
				t.Fatalf("%s fix %s has no YouCam task", occ.ID, fix.ID)
			}
			if len(fix.Criteria) == 0 {
				t.Fatalf("%s fix %s addresses no criteria", occ.ID, fix.ID)
			}
			if len(fix.Vocabulary) == 0 {
				t.Fatalf("%s fix %s has no vocabulary change", occ.ID, fix.ID)
			}
		}
	}
	if fixes != 28 {
		t.Fatalf("%d fixes, want 28", fixes)
	}

	if loaded.Vocabulary.ID != "look" {
		t.Fatalf("vocabulary id %q", loaded.Vocabulary.ID)
	}
	attrIDs := make([]string, 0, len(loaded.Vocabulary.Attributes))
	for _, attr := range loaded.Vocabulary.Attributes {
		attrIDs = append(attrIDs, attr.ID)
		if !contains(attr.Options, "none") || !contains(attr.Options, "can't tell") {
			t.Fatalf("attribute %s options %v", attr.ID, attr.Options)
		}
	}
	wantAttrs := []string{
		"jewellery", "jewellery_colour", "necklace", "necklace_colour",
		"lip_colour", "collar", "hair", "hair_colour", "beard", "expression", "garment",
	}
	if !reflect.DeepEqual(attrIDs, wantAttrs) {
		t.Fatalf("attributes %v", attrIDs)
	}

	interview := criterion(t, loaded, "interview", "lip-quiet")
	funeral := criterion(t, loaded, "funeral", "lip-quiet")
	if interview != funeral {
		t.Fatalf("lip-quiet diverged\n interview %#v\n funeral %#v", interview, funeral)
	}

	jhumkas := fix(t, loaded, "family-function", "family-gold-jhumkas")
	if !reflect.DeepEqual(jhumkas.Vocabulary, []VocabularyChange{
		{Attribute: "jewellery", To: "jhumkas"},
		{Attribute: "jewellery_colour", To: "gold"},
	}) {
		t.Fatalf("jhumkas vocabulary %#v", jhumkas.Vocabulary)
	}
	red := fix(t, loaded, "family-function", "family-red-lip")
	if !reflect.DeepEqual(red.Vocabulary, []VocabularyChange{{Attribute: "lip_colour", To: "red"}}) {
		t.Fatalf("family red lip %#v", red.Vocabulary)
	}
	if fix(t, loaded, "festival", "festival-red-lip").ID == red.ID {
		t.Fatal("festival red lip shares the family fix id")
	}

	for _, occ := range loaded.Occasions {
		for _, f := range occ.Fixes {
			switch f.YouCam.Task {
			case "hair-transfer", "beard-style":
				if f.Template == "" {
					t.Fatalf("%s fix %s has no template", occ.ID, f.ID)
				}
			default:
				if f.Template != "" {
					t.Fatalf("%s fix %s has template %q on %s", occ.ID, f.ID, f.Template, f.YouCam.Task)
				}
			}
		}
	}
	for _, tc := range []struct {
		occasion, fix, template string
		change                  VocabularyChange
	}{
		{"date", "date-tie-hair", "female_casual_updo", VocabularyChange{Attribute: "hair", To: "bun"}},
		{"funeral", "funeral-short-beard", "all_circle", VocabularyChange{Attribute: "beard", To: "goatee"}},
	} {
		f := fix(t, loaded, tc.occasion, tc.fix)
		if f.Template != tc.template {
			t.Fatalf("%s template %q, want %q", tc.fix, f.Template, tc.template)
		}
		if !reflect.DeepEqual(f.Vocabulary, []VocabularyChange{tc.change}) {
			t.Fatalf("%s vocabulary %#v", tc.fix, f.Vocabulary)
		}
	}

	hair := fix(t, loaded, "date", "date-tie-hair")
	if len(hair.Analysis) != 1 || hair.Analysis[0].Task != "face-attr-analysis" {
		t.Fatalf("hair analysis %#v", hair.Analysis)
	}
	if !reflect.DeepEqual(hair.Analysis[0].UseFields, []string{"faceShape"}) {
		t.Fatalf("use fields %v", hair.Analysis[0].UseFields)
	}
	for _, withheld := range []string{"age", "gender", "ratios"} {
		if !contains(hair.Analysis[0].NeverUse, withheld) {
			t.Fatalf("never_use %v missing %s", hair.Analysis[0].NeverUse, withheld)
		}
	}

	if len(loaded.Products) != 8 {
		t.Fatalf("%d products, want 8", len(loaded.Products))
	}
	gold := product(t, loaded, "gold-jhumkas")
	if gold.Fidelity != ExactProduct || gold.Origin != OriginCatalog {
		t.Fatalf("gold-jhumkas %#v", gold)
	}
	lip := product(t, loaded, "lip-red")
	if lip.Fidelity != ApproximateMatch || lip.Origin != OriginCatalog {
		t.Fatalf("lip-red %#v", lip)
	}
	for _, p := range loaded.Products {
		if p.Fidelity != ExactProduct && p.Fidelity != ApproximateMatch {
			t.Fatalf("%s fidelity %q", p.ID, p.Fidelity)
		}
	}
}

func TestBrokenFile(t *testing.T) {
	t.Parallel()

	t.Run("invalid json", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		assertFileError(t, dir, "bad.json", "unexpected end of JSON input")
	})

	t.Run("unknown field", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		path := filepath.Join(dir, "vocabulary.json")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		b = bytes.Replace(b, []byte("{"), []byte(`{"extra":true,`), 1)
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		assertFileError(t, dir, "vocabulary.json", "extra")
	})

	t.Run("fix without youcam task", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "date.json", func(o *Occasion) {
			fixPtr(t, o, "date-open-collar").YouCam.Task = ""
		})
		assertFileError(t, dir, "date.json", "does not name a YouCam task")
	})

	t.Run("fix without criteria", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "date.json", func(o *Occasion) {
			fixPtr(t, o, "date-open-collar").Criteria = nil
		})
		assertFileError(t, dir, "date.json", "does not name the criteria it addresses")
	})

	t.Run("fix addresses an unknown criterion", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "date.json", func(o *Occasion) {
			fixPtr(t, o, "date-nude-lip").Criteria = []string{"not-on-this-occasion"}
		})
		assertFileError(t, dir, "date.json", "not on this occasion")
	})

	t.Run("vocabulary missing none", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editVocabulary(t, dir, func(v *Vocabulary) {
			v.Attributes[0].Options = without(v.Attributes[0].Options, "none")
		})
		assertFileError(t, dir, "vocabulary.json", `must include "none"`)
	})

	t.Run("vocabulary missing can't tell", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editVocabulary(t, dir, func(v *Vocabulary) {
			v.Attributes[0].Options = without(v.Attributes[0].Options, "can't tell")
		})
		assertFileError(t, dir, "vocabulary.json", `must include "can't tell"`)
	})

	t.Run("shared criterion different question", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "interview.json", func(o *Occasion) {
			critPtr(t, o, "lip-quiet").Clef = "Is the lip bright?"
		})
		err := mustFail(t, dir)
		assertFileErrorValue(t, err, "interview.json", "different wording")
		if !strings.Contains(err.Error(), "funeral.json") || !strings.Contains(err.Error(), "lip-quiet") {
			t.Fatal(err)
		}
	})

	t.Run("shared criterion different suggestion", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "interview.json", func(o *Occasion) {
			critPtr(t, o, "lip-quiet").Suggestion = "Try something else."
		})
		err := mustFail(t, dir)
		assertFileErrorValue(t, err, "interview.json", "different wording")
		if !strings.Contains(err.Error(), "funeral.json") {
			t.Fatal(err)
		}
	})

	t.Run("same question under a different id", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		funeral := readOccasion(t, dir, "funeral.json")
		var collar string
		for _, c := range funeral.Criteria {
			if c.ID == "funeral-collar-plain" {
				collar = c.Clef
			}
		}
		if collar == "" {
			t.Fatal("funeral collar question missing")
		}
		editOccasion(t, dir, "interview.json", func(o *Occasion) {
			critPtr(t, o, "interview-collar-buttoned").Clef = collar
		})
		err := mustFail(t, dir)
		assertFileErrorValue(t, err, "interview.json", "repeats the wording")
		if !strings.Contains(err.Error(), "funeral.json") {
			t.Fatal(err)
		}
	})

	t.Run("analysis field off the allowlist", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "date.json", func(o *Occasion) {
			fixPtr(t, o, "date-tie-hair").Analysis[0].UseFields = []string{"age"}
		})
		assertFileError(t, dir, "date.json", "not on the picker allowlist")
	})

	t.Run("face analysis must withhold age", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "date.json", func(o *Occasion) {
			fixPtr(t, o, "date-tie-hair").Analysis[0].NeverUse = []string{"gender", "ratios"}
		})
		assertFileError(t, dir, "date.json", "must list age in never_use")
	})

	t.Run("analysis field used and withheld", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "date.json", func(o *Occasion) {
			a := &fixPtr(t, o, "date-tie-hair").Analysis[0]
			a.NeverUse = append(a.NeverUse, "faceShape")
		})
		assertFileError(t, dir, "date.json", "both use_fields and never_use")
	})

	t.Run("fix not opt-in", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "date.json", func(o *Occasion) {
			fixPtr(t, o, "date-open-collar").OptIn = false
		})
		assertFileError(t, dir, "date.json", "is not opt-in")
	})

	t.Run("youcam set not qualifying", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "date.json", func(o *Occasion) {
			fixPtr(t, o, "date-closed-smile").YouCam.Set = "Creators"
		})
		assertFileError(t, dir, "date.json", "Creators")
	})

	t.Run("fix id none is reserved", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "date.json", func(o *Occasion) {
			fixPtr(t, o, "date-open-collar").ID = "none"
		})
		assertFileError(t, dir, "date.json", `fix id "none" is reserved`)
	})

	t.Run("fix id cant-tell is reserved", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "date.json", func(o *Occasion) {
			fixPtr(t, o, "date-open-collar").ID = "cant-tell"
		})
		assertFileError(t, dir, "date.json", `fix id "cant-tell" is reserved`)
	})

	t.Run("duplicate fix id", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "festival.json", func(o *Occasion) {
			fixPtr(t, o, "festival-red-lip").ID = "family-red-lip"
		})
		err := mustFail(t, dir)
		assertFileErrorValue(t, err, "festival.json", "already in family-function.json")
	})

	t.Run("unknown fidelity", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		path := filepath.Join(dir, "catalog.json")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		old := []byte(`"fidelity": "exact"`)
		if !bytes.Contains(b, old) {
			t.Fatal("catalog has no exact fidelity")
		}
		b = bytes.Replace(b, old, []byte(`"fidelity": "bogus"`), 1)
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		assertFileError(t, dir, "catalog.json", "unknown fidelity")
	})

	t.Run("fix without a vocabulary change", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "funeral.json", func(o *Occasion) {
			fixPtr(t, o, "funeral-short-beard").Vocabulary = nil
		})
		assertFileError(t, dir, "funeral.json", "no vocabulary change")
	})

	t.Run("hair fix without a template", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "date.json", func(o *Occasion) {
			fixPtr(t, o, "date-tie-hair").Template = ""
		})
		assertFileError(t, dir, "date.json", `fix "date-tie-hair" YouCam task "hair-transfer" has no template`)
	})

	t.Run("beard fix without a template", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "funeral.json", func(o *Occasion) {
			fixPtr(t, o, "funeral-short-beard").Template = ""
		})
		assertFileError(t, dir, "funeral.json", `fix "funeral-short-beard" YouCam task "beard-style" has no template`)
	})

	t.Run("blank template", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "interview.json", func(o *Occasion) {
			fixPtr(t, o, "interview-short-hair").Template = "  "
		})
		assertFileError(t, dir, "interview.json", `fix "interview-short-hair" YouCam task "hair-transfer" has no template`)
	})

	t.Run("template on a task that takes none", func(t *testing.T) {
		t.Parallel()
		dir := copyOccasions(t)
		editOccasion(t, dir, "date.json", func(o *Occasion) {
			fixPtr(t, o, "date-open-collar").Template = "all_circle"
		})
		assertFileError(t, dir, "date.json", `fix "date-open-collar" YouCam task "cloth-v4" takes no template`)
	})
}

func TestFidelityLabels(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{`"exact"`, "exact product"},
		{`"approximate"`, "approximate match"},
		{`"exact product"`, "exact product"},
		{`"approximate match"`, "approximate match"},
		{`"visual simulation"`, "visual simulation"},
	}
	for _, tc := range cases {
		var got Fidelity
		if err := json.Unmarshal([]byte(tc.in), &got); err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if string(got) != tc.want {
			t.Fatalf("%s decoded %q, want %q", tc.in, got, tc.want)
		}
		b, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != `"`+tc.want+`"` {
			t.Fatalf("marshal %s, want %q", b, tc.want)
		}
	}
	for _, bad := range []string{`"exactness"`, `"simulation"`, `""`, `"visual"`} {
		var got Fidelity
		if err := json.Unmarshal([]byte(bad), &got); err == nil {
			t.Fatalf("%s was accepted as %q", bad, got)
		}
	}
}

func TestJSONRoundTrip(t *testing.T) {
	t.Parallel()
	loaded := loadReal(t)

	date := occasion(t, loaded, "date")
	roundTrip(t, date)
	roundTrip(t, date.Criteria[0])
	roundTrip(t, date.Fixes[0])
	for _, attr := range loaded.Vocabulary.Attributes {
		roundTrip(t, attr)
	}
	roundTrip(t, loaded.Vocabulary)
	for _, p := range loaded.Products {
		roundTrip(t, p)
	}

	roundTrip(t, Allowlist{
		FaceShape:   "oval",
		HairType:    "wavy",
		HairLength:  "long",
		ColourTones: "warm",
	})
	roundTrip(t, Look{
		ID:       "look-1",
		SelfieID: "selfie-1",
		Occasion: "family-function",
		Flags:    []string{"family-earring-festive", "family-lip-colour"},
		Fix:      "family-gold-jhumkas",
		RunnerUp: "family-red-lip",
		Renders: []Render{
			{
				MediaID: "media-1",
				FixID:   "family-gold-jhumkas",
				Check: &Check{
					ChangeMade:   true,
					SamePerson:   true,
					CriterionMet: true,
					Attributes: []AnsweredAttribute{
						{ID: "jewellery", Value: "jhumkas"},
						{ID: "jewellery_colour", Value: "gold"},
					},
				},
			},
			{MediaID: "media-2", FixID: "family-red-lip"},
		},
		Products: []Product{
			{ID: "gold-jhumkas", Category: "earrings", Name: "Gold jhumkas", Fidelity: ExactProduct, Origin: OriginCatalog},
			{ID: "lip-red", Category: "lip-colour", Name: "Red lip colour", Fidelity: ApproximateMatch, Origin: OriginCatalog},
			{ID: "tied-hair", Name: "Tied-back hair", Fidelity: VisualSimulation, Origin: OriginUser},
		},
		Verdict: VerdictKept,
	})
	roundTrip(t, Look{Verdict: ""})
	for _, v := range []Verdict{VerdictKept, VerdictDismissed, VerdictBought} {
		roundTrip(t, v)
	}
}

func TestAllowlistRejectsAnythingElse(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeOf(Allowlist{})
	if typ.NumField() != 4 {
		t.Fatalf("%d fields", typ.NumField())
	}
	want := []struct{ name, tag string }{
		{"FaceShape", "face_shape"},
		{"HairType", "hair_type"},
		{"HairLength", "hair_length"},
		{"ColourTones", "colour_tones"},
	}
	for i, w := range want {
		f := typ.Field(i)
		if f.Name != w.name || string(f.Tag) != `json:"`+w.tag+`"` {
			t.Fatalf("field %d is %s %s", i, f.Name, f.Tag)
		}
		if f.Type.Kind() != reflect.String {
			t.Fatalf("%s has type %s", f.Name, f.Type)
		}
	}

	for _, raw := range []string{
		`{"photo":"pixels"}`,
		`{"face_shape":"oval","photo":"pixels"}`,
		`{"face_shape":"oval","age":"30"}`,
		`{"face_shape":"oval","gender":"x"}`,
	} {
		var got Allowlist
		if err := json.Unmarshal([]byte(raw), &got); err == nil {
			t.Fatalf("%s was accepted as %#v", raw, got)
		}
	}

	allowed := map[PickerAttribute]struct{}{
		AttrFaceShape:   {},
		AttrHairType:    {},
		AttrHairLength:  {},
		AttrColourTones: {},
	}
	if youcamField["faceShape"] != AttrFaceShape {
		t.Fatalf("faceShape maps to %q", youcamField["faceShape"])
	}
	for field, attr := range youcamField {
		if _, ok := allowed[attr]; !ok {
			t.Fatalf("YouCam field %s maps to %s, which is not allowlisted", field, attr)
		}
		switch field {
		case "age", "gender", "ratios":
			t.Fatalf("forbidden field %s is mapped", field)
		}
	}
}

func TestVerdictRejectsUnknown(t *testing.T) {
	t.Parallel()
	var v Verdict
	if err := json.Unmarshal([]byte(`"score"`), &v); err == nil {
		t.Fatalf("accepted %q", v)
	}
}

func roundTrip(t *testing.T, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %#v: %v", v, err)
	}
	gotPtr := reflect.New(reflect.TypeOf(v))
	if err := json.Unmarshal(b, gotPtr.Interface()); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	got := gotPtr.Elem().Interface()
	if !reflect.DeepEqual(v, got) {
		t.Fatalf("round trip\n got %#v\nwant %#v\njson %s", got, v, b)
	}
	b2, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("remarshal: %v", err)
	}
	if !bytes.Equal(b, b2) {
		t.Fatalf("json\n got %s\nwant %s", b2, b)
	}
}

func loadReal(t *testing.T) Loaded {
	t.Helper()
	loaded, err := Load(occasionsDir(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return loaded
}

func occasionsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "occasions")
}

func copyOccasions(t *testing.T) string {
	t.Helper()
	src := occasionsDir(t)
	dst := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

func editOccasion(t *testing.T, dir, name string, edit func(*Occasion)) {
	t.Helper()
	occ := readOccasion(t, dir, name)
	edit(&occ)
	writeJSON(t, filepath.Join(dir, name), occ)
}

func readOccasion(t *testing.T, dir, name string) Occasion {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	var occ Occasion
	if err := json.Unmarshal(b, &occ); err != nil {
		t.Fatal(err)
	}
	return occ
}

func editVocabulary(t *testing.T, dir string, edit func(*Vocabulary)) {
	t.Helper()
	path := filepath.Join(dir, "vocabulary.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v Vocabulary
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	edit(&v)
	writeJSON(t, path, v)
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustFail(t *testing.T, dir string) error {
	t.Helper()
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected an error")
	}
	return err
}

func assertFileError(t *testing.T, dir, file, reason string) {
	t.Helper()
	assertFileErrorValue(t, mustFail(t, dir), file, reason)
}

func assertFileErrorValue(t *testing.T, err error, file, reason string) {
	t.Helper()
	var fe *FileError
	if !errors.As(err, &fe) {
		t.Fatalf("got %T (%v), want *FileError", err, err)
	}
	if fe.File != file {
		t.Fatalf("file %q, want %q (%v)", fe.File, file, err)
	}
	if !strings.Contains(fe.Reason, reason) {
		t.Fatalf("reason %q does not contain %q", fe.Reason, reason)
	}
	if !strings.Contains(err.Error(), file) || !strings.Contains(err.Error(), reason) {
		t.Fatalf("error %q does not carry the file and the reason", err)
	}
}

func occasion(t *testing.T, loaded Loaded, id string) Occasion {
	t.Helper()
	for _, occ := range loaded.Occasions {
		if occ.ID == id {
			return occ
		}
	}
	t.Fatalf("occasion %s not loaded", id)
	return Occasion{}
}

func criterion(t *testing.T, loaded Loaded, occasionID, id string) Criterion {
	t.Helper()
	occ := occasion(t, loaded, occasionID)
	for _, c := range occ.Criteria {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("criterion %s/%s not loaded", occasionID, id)
	return Criterion{}
}

func fix(t *testing.T, loaded Loaded, occasionID, id string) Fix {
	t.Helper()
	occ := occasion(t, loaded, occasionID)
	for _, f := range occ.Fixes {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("fix %s/%s not loaded", occasionID, id)
	return Fix{}
}

func product(t *testing.T, loaded Loaded, id string) Product {
	t.Helper()
	for _, p := range loaded.Products {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("product %s not loaded", id)
	return Product{}
}

func fixPtr(t *testing.T, o *Occasion, id string) *Fix {
	t.Helper()
	for i := range o.Fixes {
		if o.Fixes[i].ID == id {
			return &o.Fixes[i]
		}
	}
	t.Fatalf("fix %s missing", id)
	return nil
}

func critPtr(t *testing.T, o *Occasion, id string) *Criterion {
	t.Helper()
	for i := range o.Criteria {
		if o.Criteria[i].ID == id {
			return &o.Criteria[i]
		}
	}
	t.Fatalf("criterion %s missing", id)
	return nil
}

func without(options []string, drop string) []string {
	var out []string
	for _, opt := range options {
		if opt != drop {
			out = append(out, opt)
		}
	}
	return out
}
