package systemone

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

// The recorder is tested against this fake and a temp directory. It
// makes no call anywhere: nothing here reads a key, and the two values
// a real client holds are the package's standing sentinels, asserted
// absent from every byte the recorder writes.

// fakeInner is the SystemOne behind the recorder. It answers what it is
// told, counts its calls, and holds the credentials its live twin holds
// in the same fields: a recorder that reached for a header, a bearer or
// an account id would have found them here and written them.
type fakeInner struct {
	mu      sync.Mutex
	resp    Response
	err     error
	calls   int
	seen    []Request
	token   string
	account string
	entered chan struct{}
	release chan struct{}
}

func newFakeInner() *fakeInner {
	return &fakeInner{token: workersTestToken, account: workersTestAccount}
}

func (f *fakeInner) Ask(ctx context.Context, req Request) (Response, error) {
	f.mu.Lock()
	f.calls++
	f.seen = append(f.seen, req)
	resp, err := f.resp, f.err
	entered, release := f.entered, f.release
	f.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return Response{}, ctx.Err()
		}
	}
	if err != nil {
		return Response{}, err
	}
	return resp, nil
}

func (f *fakeInner) set(resp Response) {
	f.mu.Lock()
	f.resp, f.err = resp, nil
	f.mu.Unlock()
}

func (f *fakeInner) fail(err error) {
	f.mu.Lock()
	f.err, f.resp = err, Response{}
	f.mu.Unlock()
}

func (f *fakeInner) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// noulResponse is a reply the replay can serve: one noul at p, one
// choice, and a token count.
func noulResponse(p float64) Response {
	confidence := 0.4
	return Response{
		Model: ModelClef,
		Answers: map[string]Answer{
			"met":   {Type: TypeNoul, Noul: &p},
			"issue": {Type: TypeChoice, Choice: "collar", Probabilities: map[string]float64{"collar": 0.7, "none": 0.3}, Confidence: &confidence},
		},
		Usage: Usage{InputTokens: 1963},
	}
}

