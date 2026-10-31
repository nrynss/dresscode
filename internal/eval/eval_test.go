package eval

import (
	"math"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"

	"github.com/nrynss/dresscode/internal/domain"
)

// repoRoot is the repository the evaluation runs against. The package
// replays committed recordings only, so the tests need no key and no
// network.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// The evaluation is a pure read of committed recordings, so the tests
// share one report; the determinism test runs Run twice on its own.
var (
	onceRun   sync.Once
	cachedRun Report
	cachedErr error
)

func runOnce(t *testing.T) Report {
	t.Helper()
	onceRun.Do(func() { cachedRun, cachedErr = Run(repoRoot(t)) })
	if cachedErr != nil {
		t.Fatalf("eval: %v", cachedErr)
	}
	return cachedRun
}

// TestTheGroundTruthCoversTheSet proves the transcription: every
// sample's met and miss lists are exactly its occasion's criteria,
// each criterion in one list, and the cell count is the recordings'.
func TestTheGroundTruthCoversTheSet(t *testing.T) {
	loaded, err := domain.Load(repoRoot(t) + "/testdata/occasions")
	if err != nil {
		t.Fatalf("load occasions: %v", err)
	}
	occasions := map[string][]string{}
	for _, occ := range loaded.Occasions {
		var ids []string
		for _, c := range occ.Criteria {
			ids = append(ids, c.ID)
		}
		occasions[occ.ID] = ids
	}
	gt, err := LoadGroundTruth(repoRoot(t) + "/testdata/eval/ground-truth.json")
	if err != nil {
		t.Fatalf("load ground truth: %v", err)
	}
	if len(gt.Samples) != 37 {
		t.Fatalf("%d samples, want 37", len(gt.Samples))
	}
	if len(gt.Checks) != 4 {
		t.Fatalf("%d check truths, want 4", len(gt.Checks))
	}
	cells := 0
	for _, s := range gt.Samples {
		criteria, ok := occasions[s.Occasion]
		if !ok {
			t.Fatalf("%s: unknown occasion %q", s.ID, s.Occasion)
		}
		if len(s.Met)+len(s.Miss) != len(criteria) {
			t.Fatalf("%s: %d expected cells, the occasion has %d criteria", s.ID, len(s.Met)+len(s.Miss), len(criteria))
		}
		for _, id := range criteria {
			want, ok := s.Expected(id)
			if !ok {
				t.Errorf("%s: criterion %q is in neither list", s.ID, id)
				continue
			}
			if countIn(s.Met, id)+countIn(s.Miss, id) != 1 {
				t.Errorf("%s: criterion %q is not in exactly one list", s.ID, id)
			}
			if !want && !contains(s.Miss, id) {
				t.Errorf("%s: %q expected miss is not in the miss list", s.ID, id)
			}
			_ = want
		}
		cells += len(s.Met) + len(s.Miss)
	}
	if cells != 273 {
		t.Fatalf("%d cells, want 273", cells)
	}
}

func countIn(list []string, id string) int {
	n := 0
	for _, v := range list {
		if v == id {
			n++
		}
	}
	return n
}

func contains(list []string, want string) bool {
	return countIn(list, want) > 0
}

// TestRunReplaysTwiceTheSame is the determinism clause: the evaluation
// is a pure read of committed recordings, so two runs return the same
// report, field for field. It calls Run directly, past the shared
// report, so both runs are fresh.
func TestRunReplaysTwiceTheSame(t *testing.T) {
	root := repoRoot(t)
	first, err := Run(root)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := Run(root)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("two runs of the evaluation disagreed")
	}
	if !reflect.DeepEqual(first, runOnce(t)) {
		t.Fatal("the shared report differs from a fresh run")
	}
}

// offAtHalf is the thirteen cells docs/clef.md counts as off the
// expected side of 0.5 - the sanity anchors this evaluation's curves
// must show.
var offAtHalf = map[string]float64{
	"P09 date-beard-even":           0.4508,
	"P11 family-garment-festive":    0.4425,
	"P12 family-expression-present": 0.612,
	"P13 interview-beard-short":     0.5771,
	"P16 funeral-beard-close":       0.5737,
	"V02 date-light-see":            0.7913,
	"V06 family-expression-present": 0.5431,
	"V08 family-beard-even":         0.609,
	"V08 family-light-even":         0.6223,
	"V09 interview-collar-buttoned": 0.1853,
	"V12 interview-collar-buttoned": 0.401,
	"V16 festival-light":            0.5734,
	"V21 date-beard-even":           0.5985,
}

