package youcam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nrynss/keel/throttle"
)

// The v2 REST host and call shape docs/youcam.md records. Every API call
// took about a second; the signed-URL legs (the PUT upload and the result
// fetch) took up to about two.
const (
	// defaultAPIBase is the India server T0.5 recorded against.
	defaultAPIBase = "https://yce-api-01.makeupar.com"

	defaultTimeout = 2 * time.Minute

	// maxResponseByteCount bounds one reply body. A result image is a few
	// hundred kilobytes; the bound is generous so a corrupt reply fails as
	// too large instead of eating memory.
	maxResponseByteCount = 16 << 20

	// maxNoteLen caps one provider note an error carries.
	maxNoteLen = 200

	// versionHair is the one task served outside v2.0. docs/youcam.md:
	// hair-transfer is called at /s2s/v2.1/, every other task at v2.0.
	versionHair = "v2.1"
)

// HTTPError is a reply with an HTTP status the call could not use: a
// bad parameter, an unknown task or id, a refused key. The provider's note
// is trimmed and never carries the key. A rate limit or a server fault is
// retryable; every other status is a refusal.
type HTTPError struct {
	Code int
	// RetryAfter is the wait a Retry-After header named, 0 when none. No
	// recorded YouCam reply carried one.
	RetryAfter time.Duration
	// Name is the provider's error_code, when the body named one.
	Name string
	Note string
}

func (e *HTTPError) Error() string {
	line := fmt.Sprintf("youcam: status %d", e.Code)
	if e.Name != "" {
		line += " " + e.Name
	}
	if e.Note != "" {
		line += ": " + e.Note
	}
	return line
}

// Retryable reports whether a wait can plausibly fix this status: a rate
// limit or a server fault. Every other 4xx is a refusal.
func (e *HTTPError) Retryable() bool { return e.Code == http.StatusTooManyRequests || e.Code >= 500 }

// EnvelopeError is an HTTP 200 whose body is not a usable reply. The
// provider's note is trimmed and never carries the key.
type EnvelopeError struct {
	Reason string
	Note   string
}

func (e *EnvelopeError) Error() string {
	if e.Note == "" {
		return "youcam: " + e.Reason
	}
	return fmt.Sprintf("youcam: %s: %s", e.Reason, e.Note)
}

// ClientConfig is how the client reaches YouCam. The key comes from the
// caller's resolved reference, never a file.
type ClientConfig struct {
	// BaseURL is the REST host. Empty means defaultAPIBase. A test points
	// it at its replay server.
	BaseURL string
	// Token is the YouCam API key. It is sent as the bearer on the API
	// calls alone (the signed-URL legs carry no auth) and is never written
	// into an error.
	Token string
	// HTTP is the client. Nil means one that does not follow redirects,
	// uses no ambient proxy, and times out after defaultTimeout.
	HTTP *http.Client
	// Throttle paces and retries each call. The client fills the transient
	// class and the Retry-After reader; the caller keeps the attempt
	// counts, the backoff, and any pacing. The zero value retries a
	// rate-limit refusal three times and a transient fault twice.
	Throttle throttle.Config
}

// Client is the YouCam v2 REST client. It implements YouCam. One Upload is
// a slot POST plus the PUT of the bytes; Create, Poll and Fetch are one
// call each. Safe for concurrent use. Tests point BaseURL at a replay and
// do not dial the provider.
type Client struct {
	base  url.URL
	token string
	http  *http.Client
	pacer throttle.Config
}

var _ YouCam = (*Client)(nil)

// NewClient returns a client for cfg. It does not dial.
func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.Token == "" {
		return nil, errors.New("youcam: api key is empty")
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultAPIBase
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return nil, errors.New("youcam: base url must be an http address")
	}
	if u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
		return nil, errors.New("youcam: base url must be a host, not a path or query")
	}
	httpClient := cfg.HTTP
	if httpClient == nil {
		httpClient = noRedirectHTTP(defaultTimeout)
	}
	pacer := cfg.Throttle
	pacer.Transient = transientFault
	pacer.RetryAfter = chainRetryAfter(cfg.Throttle.RetryAfter)
	return &Client{base: *u, token: cfg.Token, http: httpClient, pacer: pacer}, nil
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
			return errors.New("youcam: redirect refused")
		},
	}
}

