package privacy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/nrynss/dresscode/internal/memory"
	"github.com/nrynss/dresscode/internal/store"
	"github.com/nrynss/keel/erase"
	"github.com/nrynss/keel/mediastore"
	"github.com/nrynss/keel/sqlite"
)

const (
	// TargetFiles is the stored blobs.
	TargetFiles = "files"
	// TargetRows is the account's rows: every user-data table, then the
	// identity rows. The name is the one the inner erasure ledger records.
	TargetRows = "rows"
	// TargetMemory is the account's Lambo session.
	TargetMemory = "memory"
	// TargetLooks is the looks that used one photo.
	TargetLooks = "looks"
	// TargetLook is one look's rows.
	TargetLook = "look"
)

// ErrAbsent is a photo, look, or history that is already gone, or that
// this user does not hold. Callers answer not_found. A second delete of
// an account is the identity package's answer, not this one.
var ErrAbsent = errors.New("privacy: already gone")

// Blobs is the mediastore surface deletion uses. *mediastore.Store
// implements it. A test can stand a fake in.
type Blobs interface {
	Blobs(ctx context.Context) ([]mediastore.Blob, error)
	Delete(ctx context.Context, id string) error
}

// Memory erases one Lambo session. *memory.Client implements it. Erase
// reports an already-absent session on the result, and a second call is
// safe. A test can stand a fake in. Nothing here dials.
type Memory interface {
	Erase(ctx context.Context, session string) (memory.EraseResult, error)
}

// Config configures New. The database must already hold the store schema.
// Account deletion also needs the identity schema, because the rows target
// removes those rows.
type Config struct {
	// DB is the shared SQLite file. It must not be nil. New does not
	// close it.
	DB *sqlite.DB
	// Blobs stores the account's media. It must not be nil.
	Blobs Blobs
	// Memory erases the account's Lambo session. It must not be nil.
	Memory Memory
	// Log receives a line when a delete fails. Nil discards it.
	Log *slog.Logger
}

// Engine builds the erase targets and the photo, look, and history jobs.
// Create it with New. An Engine is safe for concurrent use.
type Engine struct {
	db     *sqlite.DB
	blobs  Blobs
	memory Memory
	log    *slog.Logger
}

// New returns an engine for cfg. It does not migrate and it does not dial.
func New(cfg Config) (*Engine, error) {
	if cfg.DB == nil {
		return nil, errors.New("privacy: nil database")
	}
	if cfg.Blobs == nil {
		return nil, errors.New("privacy: nil blob store")
	}
	if cfg.Memory == nil {
		return nil, errors.New("privacy: nil memory")
	}
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Engine{db: cfg.DB, blobs: cfg.Blobs, memory: cfg.Memory, log: log}, nil
}

// RowTables returns the user-data tables the rows target deletes, in the
// order store.UserTables reports. The caller may change the slice. Identity
// tables are not in this list. The rows target deletes those after the
// user-data tables, in the same order Keel does, and Keel deletes them
// again once every target has confirmed.
func RowTables() []string {
	return store.UserTables()
}

// Source is the erase source and the identity target source. ref is the
// user id. The targets are files, rows, and memory. A restart rebuilds
// the same list from the same ref.
func (e *Engine) Source(ctx context.Context, ref string) ([]erase.Target, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ref == "" {
		return nil, errors.New("privacy: user id is empty")
	}
	if _, err := memory.Session(ref); err != nil {
		return nil, err
	}
	return []erase.Target{
		&filesTarget{eng: e, user: ref},
		&rowsTarget{eng: e, user: ref},
		&memoryTarget{eng: e, user: ref},
	}, nil
}

type filesTarget struct {
	eng  *Engine
	user string
}

func (t *filesTarget) Name() string { return TargetFiles }

// Delete removes every blob this user owns. A second call finds nothing
// and reports the target gone, which an erasure counts as confirmed.
func (t *filesTarget) Delete(ctx context.Context) error {
	owned, err := t.eng.ownedBlobs(ctx, t.user)
	if err != nil {
		return err
	}
	if len(owned) == 0 {
		return fmt.Errorf("privacy: blobs already gone: %w", erase.ErrGone)
	}
	for _, id := range owned {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := t.eng.blobs.Delete(ctx, id); err != nil && !errors.Is(err, mediastore.ErrNotFound) {
			return err
		}
	}
	return nil
}

type rowsTarget struct {
	eng  *Engine
	user string
}

func (t *rowsTarget) Name() string { return TargetRows }

// Tables returns the user-data tables this target deletes. Identity tables
// are not included. The caller may change the slice.
func (t *rowsTarget) Tables() []string { return RowTables() }

