package eval

// The live report arithmetic, lifted out of the live-tagged sweep (T3.9c).
//
// T3.9b's live sweep prints these numbers through live_test.go, a file the
// gate never builds: a break in the arithmetic only showed up after a paid
// run. Everything here is the same computation, pure: raw readings in,
// tables and counts out, no testing.T, no log line, no ledger. The tagged
// sweep calls these helpers for its report, and the pinned tests at the
// bottom of this file run them over the committed transcripts
// (testdata/eval/live) on every commit, with no tag, no key and no network.
//
// One call per recording yields both readings because pipeline.Thresholds
// and pipeline.Lines change only how the raw answers are read, never the
// request bytes - so the curves below score the same live cells at 0.5 and
// at the shipped lines from one read, and the sweep stays at 41 calls.

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/nrynss/dresscode/internal/pipeline"
	"github.com/nrynss/dresscode/internal/systemone"
	"github.com/nrynss/keel/cost"
)

// The rate card, and where each number comes from. The two neuron rates -
// 21,818 per million input tokens on clef, 3,455 on clef-flash - and the
// free plan's 10,000 neurons a day are docs/clef.md's (Cost). The dollar
// rate is not: $0.011 per 1,000 neurons is the client's own booking rate,
// internal/systemone/workersai.go (`nanodollarsPerNeuron`), the same figure
// T2.2 recorded in dev-diary/PHASE-2-clients.md.
//
// liveNeuronsPerMillion is a second copy of systemone's private table, which
// a test cannot read. The copy is tied by liveImpliedPrice: the live sweep's
// liveSpend.record works out what these rates would book for a call and
// compares it against what the ledger did book, fatalling the run on the
// first call where the two disagree. A rate card that drifted from the
// client's therefore stops the sweep instead of printing a row nobody
// compared. The pinned test re-runs the same comparison against the
// transcripts' booked prices, so the gate checks it too.
const liveNanodollarsPerNeuron = 11_000

// liveTokenCap mirrors systemone's private maxEstimateTokens, the cap on the
// token count its pricing arithmetic applies. This copy needs it for the
// same reason: so the implied price equals the booked one on any input, not
// only on the ones expected.
const liveTokenCap = 20_000_000

var liveNeuronsPerMillion = map[string]int64{
	systemone.ModelClef:      21_818,
	systemone.ModelClefFlash: 3_455,
}

// liveCall is one call's own accounting: what the response reported, what
// the documented rate implies, what the ledger booked.
type liveCall struct {
	label   string
	model   string
	tokens  int
	output  int
	neurons float64
	booked  cost.Price
}

// liveImpliedPrice is the client's own arithmetic: the neurons the
// documented rate implies for a call's reported input tokens, priced at
// liveNanodollarsPerNeuron and rounded up to the nanodollar - which is how
// internal/systemone/workersai.go prices a settle (tokenPrice, with the same
// cap on the token count).
func liveImpliedPrice(tokens int, neuronsPerMillion int64) cost.Price {
	if tokens > liveTokenCap {
		tokens = liveTokenCap
	}
	const scale = 1_000_000
	product := int64(tokens) * neuronsPerMillion * liveNanodollarsPerNeuron
	return cost.Price((product + scale - 1) / scale)
}

// liveNeurons is the documented rate applied to a call's reported input
// tokens, as a float for reporting. The client prices in exact integers,
// so the ledger total stays the money figure.
func liveNeurons(tokens int, perMillion int64) float64 {
	return float64(tokens) * float64(perMillion) / 1e6
}

// The totals over one pass's calls. Pure sums; the sweep's report and the
// pinned test read them the same way.

func liveCallsTokens(calls []liveCall) int64 {
	var total int64
	for _, c := range calls {
		total += int64(c.tokens)
	}
	return total
}

func liveCallsOutputTokens(calls []liveCall) int64 {
	var total int64
	for _, c := range calls {
		total += int64(c.output)
	}
	return total
}

func liveCallsNeurons(calls []liveCall) float64 {
	var total float64
	for _, c := range calls {
		total += c.neurons
	}
	return total
}

func liveCallsDollars(calls []liveCall) cost.Price {
	var total cost.Price
	for _, c := range calls {
		total += c.booked
	}
	return total
}

func liveCallsByModel(calls []liveCall) map[string]int {
	byModel := map[string]int{}
	for _, c := range calls {
		byModel[c.model]++
	}
	return byModel
}

