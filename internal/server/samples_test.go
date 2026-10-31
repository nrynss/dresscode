package server

// The samples replay the recorded stories a judge opens for free. Each
// open derives the sample's cached state under the judge's own user id -
// the selfie registered, the analysis rows its fix measures pre-existing,
// the render the story showed already a blob - and starts a playback of
// the recorded stage state, so every route answers with zero model asks
// and zero YouCam calls. The tests name where a replay cannot carry the
// recorded example whole:
//
//   - The ids the fixtures name: the sample open mints its own, and the
//     look id and the job id are one id, so both fixture ids rename to
//     it. The sample POST fixtures (sample-look.json and its four
//     mirrors) share the pair 5a391e...; job-done.json holds the job id.
//   - The recorded story's pick frame carries the recall sentence,
//     because that recording ran with history on. A judge is a fresh
//     guest with no memory, and the contract omits recall when history
//     is off, so the playback's pick frame carries none. The frames are
//     compared with that one member dropped, named here.
//   - The recorded shop frame carries the media join, because a stream
//     frame is rewritten on the way out (the same join the shop path
//     serves). The playback publishes the shop stage's own payload, so
//     the frame is compared with media_id and url dropped from the
//     products; the joined form is pinned on the shop route itself.
//   - The look in words. The fixture composes "gold jhumkas"; the check
//     joins the vocabulary's own order, "jhumkas, gold" - T4.4's named
//     phrase-table tension, a data decision.
//   - The full stream's frames are pinned on the playback itself. A
//     playback finishes in milliseconds, so a subscriber that arrives
//     after the terminal is answered with the retained terminal alone;
//     the sample stream test pins that late-subscriber answer, and the
//     landed look tests pin the broker's frame shape.
//   - docs/api.md says the sample's job "is already done". The open starts
//     the playback on the landed runner and answers; the runner's Result is
//     a read, not a wait, and there is no wait to take without coupling a
//     free route to a full paid queue. A client that reads the job in the
//     instant after the 202 can still see running. The stream and the job
//     read are the contract's shapes; the doc's word for the instant is
//     the orchestrator's to settle.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nrynss/dresscode/internal/config"
	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/pipeline"
	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/job"
)

// samplesDir is the samples data directory: the people and the cached
// stories beside them.
func samplesDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "testdata", "samples")
}

// mountSamples attaches the sample routes to an assembled look server,
// and refuses a second mount.
func mountSamples(t *testing.T, env *lookEnv) {
	t.Helper()
	if err := env.srv.MountSamples(t.Context(), SamplesConfig{SamplesDir: samplesDir(t)}); err != nil {
		t.Fatalf("mount samples: %v", err)
	}
	if err := env.srv.MountSamples(t.Context(), SamplesConfig{SamplesDir: samplesDir(t)}); err == nil {
		t.Fatal("a second mount succeeded")
	}
}

// sampleRenames maps the id pair the sample POST fixtures name onto the
// one id the server mints for the look and its job.
func sampleRenames(lookID string) map[string]string {
	return map[string]string{
		"5a391e00000000000000000000000001": lookID,
		"5a391e00000000000000000000000002": lookID,
	}
}

