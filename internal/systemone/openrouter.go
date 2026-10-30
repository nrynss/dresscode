package systemone

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/throttle"
)

// Jev is OpenRouter's decision model, kept so Clef can be compared
// against it on the same pick requests. The comparison itself is a later
// task; this file is only the endpoint behind the same SystemOne
// interface.
//
// T0.6 recorded no OpenRouter exchange, so nothing here is pinned by a
// recording. The request and reply shapes follow OpenRouter's documented
// decisions API: POST <base>/alpha/decisions, the same state and typed
// questions as Clef, answers keyed by question id, and a usage that
// reports tokens and the call's cost. The comparison task confirms the
// shapes against one live call and records it.
const (
	// decisionsPath is appended to the resolved base URL. The configured
	// OPENROUTER_BASE_URL is a prefix such as https://openrouter.ai/api/v1;
	// if OpenRouter serves the decisions call only outside that prefix,
	// the value in the settings changes, not this path.
	decisionsPath = "/alpha/decisions"

	// nanodollarsPerJevInputToken is OpenRouter's documented rate for
	// Jev, $0.042 per million input tokens; output bills nothing. It
	// prices the reserve. A reply that reports its own cost settles at
	// that figure instead.
	nanodollarsPerJevInputToken = 42_000
)

// OpenRouterConfig is how the client reaches OpenRouter. The token and
// the base URL come from the caller's resolved references, never a file.
type OpenRouterConfig struct {
	// BaseURL is the resolved OPENROUTER_BASE_URL, a prefix the decisions
	// path is appended to. It may carry a path; it must be an http
	// address without userinfo, query or fragment.
	BaseURL string
	// Token is the OPENROUTER_API_KEY value. It is sent as the bearer and
	// is never written into an error.
	Token string
	// Meter meters every Ask. Nil is refused.
	Meter *cost.Meter
	// HTTP is the client. Nil means the shared no-redirect client.
	HTTP *http.Client
	// Throttle paces and retries the call, as WorkersAIConfig.Throttle.
	Throttle throttle.Config
}

// OpenRouter is the System One client for Jev. One Ask is one metered
// call: quoted, reserved, and settled once. Safe for concurrent use.
// Tests point BaseURL at a local server and do not dial OpenRouter.
type OpenRouter struct {
	base  url.URL
	token string
	meter *cost.Meter
	http  *http.Client
	pacer throttle.Config
}

var _ SystemOne = (*OpenRouter)(nil)

// NewOpenRouter returns a client for cfg. It does not dial.
func NewOpenRouter(cfg OpenRouterConfig) (*OpenRouter, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("systemone: openrouter base url is empty")
	}
	if cfg.Token == "" {
		return nil, errors.New("systemone: openrouter token is empty")
	}
	if cfg.Meter == nil {
		return nil, errors.New("systemone: openrouter meter is nil")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return nil, errors.New("systemone: openrouter base url must be an http address")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("systemone: openrouter base url must not carry a query or fragment")
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	httpClient := cfg.HTTP
	if httpClient == nil {
		httpClient = noRedirectHTTP(defaultTimeout)
	}
	pacer := cfg.Throttle
	pacer.Transient = transientFault
	pacer.RetryAfter = chainRetryAfter(cfg.Throttle.RetryAfter)
	return &OpenRouter{
		base:  *u,
		token: cfg.Token,
		meter: cfg.Meter,
		http:  httpClient,
		pacer: pacer,
	}, nil
}

// Ask makes one decision call on Jev. The request shape is the same one
// Clef takes; the endpoint and the reply are OpenRouter's. The images
// ride in the state as image_url parts carrying data URLs, which is the
// documented form. An oversize image is refused typed before any call.
func (c *OpenRouter) Ask(ctx context.Context, req Request) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	if err := req.Validate(); err != nil {
		return Response{}, err
	}
	if strings.TrimSpace(req.Model) == "" {
		return Response{}, errors.New("systemone: openrouter model is empty")
	}
	if err := checkImages(req.Images, jevImageLimits); err != nil {
		return Response{}, err
	}
	state, err := jevState(req)
	if err != nil {
		return Response{}, err
	}
	body, err := json.Marshal(decisionBody{
		Model:     req.Model,
		State:     state,
		Questions: req.Questions,
	})
	if err != nil {
		return Response{}, err
	}
	if int64(len(body)) > jevImageLimits.requestBytes {
		return Response{}, &LimitError{Check: "request_bytes", Image: -1, Actual: int64(len(body)), Max: jevImageLimits.requestBytes}
	}
	key, err := req.key()
	if err != nil {
		return Response{}, err
	}
	var got Response
	_, err = c.meter.Call(ctx, estimateJevPrice(req), "openrouter."+req.Model, key,
		func(ctx context.Context) (cost.Usage, error) {
			resp, usage, err := c.attempt(ctx, body)
			if err != nil {
				return cost.Usage{}, err
			}
			got = resp
			return usage, nil
		})
	if err != nil {
		return Response{}, err
	}
	return got, nil
}

