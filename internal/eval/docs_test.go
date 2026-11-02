package eval

// The docs/evaluation.md pin (T3.13).
//
// docs/evaluation.md holds the thresholds record: the numbers the README
// and the video quote. Nothing read it before this file - the per-criterion
// figures and the biggest-issue figures could age quietly while the
// measurement moved under them, which is exactly the drift a pin exists to
// name. This file reads the document the way rerecord_test.go reads
// docs/clef.md: parsed strictly (a sentence or table row that loses the
// shape this pin compares is fatal with its name, never skipped), then
// compared field by field against the measurement the package prints -
// the shared report and, for the biggest issue, the block JudgeReport
// renders through IssueLines, whose lines name each read they carry.
//
// The doc's biggest-issue figures are re-emitted from that printed block,
// never transcribed; where the two reads differ the doc carries both
// counts with their labels, and this pin asserts each against its own
// labelled line. A future re-record that moves a margin, a plateau or an
// issue count fails here naming the figure, instead of aging the doc.
//
// No tag, no key, no network, no spend: the measurement is the offline
// replay of committed recordings, and the doc is a committed file.

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// evaluationDocPath is the document this pin reads, in the repository the
// package replays against.
func evaluationDocPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "docs", "evaluation.md")
}

// The measurement's canonical precisions, in decimals: the doc prints
// plateaus at four (trimming trailing zeros), lines and margins at five.
// A doc literal carrying fewer digits is compared at the canonical
// precision anyway, so a re-record that moves a value inside the doc's
// printed precision is still named; one carrying more is compared at its
// own, which is stricter.
const (
	evaluationPlateauDecimals = 4
	evaluationLineDecimals    = 5
	evaluationMarginDecimals  = 5
)

// evaluationNumberIs asserts one doc figure equals the measurement, at
// the stricter of the literal's own precision and the canonical one.
func evaluationNumberIs(t *testing.T, text string, want float64, canonical int, label string) {
	t.Helper()
	got, err := strconv.ParseFloat(text, 64)
	if err != nil {
		t.Fatalf("docs/evaluation.md: %s: %q is not a number: %v", label, text, err)
	}
	decimals := evaluationDocDecimals(text)
	if decimals < canonical {
		decimals = canonical
	}
	doc, measurement := fmt.Sprintf("%.*f", decimals, got), fmt.Sprintf("%.*f", decimals, want)
	if doc != measurement {
		t.Errorf("docs/evaluation.md: %s: the doc carries %s, the measurement reads %s (at %d decimals)",
			label, doc, measurement, decimals)
	}
}

// evaluationDocDecimals counts the decimals a doc literal carries.
func evaluationDocDecimals(text string) int {
	dot := strings.Index(text, ".")
	if dot < 0 {
		return 0
	}
	return len(text) - dot - 1
}