func newRecorder(t *testing.T, cfg RecorderConfig) *Recorder {
	t.Helper()
	recorder, err := NewRecorder(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return recorder
}

// liveBody is the request body the live client puts on the wire for req:
// images inline, as the call sends them. ContentKey over this is what a
// recording has to answer to.
func liveBody(t *testing.T, req Request) json.RawMessage {
	t.Helper()
	return jsonRaw(t, workersBody{
		Model:     req.Model,
		Images:    req.Images,
		State:     req.State,
		Questions: req.Questions,
	})
}

// TestTheRecorderRoundTripsIntoTheReplay is the charter's proof: the
// directory the recorder wrote is opened by replay.Open and serves the
// same request, under the same key, with the same answers and the same
// usage. Nothing dialed to make it.
func TestTheRecorderRoundTripsIntoTheReplay(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "clef")
	fake := newFakeInner()
	want := noulResponse(0.75)
	fake.set(want)
	recorder := newRecorder(t, RecorderConfig{Inner: fake, Dir: dir})
	req := sampleRequest(t, []byte{0xff, 0xd8, 0x01, 0x02})

	got, err := recorder.Record(context.Background(), "judge-date", req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the recorder did not answer as the inner client: %#v", got)
	}
	if fake.count() != 1 {
		t.Fatalf("inner calls %d, want 1", fake.count())
	}
	if names := fileNames(t, dir); !strings.Contains(strings.Join(names, " "), "judge-date.json") {
		t.Fatalf("the exchange file is not named by its exchange: %v", names)
	} else if len(names) != 2 || !strings.Contains(names[0], "images") {
		t.Fatalf("one recording and its one image: %v", names)
	}

	replay := openDir(t, dir)
	served, err := replay.Ask(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(served, want) {
		t.Fatalf("served %#v, recorded %#v", served, want)
	}
	rec, err := replay.Serve(http.MethodPost, runPath(req.Model), liveBody(t, req))
	if err != nil || rec.Key != "judge-date" {
		t.Fatalf("the live body does not reach this recording: %v (%s)", err, rec.Key)
	}
	if reply, err := replay.Reply("judge-date"); err != nil || reply.Key != rec.Key {
		t.Fatal(err)
	}
	// The stored request, with its image read back inline, is the
	// request that was sent.
	if key, err := recordedRequest(t, rec).key(); err != nil || key != mustKey(t, req) {
		t.Fatalf("stored request keys %q, want %q (%v)", key, mustKey(t, req), err)
	}

	other := req
	other.Images = []Image{{ContentType: "image/jpeg", Bytes: []byte{0x00}}}
	_, err = replay.Ask(context.Background(), other)
	var missing *Missing
	if !errors.As(err, &missing) {
		t.Fatalf("other bytes served: %v", err)
	}
}

// TestARecordingOfACommittedCallKeysIdentical is the claim the whole
// task exists for. A committed recording is read, the request it
// describes is rebuilt and recorded again through the recorder, with the
// stored reply handed back by the fake. The fresh directory and
// testdata/clef then key the same request to the same content key,
// answer the same probabilities, and hold the same envelope shape. A
// recorder that wrote a different byte shape would be caught here, and
// T3.9e would re-measure against a corpus that does not replay.
func TestARecordingOfACommittedCallKeysIdentical(t *testing.T) {
	t.Parallel()
	const exchange = "judge-V04-family-function"
	corpus := openDir(t, clefDir(t))
	stored := recorded(t, corpus, exchange)
	req := recordedRequest(t, stored)
	want, ok := decodeResponse(stored.Response)
	if !ok {
		t.Fatal("the committed reply is not one the replay can serve")
	}
	if len(req.Images) != 1 {
		t.Fatalf("this proof walks one image, the recording holds %d", len(req.Images))
	}

	// The sample photo, copied where a fresh corpus can name it by a
	// relative reference of its own. testdata is read here and never
	// written.
	root := t.TempDir()
	photo := filepath.Join(root, "samples", "woman", "V04-family-function.jpg")
	if err := os.MkdirAll(filepath.Dir(photo), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(photo, req.Images[0].Bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	matcher, err := MatchImages(filepath.Join(root, "samples"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "clef")
	fake := newFakeInner()
	fake.set(want)
	recorder := newRecorder(t, RecorderConfig{Inner: fake, Dir: dir, Images: matcher})
	if _, err := recorder.Record(context.Background(), exchange, req); err != nil {
		t.Fatal(err)
	}
	fresh := openDir(t, dir)

	// One request, two directories, one key: the replay hit is the
	// equality, because Ask serves a recording only to an identical
	// request.
	answered, err := fresh.Ask(context.Background(), req)
	if err != nil {
		t.Fatalf("the fresh recording does not serve the request it records: %v", err)
	}
	answers, usage := recordedAnswers(t, stored)
	assertSameAnswers(t, exchange, answered, answers, usage)
	fromFresh, err := fresh.Serve(http.MethodPost, runPath(req.Model), liveBody(t, req))
	if err != nil {
		t.Fatal(err)
	}
	fromCorpus, err := corpus.Serve(http.MethodPost, runPath(req.Model), liveBody(t, req))
	if err != nil {
		t.Fatal(err)
	}
	if fromFresh.Key != exchange || fromCorpus.Key != exchange {
		t.Fatalf("served %q and %q", fromFresh.Key, fromCorpus.Key)
	}
	if !bytes.Equal(canonicalBytes(t, fromFresh.Request), canonicalBytes(t, fromCorpus.Request)) {
		t.Fatalf("the two directories key the request differently:\nfresh  %s\ncorpus %s",
			fromFresh.Request, fromCorpus.Request)
	}
	if key, err := ContentKey(http.MethodPost, runPath(req.Model), liveBody(t, req)); err != nil {
		t.Fatal(err)
	} else if served, err := fresh.Serve(http.MethodPost, runPath(req.Model), liveBody(t, req)); err != nil || served.Key != exchange {
		t.Fatalf("content key %s does not reach the fresh recording: %v", key, err)
	}

	// The file itself: the same fields in the same places, and the same
	// reply but for the milliseconds the call took.
	assertSameEnvelopeShape(t,
		filepath.Join(clefDir(t), exchange+".json"),
		filepath.Join(dir, exchange+".json"))

	// The photo went to one committed file by reference; no bytes were
	// written again, and no image is duplicated in the JSON.
	raw := readFile(t, filepath.Join(dir, exchange+".json"))
	if strings.Contains(string(raw), "base64") {
		t.Fatal("the recording carries image bytes")
	}
	if refs := imageRefs(t, raw); !reflect.DeepEqual(refs, []string{"../samples/woman/V04-family-function.jpg"}) {
		t.Fatalf("image refs %v", refs)
	}
	if names := fileNames(t, dir); !reflect.DeepEqual(names, []string{"judge-V04-family-function.json"}) {
		t.Fatalf("the recorder wrote more than the exchange: %v", names)
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "images")); err == nil {
		t.Fatalf("no image needed writing, yet %d files are in images/", len(entries))
	}
	if len(raw) > 8<<10 {
		t.Fatalf("a file reference costs less than the photo: %d bytes", len(raw))
	}
}

// TestARecordingHoldsNoCredential pins the envelope the T0.6 script
// wrote and nothing more: no header, no bearer, no account id, no URL,
// no absolute path. The sentinels are where a recording HTTP client
// would have found them, and the directory name is where a writer that
// stored paths would have leaked them.
func TestARecordingHoldsNoCredential(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), workersTestAccount)
	dir := filepath.Join(root, "clef")
	fake := newFakeInner()
	fake.set(noulResponse(0.2))
	recorder := newRecorder(t, RecorderConfig{Inner: fake, Dir: dir})
	req := sampleRequest(t, []byte{0xff, 0xd8, 0x0a})
	if _, err := recorder.Record(context.Background(), "judge-date", req); err != nil {
		t.Fatal(err)
	}

	walk(t, dir, func(path string, data []byte) {
		for _, forbidden := range []string{
			workersTestToken, workersTestAccount, dir, root,
			"authorization", "Authorization", "Bearer", "bearer",
			"header", "Header",
			"http://", "https://", "api.cloudflare.com", "/client/v4/accounts/",
		} {
			if strings.Contains(string(data), forbidden) {
				t.Fatalf("%s holds %q", filepath.Base(path), forbidden)
			}
		}
	})
	raw := readFile(t, filepath.Join(dir, "judge-date.json"))
	if got := keySet(t, raw); !reflect.DeepEqual(got, []string{"exchange", "request", "response"}) {
		t.Fatalf("top level keys %v", got)
	}
	if got := keySet(t, objectAt(t, raw, "request")); !reflect.DeepEqual(got, []string{"body", "method", "path"}) {
		t.Fatalf("request keys %v", got)
	}
	// The path is the segment after the account id the URL carries,
	// which is why it can be committed at all.
	if path := stringAt(t, objectAt(t, raw, "request"), "path"); path != runPath(ModelClef) {
		t.Fatalf("path %q, want %q", path, runPath(ModelClef))
	}
}

// TestAFailedCallStoresNothing: a half-written or error-shaped recording
// is worse than a missing one, because the next Open either fails on it
// or serves a broken reply to a request nobody answered.
func TestAFailedCallStoresNothing(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "clef")
	fake := newFakeInner()
	boom := fmt.Errorf("the call was refused: %s", workersTestToken)
	fake.fail(boom)
	recorder := newRecorder(t, RecorderConfig{Inner: fake, Dir: dir})
	req := sampleRequest(t, nil)

	_, err := recorder.Record(context.Background(), "judge-date", req)
	if !errors.Is(err, boom) {
		t.Fatalf("the inner client's error did not come back: %v", err)
	}
	if fake.count() != 1 {
		t.Fatalf("the call was not made")
	}
	assertNothingWritten(t, dir)

	fake.set(noulResponse(0.5))
	if _, err := recorder.Record(context.Background(), "judge-date", req); err != nil {
		t.Fatalf("a name that failed once is not poisoned: %v", err)
	}
	replay := openDir(t, dir)
	if _, err := replay.Ask(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(readFile(t, filepath.Join(dir, "judge-date.json"))), workersTestToken) {
		t.Fatal("the failure's text reached the corpus")
	}
}

