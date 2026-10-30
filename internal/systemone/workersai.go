package systemone

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers the JPEG decoder for image.DecodeConfig
	_ "image/png"  // registers the PNG decoder for image.DecodeConfig
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/throttle"
)

// Clef's limits, as docs/clef.md states them. An image over any of them is
// refused before any call: nothing oversize reaches the wire.
const (
	// maxImageBytes is the encoded size of one image on the wire.
	maxImageBytes = 4 << 20 // 4 MiB
	// maxTotalImageBytes is the decoded size of all images in one call.
	maxTotalImageBytes = 8 << 20 // 8 MiB
	// maxRequestBodyBytes is the whole request body, images' base64 included.
	maxRequestBodyBytes = 13 << 20 // 13 MiB
	// maxImagePixels is the pixel count of one image, 16 megapixels.
	maxImagePixels = 16_000_000

	// maxImageInputTokens is the most input tokens one image can count
	// under Clef's documented tokenizer (docs/clef.md, Cost).
	maxImageInputTokens = 1_024

	// defaultAPIBase is Workers AI's REST host.
	defaultAPIBase = "https://api.cloudflare.com"

	defaultTimeout       = 2 * time.Minute
	maxResponseByteCount = 4 << 20
)

// The rate card. Workers AI bills neurons, $0.011 per 1,000 neurons, and
// prices each model per million input tokens. docs/clef.md measured these
// two rates on the sample set in T0.6; the response reports the input
// tokens and the client prices them at the model's rate.
const (
	nanodollarsPerNeuron = 11_000
	maxEstimateTokens    = 20_000_000
)

// neuronsPerMillionInputTokens is the neuron rate per model. A model
// outside this table is not one this seam runs on Workers AI.
var neuronsPerMillionInputTokens = map[string]int64{
	ModelClef:      21_818,
	ModelClefFlash: 3_455,
}

// LimitError is an image or a request body the provider would refuse.
// Ask refuses it before any call and before any reserve.
type LimitError struct {
	// Check names the limit: image_bytes, image_pixels, image_unreadable,
	// total_image_bytes, or request_bytes.
	Check string
	// Image is the 0-based index of the offending image, or -1 when the
	// check is over the whole request.
	Image  int
	Actual int64
	Max    int64
}

func (e *LimitError) Error() string {
	where := "request"
	if e.Image >= 0 {
		where = fmt.Sprintf("image %d", e.Image)
	}
	if e.Check == "image_unreadable" {
		return fmt.Sprintf("systemone: %s: the header holds no readable size", where)
	}
	return fmt.Sprintf("systemone: %s: %s is %d, over %d", where, e.Check, e.Actual, e.Max)
}

// StatusError is a reply with a status the call could not use. The
// provider's note is trimmed and never carries the key.
type StatusError struct {
	Code int
	// RetryAfter is the wait a Retry-After header named, 0 when none.
	RetryAfter time.Duration
	Note       string
}

func (e *StatusError) Error() string {
	if e.Note == "" {
		return fmt.Sprintf("systemone: status %d", e.Code)
	}
	return fmt.Sprintf("systemone: status %d: %s", e.Code, e.Note)
}

// Retryable reports whether a wait can plausibly fix this status: a rate
// limit or a server fault. Every other 4xx is a refusal.
func (e *StatusError) Retryable() bool { return statusRetryable(e.Code) }

