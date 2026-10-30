package systemone

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/throttle"
)

const (
	workersTestToken   = "workers-ai-test-token"
	workersTestAccount = "dresscode-test-account"
)

// failErr fails the test for an error that must not carry the key.
func failErr(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("nil error")
	}
	if strings.Contains(err.Error(), workersTestToken) || strings.Contains(err.Error(), openrouterTestToken) {
		t.Fatal("error contains a key")
	}
	t.Fatal(err)
}

func clefDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "testdata", "clef")
}

func newTestMeter(t *testing.T, limit cost.Price) (*cost.Meter, *cost.Budget, *cost.Ledger) {
	t.Helper()
	budget, err := cost.NewBudget(limit)
	if err != nil {
		t.Fatal(err)
	}
	ledger := cost.NewLedger()
	meter, err := cost.NewMeter(budget, ledger)
	if err != nil {
		t.Fatal(err)
	}
	return meter, budget, ledger
}

// wireStep is one synthesized reply the harness serves before it falls
// back to the recordings. The recordings hold no error replies, so the
// retry tests script their statuses here, the way the memory tests
// script theirs.
type wireStep struct {
	status     int
	retryAfter string
	body       string
}

type capturedCall struct {
	Path          string
	Authorization string
	Body          []byte
}

// clefReplay is the offline Workers AI. A request whose body and path
// match a recording is served that recording's status and body; a
// scripted step is served first. No network leaves the process.
type clefReplay struct {
	t      *testing.T
	rep    *Replay
	script []wireStep

	mu         sync.Mutex
	step       int
	calls      []capturedCall
	mismatches int
	url        string
}

func newClefReplay(t *testing.T, dir string, script []wireStep) (*clefReplay, *Replay) {
	t.Helper()
	rep := openDir(t, dir)
	s := &clefReplay{t: t, rep: rep, script: script}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	s.url = srv.URL
	return s, rep
}

func (s *clefReplay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes+1))
	if err != nil || int64(len(body)) > maxRequestBodyBytes {
		http.Error(w, "body", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	authorized := r.Header.Get("Authorization") == "Bearer "+workersTestToken
	if !authorized {
		s.mismatches++
	}
	s.calls = append(s.calls, capturedCall{
		Path:          r.URL.Path,
		Authorization: r.Header.Get("Authorization"),
		Body:          append([]byte(nil), body...),
	})
	var step *wireStep
	if s.step < len(s.script) {
		step = &s.script[s.step]
		s.step++
	}
	s.mu.Unlock()
	if !authorized {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if step != nil {
		if step.retryAfter != "" {
			w.Header().Set("Retry-After", step.retryAfter)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(step.status)
		_, _ = io.WriteString(w, step.body)
		return
	}
	path := r.URL.Path
	if i := strings.Index(path, "/ai/run/"); i >= 0 {
		path = path[i:]
	}
	rec, err := s.rep.Serve(http.MethodPost, path, body)
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(envelope.Status)
	_, _ = w.Write(envelope.Body)
}

func (s *clefReplay) lastCall() capturedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.calls) == 0 {
		s.t.Fatal("no call was made")
	}
	return s.calls[len(s.calls)-1]
}

func (s *clefReplay) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *clefReplay) unauthorized() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mismatches
}