// TestARefusedCallMakesNoCallAtAll: every refusal happens before the
// inner client runs, so a paid call is never spent on a recording that
// could not have been written.
func TestARefusedCallMakesNoCallAtAll(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "clef")
	fake := newFakeInner()
	fake.set(noulResponse(0.5))
	recorder := newRecorder(t, RecorderConfig{Inner: fake, Dir: dir})
	req := sampleRequest(t, nil)
	ctx := context.Background()

	for _, name := range []string{"", ".", "..", "../outside", "a/b", "a b", "judge-date.json", strings.Repeat("j", 101)} {
		if _, err := recorder.Record(ctx, name, req); err == nil {
			t.Fatalf("exchange %q was accepted", name)
		}
	}
	bad := req
	bad.Model = ""
	if _, err := recorder.Record(ctx, "judge-date", bad); err == nil {
		t.Fatal("an empty model was accepted")
	}
	noState := req
	noState.State = json.RawMessage("null")
	if _, err := recorder.Record(ctx, "judge-date", noState); err == nil {
		t.Fatal("a null state was accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := recorder.Record(cancelled, "judge-date", req); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled call: %v", err)
	}
	if fake.count() != 0 {
		t.Fatalf("the inner client ran %d times for calls that were refused", fake.count())
	}
	assertNothingWritten(t, dir)
	// Nothing escaped the recording directory either: not one of the
	// refused names made a file beside it, and the directory itself was
	// never created.
	if names := fileNames(t, filepath.Dir(dir)); len(names) != 0 {
		t.Fatalf("files beside the corpus: %v", names)
	}
}