// startSample opens one sample, pins the POST against its recorded
// example, and decodes the two ids, which are one.
func startSample(t *testing.T, env *lookEnv, cookie *http.Cookie, sid, fixtureName string) (string, string) {
	t.Helper()
	rec := do(t, env.handler, http.MethodPost, "/api/samples/"+sid+"/looks", cookie, nil, headerJSON)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("sample %s start status %d body %s", sid, rec.Code, rec.Body.String())
	}
	var started struct {
		LookID string `json:"look_id"`
		JobID  string `json:"job_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
		t.Fatalf("sample %s start body %s: %v", sid, rec.Body.String(), err)
	}
	if !id.Valid(started.LookID) || started.LookID != started.JobID {
		t.Fatalf("sample %s ids %q %q", sid, started.LookID, started.JobID)
	}
	if fixtureName != "" {
		compare(t, fixtureName, rec, sampleRenames(started.LookID))
	}
	return started.LookID, started.JobID
}

// productPictures seeds the catalog pictures the shop join reads, the
// same seam the production wiring passes. The bytes are any recorded
// picture: the join names ids, and the media route serves them.
func (e *lookEnv) productPictures(t *testing.T, ids ...string) {
	t.Helper()
	for _, pid := range ids {
		e.catalog[pid] = e.seed(t, "catalog-holder", "product", recordedImage(t, "earring-upload-ref"))
	}
}

// TestOpeningEverySampleSpendsNothing is the Done-when: all five samples
// open, every route answers its recorded story, and the meter, the call
// counters and the memory show nothing was spent anywhere. The family
// sample's reads are compared against their recorded examples member for
// member; the other four are pinned on their own recorded values.
func TestOpeningEverySampleSpendsNothing(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	mountSamples(t, env)
	_, cookie := bootUser(t, env.handler)
	env.productPictures(t, "gold-jhumkas", "lip-nude", "gold-chain", "shirt-dark-plain")

	type samplePin struct {
		id       string
		fixture  string
		occasion string
		flags    []string
		fix      any // string, or nil for the null the contract answers
		runnerUp string
		shownFix string // empty: suggestion-only
		words    string
		products []string
	}
	pins := []samplePin{
		{id: "sample-date", fixture: "sample-date-look", occasion: "date",
			flags: []string{"date-hair-clear", "date-lip-clear"},
			fix:   "date-nude-lip", runnerUp: "date-tie-hair",
			products: []string{"lip-nude"}},
		{id: "sample-family", fixture: "sample-look", occasion: "family-function",
			flags: []string{"family-earring-festive", "family-lip-colour"},
			fix:   "family-gold-jhumkas", runnerUp: "family-festive-top",
			shownFix: "family-gold-jhumkas", words: storyWords,
			products: []string{"gold-jhumkas"}},
		{id: "sample-interview", fixture: "sample-interview-look", occasion: "interview",
			flags: []string{}, fix: nil, runnerUp: "",
			products: []string{}},
		{id: "sample-festival", fixture: "sample-festival-look", occasion: "festival",
			flags: []string{"festival-garment-festive", "festival-earring", "festival-necklace",
				"festival-lip", "festival-hair", "festival-expression", "festival-light"},
			fix: "festival-tie-hair", runnerUp: "festival-gold-chain",
			shownFix: "festival-gold-chain", words: "chain, gold",
			products: []string{"gold-chain"}},
		{id: "sample-funeral", fixture: "sample-funeral-look", occasion: "funeral",
			flags: []string{"funeral-garment-dark", "funeral-collar-plain", "funeral-beard-close",
				"funeral-expression-still", "funeral-light-plain"},
			fix: "funeral-dark-shirt", runnerUp: "funeral-short-beard",
			products: []string{"shirt-dark-plain"}},
	}

	// The list, first: the recorded example, whole.
	compare(t, "samples", env.get(t, cookie, "/api/samples"), nil)

	for _, pin := range pins {
		lookID, jobID := startSample(t, env, cookie, pin.id, pin.fixture)
		env.waitDone(t, cookie, jobID)

		lookRec := env.get(t, cookie, "/api/looks/"+lookID)
		body := bodyOf(t, lookRec)
		if got := body["occasion"]; got != pin.occasion {
			t.Fatalf("%s: occasion %v, want %s", pin.id, got, pin.occasion)
		}
		selfieID, _ := body["selfie_id"].(string)
		if !id.Valid(selfieID) {
			t.Fatalf("%s: selfie id %q", pin.id, selfieID)
		}
		media := do(t, env.handler, http.MethodGet, "/media/"+selfieID, cookie, nil, nil)
		if media.Code != http.StatusOK || !strings.HasPrefix(media.Header().Get("Content-Type"), "image/jpeg") {
			t.Fatalf("%s: selfie media %d %s", pin.id, media.Code, media.Header().Get("Content-Type"))
		}
		if ready, _ := body["ready"].(bool); !ready {
			t.Fatalf("%s: the look never became ready: %v", pin.id, body)
		}
		if got := strings.Join(flagIDsOf(t, body), ","); got != strings.Join(pin.flags, ",") {
			t.Fatalf("%s: flags %v, want %v", pin.id, flagIDsOf(t, body), pin.flags)
		}
		switch want := pin.fix.(type) {
		case nil:
			if _, ok := body["fix"].(map[string]any); ok {
				t.Fatalf("%s: fix %v, want null", pin.id, body["fix"])
			}
		case string:
			fix, _ := body["fix"].(map[string]any)
			if fix == nil || fix["id"] != want {
				t.Fatalf("%s: fix %v, want %s", pin.id, body["fix"], want)
			}
		}
		if got, _ := body["runner_up"].(string); got != pin.runnerUp {
			t.Fatalf("%s: runner-up %q, want %q", pin.id, got, pin.runnerUp)
		}
		shown, _ := body["shown_render"].(map[string]any)
		if pin.shownFix == "" {
			if shown != nil {
				t.Fatalf("%s: shown render %v, want null", pin.id, shown)
			}
			if only, _ := body["suggestion_only"].(bool); !only {
				t.Fatalf("%s: suggestion_only false", pin.id)
			}
			if got, _ := body["words"].(string); got != "" {
				t.Fatalf("%s: words %q, want none", pin.id, got)
			}
		} else {
			if shown == nil {
				t.Fatalf("%s: no shown render", pin.id)
			}
			if shown["fix_id"] != pin.shownFix {
				t.Fatalf("%s: shown fix %v, want %s", pin.id, shown["fix_id"], pin.shownFix)
			}
			if got, _ := body["words"].(string); got != pin.words {
				t.Fatalf("%s: words %q, want %q", pin.id, got, pin.words)
			}
			if only, _ := body["suggestion_only"].(bool); only {
				t.Fatalf("%s: suggestion_only true", pin.id)
			}
			renderID, _ := shown["media_id"].(string)
			render := do(t, env.handler, http.MethodGet, "/media/"+renderID, cookie, nil, nil)
			if render.Code != http.StatusOK || !strings.HasPrefix(render.Header().Get("Content-Type"), "image/jpeg") {
				t.Fatalf("%s: render media %d %s", pin.id, render.Code, render.Header().Get("Content-Type"))
			}
		}
		if got := strings.Join(productIDsOf(t, body["products"]), ","); got != strings.Join(pin.products, ",") {
			t.Fatalf("%s: products %v, want %v", pin.id, productIDsOf(t, body["products"]), pin.products)
		}

		// The sample's own job read: done, and the shop stage last, the
		// order the contract freezes.
		_, jobBody := env.jobState(t, cookie, jobID)
		if jobBody["status"] != "done" || jobBody["stage"] != "shop" {
			t.Fatalf("%s: job %v", pin.id, jobBody)
		}

		if pin.id == "sample-family" {
			// The recorded story's routes, whole: the verdict read, the
			// job state, the list, the renders and the shop.
			renderID := shownRenderID(t, body)
			r := env.renames(lookID, selfieID, renderID)
			compare(t, "look", lookRec, r)
			compare(t, "job-done", env.get(t, cookie, "/api/jobs/"+jobID), r)
			compare(t, "looks", env.get(t, cookie, "/api/looks?occasion=family-function"), r)
			compare(t, "renders", env.get(t, cookie, "/api/looks/"+lookID+"/renders"), r)
			compare(t, "shop", env.get(t, cookie, "/api/looks/"+lookID+"/shop"), r)
		}
	}

	// The zero-spend proof, instrument by instrument. No YouCam task
	// was created, uploaded, polled or fetched; no model ask of any
	// kind; no unit quoted, reserved or settled; no memory read or
	// written; and no judge-lease seat taken - the samples took none,
	// so the whole lease is still waiting for a fresh selfie.
	creates, uploads, polls, fetches := env.cam.count()
	if creates != 0 || uploads != 0 || polls != 0 || fetches != 0 {
		t.Fatalf("youcam creates %d uploads %d polls %d fetches %d, want all zero", creates, uploads, polls, fetches)
	}
	for _, kind := range []string{"judge", "pick", "check"} {
		if n := env.clef.count(kind); n != 0 {
			t.Fatalf("%s asked %d times, want zero", kind, n)
		}
	}
	if spent := env.budget.Spent(); spent != 0 {
		t.Fatalf("the meter settled %d units, want zero", spent)
	}
	if reserved := env.budget.Reserved(); reserved != 0 {
		t.Fatalf("the meter held %d units reserved, want zero", reserved)
	}
	if recalls, writes := env.mem.count(); recalls != 0 || writes != 0 {
		t.Fatalf("memory recalls %d writes %d, want zero", recalls, writes)
	}
	if seats := judgeSessionRows(t, env); seats != 0 {
		t.Fatalf("%d judge-lease rows, want none: the samples took no seat", seats)
	}
}

// TestAJudgeStillHasTheWholeLeaseAfterTheSamples is the fresh-selfie
// pin: after every sample is open, the judge's own selfie still runs the
// landed start on the landed routes - the gate, the lease, the budget
// and the paid pipeline exactly as they were - and the meter shows the
// samples added nothing to what that one look spent.
func TestAJudgeStillHasTheWholeLeaseAfterTheSamples(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, smileAnswers(), 8, nil)
	mountSamples(t, env)
	user, cookie := bootUser(t, env.handler)
	env.productPictures(t, "gold-jhumkas", "lip-nude", "gold-chain", "shirt-dark-plain")

	for _, sid := range sampleIDs {
		_, jobID := startSample(t, env, cookie, sid, "")
		env.waitDone(t, cookie, jobID)
		if got := env.budget.Spent(); got != 0 {
			t.Fatalf("%s spent %d units, want zero", sid, got)
		}
	}

	// No sample open took a seat: the lease table holds no row.
	if n := judgeSessionRows(t, env); n != 0 {
		t.Fatalf("%d judge-lease rows, want none before a fresh look", n)
	}

	// The judge's own selfie, the normal start, the landed path.
	selfieID := env.smileSeeds(t, user)
	env.optIn(t, user, "family-function", "family-closed-smile")
	lookID, jobID, rec := env.start(t, cookie, selfieID)
	compare(t, "look-start", rec, env.renames(lookID, selfieID, ""))
	env.waitDone(t, cookie, jobID)

	// The lease took exactly one fresh selfie and the render pair for
	// the fresh look, and nothing for the samples.
	fresh, renders := judgeSessionSeats(t, env)
	if fresh != 1 || renders != 2 {
		t.Fatalf("judge seats fresh %d renders %d, want 1 and 2", fresh, renders)
	}

	// The meter: the one unit the smile's render costs, no more - the
	// five samples added nothing.
	if spent := env.budget.Spent(); spent != 1 {
		t.Fatalf("the meter settled %d units, want the smile's one", spent)
	}
	creates, _, _, _ := env.cam.count()
	if creates != 1 {
		t.Fatalf("youcam created %d tasks, want the smile's one", creates)
	}
	for _, kind := range []string{"judge", "pick", "check"} {
		if n := env.clef.count(kind); n != 1 {
			t.Fatalf("%s asked %d times, want the fresh look's one", kind, n)
		}
	}

	// The sample look and the fresh look share the list, the sample
	// still answering after a paid look ran beside it.
	body := bodyOf(t, env.get(t, cookie, "/api/looks?occasion=family-function"))
	looks, _ := body["looks"].([]any)
	if len(looks) != 2 {
		t.Fatalf("%d looks listed, want the family sample and the fresh one", len(looks))
	}
	shop := bodyOf(t, env.get(t, cookie, "/api/looks/"+lookID+"/shop"))
	products, _ := shop["products"].([]any)
	if len(products) != 1 {
		t.Fatalf("the fresh look shops %v", products)
	}
	entry, _ := products[0].(map[string]any)
	if entry["id"] != "family-closed-smile" || entry["fidelity"] != "visual simulation" {
		t.Fatalf("the fresh look's shop entry %v", entry)
	}
}

// TestTheSampleStreamAnswersLikeTheRecordedJob pins the frames a sample
// publishes - the playback of the family story against its recorded
// stream, the others' stage orders against the contract's frozen
// sequence - and the late subscriber's answer: the retained terminal
// alone.
func TestTheSampleStreamAnswersLikeTheRecordedJob(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	mountSamples(t, env)

	// The playback of the family story, whole: the recorded frames in
	// order, minus the two members a stream gains on the way out.
	sm := env.srv.samplesOf()
	st := sm.byID["sample-family"]
	var frames []job.Progress
	data, err := sm.playback(func(p job.Progress) { frames = append(frames, p) }, st, []string{"render-id"})
	if err != nil {
		t.Fatal(err)
	}
	pinFrames(t, "job-events", frames, env.renames("look-id", "selfie-id", "render-id"))
	var look map[string]any
	if err := json.Unmarshal(data, &look); err != nil {
		t.Fatalf("playback data %s: %v", data, err)
	}
	if look["fix_id"] != "family-gold-jhumkas" || look["runner_up_id"] != "family-festive-top" {
		t.Fatalf("playback pick %v %v", look["fix_id"], look["runner_up_id"])
	}
	if look["suggestion_only"] != false || look["words"] != storyWords {
		t.Fatalf("playback look words %v suggestion-only %v", look["words"], look["suggestion_only"])
	}
	shown, _ := look["shown"].(map[string]any)
	if shown == nil || shown["media_id"] != "render-id" || shown["fix_id"] != "family-gold-jhumkas" {
		t.Fatalf("playback shown %v", look["shown"])
	}

	// The other four: the stage order the contract freezes, each on its
	// own story's answer.
	for _, want := range []struct {
		id     string
		stages []string
	}{
		{"sample-date", []string{"judge", "pick", "measure", "render", "check", "render", "check", "shop"}},
		{"sample-interview", []string{"judge", "pick", "shop"}},
		{"sample-festival", []string{"judge", "pick", "measure", "render", "check", "render", "check", "shop"}},
		{"sample-funeral", []string{"judge", "pick", "measure", "render", "check", "render", "check", "shop"}},
	} {
		st := sm.byID[want.id]
		var frames []job.Progress
		if _, err := sm.playback(func(p job.Progress) { frames = append(frames, p) }, st, make([]string, len(st.Attempts))); err != nil {
			t.Fatal(err)
		}
		got := make([]string, 0, len(frames))
		for _, f := range frames {
			got = append(got, f.Stage)
		}
		if strings.Join(got, ",") != strings.Join(want.stages, ",") {
			t.Fatalf("%s: stages %v, want %v", want.id, got, want.stages)
		}
	}

	// A subscriber that arrives after the open reads the retained
	// terminal, the broker's answer to a cursorless subscriber, and the
	// job state reads done beside it.
	_, cookie := bootUser(t, env.handler)
	_, jobID := startSample(t, env, cookie, "sample-family", "")
	env.waitDone(t, cookie, jobID)
	streamFrames, _, _ := env.readFrames(t, cookie, jobID, nil)
	if len(streamFrames) != 1 || streamFrames[0].Name != "done" {
		t.Fatalf("a late subscriber read %v, want the terminal alone", streamFrames)
	}
}

// TestSamplesAreNotSpendingRoutes pins the route's own contract: the
// sample start accepts no spend handler, an unknown sample is a miss,
// and samples_only - the judging switch - keeps the samples open while
// it refuses the fresh look beside them.
func TestSamplesAreNotSpendingRoutes(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	if err := env.srv.SetSpendHandler("POST /api/samples/sample-family/looks", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})); err == nil {
		t.Fatal("the sample start accepted a spend handler")
	}
	mountSamples(t, env)
	user, cookie := bootUser(t, env.handler)
	env.productPictures(t, "gold-jhumkas", "lip-nude", "gold-chain", "shirt-dark-plain")

	rec := do(t, env.handler, http.MethodPost, "/api/samples/sample-nowhere/looks", cookie, nil, headerJSON)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown sample status %d body %s", rec.Code, rec.Body.String())
	}

	// samples_only: the judging mode. The fresh look refuses; the
	// samples stay open.
	if err := env.srv.setIntakeFlag(t.Context(), samplesOnlyFlag, true); err != nil {
		t.Fatalf("flag: %v", err)
	}
	selfieID := env.storySeeds(t, user)
	env.optIn(t, user, "family-function", "family-gold-jhumkas", "family-festive-top")
	refused := do(t, env.handler, http.MethodPost, "/api/looks", cookie,
		strings.NewReader(`{"occasion":"family-function","selfie_id":"`+selfieID+`"}`), headerJSON)
	assertErr(t, refused, http.StatusForbidden, "samples_only", "Only samples are open.")

	lookID, jobID := startSample(t, env, cookie, "sample-family", "")
	env.waitDone(t, cookie, jobID)
	body := bodyOf(t, env.get(t, cookie, "/api/looks/"+lookID))
	if ready, _ := body["ready"].(bool); !ready {
		t.Fatalf("the sample did not finish under samples_only: %v", body)
	}
	if env.budget.Spent() != 0 {
		t.Fatalf("the meter settled %d units under samples_only, want zero", env.budget.Spent())
	}
}

// TestSamplesNeedTheLookRoutes pins the mount's one requirement: without
// the look routes there is no runner to play back on and no routes to
// answer through, so the mount refuses.
func TestSamplesNeedTheLookRoutes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	app := filepath.Join(dir, "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "index.html"), []byte(appIndex), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := Open(t.Context(), Config{
		DBPath:     filepath.Join(dir, "dresscode.db"),
		AppDir:     app,
		SigningKey: []byte("test-session-signing-key-32b!!!!"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Close() }()
	if err := srv.MountSamples(t.Context(), SamplesConfig{SamplesDir: samplesDir(t)}); err == nil {
		t.Fatal("samples mounted without the look routes")
	}
}

// TestEveryCachedStoryIsWhatTheRecordingsSay is the honesty proof. Each
// cached file is read again on its own, decoded into the type the mount
// decodes it into, and every value it carries is checked against the
// recording or the dataset it names: the miss list against the evaluation
// ground truth AND against what the shipped judge lines read out of the
// recorded Clef answer, the biggest issue against that same recording, the
// shown render against the YouCam flow that made it (including the photo
// it uploaded), the look answers against the fix's own declared
// vocabulary, the analysis row against the recorded face-attr read and the
// picture that read was of, and the suggestion-only stories against the
// absence of any recorded render of their variant. A value no recording
// supports fails here by name.
func TestEveryCachedStoryIsWhatTheRecordingsSay(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	mountSamples(t, env)
	sm := env.srv.samplesOf()
	if len(sm.list) != len(sampleIDs) {
		t.Fatalf("%d stories loaded, want %d", len(sm.list), len(sampleIDs))
	}
	root := repoRoot(t)
	lines := testSettings().Judge
	misses := groundTruthMisses(t, root)
	youcamResults := youcamResultFiles(t, root)

	for _, sid := range sampleIDs {
		// (a) The file decodes into the type the mount decodes it into,
		// read on its own and not through the mount.
		raw, err := os.ReadFile(filepath.Join(root, "testdata", "samples", "cached", sid+".json"))
		if err != nil {
			t.Fatalf("%s: %v", sid, err)
		}
		var dec sampleState
		if err := json.Unmarshal(raw, &dec); err != nil {
			t.Fatalf("%s: decode: %v", sid, err)
		}
		if dec.Kind != sampleCacheKind || dec.ID != sid {
			t.Fatalf("%s: kind %q id %q", sid, dec.Kind, dec.ID)
		}
		if strings.TrimSpace(dec.Source) == "" {
			t.Fatalf("%s: no provenance in the file itself", sid)
		}
		st := sm.byID[sid]
		if st.Name != dec.Name || st.Occasion != dec.Occasion || st.Variant != dec.Variant {
			t.Fatalf("%s: the mount and the plain decode disagree", sid)
		}
		occ := st.occ

		// (b) The flags are the variant's own miss list, item for item in
		// the ground truth's order, which is the occasion's order.
		wantMiss := misses[dec.Variant]
		if got := flagIDsOfCache(dec); strings.Join(got, ",") != strings.Join(wantMiss, ",") {
			t.Fatalf("%s: %s flags %v, ground truth says %v", sid, dec.Variant, got, wantMiss)
		}

		// (c) The same flags are what the shipped judge lines read out of
		// the recorded Clef answer for this variant and occasion, so the
		// story is the one the model's own numbers tell and not a list
		// picked to look good.
		answers := recordedJudgeAnswers(t, root, dec.Variant, dec.Occasion)
		read := make([]string, 0, len(occ.Criteria))
		for _, c := range occ.Criteria {
			noul, ok := answers[c.ID]
			if !ok {
				t.Fatalf("%s: the judge recording answers no %s", sid, c.ID)
			}
			if noul < lineFor(lines, c.ID) {
				read = append(read, c.ID)
			}
		}
		if strings.Join(read, ",") != strings.Join(wantMiss, ",") {
			t.Fatalf("%s: the shipped lines read %v from the recording, the cache says %v",
				sid, read, wantMiss)
		}

		// (d) The biggest issue is that recording's own choice.
		if got := recordedIssue(t, root, dec.Variant, dec.Occasion); got != dec.Judge.Issue {
			t.Fatalf("%s: issue %q, the recording chose %q", sid, dec.Judge.Issue, got)
		}

		// (e) The analysis rows are recorded reads, and each names the
		// picture the provider actually read.
		for _, a := range dec.Analysis {
			uploaded, shape := recordedFaceAttrRead(t, root)
			if a.FaceShape != shape {
				t.Fatalf("%s: %s says %q, the recording answered %q", sid, a.Task, a.FaceShape, shape)
			}
			if a.ReadFrom != uploaded {
				t.Fatalf("%s: %s names %q, the recording read %q", sid, a.Task, a.ReadFrom, uploaded)
			}
			if filepath.Dir(a.ReadFrom) != filepath.Dir(dec.SelfiePath) {
				t.Fatalf("%s: %s read %s, another person's face", sid, a.Task, a.ReadFrom)
			}
		}

		// (f) and (g). A shown attempt is a real recorded render of this
		// sample's own selfie, its look answers are the fix's declared
		// vocabulary, and a discard carries no check at all. A story with
		// no shown attempt has no recorded render of its variant to show.
		var shown int
		for i, at := range dec.Attempts {
			if at.Shown {
				shown++
				fix := fixByID(occ, at.Fix)
				if fix == nil {
					t.Fatalf("%s: no fix %s", sid, at.Fix)
				}
				if !reflect.DeepEqual(at.Check.Attributes, declaredLook(fix)) {
					t.Fatalf("%s: %s answers %v, the fix declares %v",
						sid, at.Fix, at.Check.Attributes, declaredLook(fix))
				}
				src := youcamSourceOf(t, root, at.Image)
				if src != dec.SelfiePath {
					t.Fatalf("%s: the render of %s came from %q, not this sample's selfie %q",
						sid, at.Image, src, dec.SelfiePath)
				}
			} else if at.Check != nil {
				t.Fatalf("%s: attempt %d: a discard carries check values no check ever gave", sid, i)
			}
		}
		if shown == 0 {
			for name := range youcamResults {
				if strings.Contains(name, dec.Variant) {
					t.Fatalf("%s: the story shows no render, but %s records one of %s", sid, name, dec.Variant)
				}
			}
		}
	}
}

// TestCachedRulesRefuseAnInventedStory is the other half of the honesty
// proof: the mount refuses each way a story could drift from what was
// recorded, so the cache cannot quietly grow a number no provider read.
func TestCachedRulesRefuseAnInventedStory(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	mountSamples(t, env)
	rt := env.srv.looksOf()

	for _, want := range []struct {
		name string
		body string
		msg  string
	}{
		{"a file of another kind",
			`{"kind":"something-else","id":"sample-date","source":"s","occasion":"date","variant":"V03","selfie":"woman/V03-date.jpg"}`,
			`kind "something-else"`},
		{"no provenance",
			`{"kind":"sample-cache","id":"sample-date","source":"  ","occasion":"date","variant":"V03","selfie":"woman/V03-date.jpg"}`,
			`no source`},
		{"a selfie that is not the variant's own picture",
			`{"kind":"sample-cache","id":"sample-date","source":"s","occasion":"date","name":"Date","variant":"V04","selfie":"woman/V03-date.jpg"}`,
			`want V04-date.jpg`},
		{"a face read of someone else",
			`{"kind":"sample-cache","id":"sample-date","source":"s","occasion":"date","name":"Date","variant":"V03","selfie":"woman/V03-date.jpg",
			  "judge":{"flags":["date-hair-clear","date-lip-clear"],"issue":"date-lip-clear"},
			  "pick":{"fix":"date-tie-hair","runner_up":""},
			  "analysis":[{"task":"face-attr-analysis","face_shape":"Round","read_from":"man/V09-interview.jpg"}]}`,
			`another person's face`},
		{"a discard that carries a check",
			`{"kind":"sample-cache","id":"sample-date","source":"s","occasion":"date","name":"Date","variant":"V03","selfie":"woman/V03-date.jpg",
			  "judge":{"flags":["date-lip-clear"],"issue":"date-lip-clear"},
			  "pick":{"fix":"date-nude-lip","runner_up":""},
			  "attempts":[{"fix":"date-nude-lip","attempt":"fix","shown":false,
			               "check":{"change_made":true,"same_person":false,"criterion_met":false,"attributes":[]}}]}`,
			`names no check`},
		{"a shown render whose check says it was discarded",
			`{"kind":"sample-cache","id":"sample-family","source":"s","occasion":"family-function","name":"Family function","variant":"V04","selfie":"woman/V04-family-function.jpg",
			  "judge":{"flags":["family-earring-festive"],"issue":"family-earring-festive"},
			  "pick":{"fix":"family-gold-jhumkas","runner_up":""},
			  "attempts":[{"fix":"family-gold-jhumkas","attempt":"fix","shown":true,"image":"woman/V05-family-function.jpg",
			               "check":{"change_made":false,"same_person":true,"criterion_met":true,
			                        "attributes":[{"id":"jewellery","value":"jhumkas"},{"id":"jewellery_colour","value":"gold"}]}}]}`,
			`passed the change and the person`},
		{"a look answer the fix does not change",
			`{"kind":"sample-cache","id":"sample-family","source":"s","occasion":"family-function","name":"Family function","variant":"V04","selfie":"woman/V04-family-function.jpg",
			  "judge":{"flags":["family-earring-festive"],"issue":"family-earring-festive"},
			  "pick":{"fix":"family-gold-jhumkas","runner_up":""},
			  "attempts":[{"fix":"family-gold-jhumkas","attempt":"fix","shown":true,"image":"woman/V05-family-function.jpg",
			               "check":{"change_made":true,"same_person":true,"criterion_met":true,
			                        "attributes":[{"id":"lip_colour","value":"red"},{"id":"jewellery_colour","value":"gold"}]}}]}`,
			`which fix family-gold-jhumkas does not change`},
	} {
		dir := sampleDirWithFile(t, want.body)
		if _, err := loadSample(dir, sampleOf(want.body), rt); err == nil {
			t.Fatalf("%s: the mount accepted it", want.name)
		} else if !strings.Contains(err.Error(), want.msg) {
			t.Fatalf("%s: refused with %q, want it to say %q", want.name, err.Error(), want.msg)
		}
	}
}

