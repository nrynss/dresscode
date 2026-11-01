package main

import (
	"fmt"
	"io"
	"os"

	"github.com/nrynss/keel/id"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: dresscode version|serve|budget|flag")
		return 2
	}
	switch args[0] {
	case "version":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "usage: dresscode version")
			return 2
		}
		fmt.Fprintln(stdout, versionLine())
		return 0
	case "serve":
		return runServe(args[1:], stdout, stderr, nil, serveInterrupts())
	case "budget":
		return runBudget(args[1:], stdout, stderr, nil)
	case "flag":
		return runFlag(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		return 2
	}
}

func versionLine() string {
	if !id.Valid("0123456789abcdef0123456789abcdef") {
		return "dresscode (invalid id)"
	}
	return "dresscode 0.0.0"
}