// versionFor names the API version a task is served under.
func versionFor(task string) string {
	if task == "hair-transfer" {
		return versionHair
	}
	return "v2.0"
}

// fileExt is the name extension one content type uploads under. The slot
// validates nothing about the name; the extension keeps the bucket's
// objects readable by eye.
var fileExt = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// fileName is the file_name the slot request carries: the task and the
// extension of the content type. The recorded uploads named the sample
// photos; the name is not read by the engine.
func fileName(file File) string {
	bare, _, _ := strings.Cut(file.ContentType, ";")
	return file.Task + fileExt[strings.ToLower(strings.TrimSpace(bare))]
}

// slotBody is the File API request.
type slotBody struct {
	Files []slotFile `json:"files"`
}

type slotFile struct {
	ContentType string `json:"content_type"`
	FileName    string `json:"file_name"`
	FileSize    int    `json:"file_size"`
}

// Upload takes one upload slot, which spends nothing, and PUTs the bytes
// to the signed URL the slot names. The PUT carries no Authorization: the
// signed query string is the grant. A slot or PUT a wait can fix (a rate
// limit, a server fault, a transport timeout) is retried; a refusal comes
// back at once.
func (c *Client) Upload(ctx context.Context, file File) (Slot, error) {
	if err := ctx.Err(); err != nil {
		return Slot{}, err
	}
	if err := file.validate(); err != nil {
		return Slot{}, err
	}
	body, err := json.Marshal(slotBody{Files: []slotFile{{
		ContentType: file.ContentType,
		FileName:    fileName(file),
		FileSize:    len(file.Bytes),
	}}})
	if err != nil {
		return Slot{}, err
	}
	var replyBody []byte
	err = throttle.Retry(ctx, c.pacer, wireRetryable, func() error {
		reply, err := c.send(ctx, http.MethodPost, "/s2s/v2.0/file", body)
		if err != nil {
			return err
		}
		replyBody = reply.body
		if reply.status != http.StatusOK {
			return reply.statusError(c.token)
		}
		return nil
	})
	if err != nil {
		return Slot{}, err
	}
	slot, err := decodeSlot(replyBody, c.token)
	if err != nil {
		return Slot{}, err
	}
	if err := c.put(ctx, slot.putURL, slot.putType, file.Bytes); err != nil {
		return Slot{}, err
	}
	return Slot{FileID: slot.fileID, URL: slot.putURL}, nil
}

// putRequest is one upload the slot reply names. Exactly the two headers
// the slot carries go on the PUT.
type putRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

// slotReply is the File API reply.
type slotReply struct {
	Status int `json:"status"`
	Data   struct {
		Files []struct {
			FileID   string       `json:"file_id"`
			Requests []putRequest `json:"requests"`
		} `json:"files"`
	} `json:"data"`
	Error     string `json:"error"`
	ErrorCode string `json:"error_code"`
}

// uploadedSlot is the one slot a one-file upload names.
type uploadedSlot struct {
	fileID  string
	putURL  string
	putType string
}

func decodeSlot(body []byte, token string) (uploadedSlot, error) {
	var reply slotReply
	if err := json.Unmarshal(body, &reply); err != nil {
		return uploadedSlot{}, &EnvelopeError{Reason: "reply is not a youcam slot", Note: cleanNote(string(body), token)}
	}
	if reply.Status != http.StatusOK {
		return uploadedSlot{}, &EnvelopeError{
			Reason: fmt.Sprintf("the slot was refused with status %d", reply.Status),
			Note:   cleanNote(reply.Error, token),
		}
	}
	if len(reply.Data.Files) != 1 {
		return uploadedSlot{}, &EnvelopeError{Reason: "the slot holds no file"}
	}
	file := reply.Data.Files[0]
	if file.FileID == "" {
		return uploadedSlot{}, &EnvelopeError{Reason: "the slot holds no file id"}
	}
	for _, req := range file.Requests {
		if req.Method != http.MethodPut || req.URL == "" {
			continue
		}
		slot := uploadedSlot{fileID: file.FileID, putURL: req.URL}
		if t, ok := req.Headers["Content-Type"]; ok {
			slot.putType = t
		}
		return slot, nil
	}
	return uploadedSlot{}, &EnvelopeError{Reason: "the slot names no upload"}
}

