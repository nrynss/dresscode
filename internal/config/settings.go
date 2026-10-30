package config

import (
	"fmt"
	"math"

	keelconfig "github.com/nrynss/keel/config"
	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/flag"
)

// Environment variable names. They are the names in docs/keys.md.
const (
	VarYouCamAPIKey        = "YOUCAM_API_KEY"
	VarYouCamAPISecret     = "YOUCAM_API_SECRET"
	VarWorkersAIAPIToken   = "WORKERS_AI_API_TOKEN"
	VarCloudflareAccountID = "CLOUDFLARE_ACCOUNT_ID"
	VarResendAPIKey        = "RESEND_API_KEY"
	VarResendFrom          = "RESEND_FROM"
	VarOpenRouterAPIKey    = "OPENROUTER_API_KEY"
	VarOpenRouterBaseURL   = "OPENROUTER_BASE_URL"
	VarLamboURL            = "LAMBO_URL"
	VarLamboDresscodeToken = "LAMBO_DRESSCODE_TOKEN"
)

// YouCamCredit is the cost denomination for YouCam units. One credit is
// one minor unit. Nothing converts it into dollars.
const YouCamCredit = "youcam.credit"

// Spend owners on the YouCam credit budget. Judging is the only owner
// that may spend the held-back share. Every other call uses Development,
// whose ceiling is the units minus that share.
const (
	OwnerJudging     = "judging"
	OwnerDevelopment = "development"
)

// Runtime flag names. A true value is the restriction the name states.
const (
	FlagFreshUploadsOff = "fresh_uploads_off"
	FlagRendersOff      = "renders_off"
	FlagSamplesOnly     = "samples_only"
)

// EnvVars returns the key variable names in docs/keys.md order.
func EnvVars() []string {
	return []string{
		VarYouCamAPIKey,
		VarYouCamAPISecret,
		VarWorkersAIAPIToken,
		VarCloudflareAccountID,
		VarResendAPIKey,
		VarResendFrom,
		VarOpenRouterAPIKey,
		VarOpenRouterBaseURL,
		VarLamboURL,
		VarLamboDresscodeToken,
	}
}

// Settings is one loaded settings file.
type Settings struct {
	Secrets Secrets `toml:"secrets"`
	Budgets Budgets `toml:"budgets"`
	Lease   Lease   `toml:"lease"`
	Flags   Flags   `toml:"flags"`
}

// Secrets are Keel references. The file never holds the values, a literal
// is refused, and every formatting path prints a fixed placeholder.
type Secrets struct {
	YouCamAPIKey        keelconfig.Secret `toml:"youcam_api_key" config:"required"`
	YouCamAPISecret     keelconfig.Secret `toml:"youcam_api_secret" config:"required"`
	WorkersAIAPIToken   keelconfig.Secret `toml:"workers_ai_api_token" config:"required"`
	CloudflareAccountID keelconfig.Secret `toml:"cloudflare_account_id" config:"required"`
	ResendAPIKey        keelconfig.Secret `toml:"resend_api_key" config:"required"`
	ResendFrom          keelconfig.Secret `toml:"resend_from" config:"required"`
	OpenRouterAPIKey    keelconfig.Secret `toml:"openrouter_api_key" config:"required"`
	OpenRouterBaseURL   keelconfig.Secret `toml:"openrouter_base_url" config:"required"`
	LamboURL            keelconfig.Secret `toml:"lambo_url" config:"required"`
	LamboDresscodeToken keelconfig.Secret `toml:"lambo_dresscode_token" config:"required"`
}

// Budgets are the spend ceilings.
type Budgets struct {
	YouCam    YouCamBudget    `toml:"youcam"`
	WorkersAI WorkersAIBudget `toml:"workers_ai"`
}

// YouCamBudget is the lifetime credit ceiling. JudgingUnits is the share
// held for judging. The credits do not expire.
type YouCamBudget struct {
	Units        int64 `toml:"units" config:"required"`
	JudgingUnits int64 `toml:"judging_units" config:"required"`
}

// Denomination is the credit unit this budget counts.
func (b YouCamBudget) Denomination() cost.Denomination {
	return cost.Denomination{Name: YouCamCredit, Minor: 1}
}

// Period is the window a durable store opens with. The credits do not
// expire, so the window is cost.None, one lifetime ceiling.
func (b YouCamBudget) Period() cost.Period { return cost.None }

// DevelopmentUnits is the ceiling for every call that is not a judge
// lease. It is the total minus the share held for judging.
func (b YouCamBudget) DevelopmentUnits() int64 {
	return b.Units - b.JudgingUnits
}

// Pool is the credit budget with the two owner ceilings set. The pool
// does not admit another owner: a third owner could spend the judging share.
type YouCamPool struct {
	keyed *cost.KeyedBudget
}

