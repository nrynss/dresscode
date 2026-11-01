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
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

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

// The run-shape constants, lifted so the pinned tests project and price
// against the same numbers the tagged sweep runs on. None of them is a
// threshold: the lines the sweep measures with are read from the committed
// settings file.

// liveProbeCeiling covers the probe's two calls. liveSweepCeiling is
// 8 cents, about four times the sweep's expected settle (~1,950 neurons,
// ~$0.022): the client reserves double its token estimate for each call,
// so the ceiling sits above the expected settle, not at it. The meter
// refuses a call whose reserve does not fit, which is the guard that stops
// a runaway loop.
const (
	liveProbeCeiling = cost.Cent
	liveSweepCeiling = 8 * cost.Cent
)

// liveFreeDailyNeurons is the Workers AI free plan's day allowance
// (docs/clef.md, Cost). The sweep is projected against it.
const liveFreeDailyNeurons = 10_000

// liveJudgeCalls and liveCheckCalls are the sweep's expected calls: every
// judge recording and every check recording, once each.
const (
	liveJudgeCalls = 37
	liveCheckCalls = 4
)

// liveOrdering states what the sweep does not do: no t.Parallel, one call
// after another, so the ledger and the budget tell one story.
const liveOrdering = "sequential calls, no t.Parallel, fail fast on the first call error"

// The probe's two calls: the full-size family-function judge recording and
// the honest second-visit check on the shipped model.
const (
	liveProbeJudge = "judge-V04-family-function"
	liveProbeCheck = "check-clef-V04-to-V05"
)

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
	// PlateauLow and PlateauHigh are the live curve's separating interval:
	// every line strictly inside it decides both reads correctly. The
	// shipped line staying inside it is the "no line outside its
	// interval" claim docs/evaluation.md makes about the live set.
	PlateauLow  float64
	PlateauHigh float64
	RecMargin   float64 // the recorded report's margin for this gate and model
	Differs     bool
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
				LiveMid: curve.Chosen, PlateauLow: curve.PlateauLow, PlateauHigh: curve.PlateauHigh,
				RecMargin: recMargin,
				Differs:   !separates || !half || recMargin != margin,
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

// ---- The committed transcripts, read back into the raw readings ----
//
// testdata/eval/live holds the transcripts of T3.9b's three paid runs:
// probe.txt (the 2-call probe) and sweep-1.txt / sweep-2.txt (the two
// 41-call passes). They are the pinned arithmetic's input: the parser
// below lifts the log lines back into the shapes the helpers take, the
// tests re-run the computation and compare against the transcript line by
// line, verbatim. A number docs/evaluation.md's live re-run subsection
// quotes is either re-emitted exactly (transcript-verbatim items) or
// recomputed by the same formula the print used (rounded displays), so a
// change in the arithmetic, the settings or the recordings fails the gate
// without a key, a tag, a call or a cent.

// liveLogLinePrefix matches the testing log's prefix on a transcript line.
// Stripping it leaves the print's own payload, indentation included, which
// is what the re-emitted lines are compared against (right padding
// trimmed on both sides, since t.Log keeps it but the eye does not).
var liveLogLinePrefix = regexp.MustCompile(`^\s*live_test\.go:\d+: `)

// liveWordsPattern lifts the words a decision printed, quotes included.
var liveWordsPattern = regexp.MustCompile(`words="(.*)"`)

type liveTranscript struct {
	name  string
	lines []string // payloads, prefix stripped, right-padded spaces trimmed
}

func readLiveTranscript(t *testing.T, name string) liveTranscript {
	t.Helper()
	path := filepath.Join(repoRoot(t), "testdata", "eval", "live", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("live pin: read %s: %v", path, err)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if !liveLogLinePrefix.MatchString(line) {
			continue // RUN/PASS/ok header and timing lines carry no payload
		}
		payload := liveLogLinePrefix.ReplaceAllString(line, "")
		out = append(out, strings.TrimRight(payload, " "))
	}
	return liveTranscript{name: name, lines: out}
}

// lineWith finds the first payload line whose trimmed text starts at
// prefix, and returns it verbatim for whole-line comparisons.
func (tr liveTranscript) lineWith(prefix string) (string, bool) {
	for _, ln := range tr.lines {
		if strings.HasPrefix(strings.TrimSpace(ln), prefix) {
			return ln, true
		}
	}
	return "", false
}

