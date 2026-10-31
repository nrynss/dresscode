// Ops is this file: the operator's controls over a running server. Two
// commands drive it, `dresscode budget` and `dresscode flag`, and both
// work on the server's own database file from a second process. That
// file is the whole mechanism. The runtime flags are Keel flag rows the
// landed readers resolve on every request - the intake refuses an open
// it has to refuse by reading the row, the look start reads the same
// rows, and the render stage reads the renders switch once per attempt -
// so a row the command rewrites is the new state on the next request,
// with no restart and nothing to invalidate. The spend books are Keel
// cost stores on the same file, the stores the server's meters reserve
// against and settle into: the books the command reads are the books
// the server enforces, without talking to the process. SQLite's WAL
// lets the second handle read and write while the server holds its own,
// and the busy timeout rides out the moments both would write.
//
// The units books carry the judging split the settings file holds. The
// keyed budget has two owners and no third: `judging`, whose ceiling is
// the share held for judges, and `development`, whose ceiling is the
// rest. Everything that is not a judge lease spends as development, so
// a non-judge spend that would reach the judging share is refused by
// its own ceiling - the typed refusal the render stage wraps as
// `budget_refused` and the start checks before any paid call - while a
// judge lease draws its own share down to zero. OpenSpend is that
// wiring's seam: the caller that builds the YouCam meters hands the
// development account to everything that is not a judge session and
// the judging account to the sessions that are.
//
// This file writes flags and reads books. It mounts no route.

package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/nrynss/dresscode/internal/config"
	"github.com/nrynss/keel/cost"
	coststore "github.com/nrynss/keel/cost/sqlitestore"
	"github.com/nrynss/keel/flag"
	flagsql "github.com/nrynss/keel/flag/sqlitestore"
	"github.com/nrynss/keel/sqlite"
)

// The spend namespaces this file owns on the server's database. The
// composition wiring opens the same names when it builds the meters, so
// the books one process settles are the books the other reads. Each
// namespace keeps its own ledger and tables; neither ends in the
// reserved _owner suffix.
const (
	// NamespaceUnits is the YouCam credit budget: one lifetime ceiling,
	// the judging share held beside it.
	NamespaceUnits = "spend_youcam"
	// NamespaceDollars is the Workers AI dollar budget: one ceiling per
	// UTC day.
	NamespaceDollars = "spend_workers_ai"
)

// opsFlags is the three operator switches, in the order the flag
// command lists them. The defaults are false, which is what the landed
// readers declare: a switch with no stored row reads as the open
// position. The stored row, once the command writes one, wins.
func opsFlags() []flag.Bool {
	return []flag.Bool{
		{Name: config.FlagFreshUploadsOff, Default: false, Help: "Refuse a fresh selfie upload and POST /api/photos."},
		{Name: config.FlagRendersOff, Default: false, Help: "Refuse a YouCam render."},
		{Name: config.FlagSamplesOnly, Default: false, Help: "Refuse fresh uploads, photos, products, and links."},
	}
}

// FlagState is one operator switch as the command prints it. A switch
// nobody has set carries the zero time.
type FlagState struct {
	Name    string
	On      bool
	Changed time.Time
	Help    string
}