// evaluationDocLines reads docs/evaluation.md and returns its lines. A
// missing or unreadable document is fatal: the pin has nothing to say
// about a doc it cannot read.
func evaluationDocLines(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(evaluationDocPath(t))
	if err != nil {
		t.Fatalf("docs/evaluation.md: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 200 {
		t.Fatalf("docs/evaluation.md: %d lines, want the full document, not a fragment", len(lines))
	}
	return lines
}

// evaluationInt parses one doc integer figure, fatal with its label when
// the cell is not one.
func evaluationInt(t *testing.T, text string, label string) int {
	t.Helper()
	n, err := strconv.Atoi(text)
	if err != nil {
		t.Fatalf("docs/evaluation.md: %s: %q is not an integer: %v", label, text, err)
	}
	return n
}

// ---- the biggest-issue block: the printed report, parsed back ----

// issueBlock is the printed report's biggest-issue block, parsed back out
// of the lines IssueLines renders. Parsing its own print - rather than
// reading the report struct - is what makes the doc's figures re-emitted
// from the printed report: the block is the print, and the doc is
// compared against the block.
type issueBlock struct {
	rawNamed, rawOf     int
	rawMissed           []string
	gatedNamed, gatedOf int
	gatedMissed         []string
	// choices carries both reads per missed variant: sample -> raw choice,
	// gated choice.
	choices map[string][2]string
}

var (
	evaluationRawLine     = regexp.MustCompile(`^biggest issue, the service's raw choice: names a miss \(or none\) on (\d+) of (\d+) variants$`)
	evaluationRawMissed   = regexp.MustCompile(`^biggest issue, the service's raw choice, missed: (.+)$`)
	evaluationGatedLine   = regexp.MustCompile(`^biggest issue, gated on the look's own flags \(what pipeline\.Judge reports\): names a miss \(or none\) on (\d+) of (\d+) variants$`)
	evaluationGatedMissed = regexp.MustCompile(`^biggest issue, gated on the look's own flags, missed: (.+)$`)
	evaluationVariantLine = regexp.MustCompile(`^  ([A-Z]\d+): raw ([a-z-]+), gated ([a-z-]+)$`)
)

// evaluationIssueBlock parses the block out of IssueLines' lines. Every
// line has a known shape; one that does not is fatal with the line, so a
// change to the print fails here before the doc comparison can silently
// compare against a block that no longer says what it prints.
func evaluationIssueBlock(t *testing.T, lines []string) issueBlock {
	t.Helper()
	var block issueBlock
	block.choices = map[string][2]string{}
	inVariants := false
	for _, line := range lines {
		switch {
		case evaluationRawLine.MatchString(line):
			m := evaluationRawLine.FindStringSubmatch(line)
			block.rawNamed = evaluationInt(t, m[1], "the raw-choice count")
			block.rawOf = evaluationInt(t, m[2], "the raw-choice total")
		case evaluationRawMissed.MatchString(line):
			m := evaluationRawMissed.FindStringSubmatch(line)
			block.rawMissed = strings.Split(m[1], ", ")
		case evaluationGatedLine.MatchString(line):
			m := evaluationGatedLine.FindStringSubmatch(line)
			block.gatedNamed = evaluationInt(t, m[1], "the gated count")
			block.gatedOf = evaluationInt(t, m[2], "the gated total")
		case evaluationGatedMissed.MatchString(line):
			m := evaluationGatedMissed.FindStringSubmatch(line)
			block.gatedMissed = strings.Split(m[1], ", ")
		case line == "biggest issue, per variant, both reads, for every variant either read misses:":
			inVariants = true
		case evaluationVariantLine.MatchString(line):
			if !inVariants {
				t.Fatalf("the printed report carries a variant line before its header: %q", line)
			}
			m := evaluationVariantLine.FindStringSubmatch(line)
			if _, dup := block.choices[m[1]]; dup {
				t.Fatalf("the printed report carries %s twice", m[1])
			}
			block.choices[m[1]] = [2]string{m[2], m[3]}
		default:
			if inVariants && strings.HasPrefix(line, "  ") {
				t.Fatalf("the printed report carries a mis-shaped variant line: %q", line)
			}
		}
	}
	if block.rawOf == 0 || block.gatedOf == 0 {
		t.Fatal("the printed report's biggest-issue block is missing its count lines")
	}
	if len(block.rawMissed) == 0 || len(block.gatedMissed) == 0 {
		t.Fatal("the printed report's biggest-issue block is missing its missed lists")
	}
	// The block must be the report struct's own render: one defence that
	// the print and the measurement are the same thing.
	want := map[string]bool{}
	for _, m := range block.rawMissed {
		want[m] = true
	}
	for _, m := range block.gatedMissed {
		want[m] = true
	}
	if len(want) != len(block.choices) {
		t.Fatalf("the printed report carries %d variant lines, want one for each of the %d variants either read misses",
			len(block.choices), len(want))
	}
	return block
}

// ---- docs/evaluation.md's biggest-issue table ----

// docIssueTable is the doc's two-row biggest-issue table.
type docIssueTable struct {
	rawNamed, rawOf     int
	rawMissed           []string
	gatedNamed, gatedOf int
	gatedMissed         []string
}

// evaluationParseIssueTable finds the doc's biggest-issue table and reads
// its two rows. The header must be exact, both rows must be present, and
// a count cell must be "<n> of <total>"; anything else is fatal with the
// row, never skipped - a table this pin half-reads would pin nothing.
func evaluationParseIssueTable(t *testing.T, lines []string) docIssueTable {
	t.Helper()
	const header = "| The read | Names a miss (or none) | Misses |"
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == header {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("docs/evaluation.md: no biggest-issue table headed %q", header)
	}
	var rows [][]string
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			break
		}
		cols := rerecordRow(trimmed)
		if rerecordDividerRow(cols) {
			continue
		}
		if len(cols) != 3 {
			t.Fatalf("docs/evaluation.md: biggest-issue row %q has %d cells, want 3", trimmed, len(cols))
		}
		rows = append(rows, cols)
	}
	if len(rows) != 2 {
		t.Fatalf("docs/evaluation.md: the biggest-issue table has %d rows, want both reads labelled", len(rows))
	}
	parseRow := func(cols []string, label string, labelWant string) (int, int, []string) {
		if !strings.HasPrefix(cols[0], labelWant) {
			t.Fatalf("docs/evaluation.md: the biggest-issue table's %s row is labelled %q, want it to name the read as %q", label, cols[0], labelWant)
		}
		m := regexp.MustCompile(`^(\d+) of (\d+)$`).FindStringSubmatch(cols[1])
		if m == nil {
			t.Fatalf("docs/evaluation.md: the %s row's count is %q, want \"<n> of <total>\"", label, cols[1])
		}
		missed := strings.Split(cols[2], ", ")
		for _, sample := range missed {
			if !regexp.MustCompile(`^[A-Z]\d+$`).MatchString(sample) {
				t.Fatalf("docs/evaluation.md: the %s row misses %q, want a variant id", label, sample)
			}
		}
		return evaluationInt(t, m[1], label+" count"), evaluationInt(t, m[2], label+" total"), missed
	}
	var table docIssueTable
	table.rawNamed, table.rawOf, table.rawMissed = parseRow(rows[0], "raw-choice", "the service's raw choice")
	table.gatedNamed, table.gatedOf, table.gatedMissed = parseRow(rows[1], "gated", "gated on the look's own flags")
	return table
}

