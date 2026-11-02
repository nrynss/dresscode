package eval

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/pipeline"
	"github.com/nrynss/dresscode/internal/systemone"
)

// JudgeRead is one sample's recorded judge read: its photo (the
// recording's own bytes), the raw probabilities the pipeline turned
// into flags, and the biggest issue, read two ways. The probabilities
// exist only here, in the evaluation; the pipeline never carries them
// past the threshold.
//
// The two issue reads are the point (T3.13): Issue is the service's raw
// choice, exactly as the recording carries it - Clef's quality. GatedIssue
// is what pipeline.Judge reports for the same recording at the shipped
// settings, its choice gated on the look's own flags - what a user is
// told. Both are carried and neither replaces the other; the printed
// report names each (JudgeReport.IssueLines).
type JudgeRead struct {
	Sample        Sample
	Occasion      *domain.Occasion
	Photo         systemone.Image
	Probabilities map[string]float64
	Issue         string
	GatedIssue    string
}

// readJudgeSamples replays all 37 judge recordings through the real
// pipeline.Judge at the 0.5 reading, capturing the raw answers. Each
// call must hit its recording - the replay serves only the identical
// request - so a read here is also the proof that judge builds the
// recorded probe request byte for byte (docs/clef.md: the probe sent
// no YouCam analysis).
func readJudgeSamples(clefDir string, replay *systemone.Replay, occasions map[string]*domain.Occasion, gt GroundTruth) ([]JudgeRead, error) {
	files, err := filepath.Glob(filepath.Join(clefDir, "judge-*.json"))
	if err != nil {
		return nil, err
	}
	if len(files) != len(gt.Samples) {
		return nil, fmt.Errorf("eval: %d judge recordings, want %d samples", len(files), len(gt.Samples))
	}
	byID := make(map[string]Sample, len(gt.Samples))
	for _, s := range gt.Samples {
		byID[s.ID] = s
	}
	reads := make([]JudgeRead, 0, len(files))
	for _, file := range files {
		rec, err := readRecording(file)
		if err != nil {
			return nil, err
		}
		parts := strings.SplitN(rec.Exchange, "-", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("eval: %s: exchange does not name its variant", rec.Exchange)
		}
		sample, ok := byID[parts[1]]
		if !ok {
			return nil, fmt.Errorf("eval: %s: no ground truth for variant %q", rec.Exchange, parts[1])
		}
		state, err := stateFields(rec.Request.Body.State, "occasion")
		if err != nil {
			return nil, fmt.Errorf("eval: %s: %w", rec.Exchange, err)
		}
		if state[0] != sample.Occasion {
			return nil, fmt.Errorf("eval: %s: state names %q, ground truth says %q", rec.Exchange, state[0], sample.Occasion)
		}
		occ, ok := occasions[sample.Occasion]
		if !ok {
			return nil, fmt.Errorf("eval: %s: no occasion %q", rec.Exchange, sample.Occasion)
		}
		photo, err := readImage(clefDir, rec.Request.Body.Images[0])
		if err != nil {
			return nil, fmt.Errorf("eval: %s: %w", rec.Exchange, err)
		}
		capture := &captureClient{inner: replay}
		// The judgement this call returns is deliberately discarded: the
		// call exists for its replay hit - the proof that judge builds the
		// recorded probe request byte for byte - and for the raw answers
		// captured beside it. Its gate ran at the 0.5 reading, which is
		// not the gate the app ships, so its issue is not either read the
		// report prints. The gated read runs at the shipped settings in
		// evaluateJudge, where the chosen thresholds exist.
		if _, err := pipeline.Judge(context.Background(), capture, photo, occ, pipeline.Thresholds{Met: readingAt}); err != nil {
			return nil, fmt.Errorf("eval: %s: %w", rec.Exchange, err)
		}
		probabilities := make(map[string]float64, len(occ.Criteria))
		for _, c := range occ.Criteria {
			answer, ok := capture.resp.Answers[c.ID]
			if !ok || answer.Type != systemone.TypeNoul || answer.Noul == nil {
				return nil, fmt.Errorf("eval: %s: no yes probability for %q", rec.Exchange, c.ID)
			}
			probabilities[c.ID] = *answer.Noul
		}
		issue, ok := capture.resp.Answers["biggest_issue"]
		if !ok || issue.Type != systemone.TypeChoice {
			return nil, fmt.Errorf("eval: %s: no biggest issue", rec.Exchange)
		}
		reads = append(reads, JudgeRead{
			Sample: sample, Occasion: occ, Photo: photo,
			Probabilities: probabilities, Issue: issue.Choice,
		})
	}
	sort.Slice(reads, func(i, j int) bool { return reads[i].Sample.ID < reads[j].Sample.ID })
	return reads, nil
}