// wrongAtChosen are the four cells no line on this set can save: the
// men's beard and light readings that sit inside the met group's own
// range, and one flat stare that reads calmer than a calm face.
var wrongAtChosen = map[string]float64{
	"P09 date-beard-even":           0.4508,
	"P11 family-beard-even":         0.5106,
	"P12 family-expression-present": 0.612,
	"V02 date-light-see":            0.7913,
}

// TestTheJudgeAnchors pins the judge read against docs/clef.md: the
// sample count, the cell count, the 260-of-273 agreement at 0.5, the
// thirteen anchor cells, and the biggest-issue count.
func TestTheJudgeAnchors(t *testing.T) {
	report := runOnce(t)
	j := report.Judge
	if j.Samples != 37 || j.Cells != 273 {
		t.Fatalf("judge read %d samples and %d cells, want 37 and 273", j.Samples, j.Cells)
	}
	if j.AgreeAtHalf != 260 {
		t.Fatalf("agreement at 0.5 is %d, want 260 of 273", j.AgreeAtHalf)
	}
	if len(j.OffAtHalf) != len(offAtHalf) {
		t.Fatalf("%d cells off the expected side of 0.5, want the thirteen docs/clef.md names", len(j.OffAtHalf))
	}
	for _, cell := range j.OffAtHalf {
		key := cell.Sample + " " + cell.Criterion
		want, ok := offAtHalf[key]
		if !ok {
			t.Fatalf("unexpected off-side cell %s at %.4f", key, cell.Value)
		}
		if cell.Value != want {
			t.Errorf("%s reads %.4f, docs/clef.md records %.4f", key, cell.Value, want)
		}
	}
	if len(j.Curves) != 36 {
		t.Fatalf("%d criterion curves, want 36", len(j.Curves))
	}
	apart := 0
	for _, c := range j.Curves {
		if c.Apart {
			apart++
		}
	}
	if apart != 32 {
		t.Fatalf("%d criteria apart, want 32 of 36", apart)
	}
	if j.Issues.Named != 31 || j.Issues.Of != 37 {
		t.Fatalf("biggest issue named %d of %d, want 31 of 37", j.Issues.Named, j.Issues.Of)
	}
	if !reflect.DeepEqual(j.Issues.Missed, []string{"P09", "P11", "P15", "V07", "V15", "V21"}) {
		t.Fatalf("the biggest issue missed %v, want the six docs/clef.md names", j.Issues.Missed)
	}
}

// TestEveryCurveChoosesFromMeasurement walks every criterion curve and
// proves the chosen line is the curve's own best point: inside the
// widest plateau, reading the maximum agreement the curve allows.
func TestEveryCurveChoosesFromMeasurement(t *testing.T) {
	report := runOnce(t)
	maxTotal := 0
	for _, c := range report.Judge.Curves {
		if len(c.Cells) == 0 {
			t.Fatalf("curve %s has no cells", c.ID)
		}
		if !(c.PlateauLow < c.Chosen && c.Chosen < c.PlateauHigh) {
			t.Fatalf("%s: chosen %.5f is outside its plateau (%.4f, %.4f)", c.ID, c.Chosen, c.PlateauLow, c.PlateauHigh)
		}
		if got := agreement(c.Cells, c.Chosen); got != c.MaxAgreement {
			t.Fatalf("%s: the chosen line reads %d of %d, want its best %d", c.ID, got, len(c.Cells), c.MaxAgreement)
		}
		// The plateau is the widest best interval: no other interval
		// reads more cells, and none reads as many with more room.
		for i := 0; i <= len(c.Cells); i++ {
			var low, high float64
			switch {
			case i == 0:
				low, high = 0, c.Cells[0].Value
			case i == len(c.Cells):
				low, high = c.Cells[i-1].Value, 1
			default:
				low, high = c.Cells[i-1].Value, c.Cells[i].Value
			}
			if low >= high {
				continue
			}
			mid := math.Round((low+high)/2*1e5) / 1e5
			a := agreement(c.Cells, mid)
			if a > c.MaxAgreement {
				t.Fatalf("%s: interval (%.4f, %.4f) reads %d, over the reported best %d", c.ID, low, high, a, c.MaxAgreement)
			}
			if a == c.MaxAgreement && high-low > c.PlateauHigh-c.PlateauLow {
				t.Fatalf("%s: interval (%.4f, %.4f) reads the same %d with more room than the chosen plateau", c.ID, low, high, a)
			}
		}
		maxTotal += c.MaxAgreement
	}
	if maxTotal != 269 {
		t.Fatalf("the curves' best total is %d, want 269 of 273", maxTotal)
	}
	if report.Judge.ChosenTotal != maxTotal {
		t.Fatalf("the chosen settings read %d, want the curves' best %d", report.Judge.ChosenTotal, maxTotal)
	}
	if len(report.Judge.Wrong) != len(wrongAtChosen) {
		t.Fatalf("%d cells wrong at the chosen settings, want the four irreducible ones", len(report.Judge.Wrong))
	}
	for _, cell := range report.Judge.Wrong {
		key := cell.Sample + " " + cell.Criterion
		want, ok := wrongAtChosen[key]
		if !ok {
			t.Fatalf("unexpected wrong cell %s at %.4f", key, cell.Value)
		}
		if cell.Value != want {
			t.Errorf("%s reads %.4f, want %.4f", key, cell.Value, want)
		}
	}
}

