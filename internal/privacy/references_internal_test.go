package privacy

import (
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nrynss/dresscode/internal/store"
	identitysql "github.com/nrynss/keel/identity/sqlitestore"
	jobsql "github.com/nrynss/keel/job/sqlitestore"
	mediasql "github.com/nrynss/keel/mediastore/sqlitestore"
	"github.com/nrynss/keel/sqlite"
)

// This is the guard references.go promises: the reference count is only as
// correct as its list of referrers, so the list may not fall behind the schema.
// It runs against the production file - Dresscode's migrations and Keel's job,
// media and identity stores all opened - and asks every TEXT and BLOB column it
// holds one question: is this column read for a media id, or is it exempt for a
// stated reason? A new column answers neither, so a migration that starts
// carrying a picture reference cannot slip past the count unnoticed.
//
// The reasons are the load-bearing half. A column exempted as "an id of
// something else" that later starts holding a media id is the bug this test
// exists to catch, so an exemption is worth the sentence only while the
// sentence is true.

// namerColumns is every column the count actually reads: the declared columns
// of each namer, and, for Keel's job table, the four text columns the job namer
// reaches through jobTextColumns.
func namerColumns() map[string]bool {
	out := map[string]bool{}
	for _, namer := range blobNamers {
		if namer.isJob {
			for _, column := range jobTextColumns {
				out[namer.table+"."+column] = true
			}
			continue
		}
		for _, column := range namer.columns {
			out[namer.table+"."+column.name] = true
		}
	}
	return out
}

// exemptTables lists, per table, the TEXT and BLOB columns that cannot name a
// stored picture, with the reason each holds nothing. A table missing from here
// fails the test with its whole column list, which is what a new table has to
// answer.
var exemptTables = map[string]struct {
	reason  string
	columns []string
}{
	"analysis_cache": {"the analysis of one photo: its owner's id and the photo's digest, " +
		"and the words the model said back. A media id is nowhere in it.",
		[]string{"user_id", "photo_hash", "task", "face_shape", "hair_type", "hair_length",
			"colour_tones"}},
	"history_switch": {"a switch row: the owner and a flag.", []string{"user_id"}},
	"identities": {"Keel's identity store. provider and subject are the third party's " +
		"address of a person, not one of our pictures.",
		[]string{"id", "user_id", "provider", "subject"}},
	"identity_schema_migrations":   {"Keel's own migration ledger.", []string{"filename"}},
	"job_schema_migrations":        {"Keel's own migration ledger.", []string{"filename"}},
	"mediastore_schema_migrations": {"Keel's own migration ledger.", []string{"filename"}},
	"store_schema_migrations":      {"Dresscode's own migration ledger.", []string{"filename"}},
	"look_flags": {"a flag row names the look, its owner and the criterion it struck.",
		[]string{"look_id", "user_id", "criterion_id"}},
	"look_products": {"the shop join. Its look and owner, and the copy of the product's " +
		"words that keeps a deleted product readable.",
		[]string{"look_id", "user_id", "category", "name", "fidelity", "origin", "youcam_role"}},
	"looks": {"the look's own id, its owner, the photo's digest and the judgement in words. " +
		"selfie_id is read for a picture, so it is not here.",
		[]string{"id", "user_id", "photo_hash", "occasion", "fix", "runner_up", "verdict"}},
	"look_snapshots": {"the root a restarted look resumes under, and its owner. The snapshot " +
		"itself is read for a picture, so it is not here.", []string{"root_id", "user_id"}},
	"media": {"the blob store's own index. id IS the picture rather than a name for " +
		"another one, owner is a person, media_group is the group a picture was filed " +
		"under, and the rest is what the bytes are.",
		[]string{"id", "owner", "media_group", "content_type", "visibility"}},
	"opt_ins": {"a tapped-through: the person, the occasion and the fix.",
		[]string{"user_id", "occasion", "fix_id"}},
	"products": {"a product row's own words. Its id and photograph are read for a picture, " +
		"so they are not here.",
		[]string{"user_id", "category", "name", "fidelity", "origin", "youcam_role"}},
	"render_check_attributes": {"the vocabulary of one check: which render, whose, which " +
		"attribute and what it said.",
		[]string{"render_id", "user_id", "attribute_id", "value"}},
	"render_checks": {"three booleans about one render, and the ids that point at it.",
		[]string{"render_id", "look_id", "user_id"}},
	"renders": {"the render's own id, the look and owner it belongs to and the fix it " +
		"showed. media_id is read for a picture, so it is not here.",
		[]string{"id", "look_id", "user_id", "fix_id"}},
	"sessions": {"Keel's session store: a session id and the person it belongs to.", []string{"id", "user_id"}},
	"sign_in_codes": {"an address hash, a code hash and the session that asked. None of " +
		"them is a media id.",
		[]string{"id", "address_hash", "code_hash", "requesting_session"}},
	"users": {"the person's own id and whether it is a guest.", []string{"id", "kind"}},
	"jobs": {"a chain's lineage and bookkeeping. The four columns carrying its words " +
		"are read for a picture by jobTextColumns.",
		[]string{"id", "kind", "status", "parent_id", "root_id", "error_code"}},
}

