package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	optionNone     = "none"
	optionCantTell = "can't tell"
	issueCantTell  = "cant-tell"
)

// occasionIDs is the product's closed set of dress codes.
var occasionIDs = []string{
	"date",
	"family-function",
	"festival",
	"funeral",
	"interview",
}

// FileError is a load that stopped on one file.
// File is the base name. Reason is safe to show: it never includes a key.
type FileError struct {
	File   string
	Reason string
}

func (e *FileError) Error() string {
	return e.File + ": " + e.Reason
}

func fail(file, format string, args ...any) error {
	return &FileError{File: file, Reason: fmt.Sprintf(format, args...)}
}

// Loaded is the occasions, the look vocabulary, and the catalog.
type Loaded struct {
	Occasions  []Occasion
	Vocabulary Vocabulary
	Products   []Product
}

type loadedOccasion struct {
	file string
	occ  Occasion
}

type catalogFile struct {
	Kind     string    `json:"kind"`
	Products []Product `json:"products"`
}

// Load reads an occasions directory. It returns the occasions, the look
// vocabulary, and the catalog. A file that does not parse, or that breaks
// a menu rule, returns a FileError naming that file.
func Load(dir string) (Loaded, error) {
	names, err := jsonFiles(dir)
	if err != nil {
		return Loaded{}, err
	}
	var (
		occasions []loadedOccasion
		vocab     *Vocabulary
		products  []Product
		catalogOK bool
	)
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return Loaded{}, fail(name, "%s", err.Error())
		}
		kind, err := peekKind(name, data)
		if err != nil {
			return Loaded{}, err
		}
		switch kind {
		case "occasion":
			if name == "vocabulary.json" || name == "catalog.json" {
				return Loaded{}, fail(name, "kind %q does not belong in this file", kind)
			}
			occ, err := decode[Occasion](name, data)
			if err != nil {
				return Loaded{}, err
			}
			if err := validateOccasion(name, &occ); err != nil {
				return Loaded{}, err
			}
			occasions = append(occasions, loadedOccasion{file: name, occ: occ})
		case "vocabulary":
			if name != "vocabulary.json" {
				return Loaded{}, fail(name, "vocabulary belongs in vocabulary.json")
			}
			if vocab != nil {
				return Loaded{}, fail(name, "duplicate vocabulary")
			}
			v, err := decode[Vocabulary](name, data)
			if err != nil {
				return Loaded{}, err
			}
			if err := validateVocabulary(name, &v); err != nil {
				return Loaded{}, err
			}
			vocab = &v
		case "catalog":
			if name != "catalog.json" {
				return Loaded{}, fail(name, "catalog belongs in catalog.json")
			}
			if catalogOK {
				return Loaded{}, fail(name, "duplicate catalog")
			}
			cat, err := decode[catalogFile](name, data)
			if err != nil {
				return Loaded{}, err
			}
			if err := validateCatalog(name, &cat); err != nil {
				return Loaded{}, err
			}
			products = cat.Products
			catalogOK = true
		default:
			return Loaded{}, fail(name, "unknown kind %q", kind)
		}
	}
	if err := requireOccasions(occasions); err != nil {
		return Loaded{}, err
	}
	if vocab == nil {
		return Loaded{}, fail("vocabulary.json", "file is missing")
	}
	if !catalogOK {
		return Loaded{}, fail("catalog.json", "file is missing")
	}
	if err := checkShared(occasions); err != nil {
		return Loaded{}, err
	}
	if err := checkReferences(occasions, *vocab, products); err != nil {
		return Loaded{}, err
	}
	loaded := Loaded{Vocabulary: *vocab, Products: products}
	for _, lo := range occasions {
		loaded.Occasions = append(loaded.Occasions, lo.occ)
	}
	sort.Slice(loaded.Occasions, func(i, j int) bool {
		return loaded.Occasions[i].ID < loaded.Occasions[j].ID
	})
	return loaded, nil
}

func jsonFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fail(filepath.Base(dir), "%s", err.Error())
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return nil, fail(filepath.Base(dir), "no json files")
	}
	sort.Strings(names)
	return names, nil
}