func statusRetryable(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

// EnvelopeError is an HTTP 200 whose body is not a usable result: the
// Cloudflare envelope reported failure, or the answers are incomplete.
type EnvelopeError struct {
	Reason string
	Notes  []string
}

func (e *EnvelopeError) Error() string {
	if len(e.Notes) == 0 {
		return "systemone: " + e.Reason
	}
	return fmt.Sprintf("systemone: %s: %s", e.Reason, strings.Join(e.Notes, "; "))
}

// WorkersAIConfig is how the client reaches Workers AI. The token and the
// account id come from the caller's resolved references, never a file.
type WorkersAIConfig struct {
	// BaseURL is the REST host. Empty means defaultAPIBase. A test points
	// it at its replay server.
	BaseURL string
	// AccountID is the CLOUDFLARE_ACCOUNT_ID value. It is a URL path
	// segment and must not carry a slash or whitespace.
	AccountID string
	// Token is the WORKERS_AI_API_TOKEN value. It is sent as the bearer
	// and is never written into an error.
	Token string
	// Meter meters every Ask. Nil is refused: a call without a meter
	// would spend outside the budget.
	Meter *cost.Meter
	// HTTP is the client. Nil means one that does not follow redirects,
	// uses no ambient proxy, and times out after defaultTimeout.
	HTTP *http.Client
	// Throttle paces and retries the call. The client fills the transient
	// class and the Retry-After reader; the caller keeps the attempt
	// counts, the backoff, and any pacing. The zero value retries a
	// rate-limit refusal three times and a transient fault twice.
	Throttle throttle.Config
}

// WorkersAI is the System One client for Workers AI's Clef and Clef-flash.
// One Ask is one metered call: quoted, reserved, and settled once. Safe
// for concurrent use. Tests point BaseURL at a replay and do not dial.
type WorkersAI struct {
	base    url.URL
	account string
	token   string
	meter   *cost.Meter
	http    *http.Client
	pacer   throttle.Config
}

var _ SystemOne = (*WorkersAI)(nil)

// NewWorkersAI returns a client for cfg. It does not dial.
func NewWorkersAI(cfg WorkersAIConfig) (*WorkersAI, error) {
	if cfg.AccountID == "" {
		return nil, errors.New("systemone: workers ai account id is empty")
	}
	if strings.ContainsAny(cfg.AccountID, "/ \t\r\n") {
		return nil, errors.New("systemone: workers ai account id is not one path segment")
	}
	if cfg.Token == "" {
		return nil, errors.New("systemone: workers ai token is empty")
	}
	if cfg.Meter == nil {
		return nil, errors.New("systemone: workers ai meter is nil")
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultAPIBase
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return nil, errors.New("systemone: workers ai base url must be an http address")
	}
	if u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
		return nil, errors.New("systemone: workers ai base url must be a host, not a path or query")
	}
	httpClient := cfg.HTTP
	if httpClient == nil {
		httpClient = noRedirectHTTP(defaultTimeout)
	}
	pacer := cfg.Throttle
	pacer.Transient = transientFault
	pacer.RetryAfter = chainRetryAfter(cfg.Throttle.RetryAfter)
	return &WorkersAI{
		base:    *u,
		account: cfg.AccountID,
		token:   cfg.Token,
		meter:   cfg.Meter,
		http:    httpClient,
		pacer:   pacer,
	}, nil
}

// runPath is the path segment the recordings store, after the account id
// the URL carries and they leave out.
func runPath(model string) string {
	return "/ai/run/@cf/cloudflare/" + model
}

// noRedirectHTTP is the default client: no ambient proxy, a redirect
// refused so the bearer stays on the one request, and a timeout above any
// recorded call.
func noRedirectHTTP(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("systemone: redirect refused")
		},
	}
}

// Ask makes one decision call. The images are checked against Clef's
// limits first, so an oversize or unreadable image is refused typed with
// no request sent and nothing reserved. The call is then metered: the
// estimate is reserved, the POST is retried through the throttle for a
// rate limit or a transient fault only, and the settled price is the
// input tokens the response reports priced at the model's rate. A refused
// attempt is not billed by the provider, so the call settles once.
func (c *WorkersAI) Ask(ctx context.Context, req Request) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	if err := req.Validate(); err != nil {
		return Response{}, err
	}
	perMillion, ok := neuronsPerMillionInputTokens[req.Model]
	if !ok {
		return Response{}, fmt.Errorf("systemone: workers ai does not run model %q", req.Model)
	}
	if err := checkImages(req.Images, clefImageLimits); err != nil {
		return Response{}, err
	}
	body, err := json.Marshal(workersBody{
		Model:     req.Model,
		Images:    req.Images,
		State:     req.State,
		Questions: req.Questions,
	})
	if err != nil {
		return Response{}, err
	}
	if int64(len(body)) > maxRequestBodyBytes {
		return Response{}, &LimitError{Check: "request_bytes", Image: -1, Actual: int64(len(body)), Max: maxRequestBodyBytes}
	}
	key, err := req.key()
	if err != nil {
		return Response{}, err
	}
	var got Response
	_, err = c.meter.Call(ctx, estimateWorkersPrice(req, perMillion), "workersai."+req.Model, key,
		func(ctx context.Context) (cost.Usage, error) {
			resp, tokens, err := c.attempt(ctx, req.Model, body)
			if err != nil {
				return cost.Usage{}, err
			}
			got = resp
			if tokens <= 0 {
				// The response named no count, so the reserve stands.
				return cost.Usage{}, nil
			}
			return cost.Usage{Price: tokenPrice(tokens, perMillion), Measured: true}, nil
		})
	if err != nil {
		return Response{}, err
	}
	return got, nil
}

