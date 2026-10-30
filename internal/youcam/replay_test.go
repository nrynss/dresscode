package youcam

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCallsMissWhenNothingIsRecorded(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(repoRoot(t), "testdata", "youcam")
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("testdata/youcam exists; this task must not invent YouCam fixtures")
	}
	replay := openDir(t, dir)
	ctx := context.Background()
	file := File{Task: "cloth-v4", ContentType: "image/jpeg", Bytes: []byte("a")}
	keys := map[string]string{}
	record := func(op string, err error) {
		t.Helper()
		var missing *Missing
		if !errors.As(err, &missing) || missing.Key == "" || !strings.Contains(err.Error(), missing.Key) {
			t.Fatalf("%s: %v", op, err)
		}
		keys[op] = missing.Key
	}
	if _, err := replay.Upload(ctx, file); err == nil {
		t.Fatal("upload succeeded")
	} else {
		record("upload", err)
	}
	if _, err := replay.Create(ctx, Task{Name: "cloth-v4", Payload: json.RawMessage(`{"src":"file-1"}`)}); err == nil {
		t.Fatal("create succeeded")
	} else {
		record("create", err)
	}
	if _, err := replay.Poll(ctx, PollRef{Task: "cloth-v4", TaskID: "task-1"}); err == nil {
		t.Fatal("poll succeeded")
	} else {
		record("poll", err)
	}
	if _, err := replay.Fetch(ctx, ResultRef{TaskID: "task-1", URL: "https://example.test/r"}); err == nil {
		t.Fatal("fetch succeeded")
	} else {
		record("fetch", err)
	}
	seen := map[string]string{}
	for op, key := range keys {
		if prev, ok := seen[key]; ok {
			t.Fatalf("%s and %s share key %s", op, prev, key)
		}
		seen[key] = op
	}
	if _, err := replay.Reply("not-recorded"); err == nil || !strings.Contains(err.Error(), "not-recorded") {
		t.Fatal(err)
	}
	miss, err := replay.Serve("POST", "/s2s/v2.0/file/cloth-v4", json.RawMessage(`{"n":1}`))
	var missing *Missing
	if !errors.As(err, &missing) || missing.Key == "" || !strings.Contains(err.Error(), missing.Key) || miss.Response != nil {
		t.Fatalf("%#v %v", miss, err)
	}
}

