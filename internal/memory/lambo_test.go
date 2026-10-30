package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/keel/erase"
	"github.com/nrynss/keel/job"
	jobsql "github.com/nrynss/keel/job/sqlitestore"
	"github.com/nrynss/keel/sqlite"
	"github.com/nrynss/keel/stream"
)

const testToken = "dresscode-test-token"

func TestClientReplaysLamboRecordings(t *testing.T) {
	t.Parallel()
	rep := newReplay(t, replayConfig{})
	client := openClient(t, rep.url, "dresscode-probe")
	ctx := context.Background()

	keptA := []Concept{
		{Content: "Family function kept the gold jhumkas.", ConceptType: ConceptObservation},
		{Content: "Family function dismissed the red lip.", ConceptType: ConceptObservation},
	}
	keptB := []Concept{
		{Content: "Family function kept the silver studs.", ConceptType: ConceptObservation},
		{Content: "Family function dismissed the braid.", ConceptType: ConceptObservation},
	}

	gotA, err := client.Remember(ctx, "dc-u-probe-a", keptA)
	if err != nil {
		failErr(t, err)
	}
	if gotA.State != "applied" || gotA.Receipt != "lwr1.b46ea9a412cbde22.1a18ad1b725.1" || gotA.AgentID != "dresscode-probe" {
		t.Fatalf("%#v", gotA)
	}
	if gotA.CreatedCount != 2 || gotA.Embedded != 2 || !strings.Contains(gotA.Summary, "applied") || !strings.Contains(gotA.Summary, "2 created") {
		t.Fatalf("%#v", gotA)
	}
	if strings.Join(gotA.Created, ",") != "609855c8-b924-406a-ad73-4f1ec22463d9,1c8a743b-e85b-41f8-9ed8-78e2769d489a" {
		t.Fatalf("%#v", gotA.Created)
	}

	gotB, err := client.Remember(ctx, "dc-u-probe-b", keptB)
	if err != nil {
		failErr(t, err)
	}
	if gotB.State != "applied" || gotB.Receipt != "lwr1.691cdf3390a0d9f4.1a18ad3d2c5.1" || gotB.CreatedCount != 2 || gotB.Embedded != 2 {
		t.Fatalf("%#v", gotB)
	}

	recallA, err := client.Recall(ctx, "dc-u-probe-a", "family function")
	if err != nil {
		failErr(t, err)
	}
	assertLook(t, recallA, "gold jhumkas", "red lip", "silver studs", "braid")

	recallB, err := client.Recall(ctx, "dc-u-probe-b", "family function")
	if err != nil {
		failErr(t, err)
	}
	assertLook(t, recallB, "silver studs", "braid", "gold jhumkas", "red lip")

	crossed, err := client.Recall(ctx, "dc-u-probe-a", "silver studs")
	if err != nil {
		failErr(t, err)
	}
	assertLook(t, crossed, "gold jhumkas", "red lip", "silver studs", "braid")

	crossedB, err := client.Recall(ctx, "dc-u-probe-b", "gold jhumkas")
	if err != nil {
		failErr(t, err)
	}
	assertLook(t, crossedB, "silver studs", "braid", "gold jhumkas", "")

	first, err := client.Erase(ctx, "dc-u-probe-a")
	if err != nil {
		failErr(t, err)
	}
	if first.AlreadyAbsent || first.Session != "dc-u-probe-a" || first.Removed.Concepts != 2 || first.Removed.Vectors != 2 || first.FenceToken != 2 {
		t.Fatalf("%#v", first)
	}

	erased, err := client.Recall(ctx, "dc-u-probe-a", "family function")
	if !errors.Is(err, ErrErased) || len(erased.Hits) != 0 {
		t.Fatalf("%#v %v", erased, err)
	}
	again, err := client.Recall(ctx, "dc-u-probe-a", "family function")
	if !errors.Is(err, ErrErased) || len(again.Hits) != 0 {
		t.Fatalf("%#v %v", again, err)
	}

	second, err := client.Erase(ctx, "dc-u-probe-a")
	if err != nil {
		failErr(t, err)
	}
	if !second.AlreadyAbsent || second.Removed != (Removed{}) || second.FenceToken != first.FenceToken || second.Session != "dc-u-probe-a" {
		t.Fatalf("%#v", second)
	}

	still, err := client.Recall(ctx, "dc-u-probe-b", "family function")
	if err != nil {
		failErr(t, err)
	}
	assertLook(t, still, "silver studs", "braid", "gold jhumkas", "red lip")

	erasedB, err := client.Erase(ctx, "dc-u-probe-b")
	if err != nil {
		failErr(t, err)
	}
	if erasedB.AlreadyAbsent || erasedB.Removed.Concepts != 2 || erasedB.Removed.Vectors != 2 {
		t.Fatalf("%#v", erasedB)
	}

	calls := rep.calls()
	want := []string{
		"02-initialize-a",
		"03-initialized-a",
		"05-derive-a",
		"06-stats-wait-a",
		"07-initialize-b",
		"08-initialized-b",
		"09-derive-b",
		"10-stats-wait-b",
		"11-recall-a-family-function",
		"12-recall-b-family-function",
		"13-recall-a-other-look",
		"14-recall-b-other-look",
		"15-erase-a",
		"16-recall-a-after-erase",
		"17-initialize-a-after-erase",
		"18-erase-a-again",
		"19-recall-b-after-a-erased",
		"20-erase-b",
	}
	var got []string
	derives := 0
	var mcpA, mcpB string
	for _, call := range calls {
		got = append(got, call.Exchange)
		if strings.Contains(string(call.Body), testToken) || bytes.Contains(call.Body, []byte("created_at")) || bytes.Contains(call.Body, []byte("record_action")) || bytes.Contains(call.Body, []byte("tools/list")) {
			t.Fatalf("call %s carried a forbidden field", call.Exchange)
		}
		if call.Accept != "application/json, text/event-stream" || call.ContentType != "application/json" {
			t.Fatalf("%s headers %q %q", call.Exchange, call.Accept, call.ContentType)
		}
		switch call.Exchange {
		case "02-initialize-a", "07-initialize-b":
			if call.RequestMCP != "" {
				t.Fatal("initialize sent a session")
			}
			if !bytes.Contains(call.Body, []byte(`"protocolVersion":"2025-06-18"`)) || !bytes.Contains(call.Body, []byte(`"name":"dresscode"`)) || bytes.Contains(call.Body, []byte("dresscode-t07")) {
				t.Fatalf("%s initialize body", call.Exchange)
			}
			if call.Exchange == "02-initialize-a" {
				mcpA = call.ResponseMCP
			} else {
				mcpB = call.ResponseMCP
			}
		case "03-initialized-a", "08-initialized-b":
			if bytes.Contains(call.Body, []byte(`"id"`)) || !bytes.Contains(call.Body, []byte(`"notifications/initialized"`)) {
				t.Fatalf("%s notification", call.Exchange)
			}
		case "17-initialize-a-after-erase":
			if call.RequestMCP != "" {
				t.Fatal("fresh initialize reused the erased mcp session")
			}
		case "05-derive-a", "09-derive-b":
			derives++
			if !strings.Contains(string(call.Body), `"agent_id":"dresscode-probe"`) {
				t.Fatalf("%s agent", call.Exchange)
			}
		case "06-stats-wait-a":
			if !call.WaitSet || call.WaitMS != 30000 || !strings.Contains(string(call.Body), gotA.Receipt) {
				t.Fatalf("%#v", call)
			}
		case "10-stats-wait-b":
			if !call.WaitSet || call.WaitMS != 30000 || !strings.Contains(string(call.Body), gotB.Receipt) {
				t.Fatalf("%#v", call)
			}
		case "11-recall-a-family-function", "12-recall-b-family-function", "13-recall-a-other-look", "14-recall-b-other-look", "16-recall-a-after-erase", "19-recall-b-after-a-erased":
			if !call.TopSet || call.TopK != 10 || !call.MaxSet || call.MaxTokens != 2000 {
				t.Fatalf("%#v", call)
			}
		}
		if strings.HasPrefix(call.Path, "/admin/") && call.RequestMCP != "" {
			t.Fatalf("%s sent an mcp session", call.Exchange)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("exchanges\n got %s\nwant %s", strings.Join(got, ","), strings.Join(want, ","))
	}
	if derives != 2 {
		t.Fatalf("derives %d", derives)
	}
	if mcpA == "" || mcpB == "" || mcpA == mcpB {
		t.Fatalf("mcp %q %q", mcpA, mcpB)
	}
	for _, call := range calls {
		switch call.Exchange {
		case "03-initialized-a", "05-derive-a", "06-stats-wait-a", "11-recall-a-family-function", "13-recall-a-other-look", "16-recall-a-after-erase":
			if call.RequestMCP != mcpA {
				t.Fatalf("%s mcp", call.Exchange)
			}
		case "08-initialized-b", "09-derive-b", "10-stats-wait-b", "12-recall-b-family-function", "14-recall-b-other-look", "19-recall-b-after-a-erased":
			if call.RequestMCP != mcpB {
				t.Fatalf("%s mcp", call.Exchange)
			}
		}
	}
	if rep.mismatch != 0 {
		t.Fatal("authorization mismatch")
	}
}

func TestRememberSendsOneWriteWhenTheReceiptFails(t *testing.T) {
	t.Parallel()
	rep := newReplay(t, replayConfig{failStats: true})
	client := openClient(t, rep.url, "")
	if client.agentID != DefaultAgentID {
		t.Fatal(client.agentID)
	}
	_, err := client.Remember(context.Background(), "dc-u-probe-a", []Concept{
		{Content: "Family function kept the gold jhumkas.", ConceptType: ConceptObservation},
		{Content: "Family function dismissed the red lip.", ConceptType: ConceptObservation},
	})
	if !errors.Is(err, ErrUnconfirmed) {
		failErr(t, err)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatal("error contains the token")
	}
	derives, stats := 0, 0
	for _, call := range rep.calls() {
		if strings.Contains(string(call.Body), testToken) {
			t.Fatal("token in body")
		}
		switch call.Tool {
		case "lambo_derive":
			derives++
			if !strings.Contains(string(call.Body), `"agent_id":"dresscode"`) {
				t.Fatal("default agent")
			}
		case "lambo_stats":
			stats++
		}
	}
	if derives != 1 || stats != 1 {
		t.Fatalf("derive %d stats %d", derives, stats)
	}
}

func TestEraseTargetConfirmsGone(t *testing.T) {
	t.Parallel()
	rep := newReplay(t, replayConfig{})
	client := openClient(t, rep.url, "dresscode-probe")
	target := client.Target("dc-u-probe-a")
	if target.Name() != TargetName {
		t.Fatal(target.Name())
	}

	ctx := context.Background()
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "jobs.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := jobsql.Open(ctx, jobsql.Config{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	er, err := erase.New(func(context.Context, string) ([]erase.Target, error) {
		return []erase.Target{target}, nil
	}, erase.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := job.Open(ctx, job.Config{
		Broker: stream.New(stream.Config{}),
		Store:  store,
		Kinds:  map[string]job.Kind{erase.KindName: er.Kind()},
	})
	if err != nil {
		t.Fatal(err)
	}

	first := runErasure(t, er, runner, "dc-u-probe-a", target)
	second := runErasure(t, er, runner, "dc-u-probe-a", target)
	if first.Status != job.StatusDone || second.Status != job.StatusDone {
		t.Fatalf("%s %v / %s %v", first.Status, first.Err, second.Status, second.Err)
	}
	for _, id := range []string{first.ID, second.ID} {
		rep, err := er.Inspect(ctx, runner, id)
		if err != nil {
			t.Fatal(err)
		}
		if !rep.Complete() || len(rep.Confirmed) != 1 || rep.Confirmed[0] != TargetName || rep.Ref != "dc-u-probe-a" {
			t.Fatalf("%#v", rep)
		}
	}
	calls := rep.calls()
	if len(calls) != 2 || calls[0].Exchange != "15-erase-a" || calls[1].Exchange != "18-erase-a-again" {
		t.Fatalf("%d calls, first %s second %s", len(calls), exchangeAt(calls, 0), exchangeAt(calls, 1))
	}
}

