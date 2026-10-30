package youcam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/keel/throttle"
)

// wireStep is one scripted reply the harness serves before it falls back
// to the recordings. The recordings hold no 429 or 5xx replies, so the
// retry tests script their statuses here.
type wireStep struct {
	status     int
	retryAfter string
	body       []byte
}

// captured is one request the harness saw.
type captured struct {
	method      string
	path        string
	auth        string
	contentType string
	body        []byte
}

// harness is the offline YouCam: an httptest server that serves the
// recorded API replies by their method, path and body, builds the one slot
// reply each queued upload needs, and serves the signed-URL legs (the PUT
// and the result fetch) from the typed recordings' bytes. The result links
// the recorded polls carry are rewritten onto this server when served, so
// no request leaves the process. The bearer is checked on every API call,
// and no signed leg may carry one.
type harness struct {
	t   *testing.T
	rep *Replay

	base string

	mu         sync.Mutex
	slots      []Slot
	steps      []wireStep
	step       int
	fetch403   map[string]int
	fetches    map[string]Result
	api        []captured
	legs       []captured
	badAuth    int
	expired403 []byte
}

func newHarness(t *testing.T, rep *Replay) *harness {
	t.Helper()
	h := &harness{
		t:        t,
		rep:      rep,
		fetch403: map[string]int{},
		fetches:  map[string]Result{},
	}
	h.loadFetches(t)
	expired, err := rep.Reply("http-err-expired-result-link")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(expired.Response, &envelope); err != nil || envelope.Body == "" {
		t.Fatalf("expired link recording: %v", err)
	}
	h.expired403 = []byte(envelope.Body)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	h.base = srv.URL
	return h
}

// loadFetches indexes the typed fetch recordings by result path, so a GET
// the client makes to a recorded result link serves the recorded bytes.
func (h *harness) loadFetches(t *testing.T) {
	t.Helper()
	dir := recordingsDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		// The typed recordings carry the seam's Result; the http-* ones
		// carry the wire shape and are served through Serve, not here.
		if entry.IsDir() || !strings.HasSuffix(name, "-fetch.json") || strings.HasPrefix(name, "http-") {
			continue
		}
		rec, err := h.rep.Reply(strings.TrimSuffix(name, ".json"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var request struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(rec.Request, &request); err != nil || request.URL == "" {
			t.Fatalf("%s: %v", name, err)
		}
		result, ok := decodeAs[Result](rec.Response)
		if !ok || len(result.Bytes) == 0 {
			t.Fatalf("%s: recording is not a result with bytes", name)
		}
		u, err := url.Parse(request.URL)
		if err != nil || u.Path == "" {
			t.Fatalf("%s: result url %q", name, request.URL)
		}
		h.fetches[u.Path] = result
	}
}

func (h *harness) enqueueSlot(slot Slot) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.slots = append(h.slots, slot)
}

func (h *harness) expireOnce(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fetch403[path]++
}

func (h *harness) client(t *testing.T) *Client {
	t.Helper()
	return h.clientWithToken(t, testKey)
}

func (h *harness) clientWithToken(t *testing.T, token string) *Client {
	t.Helper()
	client, err := NewClient(ClientConfig{
		BaseURL: h.base,
		Token:   token,
		Throttle: throttle.Config{
			Backoff: time.Millisecond,
			Max:     2 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func (h *harness) apiCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.api)
}

func (h *harness) legCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.legs)
}

// mismatches reports the requests whose auth was wrong: an API call
// without this client's bearer, or a signed leg with any auth at all.
func (h *harness) mismatches() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.badAuth
}

