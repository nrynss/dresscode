package mail

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/keel/identity"
	"github.com/nrynss/keel/identity/sqlitestore"
	"github.com/nrynss/keel/sqlite"
	"github.com/nrynss/keel/wire"
)

// TestSendLimitRefusesBeforeResend checks Keel's address ceiling on the
// code route. Five codes are posted. The sixth is refused, and Resend is
// not asked again.
func TestSendLimitRefusesBeforeResend(t *testing.T) {
	reply := recordedReply(t)
	var mu sync.Mutex
	var posts int
	var codes []string
	log, text := captureLog()
	client := newClient(t, log, func(r *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return jsonResponse(http.StatusOK, reply), nil
		}
		var body resendBody
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode body: %v", err)
		} else {
			code := codeIn(body.Text)
			mu.Lock()
			posts++
			if code != "" {
				codes = append(codes, code)
			}
			mu.Unlock()
		}
		return jsonResponse(http.StatusOK, reply), nil
	})

	db := openIdentityDB(t)
	store, err := sqlitestore.Open(t.Context(), sqlitestore.Config{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_758_000_000, 0)
	svc, err := identity.New(identity.Config{
		Store:      store,
		SigningKey: []byte("test-signing-key"),
		CodeKey:    []byte("test-code-key"),
		Mail:       client,
		Log:        log,
		Now:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := svc.Middleware(svc.CodeSignInHandler("/api/session/code", "/api/session/code/verify"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	cookie := sessionCookie(t, rec)

	for i := 0; i < 5; i++ {
		got := postCode(handler, cookie)
		if got.Code != http.StatusAccepted {
			t.Fatalf("send %d = %d, want 202", i+1, got.Code)
		}
	}
	sixth := postCode(handler, cookie)
	if sixth.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth send = %d, want 429", sixth.Code)
	}
	var refused struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(sixth.Body.Bytes(), &refused); err != nil {
		t.Fatal(err)
	}
	if refused.Error.Code != wire.CodeSendLimited {
		t.Fatalf("refusal %q, want %q", refused.Error.Code, wire.CodeSendLimited)
	}
	mu.Lock()
	gotPosts := posts
	gotCodes := append([]string(nil), codes...)
	mu.Unlock()
	if gotPosts != 5 || len(gotCodes) != 5 {
		t.Fatalf("resend posts %d with %d codes, want 5 and 5", gotPosts, len(gotCodes))
	}
	logged := text() + sixth.Body.String()
	for _, code := range gotCodes {
		assertQuiet(t, logged, code)
	}
	assertQuiet(t, logged, fixtureKey)
}

func postCode(handler http.Handler, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/session/code", strings.NewReader(`{"address":"`+fromAddress+`"}`))
	req.RemoteAddr = "192.0.2.10:4000"
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "identity_session" {
			return cookie
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func openIdentityDB(t *testing.T) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(t.Context(), sqlite.Config{
		Path:   filepath.Join(t.TempDir(), "identity.db"),
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func codeIn(text string) string {
	const marker = "Your Dresscode sign-in code is "
	i := strings.Index(text, marker)
	if i < 0 || len(text) < i+len(marker)+6 {
		return ""
	}
	code := text[i+len(marker) : i+len(marker)+6]
	if !sixDigits(code) {
		return ""
	}
	return code
}
