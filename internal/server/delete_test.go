package server_test

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/memory"
	"github.com/nrynss/dresscode/internal/server"
	"github.com/nrynss/dresscode/internal/store"
	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/identity"
	identitysql "github.com/nrynss/keel/identity/sqlitestore"
	"github.com/nrynss/keel/mediastore"
	"github.com/nrynss/keel/sqlite"
)

func TestAccountDeletionNeedsTheHolder(t *testing.T) {
	t.Parallel()
	env := newDeletion(t, nil)
	owner := env.signIn("person@example.com")
	env.seed(owner.userID)

	guest := request(t, env.handler, http.MethodGet, "/api/session", nil, "")
	guestCookie := oneSessionCookie(t, guest)
	refused := postJSON(t, env.handler, "/api/account/deletion", guestCookie,
		`{"address":"person@example.com","code":"000000"}`)
	if refused.Code != http.StatusNotFound || decodeError(t, refused.Body.String()).Code != "not_found" {
		t.Fatalf("guest delete status %d body %s", refused.Code, refused.Body.String())
	}
	if count(t, env.db, store.TableLooks, owner.userID) != 1 {
		t.Fatal("a guest delete removed the account's look")
	}

	unknown := postJSON(t, env.handler, "/api/account/deletion", owner.cookie,
		`{"address":"nobody@example.com","code":"000000"}`)
	if unknown.Code != http.StatusNotFound || decodeError(t, unknown.Body.String()).Code != "not_found" {
		t.Fatalf("unknown address status %d body %s", unknown.Code, unknown.Body.String())
	}

	bad := postJSON(t, env.handler, "/api/account/deletion", owner.cookie, `{`)
	if bad.Code != http.StatusBadRequest || decodeError(t, bad.Body.String()).Code != "invalid_request" {
		t.Fatalf("bad body status %d body %s", bad.Code, bad.Body.String())
	}
	empty := postJSON(t, env.handler, "/api/account/deletion", owner.cookie, `{"address":"","code":""}`)
	if empty.Code != http.StatusBadRequest || decodeError(t, empty.Body.String()).Code != "invalid_request" {
		t.Fatalf("empty body status %d body %s", empty.Code, empty.Body.String())
	}
	wrong := postJSON(t, env.handler, "/api/account/deletion", owner.cookie,
		`{"address":"person@example.com","code":"000000"}`)
	if wrong.Code != http.StatusUnauthorized || decodeError(t, wrong.Body.String()).Code != "invalid_code" {
		t.Fatalf("wrong code status %d body %s", wrong.Code, wrong.Body.String())
	}
	if count(t, env.db, store.TableLooks, owner.userID) != 1 {
		t.Fatal("a refused code removed the look")
	}
}