// TestASamplePlaybackNeverResumesIntoThePaidPipeline holds the one claim
// that keeps a restart from spending: a playback is a look job with no
// snapshot, and the landed resume hook refuses a look it cannot rebuild,
// so a killed sample is interrupted and never re-run through the paid
// pipeline.
func TestASamplePlaybackNeverResumesIntoThePaidPipeline(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	mountSamples(t, env)
	_, cookie := bootUser(t, env.handler)
	lookID, jobID := startSample(t, env, cookie, "sample-family", "")
	env.waitDone(t, cookie, jobID)

	kind := lookKind(env.srv.looksOf())
	if _, err := kind.Resume(job.Record{ID: jobID, RootID: lookID, Kind: pipeline.KindName}); err == nil {
		t.Fatal("a sample look resumed: the paid pipeline would run for it")
	} else if !strings.Contains(err.Error(), "no snapshot") {
		t.Fatalf("resume refused with %q, want the missing snapshot", err.Error())
	}
	if spent := env.budget.Spent(); spent != 0 {
		t.Fatalf("the meter settled %d units, want zero", spent)
	}
}

// ---------------------------------------------------------------------------
// Small readers

// flagIDsOfCache is one decoded story's flags, in the file's own order.
func flagIDsOfCache(st sampleState) []string {
	return append([]string(nil), st.Judge.Flags...)
}

