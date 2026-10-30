package youcam

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/mediastore"
	"github.com/nrynss/keel/providertask"
)

// UnitsDenomination is the cost denomination YouCam units settle in. One
// whole unit is one minor unit, because a unit only ever spends whole.
var UnitsDenomination = cost.Denomination{Name: "youcam unit", Minor: 1}

// unitsPerTask is the rate card, in units per call, as docs/youcam.md
// records them: the documented price and the measured one agree on every
// task the fix menus name. A task outside the card is refused before any
// quote, any upload and any reserve.
var unitsPerTask = map[string]cost.Price{
	"ai-smile":           1,
	"makeup-vto":         1,
	"2d-vto/earring":     1,
	"2d-vto/necklace":    1,
	"cloth-v4":           2,
	"hair-transfer":      2,
	"beard-style":        2,
	"face-attr-analysis": 10,
}

// UnitsFor quotes what one call of a task costs. The second return names
// whether the task is on the rate card.
func UnitsFor(task string) (cost.Price, bool) {
	units, ok := unitsPerTask[task]
	return units, ok
}

// Job is one YouCam task under an idempotency key. The files are uploaded
// through the File API as part of the create, so a resume of a recorded
// task never re-uploads, and the payload builder turns the uploaded file
// ids into the task body.
type Job struct {
	// Key is the idempotency key. One key is one task at the provider,
	// ever: a restart that runs the same key again resumes the recorded
	// task and never creates (and pays for) it twice.
	Key string
	// Task is the YouCam task name, on the rate card.
	Task string
	// Files are uploaded in order: the source first, then any references.
	Files []File
	// Payload builds the task body from the uploaded file ids, in the
	// order the files were given.
	Payload func(fileIDs []string) (json.RawMessage, error)
	// Owner names who the copied result belongs to in mediastore.
	Owner string
	// Group is the eviction unit the copied result joins in mediastore.
	Group string
}

func (j Job) validate() error {
	if j.Key == "" {
		return errors.New("youcam: idempotency key is empty")
	}
	if j.Task == "" {
		return errors.New("youcam: task is empty")
	}
	if j.Payload == nil {
		return errors.New("youcam: payload is nil")
	}
	for i, file := range j.Files {
		if err := file.validate(); err != nil {
			return fmt.Errorf("youcam: file %d: %w", i, err)
		}
	}
	return nil
}

// Runner carries one YouCam task from upload to a copied result. The
// create and the poll run through Keel providertask under the job's
// idempotency key, so a restart mid-poll resumes the recorded task without
// a second create; the units are quoted, reserved and settled through Keel
// cost, once per key; and the result is copied into mediastore the moment
// it is in hand, because the provider's result link expires in two hours.
// Safe for concurrent use.
type Runner struct {
	client YouCam
	store  providertask.Store
	meter  *cost.Meter
	media  *mediastore.Store
	tune   providertask.Config
}

// RunnerConfig builds a Runner. The meter, the store and the media store
// are all required: a task without a meter would spend outside the budget,
// one without a store could not resume, and one without a media store
// could not keep a result whose link expires.
type RunnerConfig struct {
	Store providertask.Store
	Meter *cost.Meter
	Media *mediastore.Store
	// Config tunes the providertask run: the deadline, the poll pace, the
	// clock, the coordinator, the logger. The retry classification, the
	// meter, the estimate and the ledger kind are the runner's own.
	Config providertask.Config
}