// ---- docs/evaluation.md's per-criterion table ----

// evaluationParseCriterionTable reads the doc's per-criterion table: 36
// rows of n, best, apart, plateau, line, margin and the 0.5 anchors, one
// per criterion curve. The header must be exact and every row must name
// a curve of the report; the comparison against the curves happens in
// the pin itself.
func evaluationParseCriterionTable(t *testing.T, lines []string) map[string][][]string {
	t.Helper()
	const header = "| Criterion | n | best | apart | plateau | line | margin | 0.5 |"
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == header {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("docs/evaluation.md: no per-criterion table headed %q", header)
	}
	rows := map[string][][]string{}
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			break
		}
		cols := rerecordRow(trimmed)
		if rerecordDividerRow(cols) {
			continue
		}
		if len(cols) != 8 {
			t.Fatalf("docs/evaluation.md: criterion row %q has %d cells, want 8", trimmed, len(cols))
		}
		id := cols[0]
		if suffix := " (interview + funeral)"; strings.HasSuffix(id, suffix) {
			id = strings.TrimSuffix(id, suffix)
		}
		if _, dup := rows[id]; dup {
			t.Fatalf("docs/evaluation.md: criterion %q is tabulated twice", id)
		}
		rows[id] = append(rows[id], cols)
	}
	if len(rows) != 36 {
		t.Fatalf("docs/evaluation.md: the per-criterion table has %d criteria, want the report's 36", len(rows))
	}
	return rows
}

// ---- the pin ----