func peekKind(file string, data []byte) (string, error) {
	var peek map[string]json.RawMessage
	if err := json.Unmarshal(data, &peek); err != nil {
		return "", fail(file, "%s", err.Error())
	}
	raw, ok := peek["kind"]
	if !ok {
		return "", fail(file, "missing kind")
	}
	var kind string
	if err := json.Unmarshal(raw, &kind); err != nil {
		return "", fail(file, "kind is not a string")
	}
	switch kind {
	case "occasion", "vocabulary", "catalog":
		return kind, nil
	default:
		return "", fail(file, "unknown kind %q", kind)
	}
}

func decode[T any](file string, data []byte) (T, error) {
	var zero T
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var v T
	if err := dec.Decode(&v); err != nil {
		return zero, fail(file, "%s", err.Error())
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return zero, fail(file, "trailing data after JSON value")
		}
		return zero, fail(file, "%s", err.Error())
	}
	return v, nil
}

func requireOccasions(occasions []loadedOccasion) error {
	have := map[string]struct{}{}
	for _, lo := range occasions {
		have[lo.occ.ID] = struct{}{}
	}
	for _, id := range occasionIDs {
		if _, ok := have[id]; !ok {
			return fail(id+".json", "file is missing")
		}
	}
	return nil
}

func knownOccasion(id string) bool {
	for _, want := range occasionIDs {
		if id == want {
			return true
		}
	}
	return false
}

func validateOccasion(file string, o *Occasion) error {
	if o.Kind != "occasion" {
		return fail(file, "kind %q, want occasion", o.Kind)
	}
	if blank(o.ID) {
		return fail(file, "occasion has no id")
	}
	if file != o.ID+".json" {
		return fail(file, "occasion id %q does not match the file name", o.ID)
	}
	if !knownOccasion(o.ID) {
		return fail(file, "unknown occasion %q", o.ID)
	}
	if blank(o.Name) {
		return fail(file, "occasion %q has no name", o.ID)
	}
	n := len(o.Criteria)
	if n < 5 || n > 8 {
		return fail(file, "occasion %q has %d criteria, want 5 to 8", o.ID, n)
	}
	ids, err := validateCriteria(file, o.Criteria)
	if err != nil {
		return err
	}
	if err := validateBiggest(file, o.BiggestIssue, ids); err != nil {
		return err
	}
	if len(o.Fixes) == 0 {
		return fail(file, "occasion %q has no fixes", o.ID)
	}
	if err := validateFixes(file, o.Fixes, ids); err != nil {
		return err
	}
	return validateBasket(file, *o)
}

func validateCriteria(file string, criteria []Criterion) (map[string]struct{}, error) {
	ids := make(map[string]struct{}, len(criteria))
	for _, c := range criteria {
		if blank(c.ID) {
			return nil, fail(file, "a criterion is missing an id")
		}
		if c.ID == optionNone || c.ID == issueCantTell {
			return nil, fail(file, "criterion id %q is reserved", c.ID)
		}
		if _, ok := ids[c.ID]; ok {
			return nil, fail(file, "duplicate criterion %q", c.ID)
		}
		ids[c.ID] = struct{}{}
		if !c.Kind.Valid() {
			return nil, fail(file, "criterion %q has kind %q", c.ID, c.Kind)
		}
		if blank(c.Clef) || blank(c.Suggestion) {
			return nil, fail(file, "criterion %q is missing its Clef question or its suggestion", c.ID)
		}
	}
	return ids, nil
}

func validateBiggest(file string, issue BiggestIssue, criteria map[string]struct{}) error {
	if blank(issue.Question) {
		return fail(file, "biggest issue has no question")
	}
	want := make(map[string]struct{}, len(criteria)+2)
	for id := range criteria {
		want[id] = struct{}{}
	}
	want[optionNone] = struct{}{}
	want[issueCantTell] = struct{}{}
	if len(issue.Options) != len(want) {
		return fail(file, "biggest issue has %d options, want the criteria plus none and can't tell", len(issue.Options))
	}
	seen := map[string]struct{}{}
	for _, opt := range issue.Options {
		if _, ok := want[opt.ID]; !ok {
			return fail(file, "biggest issue option %q is not a criterion, none, or can't tell", opt.ID)
		}
		if _, ok := seen[opt.ID]; ok {
			return fail(file, "biggest issue option %q is repeated", opt.ID)
		}
		seen[opt.ID] = struct{}{}
		if blank(opt.Label) {
			return fail(file, "biggest issue option %q has no label", opt.ID)
		}
		switch opt.ID {
		case optionNone:
			if opt.Label != optionNone {
				return fail(file, "biggest issue option %q has label %q", opt.ID, opt.Label)
			}
		case issueCantTell:
			if opt.Label != optionCantTell {
				return fail(file, "biggest issue option %q has label %q", opt.ID, opt.Label)
			}
		}
	}
	return nil
}