// The print formats the tagged sweep uses, mirrored here so the pinned
// tests re-emit every transcript line from the computed values and compare
// verbatim. These are presentation, not arithmetic: the arithmetic lives
// in the helpers above, and both the sweep and these tests call it.
const (
	liveJudgeRowFormat    = "live judge %-32s issue=%-24s tokens=%d neurons=%.2f booked=%s"
	liveCheckRowFormat    = "live check %-36s change=%.4f same=%.4f met=%.4f shown=%-5v tokens=%d booked=%s"
	liveCellRowFormat     = "    %-30s %-28s recorded=%.4f live=%.4f line=%.5f expected-met=%-5v live-agrees=%-5v"
	liveCriterionFormat   = "  %-32s %3d %6d/%-3d %6d/%-3d %8d/%-3d %8d/%-3d %10.5f %5t/%-5t%s"
	liveOffCellFormat     = "      %s %-28s value=%.4f expected-met=%-5v"
	liveGateRowFormat     = "  %-14s %-12s %8.4f %8.4f %10.5f %8.4f %10v %8v %10.5f %10.4f%s"
	liveReadRowFormat     = "  %-36s %-12s %6v %9.4f %9.4f %9.4f   %.4f / %.4f / %.4f"
	liveDecisionFormat    = "    %-36s %-12s bad=%-5v shown live=%-5v shown recorded=%-5v%s words=%q"
	liveVocabTallyFormat  = "    %-12s live %d/%d correct, recorded %d/%d"
	liveVocabCellFormat   = "      %-36s %-16s truth=%-18s live=%-18s recorded=%-18s correct=%-5v%s"
	liveCostRowFormat     = "  %-36s %-12s %8d %10.2f %14s"
	liveHeadlineFormat    = "  %-46s %-14s %d/%d"
	liveMarkDiffers       = "   <-- differs from recorded"
	probeCostRowFormat    = "  %-32s %-11s %8d %10.2f %14s"
	probeJudgeLineFormat  = "probe judge %s read %d criteria and named the biggest issue %q"
	probeCheckLineFormat  = "probe check %s gates %.4f / %.4f / %.4f; shown at the shipped lines: %v"
	probeProjectionFormat = "  projected sweep: %d judge x %.2f + 2 clef check x %.2f + 2 flash check x %.2f = %.0f neurons = %s"
	probeCeilingLine      = "  sweep ceiling %s; the projection fits under it: %v"
	probeFreeLine         = "  free plan allows %d neurons a day; the projection fits under it: %v"
	probeBookedLine       = "  probe booked %d charges, total %s; budget spent %s, remaining %s"
	probeTwoCallsLine     = "probe: two calls, one judge and one check, %s"

	// The value-bearing headers and summaries, same mirror discipline.
	liveJudgingHeaderFormat = "JUDGING: live against recorded (%d samples, %d cells)"
	liveVocabHeaderFormat   = "  LOOK VOCABULARY (%d cells: %d attributes over %d reads)"
	liveDriftFormat         = "  drift: mean |move| %.4f over %d cells; %d inside 0.05, %d at 0.10 or more, %d past 0.45"
	liveLargestFormat       = "  largest move: %s recorded %.4f, live %.4f"
	liveIdenticalFormat     = "  cells identical live and recorded: %s"
	liveMovedCountFormat    = "  cells whose live value differs from the recorded one: %d of %d"
	liveFlippedCountFormat  = "  cells that sit on the other side of the shipped line live than recorded: %d"
	liveOffHeaderFmt        = "  CELLS OFF THE EXPECTED SIDE AT 0.5 - recorded %d, live %d"
	liveWrongHeaderFmt      = "  IRREDUCIBLE CELLS AT THE SHIPPED LINE - recorded %d, live %d (no line on this set saves them)"
	liveMovedBlockFmt       = "  LIVE VALUES THAT DIFFER FROM THE RECORDED ONES: %d of %d cells"
	liveFlippedBlockFmt     = "  CELLS ON THE OTHER SIDE OF THE SHIPPED LINE LIVE THAN RECORDED: %d"
	liveReproduceFmt        = "    the cells off the expected side at %s reproduce exactly: %v"
	liveOffOnlyLiveFmt      = "      off at %s live only: %s"
	liveOffOnlyRecFmt       = "      off at %s recorded only: %s"
	liveDecRecFormat        = "    recorded: known bad renders shown %d, honest renders discarded %d"
	liveDecLiveFormat       = "    live:     known bad renders shown %d, honest renders discarded %d"
	liveDecBothFormat       = "    every known bad render rejected and no honest render lost - recorded %v, live %v"
	liveCallsMadeFormat     = "  calls made: %d (%s)"
	liveTokensLineFormat    = "  input tokens reported: %d, output tokens %d"
	liveNeuronsLineFormat   = "  neurons implied by the documented rates: %.0f"
	liveDollarsLineFormat   = "  dollars booked by the ledger: %s (sum of the calls %s)"
	liveBudgetLineFormat    = "  budget ceiling %s, spent %s, remaining %s"
	liveFreeAllowFormat     = "  free plan daily allowance %d neurons; the sweep stayed under it: %v"
	liveSweepOpenFormat     = "live sweep: %d judge calls and %d check calls on Workers AI, %s"
	liveShippedReadFormat   = "shipped lines read from dresscode.toml.example: judge met %.5f over %d overrides; " +
		"check change_made %.5f same_person %.5f criterion_met %.5f model %q"
	liveConsistencyLine = "consistency: the shipped [judge] and [check] numbers equal the offline evaluation's chosen settings: true"
)

func liveFloat(t *testing.T, name, text string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		t.Fatalf("live pin: %s: parse %q: %v", name, text, err)
	}
	return v
}

func liveInt(t *testing.T, name, text string) int {
	t.Helper()
	v, err := strconv.Atoi(text)
	if err != nil {
		t.Fatalf("live pin: %s: parse %q: %v", name, text, err)
	}
	return v
}

func liveBool(t *testing.T, name, text string) bool {
	t.Helper()
	switch text {
	case "true":
		return true
	case "false":
		return false
	}
	t.Fatalf("live pin: %s: not a bool: %q", name, text)
	return false
}

func liveFraction(t *testing.T, name, text string) (num, den int) {
	t.Helper()
	n, d, ok := strings.Cut(text, "/")
	if !ok {
		t.Fatalf("live pin: %s: not a fraction: %q", name, text)
	}
	return liveInt(t, name, n), liveInt(t, name, d)
}

// parseLivePrice reads a printed $-price back to nanodollars. The
// transcript's prices are exact decimal strings; this splits on the point
// instead of routing through a float so no rounding can move a digit.
func parseLivePrice(t *testing.T, name, text string) cost.Price {
	t.Helper()
	rest, ok := strings.CutPrefix(text, "$")
	if !ok {
		t.Fatalf("live pin: %s: %q is not a $-price", name, text)
	}
	whole, frac := "0", rest
	if i := strings.IndexByte(rest, '.'); i >= 0 {
		whole, frac = rest[:i], rest[i+1:]
	}
	if len(frac) > 9 || whole == "" || whole == "-" {
		t.Fatalf("live pin: %s: %q is not a nanodollar price", name, text)
	}
	frac = frac + strings.Repeat("0", 9-len(frac))
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		t.Fatalf("live pin: %s: %q whole part: %v", name, text, err)
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		t.Fatalf("live pin: %s: %q fraction: %v", name, text, err)
	}
	return cost.Price(w*1_000_000_000 + f)
}

// liveTranscriptData is one sweep (or probe) transcript lifted into the
// shapes the helpers consume. Every row keeps its payload line, so the
// tests compare the re-emitted print verbatim as well as the value behind
// it; the sections the lists live in are picked up by their headers.
type liveTranscriptData struct {
	name         string   // the transcript file, for failure messages
	lines        []string // every payload line, for the single-line lookups
	judgeRows    []liveJudgeRow
	checkRows    []liveCheckRow
	headlines    map[string]string // trimmed label -> payload line
	cellRows     []liveCellRow
	criterion    []liveCriterionRow
	gateRows     []liveGateRowText
	readRows     []liveReadRowText
	decisionRows []liveDecisionRow
	tallyRows    []liveVocabTallyRow
	vocabRows    []liveVocabCellRow
	costRows     []liveCostRow

	movedRows    []string // the delta lines, list indent trimmed
	flippedRows  []string // the crossed-the-line lines, likewise
	offHalf      liveCellList
	wrong        liveCellList
	missRecorded []string // the recorded vocabulary misses, indent trimmed
	missLive     []string // the live vocabulary misses, likewise

	offHalfHeader   string // "CELLS OFF THE EXPECTED SIDE AT 0.5 - recorded 13, live 19"
	wrongHeader     string
	movedHeader     string
	flippedHeader   string
	judgeCallCount  int
	checkedExchange map[string]liveCheckRow
}