// projectLiveSweep is the probe's projection: the sweep is 37 judge calls on
// clef, 2 checks on clef and 2 on clef-flash; the flash recordings send the
// same bytes as the clef ones, so the probe's token count prices them at the
// flash rate. The neurons are the per-call readings summed at full
// precision; the dollars are those neurons booked at the client's rate,
// truncated to the nanodollar exactly as the sweep's cost.Price conversion
// does. A projection that reaches the sweep ceiling or the free plan's day
// fails the probe - see TestLiveProbe in the tagged file.
func projectLiveSweep(neuronsPerJudgeCall, neuronsPerClefCheck, neuronsPerFlashCheck float64,
	judgeCalls, clefCheckCalls, flashCheckCalls int) (float64, cost.Price) {
	neurons := float64(judgeCalls)*neuronsPerJudgeCall +
		float64(clefCheckCalls)*neuronsPerClefCheck + float64(flashCheckCalls)*neuronsPerFlashCheck
	return neurons, cost.Price(neurons * liveNanodollarsPerNeuron)
}

// liveCounted renders a per-model call count for one log line.
func liveCounted(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%d on %s", counts[k], k))
	}
	return strings.Join(parts, ", ")
}

// sortOffCells puts a live cell list in the recorded report's own order:
// sample first, criterion second.
func sortOffCells(cells []OffCell) {
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].Sample != cells[j].Sample {
			return cells[i].Sample < cells[j].Sample
		}
		return cells[i].Criterion < cells[j].Criterion
	})
}

// liveIssues counts the live biggest-issue answers that name one of the
// variant's misses, or none when the variant misses nothing - the rule the
// recorded report scores with.
//
// That rule is a copy, not a call: this mirrors internal/eval/judge.go
// (the ground truth's shape for the biggest issue, counted into
// report.Issues). The extraction that would remove the duplication is a
// product-file edit and outside this task, so the copy stands. Read it as a
// standing risk: an edit to judge.go's issue count that does not come here
// leaves the live biggest-issue count scored by different rules than the
// recorded one, and the 28/37 beside 31/37 line is where the difference
// would show.
func liveIssues(reads []JudgeRead) int {
	named := 0
	for _, r := range reads {
		yes := false
		if len(r.Sample.Miss) == 0 {
			yes = r.Issue == "none"
		} else {
			for _, miss := range r.Sample.Miss {
				if r.Issue == miss {
					yes = true
					break
				}
			}
		}
		if yes {
			named++
		}
	}
	return named
}

// liveJudgeNumbers is everything the live judging report computes from the
// live curves beside the recorded report and the shipped lines. The sweep's
// liveReportJudge prints it; the pinned test asserts each field against the
// number docs/evaluation.md's live re-run subsection quotes.
type liveJudgeNumbers struct {
	// The headline totals, in cells of 273 and criteria of 36.
	LiveHalf     int // cells agreeing at 0.5
	LiveLines    int // cells agreeing at the shipped lines
	LiveMaxTotal int // the live curves' best total
	LiveApart    int // live criteria separating perfectly
	RecBestTotal int // recorded curves' best total, from the recorded report
	RecApart     int // recorded criteria separating perfectly
	CriteriaLost int // criteria the live reads put below the recorded line's count
	OffHalf      []OffCell
	Wrong        []OffCell
	Moved        []string
	Flipped      []string
	RecCurves    map[string]Curve
}

