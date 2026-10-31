package eval

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/pipeline"
	"github.com/nrynss/dresscode/internal/systemone"
)

// CheckRead is one recorded check's raw gates: the honest pair (the
// second-visit render) and the different-person proxy, one read per
// model. The proxy is the known bad render class T0.4 gave the check;
// the honest pair is the positive control.
type CheckRead struct {
	Exchange string
	Model    string
	Bad      bool
	Change   float64
	Same     float64
	Met      float64
}

// gateNames are the check's three gates, in the recorded order.
var gateNames = []string{"change_made", "same_person", "criterion_met"}

// readChecks replays the four check recordings through the real
// pipeline.Check at the 0.5 reading, capturing the raw answers. Each
// call must hit its recording, so a read here is also the proof that
// check builds the recorded request - two images, the original first.
func readChecks(clefDir string, replay *systemone.Replay, occasions map[string]*domain.Occasion, vocab *domain.Vocabulary, gt GroundTruth) ([]CheckRead, map[string]systemone.Response, error) {
	truths := append([]CheckTruth(nil), gt.Checks...)
	sort.Slice(truths, func(i, j int) bool { return truths[i].Exchange < truths[j].Exchange })
	reads := make([]CheckRead, 0, len(truths))
	answers := make(map[string]systemone.Response, len(truths))
	for _, truth := range truths {
		rec, err := readRecording(filepath.Join(clefDir, truth.Exchange+".json"))
		if err != nil {
			return nil, nil, err
		}
		if rec.Exchange != truth.Exchange {
			return nil, nil, fmt.Errorf("eval: %s: exchange %q", truth.Exchange, rec.Exchange)
		}
		if rec.Request.Body.Model != truth.Model {
			return nil, nil, fmt.Errorf("eval: %s: model %q, ground truth says %q", truth.Exchange, rec.Request.Body.Model, truth.Model)
		}
		state, err := stateFields(rec.Request.Body.State, "occasion", "requested_change", "criterion")
		if err != nil {
			return nil, nil, fmt.Errorf("eval: %s: %w", truth.Exchange, err)
		}
		occ, ok := occasions[state[0]]
		if !ok {
			return nil, nil, fmt.Errorf("eval: %s: no occasion %q", truth.Exchange, state[0])
		}
		if len(rec.Request.Body.Images) != 2 {
			return nil, nil, fmt.Errorf("eval: %s: %d images, want the original and the render", truth.Exchange, len(rec.Request.Body.Images))
		}
		original, err := readImage(clefDir, rec.Request.Body.Images[0])
		if err != nil {
			return nil, nil, fmt.Errorf("eval: %s: %w", truth.Exchange, err)
		}
		render, err := readImage(clefDir, rec.Request.Body.Images[1])
		if err != nil {
			return nil, nil, fmt.Errorf("eval: %s: %w", truth.Exchange, err)
		}
		fix := fixFor(occ, state[2])
		if fix == nil {
			return nil, nil, fmt.Errorf("eval: %s: no fix on %q addresses %q", truth.Exchange, occ.ID, state[2])
		}
		capture := &captureClient{inner: replay}
		if _, err := pipeline.Check(context.Background(), capture, truth.Model, original, render,
			occ, fix, state[2], state[1], vocab, pipeline.Lines{ChangeMade: readingAt, SamePerson: readingAt, CriterionMet: readingAt}); err != nil {
			return nil, nil, fmt.Errorf("eval: %s: %w", truth.Exchange, err)
		}
		change, err := gate(capture.resp, "change_made")
		if err != nil {
			return nil, nil, fmt.Errorf("eval: %s: %w", truth.Exchange, err)
		}
		same, err := gate(capture.resp, "same_person")
		if err != nil {
			return nil, nil, fmt.Errorf("eval: %s: %w", truth.Exchange, err)
		}
		met, err := gate(capture.resp, "criterion_met")
		if err != nil {
			return nil, nil, fmt.Errorf("eval: %s: %w", truth.Exchange, err)
		}
		reads = append(reads, CheckRead{
			Exchange: truth.Exchange, Model: truth.Model, Bad: truth.Bad,
			Change: change, Same: same, Met: met,
		})
		answers[truth.Exchange] = capture.resp
	}
	return reads, answers, nil
}

