package store_test

import (
	"log/slog"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/store"
	sqlitestore "github.com/nrynss/keel/identity/sqlitestore"
	"github.com/nrynss/keel/sqlite"
)

// migrationFiles is every schema file, in the order Migrate applies them.
var migrationFiles = []string{
	"0001_looks.sql",
	"0002_renders.sql",
	"0003_analysis_cache.sql",
	"0004_products.sql",
	"0005_opt_in.sql",
	"0006_history.sql",
	"0007_look_snapshots.sql",
}

// userTables is every table that holds a user's rows. The test lists them
// here, and checks the database and store.UserTables against this list.
var userTables = []string{
	"analysis_cache",
	"history_switch",
	"look_flags",
	"look_products",
	"look_snapshots",
	"looks",
	"opt_ins",
	"products",
	"render_check_attributes",
	"render_checks",
	"renders",
}

func TestMigrateFreshFileThenAgain(t *testing.T) {
	t.Parallel()
	if err := store.Migrate(t.Context(), nil); err == nil {
		t.Fatal("Migrate(nil) succeeded")
	}

	path := filepath.Join(t.TempDir(), "dresscode.db")
	db := openDB(t, path)
	if err := store.Migrate(t.Context(), db); err != nil {
		t.Fatalf("fresh migrate: %v", err)
	}
	schema := schemaSnapshot(t, db)
	ledgerRows := ledger(t, db)
	if !slices.Equal(ledgerRows, migrationFiles) {
		t.Fatalf("ledger %v, want %v", ledgerRows, migrationFiles)
	}
	seedUser(t, db, "user-a", 1)

	if err := store.Migrate(t.Context(), db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if again := schemaSnapshot(t, db); !slices.Equal(schema, again) {
		t.Fatalf("second migrate changed the schema\n first %v\n second %v", schema, again)
	}
	if again := ledger(t, db); !slices.Equal(ledgerRows, again) {
		t.Fatalf("second migrate changed the ledger: %v", again)
	}
	if got := countUser(t, db, store.TableLooks, "user-a"); got != 1 {
		t.Fatalf("second migrate left %d looks, want 1", got)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened := openDB(t, path)
	if err := store.Migrate(t.Context(), reopened); err != nil {
		t.Fatalf("migrate after reopen: %v", err)
	}
	if again := schemaSnapshot(t, reopened); !slices.Equal(schema, again) {
		t.Fatalf("reopen migrate changed the schema\n first %v\n second %v", schema, again)
	}
	if again := ledger(t, reopened); !slices.Equal(ledgerRows, again) {
		t.Fatalf("reopen migrate changed the ledger: %v", again)
	}
	if got := countUser(t, reopened, store.TableLooks, "user-a"); got != 1 {
		t.Fatalf("reopen left %d looks, want 1", got)
	}
}

func TestUserTablesListsEveryUserTable(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "dresscode.db"))
	if err := store.Migrate(t.Context(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got := store.UserTables()
	if !slices.Equal(got, userTables) {
		t.Fatalf("UserTables() = %v, want %v", got, userTables)
	}
	got[0] = "mutated"
	if store.UserTables()[0] == "mutated" {
		t.Fatal("UserTables returned its own slice")
	}

	inDB := appTables(t, db)
	if !slices.Equal(inDB, userTables) {
		t.Fatalf("user tables in the database %v, want %v", inDB, userTables)
	}
	for _, name := range []string{"users", "sessions", "identities", "sign_in_codes"} {
		if slices.Contains(inDB, name) {
			t.Fatalf("store migration created identity table %s", name)
		}
	}

	for _, table := range store.UserTables() {
		if !columnNotNull(t, db, table, store.UserIDColumn) {
			t.Fatalf("%s.%s is missing or nullable", table, store.UserIDColumn)
		}
		_, err := db.Writer().ExecContext(t.Context(), `INSERT INTO "`+table+`" DEFAULT VALUES`)
		if err == nil {
			t.Fatalf("%s accepted a row with no user id", table)
		}
	}
}

func TestAnalysisCacheIsKeyedByThePhotoHash(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "dresscode.db"))
	if err := store.Migrate(t.Context(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	pk := pkColumns(t, db, store.TableAnalysisCache)
	wantPK := []string{store.UserIDColumn, "photo_hash", "task"}
	if !slices.Equal(pk, wantPK) {
		t.Fatalf("analysis_cache primary key %v, want %v", pk, wantPK)
	}

	cols := columnNames(t, db, store.TableAnalysisCache)
	key := map[string]bool{store.UserIDColumn: true, "photo_hash": true, "task": true}
	var values []string
	for _, col := range cols {
		if !key[col] {
			values = append(values, col)
		}
	}
	slices.Sort(values)
	allow := allowlistColumns(t)
	if !slices.Equal(values, allow) {
		t.Fatalf("analysis columns %v, want the allowlist %v", values, allow)
	}
	for _, forbidden := range []string{"age", "gender", "ratio", "ratios", "score", "scores", "attractiveness", "probability"} {
		if slices.Contains(cols, forbidden) {
			t.Fatalf("analysis_cache stores %s", forbidden)
		}
	}
}

func TestSchemaHoldsALook(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "dresscode.db"))
	if err := store.Migrate(t.Context(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	seedUser(t, db, "user-a", 1)
	seedUser(t, db, "user-b", 0)

	var (
		verdict  string
		face     string
		enabled  int
		fidelity string
	)
	err := db.Writer().QueryRowContext(t.Context(),
		`SELECT verdict FROM looks WHERE id = ?`, "user-a-look").Scan(&verdict)
	if err != nil || verdict != string(domain.VerdictKept) {
		t.Fatalf("look verdict %q, %v", verdict, err)
	}
	err = db.Writer().QueryRowContext(t.Context(),
		`SELECT face_shape FROM analysis_cache WHERE user_id = ? AND photo_hash = ? AND task = ?`,
		"user-a", "user-a-hash", "face-attr-analysis").Scan(&face)
	if err != nil || face != "oval" {
		t.Fatalf("cached face shape %q, %v", face, err)
	}
	err = db.Writer().QueryRowContext(t.Context(),
		`SELECT enabled FROM history_switch WHERE user_id = ?`, "user-b").Scan(&enabled)
	if err != nil || enabled != 0 {
		t.Fatalf("history switch %d, %v", enabled, err)
	}
	if got := countUser(t, db, store.TableHistorySwitch, "user-c"); got != 0 {
		t.Fatalf("user with no switch row has %d rows, want none (off)", got)
	}
	err = db.Writer().QueryRowContext(t.Context(),
		`SELECT fidelity FROM look_products WHERE look_id = ?`, "user-a-look").Scan(&fidelity)
	if err != nil || fidelity != string(domain.ExactProduct) {
		t.Fatalf("label %q, %v", fidelity, err)
	}

	_, err = db.Writer().ExecContext(t.Context(),
		`INSERT INTO products (id, user_id, category, name, fidelity, origin)
		 VALUES ('bad-label', 'user-a', 'earrings', 'Stud', 'exact', 'user')`)
	if err == nil {
		t.Fatal("catalog spelling \"exact\" was accepted as a label")
	}
	_, err = db.Writer().ExecContext(t.Context(),
		`UPDATE looks SET verdict = 'score' WHERE id = ?`, "user-a-look")
	if err == nil {
		t.Fatal("verdict \"score\" was accepted")
	}
	_, err = db.Writer().ExecContext(t.Context(),
		`UPDATE history_switch SET enabled = 2 WHERE user_id = ?`, "user-a")
	if err == nil {
		t.Fatal("history switch accepted a value other than on or off")
	}
}

func TestUserTablesDeleteOneUser(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "dresscode.db"))
	if err := store.Migrate(t.Context(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	seedUser(t, db, "user-a", 1)
	seedUser(t, db, "user-b", 0)

	deleteUser(t, db, "user-a", store.UserTables())
	for _, table := range store.UserTables() {
		if got := countUser(t, db, table, "user-a"); got != 0 {
			t.Fatalf("after delete, %s still holds %d rows for user-a", table, got)
		}
		if got := countUser(t, db, table, "user-b"); got != 1 {
			t.Fatalf("after delete, %s holds %d rows for user-b, want 1", table, got)
		}
	}

	reverse := store.UserTables()
	slices.Reverse(reverse)
	deleteUser(t, db, "user-b", reverse)
	for _, table := range store.UserTables() {
		if got := countUser(t, db, table, "user-b"); got != 0 {
			t.Fatalf("reverse delete left %d rows in %s", got, table)
		}
	}
}

func TestMigrateBesideIdentity(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "dresscode.db"))
	if _, err := sqlitestore.Open(t.Context(), sqlitestore.Config{DB: db}); err != nil {
		t.Fatalf("identity: %v", err)
	}
	if err := store.Migrate(t.Context(), db); err != nil {
		t.Fatalf("store: %v", err)
	}
	before := schemaSnapshot(t, db)

	if _, err := sqlitestore.Open(t.Context(), sqlitestore.Config{DB: db}); err != nil {
		t.Fatalf("identity again: %v", err)
	}
	if err := store.Migrate(t.Context(), db); err != nil {
		t.Fatalf("store again: %v", err)
	}
	if after := schemaSnapshot(t, db); !slices.Equal(before, after) {
		t.Fatalf("second migrate changed the schema\n first %v\n second %v", before, after)
	}

	for _, name := range []string{"users", "sessions", "identities", "sign_in_codes"} {
		if !tableExists(t, db, name) {
			t.Fatalf("identity table %s is missing", name)
		}
		if slices.Contains(store.UserTables(), name) {
			t.Fatalf("UserTables includes identity table %s", name)
		}
	}
	for _, name := range store.UserTables() {
		if !tableExists(t, db, name) {
			t.Fatalf("store table %s missing beside identity", name)
		}
	}
}