func TestRepeatedEraseReportsGone(t *testing.T) {
	t.Parallel()
	var gone *recording
	for _, rec := range loadRecordings(t, lamboDir(t)) {
		if rec.exchange == "18-erase-a-again" {
			gone = rec
		}
	}
	if gone == nil {
		t.Fatal("missing gone recording")
	}
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		calls++
		writeRecording(w, gone, nil)
	}))
	t.Cleanup(srv.Close)
	client := openClient(t, srv.URL, "")
	ctx := context.Background()
	first, err := client.Erase(ctx, "dc-u-probe-a")
	if err != nil {
		failErr(t, err)
	}
	if !first.AlreadyAbsent || first.Removed != (Removed{}) || first.FenceToken != 2 {
		t.Fatalf("%#v", first)
	}
	if err := client.Target("dc-u-probe-a").Delete(ctx); !errors.Is(err, erase.ErrGone) {
		failErr(t, err)
	}
	if err := client.Target("dc-u-probe-a").Delete(ctx); !errors.Is(err, erase.ErrGone) || strings.Contains(err.Error(), testToken) {
		failErr(t, err)
	}
	if calls != 3 {
		t.Fatalf("erase calls %d", calls)
	}
}

func TestUnauthorizedRecordingHidesTheToken(t *testing.T) {
	t.Parallel()
	rep := newReplay(t, replayConfig{refuse: true})
	client := openClient(t, rep.url, "dresscode-probe")
	_, err := client.Recall(context.Background(), "dc-u-probe-a", "family function")
	if !errors.Is(err, ErrUnauthorized) {
		failErr(t, err)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatal("error contains the token")
	}
	calls := rep.calls()
	if len(calls) != 1 || calls[0].Exchange != "01-initialize-no-token" {
		t.Fatalf("%#v", calls)
	}
	if rep.mismatch != 0 {
		t.Fatal("client did not send the bearer token")
	}
}

