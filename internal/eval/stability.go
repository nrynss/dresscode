package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nrynss/dresscode/internal/systemone"
)

// StabilityReport is the repeat measurement: the same judge request,
// recorded eleven times (the ten repeats and the original), compared
// cell by cell. Clef is deterministic for one request body, so the
// expected flip count is zero, and any flip names itself.
type StabilityReport struct {
	Answers int      `json:"answers"`
	Cells   int      `json:"cells"`
	Flips   []string `json:"flips"`
	// Criteria names the criteria whose reading moves across repeats -
	// the criteria only a rewording could settle. Empty when the
	// repeats agree, as they do.
	Criteria []string `json:"criteria"`
	// Issue is the biggest issue all eleven answers named, empty if
	// they disagreed.
	Issue string `json:"issue"`
}

// answerCells flattens one answer set into comparable cells.
type answerCells struct {
	noul   map[string]float64 // question id -> yes probability
	choice map[string]string  // choice question id -> chosen option
	prob   map[string]float64 // "id:option" -> probability
	weight map[string]float64 // "id:confidence" -> confidence
}

func cellsOf(resp systemone.Response) (answerCells, error) {
	cells := answerCells{
		noul:   map[string]float64{},
		choice: map[string]string{},
		prob:   map[string]float64{},
		weight: map[string]float64{},
	}
	for id, answer := range resp.Answers {
		switch answer.Type {
		case systemone.TypeNoul:
			if answer.Noul == nil {
				return answerCells{}, fmt.Errorf("eval: %s: noul without a value", id)
			}
			cells.noul[id] = *answer.Noul
		case systemone.TypeChoice:
			cells.choice[id] = answer.Choice
			for option, p := range answer.Probabilities {
				cells.prob[id+":"+option] = p
			}
			if answer.Confidence != nil {
				cells.weight[id+":confidence"] = *answer.Confidence
			}
		default:
			return answerCells{}, fmt.Errorf("eval: %s: unexpected answer type %q", id, answer.Type)
		}
	}
	return cells, nil
}

// openReply opens dir and returns the recording stored under key.
func openReply(dir, key string) (systemone.Response, error) {
	replay, err := systemone.Open(dir)
	if err != nil {
		return systemone.Response{}, err
	}
	rec, err := replay.Reply(key)
	if err != nil {
		return systemone.Response{}, err
	}
	return answerEnvelope(rec.Response)
}

// listExchanges names every recording in dir, by exchange id, sorted.
func listExchanges(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	sort.Strings(names)
	return names, nil
}