func newWorkersTestClient(t *testing.T, base string, meter *cost.Meter, pacer throttle.Config) *WorkersAI {
	t.Helper()
	client, err := NewWorkersAI(WorkersAIConfig{
		BaseURL:   base,
		AccountID: workersTestAccount,
		Token:     workersTestToken,
		Meter:     meter,
		Throttle:  pacer,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// recordedExchanges lists every recording in dir by its exchange id.
func recordedExchanges(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var envelope struct {
			Exchange string `json:"exchange"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil || envelope.Exchange == "" {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		names = append(names, envelope.Exchange)
	}
	sort.Strings(names)
	return names
}

// recordedRequest rebuilds the Request a recording answers. Reply already
// read the recorded image files into bytes, so the request carries the
// same bytes the live client sent.
func recordedRequest(t *testing.T, rec Recording) Request {
	t.Helper()
	var envelope struct {
		Method string          `json:"method"`
		Path   string          `json:"path"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(rec.Request, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Method != http.MethodPost || !strings.Contains(envelope.Path, runPath("")) {
		t.Fatalf("recording %s is not a clef POST: %s %s", rec.Key, envelope.Method, envelope.Path)
	}
	var req Request
	if err := json.Unmarshal(envelope.Body, &req); err != nil {
		t.Fatalf("%s: %v", rec.Key, err)
	}
	return req
}

type recordedResult struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

// recordedAnswers parses the probabilities a recording holds.
func recordedAnswers(t *testing.T, rec Recording) (map[string]Answer, Usage) {
	t.Helper()
	var envelope recordedResult
	if err := json.Unmarshal(rec.Response, &envelope); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Result struct {
			Answers map[string]Answer `json:"answers"`
			Usage   Usage             `json:"usage"`
		} `json:"result"`
	}
	if err := json.Unmarshal(envelope.Body, &body); err != nil {
		t.Fatal(err)
	}
	return body.Result.Answers, body.Result.Usage
}

// assertSameAnswers requires the decoded response to carry the recording's
// probabilities exactly: every answer present, every number identical.
func assertSameAnswers(t *testing.T, exchange string, got Response, want map[string]Answer, wantUsage Usage) {
	t.Helper()
	if len(got.Answers) != len(want) {
		t.Fatalf("%s: %d answers, want %d", exchange, len(got.Answers), len(want))
	}
	for id, wantAnswer := range want {
		gotAnswer, ok := got.Answers[id]
		if !ok {
			t.Fatalf("%s: answer %q is missing", exchange, id)
		}
		if gotAnswer.Type != wantAnswer.Type {
			t.Fatalf("%s: %s type %q, want %q", exchange, id, gotAnswer.Type, wantAnswer.Type)
		}
		switch wantAnswer.Type {
		case TypeNoul:
			if (gotAnswer.Noul == nil) != (wantAnswer.Noul == nil) || (gotAnswer.Noul != nil && *gotAnswer.Noul != *wantAnswer.Noul) {
				t.Fatalf("%s: %s noul %v, want %v", exchange, id, gotAnswer.Noul, wantAnswer.Noul)
			}
		case TypeChoice:
			if gotAnswer.Choice != wantAnswer.Choice {
				t.Fatalf("%s: %s choice %q, want %q", exchange, id, gotAnswer.Choice, wantAnswer.Choice)
			}
		case TypeScore:
			if (gotAnswer.Score == nil) != (wantAnswer.Score == nil) || (gotAnswer.Score != nil && *gotAnswer.Score != *wantAnswer.Score) {
				t.Fatalf("%s: %s score %v, want %v", exchange, id, gotAnswer.Score, wantAnswer.Score)
			}
		}
		if len(gotAnswer.Probabilities) != len(wantAnswer.Probabilities) {
			t.Fatalf("%s: %s %d probabilities, want %d", exchange, id, len(gotAnswer.Probabilities), len(wantAnswer.Probabilities))
		}
		for option, wantProbability := range wantAnswer.Probabilities {
			if gotAnswer.Probabilities[option] != wantProbability {
				t.Fatalf("%s: %s probability %q is %v, want %v", exchange, id, option, gotAnswer.Probabilities[option], wantProbability)
			}
		}
		if (gotAnswer.Confidence == nil) != (wantAnswer.Confidence == nil) || (gotAnswer.Confidence != nil && *gotAnswer.Confidence != *wantAnswer.Confidence) {
			t.Fatalf("%s: %s confidence %v, want %v", exchange, id, gotAnswer.Confidence, wantAnswer.Confidence)
		}
	}
	if got.Usage != wantUsage {
		t.Fatalf("%s: usage %+v, want %+v", exchange, got.Usage, wantUsage)
	}
}

// replayEveryRecordingIn points the client at one recording folder and
// asks every recorded exchange through the real client, over HTTP, and
// checks each reply decodes to the probabilities the recording holds.
// bothModels requires the folder to cover clef and clef-flash.
func replayEveryRecordingIn(t *testing.T, rel string, bothModels bool) {
	t.Helper()
	dir := filepath.Join(clefDir(t), rel)
	wire, rep := newClefReplay(t, dir, nil)
	meter, _, ledger := newTestMeter(t, cost.Dollar)
	client := newWorkersTestClient(t, wire.url, meter, throttle.Config{})
	exchanges := recordedExchanges(t, dir)
	if len(exchanges) == 0 {
		t.Fatalf("%s holds no recordings", rel)
	}
	models := map[string]int{}
	for _, exchange := range exchanges {
		rec, err := rep.Reply(exchange)
		if err != nil {
			t.Fatalf("%s: %v", exchange, err)
		}
		req := recordedRequest(t, rec)
		models[req.Model]++
		got, err := client.Ask(context.Background(), req)
		if err != nil {
			failErr(t, fmt.Errorf("%s: %w", exchange, err))
		}
		want, wantUsage := recordedAnswers(t, rec)
		assertSameAnswers(t, exchange, got, want, wantUsage)
		call := wire.lastCall()
		if want := "/client/v4/accounts/" + workersTestAccount + runPath(req.Model); call.Path != want {
			t.Fatalf("%s: path %s, want %s", exchange, call.Path, want)
		}
		if !bytes.Contains(call.Body, []byte(`"model":"`+req.Model+`"`)) {
			t.Fatalf("%s: body does not name model %q", exchange, req.Model)
		}
		if bytes.Contains(call.Body, []byte(workersTestToken)) {
			t.Fatalf("%s: body carries the token", exchange)
		}
	}
	if bothModels && (models[ModelClef] == 0 || models[ModelClefFlash] == 0) {
		t.Fatalf("%s covers models %v", rel, models)
	}
	charges := ledger.Charges()
	if len(charges) != len(exchanges) {
		t.Fatalf("%d charges for %d calls", len(charges), len(exchanges))
	}
	for _, charge := range charges {
		if !strings.HasPrefix(charge.Kind, "workersai."+ModelClef) && !strings.HasPrefix(charge.Kind, "workersai."+ModelClefFlash) {
			t.Fatalf("charge kind %q", charge.Kind)
		}
		if charge.Units != 1 || charge.UnitPrice <= 0 {
			t.Fatalf("charge %+v", charge)
		}
		if charge.Denomination != (cost.Denomination{}) {
			t.Fatalf("charge denomination %+v", charge.Denomination)
		}
	}
	if wire.unauthorized() != 0 {
		t.Fatal("the client did not send the bearer token")
	}
}

func TestWorkersAIReplaysEveryRecordedClefExchange(t *testing.T) {
	t.Parallel()
	replayEveryRecordingIn(t, "", true)
}

// The cost recordings cover a judge with no image at all and the same
// judge at three image sizes; their images are referenced by file and
// resolved on Open. They are all on clef.
func TestWorkersAIReplaysTheCostRecordings(t *testing.T) {
	t.Parallel()
	replayEveryRecordingIn(t, "cost", false)
}

func TestOversizeImagesAreRefusedBeforeAnyCall(t *testing.T) {
	t.Parallel()
	wire, _ := newClefReplay(t, clefDir(t), nil)
	meter, budget, ledger := newTestMeter(t, cost.Dollar)
	client := newWorkersTestClient(t, wire.url, meter, throttle.Config{})

	// jpegFile is a header that reads as a small picture, padded to a
	// size: big enough for the byte caps, small enough to pass the pixel
	// cap, so each case fails on the limit it names. The bare SOS after
	// the frame header is what lets the standard decoder stop there.
	jpegFile := func(size int) []byte {
		header := []byte{
			0xFF, 0xD8, // SOI
			0xFF, 0xC0, 0x00, 0x0B, 8, // SOF0, precision 8
			0x00, 100, 0x00, 100, // 100 by 100
			1, 1, 0x11, 0, // one component
			0xFF, 0xDA, 0x00, 0x02, // SOS
		}
		if size <= len(header) {
			return header
		}
		return append(header, make([]byte, size-len(header))...)
	}
	cases := []struct {
		name   string
		images []Image
		check  string
	}{
		{"image over 4 MiB", []Image{{ContentType: "image/jpeg", Bytes: jpegFile(maxImageBytes + 1)}}, "image_bytes"},
		{"images over 8 MiB in all", []Image{
			{ContentType: "image/jpeg", Bytes: jpegFile(maxImageBytes - 1024)},
			{ContentType: "image/jpeg", Bytes: jpegFile(maxImageBytes - 1024)},
			{ContentType: "image/jpeg", Bytes: jpegFile(maxImageBytes - 1024)},
		}, "total_image_bytes"},
		{"image over 16 megapixels", []Image{{ContentType: "image/webp", Bytes: webpVP8XFile(6000, 3000)}}, "image_pixels"},
		{"image with no readable size", []Image{{ContentType: "image/png", Bytes: []byte("not an image")}}, "image_unreadable"},
	}
	for _, testCase := range cases {
		req := sampleRequest(t, nil)
		req.Images = testCase.images
		_, err := client.Ask(context.Background(), req)
		var limit *LimitError
		if !errors.As(err, &limit) || limit.Check != testCase.check {
			t.Fatalf("%s: err = %v, want LimitError %s", testCase.name, err, testCase.check)
		}
	}
	// A body over 13 MiB: two images under every image cap, and a state
	// that pushes the marshalled body past the ceiling.
	req := sampleRequest(t, nil)
	req.State = jsonRaw(t, strings.Repeat("x", 12<<20))
	req.Images = []Image{
		{ContentType: "image/jpeg", Bytes: jpegFile(3 << 20)},
		{ContentType: "image/jpeg", Bytes: jpegFile(3 << 20)},
	}
	_, err := client.Ask(context.Background(), req)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Check != "request_bytes" || limit.Image != -1 {
		t.Fatalf("body over 13 MiB: err = %v", err)
	}

	if calls := wire.callCount(); calls != 0 {
		t.Fatalf("%d requests left the client", calls)
	}
	if charges := ledger.Charges(); len(charges) != 0 {
		t.Fatalf("%d charges recorded", len(charges))
	}
	if spent := budget.Spent(); spent != 0 {
		t.Fatalf("budget spent %d on refused calls", spent)
	}
	if _, err := client.Ask(context.Background(), Request{}); err == nil {
		t.Fatal("empty request accepted")
	}
	if calls := wire.callCount(); calls != 0 {
		t.Fatalf("%d requests left the client", calls)
	}
}

func jsonRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestImageDimensionsReadHeadersOnly pins the header reader the pixel cap
// stands on: PNG and JPEG through the standard decoder, WebP through the
// three chunk forms the container spec fixes.
func TestImageDimensionsReadHeadersOnly(t *testing.T) {
	t.Parallel()
	sample, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "samples", "woman", "V04-family-function.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	w, h, err := imageDimensions(sample)
	if err != nil {
		t.Fatal(err)
	}
	if w != 896 || h != 1200 {
		t.Fatalf("sample is %dx%d, want 896x1200", w, h)
	}

	if w, h, err := imageDimensions(webpVP8XFile(500, 400)); err != nil || w != 500 || h != 400 {
		t.Fatalf("vp8x: %dx%d %v", w, h, err)
	}
	if w, h, err := imageDimensions(webpLossyFile(640, 480)); err != nil || w != 640 || h != 480 {
		t.Fatalf("vp8: %dx%d %v", w, h, err)
	}
	if w, h, err := imageDimensions(webpLosslessFile(320, 200)); err != nil || w != 320 || h != 200 {
		t.Fatalf("vp8l: %dx%d %v", w, h, err)
	}
	if _, _, err := imageDimensions([]byte("gif89a" + strings.Repeat("\x00", 40))); err == nil {
		t.Fatal("a gif was measured")
	}
}

func webpFile(chunk string, body []byte) []byte {
	out := []byte("RIFF")
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, uint32(4+8+len(body)))
	out = append(out, size...)
	out = append(out, "WEBP"...)
	out = append(out, chunk...)
	chunkSize := make([]byte, 4)
	binary.LittleEndian.PutUint32(chunkSize, uint32(len(body)))
	out = append(out, chunkSize...)
	return append(out, body...)
}

func webpVP8XFile(w, h int) []byte {
	body := make([]byte, 10)
	body[4] = byte(w - 1)
	body[5] = byte((w - 1) >> 8)
	body[6] = byte((w - 1) >> 16)
	body[7] = byte(h - 1)
	body[8] = byte((h - 1) >> 8)
	body[9] = byte((h - 1) >> 16)
	return webpFile("VP8X", body)
}

func webpLossyFile(w, h int) []byte {
	body := make([]byte, 10)
	body[3], body[4], body[5] = 0x9D, 0x01, 0x2A
	binary.LittleEndian.PutUint16(body[6:8], uint16(w))
	binary.LittleEndian.PutUint16(body[8:10], uint16(h))
	return webpFile("VP8 ", body)
}

func webpLosslessFile(w, h int) []byte {
	body := make([]byte, 5)
	body[0] = 0x2F
	bits := uint32(w-1) | uint32(h-1)<<14
	binary.LittleEndian.PutUint32(body[1:5], bits)
	return append(webpFile("VP8L", body), 0, 0, 0, 0, 0)
}

func TestARetryableStatusRetriesThroughTheThrottle(t *testing.T) {
	t.Parallel()
	wire, rep := newClefReplay(t, clefDir(t), []wireStep{{status: http.StatusBadGateway}})
	meter, _, ledger := newTestMeter(t, cost.Dollar)
	var mu sync.Mutex
	var waits []time.Duration
	pacer := throttle.Config{Sleep: func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		waits = append(waits, d)
		mu.Unlock()
		return nil
	}}
	client := newWorkersTestClient(t, wire.url, meter, pacer)

	req := recordedRequest(t, recorded(t, rep, "judge-V01-date"))
	got, err := client.Ask(context.Background(), req)
	if err != nil {
		failErr(t, err)
	}
	want, wantUsage := recordedAnswers(t, recorded(t, rep, "judge-V01-date"))
	assertSameAnswers(t, "judge-V01-date", got, want, wantUsage)
	if calls := wire.callCount(); calls != 2 {
		t.Fatalf("%d requests, want the retry", calls)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(waits) != 1 {
		t.Fatalf("%d waits, want one", len(waits))
	}
	if charges := ledger.Charges(); len(charges) != 1 {
		t.Fatalf("%d charges for one settled call", len(charges))
	}
}

// A rate limit draws its own attempt budget and follows the wait the
// Retry-After header names.
func TestARateLimitFollowsTheRetryAfterHint(t *testing.T) {
	t.Parallel()
	wire, rep := newClefReplay(t, clefDir(t), []wireStep{
		{status: http.StatusTooManyRequests, retryAfter: "2"},
		{status: http.StatusTooManyRequests},
	})
	meter, _, _ := newTestMeter(t, cost.Dollar)
	var mu sync.Mutex
	var waits []time.Duration
	pacer := throttle.Config{Sleep: func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		waits = append(waits, d)
		mu.Unlock()
		return nil
	}}
	client := newWorkersTestClient(t, wire.url, meter, pacer)
	req := recordedRequest(t, recorded(t, rep, "judge-V02-date"))
	if _, err := client.Ask(context.Background(), req); err != nil {
		failErr(t, err)
	}
	if calls := wire.callCount(); calls != 3 {
		t.Fatalf("%d requests, want two refusals then the call", calls)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(waits) != 2 || waits[0] < 2*time.Second {
		t.Fatalf("waits %v, want the first above the 2s hint", waits)
	}
}

func TestANonRetryableRefusalDoesNotRetry(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()
			wire, _ := newClefReplay(t, clefDir(t), []wireStep{{status: status}})
			meter, _, ledger := newTestMeter(t, cost.Dollar)
			paced := false
			pacer := throttle.Config{Sleep: func(context.Context, time.Duration) error {
				paced = true
				return nil
			}}
			client := newWorkersTestClient(t, wire.url, meter, pacer)
			req := sampleRequest(t, nil)
			req.State = jsonRaw(t, map[string]string{"occasion": "date"})
			_, err := client.Ask(context.Background(), req)
			var statusErr *StatusError
			if !errors.As(err, &statusErr) || statusErr.Code != status {
				t.Fatalf("err = %v, want StatusError %d", err, status)
			}
			if statusErr.Retryable() {
				t.Fatalf("%d read as retryable", status)
			}
			if strings.Contains(err.Error(), workersTestToken) {
				t.Fatal("error contains the token")
			}
			if calls := wire.callCount(); calls != 1 {
				t.Fatalf("%d requests, want one", calls)
			}
			if paced {
				t.Fatal("a refusal waited")
			}
			if charges := ledger.Charges(); len(charges) != 0 {
				t.Fatalf("%d charges for a refused call", len(charges))
			}
		})
	}
}