func (h *harness) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxResponseByteCount+1))
	if err != nil {
		http.Error(w, "body", http.StatusBadRequest)
		return
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(r.URL.Path, "/s2s/") {
		h.mu.Lock()
		if auth != "Bearer "+testKey {
			h.badAuth++
		}
		h.api = append(h.api, captured{method: r.Method, path: r.URL.Path, auth: auth, body: body})
		var step *wireStep
		if h.step < len(h.steps) {
			step = &h.steps[h.step]
			h.step++
		}
		var slot Slot
		haveSlot := false
		if step == nil && r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/s2s/v2.0/file") && len(h.slots) > 0 {
			slot = h.slots[0]
			h.slots = h.slots[1:]
			haveSlot = true
		}
		h.mu.Unlock()

		if step != nil {
			if step.retryAfter != "" {
				w.Header().Set("Retry-After", step.retryAfter)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(step.status)
			_, _ = w.Write(step.body)
			return
		}
		if haveSlot {
			h.serveSlot(w, body, slot)
			return
		}
		h.serveRecording(w, r.Method, r.URL.Path, body)
		return
	}

	h.mu.Lock()
	if auth != "" {
		h.badAuth++ // the signed legs carry no Authorization
	}
	h.legs = append(h.legs, captured{method: r.Method, path: r.URL.Path, auth: auth, contentType: r.Header.Get("Content-Type"), body: body})
	left := h.fetch403[r.URL.Path]
	if left > 0 {
		h.fetch403[r.URL.Path] = left - 1
	}
	expired := h.expired403
	h.mu.Unlock()

	switch {
	case r.Method == http.MethodPut:
		w.Header().Set("ETag", `"recorded"`)
		w.WriteHeader(http.StatusOK)
	case left > 0:
		// The recorded refusal: the link outlived its two hours.
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write(expired)
	default:
		result, ok := h.fetches[r.URL.Path]
		if !ok {
			http.Error(w, "no recorded result for "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", http.DetectContentType(result.Bytes))
		_, _ = w.Write(result.Bytes)
	}
}

// serveSlot builds the one-file slot reply around a queued recorded slot.
// The recorded slot body names the sample photo; this client derives its
// own name, so the slot leg is built here instead of matched, and the
// file id is the recorded one.
func (h *harness) serveSlot(w http.ResponseWriter, body []byte, slot Slot) {
	var request slotBody
	_ = json.Unmarshal(body, &request)
	contentType := "image/jpeg"
	fileName := "recorded.jpg"
	fileSize := 0
	if len(request.Files) == 1 {
		if request.Files[0].ContentType != "" {
			contentType = request.Files[0].ContentType
		}
		if request.Files[0].FileName != "" {
			fileName = request.Files[0].FileName
		}
		fileSize = request.Files[0].FileSize
	}
	u, err := url.Parse(slot.URL)
	if err != nil || u.Path == "" {
		h.t.Errorf("slot url %q", slot.URL)
		http.Error(w, "bad slot url", http.StatusInternalServerError)
		return
	}
	reply := map[string]any{
		"status": 200,
		"data": map[string]any{
			"files": []any{map[string]any{
				"content_type": contentType,
				"file_name":    fileName,
				"file_id":      slot.FileID,
				"requests": []any{map[string]any{
					"method":  "PUT",
					"url":     h.base + u.Path,
					"headers": map[string]string{"Content-Type": contentType, "Content-Length": strconv.Itoa(fileSize)},
				}},
			}},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(reply)
}

// serveRecording finds the recorded reply for one API call and serves it.
// A recorded poll's result link is rewritten onto this server, so the
// fetch leg lands here instead of on the provider's bucket.
func (h *harness) serveRecording(w http.ResponseWriter, method, path string, body []byte) {
	raw := json.RawMessage(nil)
	if len(body) > 0 {
		raw = json.RawMessage(body)
	}
	rec, err := h.rep.Serve(method, path, raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	var envelope struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(rec.Response, &envelope); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rewritten, ok := h.rewriteResultLink(envelope.Body); ok {
		envelope.Body = rewritten
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(envelope.Status)
	_, _ = w.Write(envelope.Body)
}

// rewriteResultLink points the recorded result url at this server.
func (h *harness) rewriteResultLink(body json.RawMessage) (json.RawMessage, bool) {
	if len(body) == 0 {
		return nil, false
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, false
	}
	data, ok := decoded["data"].(map[string]any)
	if !ok {
		return nil, false
	}
	results, ok := data["results"].(map[string]any)
	if !ok {
		return nil, false
	}
	link, ok := results["url"].(string)
	if !ok || link == "" {
		return nil, false
	}
	u, err := url.Parse(link)
	if err != nil || u.Path == "" {
		return nil, false
	}
	results["url"] = h.base + u.Path
	raw, err := json.Marshal(decoded)
	if err != nil {
		return nil, false
	}
	return raw, true
}

// TestClientRunsEveryFlowOverTheWire replays every recorded flow through
// the live client over HTTP: the slot, the PUT, the create, the poll and
// the fetch all hit their recordings, the bearer rides every API call,
// and no signed leg carries it. This is the wire-half of the success
// class of the done-when.
func TestClientRunsEveryFlowOverTheWire(t *testing.T) {
	t.Parallel()
	rep := openRecordings(t)
	for _, flow := range flows {
		t.Run(flow.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, rep)
			for _, key := range flow.uploads {
				h.enqueueSlot(recordedSlot(t, rep, key))
			}
			client := h.client(t)
			ctx := context.Background()

			for _, key := range flow.uploads {
				file := recordedFile(t, rep, key)
				slot, err := client.Upload(ctx, file)
				if err != nil {
					t.Fatalf("%s upload: %v", key, err)
				}
				if slot.FileID != recordedSlot(t, rep, key).FileID {
					t.Fatalf("%s file id %q", key, slot.FileID)
				}
			}

			created, err := client.Create(ctx, recordedTask(t, rep, flow.create))
			if err != nil {
				t.Fatalf("%s create: %v", flow.name, err)
			}
			if created.TaskID != recordedCreated(t, rep, flow.create).TaskID {
				t.Fatalf("task id %q", created.TaskID)
			}

			st, err := client.Poll(ctx, PollRef{Task: flow.task, TaskID: created.TaskID})
			if err != nil {
				t.Fatalf("%s poll: %v", flow.name, err)
			}
			if st.Status != StatusSuccess || st.TaskID != created.TaskID {
				t.Fatalf("status %+v", st)
			}
			if flow.fetch == "" {
				// The analysis poll carries the face shape and no link.
				if st.FaceShape != "Oval" || st.ResultURL != "" {
					t.Fatalf("analysis status %+v, want the face shape and no link", st)
				}
			} else {
				want := recordedStatus(t, rep, flow.poll)
				if gotPath, wantPath := linkPath(t, st.ResultURL), linkPath(t, want.ResultURL); gotPath != wantPath {
					t.Fatalf("result link path %q, want %q", gotPath, wantPath)
				}
				result, err := client.Fetch(ctx, ResultRef{TaskID: created.TaskID, URL: st.ResultURL})
				if err != nil {
					t.Fatalf("%s fetch: %v", flow.name, err)
				}
				recorded := recordedResult(t, rep, flow.fetch)
				if !bytes.Equal(result.Bytes, recorded.Bytes) {
					t.Fatalf("fetched %d bytes, want the recorded %d", len(result.Bytes), len(recorded.Bytes))
				}
			}

			if h.mismatches() != 0 {
				t.Fatalf("%d calls carried the wrong auth", h.mismatches())
			}
			wantLegs := len(flow.uploads)
			if flow.fetch != "" {
				wantLegs++
			}
			if legs := h.legCount(); legs != wantLegs {
				t.Fatalf("signed legs %d, want %d", legs, wantLegs)
			}
		})
	}
}

func linkPath(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" {
		t.Fatalf("link %q", raw)
	}
	return u.Path
}

// TestClientReplaysEveryRecordedRefusal drives each error T0.5 recorded
// through the live client and pins the typed shape it comes back as. This
// is the error class of the done-when, at the wire.
func TestClientReplaysEveryRecordedRefusal(t *testing.T) {
	t.Parallel()
	rep := openRecordings(t)

	t.Run("wrong key", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, rep)
		h.steps = []wireStep{{status: 401, body: recordedWireBody(t, rep, "http-err-wrong-key")}}
		client := h.clientWithToken(t, "a placeholder that is not the key")
		_, err := client.Upload(t.Context(), recordedFile(t, rep, "smile-upload-src"))
		var refused *HTTPError
		if !errors.As(err, &refused) || refused.Code != 401 || refused.Name != "InvalidApiKey" {
			t.Fatalf("%v, want 401 InvalidApiKey", err)
		}
		if !strings.Contains(refused.Note, "isn't recognized") || strings.Contains(refused.Error(), "a placeholder") {
			t.Fatalf("note %q: the key text leaked or the note was lost", refused.Note)
		}
		if h.apiCount() != 1 {
			t.Fatalf("api calls %d, want one refused attempt", h.apiCount())
		}
	})

	createRefusals := []struct {
		name   string
		task   Task
		code   int
		errNam string
		note   string
	}{
		{"unknown file id", Task{Name: "ai-smile", Payload: json.RawMessage(`{"src_file_id":"not-a-file-id","expression_type":"closed_mouth_smile"}`)}, 400, "InvalidParameters", "One or more parameters in this request are invalid."},
		{"bad parameter", Task{Name: "ai-smile", Payload: json.RawMessage(`{"src_file_id":"not-a-file-id","expression_type":"grin"}`)}, 400, "InvalidParameters", "expression_type is not one of the accepted values."},
		{"unknown task", Task{Name: "no-such-task", Payload: json.RawMessage(`{"src_file_id":"not-a-file"}`)}, 404, "NotFound", "This endpoint doesn't exist."},
	}
	for _, tc := range createRefusals {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, rep)
			client := h.client(t)
			_, err := client.Create(t.Context(), tc.task)
			var refused *HTTPError
			if !errors.As(err, &refused) || refused.Code != tc.code || refused.Name != tc.errNam {
				t.Fatalf("%v, want %d %s", err, tc.code, tc.errNam)
			}
			if refused.Note != tc.note {
				t.Fatalf("note %q, want %q", refused.Note, tc.note)
			}
			if !refused.Retryable() {
				// A refusal is final: exactly one attempt went out.
			} else {
				t.Fatalf("%d %s read as retryable", refused.Code, refused.Name)
			}
			if h.apiCount() != 1 {
				t.Fatalf("api calls %d, want the one refused attempt", h.apiCount())
			}
		})
	}

	t.Run("unknown task id", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, rep)
		client := h.client(t)
		_, err := client.Poll(t.Context(), PollRef{Task: "ai-smile", TaskID: "not-a-real-task-id"})
		var refused *HTTPError
		if !errors.As(err, &refused) || refused.Code != 400 || refused.Name != "InvalidTaskId" {
			t.Fatalf("%v, want 400 InvalidTaskId", err)
		}
		if h.apiCount() != 1 {
			t.Fatalf("api calls %d, want one", h.apiCount())
		}
	})

	engineRefusals := []struct {
		name   string
		create string
		want   *Error
	}{
		{"no face", "err-no-face-create", &Error{Kind: KindNoFace, Detail: "error_no_face"}},
		{"bad file", "err-bad-file-create", &Error{Kind: KindBadFile, Detail: "error_decode_image"}},
		{"too small", "err-too-small-create", &Error{Kind: KindBadFile, Detail: "error_below_min_image_size"}},
		{"file not in storage", "err-not-uploaded-create", &Error{Kind: KindExpired, Detail: "error_download_image"}},
	}
	for _, tc := range engineRefusals {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, rep)
			client := h.client(t)
			created := recordedCreated(t, rep, tc.create)
			_, err := client.Poll(t.Context(), PollRef{Task: pollTask(t, rep, tc.create), TaskID: created.TaskID})
			if !errors.Is(err, tc.want) {
				t.Fatalf("%v, want %s", err, tc.want.Kind)
			}
			var ye *Error
			if !errors.As(err, &ye) || ye.Detail != tc.want.Detail {
				t.Fatalf("%v, want detail %s", err, tc.want.Detail)
			}
		})
	}

	t.Run("expired result link", func(t *testing.T) {
		t.Parallel()
		rep := openRecordings(t)
		h := newHarness(t, rep)
		client := h.client(t)
		poll := recordedStatus(t, rep, "smile-poll")
		h.expireOnce(linkPath(t, poll.ResultURL))

		_, err := client.Fetch(t.Context(), ResultRef{TaskID: poll.TaskID, URL: h.base + linkPath(t, poll.ResultURL)})
		if !errors.Is(err, ErrExpired) {
			t.Fatalf("%v, want ErrExpired", err)
		}
		// The fresh link, which a free re-poll of the finished task hands
		// out, fetches the same picture.
		result, err := client.Fetch(t.Context(), ResultRef{TaskID: poll.TaskID, URL: h.base + linkPath(t, poll.ResultURL)})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(result.Bytes, recordedResult(t, rep, "smile-fetch").Bytes) {
			t.Fatal("the fresh link did not serve the recorded picture")
		}
	})
}