// compareStability reads the ten recorded repeats of the V04
// family-function judge request and compares every answer cell with
// the original judge recording. The repeats live in their own folder
// because their request is byte-identical to the top-level judge call,
// so the replay refuses to choose between them (docs/clef.md).
func compareStability(clefDir string) (StabilityReport, error) {
	original, err := openReply(clefDir, "judge-V04-family-function")
	if err != nil {
		return StabilityReport{}, err
	}
	base, err := cellsOf(original)
	if err != nil {
		return StabilityReport{}, err
	}
	repeatDir := filepath.Join(clefDir, "stability")
	replay, err := systemone.Open(repeatDir)
	if err != nil {
		return StabilityReport{}, err
	}
	exchanges, err := listExchanges(repeatDir)
	if err != nil {
		return StabilityReport{}, err
	}
	report := StabilityReport{Answers: 1 + len(exchanges)}
	flippedCriteria := map[string]bool{}
	count := func(m map[string]float64) int { return len(m) }
	report.Cells = len(base.noul) + len(base.choice) + count(base.prob) + count(base.weight)
	for _, exchange := range exchanges {
		rec, err := replay.Reply(exchange)
		if err != nil {
			return StabilityReport{}, err
		}
		resp, err := answerEnvelope(rec.Response)
		if err != nil {
			return StabilityReport{}, err
		}
		got, err := cellsOf(resp)
		if err != nil {
			return StabilityReport{}, err
		}
		if len(got.noul) != len(base.noul) || len(got.choice) != len(base.choice) ||
			len(got.prob) != len(base.prob) || len(got.weight) != len(base.weight) {
			return StabilityReport{}, fmt.Errorf("eval: %s: answer shape differs from the original", exchange)
		}
		for id, v := range base.noul {
			if got.noul[id] != v {
				report.Flips = append(report.Flips, fmt.Sprintf("%s: %s %.4f to %.4f", exchange, id, v, got.noul[id]))
				if id != "biggest_issue" {
					flippedCriteria[id] = true
				}
			}
		}
		for id, v := range base.choice {
			if got.choice[id] != v {
				report.Flips = append(report.Flips, fmt.Sprintf("%s: %s chose %q, then %q", exchange, id, v, got.choice[id]))
			}
		}
		for id, v := range base.prob {
			if got.prob[id] != v {
				report.Flips = append(report.Flips, fmt.Sprintf("%s: %s %.4f to %.4f", exchange, id, v, got.prob[id]))
			}
		}
		for id, v := range base.weight {
			if got.weight[id] != v {
				report.Flips = append(report.Flips, fmt.Sprintf("%s: %s %.4f to %.4f", exchange, id, v, got.weight[id]))
			}
		}
	}
	sort.Strings(report.Flips)
	for id := range flippedCriteria {
		report.Criteria = append(report.Criteria, id)
	}
	sort.Strings(report.Criteria)
	if len(report.Flips) == 0 {
		report.Issue = base.choice["biggest_issue"]
	}
	return report, nil
}

// SizeReport is the size sweep: the same photo recorded at three
// smaller sizes, read under the chosen settings against the full-size
// recording. It answers docs/clef.md's open question - do the flags
// survive a resize - for the one photo the recordings cover.
type SizeReport struct {
	Variants []SizeRead `json:"variants"`
	// Note records the no-image recording, which is the evidence
	// behind the judge's empty-photo refusal and not a flag read.
	Note string `json:"note"`
}

// SizeRead is one resized recording's flag movement.
type SizeRead struct {
	Label      string   `json:"label"`
	FlagsMoved []string `json:"flags_moved"`
}

// compareSizes reads the resized judge recordings of V04 and compares
// each criterion's flag under the chosen settings with the full-size
// read at the same settings.
func compareSizes(costDir string, full systemone.Response, lines map[string]float64, met float64) (SizeReport, error) {
	base, err := cellsOf(full)
	if err != nil {
		return SizeReport{}, err
	}
	replay, err := systemone.Open(costDir)
	if err != nil {
		return SizeReport{}, err
	}
	exchanges, err := listExchanges(costDir)
	if err != nil {
		return SizeReport{}, err
	}
	var report SizeReport
	for _, exchange := range exchanges {
		if strings.HasSuffix(exchange, "-noimage") {
			report.Note = "the no-image recording is the evidence behind the judge's empty-photo refusal (every criterion answers yes without a photo); it is not a flag read"
			continue
		}
		rec, err := replay.Reply(exchange)
		if err != nil {
			return SizeReport{}, err
		}
		resp, err := answerEnvelope(rec.Response)
		if err != nil {
			return SizeReport{}, err
		}
		got, err := cellsOf(resp)
		if err != nil {
			return SizeReport{}, err
		}
		read := SizeRead{Label: sizeLabel(exchange)}
		var ids []string
		for id := range base.noul {
			if id != "biggest_issue" {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		for _, id := range ids {
			line := met
			if v, ok := lines[id]; ok {
				line = v
			}
			if (base.noul[id] >= line) != (got.noul[id] >= line) {
				read.FlagsMoved = append(read.FlagsMoved, id)
			}
		}
		report.Variants = append(report.Variants, read)
	}
	sort.Slice(report.Variants, func(i, j int) bool { return report.Variants[i].Label < report.Variants[j].Label })
	return report, nil
}

// sizeLabel names a cost recording by its size.
func sizeLabel(exchange string) string {
	parts := strings.Split(exchange, "-")
	return parts[len(parts)-1]
}