// TestTheEvaluationDocMatchesThePrintedReport reads docs/evaluation.md
// against the measurement, the way rerecord_test.go reads docs/clef.md
// against the corpus. It prints the report's biggest-issue block first,
// so the printed report and the pinned doc are in one transcript, then
// pins: both issue reads (doc table against the print, the print against
// the report struct), the per-criterion table's every row (n, best,
// apart, plateau, shipped line, margin, 0.5 anchors), the thin-lines
// sentences, the live re-run's biggest-issue row (the frozen transcript's
// literals), and the Reproducing summary's biggest-issue clause. A drift
// in any of them fails with the figure and both of its sides.
func TestTheEvaluationDocMatchesThePrintedReport(t *testing.T) {
	report := runOnce(t)
	judge := report.Judge
	printed := judge.IssueLines()
	for _, line := range printed {
		t.Log(line)
	}
	block := evaluationIssueBlock(t, printed)
	doc := evaluationDocLines(t)

	// The printed block is the report struct's render: the counts and
	// lists must be the struct's own, so the doc is pinned against a
	// block that cannot disagree with the measurement.
	if block.rawNamed != judge.Issues.Named || block.rawOf != judge.Issues.Of ||
		block.gatedNamed != judge.GatedIssues.Named || block.gatedOf != judge.GatedIssues.Of {
		t.Fatalf("the printed block counts %d of %d raw and %d of %d gated, the report carries %d of %d and %d of %d",
			block.rawNamed, block.rawOf, block.gatedNamed, block.gatedOf,
			judge.Issues.Named, judge.Issues.Of, judge.GatedIssues.Named, judge.GatedIssues.Of)
	}
	if !reflect.DeepEqual(block.rawMissed, judge.Issues.Missed) ||
		!reflect.DeepEqual(block.gatedMissed, judge.GatedIssues.Missed) {
		t.Fatalf("the printed block's missed lists %v and %v differ from the report's %v and %v",
			block.rawMissed, block.gatedMissed, judge.Issues.Missed, judge.GatedIssues.Missed)
	}
	applied := map[string]AppliedSample{}
	for _, a := range judge.Applied {
		applied[a.Sample] = a
	}
	for sample, choices := range block.choices {
		a, ok := applied[sample]
		if !ok {
			t.Fatalf("the printed block names %s, whom the report applied no read to", sample)
		}
		if choices[0] != a.Issue || choices[1] != a.GatedIssue {
			t.Fatalf("%s: the printed block reads raw %s, gated %s; the applied read carries %s and %s",
				sample, choices[0], choices[1], a.Issue, a.GatedIssue)
		}
	}

	// The doc's table, row by row against the print.
	table := evaluationParseIssueTable(t, doc)
	if table.rawNamed != block.rawNamed || table.rawOf != block.rawOf {
		t.Errorf("docs/evaluation.md: the raw-choice row counts %d of %d, the printed report prints %d of %d",
			table.rawNamed, table.rawOf, block.rawNamed, block.rawOf)
	}
	if !reflect.DeepEqual(table.rawMissed, block.rawMissed) {
		t.Errorf("docs/evaluation.md: the raw-choice row misses %v, the printed report prints %v",
			table.rawMissed, block.rawMissed)
	}
	if table.gatedNamed != block.gatedNamed || table.gatedOf != block.gatedOf {
		t.Errorf("docs/evaluation.md: the gated row counts %d of %d, the printed report prints %d of %d",
			table.gatedNamed, table.gatedOf, block.gatedNamed, block.gatedOf)
	}
	if !reflect.DeepEqual(table.gatedMissed, block.gatedMissed) {
		t.Errorf("docs/evaluation.md: the gated row misses %v, the printed report prints %v",
			table.gatedMissed, block.gatedMissed)
	}
	// Where the two counts differ, both must appear - the doc carries two
	// rows, and the counts must not be folded into one headline.
	if table.rawNamed == table.gatedNamed && table.rawMissed != nil &&
		reflect.DeepEqual(table.rawMissed, table.gatedMissed) {
		t.Error("docs/evaluation.md: the two reads agree in the doc, so the doc no longer carries both counts where they differ")
	}

	// The doc's prose names each read and says where its figures come
	// from: the printed report.
	for _, sentence := range []string{
		"the **service's raw choice** — Clef's `biggest_issue` answer, exactly as the recording carries it",
		"gated on the look's own flags** is what `pipeline.Judge` reports for the same recording",
		"Every figure in this subsection is re-emitted from the printed report's biggest-issue block",
		"Where the two counts differ, both numbers stand with their labels",
	} {
		if !containsLine(doc, sentence) {
			t.Errorf("docs/evaluation.md: the biggest-issue subsection no longer says %q, so the doc no longer names its read", sentence)
		}
	}

	// The per-variant prose claims, re-emitted: the raw nine split into
	// the five that miss nothing (gated none is then named) and the four
	// that do not; eight of the nine raw misses are the beard pick.
	evaluationPinMissedProse(t, doc, block, judge)

	// The per-criterion table, every row against its curve.
	evaluationPinCriterionTable(t, doc, judge, report.Settings)

	// The thin-lines sentences.
	evaluationPinThinLines(t, doc, judge, report.Settings)

	// The live re-run's biggest-issue row: the frozen transcript's own
	// figures, raw choice on both sides. These do not come from the
	// offline replay - they are the recorded evidence live_report_test.go
	// pins against the transcript - and they must not move with a
	// re-record.
	evaluationPinLiveIssueRow(t, doc)

	// The Reproducing summary names both labelled reads, its figures
	// built from the print.
	clause := fmt.Sprintf("the biggest issue in both labelled reads — the service's raw choice naming %d of %d with %d missed, the flag-gated read naming %d of %d with %d missed;",
		block.rawNamed, block.rawOf, len(block.rawMissed), block.gatedNamed, block.gatedOf, len(block.gatedMissed))
	if !containsLine(doc, clause) {
		t.Errorf("docs/evaluation.md: the Reproducing paragraph no longer carries %q", clause)
	}
}

