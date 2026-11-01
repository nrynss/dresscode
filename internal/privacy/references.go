package privacy

import (
	"context"
	"fmt"
	"strings"
)

// This file is the store rule behind every scoped deletion: a stored picture
// is erased only when no row this erasure leaves behind still names it.
//
// The rule is a reference count, and a reference count is only as correct as
// its list of referrers, so the list below is derived from the schema rather
// than from the two routes that write it. Every TEXT and BLOB column of every
// table that lives in this SQLite file - Dresscode's own migrations, Keel's
// job store, Keel's mediastore index and Keel's identity store - was read out
// of sqlite_master with pragma_table_info, and each column was then classified
// against the writes the routes actually make. references_internal_test.go's
// TestEveryTextColumnIsClassified re-derives that list from the schema and
// fails when the file holds a text column this enumeration has not classified,
// so a new table or a new column cannot silently drop out of the count.
//
// A media id is an opaque minted string, so the count matches it two ways: a
// column that IS a media id is matched exactly, and a column carrying text or
// JSON that may name one is matched with instr, a substring test. The
// substring test over-names rather than under-names: an id found anywhere in a
// document counts as named, which is the safe direction for a deletion.
//
// The jobs table is in the list because a look's outcome is stored there
// before the server ever reads the look: a paid job's result is the finished
// look with its render's media id, and the sample route lands a born-finished
// row of the same kind. A look nobody has read yet therefore names its own
// picture from jobs alone, with no renders row. A count that read only renders
// and looks would erase that picture from under the row that still names it.

// blobColumn is one place within a table that can hold a media id.
type blobColumn struct {
	// name is the column. It must be a plain lowercase identifier: the
	// reads below are built by concatenation, and plainIdent guards them.
	name string
	// exact marks a column whose value IS a media id, as against a column
	// holding text or JSON that may name one.
	exact bool
}

// blobNamer is one table that can name a media id, and the look column its
// rows hang from.
type blobNamer struct {
	// table is the table. It must be a plain lowercase identifier.
	table string
	// columns are the places inside it.
	columns []blobColumn
	// lookColumn names the column that points a row at the look it belongs
	// to, empty for a row that belongs to no look. A deletion scopes its
	// own rows out of the count by it, because a row this erasure removes
	// is not a surviving reference.
	lookColumn string
	// isJob marks Keel's job table: no look column, its lineage hangs on
	// root_id, and the ledgers it holds are records of deletions rather
	// than holders of a picture. See jobLedgerKinds.
	isJob bool
}

// blobNamers is the complete list of places that can name a media id.
//
// Not in it, and why:
//
//   - media.id is the blob's own index row, not a reference to it. The
//     caller already holds that answer from the blob store's owner list,
//     and an id that is not there is gone.
//   - media.media_group is the group a picture was stored under: "selfie",
//     "product", or "render/<user>/<photo hash>". It names a person's id
//     and their photo's digest, never a media id.
//   - analysis_cache.photo_hash is that digest, not an id.
//   - looks.photo_hash likewise. looks.occasion, fix, runner_up and verdict,
//     renders.fix_id, look_flags.criterion_id, opt_ins.fix_id and occasion,
//     products.category, name, fidelity, origin and youcam_role,
//     look_products the same, render_check_attributes.attribute_id and
//     value, jobs.id, kind, status, parent_id, root_id, error_code, and
//     every identity and migration column are ids and labels of other
//     things.
var blobNamers = []blobNamer{
	// The kept selfie: looks.selfie_id, and the snapshot a restarted look
	// rebuilds its request from, whose JSON names that same id in its
	// "selfie" member.
	{table: "looks", columns: []blobColumn{{"selfie_id", true}}, lookColumn: "id"},
	{table: "look_snapshots", columns: []blobColumn{{"snapshot", false}}, lookColumn: "root_id"},

	// The render a look showed.
	{table: "renders", columns: []blobColumn{{"media_id", true}}, lookColumn: "look_id"},

	// A user's own product picture: intake stores the normalised file and
	// makes its media id the product's row id, and the shop join answers
	// that id back as the entry's media_id. The photograph columns are the
	// declared home of the same value; every route writes '' to them today.
	{table: "products", columns: []blobColumn{{"id", true}, {"photograph", true}}},
	{table: "look_products", columns: []blobColumn{{"product_id", true}, {"photograph", true}}, lookColumn: "look_id"},

	// The job rows: the outcome a look left behind, the stage snapshot it
	// published, and the two error columns - a render that failed names the
	// picture it was making in its own message.
	{table: "jobs", isJob: true},
}

