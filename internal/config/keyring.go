package config

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	keelconfig "github.com/nrynss/keel/config"
	"github.com/nrynss/keel/config/source"
)

// errKeyringMiss is a keyring lookup that did not yield a value.
// The message a caller sees names the variable and not a value.
var errKeyringMiss = fmt.Errorf("%w: keyring", source.ErrNotFound)

// keyringAttr is the secret-tool pair docs/keys.md records for one variable.
// The account attribute is not here. That file does not name it, and a
// lookup matches the attributes it is given.
type keyringAttr struct {
	service string
	kind    string
}

// keyringAttrs is the variables docs/keys.md places in the keyring.
// The rest resolve from the environment only.
var keyringAttrs = map[string]keyringAttr{
	VarYouCamAPIKey:      {service: "youcam", kind: "api-key"},
	VarYouCamAPISecret:   {service: "youcam", kind: "api-secret"},
	VarWorkersAIAPIToken: {service: "cloudflare", kind: "workers-ai"},
}

// keyringSource resolves a reference whose var is one of keyringAttrs.
type keyringSource struct {
	read func(context.Context, string) (string, error)
}

func (k keyringSource) Resolve(ctx context.Context, ref keelconfig.Ref) (string, error) {
	name := ref.Var()
	if name == "" {
		return "", fmt.Errorf("%w: keyring: missing var", source.ErrMissingLocator)
	}
	if _, ok := keyringArgs(name); !ok {
		return "", keyringMiss(name)
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("%w: keyring: var=%s", err, name)
		}
	}
	if k.read != nil {
		val, err := k.read(ctx, name)
		if err != nil || val == "" {
			return "", keyringMiss(name)
		}
		return val, nil
	}
	return secretTool(ctx, name)
}

func keyringMiss(variable string) error {
	return fmt.Errorf("%w: var=%s", errKeyringMiss, variable)
}

// keyringArgs returns the secret-tool arguments for variable.
func keyringArgs(variable string) ([]string, bool) {
	attr, ok := keyringAttrs[variable]
	if !ok {
		return nil, false
	}
	return []string{"lookup", "service", attr.service, "kind", attr.kind}, true
}

// secretTool runs secret-tool and returns its standard output.
// Standard error is discarded. A failure names the variable and
// never includes output from the command.
func secretTool(ctx context.Context, variable string) (string, error) {
	args, ok := keyringArgs(variable)
	if !ok {
		return "", keyringMiss(variable)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, "secret-tool", args...)
	cmd.WaitDelay = 5 * time.Second
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%w: keyring: var=%s", ctx.Err(), variable)
		}
		return "", keyringMiss(variable)
	}
	val := stdout.String()
	if strings.HasSuffix(val, "\n") {
		val = strings.TrimSuffix(val, "\n")
		val = strings.TrimSuffix(val, "\r")
	}
	if val == "" {
		return "", keyringMiss(variable)
	}
	return val, nil
}