// TestTheRecorderRefusesToOverwrite: the default is refusal, before the
// call, and the explicit allow does what it says.
func TestTheRecorderRefusesToOverwrite(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "clef")
	fake := newFakeInner()
	fake.set(noulResponse(0.1))
	first := newRecorder(t, RecorderConfig{Inner: fake, Dir: dir})
	req := sampleRequest(t, nil)
	if _, err := first.Record(context.Background(), "judge-date", req); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, filepath.Join(dir, "judge-date.json"))

	second := newFakeInner()
	second.set(noulResponse(0.9))
	recorder := newRecorder(t, RecorderConfig{Inner: second, Dir: dir})
	_, err := recorder.Record(context.Background(), "judge-date", req)
	var exists *AlreadyRecorded
	if !errors.As(err, &exists) {
		t.Fatalf("overwrite was refused with the wrong error: %v", err)
	}
	if exists.Exchange != "judge-date" || !strings.Contains(err.Error(), "Overwrite") {
		t.Fatalf("%v", err)
	}
	if second.count() != 0 {
		t.Fatal("the refusal still spent the call")
	}
	if !bytes.Equal(before, readFile(t, filepath.Join(dir, "judge-date.json"))) {
		t.Fatal("the refusal touched the recording")
	}

	forced := newRecorder(t, RecorderConfig{Inner: second, Dir: dir, Overwrite: true})
	if _, err := forced.Record(context.Background(), "judge-date", req); err != nil {
		t.Fatalf("the explicit allow refused: %v", err)
	}
	if second.count() != 1 {
		t.Fatal("the allowed call was not made")
	}
	if bytes.Equal(before, readFile(t, filepath.Join(dir, "judge-date.json"))) {
		t.Fatal("the allowed call did not replace the recording")
	}
	got, err := openDir(t, dir).Ask(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Answers["met"].Noul == nil || *got.Answers["met"].Noul != 0.9 {
		t.Fatalf("the replay still serves the old reply: %#v", got.Answers["met"])
	}
	if names := fileNames(t, dir); !reflect.DeepEqual(names, []string{"judge-date.json"}) {
		t.Fatalf("replacement left a second file: %v", names)
	}
}