// computeLiveJudgeNumbers scores the live curves against the recorded
// report's cells at 0.5 and at the shipped lines, and names every cell that
// moved or crossed the line. A live criterion with no recorded curve, or a
// live cell with no recorded twin, is a disagreement between the two reads
// this file is built to compare, so it is an error, never a zero.
func computeLiveJudgeNumbers(liveCurves []Curve, recorded JudgeReport, settings JudgeSettings) (liveJudgeNumbers, map[string]float64, error) {
	var nums liveJudgeNumbers
	recValues := make(map[string]float64)
	nums.RecCurves = make(map[string]Curve, len(recorded.Curves))
	for _, c := range recorded.Curves {
		nums.RecCurves[c.ID] = c
		nums.RecBestTotal += c.MaxAgreement
		if c.Apart {
			nums.RecApart++
		}
		for _, cell := range c.Cells {
			recValues[cell.Sample+" "+c.ID] = cell.Value
		}
	}

	for _, c := range liveCurves {
		rec, ok := nums.RecCurves[c.ID]
		if !ok {
			return liveJudgeNumbers{}, nil, fmt.Errorf("no recorded curve for %q", c.ID)
		}
		line := settings.lineFor(c)
		half := agreement(c.Cells, readingAt)
		at := agreement(c.Cells, line)
		nums.LiveHalf += half
		nums.LiveLines += at
		nums.LiveMaxTotal += c.MaxAgreement
		if c.Apart {
			nums.LiveApart++
		}
		if agreement(rec.Cells, line) > at {
			nums.CriteriaLost++
		}
		for _, name := range offAt(c.Cells, readingAt) {
			cell, ok := findCell(c.Cells, name)
			if !ok {
				return liveJudgeNumbers{}, nil, fmt.Errorf("no cell for sample %q in %q's curve", name, c.ID)
			}
			nums.OffHalf = append(nums.OffHalf, OffCell{Sample: name, Criterion: c.ID,
				Value: cell.Value, Expected: cell.Expected})
		}
		for _, name := range offAt(c.Cells, line) {
			cell, ok := findCell(c.Cells, name)
			if !ok {
				return liveJudgeNumbers{}, nil, fmt.Errorf("no cell for sample %q in %q's curve", name, c.ID)
			}
			nums.Wrong = append(nums.Wrong, OffCell{Sample: name, Criterion: c.ID,
				Value: cell.Value, Expected: cell.Expected})
		}
		for _, cell := range c.Cells {
			recValue, known := recValues[cell.Sample+" "+c.ID]
			if !known {
				return liveJudgeNumbers{}, nil, fmt.Errorf("no recorded value for %s %s", cell.Sample, c.ID)
			}
			if cell.Value != recValue {
				nums.Moved = append(nums.Moved, fmt.Sprintf("%s %s: recorded %.4f, live %.4f, delta %+.4f",
					cell.Sample, c.ID, recValue, cell.Value, cell.Value-recValue))
			}
			if (cell.Value >= line) != (recValue >= line) {
				nums.Flipped = append(nums.Flipped, fmt.Sprintf("%s %s: recorded %.4f, live %.4f, line %.5f, expected met=%v",
					cell.Sample, c.ID, recValue, cell.Value, line, cell.Expected))
			}
		}
	}
	sort.Strings(nums.Moved)
	sort.Strings(nums.Flipped)
	sortOffCells(nums.OffHalf)
	sortOffCells(nums.Wrong)
	return nums, recValues, nil
}

// findCell reads one sample's cell out of a curve. offAt walks the same
// cells, so a miss is a bug in the caller rather than a state the sweep can
// be in.
func findCell(cells []Cell, sample string) (Cell, bool) {
	for _, c := range cells {
		if c.Sample == sample {
			return c, true
		}
	}
	return Cell{}, false
}

// liveDrift is the cell-by-cell move of the live probabilities against the
// recorded ones: how many cells moved and by how much. The mean and the
// buckets are what docs/evaluation.md quotes ("the mean move is 0.08, 140
// stay inside 0.05, 74 move 0.10 or more and three move past 0.45").
type liveDrift struct {
	Cells     int
	Identical []string // "sample criterion" cells whose live value equals the recorded one
	Differing int
	MeanMove  float64 // mean |live - recorded| over all cells
	// The buckets docs/evaluation.md counts: moves within 0.05 (the
	// identical cells included), moves of 0.10 or more, and moves past
	// 0.45. No committed cell sits exactly on a boundary, so the
	// comparisons are the doc's own wording read literally.
	Inside05  int
	AtLeast10 int
	Past045   int
	// The largest single move, named by cell.
	LargestCell  string
	LargestFrom  float64
	LargestTo    float64
	LargestDelta float64
}

// measureLiveDrift compares every live cell against its recorded twin.
func measureLiveDrift(liveCurves []Curve, recValues map[string]float64) (liveDrift, error) {
	var d liveDrift
	var sum float64
	for _, c := range liveCurves {
		for _, cell := range c.Cells {
			d.Cells++
			recValue, known := recValues[cell.Sample+" "+c.ID]
			if !known {
				return liveDrift{}, fmt.Errorf("no recorded value for %s %s", cell.Sample, c.ID)
			}
			move := math.Abs(cell.Value - recValue)
			if move == 0 {
				d.Identical = append(d.Identical, cell.Sample+" "+c.ID)
			} else {
				d.Differing++
			}
			sum += move
			if move <= 0.05 {
				d.Inside05++
			}
			if move >= 0.10 {
				d.AtLeast10++
			}
			if move > 0.45 {
				d.Past045++
			}
			if move > d.LargestDelta {
				d.LargestDelta = move
				d.LargestCell = cell.Sample + " " + c.ID
				d.LargestFrom = recValue
				d.LargestTo = cell.Value
			}
		}
	}
	if d.Cells > 0 {
		d.MeanMove = sum / float64(d.Cells)
	}
	sort.Strings(d.Identical)
	return d, nil
}