// attempt runs the POST through the throttle. A status a wait can fix
// (429, 5xx) or a transport timeout is retried; a refusal, an envelope
// the model failed, and a body that does not decode come back at once.
func (c *WorkersAI) attempt(ctx context.Context, model string, body []byte) (Response, int64, error) {
	var got Response
	var inputTokens int64
	err := throttle.Retry(ctx, c.pacer, workersRetryable, func() error {
		resp, tokens, err := c.post(ctx, model, body)
		if err != nil {
			return err
		}
		got, inputTokens = resp, tokens
		return nil
	})
	if err != nil {
		return Response{}, 0, err
	}
	return got, inputTokens, nil
}

func (c *WorkersAI) post(ctx context.Context, model string, body []byte) (Response, int64, error) {
	reply, err := c.send(ctx, runPath(model), body)
	if err != nil {
		return Response{}, 0, err
	}
	if reply.status != http.StatusOK {
		return Response{}, 0, reply.statusError(c.token)
	}
	resp, tokens, err := decodeClefResult(reply.body)
	if err != nil {
		return Response{}, 0, err
	}
	return resp, tokens, nil
}

func (c *WorkersAI) send(ctx context.Context, path string, body []byte) (reply, error) {
	endpoint := c.base.String() + "/client/v4/accounts/" + c.account + path
	return postJSON(ctx, c.http, endpoint, c.token, body)
}

type workersBody struct {
	Model     string              `json:"model"`
	Images    []Image             `json:"images,omitempty"`
	State     json.RawMessage     `json:"state"`
	Questions map[string]Question `json:"questions"`
}

// decodeClefResult unwraps Cloudflare's envelope into the answers. The
// recorded shape is {"result": {model, answers, usage}, "success": true}.
func decodeClefResult(body []byte) (Response, int64, error) {
	var envelope struct {
		Result  *json.RawMessage `json:"result"`
		Success *bool            `json:"success"`
		Errors  []providerNote   `json:"errors"`
	}
	var notes []string
	if err := json.Unmarshal(body, &envelope); err != nil || (envelope.Result == nil && envelope.Success == nil) {
		return Response{}, 0, &EnvelopeError{Reason: "reply is not a workers ai envelope"}
	}
	for _, note := range envelope.Errors {
		notes = append(notes, note.Message)
	}
	if envelope.Success == nil || !*envelope.Success {
		return Response{}, 0, &EnvelopeError{Reason: "the call was refused", Notes: notes}
	}
	if envelope.Result == nil {
		return Response{}, 0, &EnvelopeError{Reason: "the result holds no answers", Notes: notes}
	}
	var resp Response
	if err := json.Unmarshal(*envelope.Result, &resp); err != nil || resp.Model == "" || len(resp.Answers) == 0 {
		return Response{}, 0, &EnvelopeError{Reason: "the result holds no answers", Notes: notes}
	}
	for id, answer := range resp.Answers {
		if !answer.complete() {
			return Response{}, 0, &EnvelopeError{
				Reason: fmt.Sprintf("answer %q is incomplete", id),
				Notes:  notes,
			}
		}
	}
	return resp, int64(resp.Usage.InputTokens), nil
}

// estimateWorkersPrice is the quote reserved before the call. Text counts
// at four bytes a token, each image at its documented token cap, and the
// sum is doubled so the reserve sits above the measured settle.
func estimateWorkersPrice(req Request, perMillion int64) cost.Price {
	text := len(req.State)
	for _, q := range req.Questions {
		text += len(q.Instructions) + len(q.Criteria) + 32
	}
	tokens := int64(text/4+len(req.Images)*maxImageInputTokens) * 2
	if tokens > maxEstimateTokens {
		tokens = maxEstimateTokens
	}
	return tokenPrice(tokens, perMillion)
}

