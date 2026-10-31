package server

// The samples replay the recorded stories a judge opens for free. Each
// open derives the sample's cached state under the judge's own user id -
// the selfie registered, the analysis rows its fix measures pre-existing,
// the render the story showed already a blob - and plays back the
// recorded stage state, so every route answers with zero model asks
// and zero YouCam calls. The analysis half of that sentence is an arm of
// the route, not a fact about the five files: every shipped story holds
// `analysis: []`, because a row must name the one recorded face read and
// be of the very picture that read was of, and that picture (V06) is none
// of the five sample people. TestACachedStoryMeasuresFromItsOwnReading
// covers the arm on a mount-only story in a temporary directory, so the
// prose above and the code below agree. A repeat open reuses the pictures
// the judge already holds (TestASampleRepeatOpenKeepsThePicturesItAlreadyHeld)
// rather than writing the same bytes again. The tests name where a replay
// cannot carry the recorded example whole:
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
//   - The job is finished as it is created. The contract says a sample's
//     job "is already done", and the shipped app reads a look exactly once
//     - the render screen POSTs, navigates, asks and never asks again, and
//     only the verdict screen follows a stream - so a sample that answered
//     running would hang that screen. An open therefore writes its job row
//     terminal into the job store itself and publishes the story's frames
//     on the topic, taking no runner lease and entering no pipeline.
//     TestTheSampleJobIsDoneTheMomentItIsCreated reads the look ONCE after
//     the POST, with no polling and no stream, and gets the finished look;
//     TestASampleStoryReachesAStreamSubscriber gets the frames. No sample
//     test polls a job any more, and the late subscriber's answer - the
//     retained terminal alone - stays pinned.
//   - One story's measure frame deviates from the contract's wording, and
//     the file says so: festival's picked fix names an analysis the sample
//     set never read of its own picture, so the story holds no cached row
//     and the stage says skipped. That is what the playback did - it
//     measured nothing - whereas cached would claim a read that never
//     happened and false would promise one that spends.
//   - What a story may show is a rule, not a claim. A render is shown only
//     when a check recording of it passed the change and the person, and
//     the check's three facts are that recording's readings at the shipped
//     check lines. The sample set records four render checks and every one
//     of them is a family-function check of V04, so festival is the story
//     that carries a genuine recorded render of its own selfie and does
//     not show it: its chain names no check, holds no media id and reaches
//     no screen.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/dresscode/internal/config"
	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/pipeline"
	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/job"
	jobsql "github.com/nrynss/keel/job/sqlitestore"
	"github.com/nrynss/keel/wire"
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
		// fix is a string, or nil for the JSON null. The null is pinned
		// deliberately and it is not right: docs/api.md:394 documents the
		// ready body's `fix` as `{id, changes, why}` with no null option,
		// and both shipped screens refuse to read one - web/src/routes/
		// look/render/copy.ts:120-124 and web/src/routes/look/verdict/
		// copy.ts:51-55 answer it with "The render/verdict could not be
		// read". The server emits it because verdictBody sets `fix` only
		// when a fix is found, so any look that flagged nothing has this
		// shape, sample or paid: the sample route inherits it from landed
		// behaviour and cannot fix the contract or the screens from here.
		// A later hand that changes one of the three must change the other
		// two and this pin; nothing here may quietly start answering `{}`.
		fix      any
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
			// The one story that flags nothing, so its ready body answers
			// the null fix samplePin names: the contract does not admit it
			// (docs/api.md:394) and both screens throw on it. Pinned as it
			// is, not as it should be.
			flags: []string{}, fix: nil, runnerUp: "",
			products: []string{}},
		{id: "sample-festival", fixture: "sample-festival-look", occasion: "festival",
			flags: []string{"festival-garment-festive", "festival-earring", "festival-necklace",
				"festival-lip", "festival-hair", "festival-expression", "festival-light"},
			fix: "festival-tie-hair", runnerUp: "festival-gold-chain",
			products: []string{"festival-tie-hair"}},
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

		// One read, no poll and no stream: the app's own path, which asks
		// once and never again. The job was finished as it was written, so
		// this read is the finished look.
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
			// The null is what the server answers today for a look that
			// flagged nothing, paid or sample, and what no recorded ready
			// fixture carries. Asserted so a later hand cannot swap it for
			// an object without saying so here as well.
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
		// No poll and no stream: the sample's job is done the instant the
		// open answers, which is what the contract's "already done" says
		// and what the app's single read depends on.
		if _, body := env.jobState(t, cookie, jobID); body["status"] != "done" {
			t.Fatalf("%s: the job read %v, want done with no wait", sid, body)
		}
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
// plays - the story of the family recording against its recorded stream,
// the others' stage orders against the contract's frozen sequence - and
// the late subscriber's answer: the retained terminal alone.
func TestTheSampleStreamAnswersLikeTheRecordedJob(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	mountSamples(t, env)

	// The story of the family recording, whole: the recorded frames in
	// order, minus the two members a stream gains on the way out.
	sm := env.srv.samplesOf()
	st := sm.byID["sample-family"]
	data, frames, err := sm.story(st, []string{"render-id"})
	if err != nil {
		t.Fatal(err)
	}
	pinFrames(t, "job-events", frames, env.renames("look-id", "selfie-id", "render-id"))
	var look map[string]any
	if err := json.Unmarshal(data, &look); err != nil {
		t.Fatalf("story data %s: %v", data, err)
	}
	if look["fix_id"] != "family-gold-jhumkas" || look["runner_up_id"] != "family-festive-top" {
		t.Fatalf("story pick %v %v", look["fix_id"], look["runner_up_id"])
	}
	if look["suggestion_only"] != false || look["words"] != storyWords {
		t.Fatalf("story look words %v suggestion-only %v", look["words"], look["suggestion_only"])
	}
	shown, _ := look["shown"].(map[string]any)
	if shown == nil || shown["media_id"] != "render-id" || shown["fix_id"] != "family-gold-jhumkas" {
		t.Fatalf("story shown %v", look["shown"])
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
		_, frames, err := sm.story(st, make([]string, len(st.Attempts)))
		if err != nil {
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
	// job state reads done beside it. No wait comes first: the story was
	// played before the 202 was written.
	_, cookie := bootUser(t, env.handler)
	_, jobID := startSample(t, env, cookie, "sample-family", "")
	streamFrames, _, _ := env.readFrames(t, cookie, jobID, nil)
	if len(streamFrames) != 1 || streamFrames[0].Name != "done" {
		t.Fatalf("a late subscriber read %v, want the terminal alone", streamFrames)
	}
	if _, body := env.jobState(t, cookie, jobID); body["status"] != "done" {
		t.Fatalf("the job read %v, want done", body)
	}
}

// TestASampleStoryReachesAStreamSubscriber is the other half of the born-
// terminal mechanism: a subscriber that is listening when the story is
// published receives every stage frame and then the terminal, the way it
// would for a look that ran. It cannot be the route's own stream: the
// open mints its id inside itself, so no client can hold the topic before
// the story is played, which is why the frames are pinned on the publish
// path the route uses, on the route's own broker.
func TestASampleStoryReachesAStreamSubscriber(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	mountSamples(t, env)
	sm := env.srv.samplesOf()
	rt := env.srv.looksOf()

	st := sm.byID["sample-festival"]
	data, frames, err := sm.story(st, make([]string, len(st.Attempts)))
	if err != nil {
		t.Fatal(err)
	}
	jobID := mustID(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sub := rt.broker.Subscribe(ctx, job.Topic(jobID))
	defer sub.Cancel()
	if err := sm.land(ctx, jobID, data, frames); err != nil {
		t.Fatal(err)
	}

	// The stages, in the story's order, each one Keel's progress payload
	// with this job's id and the stage's own detail; then the terminal.
	var stages []string
	for {
		select {
		case event, ok := <-sub.Events:
			if !ok {
				t.Fatalf("the topic closed before the terminal: %v", stages)
			}
			if event.Terminal {
				if event.Name != string(job.StatusDone) {
					t.Fatalf("the terminal is %q, want done", event.Name)
				}
				status, ok := event.Data.(wire.StatusEvent)
				if !ok {
					t.Fatalf("the terminal payload is %T, want wire.StatusEvent", event.Data)
				}
				if status.JobID != jobID || status.Status != string(job.StatusDone) {
					t.Fatalf("the terminal payload %v", status)
				}
				if strings.Join(stages, ",") != "judge,pick,measure,render,check,render,check,shop" {
					t.Fatalf("a listening subscriber read stages %v", stages)
				}
				return
			}
			if event.Name != wire.EventProgress {
				t.Fatalf("frame %q, want progress", event.Name)
			}
			progress, ok := event.Data.(wire.ProgressEvent)
			if !ok {
				t.Fatalf("the progress payload is %T, want wire.ProgressEvent", event.Data)
			}
			if progress.JobID != jobID || progress.Stage == "" || len(progress.Detail) == 0 {
				t.Fatalf("the progress payload %v", progress)
			}
			stages = append(stages, progress.Stage)
		case <-time.After(10 * time.Second):
			t.Fatalf("the story never reached its subscriber: %v", stages)
		}
	}
}

// TestTheSampleJobIsDoneTheMomentItIsCreated is the app's own path, with
// nothing waited for: the POST, then one read of the look and one of the
// job, no polling and no stream. The finished look is there at once, with
// ready true, and the job that carries it is done - which is the only
// answer the render screen can survive, because it asks once.
func TestTheSampleJobIsDoneTheMomentItIsCreated(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	mountSamples(t, env)
	_, cookie := bootUser(t, env.handler)
	env.productPictures(t, "gold-jhumkas")

	lookID, jobID := startSample(t, env, cookie, "sample-family", "")
	body := bodyOf(t, env.get(t, cookie, "/api/looks/"+lookID))
	if ready, _ := body["ready"].(bool); !ready {
		t.Fatalf("the first read of the look was not the finished look: %v", body)
	}
	if shown, _ := body["shown_render"].(map[string]any); shown == nil {
		t.Fatalf("the finished look carries no shown render: %v", body)
	}
	_, jobBody := env.jobState(t, cookie, jobID)
	if jobBody["status"] != "done" || jobBody["stage"] != "shop" {
		t.Fatalf("the first read of the job %v, want done at shop", jobBody)
	}

	// The row was never anything but terminal, so a restart's recovery
	// finds no playback unfinished: the sample opens leave no running and
	// no queued row behind them.
	for _, sid := range sampleIDs {
		if sid == "sample-family" {
			continue
		}
		startSample(t, env, cookie, sid, "")
	}
	var unfinished int
	if err := env.srv.db.Reader().QueryRow(
		`SELECT COUNT(*) FROM jobs WHERE status NOT IN ('done', 'error', 'cancelled', 'interrupted')`).
		Scan(&unfinished); err != nil {
		t.Fatal(err)
	}
	if unfinished != 0 {
		t.Fatalf("%d unfinished job rows, want none: a sample job is born finished", unfinished)
	}
	if spent := env.budget.Spent(); spent != 0 {
		t.Fatalf("the meter settled %d units, want zero", spent)
	}
}

// TestSamplesAreNotSpendingRoutes pins the route's own contract: the
// sample start accepts no spend handler, an unknown sample is a miss,
// and samples_only - the judging switch - keeps the samples open while
// it refuses the fresh look beside them. The refusal is asked of the
// pattern the mount registers, routeSampleStart, and of the answer's own
// words: a spend slot is looked up by pattern, so naming a concrete path
// instead would pass on a typo and would still pass if the sample start
// were ever mounted as a spending route under its real pattern.
func TestSamplesAreNotSpendingRoutes(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	mountSamples(t, env)
	if err := env.srv.SetSpendHandler(routeSampleStart, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})); err == nil {
		t.Fatalf("%s: the sample start accepted a spend handler", routeSampleStart)
	} else if !strings.Contains(err.Error(), routeSampleStart) {
		t.Fatalf("%s: refused with %q, want it to name the route", routeSampleStart, err.Error())
	}
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

	lookID, _ := startSample(t, env, cookie, "sample-family", "")
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
// decodes it into, and every value it carries is traced to the recording
// or the dataset it names, or derived by a rule the file states and this
// test re-executes: the miss list against the evaluation ground truth AND
// against what the shipped judge lines read out of the recorded Clef
// answer, the biggest issue against that same recording, the runner-up
// against the rule the source states, the shown render against the YouCam
// flow that made it (including the photo it uploaded) AND against the
// recorded check that showed it - the occasion it was read for, the
// original it read, the criterion it was asked about, the model it ran on,
// its three gate readings at the shipped check lines and its look answers -
// and a story that shows nothing against the absence of any recorded check
// that could have shown it. The three check booleans are read out of the
// recording here, not trusted from the file: a shown render whose facts do
// not equal its recording's readings at the shipped lines, or whose shown
// flag is not what the discard rule makes of them, fails by name. So does a
// story that claims a render no check of it was ever recorded, and so does
// a reading the analysis cache could not hold. A value no recording
// supports and no stated rule derives fails here.
func TestEveryCachedStoryIsWhatTheRecordingsSay(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	mountSamples(t, env)
	sm := env.srv.samplesOf()
	if len(sm.list) != len(sampleIDs) {
		t.Fatalf("%d stories loaded, want %d", len(sm.list), len(sampleIDs))
	}
	root := repoRoot(t)
	settings := testSettings()
	lines := settings.Judge
	checkLines := settings.Check
	misses := groundTruthMisses(t, root)
	youcamResults := youcamResultFiles(t, root)
	checks := recordedChecks(t, root)

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

		// (e) The runner-up. Three stories state one rule and this test
		// re-executes it against the judge recording's own readings; the
		// fourth names the contract's recorded example instead, and is
		// traced there. A story with no pick names neither.
		flagged := make(map[string]bool, len(dec.Judge.Flags))
		for _, fid := range dec.Judge.Flags {
			flagged[fid] = true
		}
		switch sid {
		case "sample-family":
			fixture := fixture(t, "look")
			var body struct {
				Fix struct {
					ID string `json:"id"`
				} `json:"fix"`
				RunnerUp string `json:"runner_up"`
			}
			if err := json.Unmarshal(fixture.Body, &body); err != nil {
				t.Fatalf("sample-family: api/look.json: %v", err)
			}
			if dec.Pick.Fix != body.Fix.ID || dec.Pick.RunnerUp != body.RunnerUp {
				t.Fatalf("sample-family: the pair is %s/%s, the recorded example says %s/%s",
					dec.Pick.Fix, dec.Pick.RunnerUp, body.Fix.ID, body.RunnerUp)
			}
		default:
			if want := derivedRunnerUp(occ, answers, flagged, dec.Pick.Fix); dec.Pick.RunnerUp != want {
				t.Fatalf("%s: runner-up %q, the rule the file states gives %q", sid, dec.Pick.RunnerUp, want)
			}
		}

		// (f) The analysis rows are recorded reads, each naming the picture
		// the provider actually read - and that picture is the cache key, so
		// it must be this story's own selfie or the row cannot be here.
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
			if a.ReadFrom != dec.SelfiePath {
				t.Fatalf("%s: %s read %s, not this story's own %s, and the cache is keyed by photo hash, so this story holds no reading of itself",
					sid, a.Task, a.ReadFrom, dec.SelfiePath)
			}
		}
		// The measure frame's rule, stated where it deviates: a story says
		// cached when it holds a reading and skipped when it holds none, and
		// it holds a reading only of its own picture. A pick that measures a
		// task no recording read of this selfie therefore names no row, and
		// its file must say the measure stage says skipped.
		if pick := fixByID(occ, dec.Pick.Fix); pick != nil && len(pick.Analysis) > len(dec.Analysis) {
			if !strings.Contains(dec.Source, "skipped") {
				t.Fatalf("%s: fix %s measures %d analyses and the cache holds %d, and the file never says its measure stage says skipped",
					sid, dec.Pick.Fix, len(pick.Analysis), len(dec.Analysis))
			}
		}

		// (g) and (h). Every picture a story names - shown or not - is a
		// recorded render of this sample's own selfie. A shown one is shown
		// because a recorded check of it showed it: the check is the
		// recording the story names, it was read for this occasion, of this
		// selfie, about a criterion this fix serves, on the shipped check
		// model, and the story's three facts and its shown flag are that
		// recording's readings at the shipped lines. A story with no shown
		// attempt has no recorded check that could have shown one.
		var shown int
		for i, at := range dec.Attempts {
			if at.Image == "" {
				if at.Check != nil {
					t.Fatalf("%s: attempt %d names no render and a check all the same", sid, i)
				}
				continue
			}
			if !youcamResults[filepath.Base(at.Image)] {
				t.Fatalf("%s: attempt %d names %s, which no YouCam recording saved", sid, i, at.Image)
			}
			if src := youcamSourceOf(t, root, at.Image); src != dec.SelfiePath {
				t.Fatalf("%s: the render %s came from %q, not this sample's selfie %q",
					sid, at.Image, src, dec.SelfiePath)
			}
			if !at.Shown {
				// Evidence of a pass the check did not keep: no media id,
				// no words, and no answer a check never gave.
				if at.Check != nil {
					t.Fatalf("%s: attempt %d: a discard carries check values no check ever gave", sid, i)
				}
				continue
			}
			shown++
			fix := fixByID(occ, at.Fix)
			if fix == nil {
				t.Fatalf("%s: no fix %s", sid, at.Fix)
			}
			if at.Check == nil {
				t.Fatalf("%s: attempt %d is shown with no check to show it", sid, i)
			}
			if !reflect.DeepEqual(at.Check.Attributes, declaredLook(fix)) {
				t.Fatalf("%s: %s answers %v, the fix declares %v",
					sid, at.Fix, at.Check.Attributes, declaredLook(fix))
			}
			rc, ok := checks[at.Check.Recording]
			if !ok {
				t.Fatalf("%s: attempt %d is shown on %q, which is not a recorded render check",
					sid, i, at.Check.Recording)
			}
			if rc.occasion != dec.Occasion {
				t.Fatalf("%s: attempt %d stands on %s, a check of the %s occasion, not of %s",
					sid, i, rc.name, rc.occasion, dec.Occasion)
			}
			if rc.original != dec.SelfiePath {
				t.Fatalf("%s: attempt %d stands on %s, which read %s as the original, not this sample's own %s",
					sid, i, rc.name, rc.original, dec.SelfiePath)
			}
			if rc.model != checkLines.Model {
				t.Fatalf("%s: attempt %d stands on %s, read by %s, while the shipped check model is %s",
					sid, i, rc.name, rc.model, checkLines.Model)
			}
			if !addresses(occ, at.Fix, map[string]bool{rc.criterion: true}) {
				t.Fatalf("%s: attempt %d stands on %s, a check about %s, which fix %s does not serve",
					sid, i, rc.name, rc.criterion, at.Fix)
			}
			// The three facts, read out of the recording at the shipped
			// lines, and the discard rule applied to them.
			changes := [3]struct {
				id    string
				line  float64
				value bool
			}{
				{"change_made", checkLines.ChangeMade, at.Check.ChangeMade},
				{"same_person", checkLines.SamePerson, at.Check.SamePerson},
				{"criterion_met", checkLines.CriterionMet, at.Check.CriterionMet},
			}
			facts := map[string]bool{}
			for _, one := range changes {
				noul, ok := rc.Gates[one.id]
				if !ok {
					t.Fatalf("%s: attempt %d: %s answers no %s", sid, i, rc.name, one.id)
				}
				met := noul >= one.line
				facts[one.id] = met
				if one.value != met {
					t.Fatalf("%s: attempt %d: %s says %s is %v; %s reads %.4f against the shipped line %.5f, which is %v",
						sid, i, sid, one.id, one.value, rc.name, noul, one.line, met)
				}
			}
			if want := facts["change_made"] && facts["same_person"]; at.Shown != want {
				t.Fatalf("%s: attempt %d: the story shows it %v, the discard rule on %s reads %v",
					sid, i, at.Shown, rc.name, want)
			}
			for _, attr := range at.Check.Attributes {
				choice, ok := rc.Look[attr.ID]
				if !ok {
					t.Fatalf("%s: attempt %d: %s answers no look.%s", sid, i, rc.name, attr.ID)
				}
				if choice != attr.Value {
					t.Fatalf("%s: attempt %d: the story says %s is %q, %s read %q",
						sid, i, attr.ID, attr.Value, rc.name, choice)
				}
			}
			// The pair the recording read and the change it was asked to
			// look for are its own: a check's answers are about the second
			// picture it was sent, and its state carries the one question
			// it was given. Both are read here rather than assumed, so a
			// story cannot stand on a check and say nothing about which
			// picture that check looked at.
			if rc.render == "" || rc.request == "" {
				t.Fatalf("%s: attempt %d: %s names no render picture or no requested change", sid, i, rc.name)
			}
			if _, err := os.Stat(filepath.Join(root, "testdata", "samples", rc.render)); err != nil {
				t.Fatalf("%s: attempt %d: %s read %s as the render, which is not a recorded sample picture: %v",
					sid, i, rc.name, rc.render, err)
			}
			// The pairing this invites to be misread, closed. Where the
			// picture the story shows is not the picture the recording
			// checked - family shows the earring flow's result of V04 while
			// its recording read V05, and the two are different bytes - the
			// story's own source must name the picture that was checked and
			// carry that recording's request word for word. A reader shown
			// "the check this story shows its render on" believes the
			// picture on screen was checked; where it was not, the file says
			// so in its own words. The request is quoted, not paraphrased,
			// because it asks a wider thing than the fix alone ("Adds gold
			// jhumkas and paints the lip red."), and an assertion that the
			// recording's ask equals the shipped fix's change would fail on
			// the honest file: quoting it is what keeps the pairing true.
			if rc.render != at.Image {
				if !strings.Contains(dec.Source, rc.render) {
					t.Fatalf("%s: attempt %d: %s read %s, not the shown %s, and the source never names the picture that was checked",
						sid, i, rc.name, rc.render, at.Image)
				}
				if !strings.Contains(dec.Source, rc.request) {
					t.Fatalf("%s: attempt %d: the source does not carry %s's own request %q, so nothing tells the reader what that check was asked",
						sid, i, rc.name, rc.request)
				}
			}
		}
		if shown == 0 {
			for _, rc := range checks {
				if rc.occasion != dec.Occasion || rc.original != dec.SelfiePath || rc.model != checkLines.Model {
					continue
				}
				if rc.Gates["change_made"] >= checkLines.ChangeMade && rc.Gates["same_person"] >= checkLines.SamePerson {
					t.Fatalf("%s: the story shows no render, but %s reads a render of it as shown", sid, rc.name)
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
		{"a face read of another photo of the same person",
			`{"kind":"sample-cache","id":"sample-date","source":"s","occasion":"date","name":"Date","variant":"V03","selfie":"woman/V03-date.jpg",
			  "judge":{"flags":["date-hair-clear","date-lip-clear"],"issue":"date-lip-clear"},
			  "pick":{"fix":"date-tie-hair","runner_up":""},
			  "analysis":[{"task":"face-attr-analysis","face_shape":"Round","read_from":"woman/V05-family-function.jpg"}]}`,
			`which is not this sample's own picture`},
		{"a render naming a picture outside the two roots",
			`{"kind":"sample-cache","id":"sample-date","source":"s","occasion":"date","name":"Date","variant":"V03","selfie":"woman/V03-date.jpg",
			  "judge":{"flags":["date-hair-clear"],"issue":"date-hair-clear"},
			  "pick":{"fix":"date-tie-hair","runner_up":""},
			  "attempts":[{"fix":"date-tie-hair","attempt":"fix","shown":false,"image":"../outside.jpg"}]}`,
			`is outside the sample roots`},
		{"a discard that carries a check",
			`{"kind":"sample-cache","id":"sample-date","source":"s","occasion":"date","name":"Date","variant":"V03","selfie":"woman/V03-date.jpg",
			  "judge":{"flags":["date-lip-clear"],"issue":"date-lip-clear"},
			  "pick":{"fix":"date-nude-lip","runner_up":""},
			  "attempts":[{"fix":"date-nude-lip","attempt":"fix","shown":false,
			               "check":{"change_made":true,"same_person":false,"criterion_met":false,"attributes":[]}}]}`,
			`names no check`},
		{"a shown render whose check names no recording",
			`{"kind":"sample-cache","id":"sample-family","source":"s","occasion":"family-function","name":"Family function","variant":"V04","selfie":"woman/V04-family-function.jpg",
			  "judge":{"flags":["family-earring-festive"],"issue":"family-earring-festive"},
			  "pick":{"fix":"family-gold-jhumkas","runner_up":""},
			  "attempts":[{"fix":"family-gold-jhumkas","attempt":"fix","shown":true,"image":"woman/V05-family-function.jpg",
			               "check":{"change_made":true,"same_person":true,"criterion_met":true,
			                        "attributes":[{"id":"jewellery","value":"jhumkas"},{"id":"jewellery_colour","value":"gold"}]}}]}`,
			`the check names no recording`},
		{"a shown render whose check says it was discarded",
			`{"kind":"sample-cache","id":"sample-family","source":"s","occasion":"family-function","name":"Family function","variant":"V04","selfie":"woman/V04-family-function.jpg",
			  "judge":{"flags":["family-earring-festive"],"issue":"family-earring-festive"},
			  "pick":{"fix":"family-gold-jhumkas","runner_up":""},
			  "attempts":[{"fix":"family-gold-jhumkas","attempt":"fix","shown":true,"image":"woman/V05-family-function.jpg",
			               "check":{"recording":"check-clef-V04-to-V05","change_made":false,"same_person":true,"criterion_met":true,
			                        "attributes":[{"id":"jewellery","value":"jhumkas"},{"id":"jewellery_colour","value":"gold"}]}}]}`,
			`passed the change and the person`},
		{"a look answer the fix does not change",
			`{"kind":"sample-cache","id":"sample-family","source":"s","occasion":"family-function","name":"Family function","variant":"V04","selfie":"woman/V04-family-function.jpg",
			  "judge":{"flags":["family-earring-festive"],"issue":"family-earring-festive"},
			  "pick":{"fix":"family-gold-jhumkas","runner_up":""},
			  "attempts":[{"fix":"family-gold-jhumkas","attempt":"fix","shown":true,"image":"woman/V05-family-function.jpg",
			               "check":{"recording":"check-clef-V04-to-V05","change_made":true,"same_person":true,"criterion_met":true,
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
// so a sample is interrupted and never re-run through the paid pipeline.
func TestASamplePlaybackNeverResumesIntoThePaidPipeline(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	mountSamples(t, env)
	_, cookie := bootUser(t, env.handler)
	lookID, jobID := startSample(t, env, cookie, "sample-family", "")

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

// TestARestartInterruptsAnUnfinishedPlaybackRow walks the restart arm the
// born-finished job leaves untravelled. A look-kind row that is running
// with no snapshot is the shape an open would have left behind had it
// started a job on the runner instead of landing one; a restart interrupts
// it, the resume hook refuses it, and no replacement attempt is written, so
// the paid pipeline runs for a sample neither now nor after a death.
func TestARestartInterruptsAnUnfinishedPlaybackRow(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	mountSamples(t, env)
	ctx := t.Context()
	rt := env.srv.looksOf()

	jobID := mustID(t)
	if _, err := env.srv.db.Writer().ExecContext(ctx,
		`INSERT INTO jobs (id, kind, status, attempt, parent_id, root_id, progress, updated_at)
		 VALUES (?, ?, 'running', 1, '', ?, '', 1)`,
		jobID, pipeline.KindName, jobID); err != nil {
		t.Fatalf("seed the unfinished playback: %v", err)
	}

	// The restart, the way the process runs one: a fresh store handle and a
	// fresh runner over the same table, the same broker and the landed look
	// kind - the idempotent kind with the resume hook, not a watered-down
	// one.
	store, err := jobsql.Open(ctx, jobsql.Config{DB: env.srv.db})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]job.Kind{pipeline.KindName: lookKind(rt)}
	if _, err := job.Open(ctx, job.Config{
		Broker: rt.broker,
		Store:  kindStore{Store: store, kinds: kinds},
		Kinds:  kinds,
		Log:    slog.New(slog.DiscardHandler),
		Now:    time.Now,
	}); err != nil {
		t.Fatalf("restart: %v", err)
	}

	var status string
	var attempts int
	if err := env.srv.db.Reader().QueryRowContext(ctx,
		`SELECT status FROM jobs WHERE id = ?`, jobID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "interrupted" {
		t.Fatalf("the unfinished playback row is %s after a restart, want interrupted", status)
	}
	if err := env.srv.db.Reader().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs WHERE root_id = ?`, jobID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("%d attempts of the unfinished playback, want one: no replacement ran", attempts)
	}
	if spent := env.budget.Spent(); spent != 0 {
		t.Fatalf("the meter settled %d units, want zero", spent)
	}
	creates, uploads, polls, fetches := env.cam.count()
	if creates != 0 || uploads != 0 || polls != 0 || fetches != 0 {
		t.Fatalf("the restart spent youcam calls %d %d %d %d, want all zero", creates, uploads, polls, fetches)
	}
}

// TestASampleRepeatOpenKeepsThePicturesItAlreadyHeld is the amplification pin.
// A story's pictures are fixed data, so a second open of one sample asks
// for bytes the judge already holds, and the open reuses that media id
// instead of writing a second blob and a second row of the same picture:
// without the reuse every repeat pays the whole of the set again, 1,649,539
// bytes across the five, with no gate token and no budget reservation to
// bound it. The reuse must not hand a look the wrong picture either, so
// what each look reads is compared with the recorded bytes and not only
// with an id. The look row still lands once per open, and the list route
// that answers them has no LIMIT: that growth is the composition hand's,
// named in the task's Notes.
func TestASampleRepeatOpenKeepsThePicturesItAlreadyHeld(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	mountSamples(t, env)
	user, cookie := bootUser(t, env.handler)

	first, _ := startSample(t, env, cookie, "sample-family", "")
	held, bytes := ownedMedia(t, env, user)
	if held != 2 {
		t.Fatalf("%d pictures held after one open, want the selfie and the shown render", held)
	}
	second, _ := startSample(t, env, cookie, "sample-family", "")
	if again, againBytes := ownedMedia(t, env, user); again != held || againBytes != bytes {
		t.Fatalf("the second open grew the judge's pictures to %d rows and %d bytes from %d and %d: a repeat of fixed bytes holds them, it does not rewrite them",
			again, againBytes, held, bytes)
	}

	// Both looks still read their own pictures, and the bytes are the
	// recorded ones: a reuse that returned somebody else's same-sized
	// picture would fail here rather than quietly serve a wrong face.
	root := repoRoot(t)
	selfieRaw := readFile(t, filepath.Join(samplesDir(t), "woman", "V04-family-function.jpg"))
	renderRaw := readFile(t, filepath.Join(root, "testdata", "youcam", "results", "earring-jhumka-V04.jpg"))
	pairs := map[string]bool{}
	for _, lookID := range []string{first, second} {
		body := bodyOf(t, env.get(t, cookie, "/api/looks/"+lookID))
		selfieID, _ := body["selfie_id"].(string)
		shown, _ := body["shown_render"].(map[string]any)
		if shown == nil {
			t.Fatalf("%s: the look lost its shown render: %v", lookID, body)
		}
		renderID, _ := shown["media_id"].(string)
		if got := mediaBytes(t, env, cookie, selfieID); string(got) != string(selfieRaw) {
			t.Fatalf("%s: the selfie it reads is %d bytes, not the recorded picture", lookID, len(got))
		}
		if got := mediaBytes(t, env, cookie, renderID); string(got) != string(renderRaw) {
			t.Fatalf("%s: the render it reads is %d bytes, not the recorded result", lookID, len(got))
		}
		pairs[selfieID+"|"+renderID] = true
	}
	if len(pairs) != 1 {
		t.Fatalf("the two opens named %d picture pairs, want one: the second read the first's media ids", len(pairs))
	}

	// Two opens of two different stories hold two selves, not one: the
	// search is by content, so a second sample brings its own person and
	// its own bytes.
	startSample(t, env, cookie, "sample-date", "")
	if grew, _ := ownedMedia(t, env, user); grew != held+1 {
		t.Fatalf("%d pictures after a second sample, want the family pair plus the date selfie", grew)
	}
	if spent := env.budget.Spent(); spent != 0 {
		t.Fatalf("the meter settled %d units, want zero", spent)
	}
}

// TestACachedStoryMeasuresFromItsOwnReading covers the arm no shipped
// story reaches. The honesty rules bind a cache row to the one face read
// the recordings hold, and to the picture that read was of - V06 - so a
// story of any of the five sample people can hold no analysis row at all,
// and serveStart's cache write and the measure frame's cached answer run
// on no path a judge can take today. The rule and the branch are real, so
// they are proved here on a mount-only story built in the test's own
// directory: its selfie IS the picture its row names. Nothing lands in
// testdata/samples/cached/, where a story that showed a reading the
// sample set never recorded would be the invention this file exists to
// refuse.
func TestACachedStoryMeasuresFromItsOwnReading(t *testing.T) {
	t.Parallel()
	env := newLookEnv(t, earringAnswers(), 8, nil)
	root := repoRoot(t)
	uploaded, shape := recordedFaceAttrRead(t, root)

	body := `{"kind":"sample-cache","id":"sample-family","occasion":"family-function",` +
		`"name":"Family function","variant":"V06","selfie":"` + uploaded + `",` +
		`"source":"A mount-only story for the measure arm. The selfie is the one picture the recorded face-attr flow read (testdata/youcam/face-attr-upload-src.json), so the analysis row it carries is a reading of this photo and the cache key holds it; the faceshape is that flow's recorded answer (testdata/youcam/http-face-attr-poll.json). The pick is the family fix that measures face-attr-analysis. It shows no render, because no check of this picture was ever recorded.",` +
		`"judge":{"flags":["family-hair-finished"],"issue":"family-hair-finished"},` +
		`"pick":{"fix":"family-tie-hair","runner_up":""},` +
		`"analysis":[{"task":"face-attr-analysis","face_shape":"` + shape + `","read_from":"` + uploaded + `"}]}`

	dir := sampleDirWithStory(t, body)
	if err := env.srv.MountSamples(t.Context(), SamplesConfig{SamplesDir: dir}); err != nil {
		t.Fatalf("mount the story that holds a reading: %v", err)
	}
	user, cookie := bootUser(t, env.handler)
	ctx := t.Context()

	lookID, _ := startSample(t, env, cookie, "sample-family", "")

	// The route wrote the cache: the keep loop over the story's rows ran,
	// under this judge, keyed to the photo hash the look itself carries.
	var photoHash, task, face string
	if err := env.srv.db.Reader().QueryRowContext(ctx,
		`SELECT photo_hash, task, face_shape FROM analysis_cache WHERE user_id = ?`, user).Scan(&photoHash, &task, &face); err != nil {
		t.Fatalf("the open wrote no analysis cache row for this judge: %v", err)
	}
	if task != "face-attr-analysis" || face != shape {
		t.Fatalf("the cache row is %s/%q, want face-attr-analysis/%q", task, face, shape)
	}
	var lookHash string
	if err := env.srv.db.Reader().QueryRowContext(ctx,
		`SELECT photo_hash FROM looks WHERE id = ?`, lookID).Scan(&lookHash); err != nil {
		t.Fatal(err)
	}
	if lookHash != photoHash {
		t.Fatalf("the reading is keyed to %s and the look to %s, want one photo", photoHash, lookHash)
	}

	// The measure frame's answer for a story that holds a reading: cached.
	// Not skipped, and never false, which would promise a measurement.
	sm := env.srv.samplesOf()
	st := sm.byID["sample-family"]
	_, frames, err := sm.story(st, make([]string, len(st.Attempts)))
	if err != nil {
		t.Fatal(err)
	}
	var measure string
	for _, f := range frames {
		if f.Stage == "measure" {
			measure = string(f.Detail)
		}
	}
	if measure != `{"cached":true}` {
		t.Fatalf("the measure frame of a story that holds a reading is %q, want {\"cached\":true}", measure)
	}

	// The look reads as a finished look of its own story, and the frame
	// the route published carries the same answer the loader holds: the
	// cache row is the story's reading, not a measurement.
	lookBody := bodyOf(t, env.get(t, cookie, "/api/looks/"+lookID))
	if ready, _ := lookBody["ready"].(bool); !ready {
		t.Fatalf("the look never became ready: %v", lookBody)
	}
	if fix, _ := lookBody["fix"].(map[string]any); fix == nil || fix["id"] != "family-tie-hair" {
		t.Fatalf("the look's pick %v, want family-tie-hair", lookBody["fix"])
	}
	if only, _ := lookBody["suggestion_only"].(bool); !only {
		t.Fatalf("the story shows no render and the look is not suggestion-only: %v", lookBody)
	}

	// And it spent nothing to get there: the cached answer is the whole
	// of the measure stage, so no provider was asked.
	creates, uploads, polls, fetches := env.cam.count()
	if creates != 0 || uploads != 0 || polls != 0 || fetches != 0 {
		t.Fatalf("the cached arm spent youcam calls %d %d %d %d, want all zero", creates, uploads, polls, fetches)
	}
	for _, kind := range []string{"judge", "pick", "check"} {
		if n := env.clef.count(kind); n != 0 {
			t.Fatalf("%s asked %d times, want zero", kind, n)
		}
	}
	if spent, reserved := env.budget.Spent(), env.budget.Reserved(); spent != 0 || reserved != 0 {
		t.Fatalf("the meter settled %d and held %d, want zero", spent, reserved)
	}
	if seats := judgeSessionRows(t, env); seats != 0 {
		t.Fatalf("%d judge-lease rows, want none", seats)
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

// recCheck is one recorded render check read out of a Clef check
// recording: the occasion it was read for, the original picture it read,
// the render picture it read beside that original, the change it was
// asked about in its own words, the criterion it was asked about, the
// model it ran on, the three gate nouls it answered and the look choices
// it answered, in the attribute's own id. A shown render's three facts and
// its look answers are this recording's readings at the shipped check
// lines, not the story's claim. The two pictures and the request are read
// too, because a check answers about the pair it was sent: the story that
// shows a picture this recording did not read has to say which picture it
// read, or a reader believes the shown one was checked.
type recCheck struct {
	name      string
	occasion  string
	original  string
	render    string
	request   string
	model     string
	criterion string
	Gates     map[string]float64
	Look      map[string]string
}

// recordedChecks builds the table of recorded render checks from every
// check-*.json under testdata/clef, keyed by the recording name a story's
// check carries. The sample set records four render checks, and every one
// of them is a family-function check of V04 - two on the shipped check
// model and two on clef-flash - so the honesty test reads each shown
// render's facts back out of the recording its check names, at the shipped
// check lines, and refuses a story whose facts disagree with it or whose
// shown flag is not what the discard rule makes of them.
func recordedChecks(t *testing.T, root string) map[string]recCheck {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(root, "testdata", "clef", "check-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 4 {
		t.Fatalf("%d recorded render checks, want the four the sample set records", len(names))
	}
	out := make(map[string]recCheck, len(names))
	for _, path := range names {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read check %s: %v", path, err)
		}
		var rec struct {
			Exchange string `json:"exchange"`
			Request  struct {
				Body struct {
					Model  string `json:"model"`
					Images []struct {
						File string `json:"file"`
					} `json:"images"`
					State struct {
						Occasion        string `json:"occasion"`
						Criterion       string `json:"criterion"`
						RequestedChange string `json:"requested_change"`
					} `json:"state"`
				} `json:"body"`
			} `json:"request"`
			Response struct {
				Body struct {
					Result struct {
						Answers map[string]struct {
							Noul   *float64 `json:"noul"`
							Choice string   `json:"choice"`
						} `json:"answers"`
					} `json:"result"`
				} `json:"body"`
			} `json:"response"`
		}
		if err := json.Unmarshal(raw, &rec); err != nil {
			t.Fatalf("decode check %s: %v", path, err)
		}
		rc := recCheck{
			name:      rec.Exchange,
			occasion:  rec.Request.Body.State.Occasion,
			model:     rec.Request.Body.Model,
			criterion: rec.Request.Body.State.Criterion,
			request:   rec.Request.Body.State.RequestedChange,
			Gates:     map[string]float64{},
			Look:      map[string]string{},
		}
		if len(rec.Request.Body.Images) > 0 {
			// The original is the first picture, the render the second: a
			// check reads the render against this sample's own selfie, so
			// the first is the selfie the story must name and the second is
			// the picture the answers are about.
			rc.original = strings.TrimPrefix(rec.Request.Body.Images[0].File, "../samples/")
		}
		if len(rec.Request.Body.Images) > 1 {
			rc.render = strings.TrimPrefix(rec.Request.Body.Images[1].File, "../samples/")
		}
		for qid, one := range rec.Response.Body.Result.Answers {
			if attr, isLook := strings.CutPrefix(qid, "look."); isLook {
				rc.Look[attr] = one.Choice
				continue
			}
			if one.Noul != nil {
				rc.Gates[qid] = *one.Noul
			}
		}
		out[rec.Exchange] = rc
	}
	return out
}

// derivedRunnerUp re-executes the rule the date, festival and funeral
// stories state in their source for the runner-up no recording pairs: the
// fix of the lowest-read flagged criterion the pick does not address and
// that some fix on the menu addresses. A flagged criterion no fix on the
// menu serves (funeral's expression-still and light-plain) is skipped, for
// a runner-up that cannot render is no runner-up; a criterion the pick
// already answers is skipped too. The read that decides is the recording's
// own noul, so a threshold edit that reorders the flags reorders the pick.
// A story with no pick and no flag names no runner-up, and the empty fix
// says so.
func derivedRunnerUp(occ *domain.Occasion, answers map[string]float64, flagged map[string]bool, fix string) string {
	picked := fixByID(occ, fix)
	serves := func(criterionID string) bool {
		if picked == nil {
			return false
		}
		return slices.Contains(picked.Criteria, criterionID)
	}
	// theFix is the menu fix that renders a criterion, the first to declare
	// it: a criterion no fix names has no runner-up to reach.
	theFix := func(criterionID string) string {
		for i := range occ.Fixes {
			if slices.Contains(occ.Fixes[i].Criteria, criterionID) {
				return occ.Fixes[i].ID
			}
		}
		return ""
	}
	bestFix, bestNoul := "", 0.0
	for _, c := range occ.Criteria {
		if !flagged[c.ID] || serves(c.ID) {
			continue
		}
		noul, read := answers[c.ID]
		if !read {
			continue
		}
		fid := theFix(c.ID)
		if fid == "" {
			continue
		}
		if bestFix == "" || noul < bestNoul {
			bestFix, bestNoul = fid, noul
		}
	}
	return bestFix
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

// sampleDirWithStory builds the samples directory the mount reads as the
// repository lays it out: the story directory at <base>/samples and the
// recorded provider results beside it at <base>/youcam/results, because a
// story names a render as "../youcam/results/<name>.jpg" and the loader's
// second picture root is that sibling - a samples directory without it has
// no renders to show, which is the shape requirement the task's Notes
// name. The directory holds the crafted story, the other four recorded
// stories read from testdata, and the pictures all of them name, so a
// story can be mounted as the whole set and opened through the routes
// rather than only through the loader. Every byte is a copy of a
// repository file into the test's own temporary directory: nothing here
// lands in testdata.
func sampleDirWithStory(t *testing.T, body string) string {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "samples")
	cached := filepath.Join(dir, "cached")
	if err := os.MkdirAll(cached, 0o755); err != nil {
		t.Fatal(err)
	}
	name := sampleOf(body)
	if err := os.WriteFile(filepath.Join(cached, name+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// The four stories the crafted one does not replace, so the mount
	// finds the set it expects.
	names, err := filepath.Glob(filepath.Join(samplesDir(t), "cached", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range names {
		if strings.TrimSuffix(filepath.Base(path), ".json") == name {
			continue
		}
		if err := os.WriteFile(filepath.Join(cached, filepath.Base(path)), readFile(t, path), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The pictures, in the two roots the loader reads: the people inside
	// the samples directory, the recorded results in the sibling beside it.
	root := filepath.Join(repoRoot(t), "testdata")
	for _, rel := range []string{"samples/woman", "samples/man", "samples/anchors", "youcam/results"} {
		dst := filepath.Join(dir, strings.TrimPrefix(rel, "samples/"))
		if strings.HasPrefix(rel, "youcam/") {
			dst = filepath.Join(base, rel)
		}
		for _, entry := range osMustReadDir(t, filepath.Join(root, rel)) {
			if entry.IsDir() {
				continue
			}
			raw := readFile(t, filepath.Join(root, rel, entry.Name()))
			if err := os.MkdirAll(dst, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dst, entry.Name()), raw, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

// osMustReadDir is one directory's entries, or the test's failure.
func osMustReadDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// ownedMedia counts the pictures one judge holds in the mediastore, rows
// and bytes - the unit a repeat open would have grown. The rows are the
// index's own table on the server's file, the same one a keep reads for its
// shortlist.
func ownedMedia(t *testing.T, env *lookEnv, user string) (int, int64) {
	t.Helper()
	var (
		rows  int
		bytes int64
	)
	if err := env.srv.db.Reader().QueryRowContext(t.Context(),
		`SELECT COUNT(*), COALESCE(SUM(size_bytes), 0) FROM media WHERE owner = ?`, user).Scan(&rows, &bytes); err != nil {
		t.Fatal(err)
	}
	return rows, bytes
}

// readFile is one recorded file's bytes, the answer a look's picture is
// compared with.
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// mediaBytes is what the media route gives this judge for one id: the
// route they open a look through, not the store behind it.
func mediaBytes(t *testing.T, env *lookEnv, cookie *http.Cookie, mediaID string) []byte {
	t.Helper()
	if !id.Valid(mediaID) {
		t.Fatalf("media id %q", mediaID)
	}
	rec := do(t, env.handler, http.MethodGet, "/media/"+mediaID, cookie, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("media %s status %d", mediaID, rec.Code)
	}
	return rec.Body.Bytes()
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
