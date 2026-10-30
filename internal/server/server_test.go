package server_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nrynss/dresscode/internal/server"
	"github.com/nrynss/keel/gate"
	"github.com/nrynss/keel/identity"
)

const indexHTML = "<!doctype html><title>Dresscode</title>"

func TestOpenRejectsBadConfig(t *testing.T) {
	t.Parallel()
	dir, app := writeApp(t)
	dbPath := filepath.Join(dir, "dresscode.db")
	good := testConfig(dir, app)
	cases := []struct {
		name string
		edit func(*server.Config)
		want string
	}{
		{name: "no database", edit: func(c *server.Config) { c.DBPath = "" }, want: "database path"},
		{name: "no key", edit: func(c *server.Config) { c.SigningKey = nil }, want: "signing key"},
		{name: "no app", edit: func(c *server.Config) { c.AppDir = filepath.Join(dir, "missing") }, want: "app dir"},
		{name: "app is a file", edit: func(c *server.Config) { c.AppDir = filepath.Join(app, "index.html") }, want: "not a directory"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := good
			cfg.SigningKey = append([]byte(nil), good.SigningKey...)
			tc.edit(&cfg)
			if _, err := server.Open(t.Context(), cfg); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("open error %v, want it to mention %q", err, tc.want)
			}
		})
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatal("a rejected open created the database")
	}
	if _, err := server.Open(nil, good); err == nil || !strings.Contains(err.Error(), "nil context") {
		t.Fatalf("nil context error %v", err)
	}
}

func TestAppIsServedAndAPIMissesAreNotTheApp(t *testing.T) {
	t.Parallel()
	srv, _ := openTest(t, nil)
	h := srv.Handler()

	page := request(t, h, http.MethodGet, "/", nil, "")
	if page.Code != http.StatusOK || page.Body.String() != indexHTML {
		t.Fatalf("index status %d body %q", page.Code, page.Body.String())
	}
	if page.Header().Get("Content-Type") != "text/html; charset=utf-8" || page.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("index headers %v", page.Header())
	}
	if oneSessionCookie(t, page) == nil {
		t.Fatal("first page view set no session cookie")
	}

	asset := request(t, h, http.MethodGet, "/assets/mark.js", nil, "")
	if asset.Code != http.StatusOK || asset.Body.String() != "mark" {
		t.Fatalf("asset status %d body %q", asset.Code, asset.Body.String())
	}
	route := request(t, h, http.MethodGet, "/looks/1", nil, "")
	if route.Body.String() != indexHTML {
		t.Fatalf("client route body %q", route.Body.String())
	}
	dir := request(t, h, http.MethodGet, "/assets", nil, "")
	if dir.Body.String() != indexHTML || strings.Contains(dir.Body.String(), "mark.js") {
		t.Fatal("directory listing escaped")
	}
	// The mux redirects a path that still contains ".." to its cleaned
	// form. The redirect may echo that form. It must not echo the file
	// that sits outside the app directory.
	secret := request(t, h, http.MethodGet, "/../secret.txt", nil, "")
	if strings.Contains(secret.Body.String(), "OUTSIDE-BYTES") {
		t.Fatal("app dir allowed a path escape")
	}

	miss := request(t, h, http.MethodPost, "/api/session/code", nil, "")
	assertNotFound(t, miss)
	gone := request(t, h, http.MethodPost, "/api/account/deletion", nil, "")
	assertNotFound(t, gone)
	media := request(t, h, http.MethodGet, "/media/0123456789abcdef0123456789abcdef", nil, "")
	if media.Code != http.StatusNotFound || media.Body.String() != "404 page not found\n" {
		t.Fatalf("media miss status %d body %q", media.Code, media.Body.String())
	}
	if media.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("media cache-control %q", media.Header().Get("Cache-Control"))
	}
	if strings.Contains(media.Body.String(), "Dresscode") {
		t.Fatal("media miss served the app")
	}
}