// put sends the bytes to one signed URL. The Content-Type is the one the
// slot named, the recorded PUTs carried the file's own type; no
// Authorization, because the signed query is the grant.
func (c *Client) put(ctx context.Context, signedURL, contentType string, body []byte) error {
	u, err := url.Parse(signedURL)
	if err != nil || u.Host == "" {
		return errors.New("youcam: the slot names no usable upload url")
	}
	return throttle.Retry(ctx, c.pacer, wireRetryable, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, signedURL, bytes.NewReader(body))
		if err != nil {
			return errors.New("youcam: upload could not be built")
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return scrubSecretErr(err, c.token)
		}
		defer resp.Body.Close()
		if _, err := readBounded(resp.Body, maxResponseByteCount); err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return &HTTPError{Code: resp.StatusCode, Note: "the upload was refused"}
		}
		return nil
	})
}

// Create starts one task. The body is the task's payload unchanged. A
// refusal (an unknown task, a bad parameter, a bad file id) is a
// HTTPError and is not retried; the provider charges nothing for one.
func (c *Client) Create(ctx context.Context, task Task) (Created, error) {
	if err := ctx.Err(); err != nil {
		return Created{}, err
	}
	if err := task.validate(); err != nil {
		return Created{}, err
	}
	path := "/s2s/" + versionFor(task.Name) + "/task/" + task.Name
	var replyBody []byte
	err := throttle.Retry(ctx, c.pacer, wireRetryable, func() error {
		reply, err := c.send(ctx, http.MethodPost, path, []byte(task.Payload))
		if err != nil {
			return err
		}
		replyBody = reply.body
		if reply.status != http.StatusOK {
			return reply.statusError(c.token)
		}
		return nil
	})
	if err != nil {
		return Created{}, err
	}
	created, err := decodeCreated(replyBody, c.token)
	if err != nil {
		return Created{}, err
	}
	return Created{TaskID: created}, nil
}

// createdReply is the create reply.
type createdReply struct {
	Status int `json:"status"`
	Data   struct {
		TaskID string `json:"task_id"`
	} `json:"data"`
	Error     string `json:"error"`
	ErrorCode string `json:"error_code"`
}

func decodeCreated(body []byte, token string) (string, error) {
	var reply createdReply
	if err := json.Unmarshal(body, &reply); err != nil {
		return "", &EnvelopeError{Reason: "reply is not a youcam task", Note: cleanNote(string(body), token)}
	}
	if reply.Status != http.StatusOK {
		return "", &EnvelopeError{
			Reason: fmt.Sprintf("the task was refused with status %d", reply.Status),
			Note:   cleanNote(reply.Error, token),
		}
	}
	if reply.Data.TaskID == "" {
		return "", &EnvelopeError{Reason: "the reply holds no task id"}
	}
	return reply.Data.TaskID, nil
}

// pollReply is one poll. The engine's verdict rides inside an HTTP 200.
type pollReply struct {
	Status int `json:"status"`
	Data   struct {
		Error        *string      `json:"error"`
		ErrorMessage string       `json:"error_message"`
		Results      *pollResults `json:"results"`
		TaskStatus   string       `json:"task_status"`
	} `json:"data"`
	Error     string `json:"error"`
	ErrorCode string `json:"error_code"`
}

// pollResults is the results object. A render names a url; the analysis
// names faceshape among groups this client never reads. Only faceshape is
// decoded, and only it is ever carried: age, gender and the ratios are
// never read off the wire.
type pollResults struct {
	URL       string `json:"url"`
	FaceShape string `json:"faceshape"`
}

