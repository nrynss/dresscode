package systemone

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nrynss/keel/cost"
)

const openrouterTestToken = "openrouter-test-key"

// No OpenRouter exchange was recorded: T0.6 recorded Clef only, and no
// live call has been made. The reply below is OpenRouter's documented
// decision shape, held here to pin the decode. It is a fixture, not a
// recording; the comparison task records the real exchange.
const documentedDecisionReply = `{
  "model": "typesafe/jev-1.14-20261014",
  "answers": {
    "met": {"type": "noul", "noul": 0.25},
    "issue": {"type": "choice", "choice": "collar",
              "probabilities": {"collar": 0.7, "none": 0.3}, "confidence": 0.4}
  },
  "usage": {"input_tokens": 357, "output_tokens": 38, "cost": 0.000014994},
  "id": "gen-dec-documentation",
  "provider": "TypeSafe"
}`

type decisionReplay struct {
	t   *testing.T
	url string

	mu         sync.Mutex
	paths      []string
	bodies     []json.RawMessage
	mismatches int
	statuses   []int
	step       int
}

func newDecisionReplay(t *testing.T, statuses []int) *decisionReplay {
	t.Helper()
	s := &decisionReplay{t: t, statuses: statuses}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	s.url = srv.URL
	return s
}

func (s *decisionReplay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes+1))
	if err != nil {
		http.Error(w, "body", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	authorized := r.Header.Get("Authorization") == "Bearer "+openrouterTestToken
	if !authorized {
		s.mismatches++
	}
	s.paths = append(s.paths, r.URL.Path)
	s.bodies = append(s.bodies, json.RawMessage(body))
	status := http.StatusOK
	if s.step < len(s.statuses) {
		status = s.statuses[s.step]
		s.step++
	}
	s.mu.Unlock()
	if !authorized {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, documentedDecisionReply)
}

func (s *decisionReplay) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.paths)
}

func (s *decisionReplay) lastPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.paths) == 0 {
		s.t.Fatal("no call was made")
	}
	return s.paths[len(s.paths)-1]
}

func (s *decisionReplay) lastBody() json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bodies) == 0 {
		s.t.Fatal("no call was made")
	}
	return s.bodies[len(s.bodies)-1]
}

func (s *decisionReplay) unauthorized() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mismatches
}

