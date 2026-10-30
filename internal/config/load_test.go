package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	keelconfig "github.com/nrynss/keel/config"
	"github.com/nrynss/keel/cost"
)

const standinPrefix = "standin-value-"

func TestExampleLoads(t *testing.T) {
	t.Parallel()
	values := standins()
	var keyringCalled bool
	got, plan, err := Load(t.Context(), Options{
		Path:      examplePath(t),
		LookupEnv: lookup(values),
		Keyring: func(context.Context, string) (string, error) {
			keyringCalled = true
			return "", errors.New("absent")
		},
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if keyringCalled {
		t.Fatal("keyring consulted while the environment held every variable")
	}

	if got.Budgets.YouCam.Units != 1040 || got.Budgets.YouCam.JudgingUnits != 400 {
		t.Fatalf("youcam units %d judging %d", got.Budgets.YouCam.Units, got.Budgets.YouCam.JudgingUnits)
	}
	if got.Budgets.YouCam.DevelopmentUnits() != 640 {
		t.Fatalf("development units %d", got.Budgets.YouCam.DevelopmentUnits())
	}
	if got.Budgets.YouCam.Period() != cost.None {
		t.Fatalf("youcam period %v, want lifetime", got.Budgets.YouCam.Period())
	}
	if got.Budgets.YouCam.Denomination() != (cost.Denomination{Name: YouCamCredit, Minor: 1}) {
		t.Fatalf("denomination %+v", got.Budgets.YouCam.Denomination())
	}
	if got.Budgets.WorkersAI.DollarsPerDay != 1 || got.Budgets.WorkersAI.Period() != cost.DailyUTC {
		t.Fatalf("workers %+v period %v", got.Budgets.WorkersAI, got.Budgets.WorkersAI.Period())
	}
	if got.Lease.Judge.FreshSelfies != 5 || got.Lease.Judge.Renders != 10 {
		t.Fatalf("lease %+v", got.Lease.Judge)
	}
	if got.Flags.FreshUploadsOff || got.Flags.RendersOff || got.Flags.SamplesOnly {
		t.Fatalf("flags %+v, want the open position", got.Flags)
	}

	secrets := []struct {
		name   string
		secret keelconfig.Secret
	}{
		{VarYouCamAPIKey, got.Secrets.YouCamAPIKey},
		{VarYouCamAPISecret, got.Secrets.YouCamAPISecret},
		{VarWorkersAIAPIToken, got.Secrets.WorkersAIAPIToken},
		{VarCloudflareAccountID, got.Secrets.CloudflareAccountID},
		{VarResendAPIKey, got.Secrets.ResendAPIKey},
		{VarResendFrom, got.Secrets.ResendFrom},
		{VarOpenRouterAPIKey, got.Secrets.OpenRouterAPIKey},
		{VarOpenRouterBaseURL, got.Secrets.OpenRouterBaseURL},
		{VarLamboURL, got.Secrets.LamboURL},
		{VarLamboDresscodeToken, got.Secrets.LamboDresscodeToken},
	}
	for _, sec := range secrets {
		matchStandin(t, sec.secret, values[sec.name])
	}

	planText := plan.String()
	for _, name := range EnvVars() {
		if !strings.Contains(planText, "var="+name) {
			t.Fatalf("plan does not name %s", name)
		}
	}
	if strings.Contains(planText, standinPrefix) {
		t.Fatal("plan contains an injected value")
	}
	if leaked(got, values) {
		t.Fatal("formatted settings contain an injected value")
	}

	pool, err := got.Budgets.YouCam.Pool()
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	dev, err := pool.Development()
	if err != nil {
		t.Fatalf("development account: %v", err)
	}
	judging, err := pool.Judging()
	if err != nil {
		t.Fatalf("judging account: %v", err)
	}
	if _, err := dev.Reserve(t.Context(), 1040); !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("development reserve of the whole pool: %v", err)
	}
	if _, err := dev.Reserve(t.Context(), 640); err != nil {
		t.Fatalf("development reserve of its ceiling: %v", err)
	}
	if _, err := judging.Reserve(t.Context(), 400); err != nil {
		t.Fatalf("judging reserve of its share: %v", err)
	}
	if _, err := dev.Reserve(t.Context(), 1); !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("development past its ceiling: %v", err)
	}
	if _, err := judging.Reserve(t.Context(), 1); !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("judging past its share: %v", err)
	}

	budget, err := got.Budgets.WorkersAI.Budget()
	if err != nil {
		t.Fatalf("workers budget: %v", err)
	}
	if err := budget.Reserve(cost.Dollar); err != nil {
		t.Fatalf("reserve one dollar: %v", err)
	}
	if err := budget.Reserve(1); !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("reserve past the daily dollar: %v", err)
	}

	decls := got.Flags.Declarations()
	wantFlags := []string{FlagFreshUploadsOff, FlagRendersOff, FlagSamplesOnly}
	if len(decls) != len(wantFlags) {
		t.Fatalf("%d flags, want %d", len(decls), len(wantFlags))
	}
	for i, name := range wantFlags {
		if decls[i].Name != name || decls[i].Default || decls[i].Help == "" {
			t.Fatalf("flag %+v", decls[i])
		}
	}
}

func TestMissingKeyNamesTheVariable(t *testing.T) {
	t.Parallel()
	for _, name := range EnvVars() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			values := standins()
			delete(values, name)
			_, _, err := Load(t.Context(), Options{
				Path:      examplePath(t),
				LookupEnv: lookup(values),
				Keyring: func(context.Context, string) (string, error) {
					return "", errors.New("absent")
				},
			})
			if !errors.Is(err, keelconfig.ErrResolve) {
				t.Fatalf("err = %v, want ErrResolve", err)
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("error does not name %s: %v", name, err)
			}
			if strings.Contains(err.Error(), standinPrefix) {
				t.Fatal("error contains an injected value")
			}
		})
	}
}