func (c *OpenRouter) attempt(ctx context.Context, body []byte) (Response, cost.Usage, error) {
	var got Response
	var usage cost.Usage
	err := throttle.Retry(ctx, c.pacer, workersRetryable, func() error {
		resp, measured, err := c.post(ctx, body)
		if err != nil {
			return err
		}
		got, usage = resp, measured
		return nil
	})
	if err != nil {
		return Response{}, cost.Usage{}, err
	}
	return got, usage, nil
}

func (c *OpenRouter) post(ctx context.Context, body []byte) (Response, cost.Usage, error) {
	endpoint := c.base.String() + decisionsPath
	reply, err := postJSON(ctx, c.http, endpoint, c.token, body)
	if err != nil {
		return Response{}, cost.Usage{}, err
	}
	if reply.status != http.StatusOK {
		return Response{}, cost.Usage{}, reply.statusError(c.token)
	}
	resp, usage, err := decodeDecision(reply.body)
	if err != nil {
		return Response{}, cost.Usage{}, err
	}
	return resp, usage, nil
}

type decisionBody struct {
	Model     string              `json:"model"`
	State     json.RawMessage     `json:"state"`
	Questions map[string]Question `json:"questions"`
}

// jevState passes a text-only state through untouched, and wraps a call
// with images in the array form: the state as one text part, then one
// image_url part per image carrying the data URL.
func jevState(req Request) (json.RawMessage, error) {
	if len(req.Images) == 0 {
		return req.State, nil
	}
	parts := make([]statePart, 0, len(req.Images)+1)
	parts = append(parts, statePart{Type: "text", Text: string(req.State)})
	for _, img := range req.Images {
		dataURL := "data:" + img.ContentType + ";base64," + base64.StdEncoding.EncodeToString(img.Bytes)
		parts = append(parts, statePart{Type: "image_url", ImageURL: &imageURLPart{URL: dataURL}})
	}
	return json.Marshal(parts)
}

type statePart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *imageURLPart `json:"image_url,omitempty"`
}

type imageURLPart struct {
	URL string `json:"url"`
}

// decodeDecision unwraps the documented reply: answers keyed by question
// id and a usage that may report the call's cost in dollars.
func decodeDecision(body []byte) (Response, cost.Usage, error) {
	var decision struct {
		Model   string            `json:"model"`
		Answers map[string]Answer `json:"answers"`
		Usage   struct {
			InputTokens  int     `json:"input_tokens"`
			OutputTokens int     `json:"output_tokens"`
			Cost         float64 `json:"cost"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &decision); err != nil || decision.Model == "" || len(decision.Answers) == 0 {
		return Response{}, cost.Usage{}, &EnvelopeError{Reason: "reply is not a decision reply"}
	}
	for id, answer := range decision.Answers {
		if !answer.complete() {
			return Response{}, cost.Usage{}, &EnvelopeError{Reason: fmt.Sprintf("answer %q is incomplete", id)}
		}
	}
	usage := cost.Usage{}
	switch {
	case decision.Usage.Cost > 0 && decision.Usage.Cost <= maxMeasuredDollars:
		usage = cost.Usage{Price: cost.Price(math.Round(decision.Usage.Cost * 1e9)), Measured: true}
	case decision.Usage.InputTokens > 0:
		usage = cost.Usage{Price: cost.Price(decision.Usage.InputTokens) * nanodollarsPerJevInputToken, Measured: true}
	}
	return Response{
		Model:   decision.Model,
		Answers: decision.Answers,
		Usage:   Usage{InputTokens: decision.Usage.InputTokens, OutputTokens: decision.Usage.OutputTokens},
	}, usage, nil
}

// maxMeasuredDollars bounds the cost figure a reply may name before it is
// read as garbage and the token price stands in.
const maxMeasuredDollars = 1_000

// estimateJevPrice is the quote reserved before the call. The state and
// the questions count at four bytes a token, an image's data URL counts
// as the text it is sent as, and the sum is doubled so the reserve sits
// above the measured settle.
func estimateJevPrice(req Request) cost.Price {
	text := len(req.State)
	for _, q := range req.Questions {
		text += len(q.Instructions) + len(q.Criteria) + 32
	}
	for _, img := range req.Images {
		text += base64.StdEncoding.EncodedLen(len(img.Bytes)) + 64
	}
	tokens := int64(text/4) * 2
	if tokens > maxEstimateTokens {
		tokens = maxEstimateTokens
	}
	return cost.Price(tokens) * nanodollarsPerJevInputToken
}