func newOpenRouterTestClient(t *testing.T, base string, meter *cost.Meter) *OpenRouter {
	t.Helper()
	client, err := NewOpenRouter(OpenRouterConfig{
		BaseURL:  base,
		Token:    openrouterTestToken,
		Meter:    meter,
		Throttle: instantPacer(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestOpenRouterDecodesTheDocumentedShape(t *testing.T) {
	t.Parallel()
	// The base URL is the configured prefix, /api/v1 in docs/keys.md; the
	// decisions path is appended to it.
	wire := newDecisionReplay(t, nil)
	meter, _, ledger := newTestMeter(t, cost.Dollar)
	client := newOpenRouterTestClient(t, wire.url+"/api/v1", meter)

	req := sampleRequest(t, nil)
	req.Model = "typesafe/jev-1.14"
	got, err := client.Ask(context.Background(), req)
	if err != nil {
		failErr(t, err)
	}
	if want := "/api/v1" + decisionsPath; wire.lastPath() != want {
		t.Fatalf("path %s, want %s", wire.lastPath(), want)
	}
	if got.Model == "" || len(got.Answers) != 2 {
		t.Fatalf("decoded %+v", got)
	}
	if got.Answers["met"].Noul == nil || *got.Answers["met"].Noul != 0.25 {
		t.Fatalf("noul %#v", got.Answers["met"])
	}
	issue := got.Answers["issue"]
	if issue.Choice != "collar" || issue.Confidence == nil || *issue.Confidence != 0.4 {
		t.Fatalf("choice %#v", issue)
	}
	if issue.Probabilities["collar"] != 0.7 || issue.Probabilities["none"] != 0.3 {
		t.Fatalf("probabilities %#v", issue.Probabilities)
	}
	if got.Usage.InputTokens != 357 || got.Usage.OutputTokens != 38 {
		t.Fatalf("usage %+v", got.Usage)
	}

	body := wire.lastBody()
	var sent struct {
		Model     string              `json:"model"`
		State     json.RawMessage     `json:"state"`
		Questions map[string]Question `json:"questions"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Model != "typesafe/jev-1.14" {
		t.Fatalf("body model %q", sent.Model)
	}
	if string(sent.State) != `"date"` {
		t.Fatalf("body state %s", sent.State)
	}
	if _, ok := sent.Questions["issue"]; !ok {
		t.Fatalf("body questions %v", sent.Questions)
	}
	if strings.Contains(string(body), openrouterTestToken) {
		t.Fatal("body carries the key")
	}

	charges := ledger.Charges()
	if len(charges) != 1 {
		t.Fatalf("%d charges", len(charges))
	}
	charge := charges[0]
	if charge.Kind != "openrouter.typesafe/jev-1.14" || charge.Units != 1 {
		t.Fatalf("charge %+v", charge)
	}
	// The reply named its own cost: 0.000014994 dollars settles as
	// 14,994 nanodollars, measured.
	if charge.UnitPrice != 14_994 {
		t.Fatalf("settled %d, want the reply's cost", charge.UnitPrice)
	}
	if wire.unauthorized() != 0 {
		t.Fatal("the client did not send the bearer key")
	}
}

// Images ride in the state as image_url parts carrying data URLs, the
// documented form. The pixel cap is Clef's, not Jev's, so the byte caps
// are all this endpoint applies on its own.
func TestOpenRouterImagesRideInTheState(t *testing.T) {
	t.Parallel()
	wire := newDecisionReplay(t, nil)
	meter, _, ledger := newTestMeter(t, cost.Dollar)
	client := newOpenRouterTestClient(t, wire.url, meter)

	req := sampleRequest(t, nil)
	req.Model = "typesafe/jev-1.14"
	req.Images = []Image{{ContentType: "image/png", Bytes: []byte{0x89, 'P', 'N', 'G'}}}
	if _, err := client.Ask(context.Background(), req); err != nil {
		failErr(t, err)
	}
	var sent struct {
		State []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			ImageURL *struct {
				URL string `json:"url"`
			} `json:"image_url"`
		} `json:"state"`
	}
	if err := json.Unmarshal(wire.lastBody(), &sent); err != nil {
		t.Fatal(err)
	}
	if len(sent.State) != 2 || sent.State[0].Type != "text" || sent.State[0].Text != `"date"` {
		t.Fatalf("state parts %#v", sent.State)
	}
	part := sent.State[1]
	if part.Type != "image_url" || part.ImageURL == nil || !strings.HasPrefix(part.ImageURL.URL, "data:image/png;base64,") {
		t.Fatalf("image part %#v", part)
	}
	if len(ledger.Charges()) != 1 {
		t.Fatalf("%d charges", len(ledger.Charges()))
	}
}

func TestOpenRouterRefusesOversizeBeforeTheCall(t *testing.T) {
	t.Parallel()
	wire := newDecisionReplay(t, nil)
	meter, _, ledger := newTestMeter(t, cost.Dollar)
	client := newOpenRouterTestClient(t, wire.url, meter)
	req := sampleRequest(t, nil)
	req.Model = "typesafe/jev-1.14"
	req.Images = []Image{{ContentType: "image/jpeg", Bytes: make([]byte, maxImageBytes+1)}}
	_, err := client.Ask(context.Background(), req)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Check != "image_bytes" {
		t.Fatalf("err = %v, want LimitError", err)
	}
	if wire.calls() != 0 || len(ledger.Charges()) != 0 {
		t.Fatal("an oversize image reached the wire")
	}
}

func TestOpenRouterDoesNotRetryARefusal(t *testing.T) {
	t.Parallel()
	wire := newDecisionReplay(t, []int{http.StatusBadRequest})
	meter, _, ledger := newTestMeter(t, cost.Dollar)
	client := newOpenRouterTestClient(t, wire.url, meter)
	req := sampleRequest(t, nil)
	req.Model = "typesafe/jev-1.14"
	_, err := client.Ask(context.Background(), req)
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Code != http.StatusBadRequest {
		t.Fatalf("err = %v", err)
	}
	if wire.calls() != 1 {
		t.Fatalf("%d requests, want one", wire.calls())
	}
	if len(ledger.Charges()) != 0 {
		t.Fatal("a charge for a refused call")
	}
}

func TestOpenRouterConfigIsValidated(t *testing.T) {
	t.Parallel()
	meter, _, _ := newTestMeter(t, cost.Dollar)
	if _, err := NewOpenRouter(OpenRouterConfig{}); err == nil {
		t.Fatal("empty config accepted")
	}
	if _, err := NewOpenRouter(OpenRouterConfig{BaseURL: "https://openrouter.ai/api/v1", Meter: meter}); err == nil {
		t.Fatal("empty key accepted")
	}
	if _, err := NewOpenRouter(OpenRouterConfig{BaseURL: "https://openrouter.ai/api/v1", Token: openrouterTestToken}); err == nil {
		t.Fatal("nil meter accepted")
	}
	if _, err := NewOpenRouter(OpenRouterConfig{BaseURL: "http://user@openrouter.ai", Token: openrouterTestToken, Meter: meter}); err == nil {
		t.Fatal("userinfo accepted")
	}
	if _, err := NewOpenRouter(OpenRouterConfig{BaseURL: "https://openrouter.ai/api?q=1", Token: openrouterTestToken, Meter: meter}); err == nil {
		t.Fatal("query accepted")
	}
}