// lineWith finds the first payload line whose trimmed text starts at
// prefix, for the single-line blocks (the probe's projection, the cost
// totals) the row shapes do not cover.
func (d liveTranscriptData) lineWith(prefix string) (string, bool) {
	for _, ln := range d.lines {
		if strings.HasPrefix(strings.TrimSpace(ln), prefix) {
			return ln, true
		}
	}
	return "", false
}

// linesWith collects every payload line whose trimmed text starts at
// prefix, trimmed of the indent: the comparison lines the sweep prints
// under a heading (the off-list "live only" entries) come in lists.
func (d liveTranscriptData) linesWith(prefix string) []string {
	var out []string
	for _, ln := range d.lines {
		if trimmed := strings.TrimSpace(ln); strings.HasPrefix(trimmed, prefix) {
			out = append(out, trimmed)
		}
	}
	return out
}

// headline reads one captured single-line value, failing the test when the
// transcript does not carry it: a missing headline is the print changing,
// which is exactly what these pins exist to catch.
func (d liveTranscriptData) headline(t *testing.T, key string) string {
	t.Helper()
	ln, ok := d.headlines[key]
	if !ok {
		t.Fatalf("live pin: %s: the transcript carries no %q line", d.name, key)
	}
	return ln
}

// liveCellList is one off-cell block's recorded and live lists, payload
// lines kept verbatim for the re-emission.
type liveCellList struct {
	recorded []string
	live     []string
}

type liveJudgeRow struct {
	raw     string
	label   string
	issue   string
	tokens  int
	neurons float64
	booked  string
}

type liveCheckRow struct {
	raw      string
	exchange string
	change   float64
	same     float64
	met      float64
	shown    bool
	tokens   int
	booked   string
}

type liveCellRow struct {
	raw       string
	sample    string
	criterion string
	recorded  string
	live      float64
	line      string
	expected  bool
	agrees    bool
}

type liveCriterionRow struct {
	raw      string
	id       string
	n        int
	recBest  string
	liveBest string
	recAt    string
	liveAt   string
	line     string
	aparts   string
	differs  bool
}

type liveGateRowText struct {
	raw       string
	gate      string
	model     string
	honest    string
	bad       string
	line      string
	margin    string
	separates bool
	atHalf    bool
	mid       string
	recMargin string
	differs   bool
}

type liveReadRowText struct {
	raw       string
	exchange  string
	model     string
	bad       bool
	change    string
	same      string
	met       string
	recTriple string // the recorded "0.9866 / 0.9066 / 0.9806" tail
}

type liveDecisionRow struct {
	raw       string
	exchange  string
	model     string
	bad       bool
	shownLive bool
	shownRec  bool
	words     string
}

type liveVocabTallyRow struct {
	raw      string
	model    string
	live     string
	recorded string
}

type liveVocabCellRow struct {
	raw       string
	exchange  string
	attribute string
	truth     string
	answer    string
	recorded  string
	correct   bool
	differs   bool
}

type liveCostRow struct {
	raw     string
	label   string
	model   string
	tokens  int
	neurons float64
	booked  string
}