func TestLookGateRefusesBeforeTheHandler(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	var sawGuest atomic.Bool
	hour := time.Hour
	clock := &movedClock{at: time.Unix(1_700_000_000, 0)}
	srv, dbPath := openTest(t, func(c *server.Config) {
		c.Now = clock.now
		c.LookPerClient = gate.Limit{Burst: 1, Every: hour}
		c.LookGlobal = gate.Limit{Burst: 1, Every: hour}
	})
	h := srv.Handler()
	if err := srv.SetSpendHandler(server.RouteLook, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		user, ok := identity.UserFromContext(r.Context())
		if ok && user.Kind == identity.KindGuest && user.ID != "" {
			sawGuest.Store(true)
		}
		w.WriteHeader(http.StatusNoContent)
	})); err != nil {
		t.Fatalf("set handler: %v", err)
	}
	if err := srv.SetSpendHandler("POST /api/samples/x/looks", nil); err == nil {
		t.Fatal("a path that does not spend accepted a handler")
	}

	boot := request(t, h, http.MethodGet, "/api/session", nil, "")
	userID, _ := decodeSession(t, boot.Body.String())
	cookie := oneSessionCookie(t, boot)

	first := postLook(t, h, cookie, "203.0.113.7:1000")
	if first.Code != http.StatusNoContent {
		t.Fatalf("admitted status %d body %s", first.Code, first.Body.Bytes())
	}
	if calls.Load() != 1 || !sawGuest.Load() {
		t.Fatalf("handler calls %d guest %v", calls.Load(), sawGuest.Load())
	}

	refused := postLook(t, h, cookie, "203.0.113.7:1000")
	assertRateLimited(t, refused)
	if calls.Load() != 1 {
		t.Fatalf("refusal called the handler, calls %d", calls.Load())
	}
	other := postLook(t, h, cookie, "203.0.113.8:1000")
	assertRateLimited(t, other)
	if calls.Load() != 1 {
		t.Fatalf("global cap called the handler, calls %d", calls.Load())
	}

	sample := request(t, h, http.MethodPost, "/api/samples/sample-family/looks", cookie, "203.0.113.9:1000")
	assertNotFound(t, sample)
	session := request(t, h, http.MethodGet, "/api/session", cookie, "203.0.113.9:1000")
	if session.Code != http.StatusOK {
		t.Fatalf("session behind an exhausted gate: %d", session.Code)
	}
	kept, _ := decodeSession(t, session.Body.String())
	if kept != userID {
		t.Fatalf("session user %s, want %s", kept, userID)
	}

	db := openDB(t, dbPath)
	if got := queryCount(t, db, "SELECT COUNT(*) FROM looks"); got != 0 {
		t.Fatalf("looks %d, want 0", got)
	}

	clock.advance(hour)
	refilled := postLook(t, h, cookie, "203.0.113.7:1000")
	if refilled.Code != http.StatusNoContent || calls.Load() != 2 {
		t.Fatalf("after refill status %d calls %d", refilled.Code, calls.Load())
	}
}

func TestDefaultLookBurstThenRefuses(t *testing.T) {
	t.Parallel()
	srv, dbPath := openTest(t, nil)
	h := srv.Handler()
	var cookie *http.Cookie
	for i := 1; i <= 8; i++ {
		rec := postLook(t, h, cookie, "192.0.2.8:80")
		assertNotFound(t, rec)
		if cookie == nil {
			cookie = oneSessionCookie(t, rec)
		}
	}
	refused := postLook(t, h, cookie, "192.0.2.8:80")
	assertRateLimited(t, refused)
	db := openDB(t, dbPath)
	if got := queryCount(t, db, "SELECT COUNT(*) FROM users"); got != 1 {
		t.Fatalf("users %d, want 1", got)
	}
	if got := queryCount(t, db, "SELECT COUNT(*) FROM looks"); got != 0 {
		t.Fatalf("looks %d, want 0", got)
	}
}