// A server fault is a transient class of its own: it draws the smaller
// attempt budget, so two faults end the call without a third submission.
func TestATransientFaultDrawsItsOwnBudget(t *testing.T) {
	t.Parallel()
	wire, _ := newClefReplay(t, clefDir(t), []wireStep{{status: http.StatusBadGateway}, {status: http.StatusBadGateway}})
	meter, _, _ := newTestMeter(t, cost.Dollar)
	client := newWorkersTestClient(t, wire.url, meter, instantPacer())
	_, err := client.Ask(context.Background(), sampleRequest(t, nil))
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Code != http.StatusBadGateway {
		t.Fatalf("err = %v", err)
	}
	if calls := wire.callCount(); calls != 2 {
		t.Fatalf("%d requests, want the transient budget of two", calls)
	}
}

func TestAFailedEnvelopeDoesNotRetry(t *testing.T) {
	t.Parallel()
	refused := `{"result":null,"success":false,"errors":[{"code":70001,"message":"model not found"}],"messages":[]}`
	wire, _ := newClefReplay(t, clefDir(t), []wireStep{{status: http.StatusOK, body: refused}})
	meter, _, _ := newTestMeter(t, cost.Dollar)
	client := newWorkersTestClient(t, wire.url, meter, instantPacer())
	_, err := client.Ask(context.Background(), sampleRequest(t, nil))
	var envelope *EnvelopeError
	if !errors.As(err, &envelope) || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("err = %v, want EnvelopeError", err)
	}
	if calls := wire.callCount(); calls != 1 {
		t.Fatalf("%d requests, want one", calls)
	}

	incomplete := `{"result":{"model":"clef","answers":{"met":{"type":"noul"}}},"success":true,"errors":[],"messages":[]}`
	wire2, _ := newClefReplay(t, clefDir(t), []wireStep{{status: http.StatusOK, body: incomplete}})
	client2 := newWorkersTestClient(t, wire2.url, meter, instantPacer())
	_, err = client2.Ask(context.Background(), sampleRequest(t, nil))
	if !errors.As(err, new(*EnvelopeError)) || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("err = %v, want an incomplete answer refusal", err)
	}
	if calls := wire2.callCount(); calls != 1 {
		t.Fatalf("%d requests, want one", calls)
	}
}