// engineKinds maps the provider's error codes onto the seam's kinds, as
// docs/youcam.md records them. A code outside the table stays a plain
// error that names the code.
var engineKinds = map[string]Kind{
	"error_no_face":              KindNoFace,
	"error_decode_image":         KindBadFile,
	"error_below_min_image_size": KindBadFile,
	"error_download_image":       KindExpired,
}

// Poll asks where a task stands. A finished render carries a fresh signed
// result link: polling the finished task again is free, so a lapsed link
// is recovered by polling again, never by re-creating. A provider verdict
// of error is the typed *Error, not a status.
func (c *Client) Poll(ctx context.Context, ref PollRef) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	if err := ref.validate(); err != nil {
		return Status{}, err
	}
	path := "/s2s/" + versionFor(ref.Task) + "/task/" + ref.Task + "/" + ref.TaskID
	var replyBody []byte
	err := throttle.Retry(ctx, c.pacer, wireRetryable, func() error {
		reply, err := c.send(ctx, http.MethodGet, path, nil)
		if err != nil {
			return err
		}
		replyBody = reply.body
		if reply.status != http.StatusOK {
			return reply.statusError(c.token)
		}
		return nil
	})
	if err != nil {
		return Status{}, err
	}
	return decodePoll(replyBody, ref.TaskID, c.token)
}

func decodePoll(body []byte, taskID, token string) (Status, error) {
	var reply pollReply
	if err := json.Unmarshal(body, &reply); err != nil {
		return Status{}, &EnvelopeError{Reason: "reply is not a youcam poll", Note: cleanNote(string(body), token)}
	}
	if reply.Status != http.StatusOK {
		return Status{}, &EnvelopeError{
			Reason: fmt.Sprintf("the poll was refused with status %d", reply.Status),
			Note:   cleanNote(reply.Error, token),
		}
	}
	status := Status{TaskID: taskID}
	switch reply.Data.TaskStatus {
	case "running":
		status.Status = StatusRunning
	case "success":
		status.Status = StatusSuccess
		if reply.Data.Results != nil {
			status.ResultURL = reply.Data.Results.URL
			status.FaceShape = reply.Data.Results.FaceShape
		}
	case "error":
		return Status{}, engineError(reply.Data.Error, reply.Data.ErrorMessage)
	default:
		return Status{}, &EnvelopeError{Reason: fmt.Sprintf("the poll names no known task status %q", reply.Data.TaskStatus)}
	}
	return status, nil
}

// engineError maps the provider's verdict onto the seam's kinds.
func engineError(code *string, message string) error {
	if code == nil || *code == "" {
		return &EnvelopeError{Reason: "the task failed without an error code"}
	}
	kind, ok := engineKinds[*code]
	if !ok {
		detail := *code
		if message != "" {
			detail += ": " + cleanNote(message, "")
		}
		return fmt.Errorf("youcam: task failed with %s", detail)
	}
	return &Error{Kind: kind, Detail: *code}
}