// TestRecallSurvivesSessionExpiry drives the expiry the feedback log
// records: Lambo expires idle sessions in minutes, and a call on the
// dead Mcp-Session-Id is an HTTP 404 plain text, not a JSON-RPC error.
// The client initializes the session again — one re-initialize, the same
// open a first call runs — and the replayed recall succeeds.
func TestRecallSurvivesSessionExpiry(t *testing.T) {
	t.Parallel()
	rep := newReplay(t, replayConfig{script: []string{
		"02-initialize-a",
		"03-initialized-a",
		"21-call-on-expired-session",
		"02-initialize-a",
		"03-initialized-a",
		"11-recall-a-family-function",
	}})
	client := openClient(t, rep.url, "dresscode-probe")
	got, err := client.Recall(context.Background(), "dc-u-probe-a", "family function")
	if err != nil {
		failErr(t, err)
	}
	assertLook(t, got, "gold jhumkas", "red lip", "silver studs", "braid")

	calls := rep.calls()
	want := []string{
		"02-initialize-a",
		"03-initialized-a",
		"21-call-on-expired-session",
		"02-initialize-a",
		"03-initialized-a",
		"11-recall-a-family-function",
	}
	if len(calls) != len(want) {
		t.Fatalf("%d calls", len(calls))
	}
	for i, call := range calls {
		if call.Exchange != want[i] {
			t.Fatalf("call %d is %s, want %s", i, call.Exchange, want[i])
		}
	}
	first := calls[0].ResponseMCP
	if first == "" {
		t.Fatal("no session from the first initialize")
	}
	if calls[2].RequestMCP != first {
		t.Fatalf("the expired call carried %q", calls[2].RequestMCP)
	}
	if calls[2].RPC != "tools/call" || calls[2].Tool != "lambo_recall" {
		t.Fatalf("%s %s", calls[2].RPC, calls[2].Tool)
	}
	if calls[3].RequestMCP != "" {
		t.Fatal("the re-initialize reused the expired session")
	}
	second := calls[3].ResponseMCP
	if second == "" {
		t.Fatal("no session from the re-initialize")
	}
	if calls[4].RequestMCP != second || calls[5].RequestMCP != second {
		t.Fatalf("the replayed leg carried %q and %q", calls[4].RequestMCP, calls[5].RequestMCP)
	}
}

