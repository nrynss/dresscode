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
	"net/url"
	"strings"
	"time"

	"github.com/nrynss/keel/identity"
)

const (
	defaultBase = "https://api.resend.com"
	userAgent   = "dresscode"
	maxReply    = 1 << 20

	// requestTimeout bounds one post when the caller set no deadline.
	// The caller's context can end the call sooner.
	requestTimeout = 10 * time.Second
)

// Refusal is a Resend answer that did not accept the mail. Status is the
// HTTP status. Name is Resend's error name when the body carried one of
// the documented tokens. Neither field is the request, so the code and
// the key cannot ride out on the error.
type Refusal struct {
	Status int
	Name   string
}

func (e *Refusal) Error() string {
	if e == nil {
		return "mail: resend refused"
	}
	if e.Name == "" {
		return fmt.Sprintf("mail: resend status %d", e.Status)
	}
	return fmt.Sprintf("mail: resend status %d: %s", e.Status, e.Name)
}

// Config configures a Client. APIKey and From are the resolved settings
// RESEND_API_KEY and RESEND_FROM. BaseURL and HTTP are for tests. Empty
// BaseURL is Resend's API origin. A nil HTTP client is one with a short
// timeout that does not follow redirects.
type Config struct {
	APIKey  string
	From    string
	BaseURL string
	HTTP    *http.Client
	Log     *slog.Logger
}

// Client posts one sign-in code to Resend. Create it with New. A Client
// is safe for concurrent use.
type Client struct {
	key     secret
	from    string
	baseURL string
	http    *http.Client
	log     *slog.Logger
}

var _ identity.Sender = (*Client)(nil)

// New builds a client. It refuses an empty key, an unusable from
// address, and a base URL that is not Resend's HTTPS origin shape, so
// those never wait until a send.
func New(cfg Config) (*Client, error) {
	key, err := cleanKey(cfg.APIKey)
	if err != nil {
		return nil, err
	}
	from, err := senderFrom(cfg.From)
	if err != nil {
		return nil, err
	}
	base, err := parseBase(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &Client{
		key:     key,
		from:    from,
		baseURL: base,
		http:    httpClient(cfg.HTTP),
		log:     log,
	}, nil
}

func cleanKey(key string) (secret, error) {
	key = strings.TrimSpace(key)
	if key == "" || strings.ContainsAny(key, " \r\n\x00") {
		return "", errors.New("mail: api key is unusable")
	}
	return secret(key), nil
}

func parseBase(raw string) (string, error) {
	if raw == "" {
		raw = defaultBase
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", errors.New("mail: resend url is unusable")
	}
	if u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("mail: resend url is unusable")
	}
	return "https://" + u.Host, nil
}

// httpClient returns a client that does not follow redirects, so the
// bearer token stays on the one request. A caller-supplied client keeps
// its transport and its timeout. The copy is not the caller's value.
func httpClient(in *http.Client) *http.Client {
	if in == nil {
		return &http.Client{Timeout: requestTimeout, CheckRedirect: refuseRedirect}
	}
	copied := *in
	copied.CheckRedirect = refuseRedirect
	return &copied
}

func refuseRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

func (c *Client) String() string {
	if c == nil {
		return "<nil>"
	}
	return "mail: resend sender " + c.from
}

func (c *Client) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, c.String())
}

func (c *Client) GoString() string { return c.String() }

func (c *Client) LogValue() slog.Value {
	if c == nil {
		return slog.StringValue("<nil>")
	}
	return slog.GroupValue(
		slog.String("from", c.from),
		slog.String("base", c.baseURL),
	)
}

type resendBody struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
	HTML    string   `json:"html"`
}

// Send posts one sign-in code. Keel's identity service has already
// applied its send ceilings. A local refusal, including a cancelled
// context, returns before the request is built.
func (c *Client) Send(ctx context.Context, address, code string) error {
	if c == nil {
		return errors.New("mail: no sender")
	}
	address, code, err := normalize(address, code)
	if err != nil {
		return err
	}
	if ctx == nil {
		return errors.New("mail: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := json.Marshal(resendBody{
		From:    c.from,
		To:      []string{address},
		Subject: subject,
		Text:    textBody(code),
		HTML:    htmlBody(code),
	})
	if err != nil {
		return errors.New("mail: message could not be encoded")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/emails", bytes.NewReader(payload))
	if err != nil {
		return errors.New("mail: request could not be built")
	}
	req.Header.Set("Authorization", "Bearer "+c.key.value())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return scrub(err, code, c.key.value())
	}
	defer resp.Body.Close()
	return c.accepted(resp, code)
}

func (c *Client) accepted(resp *http.Response, code string) error {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReply+1))
	if err != nil {
		return errors.New("mail: resend response could not be read")
	}
	if len(raw) > maxReply {
		return errors.New("mail: resend response is too large")
	}
	var reply struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if len(bytes.TrimSpace(raw)) > 0 && json.Unmarshal(raw, &reply) != nil {
		if resp.StatusCode != http.StatusOK {
			return &Refusal{Status: resp.StatusCode}
		}
		return errors.New("mail: resend response is not json")
	}
	if resp.StatusCode != http.StatusOK {
		return &Refusal{Status: resp.StatusCode, Name: safeName(reply.Name)}
	}
	id := safeToken(strings.TrimSpace(reply.ID))
	if id == "" {
		return errors.New("mail: resend returned no id")
	}
	if strings.Contains(id, code) {
		c.log.Info("sign-in mail accepted")
		return nil
	}
	c.log.Info("sign-in mail accepted", "provider_id", id)
	return nil
}

// safeToken keeps a provider id that is a short token. Anything else is
// dropped. The caller also refuses to log an id that contains the code.
func safeToken(id string) string {
	if id == "" || len(id) > 64 {
		return ""
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return ""
		}
	}
	return id
}

// safeName keeps Resend's snake_case error name and drops anything else.
func safeName(name string) string {
	if name == "" || len(name) > 64 {
		return ""
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return ""
		}
	}
	return name
}

// scrub hides a transport error that managed to contain the code or the
// key. The caller still learns the request failed.
func scrub(err error, hidden ...string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	for _, h := range hidden {
		if h != "" && strings.Contains(msg, h) {
			return errors.New("mail: resend request failed")
		}
	}
	return fmt.Errorf("mail: resend: %w", err)
}