// buildCurves groups every read's cells by criterion id and sweeps each
// curve. lip-quiet rides two occasions; its curve holds both.
func buildCurves(reads []JudgeRead, occasions map[string]*domain.Occasion) ([]Curve, error) {
	var ids []string
	carriers := map[string][]string{}
	for _, occ := range occasions {
		for _, c := range occ.Criteria {
			if _, seen := carriers[c.ID]; !seen {
				ids = append(ids, c.ID)
			}
			carriers[c.ID] = append(carriers[c.ID], occ.ID)
		}
	}
	sort.Strings(ids)
	byOccasion := map[string][]JudgeRead{}
	for _, r := range reads {
		byOccasion[r.Sample.Occasion] = append(byOccasion[r.Sample.Occasion], r)
	}
	for occID := range byOccasion {
		reads := byOccasion[occID]
		sort.Slice(reads, func(i, j int) bool { return reads[i].Sample.ID < reads[j].Sample.ID })
	}
	curves := make([]Curve, 0, len(ids))
	for _, id := range ids {
		occasionIDs := append([]string(nil), carriers[id]...)
		sort.Strings(occasionIDs)
		var cells []Cell
		for _, occID := range occasionIDs {
			rs, ok := byOccasion[occID]
			if !ok {
				return nil, fmt.Errorf("eval: criterion %q: no samples for %q", id, occID)
			}
			for _, r := range rs {
				want, ok := r.Sample.Expected(id)
				if !ok {
					return nil, fmt.Errorf("eval: criterion %q is not in %s's ground truth", id, r.Sample.ID)
				}
				cells = append(cells, Cell{Sample: r.Sample.ID, Value: r.Probabilities[id], Expected: want})
			}
		}
		curve := sweep(cells)
		curve.ID = id
		curve.Occasions = occasionIDs
		curves = append(curves, curve)
	}
	return curves, nil
}

// JudgeSettings is what the curves choose: the default met line and the
// per-criterion overrides, the settings file's [judge] shape. A
// criterion carries an override only when the default line reads it
// below its own curve's best - every criterion then performs at its
// maximum with the fewest overrides.
type JudgeSettings struct {
	Met          float64
	PerCriterion map[string]float64
	Total        int
}

// chooseJudgeSettings picks the default line and the overrides.
func chooseJudgeSettings(curves []Curve) JudgeSettings {
	met, _ := chooseDefault(curves)
	per := map[string]float64{}
	total := 0
	for _, c := range curves {
		line := met
		if agreement(c.Cells, met) < c.MaxAgreement {
			line = c.Chosen
			per[c.ID] = line
		}
		total += agreement(c.Cells, line)
	}
	return JudgeSettings{Met: met, PerCriterion: per, Total: total}
}

// lineFor is the line a criterion reads under the chosen settings.
func (s JudgeSettings) lineFor(c Curve) float64 {
	if v, ok := s.PerCriterion[c.ID]; ok {
		return v
	}
	return s.Met
}

// thresholdsFor projects the settings onto one occasion. The judge
// refuses an override for a criterion the occasion does not carry (a
// stale setting fails loudly), so the wiring that builds one look's
// Thresholds projects the file's map the same way this does.
func thresholdsFor(occ *domain.Occasion, s JudgeSettings) pipeline.Thresholds {
	per := make(map[string]float64, len(s.PerCriterion))
	for _, c := range occ.Criteria {
		if v, ok := s.PerCriterion[c.ID]; ok {
			per[c.ID] = v
		}
	}
	return pipeline.Thresholds{Met: s.Met, PerCriterion: per}
}

// OffCell is one criterion read on one sample that came out on the
// other side of a line from the ground truth.
type OffCell struct {
	Sample    string  `json:"sample"`
	Criterion string  `json:"criterion"`
	Value     float64 `json:"value"`
	Expected  bool    `json:"expected"`
}

// AppliedSample is one sample's flags at the chosen settings, read
// through the real pipeline.Judge, with that judgement's biggest issue
// beside the service's raw choice for the same recording. Both travel
// with the row so the printed report (and the doc that quotes it) names
// which read each figure came from.
type AppliedSample struct {
	Sample     string   `json:"sample"`
	Occasion   string   `json:"occasion"`
	Flags      []string `json:"flags"`
	Issue      string   `json:"issue"`
	GatedIssue string   `json:"gated_issue"`
}