func parseLiveTranscript(t *testing.T, name string) liveTranscriptData {
	t.Helper()
	tr := readLiveTranscript(t, name)
	var d liveTranscriptData
	d.name = name
	d.lines = tr.lines
	d.headlines = map[string]string{}
	d.checkedExchange = map[string]liveCheckRow{}

	// The list sections their headers open. A blank line closes them.
	const (
		secNone = iota
		secOffRecorded
		secOffLive
		secWrongRecorded
		secWrongLive
		secMoved
		secFlipped
		secMissRecorded
		secMissLive
	)
	section := secNone

	for _, ln := range tr.lines {
		trimmed := strings.TrimSpace(ln)
		f := strings.Fields(ln)

		switch {
		case len(f) == 7 && f[0] == "live" && f[1] == "judge":
			d.judgeRows = append(d.judgeRows, liveJudgeRow{
				raw: ln, label: f[2], issue: strings.TrimPrefix(f[3], "issue="),
				tokens:  liveInt(t, tr.name, strings.TrimPrefix(f[4], "tokens=")),
				neurons: liveFloat(t, tr.name, strings.TrimPrefix(f[5], "neurons=")),
				booked:  strings.TrimPrefix(f[6], "booked="),
			})
			continue
		case len(f) == 9 && f[0] == "live" && f[1] == "check":
			row := liveCheckRow{
				raw:      ln,
				exchange: f[2],
				change:   liveFloat(t, tr.name, strings.TrimPrefix(f[3], "change=")),
				same:     liveFloat(t, tr.name, strings.TrimPrefix(f[4], "same=")),
				met:      liveFloat(t, tr.name, strings.TrimPrefix(f[5], "met=")),
				shown:    liveBool(t, tr.name, strings.TrimPrefix(f[6], "shown=")),
				tokens:   liveInt(t, tr.name, strings.TrimPrefix(f[7], "tokens=")),
				booked:   strings.TrimPrefix(f[8], "booked="),
			}
			d.checkRows = append(d.checkRows, row)
			d.checkedExchange[row.exchange] = row
			continue
		case len(f) == 7 && strings.HasPrefix(f[2], "recorded=") && strings.HasPrefix(f[3], "live="):
			d.cellRows = append(d.cellRows, liveCellRow{
				raw: ln, sample: f[0], criterion: f[1],
				recorded: strings.TrimPrefix(f[2], "recorded="),
				live:     liveFloat(t, tr.name, strings.TrimPrefix(f[3], "live=")),
				line:     strings.TrimPrefix(f[4], "line="),
				expected: liveBool(t, tr.name, strings.TrimPrefix(f[5], "expected-met=")),
				agrees:   liveBool(t, tr.name, strings.TrimPrefix(f[6], "live-agrees=")),
			})
			continue
		case len(f) >= 10 && f[7] == "/" && f[9] == "/" && (f[2] == "true" || f[2] == "false"):
			// The check's read table: the recorded triple trails the
			// live values with two slash separators.
			d.readRows = append(d.readRows, liveReadRowText{
				raw: ln, exchange: f[0], model: f[1],
				bad:    liveBool(t, tr.name, f[2]),
				change: f[3], same: f[4], met: f[5],
				recTriple: strings.Join(f[6:], " "),
			})
			continue
		case (len(f) == 10 || len(f) == 14) && liveHasName(gateNames, f[0]):
			d.gateRows = append(d.gateRows, liveGateRowText{
				raw: ln, gate: f[0], model: f[1], honest: f[2], bad: f[3], line: f[4],
				margin: f[5], separates: liveBool(t, tr.name, f[6]), atHalf: liveBool(t, tr.name, f[7]),
				mid: f[8], recMargin: f[9], differs: len(f) > 10,
			})
			continue
		case len(f) == 8 || len(f) == 12:
			// The per-criterion table: eight columns, the mark trailing
			// when it differs.
			if _, err := strconv.Atoi(f[1]); err == nil && strings.Contains(f[7], "/") {
				d.criterion = append(d.criterion, liveCriterionRow{
					raw: ln, id: f[0], n: liveInt(t, tr.name, f[1]),
					recBest: f[2], liveBest: f[3], recAt: f[4], liveAt: f[5],
					line: f[6], aparts: f[7], differs: len(f) > 8,
				})
				continue
			}
		case len(f) >= 7 && strings.HasPrefix(f[2], "bad=") && f[3] == "shown" &&
			strings.HasPrefix(f[4], "live=") && f[5] == "shown" && strings.HasPrefix(f[6], "recorded="):
			m := liveWordsPattern.FindStringSubmatch(trimmed)
			if m == nil {
				t.Fatalf("live pin: %s: decision line without words: %q", tr.name, trimmed)
			}
			d.decisionRows = append(d.decisionRows, liveDecisionRow{
				raw: ln, exchange: f[0], model: f[1],
				bad:       liveBool(t, tr.name, strings.TrimPrefix(f[2], "bad=")),
				shownLive: liveBool(t, tr.name, strings.TrimPrefix(f[4], "live=")),
				shownRec:  liveBool(t, tr.name, strings.TrimPrefix(f[6], "recorded=")),
				words:     m[1],
			})
			continue
		case len(f) == 6 && f[1] == "live" && f[3] == "correct,":
			d.tallyRows = append(d.tallyRows, liveVocabTallyRow{
				raw: ln, model: f[0], live: f[2], recorded: f[5],
			})
			continue
		case (len(f) == 6 || len(f) == 10) && strings.HasPrefix(f[2], "truth=") &&
			strings.HasPrefix(f[3], "live=") && strings.HasPrefix(f[4], "recorded="):
			d.vocabRows = append(d.vocabRows, liveVocabCellRow{
				raw: ln, exchange: f[0], attribute: f[1],
				truth:    strings.TrimPrefix(f[2], "truth="),
				answer:   strings.TrimPrefix(f[3], "live="),
				recorded: strings.TrimPrefix(f[4], "recorded="),
				correct:  liveBool(t, tr.name, strings.TrimPrefix(f[5], "correct=")),
				differs:  len(f) > 6,
			})
			continue
		case len(f) == 5 && strings.HasPrefix(f[4], "$") &&
			(strings.HasPrefix(f[0], "judge-") || strings.HasPrefix(f[0], "check-")):
			d.costRows = append(d.costRows, liveCostRow{
				raw: ln, label: f[0], model: f[1],
				tokens:  liveInt(t, tr.name, f[2]),
				neurons: liveFloat(t, tr.name, f[3]), booked: f[4],
			})
			continue
		}

		// Headers and list bodies: the shapes above catch none of these.
		switch {
		case strings.HasPrefix(trimmed, "cells agreeing at 0.5"):
			d.headlines["agreeHalf"] = ln
		case strings.HasPrefix(trimmed, "cells agreeing at the shipped lines"):
			d.headlines["agreeLines"] = ln
		case strings.HasPrefix(trimmed, "curves' best total"):
			d.headlines["bestTotal"] = ln
		case strings.HasPrefix(trimmed, "criteria separating perfectly"):
			d.headlines["apart"] = ln
		case strings.HasPrefix(trimmed, "biggest issue names a miss"):
			d.headlines["issues"] = ln
		case strings.HasPrefix(trimmed, "cells whose live value differs from the recorded one"):
			d.headlines["movedOf"] = ln
		case strings.HasPrefix(trimmed, "cells that sit on the other side of the shipped line"):
			d.headlines["flippedCount"] = ln
		case strings.HasPrefix(trimmed, "drift: mean |move|"):
			d.headlines["drift"] = ln
		case strings.HasPrefix(trimmed, "largest move:"):
			d.headlines["largest"] = ln
		case strings.HasPrefix(trimmed, "cells identical live and recorded:"):
			d.headlines["identical"] = ln
		case strings.HasPrefix(trimmed, "calls made:"):
			d.headlines["callsMade"] = ln
		case strings.HasPrefix(trimmed, "input tokens reported:"):
			d.headlines["tokensReported"] = ln
		case strings.HasPrefix(trimmed, "neurons implied by the documented rates:"):
			d.headlines["neuronsImplied"] = ln
		case strings.HasPrefix(trimmed, "dollars booked by the ledger:"):
			d.headlines["dollarsBooked"] = ln
		case strings.HasPrefix(trimmed, "budget ceiling"):
			d.headlines["budget"] = ln
		case strings.HasPrefix(trimmed, "free plan daily allowance"):
			d.headlines["freeAllowance"] = ln
		case strings.HasPrefix(trimmed, "the cells off the expected side at 0.5 reproduce"):
			d.headlines["reproHalf"] = ln
		case strings.HasPrefix(trimmed, "the cells off the expected side at the shipped line reproduce"):
			d.headlines["reproLine"] = ln
		case strings.HasPrefix(trimmed, "every known bad render rejected"):
			d.headlines["rejected"] = ln
		case strings.HasPrefix(trimmed, "recorded: known bad renders shown"):
			d.headlines["decRec"] = ln
		case strings.HasPrefix(trimmed, "live:     known bad renders shown"):
			d.headlines["decLive"] = ln
		case strings.HasPrefix(trimmed, "the recorded vocabulary misses reproduce"),
			strings.HasPrefix(trimmed, "the vocabulary misses differ"):
			d.headlines["vocabRepro"] = ln
		case liveClosesSection(trimmed):
			section = secNone
		case strings.HasPrefix(trimmed, "CELLS OFF THE EXPECTED SIDE AT 0.5"):
			d.offHalfHeader, section = trimmed, secNone
		case strings.HasPrefix(trimmed, "IRREDUCIBLE CELLS AT THE SHIPPED LINE"):
			d.wrongHeader, section = trimmed, secNone
		case strings.HasPrefix(trimmed, "LIVE VALUES THAT DIFFER FROM THE RECORDED ONES"):
			d.movedHeader, section = trimmed, secMoved
		case strings.HasPrefix(trimmed, "CELLS ON THE OTHER SIDE OF THE SHIPPED LINE"):
			d.flippedHeader, section = trimmed, secFlipped
		case trimmed == "recorded (docs/clef.md's thirteen):":
			section = secOffRecorded
		case trimmed == "live:" && section == secOffRecorded:
			section = secOffLive
		case trimmed == "recorded:" && section == secNone:
			section = secWrongRecorded
		case trimmed == "live:" && section == secWrongRecorded:
			section = secWrongLive
		case trimmed == "recorded misses:":
			section = secMissRecorded
		case trimmed == "live misses:":
			section = secMissLive
		case trimmed == "":
			section = secNone
		case (section == secMissRecorded || section == secMissLive) && len(f) == 6 && f[2] == "answered":
			if section == secMissRecorded {
				d.missRecorded = append(d.missRecorded, trimmed)
			} else {
				d.missLive = append(d.missLive, trimmed)
			}
		case len(f) == 4 && strings.HasPrefix(f[2], "value=") && strings.HasPrefix(f[3], "expected-met="):
			switch section {
			case secOffRecorded:
				d.offHalf.recorded = append(d.offHalf.recorded, trimmed)
			case secOffLive:
				d.offHalf.live = append(d.offHalf.live, trimmed)
			case secWrongRecorded:
				d.wrong.recorded = append(d.wrong.recorded, trimmed)
			case secWrongLive:
				d.wrong.live = append(d.wrong.live, trimmed)
			}
		case section == secMoved && trimmed != "":
			d.movedRows = append(d.movedRows, trimmed)
		case section == secFlipped && trimmed != "":
			d.flippedRows = append(d.flippedRows, trimmed)
		}
	}
	return d
}

