package privacy_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/memory"
	"github.com/nrynss/dresscode/internal/privacy"
	"github.com/nrynss/dresscode/internal/store"
	"github.com/nrynss/keel/erase"
	"github.com/nrynss/keel/identity"
	identitysql "github.com/nrynss/keel/identity/sqlitestore"
	"github.com/nrynss/keel/job"
	jobsql "github.com/nrynss/keel/job/sqlitestore"
	"github.com/nrynss/keel/mediastore"
	"github.com/nrynss/keel/sqlite"
	"github.com/nrynss/keel/stream"
)

func TestRowTablesMatchTheStoreAndClear(t *testing.T) {
	t.Parallel()
	if !slices.Equal(privacy.RowTables(), store.UserTables()) {
		t.Fatalf("RowTables() = %v, want %v", privacy.RowTables(), store.UserTables())
	}
	mutated := privacy.RowTables()
	mutated[0] = "mutated"
	if privacy.RowTables()[0] == "mutated" {
		t.Fatal("RowTables returned its own slice")
	}

	db := openDB(t)
	if err := store.Migrate(t.Context(), db); err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, err := identitysql.Open(t.Context(), identitysql.Config{DB: db}); err != nil {
		t.Fatalf("identity: %v", err)
	}
	schema := userDataTables(t, db)
	if !slices.Equal(schema, store.UserTables()) {
		t.Fatalf("schema user tables %v, erase tables %v", schema, store.UserTables())
	}
	for _, name := range []string{"users", "sessions", "identities", "sign_in_codes"} {
		if slices.Contains(privacy.RowTables(), name) {
			t.Fatalf("RowTables includes identity table %s", name)
		}
	}

	eng := newEngine(t, db, newBlobs(), newMemory(nil))
	targets, err := eng.Source(t.Context(), "user-a")
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	names := make([]string, len(targets))
	var rows erase.Target
	for i, target := range targets {
		names[i] = target.Name()
		if target.Name() == privacy.TargetRows {
			rows = target
		}
	}
	wantNames := []string{privacy.TargetFiles, privacy.TargetRows, privacy.TargetMemory}
	if !slices.Equal(names, wantNames) {
		t.Fatalf("targets %v, want %v", names, wantNames)
	}
	lister, ok := rows.(interface{ Tables() []string })
	if !ok {
		t.Fatal("rows target does not list its tables")
	}
	if !slices.Equal(lister.Tables(), store.UserTables()) {
		t.Fatalf("rows target tables %v, want %v", lister.Tables(), store.UserTables())
	}

	seedUser(t, db, "user-a", 1)
	seedUser(t, db, "user-b", 0)
	seedIdentity(t, db, "user-a")
	seedIdentity(t, db, "user-b")
	if err := rows.Delete(t.Context()); err != nil {
		t.Fatalf("delete rows: %v", err)
	}
	assertUserGone(t, db, "user-a")
	assertUserPresent(t, db, "user-b")
	if err := rows.Delete(t.Context()); err != nil {
		t.Fatalf("second delete: %v", err)
	}
	assertUserGone(t, db, "user-a")
	assertUserPresent(t, db, "user-b")
}

