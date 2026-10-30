//go:build live

package mail

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestLiveSigninCodeArrives sends one sign-in code to RESEND_TO through
// the real Resend client, the Keel identity.Sender a sign-in uses. The
// key, the from address and the recipient come from the environment; a
// missing one skips, so `-tags live` without them does nothing. The
// gate does not build this file.
//
// Exactly one send. A failure ends the test; there is no retry. The key
// is never printed, logged, or embedded, and a send error is reported
// by its refusal fields or a fixed line, never by its text.
func TestLiveSigninCodeArrives(t *testing.T) {
	key := os.Getenv("RESEND_API_KEY")
	from := os.Getenv("RESEND_FROM")
	to := os.Getenv("RESEND_TO")
	if key == "" || from == "" || to == "" {
		t.Skip("live resend needs RESEND_API_KEY, RESEND_FROM and RESEND_TO in the environment")
	}

	ids := &idCapture{}
	client, err := New(Config{
		APIKey: key,
		From:   from,
		Log:    slog.New(ids),
	})
	if err != nil {
		// New's errors name the field and never the value. The key
		// still stays out of the report.
		t.Fatal("client was not built")
	}

	// The test stands in for the sign-in that mints the code. No
	// server holds it, so it is inert.
	code, err := newCode()
	if err != nil {
		t.Fatalf("code: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now().UTC()
	if err := client.Send(ctx, to, code); err != nil {
		var refused *Refusal
		if errors.As(err, &refused) {
			t.Fatalf("resend refused the send: status %d name %q", refused.Status, refused.Name)
		}
		t.Fatal("send failed; detail withheld")
	}
	took := time.Since(start).Round(time.Millisecond)

	t.Logf("sent_at_utc=%s", start.Format("2006-01-02T15:04:05Z"))
	t.Logf("subject=%q", subject)
	t.Logf("code=%s (inert: no server holds it)", code)
	if id := ids.get(); id != "" && !strings.Contains(id, code) {
		t.Logf("resend_id=%s", id)
	} else {
		t.Log("resend_id was not logged by the sender")
	}
	t.Logf("send took %s", took)
}

// newCode is one six-digit code, the shape the sign-in mints.
func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// idCapture keeps the provider id the sender logs on an accepted send.
// It is the only way to read the id without changing the sender, and
// the sender already refuses to log an id that carries the code. The
// key never reaches this handler.
type idCapture struct {
	mu sync.Mutex
	id string
}

func (c *idCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *idCapture) Handle(_ context.Context, r slog.Record) error {
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "provider_id" {
			c.mu.Lock()
			c.id = a.Value.String()
			c.mu.Unlock()
		}
		return true
	})
	return nil
}

func (c *idCapture) WithAttrs([]slog.Attr) slog.Handler { return c }

func (c *idCapture) WithGroup(string) slog.Handler { return c }

func (c *idCapture) get() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.id
}