// TestSecondConsecutiveExpiryFailsTyped replays the 404 to the replayed
// call too. The recall fails ErrSessionExpired, and no third attempt is
// sent.
func TestSecondConsecutiveExpiryFailsTyped(t *testing.T) {
	t.Parallel()
	rep := newReplay(t, replayConfig{script: []string{
		"02-initialize-a",
		"03-initialized-a",
		"21-call-on-expired-session",
		"02-initialize-a",
		"03-initialized-a",
		"21-call-on-expired-session",
	}})
	client := openClient(t, rep.url, "dresscode-probe")
	_, err := client.Recall(context.Background(), "dc-u-probe-a", "family function")
	if !errors.Is(err, ErrSessionExpired) || strings.Contains(err.Error(), testToken) {
		failErr(t, err)
	}
	calls := rep.calls()
	if len(calls) != 6 {
		t.Fatalf("%d calls, want no third attempt", len(calls))
	}
	if calls[2].Exchange != "21-call-on-expired-session" || calls[5].Exchange != "21-call-on-expired-session" {
		t.Fatalf("the 404s are %s and %s", calls[2].Exchange, calls[5].Exchange)
	}
	if calls[3].RequestMCP != "" {
		t.Fatal("the re-initialize reused the expired session")
	}
	if calls[5].RequestMCP != calls[3].ResponseMCP {
		t.Fatalf("the replayed call carried %q", calls[5].RequestMCP)
	}
}

