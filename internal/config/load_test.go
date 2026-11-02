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
	if got.Judge.Met != 0.35265 || len(got.Judge.PerCriterion) != 6 {
		t.Fatalf("judge %+v, want the evaluation's met line and its six overrides", got.Judge)
	}
	for id, want := range map[string]float64{
		"date-beard-even":           0.19435,
		"family-beard-even":         0.1682,
		"funeral-beard-close":       0.26845,
		"interview-beard-short":     0.18815,
		"interview-collar-buttoned": 0.1126,
		"interview-expression-calm": 0.2022,
	} {
		if got.Judge.PerCriterion[id] != want {
			t.Fatalf("override %s = %v, want %v", id, got.Judge.PerCriterion[id], want)
		}
	}
	if got.Check != (Check{ChangeMade: 0.4978, SamePerson: 0.4799, CriterionMet: 0.4986, Model: "clef"}) {
		t.Fatalf("check %+v, want the evaluation's lines on clef", got.Check)
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
	// The check model is a value the contract fixes, not a name: the
	// check recordings carry clef and clef-flash, and the example names
	// the one the pipeline runs. The two [paths] values are the
	// repository's own data directories, paths and not names.
	allowed := map[string]bool{
		"env":                true,
		"keyring":            true,
		"clef":               true,
		"testdata/occasions": true,
		"testdata/samples":   true,
	}
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

// loadVariant writes one variant of the example and loads it. The
// Variant is the example with replace applied once.
func loadVariant(t *testing.T, replace func(text string) string) (Settings, error) {
	t.Helper()
	data, err := os.ReadFile(examplePath(t))
	if err != nil {
		t.Fatal(err)
	}
	text := replace(string(data))
	if text == string(data) {
		t.Fatal("the variant changed nothing: its anchor is not in the example")
	}
	path := filepath.Join(t.TempDir(), "dresscode.toml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _, err := Load(t.Context(), Options{
		Path:      path,
		LookupEnv: lookup(standins()),
		Keyring: func(context.Context, string) (string, error) {
			return "", errors.New("absent")
		},
	})
	return got, err
}

// cut drops one whole section from the example: from its header line to
// the next header that is not its sub-table, so a sub-table goes with its
// parent and an edit to the section's comments does not move the anchor.
func cut(section string) func(string) string {
	subTable := strings.TrimSuffix(section, "]") + "."
	return func(text string) string {
		lines := strings.Split(text, "\n")
		start := -1
		for i, line := range lines {
			if strings.TrimSpace(line) == section {
				start = i
				break
			}
		}
		if start < 0 {
			return text
		}
		end := len(lines)
		for i := start + 1; i < len(lines); i++ {
			header := strings.TrimSpace(lines[i])
			if strings.HasPrefix(header, "[") && !strings.HasPrefix(header, subTable) {
				end = i
				break
			}
		}
		return strings.Join(append(lines[:start:start], lines[end:]...), "\n")
	}
}

// swap replaces one exact line of the example.
func swap(from, to string) func(string) string {
	return func(text string) string {
		if !strings.Contains(text, from) {
			return text
		}
		return strings.Replace(text, from, to, 1)
	}
}

// TestAnAbsentSectionIsALoadError pins the ruling: a settings file with
// no [judge] or no [check] section fails the load naming the key, and an
// explicit zero is the same refusal, because zero is the value the file
// never meant. A zero threshold is a silent answer - a met line of zero
// flags nothing, a check line of zero passes every gate - so a missing
// section may never degrade to it.
func TestAnAbsentSectionIsALoadError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		key  string
		mod  func(string) string
	}{
		{"no judge section", "judge.met", cut("[judge]")},
		// The walk reports the section's first unsupplied required key,
		// so the cut check section is pinned on its prefix.
		{"no check section", "check.", cut("[check]")},
		{"a met line of zero", "judge.met", swap("met = 0.35265", "met = 0.0")},
		{"no check model", "check.model", swap(`model = "clef"`, "model = ''")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := loadVariant(t, tc.mod)
			if !errors.Is(err, keelconfig.ErrRequired) {
				t.Fatalf("err = %v, want the required-key error", err)
			}
			if !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("error does not name %s: %v", tc.key, err)
			}
		})
	}
}