// gate reads one gate's yes-probability from a captured response.
func gate(resp systemone.Response, id string) (float64, error) {
	answer, ok := resp.Answers[id]
	if !ok || answer.Type != systemone.TypeNoul || answer.Noul == nil {
		return 0, fmt.Errorf("no yes probability for %q", id)
	}
	return *answer.Noul, nil
}

// fixFor finds the one fix on the occasion that addresses the
// criterion, the recorded check's shape.
func fixFor(occ *domain.Occasion, criterionID string) *domain.Fix {
	var found *domain.Fix
	for i := range occ.Fixes {
		fix := &occ.Fixes[i]
		for _, id := range fix.Criteria {
			if id == criterionID {
				if found != nil {
					return nil
				}
				found = fix
			}
		}
	}
	return found
}

// GateCurve is one gate's curve on one model: the honest pair must sit
// at or above the line, the known bad render under it.
type GateCurve struct {
	Gate  string  `json:"gate"`
	Model string  `json:"model"`
	Good  float64 `json:"good"`
	Bad   float64 `json:"bad"`
	// PlateauLow and PlateauHigh bound the separating interval - every
	// line inside it decides both reads correctly. Chosen is its
	// midpoint, Margin the distance to the nearer read.
	PlateauLow  float64 `json:"plateau_low"`
	PlateauHigh float64 `json:"plateau_high"`
	Chosen      float64 `json:"chosen"`
	Margin      float64 `json:"margin"`
	Correct     int     `json:"correct"`
}

// gateCurve sweeps one gate's two reads.
func gateCurve(gateName, model string, good, bad float64) GateCurve {
	c := GateCurve{Gate: gateName, Model: model, Good: good, Bad: bad}
	cells := []Cell{
		{Sample: "honest", Value: good, Expected: true},
		{Sample: "bad", Value: bad, Expected: false},
	}
	swept := sweep(cells)
	c.PlateauLow, c.PlateauHigh = swept.PlateauLow, swept.PlateauHigh
	c.Chosen, c.Margin, c.Correct = swept.Chosen, swept.Margin, swept.MaxAgreement
	return c
}

// ChosenLines is the check's [check] shape: three lines from the
// chosen model's curves.
type ChosenLines struct {
	ChangeMade   float64 `json:"change_made"`
	SamePerson   float64 `json:"same_person"`
	CriterionMet float64 `json:"criterion_met"`
}

// ModelScore is one model's record on the recorded checks.
type ModelScore struct {
	Model     string `json:"model"`
	GatesOK   int    `json:"gates_ok"`  // gates deciding both reads right, at 0.5
	GatesOf   int    `json:"gates_of"`  // gates asked
	VocabOK   int    `json:"vocab_ok"`  // attribute answers equal to the truth
	VocabOf   int    `json:"vocab_of"`  // attribute answers given
	Discarded int    `json:"discarded"` // known bad renders discarded, at 0.5
	GoodKept  int    `json:"good_kept"` // honest renders shown, at 0.5
}

// VocabularyCell is one attribute answer against the render's truth.
type VocabularyCell struct {
	Exchange  string `json:"exchange"`
	Attribute string `json:"attribute"`
	Answer    string `json:"answer"`
	Truth     string `json:"truth"`
	Correct   bool   `json:"correct"`
	// Note carries the ground truth's judgement note - the places
	// where the vocabulary cannot say the truth exactly.
	Note string `json:"note,omitempty"`
}

// VocabularyReport scores the look vocabulary: Clef's answer against
// the true attribute on every recorded render, all four reads.
type VocabularyReport struct {
	Cells   []VocabularyCell `json:"cells"`
	Correct int              `json:"correct"`
	Of      int              `json:"of"`
}

// JointPlateau is one gate's separating interval across both models:
// a line inside it decides both models' reads correctly.
type JointPlateau struct {
	Gate string  `json:"gate"`
	Low  float64 `json:"low"`
	High float64 `json:"high"`
}