func TestAccountDeletionRestartsMidway(t *testing.T) {
	t.Parallel()
	fx := openAccount(t, time.Now)
	owner := fx.signIn("person@example.com")
	other := "other-person"
	seedUser(t, fx.db, owner.userID, 1)
	seedUser(t, fx.db, other, 1)
	fx.blobs.add(owner.userID+"-selfie", owner.userID)
	fx.blobs.add(owner.userID+"-media", owner.userID)
	fx.blobs.add(other+"-selfie", other)
	fx.mem.add(mustSession(t, owner.userID))
	fx.mem.add(mustSession(t, other))

	release := fx.mem.gate()
	t.Cleanup(release)
	code := fx.code(owner.sessionID, "person@example.com")
	jobID, err := fx.svc.Delete(t.Context(), owner.sessionID, owner.userID, "Person@Example.com ", code)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	select {
	case <-fx.mem.started:
	case <-time.After(10 * time.Second):
		t.Fatal("memory delete never started")
	}
	waitUntil(t, func() bool {
		return userDataGone(t, fx.db, owner.userID) &&
			identityGone(t, fx.db, owner.userID) &&
			fx.blobs.count(owner.userID) == 0 &&
			fx.mem.has(mustSession(t, owner.userID))
	})
	if !fx.mem.has(mustSession(t, other)) || fx.blobs.count(other) != 1 || !userDataPresent(t, fx.db, other) {
		t.Fatal("mid-deletion touched the other account")
	}
	assertNoRowsDone(t, fx.db, jobID)

	old := fx.runner
	next := fx.reopen()
	if err := old.Cancel(jobID); err != nil && !errors.Is(err, job.ErrUnknownJob) {
		t.Fatalf("cancel: %v", err)
	}
	release()
	last := waitTerminal(t, next, jobID, job.StatusDone)
	if last.Progress.Stage != "delete" || last.Progress.Current != nil || last.Progress.Total != nil {
		t.Fatalf("final progress stage %q current %v total %v", last.Progress.Stage, last.Progress.Current, last.Progress.Total)
	}
	var snap struct {
		Version  int    `json:"version"`
		User     string `json:"user"`
		RowsDone bool   `json:"rows_done"`
	}
	if err := json.Unmarshal(last.Progress.Detail, &snap); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Version != 1 || snap.User != owner.userID || !snap.RowsDone {
		t.Fatalf("snapshot %+v", snap)
	}
	assertUserGone(t, fx.db, owner.userID)
	if fx.blobs.count(owner.userID) != 0 || fx.mem.has(mustSession(t, owner.userID)) {
		t.Fatal("account blobs or memory survived")
	}
	if !userDataPresent(t, fx.db, other) || fx.blobs.count(other) != 1 || !fx.mem.has(mustSession(t, other)) {
		t.Fatal("other account was erased")
	}
	if !fx.erasureConfirmed(owner.userID) {
		t.Fatal("no erasure confirmed files, rows, and memory")
	}
	if _, err := fx.svc.Delete(t.Context(), owner.sessionID, owner.userID, "person@example.com", code); err == nil {
		t.Fatal("second delete started")
	}
}

func TestDeletionRefusesAStaleCodeAndAStranger(t *testing.T) {
	t.Parallel()
	clock := &manualClock{at: time.Unix(1_700_000_000, 0)}
	fx := openAccount(t, clock.now)
	owner := fx.signIn("late@example.com")
	seedUser(t, fx.db, owner.userID, 1)
	code := fx.code(owner.sessionID, "late@example.com")
	clock.advance(10 * time.Minute)
	if _, err := fx.svc.Delete(t.Context(), owner.sessionID, owner.userID, "late@example.com", code); !errors.Is(err, identity.ErrInvalidCode) {
		t.Fatalf("stale delete: %v", err)
	}
	if !userDataPresent(t, fx.db, owner.userID) || !identityPresent(t, fx.db, owner.userID) {
		t.Fatal("a stale code deleted rows")
	}

	stranger := fx.mint()
	otherCode := fx.code(stranger.sessionID, "late@example.com")
	if _, err := fx.svc.Delete(t.Context(), stranger.sessionID, stranger.userID, "late@example.com", otherCode); !errors.Is(err, identity.ErrNotOwner) {
		t.Fatalf("stranger delete: %v", err)
	}
	if !userDataPresent(t, fx.db, owner.userID) {
		t.Fatal("a stranger's delete removed the account")
	}
}