// TestJudgeAndCheckLinesRefused pins the range: a nan, an inf, or a line
// outside [0, 1] fails the load naming the key, and so does an override
// that is not a line.
func TestJudgeAndCheckLinesRefused(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		want string
		mod  func(string) string
	}{
		{"met nan", "judge: met", swap("met = 0.35265", "met = nan")},
		{"met inf", "judge: met", swap("met = 0.35265", "met = inf")},
		{"met under zero", "judge: met", swap("met = 0.35265", "met = -0.1")},
		{"met over one", "judge: met", swap("met = 0.35265", "met = 1.5")},
		{"an override that is not a line", "date-beard-even", swap(
			"date-beard-even = 0.19435",
			"date-beard-even = nan",
		)},
		{"change made nan", "check: change_made", swap("change_made = 0.4978", "change_made = nan")},
		{"same person inf", "check: same_person", swap("same_person = 0.4799", "same_person = inf")},
		{"criterion met under zero", "check: criterion_met", swap("criterion_met = 0.4986", "criterion_met = -1")},
		{"criterion met over one", "check: criterion_met", swap("criterion_met = 0.4986", "criterion_met = 2")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := loadVariant(t, tc.mod)
			if err == nil {
				t.Fatal("a line off [0, 1] was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error does not name %q: %v", tc.want, err)
			}
		})
	}
}

// TestAnOverrideAndItsModelLoad pins the spellings the example does not
// use: an inline per_criterion table in place of the example's sub-table,
// and the flash model. The example's own six overrides are pinned by
// TestExampleLoads.
func TestAnOverrideAndItsModelLoad(t *testing.T) {
	t.Parallel()
	t.Run("an inline per criterion table", func(t *testing.T) {
		got, err := loadVariant(t, func(text string) string {
			without := cut("[judge.per_criterion]")(text)
			return swap("met = 0.35265", "met = 0.35265\nper_criterion = { family-earring-festive = 0.35 }")(without)
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got.Judge.Met != 0.35265 || len(got.Judge.PerCriterion) != 1 || got.Judge.PerCriterion["family-earring-festive"] != 0.35 {
			t.Fatalf("judge %+v, want the example's met and the one inline override", got.Judge)
		}
	})

	t.Run("the flash model", func(t *testing.T) {
		got, err := loadVariant(t, swap(`model = "clef"`, `model = "clef-flash"`))
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got.Check.Model != "clef-flash" {
			t.Fatalf("check model %q, want clef-flash", got.Check.Model)
		}
	})
}

// The paths section is the one the server boots from. The example
// carries the repository's own data directories, laid out the way the
// loaders require; the section stays optional at load, because the ops
// commands read the same file and need no directories. A file without
// it must still load as empty, and an operator's own values must land
// in the fields the mounts read.
func TestPathsLoad(t *testing.T) {
	t.Parallel()
	t.Run("a file without the section loads empty", func(t *testing.T) {
		data, err := os.ReadFile(examplePath(t))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		// The section is cut structurally, between its header and its
		// last value line, so a reworded comment does not move the
		// anchor.
		start := strings.Index(text, "\n[paths]\n")
		end := strings.LastIndex(text, `samples = "testdata/samples"`)
		if start < 0 || end < start {
			t.Fatalf("the example carries no [paths] section to cut: start %d end %d", start, end)
		}
		end += len(`samples = "testdata/samples"`)
		text = text[:start] + text[end:]
		path := filepath.Join(t.TempDir(), "dresscode.toml")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		got, _, err := Load(t.Context(), Options{
			Path:      path,
			LookupEnv: lookup(standins()),
			Keyring: func(context.Context, string) (string, error) {
				return "", errors.New("absent")
			},
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got.Paths.Occasions != "" || got.Paths.Samples != "" {
			t.Fatalf("paths %+v, want empty", got.Paths)
		}
	})

	t.Run("the example's paths load into the fields", func(t *testing.T) {
		got, _, err := Load(t.Context(), Options{
			Path:      examplePath(t),
			LookupEnv: lookup(standins()),
			Keyring: func(context.Context, string) (string, error) {
				return "", errors.New("absent")
			},
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got.Paths.Occasions != "testdata/occasions" || got.Paths.Samples != "testdata/samples" {
			t.Fatalf("paths %+v, want the example's two data directories", got.Paths)
		}
	})

	t.Run("an operator's paths land in the fields", func(t *testing.T) {
		got, err := loadVariant(t, func(text string) string {
			text = swap(`occasions = "testdata/occasions"`, `occasions = "/srv/dresscode/occasions"`)(text)
			return swap(`samples = "testdata/samples"`, `samples = "/srv/dresscode/samples"`)(text)
		})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got.Paths.Occasions != "/srv/dresscode/occasions" || got.Paths.Samples != "/srv/dresscode/samples" {
			t.Fatalf("paths %+v", got.Paths)
		}
	})
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
