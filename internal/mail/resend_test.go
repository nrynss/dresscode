package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	fromAddress = "dresscode@nryn.dev"
	fixtureKey  = "fixture-resend-key"
	signInCode  = "918273"
)

func TestSendUsesTheRecordedReply(t *testing.T) {
	t.Parallel()
	reply := recordedReply(t)
	var parsed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(reply, &parsed); err != nil || parsed.ID == "" {
		t.Fatal("recorded reply has no id")
	}
	var posts int
	var sawCode bool
	log, text := captureLog()
	client := newClient(t, log, func(r *http.Request) (*http.Response, error) {
		posts++
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		if r.URL.String() != "https://api.resend.com/emails" {
			t.Errorf("url %s", r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer "+fixtureKey {
			t.Error("authorization does not match the configured key")
		}
		if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" {
			t.Error("content headers are wrong")
		}
		if r.Header.Get("User-Agent") != userAgent {
			t.Error("user agent is wrong")
		}
		body := readBody(t, r)
		assertQuiet(t, body, fixtureKey)
		sawCode = messageCarriesCode(t, body, fromAddress, signInCode)
		return jsonResponse(http.StatusOK, reply), nil
	})

	if err := client.Send(context.Background(), "Dresscode@nryn.dev", " "+signInCode+" "); err != nil {
		t.Fatalf("send: %v", err)
	}
	if posts != 1 || !sawCode {
		t.Fatalf("posts %d, message checked %v", posts, sawCode)
	}
	logged := text()
	if !strings.Contains(logged, parsed.ID) {
		t.Fatal("log did not record the provider id from the reply")
	}
	assertQuiet(t, logged, signInCode, fixtureKey)
	printed := fmt.Sprintf("%v %#v %s %+v", client, client, client, client)
	assertQuiet(t, printed, fixtureKey, signInCode)
	var buf bytes.Buffer
	textLogger(&buf).Info("client", "client", client, "key", client.key)
	assertQuiet(t, buf.String(), fixtureKey, signInCode)
}

func TestSendRefusesBeforePosting(t *testing.T) {
	t.Parallel()
	var posts int
	log, text := captureLog()
	client := newClient(t, log, func(*http.Request) (*http.Response, error) {
		posts++
		return jsonResponse(http.StatusOK, recordedReply(t)), nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		name    string
		ctx     context.Context
		address string
		code    string
	}{
		{"cancelled", ctx, fromAddress, signInCode},
		{"bad address", context.Background(), "not-an-address", signInCode},
		{"bad code", context.Background(), fromAddress, "12345"},
		{"letters", context.Background(), fromAddress, "12345a"},
	}
	for _, tc := range cases {
		if err := client.Send(tc.ctx, tc.address, tc.code); err == nil {
			t.Errorf("%s: send succeeded", tc.name)
		}
	}
	if posts != 0 {
		t.Fatalf("%d posts, want none before a valid send", posts)
	}
	assertQuiet(t, text(), signInCode, fixtureKey)

	if _, err := New(Config{From: fromAddress}); err == nil {
		t.Fatal("empty key was accepted")
	}
	if _, err := New(Config{APIKey: fixtureKey}); err == nil {
		t.Fatal("empty from address was accepted")
	}
	if _, err := New(Config{APIKey: fixtureKey, From: fromAddress, BaseURL: "http://api.resend.com"}); err == nil {
		t.Fatal("cleartext base url was accepted")
	}
	named, err := New(Config{APIKey: fixtureKey, From: "Dresscode <dresscode@nryn.dev>", HTTP: client.http})
	if err != nil {
		t.Fatal(err)
	}
	if named.from != "Dresscode <dresscode@nryn.dev>" {
		t.Fatal("display name was not kept")
	}
}

func TestSendFailureDoesNotLogTheCode(t *testing.T) {
	t.Parallel()
	log, text := captureLog()
	body := []byte(`{"statusCode":422,"name":"validation_error","message":"refused ` + signInCode + `"}`)
	client := newClient(t, log, func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusUnprocessableEntity, body), nil
	})
	err := client.Send(context.Background(), fromAddress, signInCode)
	if err == nil {
		t.Fatal("refused send succeeded")
	}
	assertQuiet(t, err.Error(), signInCode, fixtureKey)
	var refused *Refusal
	if !errors.As(err, &refused) || refused.Status != http.StatusUnprocessableEntity || refused.Name != "validation_error" {
		t.Fatal("expected a validation refusal")
	}
	assertQuiet(t, text(), signInCode, fixtureKey)

	echo := newClient(t, log, func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []byte(`{"id":"`+signInCode+`"}`)), nil
	})
	if err := echo.Send(context.Background(), fromAddress, signInCode); err != nil {
		t.Fatal(err)
	}
	assertQuiet(t, text(), signInCode, fixtureKey)

	leaky := newClient(t, log, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial " + signInCode + " " + fixtureKey)
	})
	err = leaky.Send(context.Background(), fromAddress, signInCode)
	if err == nil {
		t.Fatal("leaking transport error was returned as success")
	}
	assertQuiet(t, err.Error(), signInCode, fixtureKey)
	if !strings.Contains(err.Error(), "resend request failed") {
		t.Fatal("transport error was not replaced")
	}
	assertQuiet(t, text(), signInCode, fixtureKey)

	var hops int
	redirected := newClient(t, log, func(*http.Request) (*http.Response, error) {
		hops++
		return jsonResponse(http.StatusFound, []byte(signInCode)), nil
	})
	err = redirected.Send(context.Background(), fromAddress, signInCode)
	if err == nil || hops != 1 {
		t.Fatal("redirect was followed or treated as success")
	}
	assertQuiet(t, err.Error(), signInCode, fixtureKey)
	assertQuiet(t, text(), signInCode, fixtureKey)
}