func TestReplayServesATypedRecording(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := File{Task: "cloth-v4", ContentType: "image/jpeg", Bytes: []byte("a")}
	writeExchange(t, dir, "upload-cloth", file, Slot{FileID: "file-1", URL: "https://example.test/slot"})
	writeExchange(t, dir, "upload-no-face", File{Task: "cloth-v4", ContentType: "image/jpeg", Bytes: []byte("b")}, Error{Kind: KindNoFace, Detail: "error_no_face"})
	writeExchange(t, dir, "upload-bad-file", File{Task: "cloth-v4", ContentType: "image/jpeg", Bytes: []byte("c")}, Error{Kind: KindBadFile, Detail: "error_decode_image"})
	writeExchange(t, dir, "upload-expired", File{Task: "cloth-v4", ContentType: "image/jpeg", Bytes: []byte("d")}, Error{Kind: KindExpired, Detail: "expired_file"})
	writeExchange(t, dir, "upload-units", File{Task: "cloth-v4", ContentType: "image/jpeg", Bytes: []byte("e")}, Error{Kind: KindOutOfUnits, Detail: "insufficient_units"})
	writeExchange(t, dir, "create-cloth", Task{Name: "cloth-v4", Payload: json.RawMessage(`{"src":"file-1"}`)}, Created{TaskID: "task-1", PollingInterval: 2})
	writeExchange(t, dir, "poll-cloth", PollRef{Task: "cloth-v4", TaskID: "task-1"}, Status{TaskID: "task-1", Status: StatusSuccess, ResultURL: "https://example.test/r"})
	writeExchange(t, dir, "fetch-cloth", ResultRef{TaskID: "task-1", URL: "https://example.test/r"}, Result{TaskID: "task-1", URL: "https://example.test/r", Bytes: []byte("a")})

	replay := openDir(t, dir)
	ctx := context.Background()
	slot, err := replay.Upload(ctx, file)
	if err != nil || slot.FileID != "file-1" {
		t.Fatalf("%#v %v", slot, err)
	}
	checks := []struct {
		file File
		want *Error
	}{
		{File{Task: "cloth-v4", ContentType: "image/jpeg", Bytes: []byte("b")}, ErrNoFace},
		{File{Task: "cloth-v4", ContentType: "image/jpeg", Bytes: []byte("c")}, ErrBadFile},
		{File{Task: "cloth-v4", ContentType: "image/jpeg", Bytes: []byte("d")}, ErrExpired},
		{File{Task: "cloth-v4", ContentType: "image/jpeg", Bytes: []byte("e")}, ErrOutOfUnits},
	}
	for _, check := range checks {
		_, err := replay.Upload(ctx, check.file)
		if !errors.Is(err, check.want) {
			t.Fatalf("%q: %v", check.want.Kind, err)
		}
	}
	created, err := replay.Create(ctx, Task{Name: "cloth-v4", Payload: json.RawMessage(`{ "src" : "file-1" }`)})
	if err != nil || created.TaskID != "task-1" || created.PollingInterval != 2 {
		t.Fatalf("%#v %v", created, err)
	}
	status, err := replay.Poll(ctx, PollRef{Task: "cloth-v4", TaskID: "task-1"})
	if err != nil || status.Status != StatusSuccess || status.ResultURL == "" {
		t.Fatalf("%#v %v", status, err)
	}
	result, err := replay.Fetch(ctx, ResultRef{TaskID: "task-1", URL: "https://example.test/r"})
	if err != nil || string(result.Bytes) != "a" {
		t.Fatalf("%#v %v", result, err)
	}

	raw, err := replay.Reply("upload-cloth")
	if err != nil || !strings.Contains(string(raw.Response), "file-1") {
		t.Fatal(err)
	}
}

func TestServeHitsAndMisses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := json.RawMessage(`{"file_id":"file-1"}`)
	writeExchange(t, dir, "http-upload", map[string]any{
		"method":  "POST",
		"path":    "/s2s/v2.0/file/cloth-v4",
		"headers": map[string]string{"authorization": "Bearer secret"},
		"body":    body,
	}, map[string]any{"status": 200, "body": map[string]any{"file_id": "file-1"}})
	replay := openDir(t, dir)
	got, err := replay.Serve("POST", "/s2s/v2.0/file/cloth-v4", json.RawMessage(`{ "file_id" : "file-1" }`))
	if err != nil || got.Key != "http-upload" || !strings.Contains(string(got.Response), "file-1") {
		t.Fatalf("%#v %v", got, err)
	}
	if !strings.Contains(string(got.Request), "Bearer secret") {
		t.Fatal("request was rewritten")
	}
	key, err := ContentKey("POST", "/s2s/v2.0/file/other", body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := replay.Serve("POST", "/s2s/v2.0/file/other", body); err == nil || !strings.Contains(err.Error(), key) {
		t.Fatal(err)
	}
}

func TestEmptyFileIsNotAMiss(t *testing.T) {
	t.Parallel()
	replay := openDir(t, t.TempDir())
	_, err := replay.Upload(context.Background(), File{})
	if err == nil || errors.As(err, new(*Missing)) {
		t.Fatal(err)
	}
}

func openDir(t *testing.T, dir string) *Replay {
	t.Helper()
	replay, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return replay
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func writeExchange(t *testing.T, dir, key string, request, response any) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"exchange": key,
		"request":  request,
		"response": response,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, key+".json"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
}