// CheckReport is the render-check evaluation.
type CheckReport struct {
	Reads []CheckRead `json:"reads"`
	Gates []GateCurve `json:"gates"`
	// JointPlateaus bound the intervals that separate the honest pair
	// from the known bad render on both models at once.
	JointPlateaus []JointPlateau   `json:"joint_plateaus"`
	Vocabulary    VocabularyReport `json:"vocabulary"`
	Models        []ModelScore     `json:"models"`
	ChosenModel   string           `json:"chosen_model"`
	Lines         ChosenLines      `json:"lines"`
	// At the chosen lines, through the real pipeline.Check:
	Outcomes []CheckOutcome `json:"outcomes"`
	// GoodDiscarded counts the honest renders the chosen lines throw
	// away; BadShown counts the known bad renders they let through.
	// Both are expected to stay zero.
	GoodDiscarded int `json:"good_discarded"`
	BadShown      int `json:"bad_shown"`
}

// CheckOutcome is one recording's decision at the chosen lines.
type CheckOutcome struct {
	Exchange string `json:"exchange"`
	Bad      bool   `json:"bad"`
	Shown    bool   `json:"shown"`
}

// evaluateCheck reads the checks, sweeps the gates, scores the
// vocabulary, picks the model, and applies the chosen lines through
// the real pipeline.
func evaluateCheck(clefDir string, replay *systemone.Replay, occasions map[string]*domain.Occasion, vocab *domain.Vocabulary, gt GroundTruth) (CheckReport, ChosenLines, string, error) {
	reads, answers, err := readChecks(clefDir, replay, occasions, vocab, gt)
	if err != nil {
		return CheckReport{}, ChosenLines{}, "", err
	}
	report := CheckReport{Reads: reads}

	// Per model, each gate's honest value and known bad value.
	perModel := map[string]map[string][2]float64{} // model -> gate -> {bad, good}
	var modelNames []string
	for _, r := range reads {
		if _, ok := perModel[r.Model]; !ok {
			perModel[r.Model] = map[string][2]float64{}
			modelNames = append(modelNames, r.Model)
		}
	}
	sort.Strings(modelNames)
	values := func(r CheckRead) map[string]float64 {
		return map[string]float64{"change_made": r.Change, "same_person": r.Same, "criterion_met": r.Met}
	}
	for _, r := range reads {
		m := perModel[r.Model]
		for name, v := range values(r) {
			pair := m[name]
			if r.Bad {
				pair[0] = v
			} else {
				pair[1] = v
			}
			m[name] = pair
		}
	}

	scores := make([]ModelScore, 0, len(modelNames))
	for _, model := range modelNames {
		m := perModel[model]
		recordings := 0
		for _, r := range reads {
			if r.Model == model {
				recordings++
			}
		}
		score := ModelScore{Model: model, GatesOf: len(gateNames), VocabOf: recordings * len(vocab.Attributes)}
		for _, name := range gateNames {
			pair := m[name]
			report.Gates = append(report.Gates, gateCurve(name, model, pair[1], pair[0]))
			if pair[1] >= readingAt && pair[0] < readingAt {
				score.GatesOK++
			}
		}
		for _, r := range reads {
			if r.Model != model {
				continue
			}
			shown := r.Change >= readingAt && r.Same >= readingAt
			if r.Bad && !shown {
				score.Discarded++
			}
			if !r.Bad && shown {
				score.GoodKept++
			}
		}
		scores = append(scores, score)
	}
	sort.Slice(report.Gates, func(i, j int) bool {
		if report.Gates[i].Model != report.Gates[j].Model {
			return report.Gates[i].Model < report.Gates[j].Model
		}
		return report.Gates[i].Gate < report.Gates[j].Gate
	})
	report.Models = scores

	// The joint separating interval per gate: both models must agree.
	for _, name := range gateNames {
		low, high := 0.0, 1.0
		for _, model := range modelNames {
			pair := perModel[model][name]
			if pair[0] > low {
				low = pair[0]
			}
			if pair[1] < high {
				high = pair[1]
			}
		}
		if low < high {
			report.JointPlateaus = append(report.JointPlateaus, JointPlateau{Gate: name, Low: low, High: high})
		}
	}
	sort.Slice(report.JointPlateaus, func(i, j int) bool { return report.JointPlateaus[i].Gate < report.JointPlateaus[j].Gate })

	// The vocabulary: every attribute answer against the render's
	// truth, per recording, attributes in the vocabulary's order.
	truths := append([]CheckTruth(nil), gt.Checks...)
	sort.Slice(truths, func(i, j int) bool { return truths[i].Exchange < truths[j].Exchange })
	for _, truth := range truths {
		resp, ok := answers[truth.Exchange]
		if !ok {
			return CheckReport{}, ChosenLines{}, "", fmt.Errorf("eval: %s: no captured answers", truth.Exchange)
		}
		for _, attr := range vocab.Attributes {
			answer, ok := resp.Answers["look."+attr.ID]
			if !ok || answer.Type != systemone.TypeChoice {
				return CheckReport{}, ChosenLines{}, "", fmt.Errorf("eval: %s: no choice for %q", truth.Exchange, attr.ID)
			}
			cell := VocabularyCell{
				Exchange:  truth.Exchange,
				Attribute: attr.ID,
				Answer:    answer.Choice,
				Truth:     truth.Look[attr.ID],
				Correct:   answer.Choice == truth.Look[attr.ID],
				Note:      truth.Notes[attr.ID],
			}
			report.Vocabulary.Cells = append(report.Vocabulary.Cells, cell)
			report.Vocabulary.Of++
			if cell.Correct {
				report.Vocabulary.Correct++
			}
			for i := range report.Models {
				if report.Models[i].Model == truth.Model && cell.Correct {
					report.Models[i].VocabOK++
				}
			}
		}
	}

	// The model: among the models whose gates decide every read
	// correctly, the one that reads the look most truthfully. Both
	// recorded models gate perfectly on the proxies, so the vocabulary
	// decides; a tie goes to the first name in model order.
	chosenModel := ""
	chosenVocab := -1
	for _, s := range report.Models {
		if s.GatesOK != s.GatesOf {
			continue
		}
		if s.VocabOK > chosenVocab {
			chosenModel, chosenVocab = s.Model, s.VocabOK
		}
	}
	if chosenModel == "" {
		return CheckReport{}, ChosenLines{}, "", fmt.Errorf("eval: no recorded model gates every read correctly")
	}
	report.ChosenModel = chosenModel

	// The chosen lines are the chosen model's curve midpoints.
	lines := ChosenLines{}
	for _, g := range report.Gates {
		if g.Model != chosenModel {
			continue
		}
		switch g.Gate {
		case "change_made":
			lines.ChangeMade = g.Chosen
		case "same_person":
			lines.SamePerson = g.Chosen
		case "criterion_met":
			lines.CriterionMet = g.Chosen
		}
	}
	report.Lines = lines

	// Apply the chosen lines through the real check, one call per
	// recording, and count what the product would show.
	for _, truth := range truths {
		rec, err := readRecording(filepath.Join(clefDir, truth.Exchange+".json"))
		if err != nil {
			return CheckReport{}, ChosenLines{}, "", err
		}
		state, err := stateFields(rec.Request.Body.State, "occasion", "requested_change", "criterion")
		if err != nil {
			return CheckReport{}, ChosenLines{}, "", fmt.Errorf("eval: %s: %w", truth.Exchange, err)
		}
		occ := occasions[state[0]]
		original, err := readImage(clefDir, rec.Request.Body.Images[0])
		if err != nil {
			return CheckReport{}, ChosenLines{}, "", err
		}
		render, err := readImage(clefDir, rec.Request.Body.Images[1])
		if err != nil {
			return CheckReport{}, ChosenLines{}, "", err
		}
		fix := fixFor(occ, state[2])
		outcome, err := pipeline.Check(context.Background(), replay, truth.Model, original, render,
			occ, fix, state[2], state[1], vocab, pipeline.Lines(lines))
		if err != nil {
			return CheckReport{}, ChosenLines{}, "", fmt.Errorf("eval: %s: %w", truth.Exchange, err)
		}
		shown := outcome.Shown()
		report.Outcomes = append(report.Outcomes, CheckOutcome{Exchange: truth.Exchange, Bad: truth.Bad, Shown: shown})
		if truth.Bad && shown {
			report.BadShown++
		}
		if !truth.Bad && !shown {
			report.GoodDiscarded++
		}
	}
	sort.Slice(report.Outcomes, func(i, j int) bool { return report.Outcomes[i].Exchange < report.Outcomes[j].Exchange })
	return report, lines, chosenModel, nil
}