func liveHasName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// liveClosesSection names the block headers that end any open list
// section. The blank line already does this in a real `go test -v`
// transcript; this is the belt so a stray block can never bleed its rows
// into the moved or flipped lists.
func liveClosesSection(trimmed string) bool {
	for _, prefix := range []string{
		"JUDGING: live against", "PER CRITERION (", "EVERY CELL,",
		"RENDER CHECK:", "GATES (", "DECISIONS AT", "LOOK VOCABULARY (",
		"COST:", "PROBE COST (",
	} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

// ---- The ground truth beside the transcripts ----

// loadLiveGroundTruth reads the same transcription the sweep scored
// against: the samples by variant id, for the rebuilt reads and curves.
func loadLiveGroundTruth(t *testing.T) (GroundTruth, map[string]Sample) {
	t.Helper()
	gt, err := LoadGroundTruth(filepath.Join(repoRoot(t), "testdata", "eval", "ground-truth.json"))
	if err != nil {
		t.Fatalf("live pin: ground truth: %v", err)
	}
	byID := make(map[string]Sample, len(gt.Samples))
	for _, s := range gt.Samples {
		byID[s.ID] = s
	}
	return gt, byID
}

// liveCurvesFromTranscript rebuilds the live curves from the transcript's
// EVERY CELL rows: the live probabilities grouped by criterion, each group
// swept by the package's own sweep. Every cell's expected-met column is
// re-checked against the ground truth, so the transcript cannot pin a
// reading against a ground truth that has since moved.
func liveCurvesFromTranscript(t *testing.T, d liveTranscriptData, byID map[string]Sample) []Curve {
	t.Helper()
	byCriterion := map[string][]Cell{}
	var ids []string
	for _, r := range d.cellRows {
		want, ok := byID[r.sample].Expected(r.criterion)
		if !ok {
			t.Fatalf("live pin: %s: criterion %q is not in %s's ground truth", d.name, r.criterion, r.sample)
		}
		if want != r.expected {
			t.Errorf("live pin: %s: %s %s printed expected-met=%v, the ground truth says %v",
				d.name, r.sample, r.criterion, r.expected, want)
		}
		if _, seen := byCriterion[r.criterion]; !seen {
			ids = append(ids, r.criterion)
		}
		byCriterion[r.criterion] = append(byCriterion[r.criterion],
			Cell{Sample: r.sample, Value: r.live, Expected: r.expected})
	}
	sort.Strings(ids)
	curves := make([]Curve, 0, len(ids))
	for _, id := range ids {
		c := sweep(byCriterion[id])
		c.ID = id
		curves = append(curves, c)
	}
	return curves
}

// liveJudgeReadsFromTranscript rebuilds the judge reads the sweep logged:
// the variant behind each label and the biggest issue it named. The
// probabilities do not ride along - the curves above carry them - these
// rows are what liveIssues counts.
func liveJudgeReadsFromTranscript(t *testing.T, d liveTranscriptData, byID map[string]Sample) []JudgeRead {
	t.Helper()
	reads := make([]JudgeRead, 0, len(d.judgeRows))
	for _, r := range d.judgeRows {
		parts := strings.SplitN(r.label, "-", 3)
		if len(parts) != 3 {
			t.Fatalf("live pin: %s: judge label %q does not name its variant", d.name, r.label)
		}
		s, ok := byID[parts[1]]
		if !ok {
			t.Fatalf("live pin: %s: no ground truth for variant %q", d.name, parts[1])
		}
		reads = append(reads, JudgeRead{Sample: s, Issue: r.issue})
	}
	return reads
}

// ---- Comparing a re-emitted line with the transcript's own ----

func livePinLine(t *testing.T, d liveTranscriptData, label, want, got string) {
	t.Helper()
	if w := strings.TrimRight(want, " "); w != got {
		t.Errorf("live pin: %s: %s differs:\n transcript: %q\n computed:   %q", d.name, label, got, w)
	}
}

// livePinTrimmed compares a header the sweep prints with indentation
// against the transcript line captured with that indentation trimmed.
func livePinTrimmed(t *testing.T, d liveTranscriptData, label, want, got string) {
	t.Helper()
	if w := strings.TrimSpace(want); w != got {
		t.Errorf("live pin: %s: %s differs:\n transcript: %q\n computed:   %q", d.name, label, got, w)
	}
}

// livePinLineWith finds the first transcript line under prefix and compares
// it against the re-emitted one.
func livePinLineWith(t *testing.T, d liveTranscriptData, prefix, label, want string) {
	t.Helper()
	got, ok := d.lineWith(prefix)
	if !ok {
		t.Fatalf("live pin: %s: the transcript carries no line starting %q", d.name, prefix)
	}
	livePinLine(t, d, label, want, got)
}

// livePinHasLine fails unless some transcript line, indent trimmed, equals
// want exactly.
func livePinHasLine(t *testing.T, d liveTranscriptData, want string) {
	t.Helper()
	for _, ln := range d.lines {
		if strings.TrimSpace(ln) == want {
			return
		}
	}
	t.Errorf("live pin: %s: the transcript carries no line %q", d.name, want)
}

func livePinList(t *testing.T, d liveTranscriptData, label string, want, got []string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("live pin: %s: %s: the transcript lists %d lines, the computation made %d",
			d.name, label, len(got), len(want))
	}
	for i := range want {
		if want[i] != got[i] {
			t.Errorf("live pin: %s: %s line %d differs:\n transcript: %q\n computed:   %q",
				d.name, label, i, got[i], want[i])
		}
	}
}

