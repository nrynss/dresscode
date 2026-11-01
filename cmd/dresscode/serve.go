// The serve command: it composes what the server package built and
// listens. One settings file loads the budgets, the lines and the two
// data directories; the provider keys resolve out of the environment
// through the settings file's references; the clients are built over
// them without dialling anything; server.Compose opens one database
// handle, the spend books and every mount on it. The shutdown posture is
// the three-step one the look route's recovery demands: stop taking
// requests, wait for the jobs still running to go terminal, then release
// the database. A look still running at the end of the drain stays an
// unfinished row, and the next boot's recovery interrupts it and resumes
// it from its snapshot, which is why this command never cancels one: a
// cancelled look is terminal and never resumes.
//
// The signing keys are the two key materials the settings file does not
// carry: the session cookie's signing key and the code key. They come
// from the environment, never a committed file, like every other key.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nrynss/dresscode/internal/config"
	"github.com/nrynss/dresscode/internal/mail"
	"github.com/nrynss/dresscode/internal/memory"
	"github.com/nrynss/dresscode/internal/server"
	"github.com/nrynss/dresscode/internal/systemone"
	"github.com/nrynss/dresscode/internal/youcam"
	keelconfig "github.com/nrynss/keel/config"
	"github.com/nrynss/keel/cost"
)

// The two key variables this command reads. They are not settings-file
// sections, because they are key material like the provider keys, and
// their home is the environment alongside them. A boot without them is
// refused before anything opens.
const (
	VarSigningKey = "DRESSCODE_SIGNING_KEY"
	VarCodeKey    = "DRESSCODE_CODE_KEY"

	minKeyBytes = 16
)

// The defaults are this repository's own working paths and port. A
// deployment passes -db, -config, -app and -addr.
const (
	defaultServeDBPath = "data/dresscode.db"
	defaultServeAddr   = "127.0.0.1:8080"
	defaultAppDir      = "web/dist"
	// defaultDrain is how long the running jobs may keep the process
	// after the listener is down. A render is the longest paid step; the
	// drain waits for it so the event demo is not stranded mid-render.
	defaultDrain = 2 * time.Minute
	// httpGrace is how long in-flight requests may finish after the
	// listener is down. A job's terminal frame reaches a subscriber in
	// that window; a subscriber that stays past it is cut when the drain
	// ends and the database closes, and the app follows next_id on its
	// reconnect.
	httpGrace = 5 * time.Second
)

// runServe loads the settings, builds the clients, composes the server
// and listens until an interrupt arrives on interrupts. lookup is the
// environment the settings load and the two key variables read; nil means
// the process environment. interrupts is closed by the caller's signal
// handling; production passes one fed by SIGINT and SIGTERM.
func runServe(args []string, stdout, stderr io.Writer, lookup func(string) (string, bool), interrupts <-chan struct{}) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dbPath := fs.String("db", defaultServeDBPath, "database file to serve out of")
	configPath := fs.String("config", defaultConfigPath, "settings file")
	appDir := fs.String("app", defaultAppDir, "built app directory, holding index.html")
	addr := fs.String("addr", defaultServeAddr, "address to listen on")
	drain := fs.Duration("drain", defaultDrain, "how long running jobs may finish after the listener is down")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: dresscode serve [-db file] [-config file] [-app dir] [-addr host:port] [-drain duration]")
		return 2
	}
	if lookup == nil {
		lookup = os.LookupEnv
	}
	ctx := context.Background()

	settings, _, err := config.Load(ctx, config.Options{Path: *configPath, LookupEnv: lookup})
	if err != nil {
		fmt.Fprintf(stderr, "dresscode serve: %v\n", err)
		return 1
	}
	env := func(name string) string {
		value, _ := lookup(name)
		return value
	}
	signingKey := env(VarSigningKey)
	if len(signingKey) < minKeyBytes {
		fmt.Fprintf(stderr, "dresscode serve: %s is not set to at least %d bytes\n", VarSigningKey, minKeyBytes)
		return 1
	}
	codeKey := env(VarCodeKey)
	if len(codeKey) < minKeyBytes {
		fmt.Fprintf(stderr, "dresscode serve: %s is not set to at least %d bytes\n", VarCodeKey, minKeyBytes)
		return 1
	}

	cam, err := buildYouCam(&settings, stderr)
	if err != nil {
		return 1
	}
	mem, err := buildMemory(&settings, stderr)
	if err != nil {
		return 1
	}
	sender, err := buildMail(&settings, stderr)
	if err != nil {
		return 1
	}
	clef, err := buildClef(&settings, stderr)
	if err != nil {
		return 1
	}

	srv, err := server.Compose(ctx, server.Config{
		DBPath:     *dbPath,
		AppDir:     *appDir,
		SigningKey: []byte(signingKey),
		// The faults the mounts and the runners hit go to stderr, where
		// the operator running this in a terminal or a unit file reads
		// them. A discarded log would swallow the very faults an
		// operator needs.
		Log: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}, server.Wiring{
		Settings: &settings,
		Clef:     clef,
		YouCam:   cam,
		Memory:   mem,
		Mail:     sender,
		CodeKey:  []byte(codeKey),
	})
	if err != nil {
		fmt.Fprintf(stderr, "dresscode serve: %v\n", err)
		return 1
	}
	defer func() { _ = srv.Close() }()

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintf(stderr, "dresscode serve: %v\n", err)
		return 1
	}
	httpSrv := &http.Server{Handler: srv.Handler()}
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpSrv.Serve(listener) }()
	fmt.Fprintf(stdout, "listening on http://%s\n", listener.Addr())

	// The posture. First the listener stops taking requests; the looks
	// already started are runner goroutines, not requests, so they are
	// not what this wait drains. Then the drain lets them go terminal,
	// so the streams finish their story. What is still running when the
	// drain ends closes with the process and stays an unfinished row for
	// the next boot to interrupt and resume; this command never cancels
	// a look, because cancelled is terminal and never resumes.
	var reason string
	select {
	case <-interrupts:
		reason = "interrupted"
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(stderr, "dresscode serve: %v\n", err)
			return 1
		}
		reason = "listener closed"
	}
	// The two waits run in order. A wait that runs out of its grace
	// reports the leftover and moves on: the drain's leftovers stay
	// unfinished rows for the next boot, the request wait's leftovers
	// are cut by the close below.
	grace, cancel := context.WithTimeout(ctx, httpGrace)
	if err := httpSrv.Shutdown(grace); err != nil {
		fmt.Fprintf(stdout, "%s: requests still running after the grace\n", reason)
	}
	cancel()
	drainCtx, cancel := context.WithTimeout(ctx, *drain)
	if err := srv.Drain(drainCtx, 250*time.Millisecond); err != nil {
		fmt.Fprintf(stdout, "%s: jobs still running after the drain, the unfinished rows stay for the restart\n", reason)
	}
	cancel()
	_ = httpSrv.Close()
	fmt.Fprintf(stdout, "%s, stopping\n", reason)
	return 0
}

