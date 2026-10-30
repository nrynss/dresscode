// Package mail sends sign-in codes and nothing else.
//
// Client implements Keel identity.Sender over Resend. The message is
// plain text and a minimal HTML version of the same words. The from
// address is the resolved RESEND_FROM setting, and the key is the
// resolved RESEND_API_KEY. This package does not read the environment.
//
// Keel's identity service applies its send ceilings before it calls
// Send, so a refused request never reaches Resend. Recorder implements
// the same interface for tests and does not dial.
//
// The sign-in code is not written to a log, an error, or a formatting
// path. A test reads it from Recorder.Messages.
package mail

import (
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"strings"
	"time"
)

const (
	// subject is the one subject a sign-in mail uses. It does not carry
	// the code, so a mail log of the subject does not reveal it.
	subject = "Your Dresscode sign-in code"

	// codeLife is how long Keel's sign-in code stays live. Keel does not
	// export that duration. The mail states it in minutes.
	codeLife = 10 * time.Minute
)

// errAddress, errCode, and errFrom are local refusals. They name the
// field and never the value, so a log of the error reveals nothing.
var (
	errAddress = errors.New("mail: address is unusable")
	errCode    = errors.New("mail: code is unusable")
	errFrom    = errors.New("mail: from address is unusable")
)

// secret is a key held so that formatting and logging print a fixed
// placeholder. value is the only read, and it stays in this package.
type secret string

func (secret) String() string { return "<secret>" }

func (s secret) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, s.String())
}

func (s secret) GoString() string { return s.String() }

func (secret) LogValue() slog.Value { return slog.StringValue("<secret>") }

func (s secret) value() string { return string(s) }

// normalize accepts one sign-in address and one six-digit code. The
// address is trimmed and lowercased, which is the form Keel already
// passes. Anything else is refused before a sender dials.
func normalize(address, code string) (string, string, error) {
	address = strings.ToLower(strings.TrimSpace(address))
	code = strings.TrimSpace(code)
	if !plainAddress(address) {
		return "", "", errAddress
	}
	if !sixDigits(code) {
		return "", "", errCode
	}
	return address, code, nil
}

func sixDigits(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// plainAddress is one mailbox with a dotted domain and no display name.
// It is not a full address parser. It refuses the characters that would
// break a JSON string or an SMTP header if a value were ever copied.
func plainAddress(addr string) bool {
	local, domain, ok := strings.Cut(addr, "@")
	if !ok || strings.Count(addr, "@") != 1 {
		return false
	}
	if local == "" || domain == "" || len(addr) > 254 {
		return false
	}
	if strings.ContainsAny(addr, " \t<>\"(),:;\\\r\n\x00") {
		return false
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || !strings.Contains(domain, ".") {
		return false
	}
	return true
}

// senderFrom checks the RESEND_FROM value. A bare address is sent as
// itself. A single display name in angle brackets is sent in that form.
// The returned string is what Resend receives in "from".
func senderFrom(from string) (string, error) {
	from = strings.TrimSpace(from)
	if from == "" || strings.ContainsAny(from, "\r\n\x00") {
		return "", errFrom
	}
	addr := from
	if strings.Contains(from, "<") || strings.Contains(from, ">") {
		name, email, ok := strings.Cut(from, "<")
		email, suffix, cut := strings.Cut(email, ">")
		if !ok || !cut || suffix != "" || strings.Count(from, "<") != 1 || strings.Count(from, ">") != 1 {
			return "", errFrom
		}
		name = strings.TrimSpace(name)
		email = strings.TrimSpace(email)
		if name == "" || strings.ContainsAny(name, "<>\"") {
			return "", errFrom
		}
		addr = email
		from = name + " <" + email + ">"
	}
	if !plainAddress(addr) {
		return "", errFrom
	}
	return from, nil
}

func textBody(code string) string {
	return "Your Dresscode sign-in code is " + code + ".\n\n" + lifeSentence() + "\n"
}

func htmlBody(code string) string {
	return "<p>Your Dresscode sign-in code is " + html.EscapeString(code) + ".</p><p>" + html.EscapeString(lifeSentence()) + "</p>"
}

func lifeSentence() string {
	minutes := int(codeLife / time.Minute)
	return fmt.Sprintf("It expires in %d minutes. If you did not ask for this code, ignore this message.", minutes)
}