// liveOffKeys indexes an off-cell list by the key liveCompareCells uses.
func liveOffKeys(cells []OffCell) map[string]OffCell {
	m := make(map[string]OffCell, len(cells))
	for _, c := range cells {
		m[c.Sample+" "+c.Criterion] = c
	}
	return m
}

// liveOffDiff reproduces the sweep's liveCompareCells finding: the entries
// each side has that the other lacks, sorted, in the print's own wording.
func liveOffDiff(recorded, live []OffCell) (onlyLive, onlyRecorded []string) {
	rec, liv := liveOffKeys(recorded), liveOffKeys(live)
	for k, c := range liv {
		if _, ok := rec[k]; !ok {
			onlyLive = append(onlyLive, fmt.Sprintf("%s %s (live %.4f, expected met %v)",
				c.Sample, c.Criterion, c.Value, c.Expected))
		}
	}
	for k, c := range rec {
		if _, ok := liv[k]; !ok {
			onlyRecorded = append(onlyRecorded, fmt.Sprintf("%s %s (recorded %.4f, expected met %v)",
				c.Sample, c.Criterion, c.Value, c.Expected))
		}
	}
	sort.Strings(onlyLive)
	sort.Strings(onlyRecorded)
	return onlyLive, onlyRecorded
}

// livePinOffBlock re-runs the sweep's cell comparison for one off-list
// section: the header, both lists, the reproduce line and the only-one-side
// entries, all against the transcript.
func livePinOffBlock(t *testing.T, d liveTranscriptData, where, header string,
	headerWant string, recorded, live []OffCell, lists liveCellList, reproKey string,
) {
	t.Helper()
	livePinTrimmed(t, d, "the off-list header", headerWant, header)
	livePinCellsList(t, d, where+" recorded list", recorded, lists.recorded)
	livePinCellsList(t, d, where+" live list", live, lists.live)
	onlyLiveEntries, onlyRecEntries := liveOffDiff(recorded, live)
	livePinTrimmed(t, d, "the reproduce line",
		fmt.Sprintf(liveReproduceFmt, where, len(onlyLiveEntries) == 0 && len(onlyRecEntries) == 0),
		strings.TrimSpace(d.headline(t, reproKey)))
	var wantLive, wantRec []string
	for _, e := range onlyLiveEntries {
		wantLive = append(wantLive, strings.TrimSpace(fmt.Sprintf(liveOffOnlyLiveFmt, where, e)))
	}
	for _, e := range onlyRecEntries {
		wantRec = append(wantRec, strings.TrimSpace(fmt.Sprintf(liveOffOnlyRecFmt, where, e)))
	}
	livePinList(t, d, "off live-only lines", wantLive, d.linesWith("off at "+where+" live only:"))
	livePinList(t, d, "off recorded-only lines", wantRec, d.linesWith("off at "+where+" recorded only:"))
}

func livePinCellsList(t *testing.T, d liveTranscriptData, label string, cells []OffCell, got []string) {
	t.Helper()
	want := make([]string, 0, len(cells))
	for _, c := range cells {
		want = append(want, strings.TrimSpace(fmt.Sprintf(liveOffCellFormat, c.Sample, c.Criterion, c.Value, c.Expected)))
	}
	livePinList(t, d, label, want, got)
}

// ---- The pinned tests ----

