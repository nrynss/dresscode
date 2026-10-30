package server_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/dresscode/internal/server"
	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/sqlite"
)

func TestNewVisitorGetsAGuest(t *testing.T) {
	t.Parallel()
	srv, dbPath := openTest(t, nil)
	h := srv.Handler()

	rec := request(t, h, http.MethodGet, "/api/session", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", rec.Code, rec.Body.Bytes())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache-control %q", got)
	}
	userID, kind := decodeSession(t, rec.Body.String())
	if kind != "guest" {
		t.Fatalf("kind %q, want guest", kind)
	}
	if !id.Valid(userID) {
		t.Fatalf("user id %q is not a public id", userID)
	}
	cookie := oneSessionCookie(t, rec)
	if cookie == nil {
		t.Fatal("first visit set no session cookie")
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("cookie flags httponly %v secure %v samesite %v path %q", cookie.HttpOnly, cookie.Secure, cookie.SameSite, cookie.Path)
	}
	if cookie.MaxAge != int((180 * 24 * time.Hour).Seconds()) {
		t.Fatalf("cookie max-age %d, want Keel's default lifetime", cookie.MaxAge)
	}
	sid := sessionID(t, cookie.Value)
	if strings.Contains(cookie.Value, userID) {
		t.Fatal("cookie value carries the user id")
	}
	if strings.Contains(rec.Body.String(), sid) || strings.Contains(rec.Body.String(), cookie.Value) {
		t.Fatal("body carries the session id or the cookie")
	}
	if strings.Contains(rec.Body.String(), "@") {
		t.Fatal("body carries an address")
	}

	db := openDB(t, dbPath)
	if got := queryText(t, db, "SELECT kind FROM users WHERE id = ?", userID); got != "guest" {
		t.Fatalf("stored kind %q", got)
	}
	if got := queryCount(t, db, "SELECT COUNT(*) FROM sessions WHERE user_id = ? AND revoked = 0", userID); got != 1 {
		t.Fatalf("live sessions %d, want 1", got)
	}
	if got := queryText(t, db, "SELECT id FROM sessions WHERE user_id = ?", userID); got != sid {
		t.Fatalf("session row %q does not match the cookie", got)
	}
	if got := queryCount(t, db, "SELECT COUNT(*) FROM users"); got != 1 {
		t.Fatalf("users %d, want 1", got)
	}
	for _, name := range []string{"users", "sessions", "looks", "renders"} {
		if got := queryCount(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", name); got != 1 {
			t.Fatalf("table %s missing", name)
		}
	}

	again := request(t, h, http.MethodGet, "/api/session", cookie, "")
	if again.Code != http.StatusOK {
		t.Fatalf("second status = %d", again.Code)
	}
	againID, againKind := decodeSession(t, again.Body.String())
	if againID != userID || againKind != "guest" {
		t.Fatalf("second visit %s %s, want %s guest", againID, againKind, userID)
	}
	if oneSessionCookie(t, again) != nil {
		t.Fatal("second visit set a fresh cookie")
	}
	if got := queryCount(t, db, "SELECT COUNT(*) FROM sessions"); got != 1 {
		t.Fatalf("sessions after return %d, want 1", got)
	}

	page := request(t, h, http.MethodGet, "/", nil, "")
	pageCookie := oneSessionCookie(t, page)
	pageID := sessionID(t, pageCookie.Value)
	boot := request(t, h, http.MethodGet, "/api/session", pageCookie, "")
	bootID, bootKind := decodeSession(t, boot.Body.String())
	if bootKind != "guest" {
		t.Fatalf("app boot kind %q", bootKind)
	}
	if got := queryText(t, db, "SELECT user_id FROM sessions WHERE id = ?", pageID); got != bootID {
		t.Fatalf("app session user %q, boot user %q", got, bootID)
	}
}

