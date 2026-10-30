// Package youcam is the try-on seam. A call uploads a file, creates a
// task, polls it, and fetches the result. No face, a bad file, an expired
// file or link, and an empty unit balance are typed errors.
//
// The live client dials the v2 API. This package does not.
package youcam

import (
	"context"
	"encoding/json"
	"errors"
)

const (
	// KindNoFace is a source or reference the provider could not find a face in.
	KindNoFace Kind = "no face"
	// KindBadFile is a file the provider could not decode or accept.
	KindBadFile Kind = "bad file"
	// KindExpired is a file or a result link that is no longer available.
	KindExpired Kind = "expired"
	// KindOutOfUnits is a provider refusal because the unit balance is spent.
	KindOutOfUnits Kind = "out of units"

	// StatusRunning means the task is still working.
	StatusRunning = "running"
	// StatusSuccess means the result can be fetched.
	StatusSuccess = "success"
	// StatusError means the provider stopped the task.
	StatusError = "error"
)

// Kind is one of the four refusals the rest of Dresscode branches on.
type Kind string

// Valid reports whether k is one of the four kinds.
func (k Kind) Valid() bool {
	switch k {
	case KindNoFace, KindBadFile, KindExpired, KindOutOfUnits:
		return true
	default:
		return false
	}
}

// Error is a provider refusal. Detail is the provider's own code, when
// one was recorded. It is not a secret. errors.Is matches on Kind, so a
// detail still matches the sentinel.
type Error struct {
	Kind   Kind   `json:"kind"`
	Detail string `json:"detail,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return "youcam: error"
	}
	if e.Detail == "" {
		return "youcam: " + string(e.Kind)
	}
	return "youcam: " + string(e.Kind) + ": " + e.Detail
}

// Is reports whether target is the same kind. A sentinel has no detail
// and matches any detail of that kind.
func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	if !ok || e == nil || other == nil || e.Kind != other.Kind {
		return false
	}
	return other.Detail == "" || other.Detail == e.Detail
}

// Sentinels for the four refusals.
var (
	ErrNoFace     = &Error{Kind: KindNoFace}
	ErrBadFile    = &Error{Kind: KindBadFile}
	ErrExpired    = &Error{Kind: KindExpired}
	ErrOutOfUnits = &Error{Kind: KindOutOfUnits}
)

// File is the picture to upload. Bytes are the file itself.
type File struct {
	Task        string `json:"task"`
	ContentType string `json:"content_type"`
	Bytes       []byte `json:"bytes"`
}

// Slot is the upload. FileID is what a later task sends. URL is the
// slot the live client uploads to, when the recording has one.
type Slot struct {
	FileID string `json:"file_id"`
	URL    string `json:"url,omitempty"`
}

// Task is one create call. Payload is the task body, which differs by task.
type Task struct {
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload"`
}

// Created is a task the provider accepted.
type Created struct {
	TaskID          string `json:"task_id"`
	PollingInterval int    `json:"polling_interval,omitempty"`
}

// PollRef names the task to poll.
type PollRef struct {
	Task   string `json:"task"`
	TaskID string `json:"task_id"`
}

// Status is one poll. A provider refusal is the error from Poll, not a
// status with a nil error.
type Status struct {
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
	Error     *Error `json:"error,omitempty"`
	ResultURL string `json:"result_url,omitempty"`
	// FaceShape is the analysis result, when the task was an analysis one.
	// It is the only analysis field on the seam: age, gender and ratios
	// are never decoded into it, even when a reply carries them.
	FaceShape string `json:"face_shape,omitempty"`
}

// ResultRef names a result to copy. URL is the provider's link.
type ResultRef struct {
	TaskID string `json:"task_id"`
	URL    string `json:"url"`
}

// Result is the copied result. Bytes are the picture, when the recording
// kept them. The live client copies them because the link expires.
type Result struct {
	TaskID string `json:"task_id"`
	URL    string `json:"url,omitempty"`
	Bytes  []byte `json:"bytes,omitempty"`
}

// YouCam uploads, creates, polls, and fetches. It does not dial by itself.
type YouCam interface {
	Upload(ctx context.Context, file File) (Slot, error)
	Create(ctx context.Context, task Task) (Created, error)
	Poll(ctx context.Context, ref PollRef) (Status, error)
	Fetch(ctx context.Context, ref ResultRef) (Result, error)
}

func (f File) validate() error {
	if f.Task == "" {
		return errors.New("youcam: task is empty")
	}
	if f.ContentType == "" {
		return errors.New("youcam: content type is empty")
	}
	if len(f.Bytes) == 0 {
		return errors.New("youcam: file is empty")
	}
	return nil
}

func (t Task) validate() error {
	if t.Name == "" {
		return errors.New("youcam: task is empty")
	}
	if len(t.Payload) == 0 || string(t.Payload) == "null" {
		return errors.New("youcam: payload is empty")
	}
	if !json.Valid(t.Payload) {
		return errors.New("youcam: payload is not JSON")
	}
	return nil
}

func (r PollRef) validate() error {
	if r.Task == "" || r.TaskID == "" {
		return errors.New("youcam: poll needs a task and a task id")
	}
	return nil
}

func (r ResultRef) validate() error {
	if r.TaskID == "" || r.URL == "" {
		return errors.New("youcam: fetch needs a task id and a url")
	}
	return nil
}
