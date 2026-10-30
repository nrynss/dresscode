package privacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nrynss/keel/erase"
	"github.com/nrynss/keel/job"
)

const (
	// KindName is the job kind photo, look, and history deletion run under.
	// Register it before the runner opens, or a restart cannot resume one.
	KindName = "deletion"

	opPhoto   = "photo"
	opLook    = "look"
	opHistory = "history"

	ledgerVersion = 1
	targetTries   = 3
	jobAttempts   = 5
)

// ledger is the snapshot one deletion progress report carries. A restart
// reads it back and repeats only the targets that have not confirmed.
// The record keeps the scope beside targets and confirmed, because the
// resume hook has nowhere else to put the blob and look ids. The account
// job does not use this shape. That job is Keel's, and its detail is the
// deletion snapshot.
type ledger struct {
	Version   int      `json:"version"`
	User      string   `json:"user"`
	Op        string   `json:"op"`
	Resource  string   `json:"resource,omitempty"`
	Targets   []string `json:"targets"`
	Confirmed []string `json:"confirmed"`
	Blobs     []string `json:"blobs,omitempty"`
	Looks     []string `json:"looks,omitempty"`
	Hashes    []string `json:"hashes,omitempty"`
}

// Kind returns the job kind for photo, look, and history deletion. The
// kind is idempotent, because each delete is safe to repeat.
func (e *Engine) Kind() job.Kind {
	return job.Kind{
		Idempotent:  true,
		MaxAttempts: jobAttempts,
		Resume:      e.resume,
	}
}

// StartPhoto deletes the blob and the looks that cannot be shown without
// it. The selfie is that blob. Render blobs of those looks go with them.
// A missing photo, and a photo this user does not hold, are ErrAbsent.
func (e *Engine) StartPhoto(ctx context.Context, runner *job.Runner, user, photo string) (string, error) {
	if photo == "" || user == "" {
		return "", ErrAbsent
	}
	blobs, looks, hashes, ok, err := e.inspectPhoto(ctx, user, photo)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrAbsent
	}
	return e.start(ctx, runner, ledger{
		Version:  ledgerVersion,
		User:     user,
		Op:       opPhoto,
		Resource: photo,
		Targets:  []string{TargetFiles, TargetLooks},
		Blobs:    blobs,
		Looks:    looks,
		Hashes:   hashes,
	})
}

// StartLook deletes that look and its render blobs. The selfie stays
// until a photo deletion. A missing look, and a look this user does not
// hold, are ErrAbsent.
func (e *Engine) StartLook(ctx context.Context, runner *job.Runner, user, lookID string) (string, error) {
	if lookID == "" || user == "" {
		return "", ErrAbsent
	}
	blobs, ok, err := e.inspectLook(ctx, user, lookID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrAbsent
	}
	return e.start(ctx, runner, ledger{
		Version:  ledgerVersion,
		User:     user,
		Op:       opLook,
		Resource: lookID,
		Targets:  []string{TargetLook, TargetFiles},
		Blobs:    blobs,
		Looks:    []string{lookID},
	})
}

// StartHistory clears the account's style memory and turns the switch
// off. Looks stay. History that is already off is ErrAbsent.
func (e *Engine) StartHistory(ctx context.Context, runner *job.Runner, user string) (string, error) {
	if user == "" {
		return "", ErrAbsent
	}
	on, err := historyOn(ctx, e.db, user)
	if err != nil {
		return "", err
	}
	if !on {
		return "", ErrAbsent
	}
	return e.start(ctx, runner, ledger{
		Version: ledgerVersion,
		User:    user,
		Op:      opHistory,
		Targets: []string{TargetMemory},
	})
}

