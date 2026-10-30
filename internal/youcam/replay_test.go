package youcam

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCallsMissWhenNothingIsRecorded(t *testing.T) {
	t.Parallel()
	replay := openDir(t, filepath.Join(t.TempDir(), "absent"))
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

func TestAFileUploadServesLikeItsInlineTwin(t *testing.T) {
	t.Parallel()
	image := []byte{0xff, 0xd8, 0xff, 0x01, 0x02}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "photos", "one.jpg"), image)
	inlineDir := filepath.Join(root, "inline")
	fileDir := filepath.Join(root, "file")
	slot := Slot{FileID: "file-1", URL: "https://example.test/slot"}
	writeExchange(t, inlineDir, "upload", File{Task: "ai-smile", ContentType: "image/jpeg", Bytes: image}, slot)
	writeExchange(t, fileDir, "upload", map[string]string{"task": "ai-smile", "content_type": "image/jpeg", "file": "../photos/one.jpg"}, slot)

	file := File{Task: "ai-smile", ContentType: "image/jpeg", Bytes: image}
	want, err := requestKey(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{inlineDir, fileDir} {
		replay := openDir(t, dir)
		got, err := replay.Upload(context.Background(), file)
		if err != nil || got.FileID != "file-1" {
			t.Fatalf("%s: %#v %v", dir, got, err)
		}
		raw, err := replay.Reply("upload")
		if err != nil {
			t.Fatal(err)
		}
		stored, ok := decodeAs[File](raw.Request)
		if !ok {
			t.Fatalf("%s: stored request is not a file: %s", dir, raw.Request)
		}
		key, err := requestKey(stored)
		if err != nil || key != want {
			t.Fatalf("%s: key %q, want %q (%v)", dir, key, want, err)
		}
	}
	other := File{Task: "ai-smile", ContentType: "image/jpeg", Bytes: []byte{0x00}}
	if _, err := openDir(t, fileDir).Upload(context.Background(), other); !errors.As(err, new(*Missing)) {
		t.Fatalf("other bytes: %v", err)
	}
}

func TestAFileResultFetchesItsBytes(t *testing.T) {
	t.Parallel()
	image := []byte{0x89, 0x50, 0x4e, 0x47, 0x07}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "results", "smile.jpg"), image)
	ref := ResultRef{TaskID: "task-1", URL: "https://example.test/r.jpg"}
	inlineDir := filepath.Join(root, "inline")
	fileDir := root
	writeExchange(t, inlineDir, "fetch", ref, Result{TaskID: "task-1", URL: ref.URL, Bytes: image})
	writeExchange(t, fileDir, "fetch", ref, map[string]string{"task_id": "task-1", "url": ref.URL, "file": "results/smile.jpg"})
	for _, dir := range []string{inlineDir, fileDir} {
		got, err := openDir(t, dir).Fetch(context.Background(), ref)
		if err != nil || string(got.Bytes) != string(image) || got.URL != ref.URL || got.TaskID != "task-1" {
			t.Fatalf("%s: %#v %v", dir, got, err)
		}
	}
}

func TestAMissingRecordedFileFailsOpen(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		request  any
		response any
		path     string
	}{
		{"upload-gone", map[string]string{"task": "ai-smile", "content_type": "image/jpeg", "file": "../gone/photo.jpg"}, Slot{FileID: "file-1"}, "../gone/photo.jpg"},
		{"fetch-gone", ResultRef{TaskID: "task-1", URL: "https://example.test/r"}, map[string]string{"task_id": "task-1", "file": "results/gone.jpg"}, "results/gone.jpg"},
	}
	for _, c := range cases {
		dir := t.TempDir()
		writeExchange(t, dir, c.name, c.request, c.response)
		_, err := Open(dir)
		if err == nil || !strings.Contains(err.Error(), c.name+".json") || !strings.Contains(err.Error(), c.path) {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
}

func TestARecordedFileMustBeRelativeAndTyped(t *testing.T) {
	t.Parallel()
	abs := filepath.Join(t.TempDir(), "one.jpg")
	writeFile(t, abs, []byte{0x01})
	cases := []struct {
		name     string
		request  any
		response any
		want     string
	}{
		{"absolute", map[string]string{"task": "ai-smile", "content_type": "image/jpeg", "file": abs}, Slot{FileID: "file-1"}, "not relative"},
		{"untyped-request", map[string]string{"task_id": "task-1", "file": "one.jpg"}, Slot{FileID: "file-1"}, "must be an upload"},
		{"untyped-response", PollRef{Task: "ai-smile", TaskID: "task-1"}, map[string]string{"task": "ai-smile", "file": "one.jpg"}, "must be a result"},
	}
	for _, c := range cases {
		dir := t.TempDir()
		writeExchange(t, dir, c.name, c.request, c.response)
		_, err := Open(dir)
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), c.name+".json") {
			t.Fatalf("%s: %v", c.name, err)
		}
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

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExchange(t *testing.T, dir, key string, request, response any) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
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