func TestAccountDeletionStreamsDelete(t *testing.T) {
	t.Parallel()
	hold := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	t.Cleanup(release)
	env := newDeletion(t, hold)
	owner := env.signIn("person@example.com")
	env.seed(owner.userID)
	env.blobs.add(owner.userID+"-selfie", owner.userID)
	env.mem.add(mustSession(t, owner.userID))
	other := "other-person"
	env.seed(other)
	env.blobs.add(other+"-selfie", other)

	code := env.code(owner.sessionID, "person@example.com")
	accepted := postJSON(t, env.handler, "/api/account/deletion", owner.cookie,
		`{"address":"person@example.com","code":"`+code+`"}`)
	if accepted.Code != http.StatusAccepted {
		t.Fatalf("delete status %d body %s", accepted.Code, accepted.Body.String())
	}
	if !cookieCleared(accepted) {
		t.Fatalf("session cookie was not cleared: %v", accepted.Header())
	}
	jobID := decodeJob(t, accepted.Body.String())

	select {
	case <-env.mem.started:
	case <-time.After(10 * time.Second):
		t.Fatal("memory delete never started")
	}
	waitUntil(t, func() bool {
		return count(t, env.db, store.TableLooks, owner.userID) == 0 &&
			env.blobs.count(owner.userID) == 0 &&
			env.mem.has(mustSession(t, owner.userID))
	})
	state := request(t, env.handler, http.MethodGet, "/api/jobs/"+jobID, nil, "")
	if state.Code != http.StatusOK {
		t.Fatalf("job state %d %s", state.Code, state.Body.String())
	}
	var running struct {
		JobID  string `json:"job_id"`
		RootID string `json:"root_id"`
		Status string `json:"status"`
		Stage  string `json:"stage"`
	}
	decodeBody(t, state.Body.String(), &running)
	if running.JobID != jobID || running.RootID != jobID || running.Status != "running" || running.Stage != "delete" {
		t.Fatalf("running state %+v", running)
	}

	events := env.readEvents(jobID, release)
	var sawRows bool
	var sawDone bool
	for _, ev := range events {
		switch ev.Name {
		case "progress":
			var payload struct {
				JobID   string          `json:"job_id"`
				Stage   string          `json:"stage"`
				Current *int64          `json:"current"`
				Total   *int64          `json:"total"`
				Detail  json.RawMessage `json:"detail"`
			}
			if err := json.Unmarshal(ev.Data, &payload); err != nil {
				t.Fatalf("progress: %v", err)
			}
			if payload.JobID != jobID || payload.Stage != "delete" || payload.Current != nil || payload.Total != nil {
				t.Fatalf("progress payload %s", ev.Data)
			}
			var detail struct {
				Version  int    `json:"version"`
				User     string `json:"user"`
				RowsDone bool   `json:"rows_done"`
			}
			if err := json.Unmarshal(payload.Detail, &detail); err != nil {
				t.Fatalf("detail: %v", err)
			}
			if detail.Version != 1 || detail.User != owner.userID {
				t.Fatalf("detail %+v", detail)
			}
			if detail.RowsDone {
				sawRows = true
			}
		case "done":
			sawDone = true
		}
	}
	if !sawRows || !sawDone {
		t.Fatalf("stream missing rows_done or done: %+v", events)
	}
	waitUntil(t, func() bool { return usersOf(t, env.db, owner.userID) == 0 })
	if env.blobs.count(owner.userID) != 0 || env.mem.has(mustSession(t, owner.userID)) {
		t.Fatal("account blobs or memory survived")
	}
	if count(t, env.db, store.TableLooks, other) != 1 || env.blobs.count(other) != 1 {
		t.Fatal("deletion removed the other account")
	}
	done := request(t, env.handler, http.MethodGet, "/api/jobs/"+jobID, nil, "")
	var finished struct {
		Status string `json:"status"`
		Stage  string `json:"stage"`
	}
	decodeBody(t, done.Body.String(), &finished)
	if finished.Status != "done" || finished.Stage != "delete" {
		t.Fatalf("finished state %+v body %s", finished, done.Body.String())
	}
	missing := request(t, env.handler, http.MethodGet, "/api/jobs/"+strings.Repeat("ab", 16), nil, "")
	assertNotFound(t, missing)
}