// evaluationPinMissedProse pins the per-variant prose figures of the
// biggest-issue subsection against the printed block: which raw-missed
// variants miss nothing, which keep Clef's own none, and how many of the
// nine are the beard pick.
func evaluationPinMissedProse(t *testing.T, doc []string, block issueBlock, judge JudgeReport) {
	t.Helper()
	gt, err := LoadGroundTruth(filepath.Join(repoRoot(t), "testdata", "eval", "ground-truth.json"))
	if err != nil {
		t.Fatalf("ground truth: %v", err)
	}
	missesByID := map[string]Sample{}
	for _, s := range gt.Samples {
		missesByID[s.ID] = s
	}
	beard := map[string]bool{}
	for _, c := range judge.Curves {
		if strings.Contains(c.ID, "beard") {
			beard[c.ID] = true
		}
	}
	missNothing, keptNone, beardPick := 0, 0, 0
	for _, sample := range block.rawMissed {
		s, ok := missesByID[sample]
		if !ok {
			t.Fatalf("the printed report misses %s, who is in no ground truth", sample)
		}
		choices, printed := block.choices[sample]
		if !printed {
			t.Fatalf("the printed report has no per-variant line for %s", sample)
		}
		if len(s.Miss) == 0 {
			missNothing++
		}
		if choices[0] == "none" {
			keptNone++
		}
		if beard[choices[0]] {
			beardPick++
		}
	}
	if missNothing != 5 {
		t.Errorf("docs/evaluation.md: the prose says five raw-missed variants miss nothing, the ground truth says %d do", missNothing)
	}
	if keptNone != 1 {
		t.Errorf("docs/evaluation.md: the prose says one raw-missed variant answers none, the print says %d do", keptNone)
	}
	if beardPick != 8 {
		t.Errorf("docs/evaluation.md: the prose says eight of the nine raw misses are the beard pick, the print says %d are", beardPick)
	}
	if !containsLine(doc, "Eight of the nine are the beard pick.") {
		t.Error(`docs/evaluation.md: the sentence "Eight of the nine are the beard pick." is gone`)
	}
	if !containsLine(doc, "the beard pick is eight of the nine misses of the service's raw choice") {
		t.Error(`docs/evaluation.md: Recommendations no longer names the beard pick eight of the nine misses of the service's raw choice`)
	}
	// The gated five: V21 the suppressed true claim, V07 Clef's own none.
	if len(block.gatedMissed) != 5 {
		t.Errorf("docs/evaluation.md: the prose explains five gated misses, the print carries %d", len(block.gatedMissed))
	}
	v21, printed := block.choices["V21"]
	if !printed || !beard[v21[0]] || v21[1] != "none" {
		t.Errorf("docs/evaluation.md: the prose holds V21's suppressed beard, the print reads raw %s, gated %s", v21[0], v21[1])
	}
	v07, printed := block.choices["V07"]
	if !printed || v07[0] != "none" || v07[1] != "none" {
		t.Errorf("docs/evaluation.md: the prose holds V07's own none, the print reads raw %s, gated %s", v07[0], v07[1])
	}
	if !containsLine(doc, "V21 loses the one true claim Clef made") {
		t.Error(`docs/evaluation.md: the gated prose no longer explains V21's suppressed claim`)
	}
}

