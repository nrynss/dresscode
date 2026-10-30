package server_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/dresscode/internal/mail"
	"github.com/nrynss/dresscode/internal/server"
	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/sqlite"
)

func TestMountSignInRejectsABadConfig(t *testing.T) {
	t.Parallel()
	var none *server.Server
	if err := none.MountSignIn(t.Context(), server.SignInConfig{}); err == nil || !strings.Contains(err.Error(), "nil server") {
		t.Fatalf("nil server error %v", err)
	}

	srv, _ := openTest(t, nil)
	assertNotFound(t, request(t, srv.Handler(), http.MethodPost, "/api/session/code", nil, ""))

	good := server.SignInConfig{
		Mail:       &mail.Recorder{},
		CodeKey:    []byte("test-code-key"),
		SigningKey: []byte("test-session-signing-key-32b!!!!"),
	}
	if err := srv.MountSignIn(nil, good); err == nil || !strings.Contains(err.Error(), "nil context") {
		t.Fatalf("nil context error %v", err)
	}
	noMail := good
	noMail.Mail = nil
	if err := srv.MountSignIn(t.Context(), noMail); err == nil || !strings.Contains(err.Error(), "sender") {
		t.Fatalf("nil sender error %v", err)
	}
	noCode := good
	noCode.CodeKey = nil
	if err := srv.MountSignIn(t.Context(), noCode); err == nil || !strings.Contains(err.Error(), "code key") {
		t.Fatalf("empty code key error %v", err)
	}
	noSign := good
	noSign.SigningKey = nil
	if err := srv.MountSignIn(t.Context(), noSign); err == nil || !strings.Contains(err.Error(), "signing key") {
		t.Fatalf("empty signing key error %v", err)
	}
	assertNotFound(t, request(t, srv.Handler(), http.MethodPost, "/api/session/code", nil, ""))

	if err := srv.MountSignIn(t.Context(), good); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if err := srv.MountSignIn(t.Context(), good); err == nil || !strings.Contains(err.Error(), "already mounted") {
		t.Fatalf("second mount error %v", err)
	}
	refused := postJSON(t, srv.Handler(), "/api/session/code", `{}`, nil, "192.0.2.2:1000")
	assertError(t, refused, http.StatusBadRequest, "invalid_request")
	if len(good.Mail.(*mail.Recorder).Messages()) != 0 {
		t.Fatal("a refused start sent mail")
	}
}

func TestFreshSignInKeepsTheGuestLooks(t *testing.T) {
	t.Parallel()
	env := mountSignIn(t, nil)
	h := env.h
	db := openDB(t, env.dbPath)
	const remote = "192.0.2.20:4000"
	const address = "person@example.com"

	userID, cookie := bootGuest(t, h, remote)
	lookID := insertLook(t, db, userID)

	started := postCode(t, h, address, cookie, remote)
	code := latestCode(t, env.mail)
	assertNoCode(t, started, code)
	assertOK(t, started, http.StatusAccepted)
	if got := env.mail.Messages(); len(got) != 1 || got[0].Address != address {
		t.Fatalf("recorder accepted %d messages", len(got))
	}

	verified := postVerify(t, h, address, code, "", cookie, remote)
	assertNoCode(t, verified, code)
	assertOK(t, verified, http.StatusOK)
	rotated := oneSessionCookie(t, verified)
	if rotated == nil {
		t.Fatal("verify set no session cookie")
	}
	if sessionID(t, rotated.Value) == sessionID(t, cookie.Value) {
		t.Fatal("verify did not rotate the session")
	}

	ownerID, kind := readSession(t, h, rotated, remote)
	if ownerID != userID || kind != "owner" {
		t.Fatalf("session after sign-in is %s %s, want the guest id as owner", ownerID, kind)
	}
	if got := lookOwner(t, db, lookID); got != userID {
		t.Fatal("look moved off the guest")
	}
	if got := queryCount(t, db, "SELECT COUNT(*) FROM looks"); got != 1 {
		t.Fatalf("looks %d, want the one guest row", got)
	}
	if got := queryText(t, db, "SELECT user_id FROM identities WHERE provider = 'email' AND subject = ?", address); got != userID {
		t.Fatal("identity is not on the guest that upgraded")
	}
	assertQuiet(t, env, code)
	assertNotFound(t, request(t, h, http.MethodPost, "/api/session/google", rotated, remote))
}