func TestScopedDeletionRoutes(t *testing.T) {
	t.Parallel()
	env := newDeletion(t, nil)
	owner := env.signIn("person@example.com")
	photoID := mustID(t)
	lookID := mustID(t)
	mediaID := mustID(t)
	env.insertLook(owner.userID, lookID, photoID, mediaID)
	env.blobs.add(photoID, owner.userID)
	env.blobs.add(mediaID, owner.userID)
	env.mem.add(mustSession(t, owner.userID))

	otherLook := mustID(t)
	otherPhoto := mustID(t)
	env.insertLook("other-person", otherLook, otherPhoto, mustID(t))
	env.blobs.add(otherPhoto, "other-person")

	denied := postJSON(t, env.handler, "/api/photos/"+otherPhoto+"/deletion", owner.cookie, "")
	assertNotFound(t, denied)
	if !env.blobs.has(otherPhoto) {
		t.Fatal("a stranger's photo was deleted")
	}
	malformed := postJSON(t, env.handler, "/api/photos/not-an-id/deletion", owner.cookie, "")
	assertNotFound(t, malformed)

	photo := postJSON(t, env.handler, "/api/photos/"+photoID+"/deletion", owner.cookie, "")
	if photo.Code != http.StatusAccepted {
		t.Fatalf("photo status %d %s", photo.Code, photo.Body.String())
	}
	env.waitJob(decodeJob(t, photo.Body.String()))
	if count(t, env.db, store.TableLooks, owner.userID) != 0 || env.blobs.count(owner.userID) != 0 {
		t.Fatal("photo route left the look or a blob")
	}
	if count(t, env.db, store.TableLooks, "other-person") != 1 {
		t.Fatal("photo route removed the other look")
	}
	again := postJSON(t, env.handler, "/api/photos/"+photoID+"/deletion", owner.cookie, "")
	assertNotFound(t, again)

	secondLook := mustID(t)
	secondPhoto := mustID(t)
	secondMedia := mustID(t)
	env.insertLook(owner.userID, secondLook, secondPhoto, secondMedia)
	env.blobs.add(secondPhoto, owner.userID)
	env.blobs.add(secondMedia, owner.userID)
	if _, err := env.db.Writer().ExecContext(t.Context(),
		`INSERT INTO history_switch (user_id, enabled) VALUES (?, 1)`, owner.userID); err != nil {
		t.Fatalf("history: %v", err)
	}
	look := postJSON(t, env.handler, "/api/looks/"+secondLook+"/deletion", owner.cookie, "")
	if look.Code != http.StatusAccepted {
		t.Fatalf("look status %d %s", look.Code, look.Body.String())
	}
	env.waitJob(decodeJob(t, look.Body.String()))
	if count(t, env.db, store.TableLooks, owner.userID) != 0 || env.blobs.has(secondMedia) {
		t.Fatal("look route left the look or the render")
	}
	if !env.blobs.has(secondPhoto) {
		t.Fatal("look route removed the selfie")
	}

	history := postJSON(t, env.handler, "/api/history/deletion", owner.cookie, "")
	if history.Code != http.StatusAccepted {
		t.Fatalf("history status %d %s", history.Code, history.Body.String())
	}
	env.waitJob(decodeJob(t, history.Body.String()))
	if count(t, env.db, store.TableHistorySwitch, owner.userID) != 0 || env.mem.has(mustSession(t, owner.userID)) {
		t.Fatal("history route left the switch or the session")
	}
	repeat := postJSON(t, env.handler, "/api/history/deletion", owner.cookie, "")
	assertNotFound(t, repeat)
}

type deletionEnv struct {
	t       *testing.T
	handler http.Handler
	db      *sqlite.DB
	helper  *identity.Service
	mail    *box
	blobs   *blobFake
	mem     *memoryFake
}

type signedOwner struct {
	userID    string
	sessionID string
	cookie    *http.Cookie
}