// evaluationPinCriterionTable compares the doc's per-criterion table with
// the report's curves at the shipped settings: n, best, apart, plateau,
// line (bold exactly where an override carries it), margin (the shipped
// line's distance to the nearest observed probability), and the 0.5
// anchors. This is the pin behind the table's every row, including the
// date-beard-even row whose margin the thin-lines prose quotes.
func evaluationPinCriterionTable(t *testing.T, doc []string, judge JudgeReport, settings Settings) {
	t.Helper()
	rows := evaluationParseCriterionTable(t, doc)
	curves := map[string]Curve{}
	for _, c := range judge.Curves {
		curves[c.ID] = c
	}
	for id, r := range rows {
		cols := r[0]
		curve, ok := curves[id]
		if !ok {
			t.Fatalf("docs/evaluation.md: the per-criterion table names %q, who has no curve", id)
		}
		line := settings.JudgeMet
		if v, ok := settings.JudgePerCriterion[id]; ok {
			line = v
		}
		label := "criterion " + id
		if n := evaluationInt(t, cols[1], label+" n"); n != len(curve.Cells) {
			t.Errorf("docs/evaluation.md: %s's n is %s, the curve carries %d cells", id, cols[1], len(curve.Cells))
		}
		best := regexp.MustCompile(`^(\d+)/(\d+)$`).FindStringSubmatch(cols[2])
		if best == nil {
			t.Fatalf("docs/evaluation.md: %s's best is %q, want \"<agreement>/<cells>\"", id, cols[2])
		}
		if evaluationInt(t, best[1], label+" best") != curve.MaxAgreement ||
			evaluationInt(t, best[2], label+" best total") != len(curve.Cells) {
			t.Errorf("docs/evaluation.md: %s's best is %s, the curve reads %d of %d", id, cols[2], curve.MaxAgreement, len(curve.Cells))
		}
		if apart := cols[3] == "yes"; apart != curve.Apart {
			t.Errorf("docs/evaluation.md: %s's apart is %q, the curve says %t", id, cols[3], curve.Apart)
		}
		plateau := regexp.MustCompile(`^\(([0-9.]+), ([0-9.]+)\)$`).FindStringSubmatch(cols[4])
		if plateau == nil {
			t.Fatalf("docs/evaluation.md: %s's plateau is %q, want \"(<low>, <high>)\"", id, cols[4])
		}
		evaluationNumberIs(t, plateau[1], curve.PlateauLow, evaluationPlateauDecimals, label+" plateau low")
		evaluationNumberIs(t, plateau[2], curve.PlateauHigh, evaluationPlateauDecimals, label+" plateau high")
		bold := strings.Contains(cols[5], "**")
		_, isOverride := settings.JudgePerCriterion[id]
		if bold != isOverride {
			t.Errorf("docs/evaluation.md: %s's line %s is %s, want bold exactly on an override", id, cols[5],
				map[bool]string{true: "bold", false: "plain"}[bold])
		}
		lineText := strings.ReplaceAll(cols[5], "**", "")
		evaluationNumberIs(t, lineText, line, evaluationLineDecimals, label+" line")
		evaluationNumberIs(t, cols[6], nearestDistance(curve.Cells, line), evaluationMarginDecimals, label+" margin")
		wantHalf := "—"
		if len(curve.OffAtHalf) > 0 {
			wantHalf = strings.Join(curve.OffAtHalf, ", ")
		}
		if cols[7] != wantHalf {
			t.Errorf("docs/evaluation.md: %s's 0.5 column is %q, the curve names %q", id, cols[7], wantHalf)
		}
	}
	for id := range curves {
		if _, ok := rows[id]; !ok {
			t.Errorf("docs/evaluation.md: curve %s is in the report and not in the per-criterion table", id)
		}
	}
}