func TestReturningAccountSwitchesAndLeavesGuestLooks(t *testing.T) {
	t.Parallel()
	env := mountSignIn(t, nil)
	h := env.h
	db := openDB(t, env.dbPath)
	const address = "person@example.com"
	const accountRemote = "192.0.2.30:4000"
	const guestRemote = "192.0.2.31:4000"

	accountID, accountCookie := bootGuest(t, h, accountRemote)
	accountCookie = signInFresh(t, env, h, address, accountCookie, accountRemote)
	ownerID, kind := readSession(t, h, accountCookie, accountRemote)
	if ownerID != accountID || kind != "owner" {
		t.Fatalf("account session %s %s, want %s owner", ownerID, kind, accountID)
	}
	accountLook := insertLook(t, db, accountID)

	guestID, guestCookie := bootGuest(t, h, guestRemote)
	if guestID == accountID {
		t.Fatal("returning guest reused the account id")
	}
	guestLook := insertLook(t, db, guestID)

	started := postCode(t, h, address, guestCookie, guestRemote)
	code := latestCode(t, env.mail)
	assertNoCode(t, started, code)
	assertOK(t, started, http.StatusAccepted)
	if started.Body.String() != "{\"ok\":true}\n" {
		t.Fatal("start answer for a known address differs from the fresh answer")
	}

	conflict := postVerify(t, h, address, code, "", guestCookie, guestRemote)
	assertNoCode(t, conflict, code)
	assertError(t, conflict, http.StatusConflict, "guest_data_conflict")
	if decodeError(t, conflict.Body.String()).Message != "this device holds guest data the account would leave behind" {
		t.Fatal("conflict message is not Keel's")
	}
	if oneSessionCookie(t, conflict) != nil {
		t.Fatal("conflict rotated the session")
	}
	assertLooksStay(t, h, db, guestCookie, guestRemote, guestID, guestLook, accountID, accountLook)

	merged := postVerify(t, h, address, code, "merge", guestCookie, guestRemote)
	assertNoCode(t, merged, code)
	assertError(t, merged, http.StatusBadRequest, "invalid_request")
	if decodeError(t, merged.Body.String()).Message != "this request carries no usable code" {
		t.Fatal("merge was not refused as an unknown choice")
	}
	assertLooksStay(t, h, db, guestCookie, guestRemote, guestID, guestLook, accountID, accountLook)

	switched := postVerify(t, h, address, code, "switch", guestCookie, guestRemote)
	assertNoCode(t, switched, code)
	assertOK(t, switched, http.StatusOK)
	rotated := oneSessionCookie(t, switched)
	if rotated == nil {
		t.Fatal("switch set no session cookie")
	}
	nowID, nowKind := readSession(t, h, rotated, guestRemote)
	if nowID != accountID || nowKind != "owner" {
		t.Fatalf("session after switch is %s %s, want the account", nowID, nowKind)
	}
	if got := lookOwner(t, db, guestLook); got != guestID {
		t.Fatal("switch copied or moved the guest look")
	}
	if got := lookOwner(t, db, accountLook); got != accountID {
		t.Fatal("switch overwrote the account look")
	}
	if got := queryCount(t, db, "SELECT COUNT(*) FROM looks"); got != 2 {
		t.Fatalf("looks %d, want the two original rows", got)
	}
	if got := queryCount(t, db, "SELECT COUNT(*) FROM looks WHERE user_id = ?", guestID); got != 1 {
		t.Fatal("guest look count changed")
	}
	if got := queryCount(t, db, "SELECT COUNT(*) FROM looks WHERE user_id = ?", accountID); got != 1 {
		t.Fatal("account look count changed")
	}
	if got := queryText(t, db, "SELECT kind FROM users WHERE id = ?", guestID); got != "guest" {
		t.Fatalf("guest kind %q after switch", got)
	}
	if got := queryCount(t, db, "SELECT COUNT(*) FROM identities WHERE subject = ?", address); got != 1 {
		t.Fatal("switch attached a second identity")
	}
	sent := env.mail.Messages()
	if len(sent) != 2 {
		t.Fatalf("recorder messages %d, want the account start and the guest start", len(sent))
	}
	codes := make([]string, len(sent))
	for i := range sent {
		codes[i] = sent[i].Code
	}
	assertQuiet(t, env, codes...)
	assertNotFound(t, request(t, h, http.MethodPost, "/api/session/google", rotated, guestRemote))
}