// Pool builds the credit budget. Judging can spend JudgingUnits.
// Development can spend the rest. Neither ceiling can spend the other,
// and the two together cannot pass Units.
func (b YouCamBudget) Pool() (YouCamPool, error) {
	if err := b.check(); err != nil {
		return YouCamPool{}, err
	}
	keyed, err := cost.NewKeyedBudgetIn(b.Denomination(), cost.Price(b.Units))
	if err != nil {
		return YouCamPool{}, err
	}
	if err := keyed.SetLimit(OwnerJudging, cost.Price(b.JudgingUnits)); err != nil {
		return YouCamPool{}, err
	}
	if err := keyed.SetLimit(OwnerDevelopment, cost.Price(b.DevelopmentUnits())); err != nil {
		return YouCamPool{}, err
	}
	return YouCamPool{keyed: keyed}, nil
}

func (b YouCamBudget) check() error {
	if b.Units < 1 || b.JudgingUnits < 1 || b.JudgingUnits > b.Units {
		return fmt.Errorf("budgets.youcam: units %d, judging share %d", b.Units, b.JudgingUnits)
	}
	return nil
}

// Development is the account every non-judge call spends against.
func (p YouCamPool) Development() (cost.ContextAccount, error) {
	return p.account(OwnerDevelopment)
}

// Judging is the account a judge lease spends against. It is the only
// account that can spend the held-back share.
func (p YouCamPool) Judging() (cost.ContextAccount, error) {
	return p.account(OwnerJudging)
}

func (p YouCamPool) account(owner string) (cost.ContextAccount, error) {
	if p.keyed == nil {
		return nil, fmt.Errorf("budgets.youcam: no pool")
	}
	return p.keyed.OwnerAccount(owner)
}

// WorkersAIBudget is the dollar ceiling for one UTC day.
type WorkersAIBudget struct {
	DollarsPerDay int64 `toml:"dollars_per_day" config:"required"`
}

// Period is the window a durable store opens with. The in-memory budget
// from Budget has no window of its own; the daily ceiling is this period.
func (b WorkersAIBudget) Period() cost.Period { return cost.DailyUTC }

// Limit is the ceiling in USD nanodollars.
func (b WorkersAIBudget) Limit() (cost.Price, error) {
	if b.DollarsPerDay < 1 {
		return 0, fmt.Errorf("budgets.workers_ai: dollars per day %d", b.DollarsPerDay)
	}
	if b.DollarsPerDay > math.MaxInt64/int64(cost.Dollar) {
		return 0, fmt.Errorf("budgets.workers_ai: dollars per day %d overflows", b.DollarsPerDay)
	}
	return cost.Price(b.DollarsPerDay) * cost.Dollar, nil
}

// Budget is the in-memory dollar ceiling. A durable store uses the same
// limit with Period.
func (b WorkersAIBudget) Budget() (*cost.Budget, error) {
	limit, err := b.Limit()
	if err != nil {
		return nil, err
	}
	return cost.NewBudget(limit)
}

// Lease is the judge session cap.
type Lease struct {
	Judge JudgeLease `toml:"judge"`
}

// JudgeLease bounds one judge session. A sample selfie with cached
// analysis is not a fresh selfie and does not count against either cap.
type JudgeLease struct {
	FreshSelfies int `toml:"fresh_selfies" config:"required"`
	Renders      int `toml:"renders" config:"required"`
}

// Flags are the restrictions an operator flips while the process runs.
// The file holds the default a missing flag row reads as.
type Flags struct {
	FreshUploadsOff bool `toml:"fresh_uploads_off"`
	RendersOff      bool `toml:"renders_off"`
	SamplesOnly     bool `toml:"samples_only"`
}

// Declarations are the keel flags. A true value refuses the thing the
// name states. The stored value, once set, wins over this default, so a
// flip takes effect on the next read without a restart.
func (f Flags) Declarations() []flag.Bool {
	return []flag.Bool{
		{Name: FlagFreshUploadsOff, Default: f.FreshUploadsOff, Help: "Refuse a fresh selfie upload."},
		{Name: FlagRendersOff, Default: f.RendersOff, Help: "Refuse a YouCam render."},
		{Name: FlagSamplesOnly, Default: f.SamplesOnly, Help: "Serve cached samples only."},
	}
}

func (s Settings) check() error {
	if err := s.Budgets.YouCam.check(); err != nil {
		return err
	}
	if _, err := s.Budgets.WorkersAI.Limit(); err != nil {
		return err
	}
	j := s.Lease.Judge
	if j.FreshSelfies < 1 || j.Renders < 1 {
		return fmt.Errorf("lease.judge: fresh selfies %d, renders %d", j.FreshSelfies, j.Renders)
	}
	return nil
}