// NewRunner returns a Runner. It does not dial.
func NewRunner(client YouCam, cfg RunnerConfig) (*Runner, error) {
	if client == nil {
		return nil, errors.New("youcam: client is nil")
	}
	if cfg.Store == nil {
		return nil, errors.New("youcam: providertask store is nil")
	}
	if cfg.Meter == nil {
		return nil, errors.New("youcam: cost meter is nil")
	}
	if cfg.Media == nil {
		return nil, errors.New("youcam: media store is nil")
	}
	tune := cfg.Config
	// Only a wait can fix a poll error: a rate limit, a server fault, a
	// transport timeout. A refusal and the caller's own deadline are final,
	// and the providertask package never retries a provider verdict of
	// failed whatever this says.
	tune.Retryable = func(err error) bool {
		var status *HTTPError
		if errors.As(err, &status) {
			return status.Retryable()
		}
		return isTimeout(err)
	}
	return &Runner{client: client, store: cfg.Store, meter: cfg.Meter, media: cfg.Media, tune: tune}, nil
}

// Outcome is what one Run finished with. MediaID names the result copied
// into mediastore, and is empty for an analysis task, whose result is the
// FaceShape.
type Outcome struct {
	TaskID    string
	Task      string
	MediaID   string
	FaceShape string
}

// taskValue is the result one Run hands itself: the analysis field and the
// result bytes, before the keep copies the bytes into mediastore.
type taskValue struct {
	faceShape string
	bytes     []byte
}

// Run carries one job from upload to kept result. The units the task's
// rate-card line quotes are reserved before anything is uploaded, so a
// budget refusal spends nothing at all, and settled once per key when the
// task succeeds. A task the provider ends in error is refunded there (a
// net zero on the books, as docs/youcam.md records), so nothing settles
// for it either. The typed refusals are the seam's *Error: no face, a bad
// file, an expired file, and out of units.
func (r *Runner) Run(ctx context.Context, job Job) (Outcome, error) {
	if err := ctx.Err(); err != nil {
		return Outcome{}, err
	}
	if err := job.validate(); err != nil {
		return Outcome{}, err
	}
	units, ok := UnitsFor(job.Task)
	if !ok {
		return Outcome{}, fmt.Errorf("youcam: task %q is not on the rate card", job.Task)
	}
	cfg := r.tune
	cfg.Kind = "youcam." + job.Task
	cfg.Estimate = units
	cfg.Meter = r.meter

	var mediaID string
	out, err := providertask.Run(ctx, cfg, r.store, job.Key, providertask.Spec[taskValue]{
		Create: func(ctx context.Context) (string, error) {
			ids := make([]string, 0, len(job.Files))
			for _, file := range job.Files {
				slot, err := r.client.Upload(ctx, file)
				if err != nil {
					return "", err
				}
				ids = append(ids, slot.FileID)
			}
			payload, err := job.Payload(ids)
			if err != nil {
				return "", err
			}
			created, err := r.client.Create(ctx, Task{Name: job.Task, Payload: payload})
			if err != nil {
				return "", err
			}
			return created.TaskID, nil
		},
		Status: func(ctx context.Context, taskID string) (providertask.Status, error) {
			return r.status(ctx, job.Task, taskID)
		},
		Result: func(ctx context.Context, taskID string) (taskValue, error) {
			return r.result(ctx, job.Task, taskID)
		},
		Keep: func(ctx context.Context, value taskValue) error {
			if len(value.bytes) == 0 {
				return nil // an analysis result is the face shape alone
			}
			id, err := r.keep(ctx, job, value.bytes)
			if err != nil {
				return err
			}
			mediaID = id
			return nil
		},
		Price: func(ctx context.Context, value taskValue) (cost.Price, error) {
			// The measured cost is the rate-card line; the balance read
			// before and after every recorded task agreed with it.
			return units, nil
		},
	})
	if err != nil {
		var fail *providertask.TaskFailure
		if errors.As(err, &fail) {
			if kind, ok := engineKinds[fail.Code]; ok {
				return Outcome{}, &Error{Kind: kind, Detail: fail.Code}
			}
			return Outcome{}, err
		}
		return Outcome{}, err
	}
	return Outcome{
		TaskID:    out.TaskID,
		Task:      job.Task,
		MediaID:   mediaID,
		FaceShape: out.Value.faceShape,
	}, nil
}