// TestRememberReportsAnExpiredSessionTyped pins the error identity of a
// remember whose derive is refused twice: ErrSessionExpired, not
// ErrUnconfirmed. A derive refused at the session was never sent, which
// is not a lost confirmation. The recorded 404 reply is plain text with
// no JSON-RPC in it, so the one recording carries it for every tool
// call; only its response is replayed here.
func TestRememberReportsAnExpiredSessionTyped(t *testing.T) {
	t.Parallel()
	rep := newReplay(t, replayConfig{script: []string{
		"02-initialize-a",
		"03-initialized-a",
		"21-call-on-expired-session",
		"02-initialize-a",
		"03-initialized-a",
		"21-call-on-expired-session",
	}})
	client := openClient(t, rep.url, "dresscode-probe")
	kept := []Concept{
		{Content: "Family function kept the gold jhumkas.", ConceptType: ConceptObservation},
		{Content: "Family function dismissed the red lip.", ConceptType: ConceptObservation},
	}
	_, err := client.Remember(context.Background(), "dc-u-probe-a", kept)
	if !errors.Is(err, ErrSessionExpired) || errors.Is(err, ErrUnconfirmed) {
		failErr(t, err)
	}
	if calls := rep.calls(); len(calls) != 6 {
		t.Fatalf("%d calls, want no third attempt", len(calls))
	}
}

// TestANotifyExpiryDropsTheDeadSession pins the open leg. Initialize
// mints a session id and the notifications/initialized on it is refused
// with the recorded 404. The call fails ErrSessionExpired and the dead
// id is dropped, so the next call runs a fresh initialize and succeeds:
// its notification and its recall carry the id that second initialize
// returned. Without the drop the gate would keep the dead id, and every
// later call would retry only the notification on it, refusing typed
// forever.
func TestANotifyExpiryDropsTheDeadSession(t *testing.T) {
	t.Parallel()
	rep := newReplay(t, replayConfig{script: []string{
		"02-initialize-a",
		"21-call-on-expired-session",
		"02-initialize-a",
		"03-initialized-a",
		"11-recall-a-family-function",
	}})
	client := openClient(t, rep.url, "dresscode-probe")
	ctx := context.Background()
	_, err := client.Recall(ctx, "dc-u-probe-a", "family function")
	if !errors.Is(err, ErrSessionExpired) || strings.Contains(err.Error(), testToken) {
		failErr(t, err)
	}
	got, err := client.Recall(ctx, "dc-u-probe-a", "family function")
	if err != nil {
		failErr(t, err)
	}
	assertLook(t, got, "gold jhumkas", "red lip", "silver studs", "braid")

	calls := rep.calls()
	want := []string{
		"02-initialize-a",
		"21-call-on-expired-session",
		"02-initialize-a",
		"03-initialized-a",
		"11-recall-a-family-function",
	}
	if len(calls) != len(want) {
		t.Fatalf("%d calls", len(calls))
	}
	for i, call := range calls {
		if call.Exchange != want[i] {
			t.Fatalf("call %d is %s, want %s", i, call.Exchange, want[i])
		}
	}
	dead := calls[0].ResponseMCP
	if dead == "" {
		t.Fatal("no session from the first initialize")
	}
	if calls[1].RPC != "notifications/initialized" || calls[1].RequestMCP != dead {
		t.Fatalf("%s carried %q %q", calls[1].Exchange, calls[1].RPC, calls[1].RequestMCP)
	}
	fresh := calls[2].ResponseMCP
	if fresh == "" {
		t.Fatal("no session from the second initialize")
	}
	if calls[2].RequestMCP != "" {
		t.Fatal("the second initialize reused the refused session")
	}
	if calls[3].RPC != "notifications/initialized" || calls[3].RequestMCP != fresh {
		t.Fatalf("%s carried %q %q", calls[3].Exchange, calls[3].RPC, calls[3].RequestMCP)
	}
	if calls[4].Tool != "lambo_recall" || calls[4].RequestMCP != fresh {
		t.Fatalf("%s carried %q %q", calls[4].Exchange, calls[4].Tool, calls[4].RequestMCP)
	}
}