// instantPacer waits nowhere; a wrong retry then fails the test fast
// instead of sitting out the default backoff.
func instantPacer() throttle.Config {
	return throttle.Config{Sleep: func(context.Context, time.Duration) error { return nil }}
}

func TestTheMeterQuotesAndSettlesEveryCall(t *testing.T) {
	t.Parallel()
	wire, rep := newClefReplay(t, clefDir(t), nil)
	meter, budget, ledger := newTestMeter(t, cost.Dollar)
	client := newWorkersTestClient(t, wire.url, meter, throttle.Config{})

	var wantTotal cost.Price
	for _, exchange := range []string{"judge-V01-date", "judge-V02-date"} {
		rec := recorded(t, rep, exchange)
		req := recordedRequest(t, rec)
		before := len(ledger.Charges())
		if _, err := client.Ask(context.Background(), req); err != nil {
			failErr(t, err)
		}
		charges := ledger.Charges()
		if len(charges) != before+1 {
			t.Fatalf("%s: %d charges after the call", exchange, len(charges))
		}
		charge := charges[before]
		if charge.Kind != "workersai."+req.Model || charge.Ref != mustKey(t, req) || charge.Units != 1 {
			t.Fatalf("%s: charge %+v", exchange, charge)
		}
		_, wantUsage := recordedAnswers(t, rec)
		product := int64(wantUsage.InputTokens) * 21_818 * 11_000
		wantPrice := cost.Price((product + 999_999) / 1_000_000)
		if charge.UnitPrice != wantPrice {
			t.Fatalf("%s: charged %d, want the measured %d from %d tokens", exchange, charge.UnitPrice, wantPrice, wantUsage.InputTokens)
		}
		wantTotal += wantPrice
	}
	if spent := budget.Spent(); spent != wantTotal {
		t.Fatalf("budget spent %d, want %d", spent, wantTotal)
	}
}