func openDB(t *testing.T, path string) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(t.Context(), sqlite.Config{
		Path:   path,
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedUser(t *testing.T, db *sqlite.DB, user string, history int) {
	t.Helper()
	ctx := t.Context()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Writer().ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %s: %v", user, err)
		}
	}
	look := user + "-look"
	render := user + "-render"
	product := user + "-product"
	exec(`INSERT INTO looks
		(id, user_id, selfie_id, photo_hash, occasion, fix, runner_up, verdict, created_at)
		VALUES (?, ?, ?, ?, 'family-function', 'family-gold-jhumkas', 'family-red-lip', ?, 1)`,
		look, user, user+"-selfie", user+"-hash", string(domain.VerdictKept))
	exec(`INSERT INTO look_flags (look_id, user_id, position, criterion_id) VALUES (?, ?, 0, 'earring')`,
		look, user)
	exec(`INSERT INTO renders (id, look_id, user_id, media_id, fix_id, position, discarded)
		VALUES (?, ?, ?, ?, 'family-gold-jhumkas', 0, 0)`,
		render, look, user, user+"-media")
	exec(`INSERT INTO render_checks
		(render_id, look_id, user_id, change_made, same_person, criterion_met)
		VALUES (?, ?, ?, 1, 1, 1)`,
		render, look, user)
	exec(`INSERT INTO render_check_attributes (render_id, user_id, attribute_id, value)
		VALUES (?, ?, 'jewellery', 'jhumkas')`,
		render, user)
	exec(`INSERT INTO products (id, user_id, category, name, fidelity, origin)
		VALUES (?, ?, 'earrings', 'Gold jhumkas', ?, ?)`,
		product, user, string(domain.ExactProduct), string(domain.OriginUser))
	exec(`INSERT INTO look_products
		(look_id, user_id, position, product_id, category, name, fidelity, origin)
		VALUES (?, ?, 0, 'gold-jhumkas', 'earrings', 'Gold jhumkas', ?, ?)`,
		look, user, string(domain.ExactProduct), string(domain.OriginCatalog))
	exec(`INSERT INTO opt_ins (user_id, occasion, fix_id) VALUES (?, 'family-function', 'family-gold-jhumkas')`,
		user)
	exec(`INSERT INTO history_switch (user_id, enabled) VALUES (?, ?)`, user, history)
	exec(`INSERT INTO analysis_cache (user_id, photo_hash, task, face_shape)
		VALUES (?, ?, 'face-attr-analysis', 'oval')`,
		user, user+"-hash")
	exec(`INSERT INTO look_snapshots (root_id, user_id, snapshot) VALUES (?, ?, '{}')`,
		look, user)
}

