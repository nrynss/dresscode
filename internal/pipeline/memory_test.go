package pipeline

// The memory tests prove the stage on the recorded Lambo exchanges
// (testdata/lambo), the way the youcam wire tests face their recordings:
// a local harness serves the recorded replies to the live memory client,
// which never dials a serve. The fake in internal/memory is not used
// here, and the reason is worth keeping: it keys a recall by session and
// query, and the recordings hold two for the same pair - the live answer
// (11) and the refusal an erased session answers (16) - so the fake
// would refuse to choose. The wire does not have that problem: the two
// requests carry different bodies, and each exchange replays once.
//
// The tests prove the done-when end to end: the story's first visit
// writes the kept and dismissed facts (the recorded derive, its receipt
// confirmed applied), the second visit recalls for the occasion (the
// recorded recall), and the full look runs with that recall in the
// request - the kept fix offered first, the dismissed fix not offered at
// all. History off is pinned as zero Lambo interaction, and both failure
// arms are pinned: a recall that fails reads as a first visit and the
// run continues, a remember that fails leaves the stored verdict standing.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/memory"
	"github.com/nrynss/dresscode/internal/systemone"
)

// storyToken stands in for the Lambo bearer. It is never a real key, and
// the harness refuses anything else.
const storyToken = "pipeline-memory-test-token"

// storySession is the account session the recordings were taken on. The
// probe erased it on the live serve; the replay never dials, and the
// memory package's own tests replay the same ids. The sentence the
// contract serves is pick_test.go's storyLine, the same constant the
// pick tests pin.
const storySession = "dc-u-probe-a"

// storyKeptSentence and storyDismissedSentence are the recorded concept
// texts the first visit's write replays (05-derive-a).
const (
	storyKeptSentence      = "Family function kept the gold jhumkas."
	storyDismissedSentence = "Family function dismissed the red lip."
)

// occasionFix returns one of an occasion's fixes by id.
func occasionFix(t *testing.T, occ *domain.Occasion, id string) *domain.Fix {
	t.Helper()
	for i := range occ.Fixes {
		if occ.Fixes[i].ID == id {
			return &occ.Fixes[i]
		}
	}
	t.Fatalf("occasion %s has no fix %s", occ.ID, id)
	return nil
}

// lamboSSE is one event of a recorded text/event-stream reply.
type lamboSSE struct {
	ID    *string         `json:"id"`
	Retry *int            `json:"retry"`
	Data  json.RawMessage `json:"data"`
}

// lamboExchange is one recorded Lambo call: the request it matches on
// and the reply it serves.
type lamboExchange struct {
	name     string
	method   string
	path     string
	rpc      string
	tool     string
	query    string
	concepts []memory.Concept
	receipt  string
	confirm  string
	auth     bool
	status   int
	ctype    string
	mcp      string
	sse      []lamboSSE
	body     json.RawMessage

	used bool
}

// lamboReplay serves the recorded exchanges of testdata/lambo to the
// live memory client over a local server. A call matches the first
// unused recording with its method, path, JSON-RPC method and tool -
// and, for the tools that carry one, the same query, concept list,
// receipt or erase confirmation. The JSON-RPC id of every reply is
// rewritten to the caller's, because the ids are the caller's counter,
// not the recording's. An exchange serves once; a call no recording
// matches is a 404, which is the failure arm's fuel.
type lamboReplay struct {
	mu        sync.Mutex
	exchanges []*lamboExchange
	skip      map[string]bool
	served    []string
	server    *httptest.Server
}

// openLamboReplay loads testdata/lambo and starts the harness. skip
// names exchanges the harness must not serve, so a test can stand in the
// slot where a recording would have answered.
func openLamboReplay(t *testing.T, skip map[string]bool) *lamboReplay {
	t.Helper()
	dir := filepath.Join(repoRoot(t), "testdata", "lambo")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	rep := &lamboReplay{skip: skip}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		rec, err := loadLamboExchange(strings.TrimSuffix(entry.Name(), ".json"), raw)
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		rep.exchanges = append(rep.exchanges, rec)
	}
	sort.Slice(rep.exchanges, func(i, j int) bool { return rep.exchanges[i].name < rep.exchanges[j].name })
	server := httptest.NewServer(rep)
	t.Cleanup(server.Close)
	rep.server = server
	return rep
}

