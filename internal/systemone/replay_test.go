package systemone

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAskMissesWhenNothingIsRecorded(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(repoRoot(t), "testdata", "clef")
	replay := openDir(t, dir)
	req := sampleRequest(t, nil)
	_, err := replay.Ask(context.Background(), req)
	var missing *Missing
	if !errors.As(err, &missing) || missing.Key == "" || !strings.Contains(err.Error(), missing.Key) {
		t.Fatalf("%v", err)
	}
	_, err = replay.Ask(context.Background(), req)
	var second *Missing
	if !errors.As(err, &second) || second.Key != missing.Key {
		t.Fatalf("key changed: %v", err)
	}

	other := sampleRequest(t, nil)
	other.Model = ModelClefFlash
	_, err = replay.Ask(context.Background(), other)
	var different *Missing
	if !errors.As(err, &different) || different.Key == missing.Key {
		t.Fatalf("models shared a key: %v", err)
	}
	if _, err := replay.Reply("not-recorded"); err == nil || !strings.Contains(err.Error(), "not-recorded") {
		t.Fatal(err)
	}
}

func TestAskServesARecordedBody(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	state, err := json.Marshal(map[string]string{"occasion": "date"})
	if err != nil {
		t.Fatal(err)
	}
	questions := sampleQuestions(t)
	image := []byte{0xff, 0x00}
	req := Request{
		Model:     ModelClef,
		Images:    []Image{{ContentType: "image/png", Bytes: image}},
		State:     state,
		Questions: questions,
	}
	wireBody, err := json.Marshal(struct {
		Model     string              `json:"model"`
		State     json.RawMessage     `json:"state"`
		Images    []string            `json:"images"`
		Questions map[string]Question `json:"questions"`
	}{
		Model:     ModelClef,
		State:     state,
		Images:    []string{"data:image/png;base64," + base64.StdEncoding.EncodeToString(image)},
		Questions: questions,
	})
	if err != nil {
		t.Fatal(err)
	}
	noul := 0.25
	confidence := 0.4
	recorded := Response{
		Model: ModelClef,
		Answers: map[string]Answer{
			"met": {Type: TypeNoul, Noul: &noul},
			"issue": {
				Type:          TypeChoice,
				Choice:        "collar",
				Probabilities: map[string]float64{"collar": 0.7, "none": 0},
				Confidence:    &confidence,
			},
		},
		Usage: Usage{InputTokens: 10},
	}
	writeExchange(t, dir, "judge-date", map[string]any{
		"method": "POST",
		"path":   "/ai/run/@cf/cloudflare/clef",
		"headers": map[string]string{
			"authorization": "Bearer secret",
		},
		"body": json.RawMessage(wireBody),
	}, map[string]any{"result": recorded, "success": true})

	replay := openDir(t, dir)
	got, err := replay.Ask(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Answers["met"].Noul == nil || *got.Answers["met"].Noul != 0.25 {
		t.Fatalf("noul %#v", got.Answers["met"])
	}
	issue := got.Answers["issue"]
	if issue.Choice != "collar" || issue.Confidence == nil || *issue.Confidence != 0.4 {
		t.Fatalf("%#v", issue)
	}
	none, ok := issue.Probabilities["none"]
	if !ok || none != 0 || issue.Probabilities["collar"] != 0.7 {
		t.Fatalf("%#v", issue.Probabilities)
	}
	if got.Usage.InputTokens != 10 {
		t.Fatalf("usage %#v", got.Usage)
	}

	served, err := replay.Serve("POST", "/ai/run/@cf/cloudflare/clef", wireBody)
	if err != nil {
		t.Fatal(err)
	}
	if served.Key != "judge-date" {
		t.Fatalf("key %q", served.Key)
	}
	if !strings.Contains(string(served.Request), "Bearer secret") {
		t.Fatal("recording dropped the request")
	}
	raw, err := replay.Reply("judge-date")
	if err != nil || raw.Key != served.Key {
		t.Fatal(err)
	}

	req.Model = ModelClefFlash
	_, err = replay.Ask(context.Background(), req)
	var missing *Missing
	if !errors.As(err, &missing) || !strings.Contains(err.Error(), missing.Key) {
		t.Fatal(err)
	}

	writeExchange(t, dir, "judge-date-copy", map[string]any{
		"method": "POST",
		"path":   "/ai/run/@cf/cloudflare/clef",
		"body":   json.RawMessage(wireBody),
	}, map[string]any{"result": recorded})
	replay = openDir(t, dir)
	_, err = replay.Ask(context.Background(), Request{
		Model: ModelClef, Images: []Image{{ContentType: "image/png", Bytes: image}}, State: state, Questions: questions,
	})
	var ambiguous *Ambiguous
	if !errors.As(err, &ambiguous) || !strings.Contains(err.Error(), "judge-date") || !strings.Contains(err.Error(), "judge-date-copy") {
		t.Fatal(err)
	}
}

func TestAFileImageServesLikeItsInlineTwin(t *testing.T) {
	t.Parallel()
	image := []byte{0xff, 0xd8, 0xff, 0x01, 0x02}
	state, err := json.Marshal(map[string]string{"occasion": "date"})
	if err != nil {
		t.Fatal(err)
	}
	questions := sampleQuestions(t)
	noul := 0.5
	confidence := 0.3
	recorded := Response{
		Model: ModelClef,
		Answers: map[string]Answer{
			"met":   {Type: TypeNoul, Noul: &noul},
			"issue": {Type: TypeChoice, Choice: "none", Probabilities: map[string]float64{"collar": 0.2, "none": 0.8}, Confidence: &confidence},
		},
	}
	body := func(images any) json.RawMessage {
		raw, err := json.Marshal(map[string]any{"model": ModelClef, "state": json.RawMessage(state), "images": images, "questions": questions})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	inlineBody := body([]Image{{ContentType: "image/jpeg", Bytes: image}})
	fileBody := body([]map[string]string{{"content_type": "image/jpeg", "file": "../photos/one.jpg"}})

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "photos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "photos", "one.jpg"), image, 0o644); err != nil {
		t.Fatal(err)
	}
	inlineDir := filepath.Join(root, "inline")
	fileDir := filepath.Join(root, "file")
	for _, dir := range []string{inlineDir, fileDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path := "/ai/run/@cf/cloudflare/clef"
	writeExchange(t, inlineDir, "judge", map[string]any{"method": "POST", "path": path, "body": inlineBody}, map[string]any{"result": recorded})
	writeExchange(t, fileDir, "judge", map[string]any{"method": "POST", "path": path, "body": fileBody}, map[string]any{"result": recorded})

	req := Request{Model: ModelClef, Images: []Image{{ContentType: "image/jpeg", Bytes: image}}, State: state, Questions: questions}
	want, err := req.key()
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{inlineDir, fileDir} {
		replay := openDir(t, dir)
		got, err := replay.Ask(context.Background(), req)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		if got.Answers["issue"].Choice != "none" {
			t.Fatalf("%s: %#v", dir, got.Answers)
		}
		served, err := replay.Serve("POST", path, inlineBody)
		if err != nil || served.Key != "judge" {
			t.Fatalf("%s: serve %v", dir, err)
		}
		var env struct {
			Body json.RawMessage `json:"body"`
		}
		if err := json.Unmarshal(served.Request, &env); err != nil {
			t.Fatal(err)
		}
		var stored Request
		if err := json.Unmarshal(env.Body, &stored); err != nil {
			t.Fatal(err)
		}
		key, err := stored.key()
		if err != nil || key != want {
			t.Fatalf("%s: key %q, want %q (%v)", dir, key, want, err)
		}
	}

	other := req
	other.Images = []Image{{ContentType: "image/jpeg", Bytes: []byte{0x00}}}
	_, err = openDir(t, fileDir).Ask(context.Background(), other)
	var missing *Missing
	if !errors.As(err, &missing) {
		t.Fatalf("other bytes: %v", err)
	}
}

func TestAMissingImageFileFailsOpen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body, err := json.Marshal(map[string]any{
		"model":     ModelClef,
		"state":     "date",
		"images":    []map[string]string{{"content_type": "image/jpeg", "file": "../gone/photo.jpg"}},
		"questions": sampleQuestions(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	writeExchange(t, dir, "judge-gone", map[string]any{"method": "POST", "path": "/ai/run/@cf/cloudflare/clef", "body": json.RawMessage(body)}, map[string]any{"result": map[string]any{}})
	_, err = Open(dir)
	if err == nil || !strings.Contains(err.Error(), "judge-gone.json") || !strings.Contains(err.Error(), "../gone/photo.jpg") {
		t.Fatal(err)
	}
}

func TestAskRejectsABadRequestBeforeTheKey(t *testing.T) {
	t.Parallel()
	replay := openDir(t, t.TempDir())
	req := sampleRequest(t, nil)
	req.Questions["met"] = Question{Type: "essay", Instructions: "write"}
	_, err := replay.Ask(context.Background(), req)
	if err == nil || errors.As(err, new(*Missing)) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := replay.Ask(ctx, sampleRequest(t, nil)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestBrokenRecordingFailsOpen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "broken.json") {
		t.Fatal(err)
	}
}

func sampleRequest(t *testing.T, image []byte) Request {
	t.Helper()
	state, err := json.Marshal("date")
	if err != nil {
		t.Fatal(err)
	}
	req := Request{Model: ModelClef, State: state, Questions: sampleQuestions(t)}
	if image != nil {
		req.Images = []Image{{ContentType: "image/png", Bytes: image}}
	}
	return req
}

func sampleQuestions(t *testing.T) map[string]Question {
	t.Helper()
	criteria, err := json.Marshal(map[string]string{"collar": "collar", "none": "none"})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]Question{
		"met":   {Type: TypeNoul, Instructions: "Is the collar quiet?"},
		"issue": {Type: TypeChoice, Instructions: "What is the biggest issue?", Criteria: criteria},
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
