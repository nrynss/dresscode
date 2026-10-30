// Package systemone is the decision-model seam. One call takes a model
// name, optional embedded images, a JSON state, and choice, noul, and
// score questions. It returns each answer's probabilities and confidence.
//
// Images are bytes. A URL is not an image. clef and clef-flash are the
// Workers AI names. The caller names Jev when that comparison runs; this
// package does not pick a provider.
package systemone

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	// TypeChoice picks one option and returns a probability per option.
	TypeChoice Kind = "choice"
	// TypeNoul is a yes/no question. The probability is that the answer is yes.
	TypeNoul Kind = "noul"
	// TypeScore rates an ordered rubric and returns a probability per level.
	TypeScore Kind = "score"

	// ModelClef is the Workers AI name for Clef.
	ModelClef = "clef"
	// ModelClefFlash is the Workers AI name for Clef-flash.
	ModelClefFlash = "clef-flash"

	maxQuestions = 64
	maxImages    = 4
)

// Kind is choice, noul, or score.
type Kind string

// Valid reports whether k is one of the three question types.
func (k Kind) Valid() bool {
	switch k {
	case TypeChoice, TypeNoul, TypeScore:
		return true
	default:
		return false
	}
}

// Image is one embedded picture. Bytes are the file, not a URL and not
// a base64 text. ContentType is image/png, image/jpeg, or image/webp.
// The live client refuses an image Clef will not take. A recording of
// one can still be replayed, so this type does not measure the bytes.
type Image struct {
	ContentType string
	Bytes       []byte
}

type imageWire struct {
	ContentType string `json:"content_type"`
	Base64      []byte `json:"base64"`
}

// MarshalJSON writes the object form the System One API accepts.
func (m Image) MarshalJSON() ([]byte, error) {
	return json.Marshal(imageWire{ContentType: m.ContentType, Base64: m.Bytes})
}

// UnmarshalJSON accepts that object or a base64 data URL. Both become bytes.
func (m *Image) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return errors.New("systemone: image is empty")
	}
	if trimmed[0] == '"' {
		var raw string
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			return fmt.Errorf("systemone: image: %w", err)
		}
		return m.fromDataURL(raw)
	}
	var wire imageWire
	if err := json.Unmarshal(trimmed, &wire); err != nil {
		return fmt.Errorf("systemone: image: %w", err)
	}
	if wire.ContentType == "" || len(wire.Base64) == 0 {
		return errors.New("systemone: image is empty")
	}
	m.ContentType = wire.ContentType
	m.Bytes = append([]byte(nil), wire.Base64...)
	return nil
}

func (m *Image) fromDataURL(raw string) error {
	const marker = "data:"
	if !strings.HasPrefix(strings.ToLower(raw), marker) {
		return errors.New("systemone: image is not a data URL")
	}
	rest := raw[len(marker):]
	head, payload, ok := strings.Cut(rest, ",")
	if !ok || payload == "" || !strings.Contains(strings.ToLower(head), ";base64") {
		return errors.New("systemone: image is not a base64 data URL")
	}
	media, _, _ := strings.Cut(head, ";")
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(payload)
	}
	if err != nil || media == "" || len(decoded) == 0 {
		return errors.New("systemone: image data URL is not readable")
	}
	m.ContentType = media
	m.Bytes = decoded
	return nil
}

// Question is one typed question. Criteria is a JSON object for choice
// and for noul, and a JSON array of levels for score, lowest first.
// Noul may omit criteria.
type Question struct {
	Type         Kind            `json:"type"`
	Instructions string          `json:"instructions"`
	Criteria     json.RawMessage `json:"criteria,omitempty"`
}

// Request is one decision call. State is any JSON value except null.
// Images may be empty.
type Request struct {
	Model     string              `json:"model"`
	Images    []Image             `json:"images,omitempty"`
	State     json.RawMessage     `json:"state"`
	Questions map[string]Question `json:"questions"`
}