// TestADuplicateExchangeIdFailsOpen: the recorder writes one file per
// name, so it cannot make two recordings of one exchange id. A file
// copied by hand can, and the reader's refusal is what that keeps honest.
func TestADuplicateExchangeIdFailsOpen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fake := newFakeInner()
	fake.set(noulResponse(0.5))
	recorder := newRecorder(t, RecorderConfig{Inner: fake, Dir: dir})
	if _, err := recorder.Record(context.Background(), "judge-date", sampleRequest(t, nil)); err != nil {
		t.Fatal(err)
	}
	data := readFile(t, filepath.Join(dir, "judge-date.json"))
	if err := os.WriteFile(filepath.Join(dir, "a-copy.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(dir)
	if err == nil ||
		!strings.Contains(err.Error(), "judge-date.json") ||
		!strings.Contains(err.Error(), "a-copy.json") ||
		!strings.Contains(err.Error(), "judge-date") {
		t.Fatalf("two files, one exchange id: %v", err)
	}
}

// TestANestedFolderOpensOnItsOwn: Open is not recursive, which is why
// the corpus keeps stability and cost in folders of their own and
// eval.Run opens three directories. A recorder that wrote its images
// into a subfolder does not disturb the exchanges above it.
func TestANestedFolderOpensOnItsOwn(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	top := filepath.Join(root, "clef")
	nested := filepath.Join(top, "stability")
	req := sampleRequest(t, []byte{0xff, 0xd8, 0x03})
	fake := newFakeInner()
	fake.set(noulResponse(0.5))

	if _, err := newRecorder(t, RecorderConfig{Inner: fake, Dir: top}).
		Record(context.Background(), "judge-V04-family-function", req); err != nil {
		t.Fatal(err)
	}
	repeats := newRecorder(t, RecorderConfig{Inner: fake, Dir: nested})
	for _, name := range []string{"stability-V04-family-function-01", "stability-V04-family-function-02"} {
		if _, err := repeats.Record(context.Background(), name, req); err != nil {
			t.Fatal(err)
		}
	}

	topReplay := openDir(t, top)
	if _, err := topReplay.Ask(context.Background(), req); err != nil {
		t.Fatalf("the judge folder turned ambiguous: %v", err)
	}
	rec, err := topReplay.Serve(http.MethodPost, runPath(req.Model), liveBody(t, req))
	if err != nil || rec.Key != "judge-V04-family-function" {
		t.Fatalf("serve: %v (%s)", err, rec.Key)
	}

	nestedReplay := openDir(t, nested)
	_, err = nestedReplay.Ask(context.Background(), req)
	var ambiguous *Ambiguous
	if !errors.As(err, &ambiguous) ||
		!strings.Contains(err.Error(), "stability-V04-family-function-01") ||
		!strings.Contains(err.Error(), "stability-V04-family-function-02") {
		t.Fatalf("ten repeats of one request must refuse to choose: %v", err)
	}
	for _, name := range []string{"stability-V04-family-function-01", "stability-V04-family-function-02"} {
		recorded(t, nestedReplay, name)
	}
}

// TestImageBytesGoToAFileTheRecordingReferences: the resolver's committed
// file is named relative to the recording, and that recording keys
// exactly as the inline twin of the same request.
func TestImageBytesGoToAFileTheRecordingReferences(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	photos := filepath.Join(root, "photos")
	if err := os.MkdirAll(photos, 0o755); err != nil {
		t.Fatal(err)
	}
	first := []byte{0xff, 0xd8, 0x11, 0x22}
	second := []byte{0xff, 0xd8, 0x33, 0x44}
	for name, data := range map[string][]byte{"one.jpg": first, "two.jpg": second} {
		if err := os.WriteFile(filepath.Join(photos, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	matcher, err := MatchImages(photos)
	if err != nil {
		t.Fatal(err)
	}
	req := Request{
		Model:     ModelClef,
		Images:    []Image{{ContentType: "image/jpeg", Bytes: first}, {ContentType: "image/jpeg", Bytes: second}},
		State:     jsonRaw(t, map[string]string{"occasion": "date"}),
		Questions: sampleQuestions(t),
	}
	body := noulResponse(0.6)
	fake := newFakeInner()
	fake.set(body)
	fileDir := filepath.Join(root, "byfile")
	if _, err := newRecorder(t, RecorderConfig{Inner: fake, Dir: fileDir, Images: matcher}).
		Record(context.Background(), "check-two", req); err != nil {
		t.Fatal(err)
	}
	raw := readFile(t, filepath.Join(fileDir, "check-two.json"))
	want := []string{"../photos/one.jpg", "../photos/two.jpg"}
	if got := imageRefs(t, raw); !reflect.DeepEqual(got, want) {
		t.Fatalf("refs %v, want %v", got, want)
	}
	if strings.Contains(string(raw), "base64") {
		t.Fatal("the image bytes were written a second time")
	}
	if names := fileNames(t, fileDir); !reflect.DeepEqual(names, []string{"check-two.json"}) {
		t.Fatalf("the resolver's file needed no copy: %v", names)
	}

	inlineDir := filepath.Join(root, "inline")
	if err := os.MkdirAll(inlineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExchange(t, inlineDir, "check-two", map[string]any{
		"method": http.MethodPost,
		"path":   runPath(req.Model),
		"body":   liveBody(t, req),
	}, map[string]any{"result": body})

	key, err := ContentKey(http.MethodPost, runPath(req.Model), liveBody(t, req))
	if err != nil {
		t.Fatal(err)
	}
	var resolved [][]byte
	for _, dir := range []string{fileDir, inlineDir} {
		replay := openDir(t, dir)
		served, err := replay.Serve(http.MethodPost, runPath(req.Model), liveBody(t, req))
		if err != nil || served.Key != "check-two" {
			t.Fatalf("%s: the file reference and the inline bytes do not share a content key %s: %v", dir, key, err)
		}
		got, err := replay.Ask(context.Background(), req)
		if err != nil || !reflect.DeepEqual(got, body) {
			t.Fatalf("%s: ask %#v (%v)", dir, got, err)
		}
		resolved = append(resolved, canonicalBytes(t, served.Request))
	}
	if !bytes.Equal(resolved[0], resolved[1]) {
		t.Fatal("the recording by reference and the recording by bytes are not one request")
	}
}

// TestAnUnmatchedImageIsWrittenOnce: with no committed file to point at,
// the recorder puts the bytes under the recording directory, named by
// their own digest, and a second exchange with the same photo reuses it.
func TestAnUnmatchedImageIsWrittenOnce(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "clef")
	data := []byte{0xff, 0xd8, 0x07, 0x08}
	req := Request{
		Model:     ModelClef,
		Images:    []Image{{ContentType: "image/jpeg", Bytes: data}},
		State:     jsonRaw(t, map[string]string{"occasion": "date"}),
		Questions: sampleQuestions(t),
	}
	fake := newFakeInner()
	fake.set(noulResponse(0.5))
	recorder := newRecorder(t, RecorderConfig{Inner: fake, Dir: dir})
	ctx := context.Background()
	if _, err := recorder.Record(ctx, "judge-one", req); err != nil {
		t.Fatal(err)
	}
	other := req
	other.State = jsonRaw(t, map[string]string{"occasion": "interview"})
	if _, err := recorder.Record(ctx, "judge-two", other); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "images"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("one photo, %d files: %v", len(entries), entries)
	}
	if !strings.HasSuffix(entries[0].Name(), ".jpg") {
		t.Fatalf("the extension follows the content type: %s", entries[0].Name())
	}
	stored := readFile(t, filepath.Join(dir, "images", entries[0].Name()))
	if !bytes.Equal(stored, data) {
		t.Fatal("the written bytes are not the bytes that went out")
	}
	ref := imageRefs(t, readFile(t, filepath.Join(dir, "judge-one.json")))
	if len(ref) != 1 || ref[0] != "images/"+entries[0].Name() {
		t.Fatalf("refs %v", ref)
	}
	replay := openDir(t, dir)
	for _, exchange := range []string{"judge-one", "judge-two"} {
		recorded(t, replay, exchange)
	}
	served, err := replay.Serve(http.MethodPost, runPath(req.Model), liveBody(t, req))
	if err != nil || served.Key != "judge-one" {
		t.Fatalf("the file reference answers the inline request: %v", err)
	}
}

// TestAResolverNamingTheWrongPhotoIsRefused: a reference to bytes other
// than the ones sent would record a request that was never made, and the
// replay would key it as if it had been. Every form of that refusal is
// reached before the inner client runs, so a resolver pointed at the
// wrong tree costs nothing: no call spent, no file written.
func TestAResolverNamingTheWrongPhotoIsRefused(t *testing.T) {
	t.Parallel()
	fake := newFakeInner()
	fake.set(noulResponse(0.5))
	req := sampleRequest(t, []byte{0xff, 0xd8, 0x09})
	ctx := context.Background()

	// A reference naming a file that is not there.
	dir := filepath.Join(t.TempDir(), "clef")
	unreadable := func(_, _ string, _ []byte) (string, bool) {
		return "../photos/other.jpg", true
	}
	if _, err := newRecorder(t, RecorderConfig{Inner: fake, Dir: dir, Images: unreadable}).
		Record(ctx, "judge-date", req); err == nil ||
		!strings.Contains(err.Error(), "../photos/other.jpg") {
		t.Fatalf("a reference that cannot be read was accepted: %v", err)
	}
	if fake.count() != 0 {
		t.Fatalf("a reference naming no file spent %d inner calls", fake.count())
	}
	assertNothingWritten(t, dir)

	// A reference naming a file that holds other bytes.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "photos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "photos", "other.jpg"), []byte{0x01, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}
	recorder := newRecorder(t, RecorderConfig{Inner: fake, Dir: filepath.Join(root, "clef"), Images: func(_, _ string, _ []byte) (string, bool) {
		return "../photos/other.jpg", true
	}})
	if _, err := recorder.Record(ctx, "judge-date", req); err == nil ||
		!strings.Contains(err.Error(), "other than the call") {
		t.Fatalf("other bytes than the call's were accepted: %v", err)
	}
	if fake.count() != 0 {
		t.Fatalf("a reference to other bytes spent %d inner calls", fake.count())
	}
	assertNothingWritten(t, filepath.Join(root, "clef"))

	// An absolute reference is a path outside the corpus.
	recorder = newRecorder(t, RecorderConfig{Inner: fake, Dir: filepath.Join(root, "clef2"), Images: func(_, _ string, _ []byte) (string, bool) {
		return filepath.Join(root, "photos", "other.jpg"), true
	}})
	if _, err := recorder.Record(ctx, "judge-date", req); err == nil ||
		!strings.Contains(err.Error(), "relative") {
		t.Fatalf("an absolute reference was accepted: %v", err)
	}
	if fake.count() != 0 {
		t.Fatalf("an absolute reference spent %d inner calls in all", fake.count())
	}
	assertNothingWritten(t, filepath.Join(root, "clef2"))
}

// TestAnImageFileThatCannotBeHonouredIsRefusedBeforeTheCall: the other
// three ways an image path can fail, each of them settled before the
// inner client runs. A digest file that holds other bytes, one that holds
// no bytes at all, and one that cannot be read are refusals that cost
// nothing, not calls that end with nothing stored.
func TestAnImageFileThatCannotBeHonouredIsRefusedBeforeTheCall(t *testing.T) {
	t.Parallel()
	data := []byte{0xff, 0xd8, 0x21, 0x22}
	req := Request{
		Model:     ModelClef,
		Images:    []Image{{ContentType: "image/jpeg", Bytes: data}},
		State:     jsonRaw(t, map[string]string{"occasion": "date"}),
		Questions: sampleQuestions(t),
	}
	ctx := context.Background()

	for _, name := range []string{"other bytes", "empty file", "unreadable name"} {
		fake := newFakeInner()
		fake.set(noulResponse(0.5))
		dir := filepath.Join(t.TempDir(), "clef")
		recorder := newRecorder(t, RecorderConfig{Inner: fake, Dir: dir})
		path := filepath.Join(dir, "images", digestName(data)+".jpg")
		switch name {
		case "other bytes":
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte{0x00, 0x01}, 0o644); err != nil {
				t.Fatal(err)
			}
		case "empty file":
			// The shape replay.Open fails a whole directory on.
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, nil, 0o644); err != nil {
				t.Fatal(err)
			}
		case "unreadable name":
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		_, err := recorder.Record(ctx, "judge-date", req)
		if err == nil || !strings.Contains(err.Error(), "images/") {
			t.Fatalf("%s: the digest path was accepted: %v", name, err)
		}
		if fake.count() != 0 {
			t.Fatalf("%s: the refusal spent %d inner calls", name, fake.count())
		}
		if _, err := os.Stat(filepath.Join(dir, "judge-date.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s: a refused call left an exchange behind", name)
		}
		if name == "other bytes" {
			if got := readFile(t, path); !bytes.Equal(got, []byte{0x00, 0x01}) {
				t.Fatalf("%s: the refusal rewrote someone else's file", name)
			}
		}
	}
}

// TestAnAnswerTheReplayCannotServeStoresNothing: an inner client that
// answers with nothing a replay could read is a failure, not a recording.
func TestAnAnswerTheReplayCannotServeStoresNothing(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "clef")
	fake := newFakeInner()
	recorder := newRecorder(t, RecorderConfig{Inner: fake, Dir: dir})
	req := sampleRequest(t, nil)
	ctx := context.Background()

	for _, resp := range []Response{
		{},
		{Model: ModelClef},
		{Model: ModelClef, Answers: map[string]Answer{"met": {Type: TypeNoul}}},
		{Model: ModelClef, Answers: map[string]Answer{"met": {Type: "essay", Noul: ptr(0.5)}}},
	} {
		fake.set(resp)
		if _, err := recorder.Record(ctx, "judge-date", req); err == nil {
			t.Fatalf("a reply nothing could replay was stored: %#v", resp)
		}
		assertNothingWritten(t, dir)
	}
	if fake.count() != 4 {
		t.Fatalf("calls %d, want 4", fake.count())
	}
}

// TestAskNamesTheExchangeByRequestKey: Ask has one argument the corpus
// cannot invent a name from, so it stores under the request's own key
// and refuses the second copy of it.
func TestAskNamesTheExchangeByRequestKey(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "clef")
	fake := newFakeInner()
	want := noulResponse(0.55)
	fake.set(want)
	recorder := newRecorder(t, RecorderConfig{Inner: fake, Dir: dir})
	req := sampleRequest(t, nil)
	ctx := context.Background()

	got, err := recorder.Ask(ctx, req)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%v %#v", err, got)
	}
	name := mustKey(t, req)
	if names := fileNames(t, dir); !reflect.DeepEqual(names, []string{name + ".json"}) {
		t.Fatalf("named %v, want the request key", names)
	}
	if _, err := openDir(t, dir).Ask(ctx, req); err != nil {
		t.Fatal(err)
	}
	_, err = recorder.Ask(ctx, req)
	var exists *AlreadyRecorded
	if !errors.As(err, &exists) || exists.Exchange != name {
		t.Fatalf("a repeat of one request is one name: %v", err)
	}
	if fake.count() != 1 {
		t.Fatalf("the refused repeat still called the client %d times", fake.count())
	}
}

