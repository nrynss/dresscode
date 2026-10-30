package domain

import (
	"encoding/json"
	"fmt"
)

// Fidelity is the label shown on a product. The three values are the only
// ones the app may show.
type Fidelity string

// The three fidelity labels. Catalog files spell the first two "exact"
// and "approximate"; those decode as the labels below.
const (
	ExactProduct     Fidelity = "exact product"
	ApproximateMatch Fidelity = "approximate match"
	VisualSimulation Fidelity = "visual simulation"
)

// Valid reports whether f is one of the three labels.
func (f Fidelity) Valid() bool {
	switch f {
	case ExactProduct, ApproximateMatch, VisualSimulation:
		return true
	default:
		return false
	}
}

// UnmarshalJSON accepts a product label, or the catalog file's short
// form "exact" or "approximate".
func (f *Fidelity) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	parsed, ok := parseFidelity(raw)
	if !ok {
		return fmt.Errorf("unknown fidelity %q", raw)
	}
	*f = parsed
	return nil
}

// MarshalJSON writes the product label. An unknown value is an error.
func (f Fidelity) MarshalJSON() ([]byte, error) {
	if !f.Valid() {
		return nil, fmt.Errorf("unknown fidelity %q", string(f))
	}
	return json.Marshal(string(f))
}

func parseFidelity(raw string) (Fidelity, bool) {
	switch raw {
	case string(ExactProduct), "exact":
		return ExactProduct, true
	case string(ApproximateMatch), "approximate":
		return ApproximateMatch, true
	case string(VisualSimulation):
		return VisualSimulation, true
	default:
		return "", false
	}
}