// TestLiveTranscriptJudging replays T3.9b's live judging report: the
// committed sweep transcript in, the lifted helpers over it, every number
// docs/evaluation.md's live re-run quotes back out, and every printed line
// re-emitted verbatim from the computed values. No tag, no key, no call,
// no spend.
func TestLiveTranscriptJudging(t *testing.T) {
	d := parseLiveTranscript(t, "sweep-1.txt")
	gt, byID := loadLiveGroundTruth(t)
	rep := runOnce(t)
	recorded := rep.Judge
	settings := JudgeSettings{Met: rep.Settings.JudgeMet, PerCriterion: rep.Settings.JudgePerCriterion}

	liveCurves := liveCurvesFromTranscript(t, d, byID)
	nums, recValues, err := computeLiveJudgeNumbers(liveCurves, recorded, settings)
	if err != nil {
		t.Fatalf("live pin: %s: %v", d.name, err)
	}
	drift, err := measureLiveDrift(liveCurves, recValues)
	if err != nil {
		t.Fatalf("live pin: %s: %v", d.name, err)
	}
	reads := liveJudgeReadsFromTranscript(t, d, byID)

	// The counts docs/evaluation.md's table prints, out of the helpers.
	if len(liveCurves) != 36 || len(recorded.Curves) != 36 {
		t.Errorf("live pin: %d live curves, %d recorded, want 36 and 36", len(liveCurves), len(recorded.Curves))
	}
	if recorded.Cells != 273 || drift.Cells != 273 {
		t.Errorf("live pin: %d recorded cells, %d live, want 273 both", recorded.Cells, drift.Cells)
	}
	half, lines := liveIssues(reads), nums
	if lines.LiveHalf != 254 || recorded.AgreeAtHalf != 260 {
		t.Errorf("live pin: cells agreeing at 0.5: live %d recorded %d, want 254/260", lines.LiveHalf, recorded.AgreeAtHalf)
	}
	if lines.LiveLines != 254 || recorded.ChosenTotal != 269 {
		t.Errorf("live pin: cells agreeing at the shipped lines: live %d recorded %d, want 254/269", lines.LiveLines, recorded.ChosenTotal)
	}
	if lines.LiveMaxTotal != 269 || lines.RecBestTotal != 269 {
		t.Errorf("live pin: curves' best total: live %d recorded %d, want 269 both", lines.LiveMaxTotal, lines.RecBestTotal)
	}
	if lines.LiveApart != 32 || lines.RecApart != 32 {
		t.Errorf("live pin: criteria separating perfectly: live %d recorded %d, want 32 both", lines.LiveApart, lines.RecApart)
	}
	if half != 28 || recorded.Issues.Named != 31 {
		t.Errorf("live pin: biggest issue names a miss: live %d recorded %d, want 28/31", half, recorded.Issues.Named)
	}
	if len(nums.OffHalf) != 19 || len(recorded.OffAtHalf) != 13 {
		t.Errorf("live pin: cells off at 0.5: live %d recorded %d, want 19/13", len(nums.OffHalf), len(recorded.OffAtHalf))
	}
	if len(nums.Wrong) != 19 || len(recorded.Wrong) != 4 {
		t.Errorf("live pin: irreducible cells: live %d recorded %d, want 19/4", len(nums.Wrong), len(recorded.Wrong))
	}
	if len(nums.Moved) != 272 || drift.Differing != 272 {
		t.Errorf("live pin: cells that moved: %d listed, %d counted, want 272 (V20 lip-quiet is the one that did not)",
			len(nums.Moved), drift.Differing)
	}
	if len(nums.Flipped) != 17 || nums.CriteriaLost != 12 {
		t.Errorf("live pin: cells across the line %d (want 17), criteria losing a cell %d (want 12)",
			len(nums.Flipped), nums.CriteriaLost)
	}
	// The drift aggregates, one table, every value a literal
	// docs/evaluation.md's live re-run quotes and every got computed from
	// the transcript's per-cell rows. These three summaries are NOT pinned
	// as transcript text: the committed transcripts predate the lines that
	// print them (see the note under the headline pins below), so the doc
	// is their pin.
	const v20 = "V20 lip-quiet"
	for _, want := range []struct{ label, got, want string }{
		{"mean |move| over 273 cells", fmt.Sprintf("%.4f", drift.MeanMove), "0.0803"},
		{"moves inside 0.05", fmt.Sprintf("%d", drift.Inside05), "140"},
		{"moves of 0.10 or more", fmt.Sprintf("%d", drift.AtLeast10), "74"},
		{"moves past 0.45", fmt.Sprintf("%d", drift.Past045), "3"},
		{"cells whose live value differs", fmt.Sprintf("%d of %d", drift.Differing, drift.Cells), "272 of 273"},
		{"the one identical cell", strings.Join(drift.Identical, ", "), v20},
		{"that cell's value on both sides", fmt.Sprintf("%.4f", recValues[v20]), "0.0462"},
		{"largest move, named", drift.LargestCell, "V09 interview-beard-short"},
		{"largest move, recorded", fmt.Sprintf("%.4f", drift.LargestFrom), "0.7586"},
		{"largest move, live", fmt.Sprintf("%.4f", drift.LargestTo), "0.2175"},
		{"largest move, delta", fmt.Sprintf("%.4f", drift.LargestDelta), "0.5411"},
		{"cells across the shipped line", fmt.Sprintf("%d", len(nums.Flipped)), "17"},
	} {
		if want.got != want.want {
			t.Errorf("live pin: %s: %s, want %s", want.label, want.got, want.want)
		}
	}
	// The doc's second named mover, and the arrival among the flips.
	wantMove := "V17 funeral-beard-close: recorded 0.8120, live 0.3595, delta -0.4525"
	moved := false
	for _, m := range nums.Moved {
		if m == wantMove {
			moved = true
		}
	}
	if !moved {
		t.Errorf("live pin: the moved list lacks the doc's %q", wantMove)
	}
	departed, arrived, arrivalKey := 0, 0, ""
	for _, r := range d.cellRows {
		line := liveFloat(t, d.name, r.line)
		recAgrees := (recValues[r.sample+" "+r.criterion] >= line) == r.expected
		switch {
		case recAgrees && !r.agrees:
			departed++
		case !recAgrees && r.agrees:
			arrived++
			arrivalKey = r.sample + " " + r.criterion
		}
	}
	if departed != 16 || arrived != 1 || arrivalKey != "P12 family-expression-present" {
		t.Errorf("live pin: %d left the correct side, %d arrived (%s); want 16 and P12 family-expression-present",
			departed, arrived, arrivalKey)
	}

	// The printed lines, re-emitted from the computed values.
	livePinLineWith(t, d, "JUDGING:", "the judging header",
		fmt.Sprintf(liveJudgingHeaderFormat, len(reads), recorded.Cells))
	livePinLine(t, d, "the 0.5 headline", fmt.Sprintf(liveHeadlineFormat, "cells agreeing at 0.5",
		fmt.Sprintf("%d/%d", recorded.AgreeAtHalf, recorded.Cells), nums.LiveHalf, recorded.Cells),
		d.headline(t, "agreeHalf"))
	livePinLine(t, d, "the shipped-lines headline", fmt.Sprintf(liveHeadlineFormat, "cells agreeing at the shipped lines",
		fmt.Sprintf("%d/%d", recorded.ChosenTotal, recorded.Cells), nums.LiveLines, recorded.Cells),
		d.headline(t, "agreeLines"))
	livePinLine(t, d, "the best-total headline", fmt.Sprintf(liveHeadlineFormat, "curves' best total (no line beats it)",
		fmt.Sprintf("%d/%d", nums.RecBestTotal, recorded.Cells), nums.LiveMaxTotal, recorded.Cells),
		d.headline(t, "bestTotal"))
	livePinLine(t, d, "the apart headline", fmt.Sprintf(liveHeadlineFormat, "criteria separating perfectly",
		fmt.Sprintf("%d/%d", nums.RecApart, len(recorded.Curves)), nums.LiveApart, len(liveCurves)),
		d.headline(t, "apart"))
	livePinLine(t, d, "the issues headline", fmt.Sprintf(liveHeadlineFormat, "biggest issue names a miss (or none)",
		fmt.Sprintf("%d/%d", recorded.Issues.Named, recorded.Issues.Of), liveIssues(reads), len(reads)),
		d.headline(t, "issues"))
	livePinLine(t, d, "the moved count", fmt.Sprintf(liveMovedCountFormat, len(nums.Moved), recorded.Cells),
		d.headline(t, "movedOf"))
	livePinLine(t, d, "the flipped count", fmt.Sprintf(liveFlippedCountFormat, len(nums.Flipped)),
		d.headline(t, "flippedCount"))
	// The drift, largest-move and identical-cells lines are pinned as
	// COMPUTED VALUES, not as transcript text: the committed transcripts
	// predate them. They print in live_test.go now, so a future paid
	// transcript carries all three - see the Notes in
	// dev-diary/PHASE-3-pipeline.md for the ruling.

	// The per-criterion table: the drift the doc names, criterion by
	// criterion, and every row re-emitted.
	byCrit := make(map[string]liveCriterionRow, len(d.criterion))
	for _, r := range d.criterion {
		byCrit[r.id] = r
	}
	if len(d.criterion) != 36 {
		t.Fatalf("live pin: %s: %d per-criterion rows, want 36", d.name, len(d.criterion))
	}
	for _, c := range liveCurves {
		rec := nums.RecCurves[c.ID]
		line := settings.lineFor(c)
		recAt, liveAt := agreement(rec.Cells, line), agreement(c.Cells, line)
		mark := ""
		if recAt != liveAt || rec.MaxAgreement != c.MaxAgreement || rec.Apart != c.Apart {
			mark = liveMarkDiffers
		}
		row, ok := byCrit[c.ID]
		if !ok {
			t.Fatalf("live pin: %s: no per-criterion row for %q", d.name, c.ID)
		}
		livePinLine(t, d, c.ID+"'s row", fmt.Sprintf(liveCriterionFormat, c.ID, len(c.Cells),
			rec.MaxAgreement, len(c.Cells), c.MaxAgreement, len(c.Cells),
			recAt, len(c.Cells), liveAt, len(c.Cells), line, rec.Apart, c.Apart, mark), row.raw)
	}
	for _, want := range []struct{ id, recAt, liveAt string }{
		{"family-expression-present", "8/9", "6/9"},
		{"date-beard-even", "7/8", "5/8"},
		{"funeral-beard-close", "8/8", "6/8"},
	} {
		row, ok := byCrit[want.id]
		if !ok {
			t.Fatalf("live pin: %s: no per-criterion row for %q", d.name, want.id)
		}
		if row.recAt != want.recAt || row.liveAt != want.liveAt {
			t.Errorf("live pin: %s at the line: transcript %s/%s, docs say %s/%s",
				want.id, row.recAt, row.liveAt, want.recAt, want.liveAt)
		}
	}

	// The off-lists, their headers and their comparisons.
	livePinOffBlock(t, d, "0.5", d.offHalfHeader,
		fmt.Sprintf(liveOffHeaderFmt, len(recorded.OffAtHalf), len(nums.OffHalf)),
		recorded.OffAtHalf, nums.OffHalf, d.offHalf, "reproHalf")
	livePinOffBlock(t, d, "the shipped line", d.wrongHeader,
		fmt.Sprintf(liveWrongHeaderFmt, len(recorded.Wrong), len(nums.Wrong)),
		recorded.Wrong, nums.Wrong, d.wrong, "reproLine")
	livePinTrimmed(t, d, "the moved block header",
		fmt.Sprintf(liveMovedBlockFmt, len(nums.Moved), recorded.Cells), d.movedHeader)
	livePinTrimmed(t, d, "the flipped block header",
		fmt.Sprintf(liveFlippedBlockFmt, len(nums.Flipped)), d.flippedHeader)
	livePinList(t, d, "the moved lines", nums.Moved, d.movedRows)
	livePinList(t, d, "the flipped lines", nums.Flipped, d.flippedRows)

	// Every cell, live beside recorded: the raw evidence under all of it.
	if len(d.cellRows) != 273 {
		t.Fatalf("live pin: %s: %d every-cell rows, want 273", d.name, len(d.cellRows))
	}
	cellIdx := make(map[string]liveCellRow, len(d.cellRows))
	for _, r := range d.cellRows {
		cellIdx[r.sample+" "+r.criterion] = r
	}
	for _, c := range liveCurves {
		line := settings.lineFor(c)
		for _, cell := range c.Cells {
			key := cell.Sample + " " + c.ID
			row, ok := cellIdx[key]
			if !ok {
				t.Fatalf("live pin: %s: no every-cell row for %s", d.name, key)
			}
			livePinLine(t, d, key, fmt.Sprintf(liveCellRowFormat, cell.Sample, c.ID,
				recValues[key], cell.Value, line, cell.Expected,
				(cell.Value >= line) == cell.Expected), row.raw)
		}
	}

	// The per-call log lines: tokens to neurons to booking, at clef's rate.
	for _, r := range d.judgeRows {
		neurons := liveNeurons(r.tokens, liveNeuronsPerMillion[systemone.ModelClef])
		livePinLine(t, d, r.label, fmt.Sprintf(liveJudgeRowFormat, r.label, r.issue, r.tokens, neurons, r.booked), r.raw)
	}
	if len(d.judgeRows) != liveJudgeCalls {
		t.Fatalf("live pin: %s: %d judge rows, want %d", d.name, len(d.judgeRows), liveJudgeCalls)
	}
	// The run shape: one judge call per ground-truth sample.
	if len(gt.Samples) != liveJudgeCalls {
		t.Errorf("live pin: the ground truth carries %d samples, the sweep judged %d",
			len(gt.Samples), liveJudgeCalls)
	}
}
