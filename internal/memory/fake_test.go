package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestReplayServesEveryLamboRecording(t *testing.T) {
	t.Parallel()
	dir := lamboDir(t)
	fake := openDir(t, dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			names = append(names, entry.Name())
		}
	}
	if len(names) != 21 {
		t.Fatalf("%d recordings, want 21", len(names))
	}

	byContent := map[string][]string{}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		var env struct {
			Exchange string          `json:"exchange"`
			Request  json.RawMessage `json:"request"`
			Response json.RawMessage `json:"response"`
		}
		if err := json.Unmarshal(data, &env); err != nil {
			t.Fatal(err)
		}
		if env.Exchange != strings.TrimSuffix(name, ".json") {
			t.Fatalf("%s exchange %q", name, env.Exchange)
		}
		got, err := fake.Reply(env.Exchange)
		if err != nil {
			t.Fatalf("reply %s: %v", env.Exchange, err)
		}
		if got.Key != env.Exchange {
			t.Fatalf("key %q", got.Key)
		}
		jsonEqual(t, got.Response, env.Response)

		var httpReq struct {
			Method string          `json:"method"`
			Path   string          `json:"path"`
			Body   json.RawMessage `json:"body"`
		}
		if err := json.Unmarshal(env.Request, &httpReq); err != nil {
			t.Fatal(err)
		}
		key, err := ContentKey(httpReq.Method, httpReq.Path, httpReq.Body)
		if err != nil {
			t.Fatal(err)
		}
		byContent[key] = append(byContent[key], env.Exchange)
	}

	served := 0
	for key, exchanges := range byContent {
		var sample struct {
			Method string          `json:"method"`
			Path   string          `json:"path"`
			Body   json.RawMessage `json:"body"`
		}
		rec, err := fake.Reply(exchanges[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(rec.Request, &sample); err != nil {
			t.Fatal(err)
		}
		got, err := fake.Serve(sample.Method, sample.Path, sample.Body)
		if len(exchanges) == 1 {
			if err != nil {
				t.Fatalf("serve %s: %v", exchanges[0], err)
			}
			want, err := fake.Reply(exchanges[0])
			if err != nil {
				t.Fatal(err)
			}
			jsonEqual(t, got.Response, want.Response)
			served++
			continue
		}
		var ambiguous *Ambiguous
		if !errors.As(err, &ambiguous) || ambiguous.Key != key {
			t.Fatalf("shared key %s: %v", key, err)
		}
		for _, exchange := range exchanges {
			if !strings.Contains(err.Error(), exchange) {
				t.Fatalf("error %q omits %s", err, exchange)
			}
		}
		if got.Response != nil {
			t.Fatalf("ambiguous serve returned a response")
		}
	}
	if served != 19 {
		t.Fatalf("served %d unique requests, want 19", served)
	}
}

func TestServeHitsFamilyFunctionAndMissesLoudly(t *testing.T) {
	t.Parallel()
	fake := openDir(t, lamboDir(t))

	rec, err := fake.Reply("11-recall-a-family-function")
	if err != nil {
		t.Fatal(err)
	}
	var httpReq struct {
		Method string          `json:"method"`
		Path   string          `json:"path"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(rec.Request, &httpReq); err != nil {
		t.Fatal(err)
	}
	pretty, err := json.MarshalIndent(json.RawMessage(httpReq.Body), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got, err := fake.Serve(httpReq.Method, httpReq.Path, pretty)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got.Response), "gold jhumkas") {
		t.Fatal("hit did not return the recorded recall")
	}
	if strings.Contains(string(got.Response), "silver studs") {
		t.Fatal("hit returned the other session")
	}

	missBody := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"lambo_recall","arguments":{"query":"no such occasion"}}}`)
	key, err := ContentKey("POST", "/mcp/s/dc-u-probe-a", missBody)
	if err != nil {
		t.Fatal(err)
	}
	got, err = fake.Serve("POST", "/mcp/s/dc-u-probe-a", missBody)
	var missing *Missing
	if !errors.As(err, &missing) || missing.Key != key || !strings.Contains(err.Error(), key) {
		t.Fatalf("miss %v key %q", err, key)
	}
	if got.Response != nil || strings.Contains(err.Error(), "jhumkas") {
		t.Fatal("miss returned a recording")
	}

	again, err := ContentKey("POST", "/mcp/s/dc-u-probe-a", missBody)
	if err != nil || again != key {
		t.Fatalf("key changed %q %q", key, again)
	}
}

func TestRepeatedEraseIsNotChosen(t *testing.T) {
	t.Parallel()
	fake := openDir(t, lamboDir(t))
	first, err := fake.Reply("15-erase-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := fake.Reply("18-erase-a-again")
	if err != nil {
		t.Fatal(err)
	}
	if jsonEqualBool(first.Response, second.Response) {
		t.Fatal("erase replies collapsed")
	}
	var httpReq struct {
		Method string          `json:"method"`
		Path   string          `json:"path"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(first.Request, &httpReq); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.Serve(httpReq.Method, httpReq.Path, httpReq.Body); err == nil {
		t.Fatal("serve chose one erase")
	}

	ctx := context.Background()
	if _, err := fake.Erase(ctx, "dc-u-probe-a"); err == nil {
		t.Fatal("erase chose one recording")
	} else if !strings.Contains(err.Error(), "15-erase-a") || !strings.Contains(err.Error(), "18-erase-a-again") {
		t.Fatal(err)
	}

	got, err := fake.Erase(ctx, "dc-u-probe-b")
	if err != nil {
		t.Fatal(err)
	}
	if got.Session != "dc-u-probe-b" || got.AlreadyAbsent || got.Removed.Concepts != 2 || got.Removed.Vectors != 2 {
		t.Fatalf("%#v", got)
	}
}

func TestRecallAndRememberServeTheRecording(t *testing.T) {
	t.Parallel()
	fake := openDir(t, lamboDir(t))
	ctx := context.Background()

	got, err := fake.Recall(ctx, "dc-u-probe-a", "silver studs")
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, hit := range got.Hits {
		texts = append(texts, hit.Content)
		if hit.ConceptType != "Observation" || hit.Score == 0 || hit.NodeID == "" {
			t.Fatalf("%#v", hit)
		}
	}
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, "gold jhumkas") || !strings.Contains(joined, "red lip") {
		t.Fatalf("%q", joined)
	}
	if strings.Contains(joined, "silver studs") || strings.Contains(joined, "braid") {
		t.Fatalf("other session leaked: %q", joined)
	}

	other, err := fake.Recall(ctx, "dc-u-probe-b", "gold jhumkas")
	if err != nil {
		t.Fatal(err)
	}
	var otherText string
	for _, hit := range other.Hits {
		otherText += hit.Content + "\n"
	}
	if !strings.Contains(otherText, "silver studs") || strings.Contains(otherText, "jhumkas") {
		t.Fatalf("%q", otherText)
	}

	ambiguous, err := fake.Recall(ctx, "dc-u-probe-a", "family function")
	var amb *Ambiguous
	if !errors.As(err, &amb) || len(ambiguous.Hits) != 0 {
		t.Fatalf("recall %#v err %v", ambiguous, err)
	}
	if !strings.Contains(err.Error(), "11-recall-a-family-function") || !strings.Contains(err.Error(), "16-recall-a-after-erase") {
		t.Fatal(err)
	}

	kept := []Concept{
		{Content: "Family function kept the gold jhumkas.", ConceptType: ConceptObservation},
		{Content: "Family function dismissed the red lip.", ConceptType: ConceptObservation},
	}
	remembered, err := fake.Remember(ctx, "dc-u-probe-a", kept)
	if err != nil {
		t.Fatal(err)
	}
	if remembered.State != "pending" || remembered.Receipt == "" || !strings.Contains(remembered.Summary, "2 concept") {
		t.Fatalf("%#v", remembered)
	}

	swapped := []Concept{kept[1], kept[0]}
	if _, err := fake.Remember(ctx, "dc-u-probe-a", swapped); err == nil {
		t.Fatal("swapped concepts hit")
	}
	miss, err := fake.Remember(ctx, "dc-u-probe-b", kept)
	var missing *Missing
	if !errors.As(err, &missing) || missing.Key == "" || !strings.Contains(err.Error(), missing.Key) || miss.Receipt != "" {
		t.Fatalf("remember %#v err %v", miss, err)
	}

	unknown, err := fake.Recall(ctx, "dc-u-probe-a", "no such occasion")
	if !errors.As(err, &missing) || len(unknown.Hits) != 0 || !strings.Contains(err.Error(), missing.Key) {
		t.Fatalf("recall %#v err %v", unknown, err)
	}
}

func TestErasedRecordingIsNotEmptyHistory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeExchange(t, dir, "temp-erased", map[string]any{
		"method": "POST",
		"path":   "/mcp/s/dc-u-temp",
		"body": map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name": "lambo_recall",
				"arguments": map[string]any{
					"query": "temp occasion",
				},
			},
		},
	}, map[string]any{
		"status": 200,
		"body": map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"error": map[string]any{
				"code":    ErasedCode,
				"message": "erased",
			},
		},
	})
	fake := openDir(t, dir)
	got, err := fake.Recall(context.Background(), "dc-u-temp", "temp occasion")
	if !errors.Is(err, ErrErased) || len(got.Hits) != 0 {
		t.Fatalf("%#v %v", got, err)
	}
}

func TestMissingKeyAndBadDirectory(t *testing.T) {
	t.Parallel()
	fake := openDir(t, lamboDir(t))
	if _, err := fake.Reply("not-a-recording"); err == nil || !strings.Contains(err.Error(), "not-a-recording") {
		t.Fatal(err)
	}

	if _, err := fake.Recall(context.Background(), "probe-a", "family function"); err == nil || errors.As(err, new(*Missing)) {
		t.Fatal(err)
	}
	id, err := Session("probe-a")
	if err != nil || id != "dc-u-probe-a" {
		t.Fatalf("%q %v", id, err)
	}
	if _, err := Session("dc-u-probe-a"); err == nil {
		t.Fatal("prefix doubled")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fake.Recall(ctx, "dc-u-probe-a", "silver studs"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "broken.json") {
		t.Fatal(err)
	}

	dup := t.TempDir()
	body := map[string]any{"method": "POST", "path": "/mcp/s/dc-u-temp", "body": map[string]any{"ok": true}}
	resp := map[string]any{"status": 200, "body": map[string]any{"ok": true}}
	writeExchange(t, dup, "same-key", body, resp)
	payload, err := json.Marshal(map[string]any{
		"exchange": "same-key",
		"request":  body,
		"response": resp,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dup, "other.json"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dup); err == nil || !strings.Contains(err.Error(), "same-key") {
		t.Fatal(err)
	}
}

func openDir(t *testing.T, dir string) *Fake {
	t.Helper()
	fake, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return fake
}

func lamboDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "lambo")
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

func jsonEqual(t *testing.T, got, want json.RawMessage) {
	t.Helper()
	if !jsonEqualBool(got, want) {
		t.Fatalf("json\n got %s\nwant %s", got, want)
	}
}

func jsonEqualBool(got, want json.RawMessage) bool {
	var left, right any
	if json.Unmarshal(got, &left) != nil || json.Unmarshal(want, &right) != nil {
		return false
	}
	return reflect.DeepEqual(left, right)
}

func TestContentKeyIgnoresHeaderMaterial(t *testing.T) {
	t.Parallel()
	body := json.RawMessage(`{"b":1,"a":2}`)
	reordered := json.RawMessage(`{"a":2,"b":1}`)
	left, err := ContentKey("POST", "/mcp/s/dc-u-probe-a", body)
	if err != nil {
		t.Fatal(err)
	}
	right, err := ContentKey("POST", "/mcp/s/dc-u-probe-a", reordered)
	if err != nil {
		t.Fatal(err)
	}
	if left == "" || left != right {
		t.Fatalf("%q %q", left, right)
	}
	other, err := ContentKey("POST", "/mcp/s/dc-u-probe-b", body)
	if err != nil || other == left {
		t.Fatal(other)
	}
	if bytes.Contains([]byte(left), []byte("Bearer")) {
		t.Fatal(left)
	}
}