func TestPhotoLookAndHistoryDeletion(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	if err := store.Migrate(t.Context(), db); err != nil {
		t.Fatalf("store: %v", err)
	}
	blobs := newBlobs()
	mem := newMemory(nil)
	eng := newEngine(t, db, blobs, mem)
	runner := openRunner(t, db, map[string]job.Kind{privacy.KindName: eng.Kind()})

	const (
		photoUser = "user-photo"
		lookUser  = "user-look"
		histUser  = "user-hist"
		other     = "user-b"
	)
	seedUser(t, db, photoUser, 1)
	seedUser(t, db, lookUser, 1)
	seedUser(t, db, histUser, 1)
	seedUser(t, db, other, 0)
	for _, user := range []string{photoUser, lookUser, other} {
		blobs.add(user+"-selfie", user)
		blobs.add(user+"-media", user)
	}
	mem.add(mustSession(t, histUser))
	mem.add(mustSession(t, other))

	if _, err := eng.StartPhoto(t.Context(), runner, photoUser, other+"-selfie"); !errors.Is(err, privacy.ErrAbsent) {
		t.Fatalf("other photo: %v", err)
	}
	photoJob, err := eng.StartPhoto(t.Context(), runner, photoUser, photoUser+"-selfie")
	if err != nil {
		t.Fatalf("photo: %v", err)
	}
	photo := waitJob(t, runner, photoJob, job.StatusDone)
	assertErase(t, photo, []string{privacy.TargetFiles, privacy.TargetLooks})
	if count(t, db, store.TableLooks, photoUser) != 0 || count(t, db, store.TableAnalysisCache, photoUser) != 0 {
		t.Fatal("photo deletion left the look or the cache")
	}
	if blobs.count(photoUser) != 0 {
		t.Fatal("photo deletion left a blob")
	}
	if count(t, db, store.TableProducts, photoUser) != 1 || count(t, db, store.TableLooks, other) != 1 || blobs.count(other) != 2 {
		t.Fatal("photo deletion removed something it should have kept")
	}
	if _, err := eng.StartPhoto(t.Context(), runner, photoUser, photoUser+"-selfie"); !errors.Is(err, privacy.ErrAbsent) {
		t.Fatalf("repeat photo: %v", err)
	}

	lookJob, err := eng.StartLook(t.Context(), runner, lookUser, lookUser+"-look")
	if err != nil {
		t.Fatalf("look: %v", err)
	}
	look := waitJob(t, runner, lookJob, job.StatusDone)
	assertErase(t, look, []string{privacy.TargetLook, privacy.TargetFiles})
	if count(t, db, store.TableLooks, lookUser) != 0 || blobs.has(lookUser+"-media") {
		t.Fatal("look deletion left the look or the render")
	}
	if !blobs.has(lookUser+"-selfie") || count(t, db, store.TableAnalysisCache, lookUser) != 1 {
		t.Fatal("look deletion removed the selfie or its cache")
	}
	if _, err := eng.StartLook(t.Context(), runner, lookUser, lookUser+"-look"); !errors.Is(err, privacy.ErrAbsent) {
		t.Fatalf("repeat look: %v", err)
	}

	historyJob, err := eng.StartHistory(t.Context(), runner, histUser)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	history := waitJob(t, runner, historyJob, job.StatusDone)
	assertErase(t, history, []string{privacy.TargetMemory})
	if count(t, db, store.TableHistorySwitch, histUser) != 0 || mem.has(mustSession(t, histUser)) {
		t.Fatal("history deletion left the switch or the session")
	}
	if count(t, db, store.TableLooks, histUser) != 1 || count(t, db, store.TableLooks, other) != 1 || !mem.has(mustSession(t, other)) {
		t.Fatal("history deletion touched a look or the other account")
	}
	if _, err := eng.StartHistory(t.Context(), runner, histUser); !errors.Is(err, privacy.ErrAbsent) {
		t.Fatalf("repeat history: %v", err)
	}
	if _, err := eng.StartHistory(t.Context(), runner, other); !errors.Is(err, privacy.ErrAbsent) {
		t.Fatalf("history that is off: %v", err)
	}
}

func TestPhotoDeletionResumes(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	if err := store.Migrate(t.Context(), db); err != nil {
		t.Fatalf("store: %v", err)
	}
	blobs := newBlobs()
	blobs.hold = make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(blobs.hold) }) }
	t.Cleanup(release)
	eng := newEngine(t, db, blobs, newMemory(nil))
	kinds := map[string]job.Kind{privacy.KindName: eng.Kind()}
	runner := openRunner(t, db, kinds)
	const user = "user-a"
	seedUser(t, db, user, 1)
	blobs.add(user+"-selfie", user)
	blobs.add(user+"-media", user)

	jobID, err := eng.StartPhoto(t.Context(), runner, user, user+"-selfie")
	if err != nil {
		t.Fatalf("photo: %v", err)
	}
	select {
	case <-blobs.started:
	case <-time.After(10 * time.Second):
		t.Fatal("blob delete never started")
	}
	next := reopenRunner(t, db, kinds)
	if err := runner.Cancel(jobID); err != nil && !errors.Is(err, job.ErrUnknownJob) {
		t.Fatalf("cancel: %v", err)
	}
	release()
	if got := waitJob(t, next, jobID, job.StatusDone); got.Status != job.StatusDone {
		t.Fatalf("resumed photo ended %s", got.Status)
	}
	if count(t, db, store.TableLooks, user) != 0 || blobs.count(user) != 0 {
		t.Fatal("resumed photo deletion left rows or blobs")
	}
}