// Delete removes the user's rows from every user-data table, then the
// sign-in codes, sessions, identities, and the user row. The order inside
// the identity tables matches Keel, because a code names a session and a
// session names the user. A second call deletes nothing and succeeds.
func (t *rowsTarget) Delete(ctx context.Context) error {
	tx, err := t.eng.db.BeginWrite(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range store.UserTables() {
		query, err := deleteUserQuery(table)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, query, t.user); err != nil {
			return fmt.Errorf("privacy: delete %s: %w", table, err)
		}
	}
	// Codes name a session, sessions and identities name the user, and
	// the user row is last. Keel repeats this after the erasure confirms.
	steps := []struct {
		name  string
		query string
	}{
		{"sign-in codes", `DELETE FROM sign_in_codes WHERE requesting_session IN (SELECT id FROM sessions WHERE user_id = ?)`},
		{"sessions", `DELETE FROM sessions WHERE user_id = ?`},
		{"identities", `DELETE FROM identities WHERE user_id = ?`},
		{"user", `DELETE FROM users WHERE id = ?`},
	}
	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, step.query, t.user); err != nil {
			return fmt.Errorf("privacy: delete %s: %w", step.name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

type memoryTarget struct {
	eng  *Engine
	user string
}

func (t *memoryTarget) Name() string { return TargetMemory }

// Delete erases the account's one Lambo session. Already absent confirms
// the target, so a resumed erasure does not stick on a session it removed
// before the restart.
func (t *memoryTarget) Delete(ctx context.Context) error {
	session, err := memory.Session(t.user)
	if err != nil {
		return err
	}
	result, err := t.eng.memory.Erase(ctx, session)
	if err != nil {
		return err
	}
	if result.AlreadyAbsent {
		return fmt.Errorf("privacy: session already gone: %w", erase.ErrGone)
	}
	return nil
}

func (e *Engine) ownedBlobs(ctx context.Context, user string) ([]string, error) {
	blobs, err := e.blobs.Blobs(ctx)
	if err != nil {
		return nil, err
	}
	var owned []string
	for _, blob := range blobs {
		if blob.Owner == user && blob.ID != "" {
			owned = append(owned, blob.ID)
		}
	}
	return owned, nil
}

func (e *Engine) deleteBlobIDs(ctx context.Context, user string, ids []string) error {
	owners, err := e.blobOwners(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		owner, ok := owners[id]
		if !ok || owner != user {
			// Absent is already gone. A blob this user does not own
			// stays: deletion must not remove someone else's bytes.
			continue
		}
		if err := e.blobs.Delete(ctx, id); err != nil && !errors.Is(err, mediastore.ErrNotFound) {
			return err
		}
	}
	return nil
}

func (e *Engine) blobOwners(ctx context.Context) (map[string]string, error) {
	blobs, err := e.blobs.Blobs(ctx)
	if err != nil {
		return nil, err
	}
	owners := make(map[string]string, len(blobs))
	for _, blob := range blobs {
		owners[blob.ID] = blob.Owner
	}
	return owners, nil
}

func deleteUserQuery(table string) (string, error) {
	if !knownUserTable(table) || !plainIdent(table) || !plainIdent(store.UserIDColumn) {
		return "", fmt.Errorf("privacy: %s is not a user table", table)
	}
	return `DELETE FROM "` + table + `" WHERE "` + store.UserIDColumn + `" = ?`, nil
}

func knownUserTable(name string) bool {
	for _, table := range store.UserTables() {
		if table == name {
			return true
		}
	}
	return false
}

func plainIdent(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && r != '_' {
			return false
		}
	}
	return true
}

func (e *Engine) clearSwitch(ctx context.Context, user string) (int64, error) {
	res, err := e.db.Writer().ExecContext(ctx, `DELETE FROM history_switch WHERE user_id = ?`, user)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (e *Engine) deleteLooks(ctx context.Context, user string, ids, hashes []string) error {
	tx, err := e.db.BeginWrite(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM looks WHERE id = ? AND user_id = ?`, id, user); err != nil {
			return err
		}
	}
	// The cache is the photo, not the look. It goes when no look of this
	// user still uses the hash. A look deletion passes no hashes, so the
	// selfie cache stays with the selfie.
	for _, hash := range hashes {
		var left int
		err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM looks WHERE user_id = ? AND photo_hash = ?`, user, hash).Scan(&left)
		if err != nil {
			return err
		}
		if left != 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM analysis_cache WHERE user_id = ? AND photo_hash = ?`, user, hash); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (e *Engine) deleteHistory(ctx context.Context, user string) error {
	session, err := memory.Session(user)
	if err != nil {
		return err
	}
	result, err := e.memory.Erase(ctx, session)
	if err != nil {
		return err
	}
	n, err := e.clearSwitch(ctx, user)
	if err != nil {
		return err
	}
	if result.AlreadyAbsent && n == 0 {
		return fmt.Errorf("privacy: history already gone: %w", erase.ErrGone)
	}
	return nil
}

// historyOn reports whether this user has turned style history on.
// No row is off, the same as a stored off.
func historyOn(ctx context.Context, db *sqlite.DB, user string) (bool, error) {
	var enabled int
	err := db.Reader().QueryRowContext(ctx,
		`SELECT enabled FROM history_switch WHERE user_id = ?`, user).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return enabled != 0, nil
}