func TestEveryTextColumnIsClassified(t *testing.T) {
	t.Parallel()
	db := openClassificationDB(t)
	ctx := t.Context()

	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, err := jobsql.Open(ctx, jobsql.Config{DB: db}); err != nil {
		t.Fatalf("jobs: %v", err)
	}
	if _, err := mediasql.Open(ctx, mediasql.Config{DB: db}); err != nil {
		t.Fatalf("media: %v", err)
	}
	if _, err := identitysql.Open(ctx, identitysql.Config{DB: db}); err != nil {
		t.Fatalf("identity: %v", err)
	}

	read := namerColumns()
	seen := map[string]bool{}
	for _, table := range classificationTables(t, db) {
		cols := classificationTextColumns(t, db, table)
		if len(cols) == 0 {
			continue
		}
		exempt, listed := exemptTables[table]
		if !listed {
			t.Errorf("%s holds %v and no entry in exemptTables: say which of these can name "+
				"a stored picture and read it in blobNamers, and why the rest cannot", table, cols)
			for _, column := range cols {
				seen[table+"."+column] = true
			}
			continue
		}
		if exempt.reason == "" {
			t.Errorf("%s is exempted with no reason", table)
		}
		have := map[string]bool{}
		for _, column := range exempt.columns {
			have[column] = true
		}
		for _, column := range cols {
			name := table + "." + column
			seen[name] = true
			switch {
			case read[name]:
				if have[column] {
					t.Errorf("%s is both read for a picture and exempted: pick one", name)
				}
			case have[column]:
			default:
				t.Errorf("%s is read by nothing: name it in blobNamers or exempt it with a "+
					"reason (%s)", name, exempt.reason)
			}
		}
	}

	// The other direction: an entry or a namer column that the schema no longer
	// has is a claim about a table that is not there, and it would hide the next
	// rename.
	for name := range read {
		if !seen[name] {
			t.Errorf("blobNamers reads %s, which this schema does not have", name)
		}
	}
	for table, entry := range exemptTables {
		for _, column := range entry.columns {
			if !seen[table+"."+column] {
				t.Errorf("exemptTables lists %s.%s, which this schema does not have", table, column)
			}
		}
	}
}

func openClassificationDB(t *testing.T) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(t.Context(), sqlite.Config{
		Path:   filepath.Join(t.TempDir(), "classification.db"),
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func classificationTables(t *testing.T, db *sqlite.DB) []string {
	t.Helper()
	rows, err := db.Reader().QueryContext(t.Context(),
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("the table list: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("the table list: %v", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("the table list: %v", err)
	}
	sort.Strings(out)
	return out
}

// classificationTextColumns answers one table's TEXT and BLOB columns: the only
// ones that can carry an id or a sentence. An untyped column counts, because
// SQLite lets a migration leave the declaration off.
func classificationTextColumns(t *testing.T, db *sqlite.DB, table string) []string {
	t.Helper()
	rows, err := db.Reader().QueryContext(t.Context(),
		`SELECT name FROM pragma_table_info(?) WHERE upper(type) IN ('TEXT', 'BLOB', '') ORDER BY cid`, table)
	if err != nil {
		t.Fatalf("the columns of %s: %v", table, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("the columns of %s: %v", table, err)
		}
		if strings.TrimSpace(name) == "" {
			t.Fatalf("%s has a nameless column", table)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("the columns of %s: %v", table, err)
	}
	return out
}