func validateFixes(file string, fixes []Fix, criteria map[string]struct{}) error {
	seen := map[string]struct{}{}
	for _, fix := range fixes {
		if blank(fix.ID) {
			return fail(file, "a fix is missing an id")
		}
		if _, ok := seen[fix.ID]; ok {
			return fail(file, "duplicate fix %q", fix.ID)
		}
		seen[fix.ID] = struct{}{}
		if !fix.OptIn {
			return fail(file, "fix %q is not opt-in", fix.ID)
		}
		if blank(fix.Changes) {
			return fail(file, "fix %q does not say what it changes", fix.ID)
		}
		if err := validateYouCam(file, fix); err != nil {
			return err
		}
		if len(fix.Criteria) == 0 {
			return fail(file, "fix %q does not name the criteria it addresses", fix.ID)
		}
		if err := uniqueIDs(file, "fix "+fix.ID+" criterion", fix.Criteria); err != nil {
			return err
		}
		for _, id := range fix.Criteria {
			if _, ok := criteria[id]; !ok {
				return fail(file, "fix %q addresses criterion %q, which is not on this occasion", fix.ID, id)
			}
		}
		if len(fix.Vocabulary) == 0 {
			return fail(file, "fix %q has no vocabulary change", fix.ID)
		}
		if err := validateVocabChanges(file, fix); err != nil {
			return err
		}
		if err := uniqueIDs(file, "fix "+fix.ID+" product", fix.Products); err != nil {
			return err
		}
		if err := validateAnalysis(file, fix); err != nil {
			return err
		}
	}
	return nil
}

func validateYouCam(file string, fix Fix) error {
	if blank(fix.YouCam.Task) {
		return fail(file, "fix %q does not name a YouCam task", fix.ID)
	}
	if !qualifyingSet(fix.YouCam.Set) {
		return fail(file, "fix %q YouCam set %q is outside Skin, Beauty, Fashion, Jewelry & Watch, and Hair & Beard", fix.ID, fix.YouCam.Set)
	}
	if blank(fix.YouCam.Method) {
		return fail(file, "fix %q YouCam task %q has no method", fix.ID, fix.YouCam.Task)
	}
	if blank(fix.YouCam.Path) {
		return fail(file, "fix %q YouCam task %q has no path", fix.ID, fix.YouCam.Task)
	}
	if !strings.HasSuffix(fix.YouCam.Path, "/"+fix.YouCam.Task) {
		return fail(file, "fix %q YouCam path %q does not end with task %q", fix.ID, fix.YouCam.Path, fix.YouCam.Task)
	}
	return validateTemplate(file, fix)
}

// templateTasks are the YouCam tasks that render from a template id.
var templateTasks = []string{"hair-transfer", "beard-style"}

func validateTemplate(file string, fix Fix) error {
	if contains(templateTasks, fix.YouCam.Task) {
		if blank(fix.Template) {
			return fail(file, "fix %q YouCam task %q has no template", fix.ID, fix.YouCam.Task)
		}
		return nil
	}
	if fix.Template != "" {
		return fail(file, "fix %q YouCam task %q takes no template, got %q", fix.ID, fix.YouCam.Task, fix.Template)
	}
	return nil
}

func qualifyingSet(set string) bool {
	switch set {
	case "Skin", "Beauty", "Fashion", "Jewelry & Watch", "Hair & Beard":
		return true
	default:
		return false
	}
}

func validateVocabChanges(file string, fix Fix) error {
	seen := map[string]struct{}{}
	for _, ch := range fix.Vocabulary {
		if blank(ch.Attribute) || blank(ch.To) {
			return fail(file, "fix %q has a vocabulary change missing an attribute or a value", fix.ID)
		}
		if _, ok := seen[ch.Attribute]; ok {
			return fail(file, "fix %q sets %q twice", fix.ID, ch.Attribute)
		}
		seen[ch.Attribute] = struct{}{}
	}
	return nil
}