// TestANoImageCallStoresNoImagesKey, the shape of the corpus's
// cost-V04-family-function-noimage.json: a call that sent no image
// records no images field, so the live body and the recording hash alike.
func TestANoImageCallStoresNoImagesKey(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "clef")
	fake := newFakeInner()
	fake.set(noulResponse(0.5))
	req := sampleRequest(t, nil)
	if len(req.Images) != 0 {
		t.Fatal("this test needs a request with no image")
	}
	if _, err := newRecorder(t, RecorderConfig{Inner: fake, Dir: dir}).
		Record(context.Background(), "cost-V04-noimage", req); err != nil {
		t.Fatal(err)
	}
	body := objectAt(t, readFile(t, filepath.Join(dir, "cost-V04-noimage.json")), "request")
	fields := keySet(t, objectAt(t, body, "body"))
	if !reflect.DeepEqual(fields, []string{"model", "questions", "state"}) {
		t.Fatalf("body keys %v", fields)
	}
	replay := openDir(t, dir)
	served, err := replay.Serve(http.MethodPost, runPath(req.Model), liveBody(t, req))
	if err != nil || served.Key != "cost-V04-noimage" {
		t.Fatalf("%v", err)
	}
	if _, err := replay.Ask(context.Background(), req); err != nil {
		t.Fatal(err)
	}
}