// Flags reads the three operator switches from the database at dbPath.
// It refuses a path with no database, so a mistyped path is an error
// and not a fresh file that quietly forgets the flip.
func Flags(ctx context.Context, dbPath string) ([]FlagState, error) {
	db, err := openOpsDB(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	store, err := flagsql.Open(ctx, flagsql.Config{DB: db, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		return nil, fmt.Errorf("server: ops: %w", err)
	}
	decls := opsFlags()
	out := make([]FlagState, 0, len(decls))
	for _, decl := range decls {
		on, changed, err := store.Bool(ctx, decl)
		if err != nil {
			return nil, fmt.Errorf("server: ops: read %s: %w", decl.Name, err)
		}
		out = append(out, FlagState{Name: decl.Name, On: on, Changed: changed, Help: decl.Help})
	}
	return out, nil
}

// SetFlag stores value for the named operator switch in the database at
// dbPath. The running server resolves it on the next request that reads
// it: the next upload, the next look start, the next render attempt.
// Name must be one of the three; the error names the ones that are.
func SetFlag(ctx context.Context, dbPath, name string, value bool) error {
	db, err := openOpsDB(ctx, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	store, err := flagsql.Open(ctx, flagsql.Config{DB: db, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		return fmt.Errorf("server: ops: %w", err)
	}
	decls := opsFlags()
	var decl *flag.Bool
	for i := range decls {
		if decls[i].Name == name {
			decl = &decls[i]
			break
		}
	}
	if decl == nil {
		known := ""
		for i, one := range opsFlags() {
			if i > 0 {
				known += ", "
			}
			known += one.Name
		}
		return fmt.Errorf("server: ops: unknown flag %q; the flags are %s", name, known)
	}
	if _, err := store.SetBool(ctx, *decl, value); err != nil {
		return fmt.Errorf("server: ops: set %s: %w", name, err)
	}
	return nil
}

// openOpsDB opens the server's database for an ops command. The file
// must be there already: a server was open at least once, and a command
// that created a fresh file would flip a flag or read books nobody
// serves.
func openOpsDB(ctx context.Context, dbPath string) (*sqlite.DB, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("server: ops: database path is empty")
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("server: ops: database %s: %w", dbPath, err)
	}
	db, err := sqlite.Open(ctx, sqlite.Config{Path: dbPath, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		return nil, fmt.Errorf("server: ops: %w", err)
	}
	return db, nil
}

// Spend is the two spend books of one server database: the YouCam
// credits with their judging split, and the Workers AI dollars of one
// UTC day. Open it beside a running server; SQLite's WAL carries the
// second handle. Close it when the report is printed.
type Spend struct {
	db       *sqlite.DB
	settings *config.Settings
	units    *coststore.Store
	keyed    *coststore.KeyedBudget
	dollars  *coststore.Store
}

// OpenSpend opens the spend books on the database at dbPath, with the
// ceilings and the judging split of the settings file. The settings
// file owns the numbers: the ceilings are rewritten on every open, so
// the command and the server agree with the file and a changed file
// moves the ceiling without touching recorded spend. The owner
// ceilings are set the same way, so a first open on a database the
// server has not charged yet bootstraps the books at the file's split.
func OpenSpend(ctx context.Context, dbPath string, settings *config.Settings) (*Spend, error) {
	if settings == nil {
		return nil, fmt.Errorf("server: ops: no settings")
	}
	units := settings.Budgets.YouCam
	if err := checkUnitsBudget(units); err != nil {
		return nil, fmt.Errorf("server: ops: %w", err)
	}
	dollarLimit, err := settings.Budgets.WorkersAI.Limit()
	if err != nil {
		return nil, fmt.Errorf("server: ops: %w", err)
	}
	db, err := openOpsDB(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	log := slog.New(slog.DiscardHandler)
	unitStore, err := coststore.Open(ctx, coststore.Config{
		DB:           db,
		Namespace:    NamespaceUnits,
		Limit:        cost.Price(units.Units),
		Period:       units.Period(),
		Denomination: units.Denomination(),
		Logger:       log,
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("server: ops: %w", err)
	}
	keyed := coststore.NewKeyedBudget(unitStore)
	for _, owner := range []struct {
		name  string
		limit cost.Price
	}{
		{name: config.OwnerJudging, limit: cost.Price(units.JudgingUnits)},
		{name: config.OwnerDevelopment, limit: cost.Price(units.DevelopmentUnits())},
	} {
		if err := keyed.SetLimit(ctx, owner.name, owner.limit); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("server: ops: the %s share: %w", owner.name, err)
		}
	}
	dollarStore, err := coststore.Open(ctx, coststore.Config{
		DB:        db,
		Namespace: NamespaceDollars,
		Limit:     dollarLimit,
		Period:    settings.Budgets.WorkersAI.Period(),
		Logger:    log,
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("server: ops: %w", err)
	}
	return &Spend{db: db, settings: settings, units: unitStore, keyed: keyed, dollars: dollarStore}, nil
}

// checkUnitsBudget restates the settings' own validation, which is
// unexported: the books must not open onto a split that holds nothing
// or holds more than the pool.
func checkUnitsBudget(b config.YouCamBudget) error {
	if b.Units < 1 || b.JudgingUnits < 1 || b.JudgingUnits > b.Units {
		return fmt.Errorf("budgets.youcam: units %d, judging share %d", b.Units, b.JudgingUnits)
	}
	return nil
}

// Close releases the database handle. A second call does nothing.
func (s *Spend) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

// Units is the keyed budget: two owners over one pool of credits.
func (s *Spend) Units() *coststore.KeyedBudget { return s.keyed }

// Ledger is where a units meter records its settled charges. It is the
// same store the keyed budget enforces, so the books and the ledger are
// one.
func (s *Spend) Ledger() cost.ChargeSink { return s.units }

// Dollars is the Workers AI books: its Account is the meter's account
// and the store itself is the charge sink.
func (s *Spend) Dollars() *coststore.Store { return s.dollars }

// Development is the account every spend that is not a judge lease
// reserves against. Its ceiling is the units minus the judging share,
// so the share held for judges is unreachable from here: the refusal is
// the budget's own typed one, cost.ErrOverBudget, which the render
// stage carries as budget_refused and the start refuses before any paid
// call.
func (s *Spend) Development(ctx context.Context) (cost.ContextAccount, error) {
	return s.keyed.Owner(ctx, config.OwnerDevelopment)
}

// Judging is the account a judge lease reserves against. It is the only
// account that can spend the held-back share.
func (s *Spend) Judging(ctx context.Context) (cost.ContextAccount, error) {
	return s.keyed.Owner(ctx, config.OwnerJudging)
}

// UnitsLeft is the development share's remaining headroom: the read the
// look start makes before it takes a judge seat, as LooksConfig.UnitsLeft.
// A remaining below the look's estimate refuses the start, so a user can
// never spend the judging share by starting a look.
func (s *Spend) UnitsLeft(ctx context.Context) (cost.Price, error) {
	return s.keyed.Remaining(ctx, config.OwnerDevelopment)
}

// ShareReport is one ceiling's side of the books, in the store's own
// denomination. Left is the headroom for a new reservation: the ceiling
// minus booked spend minus the holds still standing.
type ShareReport struct {
	Ceiling cost.Price
	Spent   cost.Price
	Left    cost.Price
}

// UnitsReport is the credit books: the pool and the two shares.
type UnitsReport struct {
	Ceiling     cost.Price
	Spent       cost.Price
	Left        cost.Price
	Judging     ShareReport
	Development ShareReport
}

// DollarsReport is the dollar books of the running UTC day.
type DollarsReport struct {
	PerDay cost.Price
	Spent  cost.Price
	Left   cost.Price
}

// BudgetReport is what the budget command prints.
type BudgetReport struct {
	Units   UnitsReport
	Dollars DollarsReport
}

// Report reads the books: the pool, each share, and the day's dollars.
// The ceilings are the settings file's own numbers, the same ones this
// open wrote onto the store; spent and left are the store's reads. Left
// nets out the holds still standing, so a look mid-flight shows as a
// little less left until it settles or releases.
func (s *Spend) Report(ctx context.Context) (BudgetReport, error) {
	var out BudgetReport
	youcam := s.settings.Budgets.YouCam
	unitsSpent, err := s.units.Spent(ctx)
	if err != nil {
		return out, fmt.Errorf("server: ops: units spent: %w", err)
	}
	unitsLeft, err := s.units.Remaining(ctx)
	if err != nil {
		return out, fmt.Errorf("server: ops: units left: %w", err)
	}
	out.Units = UnitsReport{
		Ceiling:     cost.Price(youcam.Units),
		Spent:       unitsSpent,
		Left:        unitsLeft,
		Judging:     ShareReport{Ceiling: cost.Price(youcam.JudgingUnits)},
		Development: ShareReport{Ceiling: cost.Price(youcam.DevelopmentUnits())},
	}
	share := func(owner string, into *ShareReport) error {
		spent, err := s.keyed.SpentSince(ctx, owner, time.Time{})
		if err != nil {
			return fmt.Errorf("server: ops: %s spent: %w", owner, err)
		}
		left, err := s.keyed.Remaining(ctx, owner)
		if err != nil {
			return fmt.Errorf("server: ops: %s left: %w", owner, err)
		}
		into.Spent = spent
		into.Left = left
		return nil
	}
	if err := share(config.OwnerJudging, &out.Units.Judging); err != nil {
		return out, err
	}
	if err := share(config.OwnerDevelopment, &out.Units.Development); err != nil {
		return out, err
	}
	dollarSpent, err := s.dollars.Spent(ctx)
	if err != nil {
		return out, fmt.Errorf("server: ops: dollars spent: %w", err)
	}
	dollarLeft, err := s.dollars.Remaining(ctx)
	if err != nil {
		return out, fmt.Errorf("server: ops: dollars left: %w", err)
	}
	perDay, err := s.settings.Budgets.WorkersAI.Limit()
	if err != nil {
		return out, fmt.Errorf("server: ops: %w", err)
	}
	out.Dollars = DollarsReport{PerDay: perDay, Spent: dollarSpent, Left: dollarLeft}
	return out, nil
}

// Budget opens the spend books at dbPath, reads them, and closes them.
// It is the budget command's one call.
func Budget(ctx context.Context, dbPath string, settings *config.Settings) (BudgetReport, error) {
	spend, err := OpenSpend(ctx, dbPath, settings)
	if err != nil {
		return BudgetReport{}, err
	}
	defer func() { _ = spend.Close() }()
	return spend.Report(ctx)
}
