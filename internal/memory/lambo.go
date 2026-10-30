package memory

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/nrynss/keel/erase"
)

const (
	// DefaultAgentID is the id sent on every tool call when Config does
	// not set one. One process keeps one id.
	DefaultAgentID = "dresscode"

	protocolVersion = "2025-06-18"
	clientName      = "dresscode"
	clientVersion   = "0.0.0"

	// defaultWait is the lambo_stats wait_ms the probe used.
	defaultWait = 30 * time.Second
	maxWait     = 24 * time.Hour

	maxResponse     = 4 << 20
	recallTopK      = 10
	recallMaxTokens = 2000

	// TargetName is the erase target history deletion and account
	// deletion register. One erasure has one memory target.
	TargetName = "memory"
)

// ErrUnauthorized is a Lambo response that refused the bearer token.
// The token is not in the error.
var ErrUnauthorized = errors.New("memory: lambo refused the token")

// ErrUnconfirmed is a remember whose write was sent and whose receipt
// did not report applied. The look was not remembered. The write is not
// sent again: Lambo does not de-duplicate it.
var ErrUnconfirmed = errors.New("memory: remember was not confirmed")

// ErrSessionExpired is a call on an Mcp-Session-Id the serve no longer
// knows. Lambo expires idle sessions in minutes, and the reply is an
// HTTP 404 with plain text, not a JSON-RPC error. The client initialized
// the session again and replayed the call once; the replay was refused
// too. The call is not attempted a third time. It stays this error out
// of Remember as well: a derive refused twice was never sent, which is
// not a lost confirmation.
var ErrSessionExpired = errors.New("memory: lambo session expired")

// Config is how the client reaches one Lambo serve.
type Config struct {
	// BaseURL is the serve, the LAMBO_URL host, with no path and no
	// credentials. Requests go to /mcp/s/<session> and /admin/s/<session>/erase.
	BaseURL string

	// Token is the bearer token. It is sent on every request and is
	// never written into an error.
	Token string

	// AgentID is the stable id sent on every tool call. Empty means
	// DefaultAgentID.
	AgentID string

	// Wait is how long a remember asks lambo_stats to block, in
	// wait_ms. Zero means defaultWait. The client does not sleep that
	// long itself. The serve blocks.
	Wait time.Duration

	// HTTP is the client. Nil means a client that does not follow
	// redirects, does not use an ambient proxy, and whose timeout is
	// longer than Wait. A redirect would carry the bearer token.
	HTTP *http.Client
}

// Client talks to one Lambo serve over MCP streamable HTTP. It keeps one
// MCP session per Dresscode session. The calls are the ones in
// docs/lambo.md. Safe for concurrent use. Each session's calls run one
// at a time. A session the serve expired between calls is initialized
// again and the call replayed once. Tests point BaseURL at a replay and
// do not dial a serve.
type Client struct {
	base    url.URL
	token   string
	agentID string
	waitMS  int
	http    *http.Client

	mu     sync.Mutex
	gates  map[string]*sessionGate
	nextID int
}

// sessionGate is the MCP session for one Dresscode session.
type sessionGate struct {
	mu    sync.Mutex
	mcpID string
	ready bool
}

var (
	_ Memory       = (*Client)(nil)
	_ erase.Target = sessionTarget{}
)

// New returns a client for cfg. It does not dial.
func New(cfg Config) (*Client, error) {
	if cfg.Token == "" {
		return nil, errors.New("memory: lambo token is empty")
	}
	if strings.Contains(cfg.BaseURL, cfg.Token) {
		return nil, errors.New("memory: lambo url must not carry the token")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return nil, errors.New("memory: lambo url must be an http address")
	}
	if u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
		return nil, errors.New("memory: lambo url must be a host, not a path or query")
	}
	u.Path = ""
	wait := cfg.Wait
	if wait == 0 {
		wait = defaultWait
	}
	if wait < time.Millisecond || wait > maxWait {
		return nil, errors.New("memory: lambo wait is out of range")
	}
	agent := cfg.AgentID
	if agent == "" {
		agent = DefaultAgentID
	}
	if strings.ContainsAny(agent, "\r\n") {
		return nil, errors.New("memory: lambo agent id is not one line")
	}
	httpClient := cfg.HTTP
	if httpClient == nil {
		httpClient = defaultHTTP(wait)
	}
	return &Client{
		base:    *u,
		token:   cfg.Token,
		agentID: agent,
		waitMS:  int(wait / time.Millisecond),
		http:    httpClient,
		gates:   map[string]*sessionGate{},
	}, nil
}

