package privacy

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/nrynss/keel/erase"
	"github.com/nrynss/keel/identity"
	"github.com/nrynss/keel/job"
)

// This file erases Keel's job rows. The table belongs to the job store, not to
// Dresscode: it keeps its own schema on the same SQLite file, and it has no
// user column. What it carries instead is a chain's lineage (id, root_id,
// parent_id), the last progress snapshot a job published, the result bytes the
// work returned, and the error text and detail. A look's job row therefore
// holds that look's outcome - the render's media id, the look in words, the
// shop list - and a deletion's job row holds the ledger of what it erased: the
// user id, the media ids and the look ids. Nothing in the table says whose
// rows they are, so the sweep is built from what the account's own rows name,
// read before those rows go.
//
// A chain that is still working is never destroyed. Keel's own store.Delete
// refuses it while any row of the chain is running or queued, for the reason
// in its doc: erasure never destroys live work. Deleting a running row would
// not stop the work either - the runner would simply find no row to write its
// terminal into - so the sweep skips it, and a repeat erase takes what has
// landed since.

// activeStatuses are the job statuses that mean work is still owed.
var activeStatuses = []string{string(job.StatusRunning), string(job.StatusQueued)}

// jobTextColumns are the job columns carrying a chain's words rather than its
// lineage: the last snapshot it published, the result bytes the work returned,
// and the two error columns. Every sweep here reads them, and so does the
// reference count in references.go, because a look's outcome is in this table
// before the server ever reads the look - the paid path's result is the
// finished look and its render's media id, a check frame names that picture
// too, and a render that failed says which picture it was making. The columns
// beside these (id, kind, status, parent_id, root_id, error_code) are a
// chain's own bookkeeping and hold nothing.
var jobTextColumns = []string{"progress", "result", "error", "error_detail"}

// jobLedgerKinds are the job kinds that are an erasure's own ledger. Such a
// row names the ids it exists to erase - a look deletion's snapshot is a list
// of media ids - and the contract makes the account job's row readable after
// the erase, because the returned job id is the capability for the stream
// (docs/api.md, the deletion section). Counting them as references would
// refuse every deletion, so they are read as records, not as pictures, and the
// account's own go with it in sweep 2 above. The look kind is not here, and
// not imported: internal/pipeline's KindName is "look", and this module stays
// free of the camera stack that comes with it.
var jobLedgerKinds = []string{KindName, erase.KindName, identity.DeletionKindName}

// sweepJobChain removes every row of one finished chain, and reports how many
// went. A chain still holding a running or queued row is left whole.
func sweepJobChain(ctx context.Context, tx *sql.Tx, root string) (int64, error) {
	if root == "" {
		return 0, nil
	}
	active, err := chainActive(ctx, tx, root)
	if err != nil || active {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM jobs WHERE root_id = ?`, root)
	if err != nil {
		return 0, fmt.Errorf("privacy: delete the job chain %s: %w", root, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("privacy: count the job chain %s: %w", root, err)
	}
	return n, nil
}

// chainActive reports whether any row of one chain is still working.
func chainActive(ctx context.Context, tx *sql.Tx, root string) (bool, error) {
	args := append([]any{root}, anyArgs(activeStatuses)...)
	var active int
	err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM jobs WHERE root_id = ? AND status IN (`+marks(len(activeStatuses))+`))`,
		args...).Scan(&active)
	if err != nil {
		return false, fmt.Errorf("privacy: the chain of %s: %w", root, err)
	}
	return active != 0, nil
}