func TestReturningAccountWithNoLooksJoins(t *testing.T) {
	t.Parallel()
	env := mountSignIn(t, nil)
	h := env.h
	db := openDB(t, env.dbPath)
	const address = "person@example.com"
	const accountRemote = "192.0.2.60:4000"
	const guestRemote = "192.0.2.61:4000"

	accountID, accountCookie := bootGuest(t, h, accountRemote)
	accountCookie = signInFresh(t, env, h, address, accountCookie, accountRemote)
	if idAfter, kind := readSession(t, h, accountCookie, accountRemote); idAfter != accountID || kind != "owner" {
		t.Fatalf("account session %s %s, want %s owner", idAfter, kind, accountID)
	}

	guestID, guestCookie := bootGuest(t, h, guestRemote)
	started := postCode(t, h, address, guestCookie, guestRemote)
	code := latestCode(t, env.mail)
	assertNoCode(t, started, code)
	assertOK(t, started, http.StatusAccepted)

	verified := postVerify(t, h, address, code, "", guestCookie, guestRemote)
	assertNoCode(t, verified, code)
	assertOK(t, verified, http.StatusOK)
	rotated := oneSessionCookie(t, verified)
	if rotated == nil {
		t.Fatal("join set no session cookie")
	}
	nowID, nowKind := readSession(t, h, rotated, guestRemote)
	if nowID != accountID || nowKind != "owner" {
		t.Fatalf("session after join is %s %s, want the account", nowID, nowKind)
	}
	if got := queryCount(t, db, "SELECT COUNT(*) FROM looks"); got != 0 {
		t.Fatalf("looks %d, want none copied or created", got)
	}
	if got := queryText(t, db, "SELECT kind FROM users WHERE id = ?", guestID); got != "guest" {
		t.Fatalf("empty guest kind %q", got)
	}
	sent := env.mail.Messages()
	quiet := make([]string, len(sent))
	for i := range sent {
		quiet[i] = sent[i].Code
	}
	assertQuiet(t, env, quiet...)
}

func TestWrongCodeDoesNotSignIn(t *testing.T) {
	t.Parallel()
	env := mountSignIn(t, nil)
	h := env.h
	const remote = "192.0.2.40:4000"
	const address = "wrong@example.com"

	userID, cookie := bootGuest(t, h, remote)
	started := postCode(t, h, address, cookie, remote)
	code := latestCode(t, env.mail)
	assertNoCode(t, started, code)
	assertOK(t, started, http.StatusAccepted)

	wrong := otherCode(code)
	refused := postVerify(t, h, address, wrong, "", cookie, remote)
	assertNoCode(t, refused, code, wrong)
	assertError(t, refused, http.StatusUnauthorized, "invalid_code")
	if decodeError(t, refused.Body.String()).Message != "this code is expired, used, or never requested on this device" {
		t.Fatal("wrong-code message is not Keel's")
	}
	if oneSessionCookie(t, refused) != nil {
		t.Fatal("wrong code rotated the session")
	}
	gotID, kind := readSession(t, h, cookie, remote)
	if gotID != userID || kind != "guest" {
		t.Fatalf("session after wrong code is %s %s, want the guest", gotID, kind)
	}
	if len(env.mail.Messages()) != 1 {
		t.Fatal("wrong code sent another mail")
	}
	assertQuiet(t, env, code, wrong)
}

func TestExpiredCodeDoesNotSignIn(t *testing.T) {
	t.Parallel()
	clock := &movedClock{at: time.Unix(1_758_000_000, 0)}
	env := mountSignIn(t, clock)
	h := env.h
	const remote = "192.0.2.41:4000"
	const address = "expired@example.com"

	userID, cookie := bootGuest(t, h, remote)
	started := postCode(t, h, address, cookie, remote)
	code := latestCode(t, env.mail)
	assertNoCode(t, started, code)
	assertOK(t, started, http.StatusAccepted)

	clock.advance(10 * time.Minute)
	refused := postVerify(t, h, address, code, "", cookie, remote)
	assertNoCode(t, refused, code)
	assertError(t, refused, http.StatusUnauthorized, "invalid_code")
	if oneSessionCookie(t, refused) != nil {
		t.Fatal("expired code rotated the session")
	}
	gotID, kind := readSession(t, h, cookie, remote)
	if gotID != userID || kind != "guest" {
		t.Fatalf("session after expiry is %s %s, want the guest", gotID, kind)
	}
	assertQuiet(t, env, code)
}