func TestExampleQuotesAreNamesOnly(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(examplePath(t))
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"env": true, "keyring": true}
	for _, name := range EnvVars() {
		allowed[name] = true
	}
	quoted := regexp.MustCompile(`"([^"]*)"`)
	for _, match := range quoted.FindAllStringSubmatch(string(data), -1) {
		if !allowed[match[1]] {
			t.Fatalf("example quotes %q", match[1])
		}
	}
}

func TestLiteralSecretIsRefused(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(examplePath(t))
	if err != nil {
		t.Fatal(err)
	}
	const block = "[[secrets.youcam_api_key]]\nsource = \"env\"\nvar = \"YOUCAM_API_KEY\"\n\n[[secrets.youcam_api_key]]\nsource = \"keyring\"\nvar = \"YOUCAM_API_KEY\"\n"
	const literal = "literal-do-not-keep"
	if !strings.Contains(string(data), block) {
		t.Fatal("example no longer has the youcam key block")
	}
	bad := strings.Replace(string(data), block, "[secrets]\nyoucam_api_key = \""+literal+"\"\n", 1)
	path := filepath.Join(t.TempDir(), "dresscode.toml")
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = Load(t.Context(), Options{
		Path:      path,
		LookupEnv: lookup(standins()),
		Keyring: func(context.Context, string) (string, error) {
			return "", errors.New("absent")
		},
	})
	if !errors.Is(err, keelconfig.ErrInlineSecret) {
		t.Fatalf("err = %v, want ErrInlineSecret", err)
	}
	if strings.Contains(err.Error(), literal) || strings.Contains(err.Error(), standinPrefix) {
		t.Fatal("error contains a value")
	}
}

func TestJudgingShareCannotExceedUnits(t *testing.T) {
	t.Parallel()
	s := Settings{
		Budgets: Budgets{
			YouCam:    YouCamBudget{Units: 10, JudgingUnits: 11},
			WorkersAI: WorkersAIBudget{DollarsPerDay: 1},
		},
		Lease: Lease{Judge: JudgeLease{FreshSelfies: 1, Renders: 1}},
	}
	if err := s.check(); err == nil {
		t.Fatal("judging share above the ceiling was accepted")
	}
	if _, err := s.Budgets.YouCam.Pool(); err == nil {
		t.Fatal("pool accepted a judging share above the ceiling")
	}
}

func TestKeyringArgsMatchTheDocs(t *testing.T) {
	t.Parallel()
	want := map[string][]string{
		VarYouCamAPIKey:      {"lookup", "service", "youcam", "kind", "api-key"},
		VarYouCamAPISecret:   {"lookup", "service", "youcam", "kind", "api-secret"},
		VarWorkersAIAPIToken: {"lookup", "service", "cloudflare", "kind", "workers-ai"},
	}
	for name, args := range want {
		got, ok := keyringArgs(name)
		if !ok || !reflect.DeepEqual(got, args) {
			t.Fatalf("%s args = %v", name, got)
		}
	}
	if _, ok := keyringArgs(VarResendAPIKey); ok {
		t.Fatal("resend is not a keyring variable")
	}
}

func TestKeyringCommand(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	tool := filepath.Join(dir, "secret-tool")

	t.Run("stdout", func(t *testing.T) {
		writeTool(t, tool, "#!/bin/sh\n"+
			"test \"$1\" = lookup || exit 1\n"+
			"test \"$2\" = service || exit 1\n"+
			"test \"$3\" = youcam || exit 1\n"+
			"test \"$4\" = kind || exit 1\n"+
			"test \"$5\" = api-key || exit 1\n"+
			"printf '%s\\n' 'stand-in-from-tool'\n")
		got, err := secretTool(t.Context(), VarYouCamAPIKey)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if got != "stand-in-from-tool" {
			t.Fatal("stdout was not the tool stand-in")
		}
	})

	t.Run("failure drops output", func(t *testing.T) {
		const leaked = "LEAKED-VALUE"
		writeTool(t, tool, "#!/bin/sh\nprintf '%s\\n' '"+leaked+"' >&2\nprintf '%s\\n' '"+leaked+"'\nexit 1\n")
		_, err := secretTool(t.Context(), VarYouCamAPIKey)
		if err == nil {
			t.Fatal("command failure was accepted")
		}
		if !strings.Contains(err.Error(), VarYouCamAPIKey) {
			t.Fatalf("error does not name the variable: %v", err)
		}
		if strings.Contains(err.Error(), leaked) {
			t.Fatal("error contains command output")
		}
	})
}

func writeTool(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func standins() map[string]string {
	values := make(map[string]string, len(EnvVars()))
	for i, name := range EnvVars() {
		values[name] = fmt.Sprintf("%s%02d", standinPrefix, i+1)
	}
	return values
}

func lookup(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := values[name]
		return v, ok
	}
}

func matchStandin(t *testing.T, secret keelconfig.Secret, want string) {
	t.Helper()
	got, err := secret.Reveal()
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	if got != want {
		t.Fatal("resolved value is not the injected stand-in")
	}
}

func leaked(got Settings, values map[string]string) bool {
	encoded, err := json.Marshal(got)
	text := fmt.Sprintf("%v %+v %s", got, got, encoded)
	if err != nil {
		text += err.Error()
	}
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}

func examplePath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "dresscode.toml.example")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	return path
}
