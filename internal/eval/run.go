package eval

import (
	"fmt"
	"path/filepath"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/systemone"
)

// Settings is everything this evaluation chooses, in the settings
// file's own shape. These are measured values for [judge] and [check]:
// they go to the settings file and to docs/evaluation.md, never to a
// user-facing screen.
type Settings struct {
	JudgeMet          float64            `json:"judge_met"`
	JudgePerCriterion map[string]float64 `json:"judge_per_criterion"`
	CheckChangeMade   float64            `json:"check_change_made"`
	CheckSamePerson   float64            `json:"check_same_person"`
	CheckCriterionMet float64            `json:"check_criterion_met"`
	CheckModel        string             `json:"check_model"`
	// The measurement the chosen settings produce.
	JudgeAgreement int `json:"judge_agreement"`
	JudgeCells     int `json:"judge_cells"`
	CheckCorrect   int `json:"check_correct"`
	CheckOf        int `json:"check_of"`
}

// Report is one offline run of the whole evaluation.
type Report struct {
	Judge     JudgeReport     `json:"judge"`
	Check     CheckReport     `json:"check"`
	Stability StabilityReport `json:"stability"`
	Sizes     SizeReport      `json:"sizes"`
	Settings  Settings        `json:"settings"`
}

// Run replays the whole evaluation offline and returns the report.
// It reads only committed recordings and data: testdata/clef (judge
// and check calls), testdata/clef/stability (the ten repeats),
// testdata/clef/cost (the size sweep), testdata/occasions (the
// criteria, the fixes and the vocabulary), and testdata/eval (the
// transcribed ground truth). No key, no network, no spend.
func Run(root string) (Report, error) {
	gt, err := LoadGroundTruth(filepath.Join(root, "testdata", "eval", "ground-truth.json"))
	if err != nil {
		return Report{}, err
	}
	loaded, err := domain.Load(filepath.Join(root, "testdata", "occasions"))
	if err != nil {
		return Report{}, err
	}
	occasions := make(map[string]*domain.Occasion, len(loaded.Occasions))
	for i := range loaded.Occasions {
		occ := loaded.Occasions[i]
		occasions[occ.ID] = &occ
	}
	vocab := loaded.Vocabulary
	clefDir := filepath.Join(root, "testdata", "clef")
	replay, err := systemone.Open(clefDir)
	if err != nil {
		return Report{}, err
	}

	judge, judgeSettings, err := evaluateJudge(clefDir, replay, occasions, gt)
	if err != nil {
		return Report{}, fmt.Errorf("eval: judging: %w", err)
	}
	check, checkLines, chosenModel, err := evaluateCheck(clefDir, replay, occasions, &vocab, gt)
	if err != nil {
		return Report{}, fmt.Errorf("eval: render check: %w", err)
	}
	stability, err := compareStability(clefDir)
	if err != nil {
		return Report{}, fmt.Errorf("eval: stability: %w", err)
	}

	// The size sweep reads the full-size V04 recording through the
	// chosen lines, so the comparison is at the settings that ship.
	full, err := openReply(clefDir, "judge-V04-family-function")
	if err != nil {
		return Report{}, err
	}
	lines := map[string]float64{}
	for _, c := range occasions["family-function"].Criteria {
		if v, ok := judgeSettings.PerCriterion[c.ID]; ok {
			lines[c.ID] = v
		}
	}
	sizes, err := compareSizes(filepath.Join(clefDir, "cost"), full, lines, judgeSettings.Met)
	if err != nil {
		return Report{}, fmt.Errorf("eval: sizes: %w", err)
	}

	correct := 0
	for _, outcome := range check.Outcomes {
		if outcome.Bad != outcome.Shown {
			correct++
		}
	}
	report := Report{
		Judge:     judge,
		Check:     check,
		Stability: stability,
		Sizes:     sizes,
		Settings: Settings{
			JudgeMet:          judgeSettings.Met,
			JudgePerCriterion: judgeSettings.PerCriterion,
			CheckChangeMade:   checkLines.ChangeMade,
			CheckSamePerson:   checkLines.SamePerson,
			CheckCriterionMet: checkLines.CriterionMet,
			CheckModel:        chosenModel,
			JudgeAgreement:    judge.ChosenTotal,
			JudgeCells:        judge.Cells,
			CheckCorrect:      correct,
			CheckOf:           len(check.Outcomes),
		},
	}
	return report, nil
}
