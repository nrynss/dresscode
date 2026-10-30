package mail

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/nrynss/keel/identity"
)

// Message is one sign-in code the recorder accepted. Formatting and
// logging print the address only. Read Code in the test and do not log
// that field.
type Message struct {
	Address string
	Code    string
}

func (m Message) String() string {
	return "mail: sign-in message to " + m.Address
}

func (m Message) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, m.String())
}

func (m Message) GoString() string { return m.String() }

func (m Message) LogValue() slog.Value { return slog.StringValue(m.String()) }

// Recorder is an identity.Sender that keeps each accepted send. It does
// not dial. SetFailure makes the next sends return that error and record
// nothing, the way a provider outage does.
type Recorder struct {
	mu   sync.Mutex
	sent []Message
	fail error
}

var _ identity.Sender = (*Recorder)(nil)

// Send records one code, or returns the armed failure. A cancelled
// context records nothing.
func (r *Recorder) Send(ctx context.Context, address, code string) error {
	if r == nil {
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	r.sent = append(r.sent, Message{Address: address, Code: code})
	return nil
}

// SetFailure arms or clears the provider failure. A nil error clears it.
func (r *Recorder) SetFailure(err error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.fail = err
	r.mu.Unlock()
}

// Messages returns the accepted sends, oldest first.
func (r *Recorder) Messages() []Message {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Message(nil), r.sent...)
}

func (r *Recorder) String() string {
	if r == nil {
		return "<nil>"
	}
	r.mu.Lock()
	n := len(r.sent)
	r.mu.Unlock()
	return fmt.Sprintf("mail: recorder (%d)", n)
}

func (r *Recorder) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, r.String())
}

func (r *Recorder) GoString() string { return r.String() }

func (r *Recorder) LogValue() slog.Value { return slog.StringValue(r.String()) }