// openLamboClient is the live memory client pointed at the harness.
func openLamboClient(t *testing.T, rep *lamboReplay, agent string) *memory.Client {
	t.Helper()
	client, err := memory.New(memory.Config{BaseURL: rep.server.URL, Token: storyToken, AgentID: agent})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func loadLamboExchange(name string, raw []byte) (*lamboExchange, error) {
	var env struct {
		Exchange string `json:"exchange"`
		Request  struct {
			Method  string            `json:"method"`
			Path    string            `json:"path"`
			Headers map[string]string `json:"headers"`
			Body    json.RawMessage   `json:"body"`
		} `json:"request"`
		Response struct {
			Status  int               `json:"status"`
			Headers map[string]string `json:"headers"`
			Body    json.RawMessage   `json:"body"`
			SSE     []lamboSSE        `json:"sse"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if env.Exchange == "" || env.Request.Method == "" || env.Request.Path == "" {
		return nil, errors.New("exchange, method and path are required")
	}
	rec := &lamboExchange{
		name:   env.Exchange,
		method: env.Request.Method,
		path:   env.Request.Path,
		auth:   env.Request.Headers["authorization"] != "",
		status: env.Response.Status,
		ctype:  env.Response.Headers["content-type"],
		mcp:    env.Response.Headers["mcp-session-id"],
		sse:    env.Response.SSE,
		body:   env.Response.Body,
	}
	var call struct {
		ID      *int   `json:"id"`
		Method  string `json:"method"`
		Confirm string `json:"confirm"`
		Params  struct {
			Name      string `json:"name"`
			Arguments struct {
				Concepts []memory.Concept `json:"concepts"`
				Query    string           `json:"query"`
				Receipt  string           `json:"receipt"`
			} `json:"arguments"`
		} `json:"params"`
	}
	if json.Unmarshal(env.Request.Body, &call) == nil {
		rec.rpc = call.Method
		rec.tool = call.Params.Name
		rec.query = call.Params.Arguments.Query
		rec.concepts = call.Params.Arguments.Concepts
		rec.receipt = call.Params.Arguments.Receipt
		rec.confirm = call.Confirm
	}
	return rec, nil
}

func (r *lamboReplay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if req.Header.Get("Authorization") != "Bearer "+storyToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, 4<<20))
	if err != nil {
		http.Error(w, "body", http.StatusBadRequest)
		return
	}
	var call struct {
		ID      *int   `json:"id"`
		Method  string `json:"method"`
		Confirm string `json:"confirm"`
		Params  struct {
			Name      string `json:"name"`
			Arguments struct {
				Concepts []memory.Concept `json:"concepts"`
				Query    string           `json:"query"`
				Receipt  string           `json:"receipt"`
			} `json:"arguments"`
		} `json:"params"`
	}
	_ = json.Unmarshal(body, &call)

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rec := range r.exchanges {
		if rec.used || r.skip[rec.name] {
			continue
		}
		if rec.method != req.Method || rec.path != req.URL.Path {
			continue
		}
		// The no-token recording of the same initialize (01) must not
		// answer a call that carried the bearer, and the other way.
		if rec.auth != (req.Header.Get("Authorization") != "") {
			continue
		}
		if rec.rpc != call.Method || rec.tool != call.Params.Name {
			continue
		}
		switch rec.tool {
		case "lambo_recall":
			if rec.query != call.Params.Arguments.Query {
				continue
			}
		case "lambo_derive":
			if !sameConceptList(rec.concepts, call.Params.Arguments.Concepts) {
				continue
			}
		case "lambo_stats":
			if rec.receipt != call.Params.Arguments.Receipt {
				continue
			}
		default:
			if rec.confirm != "" && rec.confirm != call.Confirm {
				continue
			}
		}
		rec.used = true
		r.served = append(r.served, rec.name)
		r.write(w, rec, call.ID)
		return
	}
	http.Error(w, "no recording for "+req.URL.Path, http.StatusNotFound)
}

// write serves one recorded reply, the reply's JSON-RPC id rewritten to
// the caller's.
func (r *lamboReplay) write(w http.ResponseWriter, rec *lamboExchange, id *int) {
	if rec.ctype != "" {
		w.Header().Set("Content-Type", rec.ctype)
	}
	if rec.mcp != "" {
		w.Header().Set("Mcp-Session-Id", rec.mcp)
	}
	w.WriteHeader(rec.status)
	if len(rec.sse) > 0 {
		for _, ev := range rec.sse {
			if ev.ID != nil {
				fmt.Fprintf(w, "id: %s\n", *ev.ID)
			}
			if ev.Retry != nil {
				fmt.Fprintf(w, "retry: %d\n", *ev.Retry)
			}
			data := strings.TrimSpace(string(ev.Data))
			if data == "" || data == "null" {
				fmt.Fprintf(w, "data:\n\n")
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", rewriteRPCID(ev.Data, id))
		}
		return
	}
	if line := rewriteRPCID(rec.body, id); line != nil {
		_, _ = w.Write(line)
	}
}

// rewriteRPCID compacts one JSON-RPC message and sets its id to the
// caller's, so a recorded reply answers the call that asked for it.
func rewriteRPCID(raw json.RawMessage, id *int) []byte {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var msg map[string]json.RawMessage
	if json.Unmarshal(raw, &msg) != nil {
		return []byte(trimmed)
	}
	if _, ok := msg["id"]; !ok || id == nil {
		return []byte(trimmed)
	}
	encoded, err := json.Marshal(*id)
	if err != nil {
		return []byte(trimmed)
	}
	msg["id"] = encoded
	out, err := json.Marshal(msg)
	if err != nil {
		return []byte(trimmed)
	}
	return out
}

func sameConceptList(a, b []memory.Concept) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// servedList returns the exchange names the harness served, in order.
func (r *lamboReplay) servedList() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.served...)
}

// assertServed pins the exchanges a flow replayed, in order: the write's
// open, derive and receipt; the recall; nothing else.
func assertServed(t *testing.T, rep *lamboReplay, want ...string) {
	t.Helper()
	got := rep.servedList()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("served %v, want %v", got, want)
	}
}

// keptFirstStub is the model's documented reading of a recall, stood in
// where no pick recording exists (docs/clef.md): when the request's
// state carries fixes the user kept, it answers the first kept fix the
// request actually offered; without a recall in the state it answers
// what it was told to. T3.2 built the mechanism this stub assumes - the
// recall line travels verbatim and the model prefers the kept fix from
// it - and T3.9 measures the real model. The stub makes the story's
// causality testable offline: the kept fix is offered first because the
// recall travelled, and the control run without it is what proves that.
type keptFirstStub struct {
	inner *lookStub
}

func (s *keptFirstStub) Ask(ctx context.Context, req systemone.Request) (systemone.Response, error) {
	resp, err := s.inner.Ask(ctx, req)
	if err != nil || !hasQuestion(req, pickQuestionID) {
		return resp, err
	}
	var state pickState
	if json.Unmarshal(req.State, &state) != nil || state.Recall == nil {
		return resp, err
	}
	var options map[string]string
	if json.Unmarshal(req.Questions[pickQuestionID].Criteria, &options) != nil {
		return resp, err
	}
	answer, ok := resp.Answers[pickQuestionID]
	if !ok || answer.Type != systemone.TypeChoice {
		return resp, err
	}
	for _, kept := range state.Recall.Kept {
		if _, offered := options[kept]; !offered {
			continue
		}
		if answer.Choice != kept {
			answer.Choice = kept
			resp.Answers[pickQuestionID] = answer
		}
		break
	}
	return resp, err
}

// TestTheSecondVisitStoryRunsOnRecordings is the done-when. The first
// visit keeps the jhumkas and dismisses the red lip; the write replays
// the recorded derive and its applied receipt. The second visit recalls
// for the occasion; the recorded recall comes back, and the sentence and
// the ids are the contract's. The look then runs whole on the run rig
// with that recall in the request: the kept fix is the pick, the red lip
// is not among the offered options at all, and the pick frame carries
// the sentence. The control visit runs the same story with history off -
// no recall in the request, the stub's own answer standing - and it is
// the proof that the kept-first offer came from this stage's recall and
// not from the data's menu order.
func TestTheSecondVisitStoryRunsOnRecordings(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	occ := loadOccasions(t)["family-function"]
	jhumkas := occasionFix(t, occ, "family-gold-jhumkas")
	lip := occasionFix(t, occ, "family-red-lip")

	// The sentences the write composes are the recorded ones.
	keptConcept, err := factConcept(occ, Fact{Fix: jhumkas, Verdict: domain.VerdictKept})
	if err != nil {
		t.Fatal(err)
	}
	if keptConcept.Content != storyKeptSentence || keptConcept.ConceptType != memory.ConceptObservation {
		t.Fatalf("kept concept %+v, want %q as an observation", keptConcept, storyKeptSentence)
	}
	dismissedConcept, err := factConcept(occ, Fact{Fix: lip, Verdict: domain.VerdictDismissed})
	if err != nil {
		t.Fatal(err)
	}
	if dismissedConcept.Content != storyDismissedSentence {
		t.Fatalf("dismissed concept %q, want %q", dismissedConcept.Content, storyDismissedSentence)
	}

	// FIRST VISIT. The user kept the jhumkas render and dismissed the red
	// lip. The verdict handler writes the verdict that just landed; the
	// story's two facts ride one write because that is the recorded
	// exchange - the probe wrote the pair in one derive, and the replay
	// matches the concept list exactly. The call returns only after the
	// receipt reports applied.
	mem := openLamboReplay(t, nil)
	client := openLamboClient(t, mem, "dresscode-probe")
	err = Remember(t.Context(), client, storySession, occ,
		Fact{Fix: jhumkas, Verdict: domain.VerdictKept},
		Fact{Fix: lip, Verdict: domain.VerdictDismissed},
	)
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	assertServed(t, mem, "02-initialize-a", "03-initialized-a", "05-derive-a", "06-stats-wait-a")

	// SECOND VISIT. The look-start wiring recalls for the occasion before
	// a fix is picked.
	recall, err := RecallFor(t.Context(), client, storySession, occ)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	assertServed(t, mem, "02-initialize-a", "03-initialized-a", "05-derive-a", "06-stats-wait-a", "11-recall-a-family-function")
	if recall.Line != storyLine {
		t.Fatalf("line %q, want the contract's %q", recall.Line, storyLine)
	}
	if strings.Join(recall.Kept, ",") != "family-gold-jhumkas" {
		t.Fatalf("kept %v, want the jhumkas fix", recall.Kept)
	}
	if strings.Join(recall.Dismissed, ",") != "family-red-lip" {
		t.Fatalf("dismissed %v, want the lip fix", recall.Dismissed)
	}

	// THE LOOK. The full run, the recall in the request, the red lip
	// ticked so its absence from the options is the stage's doing.
	slots := []string{
		recordedSlotID(t, rep, v04Selfie),
		recordedSlotID(t, rep, v04Product),
	}
	shown := checkAnswer{change: 0.99, same: 0.99, met: 0.99}
	story := newEarringStory(t, rep, 1, []checkAnswer{shown}, recall, slots...)
	story.req.OptIn = append(story.req.OptIn, "family-festive-top")
	story.rig.clef.picks = []pickAnswer{{
		// The answer the model gives without a recall: the runner-up of
		// the menu's own order. The stub replaces it with the kept fix
		// only because the state carries one.
		choice: "family-festive-top",
		probs:  map[string]float64{"family-gold-jhumkas": 0.9, "family-festive-top": 0.3},
	}}
	story.deps.Clef = &keptFirstStub{inner: story.rig.clef}

	look, err := Run(t.Context(), story.deps, story.req, story.rig.events.publish)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// The stream is the contract's frozen stage list. The memory stage
	// adds no frame: the recall rides the pick frame that is already
	// there.
	wantStages := []string{stageJudge, stagePick, stageMeasure, stageRender, stageCheck, stageShop}
	if got := stagesOf(t, story.rig.events); !reflect.DeepEqual(got, wantStages) {
		t.Fatalf("stages %v, want %v", got, wantStages)
	}
	events := story.rig.events.all()
	pickFixture := frameOf(t, readContract(t, "job-events.json"), stagePick)
	assertShape(t, stagePick, pickFixture, detail(t, events[1]))
	var pickGot pickEvent
	if err := json.Unmarshal(events[1].Detail, &pickGot); err != nil {
		t.Fatal(err)
	}
	if pickGot.Recall != storyLine || pickGot.Fix != "family-gold-jhumkas" || pickGot.RunnerUp != "family-festive-top" {
		t.Fatalf("pick %+v, want the kept fix first with the sentence", pickGot)
	}

	// What the model was asked: the state carried the memory word for
	// word, and the red lip was never among the options - not first, not
	// runner-up, not on the wire.
	var asked pickState
	if err := json.Unmarshal(lastPickRequest(t, story.rig.clef).State, &asked); err != nil {
		t.Fatal(err)
	}
	if asked.Recall == nil || asked.Recall.Line != storyLine {
		t.Fatalf("pick state recall %+v, want the sentence", asked.Recall)
	}
	if strings.Join(asked.Recall.Kept, ",") != "family-gold-jhumkas" || strings.Join(asked.Recall.Dismissed, ",") != "family-red-lip" {
		t.Fatalf("pick state ids %+v, want the kept jhumkas and the dismissed lip", asked.Recall)
	}
	var options map[string]string
	if err := json.Unmarshal(lastPickRequest(t, story.rig.clef).Questions[pickQuestionID].Criteria, &options); err != nil {
		t.Fatal(err)
	}
	if _, offered := options["family-red-lip"]; offered {
		t.Fatalf("options %v, want the dismissed fix off the wire", options)
	}

	// The look: the kept fix offered first, the runner-up the menu's own
	// next fix - never the dismissed one.
	if look.FixID != "family-gold-jhumkas" {
		t.Fatalf("fix %q, want the kept jhumkas first", look.FixID)
	}
	if look.RunnerUpID != "family-festive-top" {
		t.Fatalf("runner-up %q, want the festive top - the dismissed lip is not the fallback", look.RunnerUpID)
	}
	if look.Shown == nil || look.Shown.FixID != "family-gold-jhumkas" {
		t.Fatalf("look %+v, want the jhumkas render shown", look)
	}

	// THE CONTROL. The same story, history off: no recall is read, none
	// is written, and the stub's own answer stands - the festive top, not
	// the kept fix. Whatever the story run gained, this run shows it came
	// from the recall and not from the menu's order.
	ctl := openLamboReplay(t, nil)
	counted := &countedMemory{Memory: openLamboClient(t, ctl, "dresscode-probe")}
	ctlSlots := []string{
		recordedSlotID(t, rep, "cloth-upload-src"),
		recordedSlotID(t, rep, "cloth-upload-ref"),
	}
	shownTop := checkAnswer{change: 0.99, same: 0.99, met: 0.99}
	control := newEarringStory(t, rep, 2, []checkAnswer{shownTop}, nil, ctlSlots...)
	control.req.OptIn = append(control.req.OptIn, "family-festive-top")
	control.rig.clef.picks = []pickAnswer{{
		choice: "family-festive-top",
		probs:  map[string]float64{"family-gold-jhumkas": 0.9, "family-festive-top": 0.3},
	}}
	control.deps.Clef = &keptFirstStub{inner: control.rig.clef}

	ctlLook, err := Run(t.Context(), control.deps, control.req, control.rig.events.publish)
	if err != nil {
		t.Fatalf("control run: %v", err)
	}
	if ctlLook.FixID != "family-festive-top" {
		t.Fatalf("control fix %q, want the menu's own answer standing without a recall", ctlLook.FixID)
	}
	state := control.rig.clef.lastPickState(t)
	if state.Recall != nil {
		t.Fatalf("control pick state recall %+v, want none with history off", state.Recall)
	}
	if counted.remembers != 0 || counted.recalls != 0 {
		t.Fatalf("history off made %d writes and %d reads, want none", counted.remembers, counted.recalls)
	}
	assertServed(t, ctl) // the control's harness served nothing at all
}

// lastPickRequest returns the latest pick request the stub captured.
func lastPickRequest(t *testing.T, stub *lookStub) systemone.Request {
	t.Helper()
	stub.mu.Lock()
	defer stub.mu.Unlock()
	for i := len(stub.asks) - 1; i >= 0; i-- {
		if hasQuestion(stub.asks[i], pickQuestionID) {
			return stub.asks[i]
		}
	}
	t.Fatal("the stub saw no pick request")
	return systemone.Request{}
}

// countedMemory counts the calls a wiring makes, so a test can pin zero.
type countedMemory struct {
	memory.Memory

	mu        sync.Mutex
	remembers int
	recalls   int
}

func (c *countedMemory) Remember(ctx context.Context, session string, concepts []memory.Concept) (memory.Remembered, error) {
	c.mu.Lock()
	c.remembers++
	c.mu.Unlock()
	return c.Memory.Remember(ctx, session, concepts)
}

func (c *countedMemory) Recall(ctx context.Context, session, occasion string) (memory.Recall, error) {
	c.mu.Lock()
	c.recalls++
	c.mu.Unlock()
	return c.Memory.Recall(ctx, session, occasion)
}

// noMemoryCalls is a memory client that must never be reached. The
// history-off refusals run before any call, the judge's empty-photo
// lesson: reaching this client fails the pin, not the call.
type noMemoryCalls struct {
	mu    sync.Mutex
	calls int
}

func (m *noMemoryCalls) count() {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
}

func (m *noMemoryCalls) Remember(context.Context, string, []memory.Concept) (memory.Remembered, error) {
	m.count()
	return memory.Remembered{}, errors.New("noMemoryCalls: a write reached the client")
}

func (m *noMemoryCalls) Recall(context.Context, string, string) (memory.Recall, error) {
	m.count()
	return memory.Recall{}, errors.New("noMemoryCalls: a read reached the client")
}

func (m *noMemoryCalls) Erase(context.Context, string) (memory.EraseResult, error) {
	m.count()
	return memory.EraseResult{}, errors.New("noMemoryCalls: an erase reached the client")
}

// TestHistoryOffTouchesNoMemory pins the history-off clause. A wiring
// with history off has no session and no client to pass, and the stage
// refuses both shapes before any call - so a mis-wired call touches
// nothing either. The first-visit look then runs whole: the request
// carries no recall, the pick frame carries none, and the counted client
// the wiring would use counts zero writes and zero reads, its harness
// having served nothing.
func TestHistoryOffTouchesNoMemory(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	occ := loadOccasions(t)["family-function"]
	jhumkas := occasionFix(t, occ, "family-gold-jhumkas")
	quiet := &noMemoryCalls{}

	refused := func(err error, want string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v, want it to name %q", err, want)
		}
	}
	recalled := func(client memory.Memory, session string, o *domain.Occasion) error {
		_, err := RecallFor(t.Context(), client, session, o)
		return err
	}
	refused(Remember(t.Context(), nil, storySession, occ, Fact{Fix: jhumkas, Verdict: domain.VerdictKept}), "no memory client")
	refused(Remember(t.Context(), quiet, "", occ, Fact{Fix: jhumkas, Verdict: domain.VerdictKept}), "no session")
	refused(Remember(t.Context(), quiet, storySession, nil, Fact{Fix: jhumkas, Verdict: domain.VerdictKept}), "no occasion")
	refused(Remember(t.Context(), quiet, storySession, occ), "no verdict to remember")
	refused(Remember(t.Context(), quiet, storySession, occ, Fact{Fix: nil, Verdict: domain.VerdictKept}), "names no fix")
	refused(Remember(t.Context(), quiet, storySession, occ, Fact{Fix: jhumkas, Verdict: ""}), "not one the memory keeps")
	refused(recalled(nil, storySession, occ), "no memory client")
	refused(recalled(quiet, "", occ), "no session")
	refused(recalled(quiet, storySession, nil), "no occasion")
	if quiet.calls != 0 {
		t.Fatalf("%d calls reached the client through the refusals, want none", quiet.calls)
	}

	// The first visit, history off: the whole look, no memory anywhere.
	mem := openLamboReplay(t, nil)
	counted := &countedMemory{Memory: openLamboClient(t, mem, "dresscode-probe")}
	slots := []string{
		recordedSlotID(t, rep, v04Selfie),
		recordedSlotID(t, rep, v04Product),
	}
	shown := checkAnswer{change: 0.99, same: 0.99, met: 0.99}
	story := newEarringStory(t, rep, 1, []checkAnswer{shown}, nil, slots...)
	story.rig.clef.picks = []pickAnswer{{
		choice: "family-gold-jhumkas",
		probs:  map[string]float64{"family-red-lip": 0.4},
	}}

	look, err := Run(t.Context(), story.deps, story.req, story.rig.events.publish)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if look.Shown == nil || look.FixID != "family-gold-jhumkas" {
		t.Fatalf("look %+v, want the first visit's jhumkas render shown", look)
	}

	// The pick frame is the contract's history-off arm: fix and
	// runner-up, no recall key.
	events := story.rig.events.all()
	pickFixture := frameOf(t, readContract(t, "job-events-suggestion.json"), stagePick)
	assertShape(t, stagePick, pickFixture, detail(t, events[1]))
	var pickGot pickEvent
	if err := json.Unmarshal(events[1].Detail, &pickGot); err != nil {
		t.Fatal(err)
	}
	if pickGot.Recall != "" {
		t.Fatalf("pick recall %q, want none with history off", pickGot.Recall)
	}

	// Nothing was read, nothing was written, nothing was served.
	if counted.remembers != 0 || counted.recalls != 0 {
		t.Fatalf("history off made %d writes and %d reads, want none", counted.remembers, counted.recalls)
	}
	assertServed(t, mem)
}

// TestAFailedRecallIsAFirstVisit pins the read's failure posture: the
// session an operator erased answers the recorded refusal, the stage
// returns it typed and the wiring's arm starts the look with no recall.
// The look then runs whole - a Lambo failure does not fail a look - and
// the only thing missing from its stream is the sentence.
func TestAFailedRecallIsAFirstVisit(t *testing.T) {
	t.Parallel()
	rep := openRenderReplay(t)
	occ := loadOccasions(t)["family-function"]

	// The live answer (11) is stood out of the way, so the first recall
	// of this session meets the recorded refusal (16) the way a session
	// erased between visits does.
	mem := openLamboReplay(t, map[string]bool{"11-recall-a-family-function": true})
	client := openLamboClient(t, mem, "dresscode-probe")

	_, err := RecallFor(t.Context(), client, storySession, occ)
	if !errors.Is(err, memory.ErrErased) {
		t.Fatalf("recall error %v, want the typed erased session", err)
	}
	if !strings.Contains(err.Error(), "family-function") {
		t.Fatalf("error %v, want it to name the occasion", err)
	}
	assertServed(t, mem, "02-initialize-a", "03-initialized-a", "16-recall-a-after-erase")

	// The wiring's arm: on an error, no recall. The look runs whole.
	slots := []string{
		recordedSlotID(t, rep, "cloth-upload-src"),
		recordedSlotID(t, rep, "cloth-upload-ref"),
	}
	shown := checkAnswer{change: 0.99, same: 0.99, met: 0.99}
	story := newEarringStory(t, rep, 2, []checkAnswer{shown}, nil, slots...)
	story.req.OptIn = append(story.req.OptIn, "family-festive-top")
	story.rig.clef.picks = []pickAnswer{{
		choice: "family-festive-top",
		probs:  map[string]float64{"family-gold-jhumkas": 0.9, "family-festive-top": 0.3},
	}}

	look, err := Run(t.Context(), story.deps, story.req, story.rig.events.publish)
	if err != nil {
		t.Fatalf("run after a failed recall: %v", err)
	}
	if look.Shown == nil || look.FixID != "family-festive-top" {
		t.Fatalf("look %+v, want the look finished on its own answer", look)
	}
	events := story.rig.events.all()
	var pickGot pickEvent
	if err := json.Unmarshal(events[1].Detail, &pickGot); err != nil {
		t.Fatal(err)
	}
	if pickGot.Recall != "" {
		t.Fatalf("pick recall %q, want the failed memory absent from the frame", pickGot.Recall)
	}
}

// TestAFailedRememberLeavesTheVerdictStanding pins the write's failure
// posture: a write no recording answers fails typed, the error names the
// occasion it was for, and the verdict's answer to the contract is
// remembered false - the look the verdict belongs to is already stored
// and stands either way.
func TestAFailedRememberLeavesTheVerdictStanding(t *testing.T) {
	t.Parallel()
	occ := loadOccasions(t)["family-function"]
	jhumkas := occasionFix(t, occ, "family-gold-jhumkas")

	// The after-erase initialize (17) stands out of the way, so the
	// retry an expired-id arm runs finds no recording and the replay
	// answers 404: the client then reports the write it sent once and
	// never confirmed.
	mem := openLamboReplay(t, map[string]bool{"17-initialize-a-after-erase": true})
	client := openLamboClient(t, mem, "dresscode-probe")

	// One fact alone matches no recording: the recorded derive carries
	// the story's pair, and the replay refuses to invent the rest. The
	// write fails; the client does not send it twice.
	err := Remember(t.Context(), client, storySession, occ, Fact{Fix: jhumkas, Verdict: domain.VerdictKept})
	if !errors.Is(err, memory.ErrUnconfirmed) {
		t.Fatalf("remember error %v, want the typed unconfirmed write", err)
	}
	if !strings.Contains(err.Error(), "family-function") {
		t.Fatalf("error %v, want it to name the occasion", err)
	}

	// The verdict handler's mapping: remembered = err == nil.
	remembered := err == nil
	if remembered {
		t.Fatal("a failed write must answer remembered false")
	}
}

// hitsMemory is a recall that answers a hand-built hit list, for the
// line and the mapping rules that need no wire.
type hitsMemory struct {
	hits []memory.Hit
	err  error
}

func (m *hitsMemory) Remember(context.Context, string, []memory.Concept) (memory.Remembered, error) {
	return memory.Remembered{}, errors.New("hitsMemory: nothing to remember")
}

func (m *hitsMemory) Recall(context.Context, string, string) (memory.Recall, error) {
	return memory.Recall{Hits: m.hits}, m.err
}

func (m *hitsMemory) Erase(context.Context, string) (memory.EraseResult, error) {
	return memory.EraseResult{}, errors.New("hitsMemory: nothing to erase")
}

func hitOf(content string) memory.Hit {
	return memory.Hit{Content: content, ConceptType: "Observation", Score: 0.65}
}

// TestTheRecallReadsTheSessionTexts pins what the recall half keeps from
// a session's hits: the occasion's own facts in the format the write
// composes, nothing else, with the rules that make the sentence safe -
// dismissal beats an older keep, a bought fix counts as kept for the
// pick, a hit of another occasion or of a fix gone is left out, the same
// fact twice is one fact, and the sentence does not follow the session's
// rank order. A typed failure passes through.
func TestTheRecallReadsTheSessionTexts(t *testing.T) {
	t.Parallel()
	occ := loadOccasions(t)["family-function"]

	recall := func(t *testing.T, hits ...memory.Hit) *Recall {
		t.Helper()
		out, err := RecallFor(t.Context(), &hitsMemory{hits: hits}, storySession, occ)
		if err != nil {
			t.Fatalf("recall: %v", err)
		}
		return out
	}

	// The story pair, in the session's own rank order - the dismissed hit
	// outranks the kept one on the recording, and the sentence still says
	// kept first.
	got := recall(t, hitOf(storyDismissedSentence), hitOf(storyKeptSentence))
	if got.Line != storyLine || strings.Join(got.Kept, ",") != "family-gold-jhumkas" || strings.Join(got.Dismissed, ",") != "family-red-lip" {
		t.Fatalf("recall %+v, want the contract's line and ids", got)
	}

	// Several kept, one dismissed: the occasion's own order inside each
	// verdict, kept before dismissed.
	got = recall(t,
		hitOf("Family function dismissed the red lip."),
		hitOf("Family function kept the festive top."),
		hitOf("Family function kept the gold jhumkas."),
	)
	if want := "Last family function you kept the gold jhumkas and the festive top and dismissed the red lip."; got.Line != want {
		t.Fatalf("line %q, want %q", got.Line, want)
	}
	if strings.Join(got.Kept, ",") != "family-gold-jhumkas,family-festive-top" || strings.Join(got.Dismissed, ",") != "family-red-lip" {
		t.Fatalf("ids %+v, want the occasion's own order", got)
	}

	// A bought fix counts as kept for the pick's ranking, and the
	// sentence says bought.
	got = recall(t, hitOf("Family function bought the gold jhumkas."))
	if got.Line != "Last family function you bought the gold jhumkas." || strings.Join(got.Kept, ",") != "family-gold-jhumkas" || len(got.Dismissed) != 0 {
		t.Fatalf("recall %+v, want the bought fix ranked kept and said bought", got)
	}

	// A keep that a later visit dismissed is a dismissal: the cost of
	// wrongly offering again is the thing the user said no to.
	got = recall(t, hitOf(storyKeptSentence), hitOf(storyDismissedSentence), hitOf("Family function kept the red lip."))
	if strings.Join(got.Kept, ",") != "family-gold-jhumkas" || strings.Join(got.Dismissed, ",") != "family-red-lip" {
		t.Fatalf("recall %+v, want the later dismissal to win", got)
	}
	if got.Line != storyLine {
		t.Fatalf("line %q, want only the winning verdicts", got.Line)
	}

	// Another occasion's fact and a fix the occasion no longer carries
	// are left out; only the session's own noise stays out of the ids and
	// the sentence alike.
	got = recall(t,
		hitOf("Date kept the open collar."),
		hitOf("Family function kept the silver studs."),
		hitOf(storyKeptSentence),
	)
	if got.Line != "Last family function you kept the gold jhumkas." || strings.Join(got.Kept, ",") != "family-gold-jhumkas" || len(got.Dismissed) != 0 {
		t.Fatalf("recall %+v, want the foreign and stale facts out", got)
	}

	// The same fact twice is one fact.
	got = recall(t, hitOf(storyKeptSentence), hitOf(storyKeptSentence))
	if strings.Join(got.Kept, ",") != "family-gold-jhumkas" {
		t.Fatalf("kept %v, want the repeated hit counted once", got.Kept)
	}

	// Nothing of this occasion: the empty recall the empty-memory answer
	// serves.
	got = recall(t, hitOf("Date kept the open collar."))
	if got == nil || got.Line != "" || len(got.Kept) != 0 || len(got.Dismissed) != 0 {
		t.Fatalf("recall %+v, want the empty recall", got)
	}

	// A typed failure passes through named: an erased session is not an
	// empty history.
	_, err := RecallFor(t.Context(), &hitsMemory{err: memory.ErrErased}, storySession, occ)
	if !errors.Is(err, memory.ErrErased) || !strings.Contains(err.Error(), "family-function") {
		t.Fatalf("error %v, want the typed erasure naming the occasion", err)
	}
}

// TestEveryVerdictWordsAndInverts sweeps all five occasions' menus: every
// fix, under each of the three verdicts, composes a sentence in the
// stage's format and parses back to the same fix and the same verdict.
// The sentence carries the occasion's name, the verdict and the fix in
// words - no id, no score, no number - so a fix whose id does not carry
// its occasion's prefix is refused, not remembered as an id.
func TestEveryVerdictWordsAndInverts(t *testing.T) {
	t.Parallel()
	occasions := loadOccasions(t)
	ids := make([]string, 0, len(occasions))
	for id := range occasions {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	verdicts := []domain.Verdict{domain.VerdictKept, domain.VerdictDismissed, domain.VerdictBought}
	total := 0
	for _, id := range ids {
		occ := occasions[id]
		for i := range occ.Fixes {
			fix := &occ.Fixes[i]
			for _, verdict := range verdicts {
				concept, err := factConcept(occ, Fact{Fix: fix, Verdict: verdict})
				if err != nil {
					t.Fatalf("%s on %s: %v", fix.ID, occ.ID, err)
				}
				if concept.ConceptType != memory.ConceptObservation {
					t.Fatalf("%s is a %q, want an observation", concept.Content, concept.ConceptType)
				}
				if !strings.HasPrefix(concept.Content, occ.Name+" ") || !strings.HasSuffix(concept.Content, ".") {
					t.Fatalf("sentence %q, want the occasion's name and a full stop", concept.Content)
				}
				if strings.Contains(concept.Content, fix.ID) {
					t.Fatalf("sentence %q carries the id %q, and no sentence may", concept.Content, fix.ID)
				}
				fact, ok := parseFact(occ, concept.Content)
				if !ok || fact.Fix.ID != fix.ID || fact.Verdict != verdict {
					t.Fatalf("sentence %q read back as %+v, want %s %s", concept.Content, fact, fix.ID, verdict)
				}
				total++
			}
		}
	}
	// The five menus hold twenty-eight fixes; three verdicts each.
	if total != 28*3 {
		t.Fatalf("%d sentences, want the twenty-eight fixes' three verdicts", total)
	}

	// A fix outside its occasion's naming convention is refused before
	// any call: the sentence would have to carry an id, and none may.
	stray := &domain.Occasion{ID: "gala", Name: "Gala"}
	fact := Fact{Fix: &domain.Fix{ID: "evening-shawl"}, Verdict: domain.VerdictKept}
	if _, err := factConcept(stray, fact); err == nil || !strings.Contains(err.Error(), "evening-shawl") {
		t.Fatalf("error %v, want the unwordable fix refused by name", err)
	}
	if _, err := factConcept(nil, fact); err == nil {
		t.Fatal("a fact without an occasion composed")
	}
}
