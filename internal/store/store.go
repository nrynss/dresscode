// Package store is Dresscode's SQLite schema.
//
// The schema is looks, renders and their checks, the analysis cache, products
// and their labels, the opt-in menu, the history switch, and the snapshot a
// restarted look rebuilds from. Migrate applies it through Keel sqlite under
// SchemaNamespace, so the ledger does not collide with identity or any other
// Keel store on the same file. Identity's own tables are not created here, and
// nothing here references them: a fresh file migrates before identity does.
//
// Every row a user owns carries UserIDColumn. UserTables is the list deletion
// walks. Foreign keys cascade, so deleting one user's rows from each table
// works in the list's order or any other.
package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/nrynss/keel/sqlite"
)

// SchemaNamespace is the migration ledger this package owns.
const SchemaNamespace = "store"

// UserIDColumn is the owner column on every table UserTables names.
// The value is the identity user id. This package does not store the user.
const UserIDColumn = "user_id"

// Table names. UserTables is this set.
const (
	TableAnalysisCache         = "analysis_cache"
	TableHistorySwitch         = "history_switch"
	TableLookFlags             = "look_flags"
	TableLookProducts          = "look_products"
	TableLookSnapshots         = "look_snapshots"
	TableLooks                 = "looks"
	TableOptIns                = "opt_ins"
	TableProducts              = "products"
	TableRenderCheckAttributes = "render_check_attributes"
	TableRenderChecks          = "render_checks"
	TableRenders               = "renders"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// userTables is sorted. UserTables returns a copy.
var userTables = []string{
	TableAnalysisCache,
	TableHistorySwitch,
	TableLookFlags,
	TableLookProducts,
	TableLookSnapshots,
	TableLooks,
	TableOptIns,
	TableProducts,
	TableRenderCheckAttributes,
	TableRenderChecks,
	TableRenders,
}

// UserTables returns every table that holds rows a user owns.
// Deletion reads this list. The caller may reorder or change the returned
// slice; the next call is unaffected.
func UserTables() []string {
	out := make([]string, len(userTables))
	copy(out, userTables)
	return out
}

// Migrate applies the store schema to db. A second call on the same file
// changes nothing. db must not be nil, and Migrate does not close it.
func Migrate(ctx context.Context, db *sqlite.DB) error {
	if db == nil {
		return fmt.Errorf("store: migrate: nil database")
	}
	schema, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if err := sqlite.Migrate(ctx, db, SchemaNamespace, schema); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}