// chosenSettings are the values the curves choose, quoted here so any
// drift in the recordings or the data fails this test loudly. They are
// the diff this report proposes for dresscode.toml's [judge] and
// [check] sections.
var chosenSettings = Settings{
	JudgeMet: 0.628,
	JudgePerCriterion: map[string]float64{
		"family-expression-present": 0.5466,
		"family-garment-festive":    0.2394,
		"interview-collar-buttoned": 0.11395,
		"interview-expression-calm": 0.36925,
	},
	CheckChangeMade:   0.50085,
	CheckSamePerson:   0.48355,
	CheckCriterionMet: 0.50315,
	CheckModel:        "clef",
	JudgeAgreement:    269,
	JudgeCells:        273,
	CheckCorrect:      4,
	CheckOf:           4,
}

// TestTheChosenSettings pins the chosen values and their measurement,
// and the default line's own curve evidence.
func TestTheChosenSettings(t *testing.T) {
	report := runOnce(t)
	got := report.Settings
	if got.JudgeMet != chosenSettings.JudgeMet {
		t.Fatalf("met %.5f, want %.5f", got.JudgeMet, chosenSettings.JudgeMet)
	}
	if !reflect.DeepEqual(got.JudgePerCriterion, chosenSettings.JudgePerCriterion) {
		t.Fatalf("per_criterion %v, want %v", got.JudgePerCriterion, chosenSettings.JudgePerCriterion)
	}
	if got.CheckChangeMade != chosenSettings.CheckChangeMade ||
		got.CheckSamePerson != chosenSettings.CheckSamePerson ||
		got.CheckCriterionMet != chosenSettings.CheckCriterionMet ||
		got.CheckModel != chosenSettings.CheckModel {
		t.Fatalf("check settings %+v, want %+v", got, chosenSettings)
	}
	if got.JudgeAgreement != 269 || got.JudgeCells != 273 {
		t.Fatalf("the chosen settings read %d of %d cells, want 269 of 273", got.JudgeAgreement, got.JudgeCells)
	}
	if got.CheckCorrect != 4 || got.CheckOf != 4 {
		t.Fatalf("the chosen check lines decide %d of %d reads, want 4 of 4", got.CheckCorrect, got.CheckOf)
	}
	// The default line is a measurement too: it reads 260 cells, the
	// most any single line reads without overrides.
	if report.Judge.DefaultTotal != 260 {
		t.Fatalf("the default line reads %d, want 260", report.Judge.DefaultTotal)
	}
	for _, c := range report.Judge.Curves {
		line := report.Judge.DefaultMet
		if v, ok := got.JudgePerCriterion[c.ID]; ok {
			line = v
		}
		if a := agreement(c.Cells, line); a != c.MaxAgreement {
			t.Fatalf("%s: its line %.5f reads %d, want its best %d", c.ID, line, a, c.MaxAgreement)
		}
	}
}