// liveGateRow is one gate on one model read against the live check calls:
// the honest and the known-bad value, the shipped line's separation and
// margin, the live curve's own midpoint, and the recorded curve's margin
// printed beside it for the move.
type liveGateRow struct {
	Gate   string
	Model  string
	Honest float64
	Bad    float64
	Line   float64
	// Margin is the shipped line's distance to the nearer live read.
	Margin        float64
	Separates     bool    // honest at or above the line, bad under it
	SeparatesHalf bool    // the same call at the 0.5 reading
	LiveMid       float64 // the live curve's own plateau midpoint
	RecMargin     float64 // the recorded report's margin for this gate and model
	Differs       bool
}

// computeLiveGateRows builds the gate table the live report prints, from
// the live reads beside the recorded report and the shipped lines.
func computeLiveGateRows(reads []CheckRead, recorded CheckReport, lines pipeline.Lines) ([]liveGateRow, error) {
	lineOf := map[string]float64{
		"change_made":   lines.ChangeMade,
		"same_person":   lines.SamePerson,
		"criterion_met": lines.CriterionMet,
	}
	var rows []liveGateRow
	for _, name := range gateNames {
		for _, model := range liveReadModels(reads) {
			good, bad := 0.0, 0.0
			honest, proxy := false, false
			for _, r := range reads {
				if r.Model != model {
					continue
				}
				v, err := checkGateValue(r, name)
				if err != nil {
					return nil, err
				}
				if r.Bad {
					bad, proxy = v, true
				} else {
					good, honest = v, true
				}
			}
			if !(honest && proxy) {
				return nil, fmt.Errorf("no live check read pair on model %q for %s (honest %v, bad %v)", model, name, honest, proxy)
			}
			line := lineOf[name]
			cells := []Cell{
				{Sample: "honest", Value: good, Expected: true},
				{Sample: "bad", Value: bad, Expected: false},
			}
			margin := nearestDistance(cells, line)
			curve := gateCurve(name, model, good, bad)
			separates := good >= line && bad < line
			half := good >= readingAt && bad < readingAt
			// The recorded gate this row prints beside. The live set runs on
			// the same three gates and the same two models, so a missing row
			// is a disagreement between the two reports, not a state to
			// print a zero margin for.
			var recMargin float64
			recFound := false
			for _, g := range recorded.Gates {
				if g.Gate == name && g.Model == model {
					recMargin, recFound = g.Margin, true
				}
			}
			if !recFound {
				return nil, fmt.Errorf("no recorded gate %q on model %q among the recorded report's %d gate rows", name, model, len(recorded.Gates))
			}
			rows = append(rows, liveGateRow{
				Gate: name, Model: model, Honest: good, Bad: bad, Line: line,
				Margin: margin, Separates: separates, SeparatesHalf: half,
				LiveMid: curve.Chosen, RecMargin: recMargin,
				Differs: !separates || !half || recMargin != margin,
			})
		}
	}
	return rows, nil
}

// checkGateValue names one gate off a read. The gate names come from
// gateNames, so an unrecognised one is a bug in the caller, not a state the
// sweep can be in.
func checkGateValue(r CheckRead, name string) (float64, error) {
	switch name {
	case "change_made":
		return r.Change, nil
	case "same_person":
		return r.Same, nil
	case "criterion_met":
		return r.Met, nil
	}
	return 0, fmt.Errorf("unknown gate %q; gateNames and this switch disagree", name)
}

// liveReadModels lists the models the live check reads ran on, sorted.
func liveReadModels(reads []CheckRead) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range reads {
		if !seen[r.Model] {
			seen[r.Model] = true
			out = append(out, r.Model)
		}
	}
	sort.Strings(out)
	return out
}

// liveTruthModels is the same list from the ground truth, so the
// vocabulary count can be cut per model.
func liveTruthModels(checks []CheckTruth) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range checks {
		if !seen[c.Model] {
			seen[c.Model] = true
			out = append(out, c.Model)
		}
	}
	sort.Strings(out)
	return out
}