func defaultHTTP(wait time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{
		Timeout:   wait + 30*time.Second,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("memory: lambo redirect refused")
		},
	}
}

// Remember sends one lambo_derive for the concepts. The write is not
// retried. The result is returned only after lambo_stats reports that
// receipt applied. ErrUnconfirmed means the write was sent and not
// confirmed. A session the serve expired is initialized again and the
// current step replayed once; a second refusal is ErrSessionExpired.
func (c *Client) Remember(ctx context.Context, session string, concepts []Concept) (Remembered, error) {
	if err := ctx.Err(); err != nil {
		return Remembered{}, err
	}
	if err := checkSession(session); err != nil {
		return Remembered{}, err
	}
	if err := validateConcepts(concepts); err != nil {
		return Remembered{}, err
	}
	g := c.gate(session)
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Remembered{}, err
	}
	if err := c.open(ctx, g, session); err != nil {
		return Remembered{}, err
	}
	pending, err := c.derive(ctx, g, session, concepts)
	if err != nil {
		return Remembered{}, err
	}
	return c.confirm(ctx, g, session, pending)
}

// Recall asks the session for the occasion text. The serve returns the
// nearest concepts in that session. It does not filter to the occasion.
// An erased session is ErrErased, not an empty hit list, and the call is
// not retried. A session the serve expired is initialized again and the
// recall replayed once; a second refusal is ErrSessionExpired.
func (c *Client) Recall(ctx context.Context, session, occasion string) (Recall, error) {
	if err := ctx.Err(); err != nil {
		return Recall{}, err
	}
	if err := checkSession(session); err != nil {
		return Recall{}, err
	}
	if occasion == "" {
		return Recall{}, errors.New("memory: occasion is empty")
	}
	g := c.gate(session)
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Recall{}, err
	}
	if err := c.open(ctx, g, session); err != nil {
		return Recall{}, err
	}
	msg, err := c.tool(ctx, g, session, "lambo_recall", recallArgs{
		AgentID:   c.agentID,
		Query:     occasion,
		TopK:      recallTopK,
		MaxTokens: recallMaxTokens,
	})
	if err != nil {
		return Recall{}, err
	}
	return recallFrom(msg)
}

// Erase deletes the session. A second call is safe. AlreadyAbsent is the
// gone report: the session was already absent, and every removed count is
// from the serve. Erase does not forget the MCP session id. The next call
// on it learns the id is gone and is not retried.
func (c *Client) Erase(ctx context.Context, session string) (EraseResult, error) {
	if err := ctx.Err(); err != nil {
		return EraseResult{}, err
	}
	if err := checkSession(session); err != nil {
		return EraseResult{}, err
	}
	g := c.gate(session)
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return EraseResult{}, err
	}
	return c.erase(ctx, session)
}

// Target is the session as a Keel erase target. Its name is TargetName.
// Delete calls Erase. A session that is already gone returns erase.ErrGone,
// which an erasure counts as confirmed. Delete is safe to call again.
func (c *Client) Target(session string) erase.Target {
	return sessionTarget{client: c, session: session}
}

type sessionTarget struct {
	client  *Client
	session string
}

func (t sessionTarget) Name() string { return TargetName }

func (t sessionTarget) Delete(ctx context.Context) error {
	result, err := t.client.Erase(ctx, t.session)
	if err != nil {
		return err
	}
	if result.AlreadyAbsent {
		return fmt.Errorf("memory: session already gone: %w", erase.ErrGone)
	}
	return nil
}

func (c *Client) gate(session string) *sessionGate {
	c.mu.Lock()
	defer c.mu.Unlock()
	g := c.gates[session]
	if g == nil {
		g = &sessionGate{}
		c.gates[session] = g
	}
	return g
}

func (g *sessionGate) drop() {
	g.mcpID = ""
	g.ready = false
}

func (c *Client) open(ctx context.Context, g *sessionGate, session string) error {
	if g.mcpID != "" && g.ready {
		return nil
	}
	if g.mcpID == "" {
		mcp, err := c.initialize(ctx, session)
		if err != nil {
			g.drop()
			return err
		}
		g.mcpID = mcp
	}
	if err := c.notify(ctx, session, g.mcpID); err != nil {
		return err
	}
	g.ready = true
	return nil
}