// reveal resolves one settings reference and reports the variable's
// name, never a value, on a failure.
func reveal(stderr io.Writer, secret keelconfig.Secret, name string) (string, bool) {
	value, err := secret.Reveal()
	if err != nil {
		fmt.Fprintf(stderr, "dresscode serve: %s: %v\n", name, err)
		return "", false
	}
	return value, true
}

// buildYouCam is the try-on client. Its constructor checks the token and
// the base URL shape; it does not dial.
func buildYouCam(settings *config.Settings, stderr io.Writer) (*youcam.Client, error) {
	token, ok := reveal(stderr, settings.Secrets.YouCamAPIKey, config.VarYouCamAPIKey)
	if !ok {
		return nil, errQuiet
	}
	cam, err := youcam.NewClient(youcam.ClientConfig{Token: token})
	if err != nil {
		fmt.Fprintf(stderr, "dresscode serve: the youcam client: %v\n", err)
		return nil, errQuiet
	}
	return cam, nil
}

// buildMemory is the Lambo client, the style memory and the account
// session the deletion mount erases. It does not dial.
func buildMemory(settings *config.Settings, stderr io.Writer) (*memory.Client, error) {
	base, ok := reveal(stderr, settings.Secrets.LamboURL, config.VarLamboURL)
	if !ok {
		return nil, errQuiet
	}
	token, ok := reveal(stderr, settings.Secrets.LamboDresscodeToken, config.VarLamboDresscodeToken)
	if !ok {
		return nil, errQuiet
	}
	mem, err := memory.New(memory.Config{BaseURL: base, Token: token})
	if err != nil {
		fmt.Fprintf(stderr, "dresscode serve: the memory client: %v\n", err)
		return nil, errQuiet
	}
	return mem, nil
}

// buildMail is the Resend sender for the sign-in codes. It does not dial.
func buildMail(settings *config.Settings, stderr io.Writer) (*mail.Client, error) {
	key, ok := reveal(stderr, settings.Secrets.ResendAPIKey, config.VarResendAPIKey)
	if !ok {
		return nil, errQuiet
	}
	from, ok := reveal(stderr, settings.Secrets.ResendFrom, config.VarResendFrom)
	if !ok {
		return nil, errQuiet
	}
	sender, err := mail.New(mail.Config{APIKey: key, From: from})
	if err != nil {
		fmt.Fprintf(stderr, "dresscode serve: the mail client: %v\n", err)
		return nil, errQuiet
	}
	return sender, nil
}

// buildClef is the model client factory. server.Compose calls it after
// the dollar books are open, so the Workers AI client meters every ask
// into the same store `dresscode budget` reads. The factory itself does
// not dial; construction checks the account id, the token and the meter.
func buildClef(settings *config.Settings, stderr io.Writer) (func(*cost.Meter) (systemone.SystemOne, error), error) {
	account, ok := reveal(stderr, settings.Secrets.CloudflareAccountID, config.VarCloudflareAccountID)
	if !ok {
		return nil, errQuiet
	}
	token, ok := reveal(stderr, settings.Secrets.WorkersAIAPIToken, config.VarWorkersAIAPIToken)
	if !ok {
		return nil, errQuiet
	}
	return func(dollars *cost.Meter) (systemone.SystemOne, error) {
		return systemone.NewWorkersAI(systemone.WorkersAIConfig{
			AccountID: account,
			Token:     token,
			Meter:     dollars,
		})
	}, nil
}

// errQuiet is the sentinel a builder returns when it has already
// reported the fault; runServe prints nothing twice.
var errQuiet = errors.New("dresscode serve: reported")

// serveInterrupts closes the channel on SIGINT or SIGTERM.
func serveInterrupts() <-chan struct{} {
	interrupts := make(chan struct{})
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer signal.Stop(sigs)
		<-sigs
		close(interrupts)
	}()
	return interrupts
}
