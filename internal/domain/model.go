package domain

import (
	"encoding/json"
	"fmt"
)

// CriterionKind is clothing, grooming, lighting, or expression.
type CriterionKind string

// Criterion kinds. A criterion is about one of these and nothing else.
const (
	KindClothing   CriterionKind = "clothing"
	KindGrooming   CriterionKind = "grooming"
	KindLighting   CriterionKind = "lighting"
	KindExpression CriterionKind = "expression"
)

// Valid reports whether k is one of the four kinds.
func (k CriterionKind) Valid() bool {
	switch k {
	case KindClothing, KindGrooming, KindLighting, KindExpression:
		return true
	default:
		return false
	}
}

// Criterion is one yes/no question, worded for Clef and for the user.
// Yes means the dress code is met. The user reads Suggestion, never a score.
type Criterion struct {
	ID         string        `json:"id"`
	Kind       CriterionKind `json:"kind"`
	Clef       string        `json:"clef"`
	Suggestion string        `json:"suggestion"`
}

// YouCam is the task that renders a fix.
type YouCam struct {
	Set          string `json:"set"`
	Task         string `json:"task"`
	Method       string `json:"method"`
	Path         string `json:"path"`
	PathVerified bool   `json:"path_verified"`
	PublicName   string `json:"public_name"`
	Doc          string `json:"doc"`
}

// Analysis is a YouCam read a fix needs before it renders, if it needs one.
// UseFields must be on the picker allowlist. NeverUse is never forwarded.
type Analysis struct {
	Task         string   `json:"task"`
	Method       string   `json:"method"`
	Path         string   `json:"path"`
	PathVerified bool     `json:"path_verified"`
	PublicName   string   `json:"public_name"`
	Doc          string   `json:"doc"`
	UseFields    []string `json:"use_fields"`
	NeverUse     []string `json:"never_use"`
}

// VocabularyChange is one look-vocabulary value a fix sets.
type VocabularyChange struct {
	Attribute string `json:"attribute"`
	To        string `json:"to"`
}

// FixDisplay is a fix's display wording, as data. Name is what the shop
// list shows for a visual-simulation entry of this fix; without it the
// entry copies the fix's changes sentence, the way it always has. Words
// is the fix in words the memory sentences keep; without it the sentence
// falls back to the id's own name. Changes stays the fix's own sentence:
// the pick menu and the render check read it, never either field here.
type FixDisplay struct {
	// Name is the display name a shop suggestion wears.
	Name string `json:"name,omitempty"`
	// Words is the fix in words a remembered sentence carries.
	Words string `json:"words,omitempty"`
}

// Fix is one opt-in menu entry: what it changes, which YouCam task renders
// it, which analysis it needs, and which criteria it addresses.
//
// Template is the YouCam template id. A hair-transfer or beard-style fix
// must name one; any other task takes none.
type Fix struct {
	ID         string             `json:"id"`
	OptIn      bool               `json:"opt_in"`
	Changes    string             `json:"changes"`
	YouCam     YouCam             `json:"youcam"`
	Template   string             `json:"template,omitempty"`
	Analysis   []Analysis         `json:"analysis"`
	Criteria   []string           `json:"criteria"`
	Vocabulary []VocabularyChange `json:"vocabulary"`
	Products   []string           `json:"products"`
	Display    *FixDisplay        `json:"display,omitempty"`
	Note       string             `json:"note,omitempty"`
}

// IssueOption is one answer on the biggest-issue question.
type IssueOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// BiggestIssue is the judge's choice question for one occasion.
// Its options are that occasion's criteria, then none, then can't tell.
type BiggestIssue struct {
	Question string        `json:"question"`
	Options  []IssueOption `json:"options"`
}

// Basket is one occasion's retail list. TemplateOnly entries have no
// product to photograph; a render of one is a visual simulation.
type Basket struct {
	Categories   []string `json:"categories"`
	Products     []string `json:"products"`
	TemplateOnly []string `json:"template_only"`
}

// Occasion is one dress code: its criteria, its fix menu, and its basket.
type Occasion struct {
	Kind         string       `json:"kind"`
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Criteria     []Criterion  `json:"criteria"`
	BiggestIssue BiggestIssue `json:"biggest_issue"`
	Fixes        []Fix        `json:"fixes"`
	Basket       Basket       `json:"basket"`
}