// jobTextColumns and jobLedgerKinds, the two readings of Keel's job table this
// count shares with the account sweep, are in jobrows.go.

// namedElsewhere reports whether a row outside this erasure's own scope still
// names mediaID. scope is the list of look ids the erasure removes: a renders
// row of a look that goes with this deletion is not a surviving reference, and
// the photo deletion in particular lists the selfie while its looks are still
// there, so the scope is what keeps that deletion erasing anything at all.
//
// A row of another account counts. The rule is about the store, not about the
// caller: a route that handed one picture to two accounts is a route bug, and
// the count refuses to turn it into bytes deleted from under a live look.
func (e *Engine) namedElsewhere(ctx context.Context, mediaID string, scope []string) (bool, error) {
	if mediaID == "" {
		return false, nil
	}
	present, err := e.schemaTables(ctx)
	if err != nil {
		return false, err
	}
	for _, namer := range blobNamers {
		if !present[namer.table] {
			// A file with no job store holds no job rows, and a
			// table this release never made names nothing.
			continue
		}
		query, args, err := namerQuery(namer, mediaID, scope)
		if err != nil {
			return false, err
		}
		var named int
		if err := e.db.Reader().QueryRowContext(ctx, query, args...).Scan(&named); err != nil {
			return false, fmt.Errorf("privacy: the reference read of %s: %w", namer.table, err)
		}
		if named != 0 {
			return true, nil
		}
	}
	return false, nil
}

// namerQuery builds the one EXISTS a namer answers, with its arguments in the
// order the placeholders take them: the media id, then the kinds a job row is
// not, then the look ids this erasure removes with the picture. A scope of
// none adds no clause at all.
func namerQuery(namer blobNamer, mediaID string, scope []string) (string, []any, error) {
	for _, name := range []string{namer.table, namer.lookColumn} {
		if name != "" && !plainIdent(name) {
			return "", nil, fmt.Errorf("privacy: %s is not a plain identifier", name)
		}
	}
	query := `SELECT EXISTS (SELECT 1 FROM "` + namer.table + `" WHERE `
	args := make([]any, 0, 1+len(jobLedgerKinds)+len(scope))
	if namer.isJob {
		parts := make([]string, 0, len(jobTextColumns))
		for _, column := range jobTextColumns {
			if !plainIdent(column) {
				return "", nil, fmt.Errorf("privacy: %s is not a plain identifier", column)
			}
			parts = append(parts, `instr("`+column+`", ?) > 0`)
			args = append(args, mediaID)
		}
		query += "(" + strings.Join(parts, " OR ") + `) AND kind NOT IN (` + marks(len(jobLedgerKinds)) + `)`
		for _, kind := range jobLedgerKinds {
			args = append(args, kind)
		}
		if len(scope) > 0 {
			query += ` AND root_id NOT IN (` + marks(len(scope)) + `)`
			args = append(args, anyArgs(scope)...)
		}
	} else {
		parts := make([]string, 0, len(namer.columns))
		for _, column := range namer.columns {
			if !plainIdent(column.name) {
				return "", nil, fmt.Errorf("privacy: %s is not a plain identifier", column.name)
			}
			if column.exact {
				parts = append(parts, `"`+column.name+`" = ?`)
			} else {
				parts = append(parts, `instr("`+column.name+`", ?) > 0`)
			}
			args = append(args, mediaID)
		}
		query += strings.Join(parts, " OR ")
		if key := namer.lookColumn; key != "" && len(scope) > 0 {
			if !plainIdent(key) {
				return "", nil, fmt.Errorf("privacy: %s is not a plain identifier", key)
			}
			query += ` AND "` + key + `" NOT IN (` + marks(len(scope)) + `)`
			args = append(args, anyArgs(scope)...)
		}
	}
	return query + `)`, args, nil
}

// schemaTables returns the tables this file holds, so a namer whose table was
// never created is skipped rather than failing the deletion.
func (e *Engine) schemaTables(ctx context.Context) (map[string]bool, error) {
	rows, err := e.db.Reader().QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		return nil, fmt.Errorf("privacy: the table list: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("privacy: the table list: %w", err)
		}
		out[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("privacy: the table list: %w", err)
	}
	return out, nil
}