// eraseAccountJobs removes the job rows of one account. It runs inside the
// rows target's transaction, before the rows it reads go.
//
// Three sweeps, because the table names the account in three ways:
//
//  1. every chain rooted at a look the account holds. A look's id is its
//     job's root id, on the paid path and the sample path alike: the
//     sample route lands a born-finished row of the same kind and the
//     same shape, so the two are not separable from the row, and both are
//     the account's. The paid route writes the looks row when the job
//     starts (internal/server/looks.go, the snapshot keeper) and the
//     sample route writes it as soon as the story lands, so a look that
//     was given out to anyone is in this sweep.
//  2. every chain whose snapshot, result or error carries the account's user
//     id. That is the photo, look and history deletion ledgers this account
//     started, which list its media ids and look ids, and the identity and
//     erase rows of its own account deletion. The two rows carrying this
//     erasure are running while it runs, so they are skipped here; what each
//     holds once it lands is the minted user id and the names of the targets
//     - a number that points at nothing else, no address, no look, no
//     picture, no photo hash.
//  3. every chain that names a picture this account owns and is rooted at no
//     look at all. This is the row a look leaves when its looks row never
//     landed: the sample route lands the story first and keeps the look
//     after it, so a failed insert leaves a finished chain that names
//     nobody and nothing but its own minted ids and the pictures it kept
//     under this owner. Sweep 1 cannot see it (no looks row) and sweep 2
//     cannot (no user id in the text), so it would outlive the erase.
//
// What survives all three, and it is the honest remainder rather than a claim
// that nothing does: a chain of sweep 3 whose pictures the files target
// already erased before this transaction read the blob list. The targets are
// concurrent, so the order is not the package's to fix. Such a row is a
// finished look no route ever gave out - every read answers not_found without
// its looks row - and it carries a story's words and 32-hex ids of bytes that
// are gone. It names no user id, no address, no photo hash and no live
// picture. Notes record it.
func (e *Engine) eraseAccountJobs(ctx context.Context, tx *sql.Tx, user string) (int64, error) {
	var jobs int
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'jobs')`).Scan(&jobs); err != nil {
		return 0, fmt.Errorf("privacy: the job table: %w", err)
	}
	if jobs == 0 {
		// A file with no job store holds no job rows. That is the
		// prove-it-was-never-created answer, and it is the shape a
		// store-only database has.
		return 0, nil
	}
	roots, err := accountJobRoots(ctx, tx, user)
	if err != nil {
		return 0, err
	}
	// Sweep 3 reads the blob store, not this transaction, and a failure
	// there is a store fault rather than an absent picture: the erase
	// reports it instead of silently keeping the rows the list would have
	// found.
	owned, err := e.ownedBlobs(ctx, user)
	if err != nil {
		return 0, err
	}
	unrecorded, err := unownedChainRoots(ctx, tx, owned)
	if err != nil {
		return 0, err
	}
	roots = append(roots, unrecorded...)
	var removed int64
	for _, root := range roots {
		n, err := sweepJobChain(ctx, tx, root)
		if err != nil {
			return removed, err
		}
		removed += n
	}
	return removed, nil
}

// accountJobRoots lists the chains that name one account, the first two ways
// the header describes.
func accountJobRoots(ctx context.Context, tx *sql.Tx, user string) ([]string, error) {
	named, err := jobTextPredicate()
	if err != nil {
		return nil, err
	}
	args := []any{user}
	for range jobTextColumns {
		args = append(args, user)
	}
	query := `SELECT DISTINCT root_id FROM jobs WHERE root_id IN (SELECT id FROM looks WHERE user_id = ?)
		UNION
		SELECT DISTINCT root_id FROM jobs WHERE ` + named
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("privacy: the account's job chains: %w", err)
	}
	return scanRoots(rows)
}

// unownedChainRoots lists the chains of sweep 3: a chain naming one of these
// media ids that is rooted at no look at all, so no account holds the look it
// describes. A chain still rooted at somebody's look is left to its own
// account's erase, because a route that handed one picture to two accounts is
// a route bug, and this erase is not the place to delete another person's
// record of their own look.
//
// The ledgers are excluded by kind: a deletion's row names the pictures it
// erased, and its chain belongs to whoever started it, which the sweep above
// already reached by the user id in its snapshot.
//
// A media id here is one this account owns, listed before this transaction's
// rows go. With no media store open, the list is empty and this sweep reads
// nothing.
func unownedChainRoots(ctx context.Context, tx *sql.Tx, mediaIDs []string) ([]string, error) {
	if len(mediaIDs) == 0 {
		return nil, nil
	}
	named, err := jobTextPredicate()
	if err != nil {
		return nil, err
	}
	var roots []string
	for _, mediaID := range mediaIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		args := anyArgs(jobLedgerKinds)
		for range jobTextColumns {
			args = append(args, mediaID)
		}
		rows, err := tx.QueryContext(ctx,
			`SELECT DISTINCT root_id FROM jobs WHERE kind NOT IN (`+marks(len(jobLedgerKinds))+
				`) AND root_id NOT IN (SELECT id FROM looks) AND (`+named+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("privacy: the chains naming %s: %w", mediaID, err)
		}
		found, err := scanRoots(rows)
		if err != nil {
			return nil, err
		}
		roots = append(roots, found...)
	}
	return roots, nil
}

// jobTextPredicate is the OR that says "this chain's text carries the value in
// the placeholder", once per text column. Each column name goes through
// plainIdent, because the read is built by concatenation.
func jobTextPredicate() (string, error) {
	parts := make([]string, 0, len(jobTextColumns))
	for _, column := range jobTextColumns {
		if !plainIdent(column) {
			return "", fmt.Errorf("privacy: %s is not a plain identifier", column)
		}
		parts = append(parts, `instr("`+column+`", ?) > 0`)
	}
	return strings.Join(parts, " OR "), nil
}

// scanRoots collects the root ids one query returned.
func scanRoots(rows *sql.Rows) ([]string, error) {
	defer rows.Close()
	var roots []string
	for rows.Next() {
		var root string
		if err := rows.Scan(&root); err != nil {
			return nil, fmt.Errorf("privacy: the job chains: %w", err)
		}
		roots = append(roots, root)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("privacy: the job chains: %w", err)
	}
	return roots, nil
}

func marks(n int) string { return strings.TrimSuffix(strings.Repeat("?, ", n), ", ") }

func anyArgs(in []string) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}