// evaluationPinThinLines pins the thin-lines bullet: the thinnest line's
// own figures (line, plateau width, margin, and the two bounding cells),
// the five margins that follow it in order, the override counts, and the
// default-riding criterion named at its margin.
func evaluationPinThinLines(t *testing.T, doc []string, judge JudgeReport, settings Settings) {
	t.Helper()
	var bullet string
	for _, line := range doc {
		if strings.Contains(line, "**The thin lines.**") {
			if bullet != "" {
				t.Fatal("docs/evaluation.md: two thin-lines bullets")
			}
			bullet = line
		}
	}
	if bullet == "" {
		t.Fatal("docs/evaluation.md: the thin-lines bullet is gone")
	}
	lineOf := func(id string) float64 {
		if v, ok := settings.JudgePerCriterion[id]; ok {
			return v
		}
		return settings.JudgeMet
	}
	curves := map[string]Curve{}
	for _, c := range judge.Curves {
		curves[c.ID] = c
	}
	marginOf := func(id string) float64 {
		return nearestDistance(curves[id].Cells, lineOf(id))
	}

	head := regexp.MustCompile("`date-beard-even` is now the thinnest line in the product: ([0-9.]+) on a plateau ([0-9.]+) wide, a margin of ([0-9.]+), bounded by ([A-Z]\\d+)'s patchy ([0-9.]+) and ([A-Z]\\d+)'s even ([0-9.]+)\\.")
	m := head.FindStringSubmatch(bullet)
	if m == nil {
		t.Fatal("docs/evaluation.md: the thin-lines bullet no longer states the thinnest line, its plateau, margin and bounds in the shape this pin reads")
	}
	thinnest := "date-beard-even"
	evaluationNumberIs(t, m[1], lineOf(thinnest), evaluationLineDecimals, "the thin line itself")
	curve := curves[thinnest]
	evaluationNumberIs(t, m[2], curve.PlateauHigh-curve.PlateauLow, evaluationPlateauDecimals, "the thin plateau's width")
	evaluationNumberIs(t, m[3], marginOf(thinnest), evaluationMarginDecimals, "the thin margin")
	// The thinnest claim itself: no other shipped line sits closer to an
	// observed probability.
	for _, c := range judge.Curves {
		if other := marginOf(c.ID); other < marginOf(thinnest) {
			t.Errorf("docs/evaluation.md: the thin-lines bullet calls %s the thinnest line, but %s sits %.5f from its nearest probability against %.5f",
				thinnest, c.ID, other, marginOf(thinnest))
		}
	}
	// The bounds: the named cells are the nearest observed probabilities
	// on each side, at the values the doc prints.
	below, above := evaluationBoundCells(t, curve, lineOf(thinnest))
	if m[4] != below.Sample || m[6] != above.Sample {
		t.Errorf("docs/evaluation.md: the thin margin is bounded by %s below and %s above, the doc names %s and %s",
			below.Sample, above.Sample, m[4], m[6])
	}
	evaluationNumberIs(t, m[5], below.Value, evaluationPlateauDecimals, "the lower bounding cell")
	evaluationNumberIs(t, m[7], above.Value, evaluationPlateauDecimals, "the upper bounding cell")

	follow := regexp.MustCompile("After it come `family-expression-present` at ([0-9.]+), then `festival-light` ([0-9.]+), `interview-beard-short` ([0-9.]+), `funeral-beard-close` ([0-9.]+) and `family-beard-even` ([0-9.]+)\\.")
	m = follow.FindStringSubmatch(bullet)
	if m == nil {
		t.Fatal("docs/evaluation.md: the thin-lines bullet no longer lists the margins that follow the thinnest in the shape this pin reads")
	}
	ordered := []struct {
		id   string
		text string
	}{
		{"family-expression-present", m[1]},
		{"festival-light", m[2]},
		{"interview-beard-short", m[3]},
		{"funeral-beard-close", m[4]},
		{"family-beard-even", m[5]},
	}
	six := []struct {
		id     string
		margin float64
	}{{thinnest, marginOf(thinnest)}}
	for _, o := range ordered {
		evaluationNumberIs(t, o.text, marginOf(o.id), evaluationMarginDecimals, o.id+"'s thin margin")
		six = append(six, struct {
			id     string
			margin float64
		}{o.id, marginOf(o.id)})
	}
	// The list is the six thinnest lines, thinnest first: every other
	// criterion's margin sits above the last of them, and each listed
	// margin is not above the one before it.
	for i := 1; i < len(six); i++ {
		if six[i].margin < six[i-1].margin {
			t.Errorf("docs/evaluation.md: the thin-lines bullet lists %s (%.5f) before %s (%.5f), out of ascending order",
				six[i-1].id, six[i-1].margin, six[i].id, six[i].margin)
		}
	}
	for _, c := range judge.Curves {
		listed := false
		for _, s := range six {
			if s.id == c.ID {
				listed = true
			}
		}
		if !listed && marginOf(c.ID) < six[len(six)-1].margin {
			t.Errorf("docs/evaluation.md: the thin-lines bullet omits %s, whose margin %.5f is inside the six it lists",
				c.ID, marginOf(c.ID))
		}
	}

	overrides := regexp.MustCompile("Four of the six overrides are beard lines \\(`date-beard-even`, `family-beard-even`, `funeral-beard-close`, `interview-beard-short`\\)")
	if !overrides.MatchString(bullet) {
		t.Fatal("docs/evaluation.md: the thin-lines bullet no longer states the beard overrides in the shape this pin reads")
	}
	if len(settings.JudgePerCriterion) != 6 {
		t.Errorf("docs/evaluation.md: the bullet says six overrides, the settings carry %d", len(settings.JudgePerCriterion))
	}
	beardOverrides := 0
	for _, id := range []string{"date-beard-even", "family-beard-even", "funeral-beard-close", "interview-beard-short"} {
		if _, ok := settings.JudgePerCriterion[id]; !ok {
			t.Errorf("docs/evaluation.md: the bullet names %s an override, the settings do not carry it", id)
		}
	}
	for id := range settings.JudgePerCriterion {
		if strings.Contains(id, "beard") {
			beardOverrides++
		}
	}
	if beardOverrides != 4 {
		t.Errorf("docs/evaluation.md: the bullet says four of the overrides are beard lines, the settings carry %d", beardOverrides)
	}
	beardCriteria := 0
	for id := range curves {
		if strings.Contains(id, "beard") {
			beardCriteria++
		}
	}
	if beardCriteria != 5 {
		t.Errorf("docs/evaluation.md: the bullet says four of the five beard criteria need a line of their own, the set carries %d beard criteria", beardCriteria)
	}
	if _, overridden := settings.JudgePerCriterion["festival-beard"]; overridden {
		t.Error("docs/evaluation.md: the bullet says four of the five beard criteria need a line of their own, but festival-beard carries an override too")
	}

	default_ := regexp.MustCompile("`date-light-see`, thin on the T0.6 corpus at 0\\.0057, rides the default here with ([0-9.]+) of margin\\.")
	m = default_.FindStringSubmatch(bullet)
	if m == nil {
		t.Fatal("docs/evaluation.md: the thin-lines bullet no longer states date-light-see's default margin in the shape this pin reads")
	}
	if _, overridden := settings.JudgePerCriterion["date-light-see"]; overridden {
		t.Error("docs/evaluation.md: the bullet says date-light-see rides the default, the settings carry an override for it")
	}
	evaluationNumberIs(t, m[1], marginOf("date-light-see"), evaluationMarginDecimals, "date-light-see's default margin")
}