type accountFix struct {
	t       *testing.T
	db      *sqlite.DB
	svc     *identity.Service
	runner  *job.Runner
	eraser  *erase.Eraser
	mail    *mailbox
	blobs   *fakeBlobs
	mem     *fakeMemory
	kinds   map[string]job.Kind
	now     func() time.Time
	signing []byte
	codeKey []byte
}

type signedIn struct {
	userID    string
	sessionID string
}

func openAccount(t *testing.T, now func() time.Time) *accountFix {
	t.Helper()
	db := openDB(t)
	if err := store.Migrate(t.Context(), db); err != nil {
		t.Fatalf("store: %v", err)
	}
	idStore, err := identitysql.Open(t.Context(), identitysql.Config{DB: db})
	if err != nil {
		t.Fatalf("identity store: %v", err)
	}
	blobs := newBlobs()
	mem := newMemory(make(chan struct{}))
	eng := newEngine(t, db, blobs, mem)
	eraser, err := erase.New(eng.Source, erase.Config{Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatalf("eraser: %v", err)
	}
	mail := &mailbox{}
	signing := []byte("privacy-test-signing-key")
	codeKey := []byte("privacy-test-code-key")
	svc, err := identity.New(identity.Config{
		Store:      idStore,
		SigningKey: signing,
		CodeKey:    codeKey,
		Mail:       mail,
		Now:        now,
		Log:        slog.New(slog.DiscardHandler),
		Eraser:     eraser,
		Targets:    eng.Source,
	})
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	fx := &accountFix{
		t: t, db: db, svc: svc, eraser: eraser, mail: mail, blobs: blobs, mem: mem,
		now: now, signing: signing, codeKey: codeKey,
	}
	fx.kinds = svc.Kinds()
	fx.runner = openRunner(t, db, fx.kinds)
	if err := svc.BindRunner(fx.runner); err != nil {
		t.Fatalf("bind: %v", err)
	}
	return fx
}

func (fx *accountFix) reopen() *job.Runner {
	fx.t.Helper()
	fx.runner = reopenRunner(fx.t, fx.db, fx.kinds)
	if err := fx.svc.BindRunner(fx.runner); err != nil {
		fx.t.Fatalf("rebind: %v", err)
	}
	return fx.runner
}

func (fx *accountFix) mint() signedIn {
	fx.t.Helper()
	var user identity.User
	var session identity.Session
	handler := fx.svc.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var ok bool
		user, ok = identity.UserFromContext(r.Context())
		if !ok {
			return
		}
		session, _ = identity.SessionFromContext(r.Context())
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if user.ID == "" || session.ID == "" {
		fx.t.Fatal("mint produced no session")
	}
	return signedIn{userID: user.ID, sessionID: session.ID}
}

func (fx *accountFix) code(sessionID, address string) string {
	fx.t.Helper()
	if err := fx.svc.RequestSignInCode(fx.t.Context(), sessionID, address); err != nil {
		fx.t.Fatalf("request code: %v", err)
	}
	code := fx.mail.last()
	if code == "" {
		fx.t.Fatal("mail kept no code")
	}
	return code
}

func (fx *accountFix) signIn(address string) signedIn {
	fx.t.Helper()
	guest := fx.mint()
	code := fx.code(guest.sessionID, address)
	outcome, err := fx.svc.VerifySignInCode(fx.t.Context(), guest.sessionID, guest.userID, address, code, "")
	if err != nil {
		fx.t.Fatalf("verify: %v", err)
	}
	return signedIn{userID: outcome.UserID, sessionID: outcome.SessionID}
}

func (fx *accountFix) erasureConfirmed(userID string) bool {
	fx.t.Helper()
	rows, err := fx.db.Reader().QueryContext(fx.t.Context(), `SELECT id FROM jobs WHERE kind = ?`, erase.KindName)
	if err != nil {
		fx.t.Fatalf("erase jobs: %v", err)
	}
	defer rows.Close()
	want := []string{privacy.TargetFiles, privacy.TargetMemory, privacy.TargetRows}
	slices.Sort(want)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			fx.t.Fatalf("scan: %v", err)
		}
		rep, err := fx.eraser.Inspect(fx.t.Context(), fx.runner, id)
		if err != nil || rep.Ref != userID || !rep.Complete() {
			continue
		}
		got := append([]string(nil), rep.Confirmed...)
		slices.Sort(got)
		if slices.Equal(got, want) {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		fx.t.Fatalf("erase jobs: %v", err)
	}
	return false
}

