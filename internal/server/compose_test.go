package server

// The composition test: Compose is what `dresscode serve` boots, and
// these tests boot it over a temp file and read every route it claims.
// The clients are the rig's own: the typed YouCam replay of
// testdata/youcam, the Clef stub, the memory fake, the mail recorder.
// Nothing dials and no unit moves except the one unit the paid look's
// recorded render settles, which is the point of the books proof: the
// settling lands in the spend_youcam namespace `dresscode budget` reads
// through a second handle while this server holds its own.
//
// The occasions and samples directories are the repository's own
// testdata, laid out the way the loader requires: the sample pictures
// read from <SamplesDir> and the sibling <SamplesDir>/../youcam/results,
// and the catalog pictures from <SamplesDir>/products.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/dresscode/internal/config"
	"github.com/nrynss/dresscode/internal/mail"
	"github.com/nrynss/dresscode/internal/memory"
	"github.com/nrynss/dresscode/internal/pipeline"
	"github.com/nrynss/dresscode/internal/systemone"
	"github.com/nrynss/dresscode/internal/youcam"
	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/gate"
	"github.com/nrynss/keel/id"
	jobsql "github.com/nrynss/keel/job/sqlitestore"
	"github.com/nrynss/keel/mediastore"
)

// composeSettings carries the budgets the books open with, the lease and
// the lines the looks read, and the two data directories, pointed at the
// repository's own data. It is the settings file `dresscode serve` loads,
// with [paths] filled in.
func composeSettings(t *testing.T) *config.Settings {
	t.Helper()
	s := *testSettings()
	s.Budgets = config.Budgets{
		YouCam:    config.YouCamBudget{Units: 1040, JudgingUnits: 400},
		WorkersAI: config.WorkersAIBudget{DollarsPerDay: 1},
	}
	s.Paths = config.Paths{
		Occasions: filepath.Join(repoRoot(t), "testdata", "occasions"),
		Samples:   samplesDir(t),
	}
	return &s
}

// composeEnv is one composed server and the clients behind it.
type composeEnv struct {
	t        *testing.T
	srv      *Server
	handler  http.Handler
	clef     *clefStub
	cam      *countedYouCam
	mem      *fakeMemory
	mail     *mail.Recorder
	dbPath   string
	settings *config.Settings
}

