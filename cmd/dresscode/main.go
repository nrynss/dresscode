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
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: dresscode version|serve")
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, versionLine())
		return 0
	case "serve":
		// Not a server yet: do not listen, dial, or read a key.
		fmt.Fprintln(stdout, "dresscode serve: not listening")
		return 0
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