// tokenPrice prices input tokens in nanodollars, rounded up. The count is
// capped, so a corrupt reply cannot wrap the product.
func tokenPrice(tokens, neuronsPerMillion int64) cost.Price {
	if tokens > maxEstimateTokens {
		tokens = maxEstimateTokens
	}
	const scale = 1_000_000
	product := tokens * neuronsPerMillion * nanodollarsPerNeuron
	return cost.Price((product + scale - 1) / scale)
}

// imageLimits are the guards one client applies before any call. Clef's
// are the limits docs/clef.md states. The OpenRouter endpoint keeps the
// byte guards until the comparison task records Jev's own.
type imageLimits struct {
	perImageBytes int64
	totalBytes    int64
	requestBytes  int64
	maxPixels     int64
	checkPixels   bool
}

var clefImageLimits = imageLimits{
	perImageBytes: maxImageBytes,
	totalBytes:    maxTotalImageBytes,
	requestBytes:  maxRequestBodyBytes,
	maxPixels:     maxImagePixels,
	checkPixels:   true,
}

var jevImageLimits = imageLimits{
	perImageBytes: maxImageBytes,
	totalBytes:    maxTotalImageBytes,
	requestBytes:  maxRequestBodyBytes,
}

// checkImages refuses an oversize or unreadable image before any call.
func checkImages(images []Image, lim imageLimits) error {
	var total int64
	for i, img := range images {
		if int64(len(img.Bytes)) > lim.perImageBytes {
			return &LimitError{Check: "image_bytes", Image: i, Actual: int64(len(img.Bytes)), Max: lim.perImageBytes}
		}
		if lim.checkPixels {
			w, h, err := imageDimensions(img.Bytes)
			if err != nil {
				return &LimitError{Check: "image_unreadable", Image: i}
			}
			pixels := int64(w) * int64(h)
			if pixels > lim.maxPixels {
				return &LimitError{Check: "image_pixels", Image: i, Actual: pixels, Max: lim.maxPixels}
			}
		}
		total += int64(len(img.Bytes))
		if total > lim.totalBytes {
			return &LimitError{Check: "total_image_bytes", Image: -1, Actual: total, Max: lim.totalBytes}
		}
	}
	return nil
}

// imageDimensions reads an image's pixel size from its header alone. The
// photo pipeline has already applied EXIF orientation, so these are the
// displayed dimensions. A file whose header does not name a size is
// refused: Clef could not decode it either.
func imageDimensions(data []byte) (int, int, error) {
	w, h, err := decodeDimensions(data)
	if err != nil || w <= 0 || h <= 0 {
		return 0, 0, errors.New("systemone: image header holds no readable size")
	}
	return w, h, nil
}

func decodeDimensions(data []byte) (int, int, error) {
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF,
		len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return 0, 0, err
		}
		return config.Width, config.Height, nil
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return webpDimensions(data)
	default:
		return 0, 0, errors.New("systemone: image is not a png, jpeg, or webp file")
	}
}

// webpDimensions reads the canvas size from the first chunk: VP8X carries
// it in its 24-bit fields, VP8 in its 14-bit frame header, VP8L in its
// packed 14-bit header. The WebP container spec fixes every offset.
func webpDimensions(data []byte) (int, int, error) {
	if len(data) < 30 {
		return 0, 0, errors.New("systemone: webp file is too short")
	}
	chunk := string(data[12:16])
	body := data[20:]
	switch chunk {
	case "VP8X":
		return int(uint32(body[4])|uint32(body[5])<<8|uint32(body[6])<<16) + 1,
			int(uint32(body[7])|uint32(body[8])<<8|uint32(body[9])<<16) + 1,
			nil
	case "VP8 ":
		if len(body) < 10 || body[3] != 0x9D || body[4] != 0x01 || body[5] != 0x2A {
			return 0, 0, errors.New("systemone: webp lossy frame has no start code")
		}
		return int(binary.LittleEndian.Uint16(body[6:8])) & 0x3FFF,
			int(binary.LittleEndian.Uint16(body[8:10])) & 0x3FFF,
			nil
	case "VP8L":
		if len(body) < 5 || body[0] != 0x2F {
			return 0, 0, errors.New("systemone: webp lossless frame has no signature")
		}
		bits := binary.LittleEndian.Uint32(body[1:5])
		return int(bits&0x3FFF) + 1, int((bits>>14)&0x3FFF) + 1, nil
	default:
		return 0, 0, fmt.Errorf("systemone: webp first chunk is %q", chunk)
	}
}