func TestClientRejectsBeforeDialing(t *testing.T) {
	t.Parallel()
	var dialed bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		dialed = true
	}))
	t.Cleanup(srv.Close)
	client := openClient(t, srv.URL, "dresscode-probe")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	concepts := []Concept{{Content: "Family function kept the gold jhumkas.", ConceptType: ConceptObservation}}
	if _, err := client.Remember(ctx, "dc-u-probe-a", concepts); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := client.Recall(ctx, "dc-u-probe-a", "family function"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := client.Erase(ctx, "dc-u-probe-a"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	live := context.Background()
	if _, err := client.Remember(live, "probe-a", concepts); err == nil {
		t.Fatal("bad session")
	}
	if _, err := client.Remember(live, "dc-u-probe-a", nil); err == nil {
		t.Fatal("no concepts")
	}
	if _, err := client.Remember(live, "dc-u-probe-a", []Concept{{Content: "x", ConceptType: "nope"}}); err == nil {
		t.Fatal("bad type")
	}
	if _, err := client.Recall(live, "dc-u-probe-a", ""); err == nil {
		t.Fatal("empty occasion")
	}
	if _, err := client.Erase(live, "dc-u-"); err == nil {
		t.Fatal("short session")
	}
	if err := client.Target("nope").Delete(live); err == nil {
		t.Fatal("bad target")
	}
	if dialed {
		t.Fatal("dialed")
	}

	if _, err := New(Config{}); err == nil {
		t.Fatal("empty config")
	}
	if _, err := New(Config{BaseURL: "ftp://127.0.0.1", Token: testToken}); err == nil {
		t.Fatal("scheme")
	}
	if _, err := New(Config{BaseURL: "http://user@" + "127.0.0.1", Token: testToken}); err == nil {
		t.Fatal("userinfo")
	}
	if _, err := New(Config{BaseURL: "http://127.0.0.1/" + testToken, Token: testToken}); err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatal("url accepted the token or the error repeated it")
	}
}

func assertLook(t *testing.T, got Recall, wantA, wantB, absentA, absentB string) {
	t.Helper()
	if len(got.Hits) != 2 || len(got.Warnings) != 0 || got.Context == "" {
		t.Fatalf("%#v", got)
	}
	var text string
	for _, hit := range got.Hits {
		text += hit.Content + "\n"
		if hit.ConceptType != "Observation" || hit.Score == 0 || hit.NodeID == "" || hit.IsCanonical || hit.BlastRadius != nil {
			t.Fatalf("%#v", hit)
		}
	}
	if !strings.Contains(text, wantA) || (wantB != "" && !strings.Contains(text, wantB)) {
		t.Fatalf("%q", text)
	}
	if absentA != "" && strings.Contains(text, absentA) {
		t.Fatalf("leaked %s in %q", absentA, text)
	}
	if absentB != "" && strings.Contains(text, absentB) {
		t.Fatalf("leaked %s in %q", absentB, text)
	}
}

func runErasure(t *testing.T, er *erase.Eraser, runner *job.Runner, ref string, target erase.Target) job.Result {
	t.Helper()
	ctx := context.Background()
	id, err := er.Start(ctx, runner, ref, []erase.Target{target})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		res, err := runner.Result(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != job.StatusRunning && res.Status != job.StatusQueued {
			return res
		}
		if time.Now().After(deadline) {
			t.Fatal("erasure still running")
		}
		time.Sleep(time.Millisecond)
	}
}

func openClient(t *testing.T, base, agent string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: base, Token: testToken, AgentID: agent})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func failErr(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("nil error")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatal("error contains the token")
	}
	t.Fatal(err)
}

func exchangeAt(calls []replayCall, i int) string {
	if i >= len(calls) {
		return ""
	}
	return calls[i].Exchange
}

type replayConfig struct {
	refuse    bool
	failStats bool
	// script names the exchange served for each request, in order. It
	// is set for the session-expiry tests, whose sequence re-uses one
	// initialize recording and answers a call with the recorded 404.
	script []string
}

type replay struct {
	url       string
	token     string
	refuse    bool
	failStats bool
	script    []string
	step      int
	mu        sync.Mutex
	recs      []*recording
	log       []replayCall
	mismatch  int
}

type recording struct {
	exchange string
	method   string
	path     string
	hasAuth  bool
	rpc      string
	tool     string
	query    string
	receipt  string
	confirm  string
	concepts []Concept
	status   int
	ctype    string
	wwwAuth  string
	mcp      string
	sse      []sseEvent
	body     json.RawMessage
	bodyText string
	used     bool
}

type sseEvent struct {
	ID    *string         `json:"id"`
	Retry *int            `json:"retry"`
	Data  json.RawMessage `json:"data"`
}

type replayCall struct {
	Exchange    string
	Path        string
	RPC         string
	Tool        string
	Accept      string
	ContentType string
	RequestMCP  string
	ResponseMCP string
	WaitMS      int
	WaitSet     bool
	TopK        int
	TopSet      bool
	MaxTokens   int
	MaxSet      bool
	Body        []byte
}

func newReplay(t *testing.T, cfg replayConfig) *replay {
	t.Helper()
	rep := &replay{
		token:     testToken,
		refuse:    cfg.refuse,
		failStats: cfg.failStats,
		script:    cfg.script,
		recs:      loadRecordings(t, lamboDir(t)),
	}
	srv := httptest.NewServer(rep)
	t.Cleanup(srv.Close)
	rep.url = srv.URL
	return rep
}