// newComposeEnv boots the composition over a temp file. The spending
// gate is opened wide: this test reads routes, it does not measure the
// gate, which server_test.go pins.
func newComposeEnv(t *testing.T, answers storyAnswers) *composeEnv {
	t.Helper()
	dir := t.TempDir()
	app := filepath.Join(dir, "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "index.html"), []byte(appIndex), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := youcam.Open(filepath.Join(repoRoot(t), "testdata", "youcam"))
	if err != nil {
		t.Fatalf("open the replay: %v", err)
	}
	env := &composeEnv{
		t:        t,
		clef:     newClefStub(answers),
		cam:      &countedYouCam{YouCam: rep},
		mem:      &fakeMemory{byOccasion: map[string][]memory.Hit{}},
		mail:     &mail.Recorder{},
		dbPath:   filepath.Join(dir, "dresscode.db"),
		settings: composeSettings(t),
	}
	stub := env.clef
	srv, err := Compose(context.Background(), Config{
		DBPath:        env.dbPath,
		AppDir:        app,
		SigningKey:    []byte("test-session-signing-key-32b!!!!"),
		Log:           nil,
		LookPerClient: gate.Limit{Burst: 1000, Every: time.Hour},
		LookGlobal:    gate.Limit{Burst: 1000, Every: time.Hour},
	}, Wiring{
		Settings: env.settings,
		Clef: func(*cost.Meter) (systemone.SystemOne, error) {
			return stub, nil
		},
		YouCam:  env.cam,
		Memory:  env.mem,
		Mail:    env.mail,
		CodeKey: []byte("test-signin-code-key-32b!!!!!!!"),
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	env.srv = srv
	env.handler = srv.Handler()
	return env
}

// seedSelfie puts the recorded V04 selfie into the owner's store, the
// bytes the replay matches the recorded upload by.
func (e *composeEnv) seedSelfie(t *testing.T, owner string) string {
	t.Helper()
	return e.seedImage(t, owner, "selfie", recordedImage(t, "earring-upload-src"))
}

func (e *composeEnv) seedImage(t *testing.T, owner, group string, img systemone.Image) string {
	t.Helper()
	mediaID, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	media := e.srv.intakeOf().media
	put := mediastore.Put{Owner: owner, Group: group, ContentType: img.ContentType, Visibility: mediastore.Private}
	if err := media.PersistWithID(t.Context(), mediaID, strings.NewReader(string(img.Bytes)), put); err != nil {
		t.Fatalf("seed %s: %v", mediaID, err)
	}
	return mediaID
}

// waitDone polls the job read until the job is done. A look job is read
// by its owner, so the caller's cookie rides along.
func (e *composeEnv) waitDone(jobID string, cookie *http.Cookie) {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		rec := do(e.t, e.handler, http.MethodGet, "/api/jobs/"+jobID, cookie, nil, nil)
		if rec.Code != http.StatusOK {
			e.t.Fatalf("job %d %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			e.t.Fatal(err)
		}
		switch body.Status {
		case "done":
			return
		case "error", "cancelled":
			e.t.Fatalf("job ended %s", rec.Body.String())
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("job stayed %s", body.Status)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// readTerminal reads a job's event stream over real HTTP until a
// terminal frame. ServeTopic keeps the response open after the terminal,
// so the reader cancels its own request once the frame has arrived. A
// subscriber that arrives after the story ended is answered with the
// retained terminal alone, which is what this proves for both arcs.
func (e *composeEnv) readTerminal(jobID string, cookie *http.Cookie) string {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ts := httptest.NewServer(e.handler)
	defer ts.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/jobs/"+jobID+"/events", nil)
	if err != nil {
		e.t.Fatal(err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		e.t.Fatalf("stream status %d type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("X-Accel-Buffering") != "no" {
		e.t.Fatalf("stream headers %v", resp.Header)
	}
	sc := bufio.NewScanner(resp.Body)
	var name, data string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if name == "" {
				continue
			}
			terminal := name == "done" || name == "error" || name == "cancelled" || name == "interrupted"
			if !terminal {
				e.t.Fatalf("stream carried %s (%s) before any terminal", name, data)
			}
			cancel()
			return name
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		}
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		e.t.Fatal(err)
	}
	e.t.Fatal("stream ended without a terminal frame")
	return ""
}

// composeCode asks for a sign-in code and returns the code the recorder
// accepted.
func (e *composeEnv) composeCode(cookie *http.Cookie, address string) string {
	e.t.Helper()
	rec := do(e.t, e.handler, http.MethodPost, "/api/session/code", cookie,
		strings.NewReader(fmt.Sprintf(`{"address":%q}`, address)), headerJSON)
	if rec.Code != http.StatusAccepted {
		e.t.Fatalf("code %d %s", rec.Code, rec.Body.String())
	}
	sent := e.mail.Messages()
	if len(sent) == 0 {
		e.t.Fatal("the recorder kept no code")
	}
	return sent[len(sent)-1].Code
}

// composeVerify answers the verify route and returns the rotated cookie.
func (e *composeEnv) composeVerify(cookie *http.Cookie, address, code string) *http.Cookie {
	e.t.Helper()
	rec := do(e.t, e.handler, http.MethodPost, "/api/session/code/verify", cookie,
		strings.NewReader(fmt.Sprintf(`{"address":%q,"code":%q,"choice":""}`, address, code)), headerJSON)
	if rec.Code != http.StatusOK {
		e.t.Fatalf("verify %d %s", rec.Code, rec.Body.String())
	}
	return sessionCookie(e.t, rec)
}

// TestTheComposedServerAnswers boots the composition over a temp file
// and reads every route it claims, on one server: the intake, the media
// read, the paid look's whole arc, a sample opened beside it, the history
// pair and the recall, the job reads and their streams, the four
// deletions, sign-in, and the account erasure. The books proof rides the
// same server: the paid look settles one unit into the judging share of
// the namespace `dresscode budget` reads, the sample that opens beside
// it settles nothing, and the dollars books sit at their ceiling with
// nothing spent.
func TestTheComposedServerAnswers(t *testing.T) {
	env := newComposeEnv(t, earringAnswers())
	ctx := context.Background()

	// A first visit mints a guest.
	userID, cookie := bootUser(t, env.handler)

	// The intake: a resumable upload, a photo out of it with its EXIF
	// gone, the media read, and a product by photo. The link route is
	// answered with the policy's own refusal of a private address, which
	// is decided without dialling.
	marker := "GPS 51.5000 -0.1200"
	body := jpegWithEXIF(t, 80, 40, 6, marker)
	uploadID := uploadBytes(t, env.handler, cookie, userID, groupSelfie, "image/jpeg", body)
	photo := postPhoto(t, env.handler, cookie, uploadID)
	media := do(t, env.handler, http.MethodGet, photo.URL, cookie, nil, nil)
	if media.Code != http.StatusOK || media.Header().Get("Content-Type") != "image/jpeg" {
		e2 := fmt.Sprintf("media %d %s", media.Code, media.Body.String())
		e2 += " " + media.Header().Get("Content-Type")
		t.Fatalf("the composed media read: %s", e2)
	}
	productUpload := uploadBytes(t, env.handler, cookie, userID, groupProduct, "image/jpeg", jpegWithEXIF(t, 200, 90, 0, "plain product"))
	productRec := do(t, env.handler, http.MethodPost, "/api/products", cookie,
		strings.NewReader(fmt.Sprintf(`{"upload_id":%q,"name":"Test shirt","category":"garments"}`, productUpload)), headerJSON)
	if productRec.Code != http.StatusCreated {
		t.Fatalf("product %d %s", productRec.Code, productRec.Body.String())
	}
	linkRec := do(t, env.handler, http.MethodPost, "/api/products/link", cookie,
		strings.NewReader(`{"url":"https://10.1.2.3/one.jpg"}`), headerJSON)
	assertErr(t, linkRec, http.StatusUnprocessableEntity, "fetch_blocked_address", "That link was refused.")

	// The paid look: the recorded story, the jhumkas picked and shown,
	// the catalog picture joined onto the shop list out of the pictures
	// the composition seeded from the samples directory. The opt-in rows
	// ride in the way the rig's own tests seat them, through the same
	// table the opt-in route is meant to serve.
	if _, err := env.srv.db.Writer().ExecContext(ctx,
		`INSERT INTO opt_ins (user_id, occasion, fix_id) VALUES (?, ?, ?)`,
		userID, "family-function", "family-gold-jhumkas"); err != nil {
		t.Fatalf("opt in: %v", err)
	}
	if _, err := env.srv.db.Writer().ExecContext(ctx,
		`INSERT INTO opt_ins (user_id, occasion, fix_id) VALUES (?, ?, ?)`,
		userID, "family-function", "family-festive-top"); err != nil {
		t.Fatalf("opt in: %v", err)
	}
	selfieID := env.seedSelfie(t, userID)
	start := do(t, env.handler, http.MethodPost, "/api/looks", cookie,
		strings.NewReader(fmt.Sprintf(`{"occasion":"family-function","selfie_id":%q}`, selfieID)), headerJSON)
	if start.Code != http.StatusAccepted {
		t.Fatalf("start %d %s", start.Code, start.Body.String())
	}
	var started struct {
		LookID string `json:"look_id"`
		JobID  string `json:"job_id"`
	}
	if err := json.Unmarshal(start.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if !id.Valid(started.LookID) || started.LookID != started.JobID {
		t.Fatalf("start ids %+v", started)
	}
	env.waitDone(started.JobID, cookie)

	verdict := do(t, env.handler, http.MethodGet, "/api/looks/"+started.LookID, cookie, nil, nil)
	if verdict.Code != http.StatusOK {
		t.Fatalf("look %d %s", verdict.Code, verdict.Body.String())
	}
	var look struct {
		Ready bool `json:"ready"`
		Fix   *struct {
			ID string `json:"id"`
		} `json:"fix"`
		ShownRender *struct {
			MediaID string `json:"media_id"`
		} `json:"shown_render"`
		SuggestionOnly bool `json:"suggestion_only"`
		Products       []struct {
			ID      string `json:"id"`
			MediaID string `json:"media_id"`
		} `json:"products"`
	}
	if err := json.Unmarshal(verdict.Body.Bytes(), &look); err != nil {
		t.Fatal(err)
	}
	if !look.Ready || look.Fix == nil || look.Fix.ID != "family-gold-jhumkas" || look.ShownRender == nil || look.ShownRender.MediaID == "" {
		t.Fatalf("look body: %s", verdict.Body.String())
	}
	renders := do(t, env.handler, http.MethodGet, "/api/looks/"+started.LookID+"/renders", cookie, nil, nil)
	if renders.Code != http.StatusOK || !strings.Contains(renders.Body.String(), "family-gold-jhumkas") {
		t.Fatalf("renders %d %s", renders.Code, renders.Body.String())
	}
	shop := do(t, env.handler, http.MethodGet, "/api/looks/"+started.LookID+"/shop", cookie, nil, nil)
	if shop.Code != http.StatusOK || !strings.Contains(shop.Body.String(), "/media/") {
		t.Fatalf("shop %d %s", shop.Code, shop.Body.String())
	}
	kept := do(t, env.handler, http.MethodPut, "/api/looks/"+started.LookID+"/verdict", cookie,
		strings.NewReader(`{"verdict":"kept"}`), headerJSON)
	if kept.Code != http.StatusOK || !strings.Contains(kept.Body.String(), `"remembered":false`) {
		t.Fatalf("verdict %d %s", kept.Code, kept.Body.String())
	}
	list := do(t, env.handler, http.MethodGet, "/api/looks?occasion=family-function", cookie, nil, nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), started.LookID) {
		t.Fatalf("list %d %s", list.Code, list.Body.String())
	}
	if env.readTerminal(started.JobID, cookie) != "done" {
		t.Fatal("the paid look's stream kept no done terminal")
	}

	// History: off until asked, then the recall answers with the
	// occasion and no line, because nothing was remembered yet.
	history := do(t, env.handler, http.MethodGet, "/api/history", cookie, nil, nil)
	if history.Code != http.StatusOK || !strings.Contains(history.Body.String(), `"enabled":false`) {
		t.Fatalf("history %d %s", history.Code, history.Body.String())
	}
	turned := do(t, env.handler, http.MethodPut, "/api/history", cookie,
		strings.NewReader(`{"enabled":true}`), headerJSON)
	if turned.Code != http.StatusOK || !strings.Contains(turned.Body.String(), `"enabled":true`) {
		t.Fatalf("history put %d %s", turned.Code, turned.Body.String())
	}
	recall := do(t, env.handler, http.MethodGet, "/api/history/recall?occasion=family-function", cookie, nil, nil)
	if recall.Code != http.StatusOK || !strings.Contains(recall.Body.String(), `"line":""`) {
		t.Fatalf("recall %d %s", recall.Code, recall.Body.String())
	}

	// A sample opened beside the paid look, on the same server: born
	// finished, spending nothing, its stream answering with the retained
	// terminal.
	samples := do(t, env.handler, http.MethodGet, "/api/samples", cookie, nil, nil)
	if samples.Code != http.StatusOK || strings.Count(samples.Body.String(), `"id"`) != 5 {
		t.Fatalf("samples %d %s", samples.Code, samples.Body.String())
	}
	sampleStart := do(t, env.handler, http.MethodPost, "/api/samples/sample-family/looks", cookie, nil, headerJSON)
	if sampleStart.Code != http.StatusAccepted {
		t.Fatalf("sample start %d %s", sampleStart.Code, sampleStart.Body.String())
	}
	var sample struct {
		LookID string `json:"look_id"`
		JobID  string `json:"job_id"`
	}
	if err := json.Unmarshal(sampleStart.Body.Bytes(), &sample); err != nil {
		t.Fatal(err)
	}
	sampleRead := do(t, env.handler, http.MethodGet, "/api/looks/"+sample.LookID, cookie, nil, nil)
	if sampleRead.Code != http.StatusOK || !strings.Contains(sampleRead.Body.String(), `"ready":true`) {
		t.Fatalf("sample look %d %s", sampleRead.Code, sampleRead.Body.String())
	}
	if env.readTerminal(sample.JobID, cookie) != "done" {
		t.Fatal("the sample's stream kept no done terminal")
	}

	// The books: the paid look settled one unit into the judging share;
	// the sample beside it settled nothing; development and the day's
	// dollars sit untouched. Budget is the exact call `dresscode budget`
	// makes, through a second handle while this server holds its own.
	camBefore, _, pollsBefore, fetchesBefore := env.cam.count()
	rep, err := Budget(ctx, env.dbPath, env.settings)
	if err != nil {
		t.Fatalf("budget: %v", err)
	}
	if rep.Units.Judging.Spent != 1 || rep.Units.Judging.Left != 399 {
		t.Fatalf("judging books %+v, want one unit settled", rep.Units.Judging)
	}
	if rep.Units.Development.Spent != 0 || rep.Units.Development.Left != 640 {
		t.Fatalf("development books %+v", rep.Units.Development)
	}
	if rep.Units.Spent != 1 || rep.Units.Left != 1039 {
		t.Fatalf("units books %+v", rep.Units)
	}
	if rep.Dollars.Spent != 0 || rep.Dollars.PerDay != cost.Dollar {
		t.Fatalf("dollar books %+v", rep.Dollars)
	}

	// The four deletions. History goes first, then the look with its
	// render, then the photo with what used it; the account erasure runs
	// last, over an owner the sign-in routes just made.
	historyDel := do(t, env.handler, http.MethodPost, "/api/history/deletion", cookie, nil, headerJSON)
	if historyDel.Code != http.StatusAccepted {
		t.Fatalf("history deletion %d %s", historyDel.Code, historyDel.Body.String())
	}
	var historyJob struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(historyDel.Body.Bytes(), &historyJob); err != nil {
		t.Fatal(err)
	}
	env.waitDone(historyJob.JobID, cookie)

	lookDel := do(t, env.handler, http.MethodPost, "/api/looks/"+started.LookID+"/deletion", cookie, nil, headerJSON)
	if lookDel.Code != http.StatusAccepted {
		t.Fatalf("look deletion %d %s", lookDel.Code, lookDel.Body.String())
	}
	var lookJob struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(lookDel.Body.Bytes(), &lookJob); err != nil {
		t.Fatal(err)
	}
	env.waitDone(lookJob.JobID, cookie)

	photoDel := do(t, env.handler, http.MethodPost, "/api/photos/"+photo.MediaID+"/deletion", cookie, nil, headerJSON)
	if photoDel.Code != http.StatusAccepted {
		t.Fatalf("photo deletion %d %s", photoDel.Code, photoDel.Body.String())
	}
	var photoJob struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(photoDel.Body.Bytes(), &photoJob); err != nil {
		t.Fatal(err)
	}
	env.waitDone(photoJob.JobID, cookie)

	// Sign-in: a code arrives through the composition's own recorder,
	// the verify rotates the cookie, and the session reads owner.
	const address = "judge@example.com"
	code := env.composeCode(cookie, address)
	ownerCookie := env.composeVerify(cookie, address, code)
	sessionBody := do(t, env.handler, http.MethodGet, "/api/session", ownerCookie, nil, nil)
	if sessionBody.Code != http.StatusOK {
		t.Fatalf("owner session %d %s", sessionBody.Code, sessionBody.Body.String())
	}
	var ownerSession struct {
		UserID string `json:"user_id"`
		Kind   string `json:"kind"`
	}
	if err := json.Unmarshal(sessionBody.Body.Bytes(), &ownerSession); err != nil {
		t.Fatal(err)
	}
	if ownerSession.Kind != "owner" || ownerSession.UserID != userID {
		t.Fatalf("after verify the session is %+v, want the same user as owner", ownerSession)
	}

	// Account deletion: a fresh code confirms it, the response carries
	// the capability, and the capability reads without a session. The
	// account's own bytes go; the catalog's do not, because the catalog
	// is not user data and its owner is not a user id.
	fresh := env.composeCode(ownerCookie, address)
	accountDel := do(t, env.handler, http.MethodPost, "/api/account/deletion", ownerCookie,
		strings.NewReader(fmt.Sprintf(`{"address":%q,"code":%q}`, address, fresh)), headerJSON)
	if accountDel.Code != http.StatusAccepted {
		t.Fatalf("account deletion %d %s", accountDel.Code, accountDel.Body.String())
	}
	var account struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(accountDel.Body.Bytes(), &account); err != nil {
		t.Fatal(err)
	}
	env.waitDone(account.JobID, nil)
	gone := do(t, env.handler, http.MethodGet, "/media/"+photo.MediaID, ownerCookie, nil, nil)
	if gone.Code != http.StatusNotFound {
		t.Fatalf("the erased account's blob reads %d", gone.Code)
	}
	still := do(t, env.handler, http.MethodGet, "/media/"+catalogBlobID("gold-jhumkas"), nil, nil, nil)
	if still.Code != http.StatusOK {
		t.Fatalf("the catalog picture reads %d after the erasure, want 200", still.Code)
	}

	// The drain: every job is terminal, so the posture's second step
	// returns at once, and the books still read one settled unit.
	if err := env.srv.Drain(ctx, 25*time.Millisecond); err != nil {
		t.Fatalf("drain: %v", err)
	}
	rep, err = Budget(ctx, env.dbPath, env.settings)
	if err != nil {
		t.Fatalf("budget after the deletions: %v", err)
	}
	if rep.Units.Judging.Spent != 1 {
		t.Fatalf("judging books after the deletions %+v, want the one settled unit", rep.Units.Judging)
	}
	camAfter, _, pollsAfter, fetchesAfter := env.cam.count()
	if camAfter != camBefore || pollsAfter != pollsBefore || fetchesAfter != fetchesBefore {
		t.Fatalf("the deletions spent youcam calls: %d/%d %d/%d", camBefore, camAfter, pollsBefore, pollsAfter)
	}
}

// TestAFlagFlipReachesTheComposedServer is the second proof: the flip
// goes through SetFlag, the exact call `dresscode flag` makes, on a
// second handle while the composed server holds its own, and the next
// request the server answers carries it, with no restart.
func TestAFlagFlipReachesTheComposedServer(t *testing.T) {
	env := newComposeEnv(t, earringAnswers())
	_, cookie := bootUser(t, env.handler)

	open := func() int {
		rec := do(t, env.handler, http.MethodPost, "/api/uploads", cookie,
			strings.NewReader(`{"content_type":"image/jpeg","chunk_size":100,"size_bytes":10,"group":"selfie"}`), headerJSON)
		return rec.Code
	}
	if code := open(); code != http.StatusCreated {
		t.Fatalf("a selfie open answers %d before the flip, want 201", code)
	}
	if err := SetFlag(context.Background(), env.dbPath, config.FlagFreshUploadsOff, true); err != nil {
		t.Fatalf("set the flag: %v", err)
	}
	if code := open(); code != http.StatusForbidden {
		t.Fatalf("a selfie open answers %d with the flag on, want 403", code)
	}
	// The flag refuses fresh selfies, not products: a product open still
	// proceeds.
	productOpen := do(t, env.handler, http.MethodPost, "/api/uploads", cookie,
		strings.NewReader(`{"content_type":"image/jpeg","chunk_size":100,"size_bytes":10,"group":"product"}`), headerJSON)
	if productOpen.Code != http.StatusCreated {
		t.Fatalf("a product open answers %d with the flag on, want 201", productOpen.Code)
	}
	if err := SetFlag(context.Background(), env.dbPath, config.FlagFreshUploadsOff, false); err != nil {
		t.Fatalf("clear the flag: %v", err)
	}
	if code := open(); code != http.StatusCreated {
		t.Fatalf("a selfie open answers %d after the flag clears, want 201", code)
	}
}

// TestAComposedRestartInterruptsAnUnfinishedLookRow is the restart half
// of the posture: a look row left running by a dead process is
// interrupted by the composed boot's own recovery, no replacement runs
// without a snapshot to resume from, and nothing is spent finding out.
func TestAComposedRestartInterruptsAnUnfinishedLookRow(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "index.html"), []byte(appIndex), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "dresscode.db")
	ctx := context.Background()

	// The dead process's row: one attempt, running, no snapshot. A
	// runner open over the same table with this row in view would hang
	// waiting on a lease; the composed boot's recovery must interrupt it
	// at once. The schema comes from a bare Open, the way a first boot
	// makes the file, then the row, then the close.
	first, err := Open(ctx, Config{
		DBPath:     dbPath,
		AppDir:     app,
		SigningKey: []byte("test-session-signing-key-32b!!!!"),
	})
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	// The jobs table is Keel's job store's, so the schema lands with the
	// store's open, the way every mount opens it.
	jobSchema, err := jobsql.Open(ctx, jobsql.Config{DB: first.db})
	if err != nil {
		t.Fatalf("job schema: %v", err)
	}
	_ = jobSchema
	jobID, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.db.Writer().ExecContext(ctx,
		`INSERT INTO jobs (id, kind, status, attempt, parent_id, root_id, progress, updated_at)
		 VALUES (?, ?, 'running', 1, '', ?, '', 1)`,
		jobID, pipeline.KindName, jobID); err != nil {
		t.Fatalf("seed the unfinished row: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	rep, err := youcam.Open(filepath.Join(repoRoot(t), "testdata", "youcam"))
	if err != nil {
		t.Fatalf("open the replay: %v", err)
	}
	env := &composeEnv{
		t:        t,
		clef:     newClefStub(earringAnswers()),
		cam:      &countedYouCam{YouCam: rep},
		mem:      &fakeMemory{byOccasion: map[string][]memory.Hit{}},
		mail:     &mail.Recorder{},
		dbPath:   dbPath,
		settings: composeSettings(t),
	}
	// The composed boot over the same file.
	stub := env.clef
	srv, err := Compose(ctx, Config{
		DBPath:        dbPath,
		AppDir:        app,
		SigningKey:    []byte("test-session-signing-key-32b!!!!"),
		LookPerClient: gate.Limit{Burst: 1000, Every: time.Hour},
		LookGlobal:    gate.Limit{Burst: 1000, Every: time.Hour},
	}, Wiring{
		Settings: env.settings,
		Clef: func(*cost.Meter) (systemone.SystemOne, error) {
			return stub, nil
		},
		YouCam:  env.cam,
		Memory:  env.mem,
		Mail:    env.mail,
		CodeKey: []byte("test-signin-code-key-32b!!!!!!!"),
	})
	if err != nil {
		t.Fatalf("the composed restart: %v", err)
	}
	defer func() { _ = srv.Close() }()

	var status string
	if err := srv.db.Reader().QueryRowContext(ctx,
		`SELECT status FROM jobs WHERE id = ?`, jobID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "interrupted" {
		t.Fatalf("the unfinished look row is %s after the composed restart, want interrupted", status)
	}
	var attempts int
	if err := srv.db.Reader().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs WHERE root_id = ?`, jobID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("%d attempts of the unfinished row, want one", attempts)
	}
	creates, uploads, polls, fetches := env.cam.count()
	if creates != 0 || uploads != 0 || polls != 0 || fetches != 0 {
		t.Fatalf("the restart spent youcam calls %d %d %d %d, want all zero", creates, uploads, polls, fetches)
	}
}