// declaredLook is the look a fix changes, as the occasion's own data
// declares it: the attribute ids and the values it sets them to.
func declaredLook(fix *domain.Fix) []domain.AnsweredAttribute {
	out := make([]domain.AnsweredAttribute, 0, len(fix.Vocabulary))
	for _, change := range fix.Vocabulary {
		out = append(out, domain.AnsweredAttribute{ID: change.Attribute, Value: change.To})
	}
	return out
}

// lineFor is the shipped met line of one criterion: its own override when
// the settings name one, the default otherwise.
func lineFor(j config.Judge, criterionID string) float64 {
	if line, ok := j.PerCriterion[criterionID]; ok {
		return line
	}
	return j.Met
}

// recording is the shape both the Clef and the YouCam recordings share: a
// request and a response, as recorded.
type recording struct {
	Request struct {
		File string `json:"file"`
	} `json:"request"`
	Response struct {
		File string          `json:"file"`
		Body json.RawMessage `json:"body"`
	} `json:"response"`
}

// answers is one Clef recording's reply table: the question id and its raw
// answer. A Clef reply carries them under response.body.result.answers.
func (r recording) answers(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	var body struct {
		Result struct {
			Answers map[string]json.RawMessage `json:"answers"`
		} `json:"result"`
	}
	if err := json.Unmarshal(r.Response.Body, &body); err != nil {
		t.Fatalf("clef reply: %v", err)
	}
	if len(body.Result.Answers) == 0 {
		t.Fatal("the reply carries no answers")
	}
	return body.Result.Answers
}