func (s *replay) calls() []replayCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]replayCall, len(s.log))
	copy(out, s.log)
	return out
}

func (s *replay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxResponse+1))
	if err != nil || int64(len(body)) > maxResponse {
		http.Error(w, "body", http.StatusBadRequest)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.refuse {
		rec := s.findExchange("01-initialize-no-token")
		s.note(r, body, rec)
		writeRecording(w, rec, nil)
		return
	}
	if len(s.script) > 0 {
		s.mu.Lock()
		i := s.step
		s.step++
		s.mu.Unlock()
		if i >= len(s.script) {
			http.Error(w, "script is exhausted", http.StatusConflict)
			return
		}
		rec := s.findExchange(s.script[i])
		s.note(r, body, rec)
		writeRecording(w, rec, parseCall(body).id)
		return
	}
	parsed := parseCall(body)
	if s.failStats && parsed.tool == "lambo_stats" {
		s.note(r, body, &recording{tool: parsed.tool, rpc: parsed.rpc, path: r.URL.Path})
		http.Error(w, "confirmation failed", http.StatusBadGateway)
		return
	}
	rec, ok := s.match(r.Method, r.URL.Path, parsed)
	if !ok {
		http.Error(w, fmt.Sprintf("no recording for %s %s %s %s", r.Method, r.URL.Path, parsed.rpc, parsed.tool), http.StatusNotFound)
		return
	}
	s.note(r, body, rec)
	writeRecording(w, rec, parsed.id)
}

func (s *replay) authorized(r *http.Request) bool {
	if r.Header.Get("Authorization") == "Bearer "+s.token {
		return true
	}
	s.mu.Lock()
	s.mismatch++
	s.mu.Unlock()
	return false
}

func (s *replay) findExchange(name string) *recording {
	for _, rec := range s.recs {
		if rec.exchange == name {
			return rec
		}
	}
	return nil
}

type parsedCall struct {
	id        *int
	rpc       string
	tool      string
	query     string
	receipt   string
	confirm   string
	concepts  []Concept
	waitMS    int
	waitSet   bool
	topK      int
	topSet    bool
	maxTokens int
	maxSet    bool
}