func openRunner(t *testing.T, db *sqlite.DB, kinds map[string]job.Kind) *job.Runner {
	t.Helper()
	store, err := jobsql.Open(t.Context(), jobsql.Config{DB: db})
	if err != nil {
		t.Fatalf("job store: %v", err)
	}
	runner, err := job.Open(t.Context(), job.Config{
		Broker: stream.New(stream.Config{}),
		Store:  store,
		Kinds:  kinds,
		Log:    slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("runner: %v", err)
	}
	return runner
}

func reopenRunner(t *testing.T, db *sqlite.DB, kinds map[string]job.Kind) *job.Runner {
	t.Helper()
	return openRunner(t, db, kinds)
}

func newEngine(t *testing.T, db *sqlite.DB, blobs *fakeBlobs, mem *fakeMemory) *privacy.Engine {
	t.Helper()
	eng, err := privacy.New(privacy.Config{
		DB:     db,
		Blobs:  blobs,
		Memory: mem,
		Log:    slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	return eng
}

func waitJob(t *testing.T, runner *job.Runner, id string, want job.Status) job.Record {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		attempts, err := runner.Attempts(t.Context(), id)
		if err != nil {
			t.Fatalf("attempts: %v", err)
		}
		if len(attempts) > 0 {
			last := attempts[len(attempts)-1]
			switch last.Status {
			case want:
				return last
			case job.StatusError, job.StatusCancelled:
				t.Fatalf("job %s ended %s: %v", id, last.Status, last.Err)
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s never reached %s", id, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitTerminal(t *testing.T, runner *job.Runner, id string, want job.Status) job.Record {
	t.Helper()
	return waitJob(t, runner, id, want)
}

func assertErase(t *testing.T, rec job.Record, targets []string) {
	t.Helper()
	if rec.Progress.Stage != "erase" || rec.Progress.Current == nil || rec.Progress.Total == nil {
		t.Fatalf("progress %+v", rec.Progress)
	}
	if *rec.Progress.Current != *rec.Progress.Total || *rec.Progress.Total != int64(len(targets)) {
		t.Fatalf("current %d total %d, want %d", *rec.Progress.Current, *rec.Progress.Total, len(targets))
	}
	var detail struct {
		Targets   []string `json:"targets"`
		Confirmed []string `json:"confirmed"`
	}
	if err := json.Unmarshal(rec.Progress.Detail, &detail); err != nil {
		t.Fatalf("detail: %v", err)
	}
	if !slices.Equal(detail.Targets, targets) || !slices.Equal(detail.Confirmed, targets) {
		t.Fatalf("detail targets %v confirmed %v, want %v", detail.Targets, detail.Confirmed, targets)
	}
}

func assertNoRowsDone(t *testing.T, db *sqlite.DB, jobID string) {
	t.Helper()
	var raw string
	if err := db.Reader().QueryRowContext(t.Context(), `SELECT progress FROM jobs WHERE id = ?`, jobID).Scan(&raw); err != nil {
		t.Fatalf("progress: %v", err)
	}
	var progress struct {
		Stage   string          `json:"stage"`
		Current *int64          `json:"current"`
		Total   *int64          `json:"total"`
		Detail  json.RawMessage `json:"detail"`
	}
	if err := json.Unmarshal([]byte(raw), &progress); err != nil {
		t.Fatalf("decode progress: %v", err)
	}
	if progress.Stage != "delete" || progress.Current != nil || progress.Total != nil {
		t.Fatalf("mid progress %+v", progress)
	}
	var snap struct {
		RowsDone bool `json:"rows_done"`
	}
	if err := json.Unmarshal(progress.Detail, &snap); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if snap.RowsDone {
		t.Fatal("rows_done was true before every target confirmed")
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the mid-deletion state")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func userDataGone(t *testing.T, db *sqlite.DB, user string) bool {
	t.Helper()
	for _, table := range store.UserTables() {
		if count(t, db, table, user) != 0 {
			return false
		}
	}
	return true
}

func userDataPresent(t *testing.T, db *sqlite.DB, user string) bool {
	t.Helper()
	for _, table := range store.UserTables() {
		if count(t, db, table, user) != 1 {
			return false
		}
	}
	return true
}

func identityGone(t *testing.T, db *sqlite.DB, user string) bool {
	t.Helper()
	users, sessions, identities, codes := identityCounts(t, db, user)
	return users == 0 && sessions == 0 && identities == 0 && codes == 0
}

func identityPresent(t *testing.T, db *sqlite.DB, user string) bool {
	t.Helper()
	users, sessions, identities, codes := identityCounts(t, db, user)
	return users == 1 && sessions > 0 && identities == 1 && codes > 0
}

func assertUserGone(t *testing.T, db *sqlite.DB, user string) {
	t.Helper()
	if !userDataGone(t, db, user) || !identityGone(t, db, user) {
		t.Fatalf("%s still has rows", user)
	}
}

func assertUserPresent(t *testing.T, db *sqlite.DB, user string) {
	t.Helper()
	if !userDataPresent(t, db, user) {
		t.Fatalf("%s lost user-data rows", user)
	}
	users, sessions, identities, codes := identityCounts(t, db, user)
	if users != 1 || sessions != 1 || identities != 1 || codes != 1 {
		t.Fatalf("%s identity rows users %d sessions %d identities %d codes %d", user, users, sessions, identities, codes)
	}
}

func identityCounts(t *testing.T, db *sqlite.DB, user string) (users, sessions, identities, codes int) {
	t.Helper()
	queries := []struct {
		query string
		into  *int
	}{
		{"SELECT COUNT(*) FROM users WHERE id = ?", &users},
		{"SELECT COUNT(*) FROM sessions WHERE user_id = ?", &sessions},
		{"SELECT COUNT(*) FROM identities WHERE user_id = ?", &identities},
		{`SELECT COUNT(*) FROM sign_in_codes WHERE requesting_session IN (SELECT id FROM sessions WHERE user_id = ?)`, &codes},
	}
	for _, query := range queries {
		if err := db.Reader().QueryRowContext(t.Context(), query.query, user).Scan(query.into); err != nil {
			t.Fatalf("count identity: %v", err)
		}
	}
	return users, sessions, identities, codes
}

func count(t *testing.T, db *sqlite.DB, table, user string) int {
	t.Helper()
	var n int
	query := `SELECT COUNT(*) FROM "` + table + `" WHERE "` + store.UserIDColumn + `" = ?`
	if err := db.Reader().QueryRowContext(t.Context(), query, user).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func userDataTables(t *testing.T, db *sqlite.DB) []string {
	t.Helper()
	rows, err := db.Reader().QueryContext(t.Context(),
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("tables: %v", err)
	}
	defer rows.Close()
	skip := map[string]bool{"sessions": true, "identities": true}
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("tables: %v", err)
		}
		if skip[name] || strings.HasSuffix(name, "_schema_migrations") {
			continue
		}
		var n int
		err := db.Reader().QueryRowContext(t.Context(),
			`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, name, store.UserIDColumn).Scan(&n)
		if err != nil {
			t.Fatalf("columns %s: %v", name, err)
		}
		if n == 1 {
			got = append(got, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("tables: %v", err)
	}
	return got
}

func seedIdentity(t *testing.T, db *sqlite.DB, user string) {
	t.Helper()
	ctx := t.Context()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Writer().ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed identity %s: %v", user, err)
		}
	}
	exec(`INSERT INTO users (id, kind, created_at, last_seen_at) VALUES (?, 'owner', 1, 1)`, user)
	exec(`INSERT INTO sessions (id, user_id, created_at, revoked) VALUES (?, ?, 1, 0)`, user+"-session", user)
	exec(`INSERT INTO identities (id, user_id, provider, subject, created_at) VALUES (?, ?, 'email', ?, 1)`,
		user+"-identity", user, user+"@example.com")
	exec(`INSERT INTO sign_in_codes (id, address_hash, code_hash, requesting_session, expires_at, attempts, used_at, created_at)
		VALUES (?, 'hash', 'hash', ?, 10, 0, 0, 1)`, user+"-code", user+"-session")
}

func seedUser(t *testing.T, db *sqlite.DB, user string, history int) {
	t.Helper()
	ctx := t.Context()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Writer().ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %s: %v", user, err)
		}
	}
	look := user + "-look"
	render := user + "-render"
	product := user + "-product"
	exec(`INSERT INTO looks
		(id, user_id, selfie_id, photo_hash, occasion, fix, runner_up, verdict, created_at)
		VALUES (?, ?, ?, ?, 'family-function', 'family-gold-jhumkas', 'family-red-lip', ?, 1)`,
		look, user, user+"-selfie", user+"-hash", string(domain.VerdictKept))
	exec(`INSERT INTO look_flags (look_id, user_id, position, criterion_id) VALUES (?, ?, 0, 'earring')`, look, user)
	exec(`INSERT INTO renders (id, look_id, user_id, media_id, fix_id, position, discarded)
		VALUES (?, ?, ?, ?, 'family-gold-jhumkas', 0, 0)`, render, look, user, user+"-media")
	exec(`INSERT INTO render_checks (render_id, look_id, user_id, change_made, same_person, criterion_met)
		VALUES (?, ?, ?, 1, 1, 1)`, render, look, user)
	exec(`INSERT INTO render_check_attributes (render_id, user_id, attribute_id, value)
		VALUES (?, ?, 'jewellery', 'jhumkas')`, render, user)
	exec(`INSERT INTO products (id, user_id, category, name, fidelity, origin)
		VALUES (?, ?, 'earrings', 'Gold jhumkas', ?, ?)`,
		product, user, string(domain.ExactProduct), string(domain.OriginUser))
	exec(`INSERT INTO look_products
		(look_id, user_id, position, product_id, category, name, fidelity, origin)
		VALUES (?, ?, 0, 'gold-jhumkas', 'earrings', 'Gold jhumkas', ?, ?)`,
		look, user, string(domain.ExactProduct), string(domain.OriginCatalog))
	exec(`INSERT INTO opt_ins (user_id, occasion, fix_id) VALUES (?, 'family-function', 'family-gold-jhumkas')`, user)
	exec(`INSERT INTO history_switch (user_id, enabled) VALUES (?, ?)`, user, history)
	exec(`INSERT INTO analysis_cache (user_id, photo_hash, task, face_shape)
		VALUES (?, ?, 'face-attr-analysis', 'oval')`, user, user+"-hash")
	exec(`INSERT INTO look_snapshots (root_id, user_id, snapshot) VALUES (?, ?, '{}')`, look, user)
}

func mustSession(t *testing.T, account string) string {
	t.Helper()
	id, err := memory.Session(account)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	return id
}

func openDB(t *testing.T) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(t.Context(), sqlite.Config{
		Path:   filepath.Join(t.TempDir(), "dresscode.db"),
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type manualClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *manualClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *manualClock) advance(d time.Duration) {
	c.mu.Lock()
	c.at = c.at.Add(d)
	c.mu.Unlock()
}

type mailbox struct {
	mu   sync.Mutex
	code string
}

func (m *mailbox) Send(_ context.Context, _, code string) error {
	m.mu.Lock()
	m.code = code
	m.mu.Unlock()
	return nil
}

func (m *mailbox) last() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.code
}

type fakeBlobs struct {
	mu      sync.Mutex
	items   map[string]mediastore.Blob
	hold    chan struct{}
	started chan struct{}
	once    sync.Once
}

func newBlobs() *fakeBlobs {
	return &fakeBlobs{items: map[string]mediastore.Blob{}, started: make(chan struct{})}
}

func (f *fakeBlobs) add(id, owner string) {
	f.mu.Lock()
	f.items[id] = mediastore.Blob{ID: id, Owner: owner}
	f.mu.Unlock()
}

func (f *fakeBlobs) has(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.items[id]
	return ok
}

func (f *fakeBlobs) count(owner string) int {
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

func (f *fakeBlobs) Blobs(ctx context.Context) ([]mediastore.Blob, error) {
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

func (f *fakeBlobs) Delete(ctx context.Context, id string) error {
	if f.hold != nil {
		block := false
		f.once.Do(func() {
			block = true
			close(f.started)
		})
		if block {
			select {
			case <-f.hold:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
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

type fakeMemory struct {
	mu      sync.Mutex
	live    map[string]bool
	hold    chan struct{}
	started chan struct{}
	once    sync.Once
}

func newMemory(hold chan struct{}) *fakeMemory {
	return &fakeMemory{live: map[string]bool{}, hold: hold, started: make(chan struct{})}
}

func (f *fakeMemory) add(session string) {
	f.mu.Lock()
	f.live[session] = true
	f.mu.Unlock()
}

func (f *fakeMemory) has(session string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.live[session]
}

func (f *fakeMemory) gate() func() {
	var once sync.Once
	return func() { once.Do(func() { close(f.hold) }) }
}

func (f *fakeMemory) Erase(ctx context.Context, session string) (memory.EraseResult, error) {
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