func (c *Client) initialize(ctx context.Context, session string) (string, error) {
	msg, mcp, err := c.post(ctx, mcpPath(session), rpcCall{
		JSONRPC: "2.0",
		ID:      c.allocID(),
		Method:  "initialize",
		Params: initParams{
			ProtocolVersion: protocolVersion,
			Capabilities:    map[string]any{},
			ClientInfo:      clientInfo{Name: clientName, Version: clientVersion},
		},
	}, "")
	if err != nil {
		return "", err
	}
	if mcp == "" {
		return "", errors.New("memory: lambo initialize returned no session")
	}
	var result struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if json.Unmarshal(msg, &result) != nil || result.Result.ProtocolVersion != protocolVersion {
		return "", errors.New("memory: lambo initialize did not accept the protocol")
	}
	return mcp, nil
}

func (c *Client) notify(ctx context.Context, session, mcp string) error {
	_, _, err := c.post(ctx, mcpPath(session), rpcCall{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}, mcp)
	return err
}

func (c *Client) derive(ctx context.Context, g *sessionGate, session string, concepts []Concept) (Remembered, error) {
	msg, err := c.tool(ctx, g, session, "lambo_derive", deriveArgs{
		AgentID:  c.agentID,
		Concepts: concepts,
	})
	if err != nil {
		if errors.Is(err, ErrErased) || errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrSessionExpired) {
			return Remembered{}, err
		}
		return Remembered{}, unconfirmed(err)
	}
	sc, err := toolContent(msg)
	if err != nil {
		return Remembered{}, unconfirmed(err)
	}
	var body struct {
		AgentID      string   `json:"agent_id"`
		Receipt      string   `json:"receipt"`
		ReceiptState string   `json:"receipt_state"`
		Summary      string   `json:"summary"`
		Warnings     []string `json:"warnings"`
	}
	if json.Unmarshal(sc, &body) != nil || body.Receipt == "" {
		return Remembered{}, ErrUnconfirmed
	}
	return Remembered{
		Receipt:  body.Receipt,
		State:    body.ReceiptState,
		Summary:  body.Summary,
		AgentID:  body.AgentID,
		Warnings: body.Warnings,
	}, nil
}

func (c *Client) confirm(ctx context.Context, g *sessionGate, session string, pending Remembered) (Remembered, error) {
	msg, err := c.tool(ctx, g, session, "lambo_stats", statsArgs{
		AgentID: c.agentID,
		Receipt: pending.Receipt,
		WaitMS:  c.waitMS,
	})
	if err != nil {
		if errors.Is(err, ErrErased) || errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrSessionExpired) {
			return Remembered{}, err
		}
		return Remembered{}, unconfirmed(err)
	}
	sc, err := toolContent(msg)
	if err != nil {
		return Remembered{}, unconfirmed(err)
	}
	var body struct {
		Receipt struct {
			ID           string   `json:"id"`
			State        string   `json:"state"`
			Detail       string   `json:"detail"`
			Created      []string `json:"created"`
			CreatedCount int      `json:"created_count"`
			Embedded     int      `json:"embedded"`
		} `json:"receipt"`
		Warnings []string `json:"warnings"`
	}
	if json.Unmarshal(sc, &body) != nil {
		return Remembered{}, ErrUnconfirmed
	}
	if body.Receipt.ID != pending.Receipt || body.Receipt.State != "applied" {
		return Remembered{}, ErrUnconfirmed
	}
	out := pending
	out.State = body.Receipt.State
	if body.Receipt.Detail != "" {
		out.Summary = body.Receipt.Detail
	}
	out.Created = body.Receipt.Created
	out.CreatedCount = body.Receipt.CreatedCount
	out.Embedded = body.Receipt.Embedded
	if body.Warnings != nil {
		out.Warnings = body.Warnings
	}
	return out, nil
}

