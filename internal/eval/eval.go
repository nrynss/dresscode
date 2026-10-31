// Package eval replays the recorded Clef calls against the sample set's
// ground truth and chooses every threshold from measurement, so the
// settings file holds no guessed number. It reads the T0.6 recordings
// through the same replay machinery the pipeline uses, sweeps each
// criterion's and each gate's line over its own curve, and reports the
// numbers docs/evaluation.md quotes and the README and video cite.
//
// Nothing here dials. One Run opens testdata/clef (judge and check
// recordings), testdata/clef/stability (the ten repeats), and
// testdata/clef/cost (the size sweep), replays them through the real
// pipeline.Judge and pipeline.Check with a capturing seam, and returns
// one deterministic Report. Two runs over the same repository give the
// same Report bit for bit.
//
// The thresholds this package chooses are numbers for settings and for
// docs. They are not a reading for a user: probabilities steer the
// pipeline and die there, and no screen shows one.
package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
)

// The reading docs/clef.md counts agreement at, and the anchor this
// package's sanity checks are stated against. It is a measurement
// point on every curve, never a setting.
const readingAt = 0.5

// GroundTruth is the sample set's expected reads: each variant's met and
// miss criteria, and each recorded check's true look. The values are
// transcribed from testdata/samples/README.md (T0.4), the evaluation's
// ground truth.
type GroundTruth struct {
	Kind    string       `json:"kind"`
	ID      string       `json:"id"`
	Source  string       `json:"source"`
	Samples []Sample     `json:"samples"`
	Checks  []CheckTruth `json:"checks"`
}

// Sample is one variant's ground truth. Met lists the criteria the shot
// should pass; Miss the criteria it should fail. Together they are
// exactly the occasion's criteria, each in one list.
type Sample struct {
	ID       string   `json:"id"`
	Occasion string   `json:"occasion"`
	Met      []string `json:"met"`
	Miss     []string `json:"miss"`
}

// Expected reports whether the sample should pass the criterion, and
// whether the ground truth names the criterion at all.
func (s Sample) Expected(id string) (want bool, ok bool) {
	for _, c := range s.Met {
		if c == id {
			return true, true
		}
	}
	for _, c := range s.Miss {
		if c == id {
			return false, true
		}
	}
	return false, false
}

// CheckTruth is one recorded check's ground truth: which model read it,
// whether the render is one of the known bad ones, and the render's true
// attributes in the vocabulary's own values. Notes carry the judgement
// calls a human made where the vocabulary cannot say the truth exactly.
type CheckTruth struct {
	Exchange string            `json:"exchange"`
	Model    string            `json:"model"`
	Bad      bool              `json:"bad"`
	Look     map[string]string `json:"look"`
	Notes    map[string]string `json:"notes,omitempty"`
}