// IssueReport counts one read of the biggest issue against the ground
// truth's shape: the read is expected to name one of the variant's
// misses, or none when the variant misses nothing. The choice steers
// the pick; it is informational here, not a threshold.
type IssueReport struct {
	Named  int      `json:"named"`
	Of     int      `json:"of"`
	Missed []string `json:"missed"`
}

// issueNamesAGroundTruthMiss is the ground truth's shape for the
// biggest issue, the one scoring rule both reads go through: named when
// the read is one of the variant's misses, or none when the variant
// misses nothing. One rule, two callers - the service's raw choice and
// the flag-gated one pipeline.Judge reports - so the two columns of the
// report are always scored the same way.
func issueNamesAGroundTruthMiss(miss []string, issue string) bool {
	if len(miss) == 0 {
		return issue == "none"
	}
	for _, m := range miss {
		if issue == m {
			return true
		}
	}
	return false
}

// JudgeReport is the judging evaluation.
type JudgeReport struct {
	Samples int     `json:"samples"`
	Cells   int     `json:"cells"`
	Curves  []Curve `json:"curves"`
	// AgreeAtHalf is the cell count the recorded reading gets right at
	// 0.5 - the anchor docs/clef.md counts.
	AgreeAtHalf int       `json:"agree_at_half"`
	OffAtHalf   []OffCell `json:"off_at_half"`
	// DefaultMet and DefaultTotal are the best single line and what it
	// reads; ChosenTotal is what the chosen settings read through the
	// overrides. Wrong names the cells the chosen settings still
	// misread - the ones no line on this set can save.
	DefaultMet   float64         `json:"default_met"`
	DefaultTotal int             `json:"default_total"`
	ChosenTotal  int             `json:"chosen_total"`
	Applied      []AppliedSample `json:"applied"`
	Wrong        []OffCell       `json:"wrong"`
	// Issues and GatedIssues are the biggest issue's two reads, each
	// counted against the ground truth's shape by the one rule
	// issueNamesAGroundTruthMiss carries. Issues scores the service's
	// raw choice, the recording's own biggest_issue answer - Clef's
	// quality. GatedIssues scores what pipeline.Judge reports for the
	// same recording at the shipped settings, the choice gated on the
	// look's own flags - what a user is told. Neither is recomputed by
	// hand and neither silently replaces the other: the printed report
	// (IssueLines) names both, and docs/evaluation.md says which of the
	// two each of its figures is read from.
	Issues      IssueReport `json:"issues"`
	GatedIssues IssueReport `json:"gated_issues"`
}