func newDeletion(t *testing.T, hold chan struct{}) *deletionEnv {
	t.Helper()
	dir, app := writeApp(t)
	cfg := testConfig(dir, app)
	srv, err := server.Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	db := openDB(t, cfg.DBPath)
	mail := &box{}
	blobs := newBlobFake()
	mem := newMemoryFake(hold)
	if err := srv.MountDeletion(t.Context(), server.DeletionConfig{
		SigningKey: cfg.SigningKey,
		CodeKey:    []byte("deletion-code-key"),
		Mail:       mail,
		Blobs:      blobs,
		Memory:     mem,
		Log:        slog.New(slog.DiscardHandler),
	}); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if err := srv.MountDeletion(t.Context(), server.DeletionConfig{
		SigningKey: cfg.SigningKey,
		CodeKey:    []byte("deletion-code-key"),
		Blobs:      blobs,
		Memory:     mem,
	}); err == nil {
		t.Fatal("second mount succeeded")
	}
	idStore, err := identitysql.Open(t.Context(), identitysql.Config{DB: db})
	if err != nil {
		t.Fatalf("helper store: %v", err)
	}
	helper, err := identity.New(identity.Config{
		Store:      idStore,
		SigningKey: append([]byte(nil), cfg.SigningKey...),
		CodeKey:    []byte("deletion-code-key"),
		Mail:       mail,
		Log:        slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("helper: %v", err)
	}
	return &deletionEnv{
		t: t, handler: srv.Handler(), db: db, helper: helper, mail: mail,
		blobs: blobs, mem: mem,
	}
}

func (e *deletionEnv) signIn(address string) signedOwner {
	e.t.Helper()
	boot := request(e.t, e.handler, http.MethodGet, "/api/session", nil, "")
	userID, kind := decodeSession(e.t, boot.Body.String())
	if kind != identity.KindGuest {
		e.t.Fatalf("kind %s", kind)
	}
	cookie := oneSessionCookie(e.t, boot)
	sessionID := sessionID(e.t, cookie.Value)
	if err := e.helper.RequestSignInCode(e.t.Context(), sessionID, address); err != nil {
		e.t.Fatalf("request code: %v", err)
	}
	outcome, err := e.helper.VerifySignInCode(e.t.Context(), sessionID, userID, address, e.mail.last(), "")
	if err != nil {
		e.t.Fatalf("verify: %v", err)
	}
	rec := httptest.NewRecorder()
	e.helper.SetSessionCookie(rec, outcome.SessionID)
	next := oneSessionCookie(e.t, rec)
	return signedOwner{userID: outcome.UserID, sessionID: outcome.SessionID, cookie: next}
}

func (e *deletionEnv) code(sessionID, address string) string {
	e.t.Helper()
	if err := e.helper.RequestSignInCode(e.t.Context(), sessionID, address); err != nil {
		e.t.Fatalf("request code: %v", err)
	}
	code := e.mail.last()
	if code == "" {
		e.t.Fatal("mail kept no code")
	}
	return code
}

func (e *deletionEnv) seed(user string) {
	e.t.Helper()
	ctx := e.t.Context()
	if _, err := e.db.Writer().ExecContext(ctx, `INSERT INTO looks
		(id, user_id, selfie_id, photo_hash, occasion, fix, runner_up, verdict, created_at)
		VALUES (?, ?, ?, ?, 'family-function', 'family-gold-jhumkas', '', ?, 1)`,
		user+"-look", user, user+"-selfie", user+"-hash", string(domain.VerdictKept)); err != nil {
		e.t.Fatalf("seed look: %v", err)
	}
}

func (e *deletionEnv) insertLook(user, lookID, photoID, mediaID string) {
	e.t.Helper()
	ctx := e.t.Context()
	if _, err := e.db.Writer().ExecContext(ctx, `INSERT INTO looks
		(id, user_id, selfie_id, photo_hash, occasion, verdict, created_at)
		VALUES (?, ?, ?, ?, 'family-function', '', 1)`,
		lookID, user, photoID, photoID+"-hash"); err != nil {
		e.t.Fatalf("insert look: %v", err)
	}
	if _, err := e.db.Writer().ExecContext(ctx, `INSERT INTO renders
		(id, look_id, user_id, media_id, fix_id, position, discarded)
		VALUES (?, ?, ?, ?, 'family-gold-jhumkas', 0, 0)`,
		lookID+"-render", lookID, user, mediaID); err != nil {
		e.t.Fatalf("insert render: %v", err)
	}
}

func (e *deletionEnv) waitJob(id string) {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		rec := request(e.t, e.handler, http.MethodGet, "/api/jobs/"+id, nil, "")
		if rec.Code != http.StatusOK {
			e.t.Fatalf("job %d %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Status string `json:"status"`
		}
		decodeBody(e.t, rec.Body.String(), &body)
		switch body.Status {
		case "done":
			return
		case "error", "cancelled":
			e.t.Fatalf("job ended %s", rec.Body.String())
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("job stayed %s", body.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (e *deletionEnv) readEvents(jobID string, release func()) []streamEvent {
	e.t.Helper()
	// ServeTopic keeps the response open after the terminal frame. The
	// caller stops it once that frame has arrived.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ts := httptest.NewServer(e.handler)
	defer ts.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/jobs/"+jobID+"/events", nil)
	if err != nil {
		e.t.Fatal(err)
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
	release()
	sc := bufio.NewScanner(resp.Body)
	var out []streamEvent
	var name, data string
	flush := func() bool {
		if name == "" && data == "" {
			return false
		}
		out = append(out, streamEvent{Name: name, Data: json.RawMessage(data)})
		terminal := name == "done" || name == "error" || name == "cancelled" || name == "interrupted"
		name, data = "", ""
		return terminal
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if flush() {
				cancel()
				return out
			}
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		}
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		e.t.Fatal(err)
	}
	e.t.Fatalf("stream ended without a terminal frame: %+v", out)
	return nil
}

type streamEvent struct {
	Name string
	Data json.RawMessage
}

func postJSON(t *testing.T, h http.Handler, path string, cookie *http.Cookie, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.RemoteAddr = "192.0.2.1:1234"
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeJob(t *testing.T, raw string) string {
	t.Helper()
	var body struct {
		JobID string `json:"job_id"`
	}
	decodeBody(t, raw, &body)
	if !id.Valid(body.JobID) {
		t.Fatalf("job id %q", body.JobID)
	}
	return body.JobID
}

func decodeBody(t *testing.T, raw string, dest any) {
	t.Helper()
	if !strings.HasSuffix(raw, "\n") {
		t.Fatalf("body has no newline: %q", raw)
	}
	if err := json.Unmarshal([]byte(raw), dest); err != nil {
		t.Fatalf("decode: %v body %s", err, raw)
	}
}

func cookieCleared(rec *httptest.ResponseRecorder) bool {
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "identity_session" && cookie.MaxAge < 0 {
			return true
		}
	}
	return false
}

func count(t *testing.T, db *sqlite.DB, table, user string) int {
	t.Helper()
	var n int
	query := `SELECT COUNT(*) FROM "` + table + `" WHERE user_id = ?`
	if err := db.Reader().QueryRowContext(t.Context(), query, user).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func usersOf(t *testing.T, db *sqlite.DB, user string) int {
	t.Helper()
	var n int
	if err := db.Reader().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM users WHERE id = ?`, user).Scan(&n); err != nil {
		t.Fatalf("users: %v", err)
	}
	return n
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func mustID(t *testing.T) string {
	t.Helper()
	value, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustSession(t *testing.T, account string) string {
	t.Helper()
	value, err := memory.Session(account)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

type box struct {
	mu   sync.Mutex
	code string
}

func (b *box) Send(_ context.Context, _, code string) error {
	b.mu.Lock()
	b.code = code
	b.mu.Unlock()
	return nil
}

func (b *box) last() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.code
}

type blobFake struct {
	mu    sync.Mutex
	items map[string]mediastore.Blob
}

func newBlobFake() *blobFake {
	return &blobFake{items: map[string]mediastore.Blob{}}
}

func (f *blobFake) add(id, owner string) {
	f.mu.Lock()
	f.items[id] = mediastore.Blob{ID: id, Owner: owner}
	f.mu.Unlock()
}

func (f *blobFake) has(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.items[id]
	return ok
}

func (f *blobFake) count(owner string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, blob := range f.items {
		if blob.Owner == owner {
			n++
		}
	}
	return n
}

func (f *blobFake) Blobs(ctx context.Context) ([]mediastore.Blob, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]mediastore.Blob, 0, len(f.items))
	for _, blob := range f.items {
		out = append(out, blob)
	}
	return out, nil
}

func (f *blobFake) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.items[id]; !ok {
		return mediastore.ErrNotFound
	}
	delete(f.items, id)
	return nil
}

type memoryFake struct {
	mu      sync.Mutex
	live    map[string]bool
	hold    chan struct{}
	started chan struct{}
	once    sync.Once
}

func newMemoryFake(hold chan struct{}) *memoryFake {
	return &memoryFake{live: map[string]bool{}, hold: hold, started: make(chan struct{})}
}

func (f *memoryFake) add(session string) {
	f.mu.Lock()
	f.live[session] = true
	f.mu.Unlock()
}

func (f *memoryFake) has(session string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.live[session]
}

func (f *memoryFake) Erase(ctx context.Context, session string) (memory.EraseResult, error) {
	if f.hold != nil {
		f.once.Do(func() { close(f.started) })
		select {
		case <-f.hold:
		case <-ctx.Done():
			return memory.EraseResult{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	result := memory.EraseResult{Session: session}
	if !f.live[session] {
		result.AlreadyAbsent = true
		return result, nil
	}
	delete(f.live, session)
	result.Removed.Sessions = 1
	return result, nil
}