func TestRevokedSessionEndsOnTheNextRequest(t *testing.T) {
	t.Parallel()
	srv, dbPath := openTest(t, nil)
	h := srv.Handler()
	rec := request(t, h, http.MethodGet, "/api/session", nil, "")
	userID, _ := decodeSession(t, rec.Body.String())
	cookie := oneSessionCookie(t, rec)
	oldID := sessionID(t, cookie.Value)

	if err := srv.Revoke(t.Context(), oldID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	db := openDB(t, dbPath)
	if got := queryCount(t, db, "SELECT revoked FROM sessions WHERE id = ?", oldID); got != 1 {
		t.Fatalf("revoked flag %d, want 1", got)
	}

	next := request(t, h, http.MethodGet, "/api/session", cookie, "")
	if next.Code != http.StatusOK {
		t.Fatalf("status after revoke = %d, body %s", next.Code, next.Body.Bytes())
	}
	nextID, kind := decodeSession(t, next.Body.String())
	if kind != "guest" || nextID == userID {
		t.Fatalf("after revoke user %s kind %s, want a new guest", nextID, kind)
	}
	fresh := oneSessionCookie(t, next)
	if fresh == nil {
		t.Fatal("revoked cookie set no replacement")
	}
	if sessionID(t, fresh.Value) == oldID {
		t.Fatal("replacement cookie kept the revoked session")
	}
	if got := queryCount(t, db, "SELECT revoked FROM sessions WHERE id = ?", oldID); got != 1 {
		t.Fatalf("old session revoked flag %d after the next request", got)
	}
	kept := request(t, h, http.MethodGet, "/api/session", fresh, "")
	keptID, _ := decodeSession(t, kept.Body.String())
	if keptID != nextID {
		t.Fatalf("replacement cookie resolved %s, want %s", keptID, nextID)
	}
	back := request(t, h, http.MethodGet, "/api/session", cookie, "")
	backID, _ := decodeSession(t, back.Body.String())
	if backID == userID || backID == nextID {
		t.Fatalf("revoked cookie resolved %s", backID)
	}
}

func TestDeletedSessionRowDoesNotResolve(t *testing.T) {
	t.Parallel()
	srv, dbPath := openTest(t, nil)
	h := srv.Handler()
	rec := request(t, h, http.MethodGet, "/api/session", nil, "")
	userID, _ := decodeSession(t, rec.Body.String())
	cookie := oneSessionCookie(t, rec)
	session := sessionID(t, cookie.Value)

	db := openDB(t, dbPath)
	if _, err := db.Writer().ExecContext(t.Context(), "DELETE FROM sessions WHERE id = ?", session); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	next := request(t, h, http.MethodGet, "/api/session", cookie, "")
	nextID, kind := decodeSession(t, next.Body.String())
	if kind != "guest" || nextID == userID {
		t.Fatalf("deleted session resolved %s %s", nextID, kind)
	}
	if got := queryCount(t, db, "SELECT COUNT(*) FROM sessions WHERE id = ?", session); got != 0 {
		t.Fatal("deleted session row came back")
	}
}

func TestForgedCookieDoesNotResolve(t *testing.T) {
	t.Parallel()
	srv, _ := openTest(t, nil)
	h := srv.Handler()
	rec := request(t, h, http.MethodGet, "/api/session", nil, "")
	userID, _ := decodeSession(t, rec.Body.String())
	cookie := oneSessionCookie(t, rec)
	forged := *cookie
	forged.Value = forged.Value[:len(forged.Value)-1] + otherHex(forged.Value[len(forged.Value)-1])

	next := request(t, h, http.MethodGet, "/api/session", &forged, "")
	nextID, _ := decodeSession(t, next.Body.String())
	if nextID == userID {
		t.Fatal("forged signature resolved the guest")
	}
}

func TestRestartResolvesTheSameGuest(t *testing.T) {
	t.Parallel()
	dir, app := writeApp(t)
	cfg := testConfig(dir, app)
	srv, err := server.Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	rec := request(t, srv.Handler(), http.MethodGet, "/api/session", nil, "")
	userID, _ := decodeSession(t, rec.Body.String())
	cookie := oneSessionCookie(t, rec)
	if err := srv.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	again, err := server.Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = again.Close() })
	kept := request(t, again.Handler(), http.MethodGet, "/api/session", cookie, "")
	keptID, kind := decodeSession(t, kept.Body.String())
	if keptID != userID || kind != "guest" {
		t.Fatalf("restart resolved %s %s, want %s guest", keptID, kind, userID)
	}
	if oneSessionCookie(t, kept) != nil {
		t.Fatal("restart set a fresh cookie for a live session")
	}

	if err := again.Close(); err != nil {
		t.Fatalf("close reopened: %v", err)
	}
	cfg.SigningKey = []byte("other-test-signing-key-not-the-first")
	other, err := server.Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("open with other key: %v", err)
	}
	t.Cleanup(func() { _ = other.Close() })
	stranger := request(t, other.Handler(), http.MethodGet, "/api/session", cookie, "")
	strangerID, _ := decodeSession(t, stranger.Body.String())
	if strangerID == userID {
		t.Fatal("a different signing key resolved the old cookie")
	}
}

func otherHex(b byte) string {
	if b == 'a' {
		return "b"
	}
	return "a"
}

func decodeSession(t *testing.T, raw string) (string, string) {
	t.Helper()
	if !strings.HasSuffix(raw, "\n") {
		t.Fatalf("session body has no trailing newline")
	}
	var body struct {
		UserID string `json:"user_id"`
		Kind   string `json:"kind"`
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	var extra struct{}
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatalf("session body has trailing data: %v", err)
	}
	return body.UserID, body.Kind
}

func oneSessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	var found *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name != "identity_session" {
			t.Fatalf("unexpected cookie %q", c.Name)
		}
		if found != nil {
			t.Fatal("two session cookies")
		}
		found = c
	}
	return found
}

func sessionID(t *testing.T, value string) string {
	t.Helper()
	idPart, sig, ok := strings.Cut(value, ".")
	if !ok || idPart == "" || sig == "" || strings.Contains(sig, ".") {
		t.Fatal("cookie is not one session id and one signature")
	}
	if !id.Valid(idPart) {
		t.Fatalf("session id %q is not a public id", idPart)
	}
	if len(sig) != 64 || strings.Trim(sig, "0123456789abcdef") != "" {
		t.Fatal("signature is not 64 lowercase hex characters")
	}
	return idPart
}

func openDB(t *testing.T, path string) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(t.Context(), sqlite.Config{
		Path:   path,
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("open rows: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func queryText(t *testing.T, db *sqlite.DB, query string, args ...any) string {
	t.Helper()
	var got string
	if err := db.Reader().QueryRowContext(t.Context(), query, args...).Scan(&got); err != nil {
		t.Fatalf("query: %v", err)
	}
	return got
}

func queryCount(t *testing.T, db *sqlite.DB, query string, args ...any) int64 {
	t.Helper()
	var got int64
	if err := db.Reader().QueryRowContext(t.Context(), query, args...).Scan(&got); err != nil {
		t.Fatalf("query: %v", err)
	}
	return got
}