// evaluationBoundCells names the observed probabilities that bound a line:
// the nearest below and the nearest above, as cells.
func evaluationBoundCells(t *testing.T, curve Curve, line float64) (Cell, Cell) {
	t.Helper()
	var below, above Cell
	haveBelow, haveAbove := false, false
	for _, cell := range curve.Cells {
		switch {
		case cell.Value < line:
			if !haveBelow || cell.Value > below.Value {
				below, haveBelow = cell, true
			}
		default:
			if !haveAbove || cell.Value < above.Value {
				above, haveAbove = cell, true
			}
		}
	}
	if !haveBelow || !haveAbove {
		t.Fatalf("curve %s has no observed probability on each side of %.5f", curve.ID, line)
	}
	return below, above
}

// evaluationPinLiveIssueRow pins the live re-run's biggest-issue row: the
// frozen transcript's own figures, 31 recorded and 28 live, raw choice on
// both sides. They are recorded evidence about a pass that already
// happened; a re-record must not move them and the doc must not age them.
func evaluationPinLiveIssueRow(t *testing.T, doc []string) {
	t.Helper()
	found := 0
	for _, line := range doc {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "| biggest issue names a miss (or none) |") {
			continue
		}
		found++
		cols := rerecordRow(trimmed)
		if len(cols) != 3 {
			t.Fatalf("docs/evaluation.md: the live biggest-issue row %q has %d cells, want 3", trimmed, len(cols))
		}
		if cols[1] != "31/37" || cols[2] != "28/37" {
			t.Errorf("docs/evaluation.md: the live biggest-issue row reads %q and %q, want the frozen transcript's 31/37 and 28/37", cols[1], cols[2])
		}
	}
	if found != 1 {
		t.Fatalf("docs/evaluation.md: %d live biggest-issue rows, want exactly one", found)
	}
	if !containsLine(doc, "biggest-issue row below is the service's raw choice on both sides") {
		t.Error("docs/evaluation.md: the live re-run no longer names its biggest-issue row the service's raw choice")
	}
}

// containsLine reports whether the doc carries the text on some line.
func containsLine(lines []string, text string) bool {
	for _, line := range lines {
		if strings.Contains(line, text) {
			return true
		}
	}
	return false
}
