package main

import (
	"os"
	"os/exec"
	"testing"
)

func TestMain(m *testing.M) {
	if cmd := os.Getenv("DRESSCODE_TEST_CMD"); cmd != "" {
		os.Exit(run([]string{cmd}, os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

func TestVersionAndServeExitZero(t *testing.T) {
	cases := []struct {
		cmd  string
		want string
	}{
		{cmd: "version", want: "dresscode 0.0.0\n"},
		{cmd: "serve", want: "dresscode serve: not listening\n"},
	}
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			cmd := exec.Command(os.Args[0])
			cmd.Env = append(os.Environ(), "DRESSCODE_TEST_CMD="+tc.cmd)
			out, err := cmd.Output()
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					t.Fatalf("%s: %v\n%s", tc.cmd, err, ee.Stderr)
				}
				t.Fatalf("%s: %v", tc.cmd, err)
			}
			if string(out) != tc.want {
				t.Fatalf("%s output %q, want %q", tc.cmd, out, tc.want)
			}
		})
	}
}