func readRecording(t *testing.T, root, name string) recording {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "testdata", name+".json"))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var rec recording
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return rec
}

// groundTruthMisses is the evaluation's ground truth: each variant's miss
// list, in the order the dataset writes it.
func groundTruthMisses(t *testing.T, root string) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "testdata", "eval", "ground-truth.json"))
	if err != nil {
		t.Fatal(err)
	}
	var gt struct {
		Samples []struct {
			ID   string   `json:"id"`
			Miss []string `json:"miss"`
		} `json:"samples"`
	}
	if err := json.Unmarshal(raw, &gt); err != nil {
		t.Fatal(err)
	}
	out := make(map[string][]string, len(gt.Samples))
	for _, s := range gt.Samples {
		out[s.ID] = append([]string(nil), s.Miss...)
	}
	if len(out) == 0 {
		t.Fatal("the ground truth holds no samples")
	}
	return out
}

// recordedJudgeAnswers is one variant's recorded Clef read, as the
// criterion id and its yes-probability.
func recordedJudgeAnswers(t *testing.T, root, variant, occasion string) map[string]float64 {
	t.Helper()
	rec := readRecording(t, root, "clef/judge-"+variant+"-"+occasion)
	answers := rec.answers(t)
	out := make(map[string]float64, len(answers))
	for qid, raw := range answers {
		var one struct {
			Noul *float64 `json:"noul"`
		}
		if err := json.Unmarshal(raw, &one); err != nil {
			t.Fatalf("%s: answer %s: %v", variant, qid, err)
		}
		if one.Noul == nil {
			continue // the biggest-issue question is a choice, not a noul
		}
		out[qid] = *one.Noul
	}
	if len(out) == 0 {
		t.Fatalf("judge-%s-%s carries no reading", variant, occasion)
	}
	return out
}