func validateAnalysis(file string, fix Fix) error {
	for _, a := range fix.Analysis {
		if blank(a.Task) {
			return fail(file, "fix %q has an analysis with no task", fix.ID)
		}
		if blank(a.Method) || blank(a.Path) {
			return fail(file, "fix %q analysis %q is missing a method or a path", fix.ID, a.Task)
		}
		if !strings.HasSuffix(a.Path, "/"+a.Task) {
			return fail(file, "fix %q analysis path %q does not end with task %q", fix.ID, a.Path, a.Task)
		}
		for _, field := range a.UseFields {
			if _, ok := pickerAttributeForYouCamField(field); !ok {
				return fail(file, "fix %q analysis field %q is not on the picker allowlist", fix.ID, field)
			}
			if contains(a.NeverUse, field) {
				return fail(file, "fix %q lists analysis field %q in both use_fields and never_use", fix.ID, field)
			}
		}
		if a.Task == "face-attr-analysis" {
			for _, withheld := range []string{"age", "gender", "ratios"} {
				if !contains(a.NeverUse, withheld) {
					return fail(file, "fix %q analysis %s must list %s in never_use", fix.ID, a.Task, withheld)
				}
			}
		}
	}
	return nil
}

func validateBasket(file string, o Occasion) error {
	if len(o.Basket.Categories) == 0 || len(o.Basket.Products) == 0 {
		return fail(file, "occasion %q has an empty basket", o.ID)
	}
	if err := uniqueIDs(file, "basket category", o.Basket.Categories); err != nil {
		return err
	}
	if err := uniqueIDs(file, "basket product", o.Basket.Products); err != nil {
		return err
	}
	return uniqueIDs(file, "template-only entry", o.Basket.TemplateOnly)
}

func validateVocabulary(file string, v *Vocabulary) error {
	if v.Kind != "vocabulary" {
		return fail(file, "kind %q, want vocabulary", v.Kind)
	}
	if blank(v.ID) {
		return fail(file, "vocabulary has no id")
	}
	if len(v.Attributes) == 0 {
		return fail(file, "vocabulary has no attributes")
	}
	seen := map[string]struct{}{}
	for _, attr := range v.Attributes {
		if blank(attr.ID) {
			return fail(file, "a vocabulary attribute is missing an id")
		}
		if _, ok := seen[attr.ID]; ok {
			return fail(file, "duplicate attribute %q", attr.ID)
		}
		seen[attr.ID] = struct{}{}
		if blank(attr.Question) {
			return fail(file, "attribute %q has no question", attr.ID)
		}
		if err := validateOptions(file, attr); err != nil {
			return err
		}
	}
	return nil
}

func validateOptions(file string, attr LookAttribute) error {
	seen := map[string]struct{}{}
	hasNone := false
	hasCant := false
	for _, opt := range attr.Options {
		if opt == "" || opt != strings.TrimSpace(opt) {
			return fail(file, "attribute %q has an empty or padded option", attr.ID)
		}
		if _, ok := seen[opt]; ok {
			return fail(file, "attribute %q repeats option %q", attr.ID, opt)
		}
		seen[opt] = struct{}{}
		switch opt {
		case optionNone:
			hasNone = true
		case optionCantTell:
			hasCant = true
		}
	}
	if !hasNone {
		return fail(file, "attribute %q options must include \"none\"", attr.ID)
	}
	if !hasCant {
		return fail(file, "attribute %q options must include \"can't tell\"", attr.ID)
	}
	return nil
}