func parseCall(body []byte) parsedCall {
	var env struct {
		ID      *int   `json:"id"`
		Method  string `json:"method"`
		Confirm string `json:"confirm"`
		Params  struct {
			Name      string `json:"name"`
			Arguments struct {
				Concepts  []Concept `json:"concepts"`
				Query     string    `json:"query"`
				Receipt   string    `json:"receipt"`
				WaitMS    *int      `json:"wait_ms"`
				TopK      *int      `json:"top_k"`
				MaxTokens *int      `json:"max_tokens"`
			} `json:"arguments"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &env) != nil {
		return parsedCall{}
	}
	out := parsedCall{
		id:       env.ID,
		rpc:      env.Method,
		tool:     env.Params.Name,
		query:    env.Params.Arguments.Query,
		receipt:  env.Params.Arguments.Receipt,
		confirm:  env.Confirm,
		concepts: env.Params.Arguments.Concepts,
	}
	if env.Params.Arguments.WaitMS != nil {
		out.waitSet = true
		out.waitMS = *env.Params.Arguments.WaitMS
	}
	if env.Params.Arguments.TopK != nil {
		out.topSet = true
		out.topK = *env.Params.Arguments.TopK
	}
	if env.Params.Arguments.MaxTokens != nil {
		out.maxSet = true
		out.maxTokens = *env.Params.Arguments.MaxTokens
	}
	return out
}

func (s *replay) match(method, path string, call parsedCall) (*recording, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rec := range s.recs {
		if rec.used || rec.method != method || rec.path != path || rec.hasAuth != true {
			continue
		}
		if rec.rpc != call.rpc || rec.tool != call.tool {
			continue
		}
		if rec.tool == "lambo_recall" && rec.query != call.query {
			continue
		}
		if rec.tool == "lambo_derive" && !sameConcepts(rec.concepts, call.concepts) {
			continue
		}
		if rec.tool == "lambo_stats" && rec.receipt != call.receipt {
			continue
		}
		if rec.confirm != "" && rec.confirm != call.confirm {
			continue
		}
		rec.used = true
		return rec, true
	}
	return nil, false
}

func (s *replay) note(r *http.Request, body []byte, rec *recording) {
	call := replayCall{
		Path:        r.URL.Path,
		Accept:      r.Header.Get("Accept"),
		ContentType: r.Header.Get("Content-Type"),
		RequestMCP:  r.Header.Get("Mcp-Session-Id"),
		Body:        append([]byte(nil), body...),
	}
	parsed := parseCall(body)
	call.RPC = parsed.rpc
	call.Tool = parsed.tool
	call.WaitMS = parsed.waitMS
	call.WaitSet = parsed.waitSet
	call.TopK = parsed.topK
	call.TopSet = parsed.topSet
	call.MaxTokens = parsed.maxTokens
	call.MaxSet = parsed.maxSet
	if rec != nil {
		call.Exchange = rec.exchange
		call.ResponseMCP = rec.mcp
		if call.Tool == "" {
			call.Tool = rec.tool
		}
	}
	s.mu.Lock()
	s.log = append(s.log, call)
	s.mu.Unlock()
}

func writeRecording(w http.ResponseWriter, rec *recording, reqID *int) {
	if rec == nil {
		http.Error(w, "missing recording", http.StatusInternalServerError)
		return
	}
	if rec.ctype != "" {
		w.Header().Set("Content-Type", rec.ctype)
	}
	if rec.wwwAuth != "" {
		w.Header().Set("Www-Authenticate", rec.wwwAuth)
	}
	if rec.mcp != "" {
		w.Header().Set("Mcp-Session-Id", rec.mcp)
	}
	w.WriteHeader(rec.status)
	if rec.bodyText != "" {
		_, _ = io.WriteString(w, rec.bodyText)
		return
	}
	if len(rec.sse) > 0 {
		for _, ev := range rec.sse {
			if ev.ID != nil {
				fmt.Fprintf(w, "id: %s\n", *ev.ID)
			}
			if ev.Retry != nil {
				fmt.Fprintf(w, "retry: %d\n", *ev.Retry)
			}
			line := oneLine(ev.Data, reqID)
			if line == nil {
				fmt.Fprintf(w, "data:\n\n")
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", line)
		}
		return
	}
	if line := oneLine(rec.body, reqID); line != nil {
		_, _ = w.Write(line)
	}
}

func oneLine(raw json.RawMessage, id *int) json.RawMessage {
	compacted := compactRaw(raw)
	if compacted == nil || id == nil {
		return compacted
	}
	var msg map[string]json.RawMessage
	if json.Unmarshal(compacted, &msg) != nil {
		return compacted
	}
	if _, ok := msg["id"]; !ok {
		return compacted
	}
	encoded, err := json.Marshal(*id)
	if err != nil {
		return compacted
	}
	msg["id"] = encoded
	out, err := json.Marshal(msg)
	if err != nil {
		return compacted
	}
	return out
}

func compactRaw(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, trimmed); err != nil {
		return append([]byte(nil), trimmed...)
	}
	return buf.Bytes()
}

func loadRecordings(t *testing.T, dir string) []*recording {
	t.Helper()
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
	sort.Strings(names)
	if len(names) != 21 {
		t.Fatalf("%d recordings", len(names))
	}
	out := make([]*recording, 0, len(names))
	for _, name := range names {
		out = append(out, loadRecording(t, dir, name))
	}
	return out
}

func loadRecording(t *testing.T, dir, name string) *recording {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Exchange string `json:"exchange"`
		Request  struct {
			Method  string            `json:"method"`
			Path    string            `json:"path"`
			Headers map[string]string `json:"headers"`
			Body    json.RawMessage   `json:"body"`
		} `json:"request"`
		Response struct {
			Status   int               `json:"status"`
			Headers  map[string]string `json:"headers"`
			SSE      []sseEvent        `json:"sse"`
			Body     json.RawMessage   `json:"body"`
			BodyText string            `json:"body_text"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	if env.Exchange != strings.TrimSuffix(name, ".json") {
		t.Fatalf("%s exchange %q", name, env.Exchange)
	}
	parsed := parseCall(env.Request.Body)
	var concepts []Concept
	if parsed.tool == "lambo_derive" {
		concepts = parsed.concepts
	}
	return &recording{
		exchange: env.Exchange,
		method:   env.Request.Method,
		path:     env.Request.Path,
		hasAuth:  env.Request.Headers["authorization"] != "",
		rpc:      parsed.rpc,
		tool:     parsed.tool,
		query:    parsed.query,
		receipt:  parsed.receipt,
		confirm:  parsed.confirm,
		concepts: concepts,
		status:   env.Response.Status,
		ctype:    env.Response.Headers["content-type"],
		wwwAuth:  env.Response.Headers["www-authenticate"],
		mcp:      env.Response.Headers["mcp-session-id"],
		sse:      env.Response.SSE,
		body:     env.Response.Body,
		bodyText: env.Response.BodyText,
	}
}