// recordedIssue is the biggest issue the same recording chose.
func recordedIssue(t *testing.T, root, variant, occasion string) string {
	t.Helper()
	rec := readRecording(t, root, "clef/judge-"+variant+"-"+occasion)
	var one struct {
		Choice string `json:"choice"`
	}
	raw, ok := rec.answers(t)["biggest_issue"]
	if !ok {
		t.Fatalf("judge-%s-%s answers no biggest_issue", variant, occasion)
	}
	if err := json.Unmarshal(raw, &one); err != nil {
		t.Fatal(err)
	}
	return one.Choice
}

// recordedFaceAttrRead is the recorded face-attr flow: the sample picture
// it uploaded and the face shape it answered.
func recordedFaceAttrRead(t *testing.T, root string) (uploaded, shape string) {
	t.Helper()
	up := readRecording(t, root, "youcam/face-attr-upload-src")
	if !strings.HasPrefix(up.Request.File, "../samples/") {
		t.Fatalf("face-attr-upload-src read %q, not a sample picture", up.Request.File)
	}
	poll := readRecording(t, root, "youcam/http-face-attr-poll")
	var body struct {
		Data struct {
			Results struct {
				FaceShape string `json:"faceshape"`
			} `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(poll.Response.Body, &body); err != nil {
		t.Fatal(err)
	}
	return strings.TrimPrefix(up.Request.File, "../samples/"), body.Data.Results.FaceShape
}

// youcamResultFiles is every recorded render, by file name.
func youcamResultFiles(t *testing.T, root string) map[string]bool {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(root, "testdata", "youcam", "results", "*.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[filepath.Base(n)] = true
	}
	if len(out) == 0 {
		t.Fatal("no recorded renders at all")
	}
	return out
}

// youcamSourceOf finds the recording that saved this render and returns
// the sample picture that flow uploaded, so a shown render is proved to
// be a render of this sample's own selfie and not of someone else's.
func youcamSourceOf(t *testing.T, root, image string) string {
	t.Helper()
	want := filepath.Base(image)
	files, err := filepath.Glob(filepath.Join(root, "testdata", "youcam", "*-fetch.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		rec := readRecording(t, root, "youcam/"+strings.TrimSuffix(filepath.Base(f), ".json"))
		if filepath.Base(rec.Response.File) != want {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(f), "-fetch.json")
		up := readRecording(t, root, "youcam/"+name+"-upload-src")
		return strings.TrimPrefix(up.Request.File, "../samples/")
	}
	t.Fatalf("no YouCam recording saved %s", image)
	return ""
}

// sampleOf is the id a crafted cached file claims, so the refusal test can
// hand loadSample the name it must match.
func sampleOf(body string) string {
	for _, sid := range sampleIDs {
		if strings.Contains(body, `"id":"`+sid+`"`) {
			return sid
		}
	}
	return "sample-date"
}

// sampleDirWithFile builds a samples directory holding one crafted story
// and the pictures it names, so the loader reaches the rule under test
// instead of failing on a missing file.
func sampleDirWithFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	cached := filepath.Join(dir, "cached")
	if err := os.MkdirAll(cached, 0o755); err != nil {
		t.Fatal(err)
	}
	name := sampleOf(body)
	if err := os.WriteFile(filepath.Join(cached, name+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(repoRoot(t), "testdata")
	// Every path a crafted story may name: the people, the recorded
	// renders, and the youcam directory the "../youcam" shape needs.
	for _, rel := range []string{"samples/woman", "samples/man", "youcam/results"} {
		src := filepath.Join(root, rel)
		names, err := os.ReadDir(src)
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(dir, strings.TrimPrefix(rel, "samples/"))
		if strings.HasPrefix(rel, "youcam/") {
			dst = filepath.Join(dir, "youcam", filepath.Base(rel))
		}
		if err := os.MkdirAll(dst, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, e := range names {
			if e.IsDir() {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(src, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dst, e.Name()), raw, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

func flagIDsOf(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, _ := body["flags"].([]any)
	out := make([]string, 0, len(raw))
	for _, one := range raw {
		flag, _ := one.(map[string]any)
		flagID, _ := flag["id"].(string)
		out = append(out, flagID)
	}
	return out
}

func productIDsOf(t *testing.T, raw any) []string {
	t.Helper()
	list, _ := raw.([]any)
	out := make([]string, 0, len(list))
	for _, one := range list {
		product, _ := one.(map[string]any)
		productID, _ := product["id"].(string)
		out = append(out, productID)
	}
	return out
}

// judgeSessionRows counts the lease rows on the server's own database.
func judgeSessionRows(t *testing.T, env *lookEnv) int64 {
	t.Helper()
	var got int64
	if err := env.srv.db.Reader().QueryRow("SELECT COUNT(*) FROM judge_sessions").Scan(&got); err != nil {
		t.Fatalf("query: %v", err)
	}
	return got
}

// judgeSessionSeats reads the one session's lease counters.
func judgeSessionSeats(t *testing.T, env *lookEnv) (fresh, renders int) {
	t.Helper()
	var row struct{ fresh, renders int }
	if err := env.srv.db.Reader().QueryRow("SELECT fresh_selfies, renders FROM judge_sessions").
		Scan(&row.fresh, &row.renders); err != nil {
		t.Fatal(err)
	}
	return row.fresh, row.renders
}

// pinFrames compares one playback's progress frames against a recorded
// stream example: the frame names in order, and every payload member for
// member, with the two named deviations dropped from the fixture side -
// the recall member the recording carried under history on, and the
// shop frame's media join, which the stream writer adds on the way out.
// The recorded example's last frame is the terminal, which the broker
// publishes after the job's data lands; the playback publishes the
// stages only, so the terminal is pinned as the fixture's remainder.
func pinFrames(t *testing.T, name string, frames []job.Progress, renames map[string]string) {
	t.Helper()
	f := fixture(t, name)
	var text string
	if err := json.Unmarshal(f.Body, &text); err != nil {
		t.Fatalf("%s: fixture body is not frame text: %v", name, err)
	}
	want := parseFrames(strings.NewReader(text))
	if len(want) == 0 || want[len(want)-1].Name != "done" {
		t.Fatalf("%s: the fixture does not end on done", name)
	}
	want = want[:len(want)-1]
	if len(want) != len(frames) {
		t.Fatalf("%s: %d frames, want %d", name, len(frames), len(want))
	}
	for i := range want {
		if want[i].Name != "progress" {
			t.Fatalf("%s: fixture frame %d is %q, want progress", name, i, want[i].Name)
		}
		var w, g any
		if err := json.Unmarshal(want[i].Data, &w); err != nil {
			t.Fatalf("%s: fixture frame %d: %v", name, i, err)
		}
		if err := json.Unmarshal(frames[i].Detail, &g); err != nil {
			t.Fatalf("%s: frame %d: %v", name, i, err)
		}
		// The fixture frame is Keel's {job_id, stage, detail}; the
		// playback carries the stage and its payload. The stage is
		// pinned by name, the payload member for member.
		envelope, _ := w.(map[string]any)
		if envelope == nil {
			t.Fatalf("%s: fixture frame %d is not an event object", name, i)
		}
		if stage, _ := envelope["stage"].(string); stage != frames[i].Stage {
			t.Fatalf("%s: frame %d stage %q, want %q", name, i, frames[i].Stage, stage)
		}
		w = dropStreamOnly(envelope["detail"])
		w = renameTree(w, renames)
		if !reflect.DeepEqual(w, g) {
			wj, _ := json.Marshal(w)
			gj, _ := json.Marshal(g)
			t.Fatalf("%s: frame %d\n got %s\nwant %s", name, i, gj, wj)
		}
	}
}

// dropStreamOnly removes the members a stream frame gains on the way
// out, which a stage's own payload never carries: the pick frame's
// recall sentence and the shop products' media join. The argument is
// one stage's payload.
func dropStreamOnly(payload any) any {
	m, ok := payload.(map[string]any)
	if !ok {
		return payload
	}
	delete(m, "recall")
	if products, ok := m["products"].([]any); ok {
		for _, p := range products {
			if entry, ok := p.(map[string]any); ok {
				delete(entry, "media_id")
				delete(entry, "url")
			}
		}
	}
	return m
}