// pollTask names the task a create recording belongs to.
func pollTask(t *testing.T, rep *Replay, create string) string {
	t.Helper()
	return recordedTask(t, rep, create).Name
}

// TestClientRetriesOnlyWhatAWaitCanFix scripts the faults the recordings
// never hold: a server fault is retried, a rate limit follows the wait the
// provider names, and a refusal is not retried at all.
func TestClientRetriesOnlyWhatAWaitCanFix(t *testing.T) {
	t.Parallel()
	rep := openRecordings(t)

	t.Run("server fault retried", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, rep)
		h.steps = []wireStep{{status: 500, body: []byte("boom")}}
		client := h.client(t)
		created, err := client.Create(t.Context(), recordedTask(t, rep, "smile-create"))
		if err != nil {
			t.Fatal(err)
		}
		if created.TaskID != recordedCreated(t, rep, "smile-create").TaskID {
			t.Fatalf("task id %q", created.TaskID)
		}
		if h.apiCount() != 2 {
			t.Fatalf("api calls %d, want the fault and the retry", h.apiCount())
		}
	})

	t.Run("rate limit follows the hint", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, rep)
		h.steps = []wireStep{{status: 429, retryAfter: "1", body: []byte("slow down")}}
		// A client whose Max does not clamp the one-second hint away.
		client, err := NewClient(ClientConfig{
			BaseURL: h.base,
			Token:   testKey,
			Throttle: throttle.Config{
				Backoff: time.Millisecond,
				Max:     5 * time.Second,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		_, err = client.Create(t.Context(), recordedTask(t, rep, "smile-create"))
		if err != nil {
			t.Fatal(err)
		}
		waited := time.Since(started)
		if waited < time.Second {
			t.Fatalf("waited %s, want at least the one second the provider named", waited)
		}
		if waited > 5*time.Second {
			t.Fatalf("waited %s, want the hint plus a little spread", waited)
		}
		if h.apiCount() != 2 {
			t.Fatalf("api calls %d, want the refusal and the retry", h.apiCount())
		}
	})

	t.Run("rate limit without a hint uses the backoff", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, rep)
		h.steps = []wireStep{{status: 429, body: []byte("slow down")}}
		client := h.client(t)
		started := time.Now()
		if _, err := client.Create(t.Context(), recordedTask(t, rep, "smile-create")); err != nil {
			t.Fatal(err)
		}
		if waited := time.Since(started); waited > time.Second {
			t.Fatalf("waited %s, want the test's own millisecond backoff", waited)
		}
	})

	t.Run("classification", func(t *testing.T) {
		t.Parallel()
		if !wireRetryable(&HTTPError{Code: 429}) || !wireRetryable(&HTTPError{Code: 503}) {
			t.Fatal("a rate limit and a server fault are retryable")
		}
		if wireRetryable(&HTTPError{Code: 400}) || wireRetryable(&HTTPError{Code: 401}) || wireRetryable(&HTTPError{Code: 404}) {
			t.Fatal("a refusal is not retryable")
		}
		if wireRetryable(ErrExpired) || wireRetryable(&Error{Kind: KindNoFace}) {
			t.Fatal("a provider verdict is not retryable")
		}
		if wireRetryable(context.DeadlineExceeded) || wireRetryable(context.Canceled) {
			t.Fatal("the caller's own deadline is not retryable")
		}
		if !transientFault(&HTTPError{Code: 500}) {
			t.Fatal("a server fault is transient")
		}
		if transientFault(&HTTPError{Code: 429}) {
			t.Fatal("a rate limit is not transient, it draws its own budget")
		}
	})
}

// TestClientRefusesABadBuild pins the constructor checks.
func TestClientRefusesABadBuild(t *testing.T) {
	t.Parallel()
	if _, err := NewClient(ClientConfig{}); err == nil || !strings.Contains(err.Error(), "api key") {
		t.Fatalf("%v", err)
	}
	for name, base := range map[string]string{
		"path":      "https://yce-api-01.makeupar.com/api",
		"query":     "https://yce-api-01.makeupar.com/?x=1",
		"not-http":  "ftp://yce-api-01.makeupar.com",
		"no-host":   "//path/only",
		"not-valid": "://broken",
	} {
		if _, err := NewClient(ClientConfig{Token: testKey, BaseURL: base}); err == nil {
			t.Fatalf("%s base accepted", name)
		}
	}
	client, err := NewClient(ClientConfig{Token: testKey})
	if err != nil {
		t.Fatal(err)
	}
	if client.base.String() != "https://yce-api-01.makeupar.com" {
		t.Fatalf("default base %q", client.base.String())
	}
	if versionFor("hair-transfer") != "v2.1" || versionFor("ai-smile") != "v2.0" {
		t.Fatal("version routing")
	}
}
