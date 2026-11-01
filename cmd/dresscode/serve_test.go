package main

// The serve command's proof, over a real listener: the command loads the
// settings, builds the clients without dialling anything, composes the
// server over a temp file, and answers HTTP. The flag flip runs through
// runFlag, the budget read through runBudget, so the proofs are the
// operator's own two commands reaching a running server. Nothing dials:
// the provider clients hold stand-in credentials and are never asked to
// make a call, so no key material is real and no unit moves.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/dresscode/internal/config"
)

// repoRoot is the repository root, for the data directories the settings
// file names.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// serveEnv answers the ten key variables and the two signing keys. The
// Lambo URL and the from address are stand-ins of the shape the client
// constructors check, because construction validates shape and dials
// nothing; no other value is read.
func serveEnv(name string) (string, bool) {
	switch name {
	case config.VarLamboURL:
		return "http://127.0.0.1:7720", true
	case config.VarResendFrom:
		return "codes@example.com", true
	case VarSigningKey:
		return "serve-test-session-signing-key-32b", true
	case VarCodeKey:
		return "serve-test-signin-code-key-32b!!!", true
	}
	return standInEnv(name)
}

// serveSettings is the ops fixture's settings file with the [paths]
// section the server boots from, pointing at the repository's own data.
func serveSettings(t *testing.T) string {
	t.Helper()
	path := settingsFile(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.Write(raw)
	fmt.Fprintf(&b, "\n[paths]\noccasions = %q\nsamples = %q\n",
		filepath.Join(repoRoot(t), "testdata", "occasions"),
		filepath.Join(repoRoot(t), "testdata", "samples"))
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// syncWriter is safe for the serve goroutine and the test to share.
type syncWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// waitListening polls the output until the listening line names an
// address, then waits until the server answers, because the line is
// printed a moment before Serve starts accepting.
func waitListening(t *testing.T, out *syncWriter) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var addr string
	for {
		if line := out.String(); strings.Contains(line, "listening on http://") {
			first := strings.Split(line, "\n")[0]
			addr = strings.TrimSpace(strings.TrimPrefix(first, "listening on "))
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the server never listened; output:\n%s", out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	for {
		resp, err := http.Get(addr + "/api/session")
		if err == nil {
			resp.Body.Close()
			return addr
		}
		if time.Now().After(deadline) {
			t.Fatalf("the listener never answered: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestServeComposesAndListens(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "index.html"), []byte(appIndexHTML), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "dresscode.db")
	settingsPath := serveSettings(t)

	out := &syncWriter{}
	stop := make(chan struct{})
	exit := make(chan int, 1)
	go func() {
		exit <- runServe([]string{
			"-db", dbPath, "-config", settingsPath, "-app", app, "-addr", "127.0.0.1:0",
		}, out, out, serveEnv, stop)
	}()

	base := waitListening(t, out)

	// A first visit mints a guest, over the real listener.
	resp, err := http.Get(base + "/api/session")
	if err != nil {
		t.Fatal(err)
	}
	var session struct {
		Kind string `json:"kind"`
	}
	decodeInto(t, resp.Body, &session)
	resp.Body.Close()
	if session.Kind != "guest" {
		t.Fatalf("session kind %q", session.Kind)
	}

	// The flag proof, through the operator's own command: the flip
	// reaches the running server's next request, with no restart.
	var cmd bytes.Buffer
	if code := runFlag([]string{"-db", dbPath, "fresh_uploads_off", "on"}, &cmd, &cmd); code != 0 {
		t.Fatalf("flag flip exit %d %s", code, cmd.String())
	}
	resp, err = http.Post(base+"/api/uploads", "application/json",
		strings.NewReader(`{"content_type":"image/jpeg","chunk_size":100,"size_bytes":10,"group":"selfie"}`))
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, "uploads_paused") {
		t.Fatalf("the paused open answers %d %s, want 403 uploads_paused", resp.StatusCode, body)
	}
	cmd.Reset()
	if code := runFlag([]string{"-db", dbPath, "fresh_uploads_off", "off"}, &cmd, &cmd); code != 0 {
		t.Fatalf("flag clear exit %d %s", code, cmd.String())
	}
	resp, err = http.Post(base+"/api/uploads", "application/json",
		strings.NewReader(`{"content_type":"image/jpeg","chunk_size":100,"size_bytes":10,"group":"selfie"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("the reopened upload answers %d, want 201", resp.StatusCode)
	}

	// The books proof, through the operator's own command: the books the
	// composed server opened are the books the budget command reads,
	// through a second handle while the server holds its own.
	cmd.Reset()
	if code := runBudget([]string{"-db", dbPath, "-config", settingsPath}, &cmd, &cmd, serveEnv); code != 0 {
		t.Fatalf("budget exit %d %s", code, cmd.String())
	}
	page := cmd.String()
	for _, want := range []string{
		"youcam units: spent 0  left 1040  ceiling 1040",
		"judging:     spent 0  left 400  ceiling 400",
		"development: spent 0  left 640  ceiling 640",
		"workers ai: spent $0.00  left $1.00  ceiling $1.00 a day",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the budget page is missing %q:\n%s", want, page)
		}
	}

	// A sample opened over the listener: the composition's samples mount
	// answers, spending nothing.
	resp, err = http.Get(base + "/api/samples")
	if err != nil {
		t.Fatal(err)
	}
	samples := readAll(t, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || strings.Count(samples, `"id"`) != 5 {
		t.Fatalf("samples %d %s", resp.StatusCode, samples)
	}

	// The posture: the interrupt stops the listener, the drain finds no
	// unfinished job, and the command exits clean.
	close(stop)
	select {
	case code := <-exit:
		if code != 0 {
			t.Fatalf("serve exit %d; output:\n%s", code, out.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("serve never stopped; output:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "stopping") {
		t.Fatalf("serve output after the stop:\n%s", out.String())
	}
}

// TestServeRefusesABrokenBoot pins the refusals before anything opens:
// a settings file that does not load, and a boot without the signing
// keys, each exit 1 with the variable or fault named and no listener.
func TestServeRefusesABrokenBoot(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "index.html"), []byte(appIndexHTML), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := runServe([]string{"-db", filepath.Join(dir, "x.db"), "-config", filepath.Join(dir, "absent.toml"), "-app", app}, &out, &out, serveEnv, nil); code != 1 {
		t.Fatalf("missing settings exit %d", code)
	}
	if !strings.Contains(out.String(), "settings file") {
		t.Fatalf("missing settings stderr %q", out.String())
	}

	out.Reset()
	if code := runServe([]string{"-db", filepath.Join(dir, "x.db"), "-config", settingsFile(t), "-app", app, "-addr", "127.0.0.1:0"}, &out, &out, standInEnv, nil); code != 1 {
		t.Fatalf("missing signing key exit %d", code)
	}
	if !strings.Contains(out.String(), VarSigningKey) {
		t.Fatalf("missing signing key stderr %q", out.String())
	}

	out.Reset()
	stop := make(chan struct{})
	close(stop)
	if code := runServe([]string{"stray"}, &out, &out, serveEnv, stop); code != 2 {
		t.Fatalf("stray argument exit %d", code)
	}
	if !strings.Contains(out.String(), "usage: dresscode serve") {
		t.Fatalf("stray argument stderr %q", out.String())
	}

	// The help carries the operator's caveat about the budget command's
	// read that writes.
	help := &bytes.Buffer{}
	if code := runBudget([]string{"-h"}, help, help, serveEnv); code != 0 {
		t.Fatalf("budget help exit %d:\n%s", code, help.String())
	}
	for _, want := range []string{
		"run it with the settings file the server booted from",
		"rewrites the ceilings",
	} {
		if !strings.Contains(help.String(), want) {
			t.Fatalf("the budget help is missing %q:\n%s", want, help.String())
		}
	}
}

const appIndexHTML = "<!doctype html><title>Dresscode</title>"

func decodeInto(t *testing.T, r io.Reader, into any) {
	t.Helper()
	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, into); err != nil {
		t.Fatalf("body %s: %v", body, err)
	}
}

func readAll(t *testing.T, r io.Reader) string {
	t.Helper()
	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