// Fetch copies the result bytes. The link is signed for two hours; a 403
// that names an expired grant is the typed ErrExpired, and a fresh link
// comes from polling the finished task again, which the task runner does.
// The bytes are returned as they came: PNG results arrive as
// binary/octet-stream under a .jpg name, so the caller sniffs the bytes
// and never trusts the header or the name.
func (c *Client) Fetch(ctx context.Context, ref ResultRef) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := ref.validate(); err != nil {
		return Result{}, err
	}
	var body []byte
	err := throttle.Retry(ctx, c.pacer, wireRetryable, func() error {
		got, err := c.get(ctx, ref.URL)
		if err != nil {
			return err
		}
		if got.status == http.StatusForbidden && bytes.Contains(got.body, []byte("AccessDenied")) {
			// A signed link that lived its two hours. Not retryable: no
			// wait fixes it, a fresh link does.
			return ErrExpired
		}
		if got.status != http.StatusOK {
			return &HTTPError{Code: got.status, Note: cleanNote(string(got.body), c.token)}
		}
		body = got.body
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return Result{TaskID: ref.TaskID, URL: ref.URL, Bytes: body}, nil
}

// get reads one signed URL with no Authorization.
func (c *Client) get(ctx context.Context, signedURL string) (payload, error) {
	u, err := url.Parse(signedURL)
	if err != nil || u.Host == "" {
		return payload{}, errors.New("youcam: the result link is not a usable url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signedURL, nil)
	if err != nil {
		return payload{}, errors.New("youcam: fetch could not be built")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return payload{}, scrubSecretErr(err, c.token)
	}
	defer resp.Body.Close()
	body, err := readBounded(resp.Body, maxResponseByteCount)
	if err != nil {
		return payload{}, err
	}
	return payload{status: resp.StatusCode, header: resp.Header, body: body}, nil
}

// payload is one HTTP answer with its body read and bounded.
type payload struct {
	status int
	header http.Header
	body   []byte
}

// send makes one API call under the bearer. A GET carries Accept alone;
// the recorded poll GETs sent no Content-Type.
func (c *Client) send(ctx context.Context, method, path string, body []byte) (payload, error) {
	endpoint := c.base.String() + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return payload{}, errors.New("youcam: request could not be built")
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return payload{}, scrubSecretErr(err, c.token)
	}
	defer resp.Body.Close()
	replyBody, err := readBounded(resp.Body, maxResponseByteCount)
	if err != nil {
		return payload{}, err
	}
	return payload{status: resp.StatusCode, header: resp.Header, body: replyBody}, nil
}

// statusError builds the HTTPError for a non-200 reply. The provider's
// note is trimmed short and scrubbed of the key.
func (r payload) statusError(token string) error {
	var body struct {
		Error     string `json:"error"`
		ErrorCode string `json:"error_code"`
	}
	note := ""
	name := ""
	if err := json.Unmarshal(r.body, &body); err == nil && body.Error != "" {
		note = body.Error
		name = body.ErrorCode
	}
	if note == "" {
		note = strings.TrimSpace(string(r.body))
	}
	return &HTTPError{
		Code:       r.status,
		RetryAfter: parseRetryAfter(r.header.Get("Retry-After")),
		Name:       name,
		Note:       cleanNote(note, token),
	}
}

// wireRetryable says which failures a wait can plausibly fix: a rate
// limit, a server fault, or a transport timeout. Every refusal, every
// engine verdict, and the caller's own deadline are final.
func wireRetryable(err error) bool {
	var status *HTTPError
	if errors.As(err, &status) {
		return status.Retryable()
	}
	var envelope *EnvelopeError
	if errors.As(err, &envelope) {
		return false
	}
	var ye *Error
	if errors.As(err, &ye) {
		return false
	}
	if errors.Is(err, ErrExpired) {
		return false
	}
	return isTimeout(err)
}

// transientFault claims the failures that are faults rather than
// refusals: a server error or a timeout. They draw the smaller transient
// attempt budget.
func transientFault(err error) bool {
	var status *HTTPError
	if errors.As(err, &status) {
		return status.Code >= 500
	}
	return isTimeout(err)
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
		var status *HTTPError
		if errors.As(err, &status) && status.RetryAfter > 0 {
			return status.RetryAfter, true
		}
		return 0, false
	}
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

// parseRetryAfter reads the seconds form. A date form or a negative is no
// hint; the backoff stands in.
func parseRetryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func readBounded(r io.Reader, n int64) ([]byte, error) {
	payload, err := io.ReadAll(io.LimitReader(r, n+1))
	if err != nil {
		return nil, fmt.Errorf("youcam: reply: %w", err)
	}
	if int64(len(payload)) > n {
		return nil, errors.New("youcam: reply is too large")
	}
	return payload, nil
}

// cleanNote scrubs the key out of provider text an error carries and
// trims it to the note length. A non-200 reply's note and an envelope's
// note both go through it, so no provider text reaches an error
// unscrubbed or unbounded.
func cleanNote(note, token string) string {
	if token != "" {
		note = strings.ReplaceAll(note, token, "redacted")
	}
	if len(note) > maxNoteLen {
		note = note[:maxNoteLen]
	}
	return note
}

// scrubSecretErr replaces the key in a transport error's text.
func scrubSecretErr(err error, secret string) error {
	if err == nil || secret == "" || !strings.Contains(err.Error(), secret) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), secret, "redacted"))
}