// liveModelByExchange maps each recorded check exchange to its model, so
// the vocabulary cells can be cut per model.
func liveModelByExchange(checks []CheckTruth) map[string]string {
	m := make(map[string]string, len(checks))
	for _, c := range checks {
		m[c.Exchange] = c.Model
	}
	return m
}

// liveCheckDecision is one live check read's decision at the shipped lines,
// as the real pipeline made it.
type liveCheckDecision struct {
	Exchange string
	Model    string
	Bad      bool
	Shown    bool
	Words    string
}

// liveDecisionCounts counts the two numbers the check report is pinned on:
// known bad renders the shipped lines showed, and honest renders they
// discarded. Both must stay zero.
func liveDecisionCounts(decisions []liveCheckDecision) (badShown, honestDiscarded int) {
	for _, d := range decisions {
		if d.Bad && d.Shown {
			badShown++
		}
		if !d.Bad && !d.Shown {
			honestDiscarded++
		}
	}
	return badShown, honestDiscarded
}

// liveVocabTally is one model's vocabulary score, live beside recorded.
type liveVocabTally struct {
	Model       string
	LiveCorrect int
	LiveOf      int
	RecCorrect  int
	RecOf       int
}

// tallyLiveVocabulary counts correct cells per model over the live and the
// recorded vocabulary reads. An answer whose exchange is not in the ground
// truth's checks would silently drop a cell from both per-model counts, so
// it is an error, not a skip.
func tallyLiveVocabulary(models []string, live, rec []VocabularyCell, modelOf map[string]string) ([]liveVocabTally, error) {
	tallies := make([]liveVocabTally, 0, len(models))
	for _, model := range models {
		ta := liveVocabTally{Model: model}
		for _, c := range live {
			m, ok := modelOf[c.Exchange]
			if !ok {
				return nil, fmt.Errorf("no ground truth check for exchange %q", c.Exchange)
			}
			if m != model {
				continue
			}
			ta.LiveOf++
			if c.Correct {
				ta.LiveCorrect++
			}
		}
		for _, c := range rec {
			m, ok := modelOf[c.Exchange]
			if !ok {
				return nil, fmt.Errorf("no ground truth check for exchange %q", c.Exchange)
			}
			if m != model {
				continue
			}
			ta.RecOf++
			if c.Correct {
				ta.RecCorrect++
			}
		}
		tallies = append(tallies, ta)
	}
	return tallies, nil
}

// liveVocabDiff is one vocabulary cell whose live answer differs from the
// recorded one.
type liveVocabDiff struct {
	Exchange  string
	Attribute string
	Recorded  string
	Live      string
}

// diffLiveVocabulary names every cell the live reads answered differently
// from the recorded ones. A live cell with no recorded twin is a
// disagreement between the two reads, so it is an error.
func diffLiveVocabulary(live, rec []VocabularyCell) ([]liveVocabDiff, error) {
	recByKey := make(map[string]VocabularyCell, len(rec))
	for _, c := range rec {
		recByKey[c.Exchange+" "+c.Attribute] = c
	}
	var diffs []liveVocabDiff
	for _, c := range live {
		key := c.Exchange + " " + c.Attribute
		rc, ok := recByKey[key]
		if !ok {
			return nil, fmt.Errorf("no recorded vocabulary cell for %s %s", c.Exchange, c.Attribute)
		}
		if rc.Answer != c.Answer || rc.Correct != c.Correct {
			diffs = append(diffs, liveVocabDiff{Exchange: c.Exchange, Attribute: c.Attribute,
				Recorded: rc.Answer, Live: c.Answer})
		}
	}
	sort.Slice(diffs, func(i, j int) bool {
		if diffs[i].Exchange != diffs[j].Exchange {
			return diffs[i].Exchange < diffs[j].Exchange
		}
		return diffs[i].Attribute < diffs[j].Attribute
	})
	return diffs, nil
}

// liveVocabularyMisses renders a vocabulary read's wrong answers, sorted:
// the doc's "answered X, true Y" list.
func liveVocabularyMisses(cells []VocabularyCell) []string {
	var misses []string
	for _, c := range cells {
		if !c.Correct {
			misses = append(misses, fmt.Sprintf("%s %s: answered %s, true %s",
				c.Exchange, c.Attribute, c.Answer, c.Truth))
		}
	}
	sort.Strings(misses)
	return misses
}