func TestSendLimitRefusesBeforeSending(t *testing.T) {
	t.Parallel()
	clock := &movedClock{at: time.Unix(1_758_000_000, 0)}
	env := mountSignIn(t, clock)
	h := env.h
	const remote = "192.0.2.50:4000"
	const address = "capped@example.com"

	userID, cookie := bootGuest(t, h, remote)
	var codes []string
	for i := 1; i <= 5; i++ {
		got := postCode(t, h, address, cookie, remote)
		code := latestCode(t, env.mail)
		codes = append(codes, code)
		assertNoCode(t, got, codes...)
		assertOK(t, got, http.StatusAccepted)
	}
	if len(env.mail.Messages()) != 5 {
		t.Fatalf("recorder messages %d, want 5", len(env.mail.Messages()))
	}

	refused := postCode(t, h, address, cookie, remote)
	assertNoCode(t, refused, codes...)
	assertError(t, refused, http.StatusTooManyRequests, "send_limited")
	envErr := decodeError(t, refused.Body.String())
	if envErr.Message != "too many codes were requested, retry after the wait" {
		t.Fatalf("send limit message %q", envErr.Message)
	}
	if refused.Header().Get("Retry-After") != "86400" || envErr.RetryAfter != 86400 {
		t.Fatalf("Retry-After %q detail %d, want the 24h address window", refused.Header().Get("Retry-After"), envErr.RetryAfter)
	}
	if len(env.mail.Messages()) != 5 {
		t.Fatal("the refused start sent mail")
	}
	gotID, kind := readSession(t, h, cookie, remote)
	if gotID != userID || kind != "guest" {
		t.Fatalf("session after the limit is %s %s, want the guest", gotID, kind)
	}
	assertQuiet(t, env, codes...)
}

type signInEnv struct {
	h      http.Handler
	mail   *mail.Recorder
	dbPath string
	log    *bytes.Buffer
}

func mountSignIn(t *testing.T, clock *movedClock) signInEnv {
	t.Helper()
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	dir, app := writeApp(t)
	cfg := testConfig(dir, app)
	cfg.Log = log
	if clock != nil {
		cfg.Now = clock.now
	}
	srv, err := server.Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	rec := &mail.Recorder{}
	if err := srv.MountSignIn(t.Context(), server.SignInConfig{
		Mail:       rec,
		CodeKey:    []byte("test-code-key"),
		SigningKey: append([]byte(nil), cfg.SigningKey...),
		Now:        cfg.Now,
		Log:        log,
	}); err != nil {
		t.Fatalf("mount sign-in: %v", err)
	}
	return signInEnv{h: srv.Handler(), mail: rec, dbPath: cfg.DBPath, log: &buf}
}

func signInFresh(t *testing.T, env signInEnv, h http.Handler, address string, cookie *http.Cookie, remote string) *http.Cookie {
	t.Helper()
	started := postCode(t, h, address, cookie, remote)
	code := latestCode(t, env.mail)
	assertNoCode(t, started, code)
	assertOK(t, started, http.StatusAccepted)
	verified := postVerify(t, h, address, code, "", cookie, remote)
	assertNoCode(t, verified, code)
	assertOK(t, verified, http.StatusOK)
	rotated := oneSessionCookie(t, verified)
	if rotated == nil {
		t.Fatal("verify set no session cookie")
	}
	return rotated
}

func bootGuest(t *testing.T, h http.Handler, remote string) (string, *http.Cookie) {
	t.Helper()
	rec := request(t, h, http.MethodGet, "/api/session", nil, remote)
	if rec.Code != http.StatusOK {
		t.Fatalf("session status %d", rec.Code)
	}
	userID, kind := decodeSession(t, rec.Body.String())
	if kind != "guest" || !id.Valid(userID) {
		t.Fatalf("boot kind %s", kind)
	}
	cookie := oneSessionCookie(t, rec)
	if cookie == nil {
		t.Fatal("boot set no cookie")
	}
	return userID, cookie
}