// TestTheAppliedFlagsPinsTheStory runs the chosen settings through the
// real pipeline.Judge and pins the second-visit story: V04 flags
// exactly the earring and the lip, V05 flags nothing.
func TestTheAppliedFlagsPinsTheStory(t *testing.T) {
	report := runOnce(t)
	byID := map[string]AppliedSample{}
	for _, a := range report.Judge.Applied {
		byID[a.Sample] = a
	}
	v04, ok := byID["V04"]
	if !ok {
		t.Fatal("no applied read for V04")
	}
	if !reflect.DeepEqual(v04.Flags, []string{"family-earring-festive", "family-lip-colour"}) {
		t.Fatalf("V04 flags %v, want the story's two", v04.Flags)
	}
	v05, ok := byID["V05"]
	if !ok {
		t.Fatal("no applied read for V05")
	}
	if len(v05.Flags) != 0 {
		t.Fatalf("V05 flags %v, want none", v05.Flags)
	}
}

// TestTheCheckDecidesEveryRecordedRead pins the check evaluation: both
// models separate the honest pair from the different-person proxy on
// all three gates, the chosen lines keep every honest render and
// discard every known bad render, and 0.5 sits inside the joint
// interval of all three gates.
func TestTheCheckDecidesEveryRecordedRead(t *testing.T) {
	report := runOnce(t)
	check := report.Check
	if len(check.Reads) != 4 {
		t.Fatalf("%d check reads, want 4", len(check.Reads))
	}
	for _, g := range check.Gates {
		if g.Correct != 2 {
			t.Fatalf("%s on %s: %d of 2 reads correct", g.Gate, g.Model, g.Correct)
		}
		if !(g.Bad < g.Chosen && g.Chosen < g.Good) {
			t.Fatalf("%s on %s: chosen %.5f does not sit between bad %.4f and good %.4f",
				g.Gate, g.Model, g.Chosen, g.Bad, g.Good)
		}
	}
	if len(check.JointPlateaus) != 3 {
		t.Fatalf("%d joint plateaus, want 3", len(check.JointPlateaus))
	}
	for _, j := range check.JointPlateaus {
		if !(j.Low < readingAt && readingAt < j.High) {
			t.Fatalf("%s: the joint interval (%.4f, %.4f) does not hold 0.5", j.Gate, j.Low, j.High)
		}
	}
	if len(check.Outcomes) != 4 {
		t.Fatalf("%d outcomes, want 4", len(check.Outcomes))
	}
	for _, o := range check.Outcomes {
		if o.Bad == o.Shown {
			t.Fatalf("%s: bad=%v shown=%v, want them opposite", o.Exchange, o.Bad, o.Shown)
		}
	}
	if check.GoodDiscarded != 0 {
		t.Fatalf("%d good renders discarded at the chosen lines, want none", check.GoodDiscarded)
	}
	if check.BadShown != 0 {
		t.Fatalf("%d known bad renders shown at the chosen lines, want none", check.BadShown)
	}
}

// TestTheModelChoice pins the model decision: both recorded models
// gate perfectly, and clef reads the look more truthfully - 19 of 22
// against clef-flash's 17 of 22.
func TestTheModelChoice(t *testing.T) {
	report := runOnce(t)
	if report.Check.ChosenModel != "clef" {
		t.Fatalf("chosen check model %q, want clef", report.Check.ChosenModel)
	}
	scores := map[string]ModelScore{}
	for _, m := range report.Check.Models {
		scores[m.Model] = m
		if m.GatesOK != m.GatesOf {
			t.Errorf("%s gates %d of %d, want both models perfect on the proxies", m.Model, m.GatesOK, m.GatesOf)
		}
		if m.Discarded != 1 || m.GoodKept != 1 {
			t.Errorf("%s discarded %d and kept %d, want one of each", m.Model, m.Discarded, m.GoodKept)
		}
	}
	clef, ok := scores["clef"]
	if !ok || clef.VocabOK != 19 || clef.VocabOf != 22 {
		t.Fatalf("clef vocabulary %+v, want 19 of 22", clef)
	}
	flash, ok := scores["clef-flash"]
	if !ok || flash.VocabOK != 17 || flash.VocabOf != 22 {
		t.Fatalf("clef-flash vocabulary %+v, want 17 of 22", flash)
	}
}