func TestABudgetRefusalSpendsNothing(t *testing.T) {
	t.Parallel()
	wire, _ := newClefReplay(t, clefDir(t), nil)
	meter, budget, ledger := newTestMeter(t, 1)
	client := newWorkersTestClient(t, wire.url, meter, throttle.Config{})
	_, err := client.Ask(context.Background(), sampleRequest(t, nil))
	if !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("err = %v, want ErrOverBudget", err)
	}
	if calls := wire.callCount(); calls != 0 {
		t.Fatalf("%d requests left the client", calls)
	}
	if len(ledger.Charges()) != 0 || budget.Spent() != 0 {
		t.Fatal("a refused call booked spend")
	}
}

func TestWorkersAIRejectsBeforeDialing(t *testing.T) {
	t.Parallel()
	wire, _ := newClefReplay(t, clefDir(t), nil)
	meter, _, _ := newTestMeter(t, cost.Dollar)
	client := newWorkersTestClient(t, wire.url, meter, throttle.Config{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Ask(ctx, sampleRequest(t, nil)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	unknown := sampleRequest(t, nil)
	unknown.Model = "gpt-4"
	if _, err := client.Ask(context.Background(), unknown); err == nil || !strings.Contains(err.Error(), "gpt-4") {
		t.Fatalf("err = %v, want the model refused by name", err)
	}
	if calls := wire.callCount(); calls != 0 {
		t.Fatalf("%d requests left the client", calls)
	}

	if _, err := NewWorkersAI(WorkersAIConfig{}); err == nil {
		t.Fatal("empty config accepted")
	}
	if _, err := NewWorkersAI(WorkersAIConfig{AccountID: "a/b", Token: workersTestToken, Meter: meter}); err == nil {
		t.Fatal("an account id with a slash was accepted")
	}
	if _, err := NewWorkersAI(WorkersAIConfig{AccountID: workersTestAccount, Token: "", Meter: meter}); err == nil {
		t.Fatal("an empty token was accepted")
	}
	if _, err := NewWorkersAI(WorkersAIConfig{AccountID: workersTestAccount, Token: workersTestToken}); err == nil {
		t.Fatal("a nil meter was accepted")
	}
	if _, err := NewWorkersAI(WorkersAIConfig{BaseURL: "ftp://api.cloudflare.com", AccountID: workersTestAccount, Token: workersTestToken, Meter: meter}); err == nil {
		t.Fatal("a non-http base was accepted")
	}
	if _, err := NewWorkersAI(WorkersAIConfig{BaseURL: "https://user@api.cloudflare.com", AccountID: workersTestAccount, Token: workersTestToken, Meter: meter}); err == nil {
		t.Fatal("userinfo was accepted")
	}
}

func TestWorkersAITimeoutsAreTheOnlyTransportRetry(t *testing.T) {
	t.Parallel()
	if !isTimeout(&url.Error{Op: "Post", Err: timeoutStub{}}) {
		t.Fatal("a transport timeout was not read as one")
	}
	if isTimeout(&url.Error{Op: "Post", Err: context.DeadlineExceeded}) {
		t.Fatal("the caller's deadline read as a provider timeout")
	}
	if isTimeout(context.Canceled) || isTimeout(nil) {
		t.Fatal("a cancelled call read as a timeout")
	}
	if workersRetryable(&StatusError{Code: http.StatusBadRequest}) {
		t.Fatal("a 400 read as retryable")
	}
	if !workersRetryable(&StatusError{Code: http.StatusServiceUnavailable}) {
		t.Fatal("a 503 did not read as retryable")
	}
	if !transientFault(&StatusError{Code: http.StatusBadGateway}) || transientFault(&StatusError{Code: http.StatusTooManyRequests}) {
		t.Fatal("the transient class split wrongly")
	}
	if !workersRetryable(timeoutStub{}) {
		t.Fatal("a bare timeout did not read as retryable")
	}
	if workersRetryable(errors.New("no dial")) {
		t.Fatal("a plain error read as retryable")
	}
}

type timeoutStub struct{}

func (timeoutStub) Error() string   { return "timed out" }
func (timeoutStub) Timeout() bool   { return true }
func (timeoutStub) Temporary() bool { return true }

func recorded(t *testing.T, rep *Replay, exchange string) Recording {
	t.Helper()
	rec, err := rep.Reply(exchange)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func mustKey(t *testing.T, req Request) string {
	t.Helper()
	key, err := req.key()
	if err != nil {
		t.Fatal(err)
	}
	return key
}