func (e *Engine) start(ctx context.Context, runner *job.Runner, led ledger) (string, error) {
	if runner == nil {
		return "", errors.New("privacy: nil runner")
	}
	led.Confirmed = []string{}
	id, err := runner.StartKind(ctx, KindName, func(ctx context.Context, progress func(job.Progress)) ([]byte, error) {
		return e.run(ctx, progress, led)
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

func (e *Engine) resume(rec job.Record) (job.Func, error) {
	led, err := decodeLedger(rec.Progress.Detail)
	if err != nil {
		return nil, fmt.Errorf("privacy: resume %s: %w", rec.ID, err)
	}
	return func(ctx context.Context, progress func(job.Progress)) ([]byte, error) {
		return e.run(ctx, progress, led)
	}, nil
}

func (e *Engine) run(ctx context.Context, progress func(job.Progress), led ledger) ([]byte, error) {
	done := make(map[string]bool, len(led.Targets))
	for _, name := range led.Confirmed {
		done[name] = true
	}
	// The ledger lands before any delete. A restart during the work
	// then has the scope, instead of a record that names nothing.
	e.publish(progress, led, done)
	for _, name := range led.Targets {
		if done[name] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := e.tryTarget(ctx, led, name); err != nil {
			return nil, err
		}
		done[name] = true
		e.publish(progress, led, done)
	}
	return nil, nil
}

func (e *Engine) tryTarget(ctx context.Context, led ledger, name string) error {
	var err error
	for attempt := 1; attempt <= targetTries; attempt++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		err = e.deleteNamed(ctx, led, name)
		if err == nil || errors.Is(err, erase.ErrGone) {
			return nil
		}
		e.log.Warn("privacy: delete failed", "target", name, "op", led.Op, "attempt", attempt, "error", err)
	}
	return err
}

func (e *Engine) deleteNamed(ctx context.Context, led ledger, name string) error {
	switch led.Op {
	case opPhoto, opLook:
		switch name {
		case TargetFiles:
			return e.deleteBlobIDs(ctx, led.User, led.Blobs)
		case TargetLooks, TargetLook:
			return e.deleteLooks(ctx, led.User, led.Looks, led.Hashes)
		default:
			return fmt.Errorf("privacy: unknown target %s", name)
		}
	case opHistory:
		if name != TargetMemory {
			return fmt.Errorf("privacy: unknown target %s", name)
		}
		return e.deleteHistory(ctx, led.User)
	default:
		return fmt.Errorf("privacy: unknown deletion %s", led.Op)
	}
}

func (e *Engine) publish(progress func(job.Progress), led ledger, done map[string]bool) {
	led.Confirmed = confirmedInOrder(led.Targets, done)
	detail, err := json.Marshal(led)
	if err != nil {
		return
	}
	current := int64(len(led.Confirmed))
	total := int64(len(led.Targets))
	progress(job.Progress{Stage: "erase", Current: &current, Total: &total, Detail: detail})
}

func confirmedInOrder(targets []string, done map[string]bool) []string {
	out := make([]string, 0, len(targets))
	for _, name := range targets {
		if done[name] {
			out = append(out, name)
		}
	}
	return out
}

func decodeLedger(detail json.RawMessage) (ledger, error) {
	if len(detail) == 0 {
		return ledger{}, errors.New("privacy: no snapshot")
	}
	var led ledger
	if err := json.Unmarshal(detail, &led); err != nil {
		return ledger{}, err
	}
	if led.Version != ledgerVersion || led.User == "" || led.Op == "" || len(led.Targets) == 0 {
		return ledger{}, errors.New("privacy: unknown snapshot")
	}
	if led.Confirmed == nil {
		led.Confirmed = []string{}
	}
	return led, nil
}

func (e *Engine) inspectPhoto(ctx context.Context, user, photo string) (blobs, looks, hashes []string, ok bool, err error) {
	rows, err := e.db.Reader().QueryContext(ctx,
		`SELECT id, photo_hash FROM looks WHERE user_id = ? AND selfie_id = ? ORDER BY id`, user, photo)
	if err != nil {
		return nil, nil, nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash); err != nil {
			return nil, nil, nil, false, err
		}
		looks = append(looks, id)
		hashes = append(hashes, hash)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, false, err
	}
	media, err := e.mediaForLooks(ctx, user, looks)
	if err != nil {
		return nil, nil, nil, false, err
	}
	owners, err := e.blobOwners(ctx)
	if err != nil {
		return nil, nil, nil, false, err
	}
	ownedPhoto := owners[photo] == user
	if len(looks) == 0 && !ownedPhoto {
		return nil, nil, nil, false, nil
	}
	if ownedPhoto {
		blobs = append(blobs, photo)
	}
	blobs = append(blobs, media...)
	return unique(blobs), unique(looks), unique(hashes), true, nil
}

func (e *Engine) inspectLook(ctx context.Context, user, lookID string) ([]string, bool, error) {
	var n int
	err := e.db.Reader().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM looks WHERE id = ? AND user_id = ?`, lookID, user).Scan(&n)
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil
	}
	media, err := e.mediaForLooks(ctx, user, []string{lookID})
	if err != nil {
		return nil, false, err
	}
	return unique(media), true, nil
}

func (e *Engine) mediaForLooks(ctx context.Context, user string, looks []string) ([]string, error) {
	var media []string
	for _, lookID := range looks {
		rows, err := e.db.Reader().QueryContext(ctx,
			`SELECT media_id FROM renders WHERE user_id = ? AND look_id = ? ORDER BY position`, user, lookID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			media = append(media, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return media, nil
}

func unique(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