// workersRetryable says which failures a wait can plausibly fix: a rate
// limit, a server fault, or a transport timeout. Every other refusal,
// every envelope failure, and the caller's own deadline are final.
func workersRetryable(err error) bool {
	var status *StatusError
	if errors.As(err, &status) {
		return status.Retryable()
	}
	return isTimeout(err)
}

// transientFault claims the failures that are faults rather than
// refusals: a server error or a timeout. They draw the smaller transient
// attempt budget, because every further try is another paid submission.
func transientFault(err error) bool {
	var status *StatusError
	if errors.As(err, &status) {
		return status.Code >= 500
	}
	return isTimeout(err)
}

func isTimeout(err error) bool {
	if err == nil ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// chainRetryAfter joins the caller's hint reader with this package's: a
// status the provider gave a Retry-After header names its own wait.
func chainRetryAfter(caller func(error) (time.Duration, bool)) func(error) (time.Duration, bool) {
	return func(err error) (time.Duration, bool) {
		if caller != nil {
			if d, ok := caller(err); ok {
				return d, true
			}
		}
		var status *StatusError
		if errors.As(err, &status) && status.RetryAfter > 0 {
			return status.RetryAfter, true
		}
		return 0, false
	}
}

// reply is one HTTP answer with its body read and bounded.
type reply struct {
	status int
	header http.Header
	body   []byte
}

// statusError builds the StatusError for a non-200 reply. The provider's
// note is trimmed short and scrubbed of the key.
func (r reply) statusError(token string) error {
	var envelope struct {
		Errors []providerNote `json:"errors"`
	}
	var note string
	if err := json.Unmarshal(r.body, &envelope); err == nil {
		var parts []string
		for _, e := range envelope.Errors {
			parts = append(parts, e.Message)
		}
		note = strings.Join(parts, "; ")
	}
	if note == "" {
		note = strings.TrimSpace(string(r.body))
	}
	note = scrubSecret(note, token)
	if len(note) > 200 {
		note = note[:200]
	}
	return &StatusError{
		Code:       r.status,
		RetryAfter: parseRetryAfter(r.header.Get("Retry-After")),
		Note:       note,
	}
}

type providerNote struct {
	Code    any    `json:"code"`
	Message string `json:"message"`
}

// parseRetryAfter reads the seconds form. A date form or a negative is no
// hint; the backoff stands in.
func parseRetryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// postJSON sends one authenticated POST and reads the reply. It follows
// no redirect, reads at most maxResponseByteCount, and never returns the
// key inside an error.
func postJSON(ctx context.Context, client *http.Client, endpoint, token string, body []byte) (reply, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return reply{}, errors.New("systemone: request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return reply{}, scrubSecretErr(err, token)
	}
	defer resp.Body.Close()
	payload, err := readBounded(resp.Body, maxResponseByteCount)
	if err != nil {
		return reply{}, err
	}
	return reply{status: resp.StatusCode, header: resp.Header, body: payload}, nil
}

func readBounded(r io.Reader, n int64) ([]byte, error) {
	payload, err := io.ReadAll(io.LimitReader(r, n+1))
	if err != nil {
		return nil, fmt.Errorf("systemone: reply: %w", err)
	}
	if int64(len(payload)) > n {
		return nil, errors.New("systemone: reply is too large")
	}
	return payload, nil
}

// scrubSecret replaces the key in text an error carries.
func scrubSecret(text, secret string) string {
	if secret == "" || !strings.Contains(text, secret) {
		return text
	}
	return strings.ReplaceAll(text, secret, "redacted")
}

func scrubSecretErr(err error, secret string) error {
	if err == nil || secret == "" || !strings.Contains(err.Error(), secret) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), secret, "redacted"))
}