func validateCatalog(file string, cat *catalogFile) error {
	if cat.Kind != "catalog" {
		return fail(file, "kind %q, want catalog", cat.Kind)
	}
	if len(cat.Products) == 0 {
		return fail(file, "catalog has no products")
	}
	seen := map[string]struct{}{}
	for i := range cat.Products {
		p := &cat.Products[i]
		if blank(p.ID) {
			return fail(file, "a product is missing an id")
		}
		if _, ok := seen[p.ID]; ok {
			return fail(file, "duplicate product %q", p.ID)
		}
		seen[p.ID] = struct{}{}
		if blank(p.Category) || blank(p.Name) {
			return fail(file, "product %q is missing a category or a name", p.ID)
		}
		if !p.Fidelity.Valid() {
			return fail(file, "product %q has fidelity %q, want exact product, approximate match, or visual simulation", p.ID, p.Fidelity)
		}
		if p.Origin != "" && p.Origin != OriginCatalog {
			return fail(file, "product %q has origin %q, want catalog", p.ID, p.Origin)
		}
		if blank(p.YouCamRole) || blank(p.Photograph) {
			return fail(file, "product %q is missing its YouCam role or its photograph note", p.ID)
		}
		p.Origin = OriginCatalog
	}
	return nil
}

// checkShared refuses a criterion id reused with different wording.
// The same id, kind, question, and suggestion may appear on two occasions.
// Interview and funeral share lip-quiet that way. The same Clef question
// under a different id is refused too.
func checkShared(occasions []loadedOccasion) error {
	type wording struct {
		file, id, kind, clef, suggestion string
	}
	byID := map[string]wording{}
	byClef := map[string]wording{}
	for _, lo := range occasions {
		for _, c := range lo.occ.Criteria {
			w := wording{
				file:       lo.file,
				id:         c.ID,
				kind:       string(c.Kind),
				clef:       c.Clef,
				suggestion: c.Suggestion,
			}
			if prev, ok := byID[c.ID]; ok {
				if prev.kind != w.kind || prev.clef != w.clef || prev.suggestion != w.suggestion {
					return fail(lo.file, "criterion %q is shared with %s under different wording", c.ID, prev.file)
				}
			} else {
				byID[c.ID] = w
			}
			if prev, ok := byClef[c.Clef]; ok {
				if prev.id != c.ID || prev.kind != w.kind || prev.suggestion != w.suggestion {
					return fail(lo.file, "criterion %q repeats the wording of %q in %s", c.ID, prev.id, prev.file)
				}
			} else {
				byClef[c.Clef] = w
			}
		}
	}
	return nil
}

func checkReferences(occasions []loadedOccasion, vocab Vocabulary, products []Product) error {
	attrs := make(map[string]LookAttribute, len(vocab.Attributes))
	for _, attr := range vocab.Attributes {
		attrs[attr.ID] = attr
	}
	catalog := make(map[string]Product, len(products))
	for _, p := range products {
		catalog[p.ID] = p
	}
	seenFix := map[string]string{}
	for _, lo := range occasions {
		basket := map[string]struct{}{}
		for _, id := range lo.occ.Basket.Products {
			basket[id] = struct{}{}
			p, ok := catalog[id]
			if !ok {
				return fail(lo.file, "basket product %q is not in the catalog", id)
			}
			if !contains(lo.occ.Basket.Categories, p.Category) {
				return fail(lo.file, "basket product %q has category %q, which is not on the basket", id, p.Category)
			}
		}
		for _, fix := range lo.occ.Fixes {
			if prev, ok := seenFix[fix.ID]; ok {
				return fail(lo.file, "fix %q is already in %s", fix.ID, prev)
			}
			seenFix[fix.ID] = lo.file
			for _, ch := range fix.Vocabulary {
				attr, ok := attrs[ch.Attribute]
				if !ok {
					return fail(lo.file, "fix %q sets unknown attribute %q", fix.ID, ch.Attribute)
				}
				if !contains(attr.Options, ch.To) {
					return fail(lo.file, "fix %q sets %s to %q, which is not in the vocabulary", fix.ID, ch.Attribute, ch.To)
				}
			}
			for _, id := range fix.Products {
				if _, ok := catalog[id]; !ok {
					return fail(lo.file, "fix %q names product %q, which is not in the catalog", fix.ID, id)
				}
				if _, ok := basket[id]; !ok {
					return fail(lo.file, "fix %q names product %q, which is not in the basket", fix.ID, id)
				}
			}
		}
	}
	return nil
}

func uniqueIDs(file, what string, ids []string) error {
	seen := map[string]struct{}{}
	for _, id := range ids {
		if blank(id) {
			return fail(file, "%s has an empty id", what)
		}
		if _, ok := seen[id]; ok {
			return fail(file, "%s repeats %q", what, id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func blank(s string) bool {
	return strings.TrimSpace(s) == ""
}