func TestMessageIsPlainTextAndMinimalHTML(t *testing.T) {
	t.Parallel()
	text := textBody(signInCode)
	page := htmlBody(signInCode)
	if strings.Count(text, signInCode) != 1 || strings.Count(page, signInCode) != 1 {
		t.Fatal("the code is not in both versions once")
	}
	if strings.Contains(text, "<") || strings.Contains(page, "http") || strings.Contains(page, "src=") || strings.Contains(page, "<a") || strings.Contains(page, "style=") {
		t.Fatal("the html version is not minimal, or the text version is html")
	}
	if strings.Contains(subject, signInCode) || strings.ContainsAny(subject, "0123456789") {
		t.Fatal("subject carries the code")
	}
	if !strings.Contains(text, "10 minutes") || !strings.Contains(page, "10 minutes") {
		t.Fatal("the message does not say how long the code lives")
	}
}

func newClient(t *testing.T, log *slog.Logger, rt func(*http.Request) (*http.Response, error)) *Client {
	t.Helper()
	client, err := New(Config{
		APIKey: fixtureKey,
		From:   fromAddress,
		Log:    log,
		HTTP:   &http.Client{Transport: roundTripFunc(rt)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d", status),
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

func readBody(t *testing.T, r *http.Request) string {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func messageCarriesCode(t *testing.T, raw, from, code string) bool {
	t.Helper()
	var body resendBody
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}
	if body.From != from || len(body.To) != 1 || body.To[0] != from {
		t.Fatal("from or recipient is wrong")
	}
	if body.Subject != subject {
		t.Fatal("subject is wrong")
	}
	if strings.Contains(body.Subject, code) {
		t.Fatal("subject contains the code")
	}
	keys := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 5 {
		t.Fatal("message carries more than the sign-in fields")
	}
	return strings.Contains(body.Text, code) && strings.Contains(body.HTML, code) &&
		strings.Count(body.Text, code) == 1 && strings.Count(body.HTML, code) == 1 &&
		!strings.Contains(body.HTML, "http") && !strings.Contains(body.Text, "<")
}

func recordedReply(t *testing.T) []byte {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	path := filepath.Join(filepath.Dir(file), "testdata", "send.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "@") || strings.Contains(text, fixtureKey) || strings.Contains(text, "Bearer") {
		t.Fatal("recorded reply contains an address or a key")
	}
	var reply struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil || reply.ID == "" {
		t.Fatal("recorded reply is not a resend id")
	}
	return raw
}

func captureLog() (*slog.Logger, func() string) {
	var buf bytes.Buffer
	return textLogger(&buf), func() string { return buf.String() }
}

func textLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
}

func assertQuiet(t *testing.T, text string, hidden ...string) {
	t.Helper()
	for _, secret := range hidden {
		if secret != "" && strings.Contains(text, secret) {
			t.Fatal("output contains a hidden value")
		}
	}
}
