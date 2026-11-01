package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if cmd := os.Getenv("DRESSCODE_TEST_CMD"); cmd != "" {
		os.Exit(run([]string{cmd}, os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

func TestVersionExitsZero(t *testing.T) {
	// The serve subcommand is the listener's proof, in serve_test.go
	// over a real listener; the exec harness here would boot it for
	// real, so only version rides this path.
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "DRESSCODE_TEST_CMD=version")
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("version: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("version: %v", err)
	}
	if string(out) != "dresscode 0.0.0\n" {
		t.Fatalf("version output %q", out)
	}
}

func TestUsageNamesTheCommands(t *testing.T) {
	var out bytes.Buffer
	if code := run(nil, &out, &out); code != 2 {
		t.Fatalf("no arguments exit %d", code)
	}
	if !strings.Contains(out.String(), "usage: dresscode version|serve|budget|flag") {
		t.Fatalf("usage %q", out.String())
	}
	out.Reset()
	if code := run([]string{"nope"}, &out, &out); code != 2 {
		t.Fatalf("unknown command exit %d", code)
	}
	if !strings.Contains(out.String(), `unknown command "nope"`) {
		t.Fatalf("unknown command %q", out.String())
	}
}