func (c *Client) erase(ctx context.Context, session string) (EraseResult, error) {
	raw, err := json.Marshal(eraseRequest{Confirm: session})
	if err != nil {
		return EraseResult{}, err
	}
	req, err := c.newRequest(ctx, erasePath(session), raw, "")
	if err != nil {
		return EraseResult{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return EraseResult{}, c.scrub(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return EraseResult{}, ErrUnauthorized
	}
	body, err := readLimit(resp.Body, maxResponse)
	if err != nil {
		return EraseResult{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return EraseResult{}, c.statusError(resp.StatusCode, body)
	}
	var result EraseResult
	if json.Unmarshal(body, &result) != nil || result.Session != session {
		return EraseResult{}, errors.New("memory: lambo erase response is not this session")
	}
	return result, nil
}

// tool sends one tools/call on the session. A 404 on the session id is
// an idle session the serve expired: it drops the dead id, initializes
// the session again — the same open a first call runs — and replays the
// call once. A second 404 comes back as ErrSessionExpired; there is no
// third attempt. The caller holds the gate for the whole call, so only
// one re-initialize per session is ever in flight.
func (c *Client) tool(ctx context.Context, g *sessionGate, session, name string, args any) (json.RawMessage, error) {
	msg, err := c.callTool(ctx, g, session, name, args)
	if errors.Is(err, ErrSessionExpired) {
		g.drop()
		if err := c.open(ctx, g, session); err != nil {
			return nil, err
		}
		msg, err = c.callTool(ctx, g, session, name, args)
	}
	if errors.Is(err, ErrErased) {
		g.drop()
	}
	return msg, err
}

func (c *Client) callTool(ctx context.Context, g *sessionGate, session, name string, args any) (json.RawMessage, error) {
	msg, _, err := c.post(ctx, mcpPath(session), rpcCall{
		JSONRPC: "2.0",
		ID:      c.allocID(),
		Method:  "tools/call",
		Params:  toolParams{Name: name, Arguments: args},
	}, g.mcpID)
	return msg, err
}

func (c *Client) post(ctx context.Context, path string, call rpcCall, mcp string) (json.RawMessage, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	raw, err := json.Marshal(call)
	if err != nil {
		return nil, "", err
	}
	req, err := c.newRequest(ctx, path, raw, mcp)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", c.scrub(err)
	}
	defer resp.Body.Close()
	session := resp.Header.Get("Mcp-Session-Id")
	if resp.StatusCode == http.StatusUnauthorized {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponse))
		return nil, "", ErrUnauthorized
	}
	if resp.StatusCode == http.StatusNotFound && mcp != "" {
		// The serve does not know this Mcp-Session-Id: an idle session
		// it expired. The body is plain text, not a JSON-RPC reply, so
		// it is read only to drain it. The tool call re-initializes
		// and replays once; a call that never carried a session is a
		// routing failure and stays a status error.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponse))
		return nil, "", ErrSessionExpired
	}
	if call.ID == 0 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponse))
		if resp.StatusCode != http.StatusAccepted {
			return nil, "", c.statusError(resp.StatusCode, nil)
		}
		return nil, session, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := readLimit(resp.Body, 512)
		return nil, "", c.statusError(resp.StatusCode, body)
	}
	msg, err := readRPC(resp.Header.Get("Content-Type"), resp.Body)
	if err != nil {
		return nil, "", err
	}
	if err := c.checkRPC(msg, call.ID); err != nil {
		return nil, "", err
	}
	return msg, session, nil
}

func (c *Client) newRequest(ctx context.Context, path string, body []byte, mcp string) (*http.Request, error) {
	ref, err := url.Parse(path)
	if err != nil {
		return nil, errors.New("memory: lambo path is not a path")
	}
	endpoint := c.base.ResolveReference(ref).String()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, c.scrub(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.token)
	if mcp != "" {
		req.Header.Set("Mcp-Session-Id", mcp)
	}
	return req, nil
}

func (c *Client) allocID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	if c.nextID < 1 {
		c.nextID = 1
	}
	return c.nextID
}