// Answer is one question's result. Noul carries the yes probability.
// Choice and score carry a probability per option and a confidence.
// A missing confidence is nil, not a stored zero.
type Answer struct {
	Type          Kind                       `json:"type"`
	Choice        string                     `json:"choice,omitempty"`
	Noul          *float64                   `json:"noul,omitempty"`
	Score         *float64                   `json:"score,omitempty"`
	Legend        map[string]json.RawMessage `json:"legend,omitempty"`
	Probabilities map[string]float64         `json:"probabilities,omitempty"`
	Confidence    *float64                   `json:"confidence,omitempty"`
}

// Usage is the token count a provider reported. Zero means the recording
// did not carry a count.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Response is the model's answers, keyed by the same question ids.
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
}

// SystemOne is one decision call. Ask does not dial. A provider
// implementation and the replay both satisfy it.
type SystemOne interface {
	Ask(ctx context.Context, req Request) (Response, error)
}

// Validate reports a request the model would refuse for its shape.
// It does not look at image dimensions.
func (r Request) Validate() error {
	if strings.TrimSpace(r.Model) == "" {
		return errors.New("systemone: model is empty")
	}
	if len(bytes.TrimSpace(r.State)) == 0 || string(bytes.TrimSpace(r.State)) == "null" {
		return errors.New("systemone: state is empty")
	}
	if !json.Valid(r.State) {
		return errors.New("systemone: state is not JSON")
	}
	if len(r.Questions) == 0 || len(r.Questions) > maxQuestions {
		return errors.New("systemone: a call needs 1 to 64 questions")
	}
	if len(r.Images) > maxImages {
		return errors.New("systemone: a call takes at most 4 images")
	}
	for i, image := range r.Images {
		if err := image.validate(); err != nil {
			return fmt.Errorf("systemone: image %d: %w", i, err)
		}
	}
	for id, question := range r.Questions {
		if err := question.validate(id); err != nil {
			return err
		}
	}
	return nil
}

func (m Image) validate() error {
	switch m.ContentType {
	case "image/png", "image/jpeg", "image/webp":
	default:
		return errors.New("content type must be image/png, image/jpeg, or image/webp")
	}
	if len(m.Bytes) == 0 {
		return errors.New("bytes are empty")
	}
	return nil
}

func (q Question) validate(id string) error {
	if !validID(id) {
		return fmt.Errorf("systemone: question id %q is not usable", id)
	}
	if strings.TrimSpace(q.Instructions) == "" {
		return fmt.Errorf("systemone: question %q has no instructions", id)
	}
	if !q.Type.Valid() {
		return fmt.Errorf("systemone: question %q has unknown type %q", id, q.Type)
	}
	criteria := bytes.TrimSpace(q.Criteria)
	switch q.Type {
	case TypeNoul:
		if len(criteria) == 0 || string(criteria) == "null" {
			return nil
		}
		if !jsonObject(criteria) {
			return fmt.Errorf("systemone: noul %q criteria must be an object", id)
		}
	case TypeChoice:
		var options map[string]json.RawMessage
		if err := json.Unmarshal(criteria, &options); err != nil || len(options) < 2 || len(options) > 255 {
			return fmt.Errorf("systemone: choice %q needs 2 to 255 options", id)
		}
		for option := range options {
			if option == "" {
				return fmt.Errorf("systemone: choice %q has an empty option", id)
			}
		}
	case TypeScore:
		var levels []json.RawMessage
		if err := json.Unmarshal(criteria, &levels); err != nil || len(levels) < 2 || len(levels) > 10 {
			return fmt.Errorf("systemone: score %q needs 2 to 10 levels", id)
		}
	}
	return nil
}

func validID(id string) bool {
	if id == "" || len(id) > 100 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
		default:
			return false
		}
	}
	return true
}

func jsonObject(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(trimmed, &object) == nil
}

func (a Answer) complete() bool {
	switch a.Type {
	case TypeNoul:
		return a.Noul != nil
	case TypeChoice:
		return a.Choice != "" && len(a.Probabilities) > 0 && a.Confidence != nil
	case TypeScore:
		return a.Score != nil && len(a.Probabilities) > 0 && a.Confidence != nil && len(a.Legend) > 0
	default:
		return false
	}
}