// LookAttribute is one choice question over the fixed look vocabulary.
type LookAttribute struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

// Vocabulary is the only look vocabulary. Attributes are the questions.
//
// Phrases is the display-phrase table: attribute id, then option value,
// then the phrase the look-in-words line speaks for that winner. An
// option with no entry is spoken as its own bare label. A phrase may
// carry `{attribute}` placeholders that embed another attribute's winner
// from the same answer set; the loader refuses one that names an
// attribute the vocabulary does not have.
type Vocabulary struct {
	Kind       string                       `json:"kind"`
	ID         string                       `json:"id"`
	Attributes []LookAttribute              `json:"attributes"`
	Phrases    map[string]map[string]string `json:"phrases,omitempty"`
}

// Origin says whether a product came from the catalog or from the user.
type Origin string

// Product origins.
const (
	OriginCatalog Origin = "catalog"
	OriginUser    Origin = "user"
)

// Valid reports whether o is a known origin.
func (o Origin) Valid() bool {
	return o == OriginCatalog || o == OriginUser
}

// Product is a catalog entry or a user-supplied item, with its fidelity label.
type Product struct {
	ID         string   `json:"id"`
	Category   string   `json:"category"`
	Name       string   `json:"name"`
	Fidelity   Fidelity `json:"fidelity"`
	Origin     Origin   `json:"origin,omitempty"`
	YouCamRole string   `json:"youcam_role,omitempty"`
	Photograph string   `json:"photograph,omitempty"`
}

// Verdict is the user's decision on a look. Empty means they have not decided.
type Verdict string

// Verdicts the user can record. Empty is an unfinished look.
const (
	VerdictKept      Verdict = "kept"
	VerdictDismissed Verdict = "dismissed"
	VerdictBought    Verdict = "bought"
)

// Valid reports whether v is empty or one of the three verdicts.
func (v Verdict) Valid() bool {
	switch v {
	case "", VerdictKept, VerdictDismissed, VerdictBought:
		return true
	default:
		return false
	}
}

// MarshalJSON writes the verdict. An unknown value is an error.
func (v Verdict) MarshalJSON() ([]byte, error) {
	if !v.Valid() {
		return nil, fmt.Errorf("unknown verdict %q", string(v))
	}
	return json.Marshal(string(v))
}

// UnmarshalJSON accepts an empty verdict or one of the three decisions.
func (v *Verdict) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	parsed := Verdict(raw)
	if !parsed.Valid() {
		return fmt.Errorf("unknown verdict %q", raw)
	}
	*v = parsed
	return nil
}

// AnsweredAttribute is one vocabulary answer on a render check.
type AnsweredAttribute struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

// Check is Clef's read of one render: the change was made, it is the same
// person, the criterion is met, and what the look shows.
type Check struct {
	ChangeMade   bool                `json:"change_made"`
	SamePerson   bool                `json:"same_person"`
	CriterionMet bool                `json:"criterion_met"`
	Attributes   []AnsweredAttribute `json:"attributes"`
}

// Render is a YouCam result copied into mediastore, with Clef's check.
// Check is nil until the render has been checked. A YouCam link is not
// stored; those links expire.
type Render struct {
	MediaID string `json:"media_id"`
	FixID   string `json:"fix_id"`
	Check   *Check `json:"check"`
}

// Look is one attempt: the selfie, the occasion, the flags, the chosen fix
// and runner-up, the renders with their checks, the products, and the verdict.
//
// SelfieID is the mediastore id of the normalised selfie, not the photo.
// Flags, Fix, and RunnerUp are ids. The fix picker does not read this
// struct for pixels; it receives an Allowlist.
type Look struct {
	ID       string    `json:"id"`
	SelfieID string    `json:"selfie_id"`
	Occasion string    `json:"occasion"`
	Flags    []string  `json:"flags"`
	Fix      string    `json:"fix"`
	RunnerUp string    `json:"runner_up"`
	Renders  []Render  `json:"renders"`
	Products []Product `json:"products"`
	Verdict  Verdict   `json:"verdict"`
}
