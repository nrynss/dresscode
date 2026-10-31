// The operator's commands: `dresscode budget` and `dresscode flag`.
// Both work on the database file the server is already serving out of,
// so the flip and the report need no restart and no second channel:
// the flags are rows the running server resolves on the request that
// needs them, and the books are the ledgers its meters settle into.
// internal/server/ops.go is the mechanism; this file is the argument
// parsing and the printing.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/nrynss/dresscode/internal/config"
	"github.com/nrynss/dresscode/internal/server"
	"github.com/nrynss/keel/cost"
)

// The defaults are this repository's own working paths. A deployment
// passes -db and -config.
const (
	defaultDBPath     = "data/dresscode.db"
	defaultConfigPath = "dresscode.toml"
)

// runBudget prints the two books: the YouCam units with their judging
// split, and the Workers AI dollars of the running UTC day, each as
// spent and left. The settings file owns the ceilings, the way it does
// for the server; loading it resolves the key references and resolves
// nothing to print: no key value is ever on the page. lookup is the
// environment the load reads; nil means the process environment.
func runBudget(args []string, stdout, stderr io.Writer, lookup func(string) (string, bool)) int {
	fs := flag.NewFlagSet("budget", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dbPath := fs.String("db", defaultDBPath, "database file the server serves out of")
	configPath := fs.String("config", defaultConfigPath, "settings file with the budgets")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: dresscode budget [-db file] [-config file]")
		return 2
	}
	ctx := context.Background()
	settings, _, err := config.Load(ctx, config.Options{Path: *configPath, LookupEnv: lookup})
	if err != nil {
		fmt.Fprintf(stderr, "dresscode budget: %v\n", err)
		return 1
	}
	rep, err := server.Budget(ctx, *dbPath, &settings)
	if err != nil {
		fmt.Fprintf(stderr, "dresscode budget: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "youcam units: spent %d  left %d  ceiling %d\n",
		rep.Units.Spent, rep.Units.Left, rep.Units.Ceiling)
	fmt.Fprintf(stdout, "  judging:     spent %d  left %d  ceiling %d\n",
		rep.Units.Judging.Spent, rep.Units.Judging.Left, rep.Units.Judging.Ceiling)
	fmt.Fprintf(stdout, "  development: spent %d  left %d  ceiling %d\n",
		rep.Units.Development.Spent, rep.Units.Development.Left, rep.Units.Development.Ceiling)
	fmt.Fprintf(stdout, "workers ai: spent %s  left %s  ceiling %s a day\n",
		dollars(rep.Dollars.Spent), dollars(rep.Dollars.Left), dollars(rep.Dollars.PerDay))
	return 0
}

// dollars prints a price in dollars. An exact cent is two places; a
// finer price keeps its nine places and drops the trailing zeros, so a
// printed figure never rounds what the books hold.
func dollars(p cost.Price) string {
	whole := int64(p) / int64(cost.Dollar)
	frac := int64(p) % int64(cost.Dollar)
	cent := int64(cost.Dollar) / 100
	if frac%cent == 0 {
		return fmt.Sprintf("$%d.%02d", whole, frac/cent)
	}
	return strings.TrimRight(fmt.Sprintf("$%d.%09d", whole, frac), "0")
}

// runFlag shows the three operator switches, or flips one. A flip is
// one row in the server's database; the running server reads it on the
// next request that checks it.
func runFlag(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("flag", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dbPath := fs.String("db", defaultDBPath, "database file the server serves out of")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx := context.Background()
	switch fs.NArg() {
	case 0:
		states, err := server.Flags(ctx, *dbPath)
		if err != nil {
			fmt.Fprintf(stderr, "dresscode flag: %v\n", err)
			return 1
		}
		for _, state := range states {
			position := "off"
			if state.On {
				position = "on"
			}
			changed := "-"
			if !state.Changed.IsZero() {
				changed = state.Changed.Format(time.RFC3339)
			}
			fmt.Fprintf(stdout, "%-18s %-3s %-24s %s\n", state.Name, position, changed, state.Help)
		}
		return 0
	case 2:
		name, value := fs.Arg(0), fs.Arg(1)
		var on bool
		switch value {
		case "on":
			on = true
		case "off":
			on = false
		default:
			fmt.Fprintf(stderr, "dresscode flag: %q is not on or off\n", value)
			return 2
		}
		if err := server.SetFlag(ctx, *dbPath, name, on); err != nil {
			fmt.Fprintf(stderr, "dresscode flag: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s %s\n", name, value)
		return 0
	default:
		fmt.Fprintln(stderr, "usage: dresscode flag [-db file]              show the switches")
		fmt.Fprintln(stderr, "       dresscode flag [-db file] <name> on|off  flip one")
		return 2
	}
}