// LoadGroundTruth reads the transcribed ground truth.
func LoadGroundTruth(path string) (GroundTruth, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return GroundTruth{}, fmt.Errorf("eval: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var gt GroundTruth
	if err := dec.Decode(&gt); err != nil {
		return GroundTruth{}, fmt.Errorf("eval: %s: %w", path, err)
	}
	if gt.Kind != "ground-truth" {
		return GroundTruth{}, fmt.Errorf("eval: %s: kind %q, want ground-truth", path, gt.Kind)
	}
	seenSample := map[string]bool{}
	for _, s := range gt.Samples {
		if s.ID == "" || s.Occasion == "" || len(s.Met)+len(s.Miss) == 0 {
			return GroundTruth{}, fmt.Errorf("eval: %s: sample %q is incomplete", path, s.ID)
		}
		if seenSample[s.ID] {
			return GroundTruth{}, fmt.Errorf("eval: %s: sample %q is repeated", path, s.ID)
		}
		seenSample[s.ID] = true
	}
	seenCheck := map[string]bool{}
	for _, c := range gt.Checks {
		if c.Exchange == "" || c.Model == "" || len(c.Look) == 0 {
			return GroundTruth{}, fmt.Errorf("eval: %s: check %q is incomplete", path, c.Exchange)
		}
		if seenCheck[c.Exchange] {
			return GroundTruth{}, fmt.Errorf("eval: %s: check %q is repeated", path, c.Exchange)
		}
		seenCheck[c.Exchange] = true
	}
	return gt, nil
}

// Cell is one criterion read on one sample: the raw yes-probability and
// what the shot should have read. Expected true means the criterion
// should pass; false means it should be flagged.
type Cell struct {
	Sample   string  `json:"sample"`
	Value    float64 `json:"value"`
	Expected bool    `json:"expected"`
}

// agreement counts the cells a threshold reads correctly. A cell
// expected met reads met at or above the line; a cell expected miss
// reads flagged under it.
func agreement(cells []Cell, th float64) int {
	n := 0
	for _, c := range cells {
		if (c.Value >= th) == c.Expected {
			n++
		}
	}
	return n
}

// Curve is one criterion's measurement: every cell, the best plateau on
// its curve, and the line this evaluation chose at the plateau's
// midpoint - the point farthest from any observed probability, so the
// reading is the one a stray digit moves least.
type Curve struct {
	ID        string   `json:"id"`
	Occasions []string `json:"occasions"`
	Cells     []Cell   `json:"cells"`
	// MaxAgreement is the most cells any line reads correctly. Apart
	// reports whether some line reads them all.
	MaxAgreement int  `json:"max_agreement"`
	Apart        bool `json:"apart"`
	// PlateauLow and PlateauHigh bound the best plateau: every line in
	// the open interval reads MaxAgreement cells correctly. Chosen is
	// its midpoint and Margin the distance to the nearest observed
	// probability.
	PlateauLow  float64 `json:"plateau_low"`
	PlateauHigh float64 `json:"plateau_high"`
	Chosen      float64 `json:"chosen"`
	Margin      float64 `json:"margin"`
	// OffAtHalf names the samples the recorded reading puts on the other
	// side of 0.5 from the ground truth - the curve's sanity anchors.
	OffAtHalf []string `json:"off_at_half"`
}

// sweep finds the best plateau on one criterion's curve. The agreement
// of a line only changes at an observed value, so the candidates are
// the open intervals between consecutive distinct values, bounded by
// the line's own [0, 1] range at the ends; the widest best interval is
// the curve's most robust reading.
func sweep(cells []Cell) Curve {
	sorted := append([]Cell(nil), cells...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Value != sorted[j].Value {
			return sorted[i].Value < sorted[j].Value
		}
		return sorted[i].Sample < sorted[j].Sample
	})
	curve := Curve{Cells: sorted}
	curve.OffAtHalf = offAt(cells, readingAt)
	best := -1
	var bestLow, bestHigh float64
	for i := 0; i <= len(sorted); i++ {
		// The open interval before sorted[i]: [0, first) before the
		// first value, (last, 1] after it, (sorted[i-1], sorted[i])
		// between two.
		var low, high float64
		switch {
		case i == 0:
			low, high = 0, sorted[0].Value
		case i == len(sorted):
			low, high = sorted[i-1].Value, 1
		default:
			low, high = sorted[i-1].Value, sorted[i].Value
		}
		if low == high {
			continue
		}
		mid := intervalMid(low, high)
		a := agreement(sorted, mid)
		width := high - low
		switch {
		case a > best:
			best, bestLow, bestHigh = a, low, high
		case a == best && width > bestHigh-bestLow:
			bestLow, bestHigh = low, high
		}
	}
	curve.MaxAgreement = best
	curve.PlateauLow, curve.PlateauHigh = bestLow, bestHigh
	curve.Apart = best == len(cells)
	curve.Chosen = intervalMid(bestLow, bestHigh)
	curve.Margin = nearestDistance(sorted, curve.Chosen)
	return curve
}

// intervalMid is the interval's midpoint, rounded to five decimals so
// the value is exact for the settings file and stays strictly inside
// the open interval: the recorded probabilities carry four decimals, so
// the midpoint of two of them is exact at five.
func intervalMid(low, high float64) float64 {
	return math.Round((low+high)/2*1e5) / 1e5
}

// nearestDistance is how far a line sits from the closest observed
// probability - the margin a stray digit would have to cross to change
// the reading.
func nearestDistance(cells []Cell, th float64) float64 {
	d := math.Inf(1)
	for _, c := range cells {
		if v := math.Abs(c.Value - th); v < d {
			d = v
		}
	}
	return d
}

// offAt names the samples whose recorded probability sits on the other
// side of th from the ground truth, in the cells' sorted order.
func offAt(cells []Cell, th float64) []string {
	sorted := append([]Cell(nil), cells...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Sample < sorted[j].Sample })
	var off []string
	for _, c := range sorted {
		if (c.Value >= th) != c.Expected {
			off = append(off, c.Sample)
		}
	}
	return off
}

// chooseDefault picks the single line that reads the most cells
// correctly across all criteria when no override applies. Ties go to
// the line needing fewer per-criterion overrides, then to the one
// closest to 0.5. The agreement of a line only changes at an observed
// value, so the candidates are the intervals between the union of all
// observed probabilities, plus the 0.5 reading itself.
func chooseDefault(curves []Curve) (th float64, total int) {
	var edges []float64
	for _, c := range curves {
		for _, cell := range c.Cells {
			edges = append(edges, cell.Value)
		}
	}
	sort.Float64s(edges)
	candidates := []float64{readingAt}
	for i := 1; i < len(edges); i++ {
		if edges[i] > edges[i-1] {
			candidates = append(candidates, intervalMid(edges[i-1], edges[i]))
		}
	}
	best := -1
	bestOverrides := math.MaxInt
	for _, cand := range candidates {
		total := 0
		overrides := 0
		for _, c := range curves {
			a := agreement(c.Cells, cand)
			total += a
			if a < c.MaxAgreement {
				overrides++
			}
		}
		if total > best ||
			(total == best && overrides < bestOverrides) ||
			(total == best && overrides == bestOverrides && math.Abs(cand-readingAt) < math.Abs(th-readingAt)) {
			best, bestOverrides, th = total, overrides, cand
		}
	}
	return th, best
}