// evaluateJudge reads the samples, sweeps the curves, chooses, and
// applies the chosen settings through the real pipeline.
func evaluateJudge(clefDir string, replay *systemone.Replay, occasions map[string]*domain.Occasion, gt GroundTruth) (JudgeReport, JudgeSettings, error) {
	reads, err := readJudgeSamples(clefDir, replay, occasions, gt)
	if err != nil {
		return JudgeReport{}, JudgeSettings{}, err
	}
	curves, err := buildCurves(reads, occasions)
	if err != nil {
		return JudgeReport{}, JudgeSettings{}, err
	}
	settings := chooseJudgeSettings(curves)
	defaultMet, defaultTotal := chooseDefault(curves)

	report := JudgeReport{Samples: len(reads), Curves: curves}
	for _, c := range curves {
		report.Cells += len(c.Cells)
		report.AgreeAtHalf += agreement(c.Cells, readingAt)
		for _, cell := range c.Cells {
			if (cell.Value >= readingAt) != cell.Expected {
				report.OffAtHalf = append(report.OffAtHalf, OffCell{
					Sample: cell.Sample, Criterion: c.ID, Value: cell.Value, Expected: cell.Expected,
				})
			}
		}
	}
	sort.Slice(report.OffAtHalf, func(i, j int) bool {
		if report.OffAtHalf[i].Sample != report.OffAtHalf[j].Sample {
			return report.OffAtHalf[i].Sample < report.OffAtHalf[j].Sample
		}
		return report.OffAtHalf[i].Criterion < report.OffAtHalf[j].Criterion
	})
	report.DefaultMet, report.DefaultTotal = defaultMet, defaultTotal
	report.ChosenTotal = settings.Total

	// Apply the chosen settings through the real judge, one call per
	// sample, and compare the flags with the ground truth.
	for i, r := range reads {
		capture := &captureClient{inner: replay}
		j, err := pipeline.Judge(context.Background(), capture, r.Photo, r.Occasion, thresholdsFor(r.Occasion, settings))
		if err != nil {
			return JudgeReport{}, JudgeSettings{}, err
		}
		// The judgement's own biggest issue: the choice gated on this
		// look's flags at the shipped settings - what the app reports.
		// It travels with the read (so nothing recomputes it) and is
		// scored beside the raw choice, never instead of it.
		gated := j.Issue.ID
		reads[i].GatedIssue = gated
		applied := AppliedSample{Sample: r.Sample.ID, Occasion: r.Sample.Occasion, Issue: r.Issue, GatedIssue: gated}
		flagged := make(map[string]bool, len(j.Flags))
		for _, f := range j.Flags {
			applied.Flags = append(applied.Flags, f.ID)
			flagged[f.ID] = true
			if want, _ := r.Sample.Expected(f.ID); want {
				report.Wrong = append(report.Wrong, OffCell{
					Sample: r.Sample.ID, Criterion: f.ID, Value: r.Probabilities[f.ID], Expected: true,
				})
			}
		}
		for _, c := range r.Occasion.Criteria {
			if want, ok := r.Sample.Expected(c.ID); ok && !want && !flagged[c.ID] {
				report.Wrong = append(report.Wrong, OffCell{
					Sample: r.Sample.ID, Criterion: c.ID, Value: r.Probabilities[c.ID], Expected: false,
				})
			}
		}
		report.Applied = append(report.Applied, applied)

		// The biggest issue, both reads against the same ground truth's
		// shape: one of the variant's misses, or none when it misses
		// nothing. The raw choice is Clef's quality; the gated one is
		// what a user is told. Neither replaces the other.
		report.Issues.Of++
		if issueNamesAGroundTruthMiss(r.Sample.Miss, r.Issue) {
			report.Issues.Named++
		} else {
			report.Issues.Missed = append(report.Issues.Missed, r.Sample.ID)
		}
		report.GatedIssues.Of++
		if issueNamesAGroundTruthMiss(r.Sample.Miss, gated) {
			report.GatedIssues.Named++
		} else {
			report.GatedIssues.Missed = append(report.GatedIssues.Missed, r.Sample.ID)
		}
	}
	sort.Slice(report.Applied, func(i, j int) bool { return report.Applied[i].Sample < report.Applied[j].Sample })
	sort.Slice(report.Wrong, func(i, j int) bool {
		if report.Wrong[i].Sample != report.Wrong[j].Sample {
			return report.Wrong[i].Sample < report.Wrong[j].Sample
		}
		return report.Wrong[i].Criterion < report.Wrong[j].Criterion
	})
	sort.Strings(report.Issues.Missed)
	sort.Strings(report.GatedIssues.Missed)
	return report, settings, nil
}

// IssueLines renders the judge report's biggest-issue block: the lines
// the evaluation prints about the two reads. Each read is named on its
// own line - the service's raw choice, and the flag-gated read
// pipeline.Judge reports - with the variants it misses, then one line
// per missed variant carrying both choices, so nothing downstream of
// this block (docs/evaluation.md quotes it) can quote a figure the
// block does not carry. The read each count belongs to is on the line
// with it; neither count is printed unlabelled.
func (r JudgeReport) IssueLines() []string {
	raw := fmt.Sprintf("biggest issue, the service's raw choice: names a miss (or none) on %d of %d variants",
		r.Issues.Named, r.Issues.Of)
	rawMissed := fmt.Sprintf("biggest issue, the service's raw choice, missed: %s",
		strings.Join(r.Issues.Missed, ", "))
	gated := fmt.Sprintf("biggest issue, gated on the look's own flags (what pipeline.Judge reports): names a miss (or none) on %d of %d variants",
		r.GatedIssues.Named, r.GatedIssues.Of)
	gatedMissed := fmt.Sprintf("biggest issue, gated on the look's own flags, missed: %s",
		strings.Join(r.GatedIssues.Missed, ", "))
	lines := []string{raw, rawMissed, gated, gatedMissed,
		"biggest issue, per variant, both reads, for every variant either read misses:"}
	missed := map[string]bool{}
	for _, m := range r.Issues.Missed {
		missed[m] = true
	}
	for _, m := range r.GatedIssues.Missed {
		missed[m] = true
	}
	byID := make(map[string]AppliedSample, len(r.Applied))
	for _, a := range r.Applied {
		byID[a.Sample] = a
	}
	names := make([]string, 0, len(missed))
	for name := range missed {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		a, ok := byID[name]
		if !ok {
			lines = append(lines, fmt.Sprintf("  %s: no applied read", name))
			continue
		}
		lines = append(lines, fmt.Sprintf("  %s: raw %s, gated %s", a.Sample, a.Issue, a.GatedIssue))
	}
	return lines
}