// TestTheVocabularyScoresTheLook pins the vocabulary scoring: 36 of 44
// cells true, with exactly the eight misses - two the vocabulary
// cannot express at all, one on each model's read of the honest render,
// and the flash-only bare-ear gold and collar calls on the discarded
// proxy.
func TestTheVocabularyScoresTheLook(t *testing.T) {
	report := runOnce(t)
	v := report.Check.Vocabulary
	if v.Of != 44 || v.Correct != 36 {
		t.Fatalf("vocabulary %d of %d true, want 36 of 44", v.Correct, v.Of)
	}
	misses := map[string]string{} // "exchange attribute" -> answer
	for _, cell := range v.Cells {
		key := cell.Exchange + " " + cell.Attribute
		if cell.Correct {
			if _, wrong := misses[key]; wrong {
				t.Fatalf("%s marked both true and false", key)
			}
			continue
		}
		misses[key] = cell.Answer
	}
	want := map[string]string{
		"check-clef-V04-to-V05 hair":                           "tied-back",
		"check-clef-V04-to-V05 expression":                     "flat",
		"check-clef-V04-to-woman-white lip_colour":             "nude",
		"check-clef-flash-V04-to-V05 hair":                     "tied-back",
		"check-clef-flash-V04-to-V05 expression":               "flat",
		"check-clef-flash-V04-to-woman-white collar":           "buttoned",
		"check-clef-flash-V04-to-woman-white lip_colour":       "nude",
		"check-clef-flash-V04-to-woman-white jewellery_colour": "gold",
	}
	if !reflect.DeepEqual(misses, want) {
		t.Fatalf("vocabulary misses %v, want %v", misses, want)
	}
	for _, cell := range v.Cells {
		if cell.Attribute == "hair" && !cell.Correct && cell.Note == "" {
			t.Fatal("the hair miss carries no judgement note")
		}
		if cell.Attribute == "expression" && !cell.Correct && cell.Note == "" {
			t.Fatal("the expression miss carries no judgement note")
		}
	}
}

// TestTheStabilityRepeats pins the repeat measurement: eleven recorded
// answers of the same request, every cell identical, no criterion
// flipping, one biggest issue.
func TestTheStabilityRepeats(t *testing.T) {
	report := runOnce(t)
	s := report.Stability
	if s.Answers != 11 {
		t.Fatalf("%d recorded answers, want 11 (ten repeats and the original)", s.Answers)
	}
	if s.Cells != 18 {
		t.Fatalf("%d cells compared per answer, want 18", s.Cells)
	}
	if len(s.Flips) != 0 {
		t.Fatalf("the repeats flip %v, want none", s.Flips)
	}
	if len(s.Criteria) != 0 {
		t.Fatalf("criteria %v flip across repeats, want none", s.Criteria)
	}
	if s.Issue != "family-earring-festive" {
		t.Fatalf("the biggest issue across repeats is %q", s.Issue)
	}
}

// TestTheSizeSweep pins the resize question docs/clef.md left open: at
// the chosen lines the flags survive 1024 and 512 px, and at 768 px
// the one criterion that moves is the expression - the curve's
// thinnest plateau.
func TestTheSizeSweep(t *testing.T) {
	report := runOnce(t)
	if report.Sizes.Note == "" {
		t.Fatal("the size sweep says nothing about the no-image recording")
	}
	byLabel := map[string][]string{}
	for _, v := range report.Sizes.Variants {
		byLabel[v.Label] = v.FlagsMoved
	}
	if len(byLabel) != 3 {
		t.Fatalf("%d resized reads, want 3", len(byLabel))
	}
	if moved := byLabel["1024"]; len(moved) != 0 {
		t.Fatalf("at 1024 px the flags move: %v", moved)
	}
	if moved := byLabel["512"]; len(moved) != 0 {
		t.Fatalf("at 512 px the flags move: %v", moved)
	}
	if moved := byLabel["768"]; !reflect.DeepEqual(moved, []string{"family-expression-present"}) {
		t.Fatalf("at 768 px the flags move %v, want only the expression", moved)
	}
}