func deleteUser(t *testing.T, db *sqlite.DB, user string, tables []string) {
	t.Helper()
	for _, table := range tables {
		query := `DELETE FROM "` + table + `" WHERE "` + store.UserIDColumn + `" = ?`
		if _, err := db.Writer().ExecContext(t.Context(), query, user); err != nil {
			t.Fatalf("delete %s for %s: %v", table, user, err)
		}
	}
}

func countUser(t *testing.T, db *sqlite.DB, table, user string) int {
	t.Helper()
	query := `SELECT COUNT(*) FROM "` + table + `" WHERE "` + store.UserIDColumn + `" = ?`
	var n int
	if err := db.Writer().QueryRowContext(t.Context(), query, user).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func schemaSnapshot(t *testing.T, db *sqlite.DB) []string {
	t.Helper()
	rows, err := db.Writer().QueryContext(t.Context(),
		`SELECT type, name, COALESCE(sql, '') FROM sqlite_master
		 WHERE name NOT LIKE 'sqlite_%' ORDER BY type, name`)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var kind, name, sql string
		if err := rows.Scan(&kind, &name, &sql); err != nil {
			t.Fatalf("schema scan: %v", err)
		}
		got = append(got, kind+" "+name+" "+sql)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return got
}

func ledger(t *testing.T, db *sqlite.DB) []string {
	t.Helper()
	query := `SELECT filename FROM "` + store.SchemaNamespace + `_schema_migrations" ORDER BY filename`
	rows, err := db.Writer().QueryContext(t.Context(), query)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("ledger scan: %v", err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("ledger: %v", err)
	}
	return got
}

// appTables returns every application table. Migration ledgers are not
// user data, so they are left out.
func appTables(t *testing.T, db *sqlite.DB) []string {
	t.Helper()
	rows, err := db.Writer().QueryContext(t.Context(),
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("tables: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("tables scan: %v", err)
		}
		if strings.HasSuffix(name, "_schema_migrations") {
			continue
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("tables: %v", err)
	}
	return got
}

func tableExists(t *testing.T, db *sqlite.DB, name string) bool {
	t.Helper()
	var n int
	err := db.Writer().QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n)
	if err != nil {
		t.Fatalf("table %s: %v", name, err)
	}
	return n == 1
}

func columnNotNull(t *testing.T, db *sqlite.DB, table, column string) bool {
	t.Helper()
	for _, col := range columns(t, db, table) {
		if col.name == column {
			return col.notNull
		}
	}
	return false
}

func columnNames(t *testing.T, db *sqlite.DB, table string) []string {
	t.Helper()
	cols := columns(t, db, table)
	names := make([]string, len(cols))
	for i, col := range cols {
		names[i] = col.name
	}
	return names
}

func pkColumns(t *testing.T, db *sqlite.DB, table string) []string {
	t.Helper()
	cols := columns(t, db, table)
	type keyed struct {
		pos  int
		name string
	}
	var keys []keyed
	for _, col := range cols {
		if col.pk > 0 {
			keys = append(keys, keyed{pos: col.pk, name: col.name})
		}
	}
	slices.SortFunc(keys, func(a, b keyed) int { return a.pos - b.pos })
	names := make([]string, len(keys))
	for i, key := range keys {
		names[i] = key.name
	}
	return names
}

type column struct {
	name    string
	notNull bool
	pk      int
}

func columns(t *testing.T, db *sqlite.DB, table string) []column {
	t.Helper()
	rows, err := db.Writer().QueryContext(t.Context(), `SELECT name, "notnull", pk FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("columns %s: %v", table, err)
	}
	defer rows.Close()
	var got []column
	for rows.Next() {
		var col column
		var notNull int
		if err := rows.Scan(&col.name, &notNull, &col.pk); err != nil {
			t.Fatalf("columns %s: %v", table, err)
		}
		col.notNull = notNull == 1
		got = append(got, col)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("columns %s: %v", table, err)
	}
	if len(got) == 0 {
		t.Fatalf("table %s has no columns", table)
	}
	return got
}

func allowlistColumns(t *testing.T) []string {
	t.Helper()
	typ := reflect.TypeOf(domain.Allowlist{})
	cols := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			t.Fatalf("allowlist field %s has no json name", typ.Field(i).Name)
		}
		cols = append(cols, name)
	}
	slices.Sort(cols)
	return cols
}