func TestGateTrustsForwardedHeadersOnlyFromAProxy(t *testing.T) {
	t.Parallel()
	srv, _ := openTest(t, func(c *server.Config) {
		c.LookPerClient = gate.Limit{Burst: 1, Every: time.Hour}
		c.LookGlobal = gate.Limit{Burst: 5, Every: time.Hour}
	})
	h := srv.Handler()
	if err := srv.SetSpendHandler(server.RouteLook, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})); err != nil {
		t.Fatalf("set handler: %v", err)
	}
	cookie := oneSessionCookie(t, request(t, h, http.MethodGet, "/api/session", nil, ""))

	public := func(forwarded string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/looks", nil)
		req.RemoteAddr = "203.0.113.20:4000"
		req.Header.Set("X-Forwarded-For", forwarded)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if public("198.51.100.1").Code != http.StatusNoContent {
		t.Fatal("first public request was refused")
	}
	if public("198.51.100.2").Code != http.StatusTooManyRequests {
		t.Fatal("a public peer minted a bucket from X-Forwarded-For")
	}

	viaProxy := func(client string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/looks", nil)
		req.RemoteAddr = "127.0.0.1:4000"
		req.Header.Set("CF-Connecting-IP", client)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if viaProxy("198.51.100.10") != http.StatusNoContent || viaProxy("198.51.100.11") != http.StatusNoContent {
		t.Fatal("loopback did not honour two CF-Connecting-IP clients")
	}
	if viaProxy("198.51.100.10") != http.StatusTooManyRequests {
		t.Fatal("a proxied client exceeded its own bucket")
	}
}

type movedClock struct {
	at time.Time
}

func (c *movedClock) now() time.Time { return c.at }

func (c *movedClock) advance(d time.Duration) { c.at = c.at.Add(d) }

func openTest(t *testing.T, tweak func(*server.Config)) (*server.Server, string) {
	t.Helper()
	dir, app := writeApp(t)
	cfg := testConfig(dir, app)
	if tweak != nil {
		tweak(&cfg)
	}
	srv, err := server.Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv, cfg.DBPath
}

func testConfig(dir, app string) server.Config {
	return server.Config{
		DBPath:     filepath.Join(dir, "dresscode.db"),
		AppDir:     app,
		SigningKey: []byte("test-session-signing-key-32b!!!!"),
		Log:        slog.New(slog.DiscardHandler),
	}
}

func writeApp(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	app := filepath.Join(dir, "app")
	if err := os.MkdirAll(filepath.Join(app, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "index.html"), []byte(indexHTML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "assets", "mark.js"), []byte("mark"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("OUTSIDE-BYTES"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, app
}

func request(t *testing.T, h http.Handler, method, path string, cookie *http.Cookie, remote string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
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

func postLook(t *testing.T, h http.Handler, cookie *http.Cookie, remote string) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, h, http.MethodPost, "/api/looks", cookie, remote)
}

func assertNotFound(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	env := decodeError(t, rec.Body.String())
	if rec.Code != http.StatusNotFound || env.Code != "not_found" || env.Message != "No such resource exists." {
		t.Fatalf("not found status %d code %q message %q", rec.Code, env.Code, env.Message)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("not found headers %v", rec.Header())
	}
}

func assertRateLimited(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	env := decodeError(t, rec.Body.String())
	if rec.Code != http.StatusTooManyRequests || env.Code != "rate_limited" || env.Message != "the route rate limit was reached" {
		t.Fatalf("rate limit status %d code %q message %q", rec.Code, env.Code, env.Message)
	}
	if env.RetryAfter < 1 {
		t.Fatalf("retry_after_seconds %d", env.RetryAfter)
	}
	if rec.Header().Get("Retry-After") != jsonNumber(env.RetryAfter) {
		t.Fatalf("Retry-After %q, detail %d", rec.Header().Get("Retry-After"), env.RetryAfter)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("rate limit headers %v", rec.Header())
	}
}

func jsonNumber(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

type errorBody struct {
	Code       string
	Message    string
	RetryAfter int
}

func decodeError(t *testing.T, raw string) errorBody {
	t.Helper()
	if !strings.HasSuffix(raw, "\n") {
		t.Fatal("error body has no trailing newline")
	}
	var body struct {
		Error struct {
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Detail  json.RawMessage `json:"detail"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	out := errorBody{Code: body.Error.Code, Message: body.Error.Message}
	if len(body.Error.Detail) > 0 {
		var detail struct {
			RetryAfter int `json:"retry_after_seconds"`
		}
		if err := json.Unmarshal(body.Error.Detail, &detail); err != nil {
			t.Fatalf("decode detail: %v", err)
		}
		out.RetryAfter = detail.RetryAfter
	}
	return out
}