// status maps one poll onto the providertask answer. A provider verdict is
// the failed state with its code, never an error, so the poll loop treats
// it as terminal instead of waiting on it.
func (r *Runner) status(ctx context.Context, task, taskID string) (providertask.Status, error) {
	st, err := r.client.Poll(ctx, PollRef{Task: task, TaskID: taskID})
	if err != nil {
		var ye *Error
		if errors.As(err, &ye) {
			code := ye.Detail
			if code == "" {
				code = string(ye.Kind)
			}
			return providertask.Status{State: providertask.StateFailed, Code: code}, nil
		}
		return providertask.Status{}, err
	}
	switch st.Status {
	case StatusRunning:
		return providertask.Status{State: providertask.StateRunning}, nil
	case StatusSuccess:
		return providertask.Status{State: providertask.StateSucceeded}, nil
	case StatusError:
		code := ""
		if st.Error != nil {
			code = st.Error.Detail
			if code == "" {
				code = string(st.Error.Kind)
			}
		}
		return providertask.Status{State: providertask.StateFailed, Code: code}, nil
	default:
		return providertask.Status{}, fmt.Errorf("youcam: poll named no known status %q", st.Status)
	}
}

// result fetches the outcome once the task is terminal. It polls once more
// first, because a finished task hands out a fresh signed link for free
// and a link recorded hours ago has lived its two hours. An analysis task
// names no link: its result is the face shape, and nothing is fetched.
// One lapsed link is recovered with one fresh poll; a link that expires
// again comes back as the typed ErrExpired.
func (r *Runner) result(ctx context.Context, task, taskID string) (taskValue, error) {
	st, err := r.client.Poll(ctx, PollRef{Task: task, TaskID: taskID})
	if err != nil {
		return taskValue{}, err
	}
	if st.Status != StatusSuccess {
		return taskValue{}, fmt.Errorf("youcam: task %s reported %q where a result was due", taskID, st.Status)
	}
	value := taskValue{faceShape: st.FaceShape}
	if st.ResultURL == "" {
		return value, nil
	}
	res, err := r.client.Fetch(ctx, ResultRef{TaskID: taskID, URL: st.ResultURL})
	if errors.Is(err, ErrExpired) {
		fresh, perr := r.client.Poll(ctx, PollRef{Task: task, TaskID: taskID})
		if perr != nil {
			return taskValue{}, perr
		}
		if fresh.Status != StatusSuccess || fresh.ResultURL == "" {
			return taskValue{}, fmt.Errorf("youcam: task %s named no fresh result link", taskID)
		}
		res, err = r.client.Fetch(ctx, ResultRef{TaskID: taskID, URL: fresh.ResultURL})
	}
	if err != nil {
		return taskValue{}, err
	}
	value.bytes = res.Bytes
	return value, nil
}

// keep copies the result into mediastore under an id derived from the
// idempotency key, so a repeat of the same key lands on the same bytes and
// a second keep is already done. The content type is sniffed from the
// bytes: PNG results arrive as binary/octet-stream under a .jpg name, so
// neither the header nor the name is trusted.
func (r *Runner) keep(ctx context.Context, job Job, result []byte) (string, error) {
	blobID := keepID(job.Key)
	err := r.media.PersistWithID(ctx, blobID, bytes.NewReader(result), mediastore.Put{
		ContentType: http.DetectContentType(result),
		Owner:       job.Owner,
		Group:       job.Group,
		Visibility:  mediastore.Private,
	})
	if errors.Is(err, mediastore.ErrAlreadyExists) {
		return blobID, nil // the same key kept these bytes already
	}
	if err != nil {
		return "", err
	}
	return blobID, nil
}

// keepID derives the mediastore id of one key's result: 32 lowercase hex
// characters, the shape the id package produces, from the key's digest.
func keepID(key string) string {
	sum := sha256.Sum256([]byte("youcam:" + key))
	return hex.EncodeToString(sum[:16])
}
