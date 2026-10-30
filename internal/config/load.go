package config

import (
	"context"
	"fmt"
	"os"

	keelconfig "github.com/nrynss/keel/config"
	"github.com/nrynss/keel/config/source"
)

// Options configures Load.
type Options struct {
	// Path is the settings file. It must name a file that exists.
	// Keel treats a missing file as empty, and an empty file would fail
	// a required key without naming its variable. Load refuses that path.
	Path string

	// LookupEnv reads an environment variable. Nil means os.LookupEnv.
	// The same function serves secret references and non-secret overrides.
	LookupEnv func(name string) (string, bool)

	// Keyring reads a keyring reference. Nil runs secret-tool.
	// The function must not return a value alongside an error, and the
	// error text is dropped: a failure names the variable and nothing else.
	Keyring func(ctx context.Context, variable string) (string, error)
}

// Load reads the settings file and resolves every key. A missing key
// returns an error that names the variable and carries no value.
func Load(ctx context.Context, opt Options) (Settings, keelconfig.Plan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opt.Path == "" {
		return Settings{}, keelconfig.Plan{}, fmt.Errorf("config: settings path is empty")
	}
	info, err := os.Stat(opt.Path)
	if err != nil {
		return Settings{}, keelconfig.Plan{}, fmt.Errorf("config: settings file: %w", err)
	}
	if info.IsDir() {
		return Settings{}, keelconfig.Plan{}, fmt.Errorf("config: settings file %s is a directory", opt.Path)
	}

	reg := keelconfig.NewRegistry()
	if err := source.RegisterDefaults(reg, source.Config{LookupEnv: opt.LookupEnv}); err != nil {
		return Settings{}, keelconfig.Plan{}, err
	}
	if err := reg.Register("keyring", keyringSource{read: opt.Keyring}); err != nil {
		return Settings{}, keelconfig.Plan{}, err
	}

	var got Settings
	plan, err := keelconfig.Load(ctx, &got, keelconfig.Config{
		Path:      opt.Path,
		LookupEnv: opt.LookupEnv,
		Registry:  reg,
	})
	if err != nil {
		return Settings{}, keelconfig.Plan{}, err
	}
	if err := got.check(); err != nil {
		return Settings{}, keelconfig.Plan{}, err
	}
	return got, plan, nil
}