func (c *Client) checkRPC(msg json.RawMessage, id int) error {
	var rpc struct {
		ID    *int `json:"id"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(msg, &rpc) != nil {
		return errors.New("memory: lambo response is not json-rpc")
	}
	if rpc.ID == nil || *rpc.ID != id {
		return errors.New("memory: lambo response id does not match")
	}
	if rpc.Error == nil {
		return nil
	}
	if rpc.Error.Code == ErasedCode {
		return ErrErased
	}
	text := rpc.Error.Message
	if c.token != "" {
		text = strings.ReplaceAll(text, c.token, "redacted")
	}
	if text == "" {
		return fmt.Errorf("memory: lambo json-rpc error %d", rpc.Error.Code)
	}
	return fmt.Errorf("memory: lambo json-rpc error %d: %s", rpc.Error.Code, text)
}

func (c *Client) statusError(code int, body []byte) error {
	if code == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	msg := fmt.Sprintf("memory: lambo status %d", code)
	text := strings.TrimSpace(string(body))
	if text == "" || (c.token != "" && strings.Contains(text, c.token)) || len(text) > 200 {
		return errors.New(msg)
	}
	return fmt.Errorf("%s: %s", msg, text)
}

func (c *Client) scrub(err error) error {
	if err == nil || c.token == "" || !strings.Contains(err.Error(), c.token) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), c.token, "redacted"))
}

func unconfirmed(err error) error {
	if err == nil || errors.Is(err, ErrUnconfirmed) {
		return ErrUnconfirmed
	}
	return fmt.Errorf("%w: %w", ErrUnconfirmed, err)
}

func mcpPath(session string) string {
	return "/mcp/s/" + url.PathEscape(session)
}

func erasePath(session string) string {
	return "/admin/s/" + url.PathEscape(session) + "/erase"
}

type rpcCall struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type initParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      clientInfo     `json:"clientInfo"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type toolParams struct {
	Name      string `json:"name"`
	Arguments any    `json:"arguments"`
}

type deriveArgs struct {
	AgentID  string    `json:"agent_id"`
	Concepts []Concept `json:"concepts"`
}

type statsArgs struct {
	AgentID string `json:"agent_id"`
	Receipt string `json:"receipt"`
	WaitMS  int    `json:"wait_ms"`
}

type recallArgs struct {
	AgentID   string `json:"agent_id"`
	Query     string `json:"query"`
	TopK      int    `json:"top_k"`
	MaxTokens int    `json:"max_tokens"`
}

type eraseRequest struct {
	Confirm string `json:"confirm"`
}

func toolContent(msg json.RawMessage) (json.RawMessage, error) {
	var rpc struct {
		Result *struct {
			IsError           bool            `json:"isError"`
			StructuredContent json.RawMessage `json:"structuredContent"`
		} `json:"result"`
	}
	if json.Unmarshal(msg, &rpc) != nil || rpc.Result == nil {
		return nil, errors.New("memory: lambo response has no result")
	}
	if rpc.Result.IsError {
		return nil, errors.New("memory: lambo refused the call")
	}
	if rawEmpty(rpc.Result.StructuredContent) {
		return nil, errors.New("memory: lambo response has no structured content")
	}
	return rpc.Result.StructuredContent, nil
}

func recallFrom(msg json.RawMessage) (Recall, error) {
	sc, err := toolContent(msg)
	if err != nil {
		return Recall{}, err
	}
	var body struct {
		Context  string   `json:"context"`
		Hits     []Hit    `json:"hits"`
		Warnings []string `json:"warnings"`
	}
	if json.Unmarshal(sc, &body) != nil {
		return Recall{}, errors.New("memory: lambo recall is not a recall")
	}
	if body.Hits == nil {
		body.Hits = []Hit{}
	}
	if body.Warnings == nil {
		body.Warnings = []string{}
	}
	return Recall{Context: body.Context, Hits: body.Hits, Warnings: body.Warnings}, nil
}

func readRPC(contentType string, r io.Reader) (json.RawMessage, error) {
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, errors.New("memory: lambo response has no content type")
	}
	switch media {
	case "application/json":
		body, err := readLimit(r, maxResponse)
		if err != nil {
			return nil, err
		}
		if !json.Valid(body) {
			return nil, errors.New("memory: lambo response is not json")
		}
		return body, nil
	case "text/event-stream":
		return readSSEMessage(r)
	default:
		return nil, errors.New("memory: lambo response is neither json nor an event stream")
	}
}

// readSSEMessage returns the first JSON-RPC message in the stream. The
// priming event has empty data and is ignored. The serve closes the body
// after the message. Reading stops at that message so an open stream does
// not hold the call.
func readSSEMessage(r io.Reader) (json.RawMessage, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 4096), maxResponse)
	var data []string
	flush := func() (json.RawMessage, bool) {
		raw := strings.TrimSpace(strings.Join(data, "\n"))
		data = nil
		if raw == "" {
			return nil, false
		}
		return json.RawMessage(raw), true
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if msg, ok := flush(); ok {
				return msg, nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		if field == "data" {
			data = append(data, strings.TrimPrefix(value, " "))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("memory: lambo event stream: %w", err)
	}
	if msg, ok := flush(); ok {
		return msg, nil
	}
	return nil, errors.New("memory: lambo event stream has no message")
}

func readLimit(r io.Reader, n int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, n+1))
	if err != nil {
		return nil, fmt.Errorf("memory: lambo response: %w", err)
	}
	if int64(len(body)) > n {
		return nil, errors.New("memory: lambo response is too large")
	}
	return body, nil
}