func readSession(t *testing.T, h http.Handler, cookie *http.Cookie, remote string) (string, string) {
	t.Helper()
	rec := request(t, h, http.MethodGet, "/api/session", cookie, remote)
	if rec.Code != http.StatusOK {
		t.Fatalf("session status %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "@") {
		t.Fatal("session body carries an address")
	}
	return decodeSession(t, rec.Body.String())
}

func postCode(t *testing.T, h http.Handler, address string, cookie *http.Cookie, remote string) *httptest.ResponseRecorder {
	t.Helper()
	return postJSON(t, h, "/api/session/code", `{"address":`+quoteJSON(t, address)+`}`, cookie, remote)
}

func postVerify(t *testing.T, h http.Handler, address, code, choice string, cookie *http.Cookie, remote string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"address":` + quoteJSON(t, address) + `,"code":` + quoteJSON(t, code) + `,"choice":` + quoteJSON(t, choice) + `}`
	return postJSON(t, h, "/api/session/code/verify", body, cookie, remote)
}

func postJSON(t *testing.T, h http.Handler, path, body string, cookie *http.Cookie, remote string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if remote == "" {
		remote = "192.0.2.1:1234"
	}
	req.RemoteAddr = remote
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func insertLook(t *testing.T, db *sqlite.DB, userID string) string {
	t.Helper()
	lookID, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer().ExecContext(t.Context(),
		`INSERT INTO looks (id, user_id, selfie_id, photo_hash, occasion, created_at)
		 VALUES (?, ?, ?, ?, 'family-function', 1700000000)`,
		lookID, userID, lookID, lookID); err != nil {
		t.Fatalf("insert look: %v", err)
	}
	return lookID
}

func lookOwner(t *testing.T, db *sqlite.DB, lookID string) string {
	t.Helper()
	return queryText(t, db, "SELECT user_id FROM looks WHERE id = ?", lookID)
}

func assertLooksStay(t *testing.T, h http.Handler, db *sqlite.DB, cookie *http.Cookie, remote, guestID, guestLook, accountID, accountLook string) {
	t.Helper()
	gotID, kind := readSession(t, h, cookie, remote)
	if gotID != guestID || kind != "guest" {
		t.Fatalf("session is %s %s, want the guest", gotID, kind)
	}
	if lookOwner(t, db, guestLook) != guestID || lookOwner(t, db, accountLook) != accountID {
		t.Fatal("look rows changed")
	}
	if got := queryCount(t, db, "SELECT COUNT(*) FROM looks"); got != 2 {
		t.Fatalf("looks %d, want the two original rows", got)
	}
}

func latestCode(t *testing.T, rec *mail.Recorder) string {
	t.Helper()
	msgs := rec.Messages()
	if len(msgs) == 0 || !sixDigitCode(msgs[len(msgs)-1].Code) {
		t.Fatal("recorder has no six-digit code")
	}
	return msgs[len(msgs)-1].Code
}

func sixDigitCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func otherCode(code string) string {
	b := []byte(code)
	if b[0] == '0' {
		b[0] = '1'
	} else {
		b[0] = '0'
	}
	return string(b)
}

func quoteJSON(t *testing.T, value string) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func assertOK(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status || rec.Body.String() != "{\"ok\":true}\n" {
		t.Fatalf("status %d, want %d with ok", rec.Code, status)
	}
	if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("ok response is missing json or no-store")
	}
}

func assertError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	env := decodeError(t, rec.Body.String())
	if rec.Code != status || env.Code != code {
		t.Fatalf("status %d code %s, want %d %s", rec.Code, env.Code, status, code)
	}
	if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("error response is missing json or no-store")
	}
}

func assertNoCode(t *testing.T, rec *httptest.ResponseRecorder, codes ...string) {
	t.Helper()
	body := rec.Body.String()
	for _, code := range codes {
		if code != "" && strings.Contains(body, code) {
			t.Fatal("response carries a sign-in code")
		}
	}
}

func assertQuiet(t *testing.T, env signInEnv, codes ...string) {
	t.Helper()
	logged := env.log.String()
	if strings.Contains(logged, "test-code-key") || strings.Contains(logged, "test-session-signing-key") {
		t.Fatal("log carries a key")
	}
	for _, code := range codes {
		if code != "" && strings.Contains(logged, code) {
			t.Fatal("log carries a sign-in code")
		}
	}
}