// TestTwoCallsNamingOneExchangeDoNotShareTheFile holds a name across a
// live call and asks for it again while it is held.
func TestTwoCallsNamingOneExchangeDoNotShareTheFile(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "clef")
	fake := newFakeInner()
	fake.set(noulResponse(0.5))
	fake.entered = make(chan struct{}, 1)
	fake.release = make(chan struct{})
	recorder := newRecorder(t, RecorderConfig{Inner: fake, Dir: dir})
	req := sampleRequest(t, nil)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		_, err := recorder.Record(ctx, "judge-date", req)
		done <- err
	}()
	<-fake.entered
	_, err := recorder.Record(ctx, "judge-date", req)
	if err == nil || !strings.Contains(err.Error(), "another call") {
		t.Fatalf("the held name was taken too: %v", err)
	}
	close(fake.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if names := fileNames(t, dir); !reflect.DeepEqual(names, []string{"judge-date.json"}) {
		t.Fatalf("%v", names)
	}
	if _, err := openDir(t, dir).Ask(ctx, req); err != nil {
		t.Fatal(err)
	}
}

// TestNewRecorderIsValidated: it needs a client and a directory, and it
// records what it was given as an absolute path.
func TestNewRecorderIsValidated(t *testing.T) {
	t.Parallel()
	fake := newFakeInner()
	if _, err := NewRecorder(RecorderConfig{Dir: t.TempDir()}); err == nil {
		t.Fatal("a recorder with no inner client")
	}
	if _, err := NewRecorder(RecorderConfig{Inner: fake}); err == nil {
		t.Fatal("a recorder with no directory")
	}
	dir := filepath.Join(t.TempDir(), "clef")
	recorder, err := NewRecorder(RecorderConfig{Inner: fake, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(recorder.Dir()) || recorder.Dir() != filepath.Clean(dir) {
		t.Fatalf("dir %q", recorder.Dir())
	}
	if _, err := MatchImages(filepath.Join(t.TempDir(), "not-there")); err == nil {
		t.Fatal("an image root that is not there")
	}
	if _, err := MatchImages(""); err == nil {
		t.Fatal("no image root named")
	}
}

func ptr(v float64) *float64 { return &v }

// assertNothingWritten requires the directory to hold no file and no
// folder, at any depth: a refused or failed call leaves nothing behind,
// not even an image nobody references.
func assertNothingWritten(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("%s holds %d files after a call that stored nothing", dir, len(entries))
	}
}

func fileNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func walk(t *testing.T, root string, fn func(path string, data []byte)) {
	t.Helper()
	err := filepath.Walk(root, func(path string, entry os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fn(path, data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func canonicalBytes(t *testing.T, raw json.RawMessage) json.RawMessage {
	t.Helper()
	canon, err := canonicalJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return canon
}

func objectAt(t *testing.T, raw json.RawMessage, key string) json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	value, ok := fields[key]
	if !ok {
		t.Fatalf("no %q in %s", key, raw)
	}
	return value
}

func stringAt(t *testing.T, raw json.RawMessage, key string) string {
	t.Helper()
	var value string
	if err := json.Unmarshal(objectAt(t, raw, key), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func keySet(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("not a JSON object: %v (%s)", err, raw)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// imageRefs lists every recorded image's file, in order, and refuses an
// image that is not a plain content type plus file.
func imageRefs(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var body struct {
		Images []struct {
			ContentType string `json:"content_type"`
			File        string `json:"file"`
			Base64      json.RawMessage
		} `json:"images"`
	}
	if err := json.Unmarshal(objectAt(t, objectAt(t, raw, "request"), "body"), &body); err != nil {
		t.Fatal(err)
	}
	refs := make([]string, 0, len(body.Images))
	for i, image := range body.Images {
		if image.ContentType == "" || image.File == "" {
			t.Fatalf("image %d is not a reference: %#v", i, image)
		}
		if len(image.Base64) != 0 {
			t.Fatalf("image %d carries bytes as well as a reference", i)
		}
		if filepath.IsAbs(image.File) {
			t.Fatalf("image %d names %s, outside the corpus", i, image.File)
		}
		refs = append(refs, image.File)
	}
	return refs
}

// assertSameEnvelopeShape compares a fresh recording against a committed
// one: the same fields in the same places, the same request document,
// and the same stored reply but for the milliseconds the call took.
func assertSameEnvelopeShape(t *testing.T, committed, fresh string) {
	t.Helper()
	corpusRaw := readFile(t, committed)
	freshRaw := readFile(t, fresh)
	if got, want := keySet(t, freshRaw), keySet(t, corpusRaw); !reflect.DeepEqual(got, want) {
		t.Fatalf("top level keys %v, want %v", got, want)
	}
	if got, want := stringAt(t, freshRaw, "exchange"), stringAt(t, corpusRaw, "exchange"); got != want {
		t.Fatalf("exchange %q, want %q", got, want)
	}
	corpusRequest := objectAt(t, corpusRaw, "request")
	freshRequest := objectAt(t, freshRaw, "request")
	if got, want := keySet(t, freshRequest), keySet(t, corpusRequest); !reflect.DeepEqual(got, want) {
		t.Fatalf("request keys %v, want %v", got, want)
	}
	if !bytes.Equal(canonicalBytes(t, freshRequest), canonicalBytes(t, corpusRequest)) {
		t.Fatalf("the fresh request is not the committed one:\nfresh  %s\ncorpus %s", freshRequest, corpusRequest)
	}
	corpusResponse := objectAt(t, corpusRaw, "response")
	freshResponse := objectAt(t, freshRaw, "response")
	if got, want := keySet(t, freshResponse), keySet(t, corpusResponse); !reflect.DeepEqual(got, want) {
		t.Fatalf("response keys %v, want %v", got, want)
	}
	if got, want := keySet(t, objectAt(t, freshResponse, "body")), keySet(t, objectAt(t, corpusResponse, "body")); !reflect.DeepEqual(got, want) {
		t.Fatalf("response body keys %v, want %v", got, want)
	}
	if got, want := keySet(t, objectAt(t, objectAt(t, freshResponse, "body"), "result")),
		keySet(t, objectAt(t, objectAt(t, corpusResponse, "body"), "result")); !reflect.DeepEqual(got, want) {
		t.Fatalf("result keys %v, want %v", got, want)
	}
	if got := string(objectAt(t, freshResponse, "status")); got != string(objectAt(t, corpusResponse, "status")) {
		t.Fatalf("status %s, want %s", got, objectAt(t, corpusResponse, "status"))
	}
	drop := func(raw json.RawMessage) json.RawMessage {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, "elapsed_ms")
		return jsonRaw(t, fields)
	}
	if !bytes.Equal(canonicalBytes(t, drop(freshResponse)), canonicalBytes(t, drop(corpusResponse))) {
		t.Fatalf("the stored reply is not the committed one but for its timing:\nfresh  %s\ncorpus %s",
			freshResponse, corpusResponse)
	}
}
