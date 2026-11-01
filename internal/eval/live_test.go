//go:build live

// This file is the live re-run of the judging and the render-check
// evaluation (T3.9b). The gate does not build it: `go vet ./...` and
// `go test -race ./...` skip it, and it runs only under the live tag.
//
// Run the two tests apart, never together:
//
//   go test -tags live ./internal/eval/ -run TestLiveProbe      # 2 calls, $0.01 ceiling
//   go test -tags live ./internal/eval/ -run TestLiveEvaluation # 41 calls, $0.08 ceiling
//
// `-run TestLive` matches both, and both spend: the probe's 2 calls and the
// sweep's 41, 43 paid calls in one command, across the two ceilings above
// ($0.09 combined). The probe first, always - it prices the sweep, and it
// fails if the projection reaches the sweep's ceiling or the free plan's
// daily neurons. That stop rule is the operator's precondition: a red probe
// says do not run the sweep, but what fails is the probe's own exit code,
// not a lock on the separate sweep command. What bounds a sweep that runs
// regardless is its $0.08 meter ceiling - the backstop. The meter refuses a
// call whose reserve does not fit, so a sweep books at most 8 cents:
// 80,000,000 nanodollars at the client's 11,000 nanodollars per neuron is
// ~7,272 neurons, under the free plan's 10,000-neuron day.
//
// It dials Workers AI and nothing else. No YouCam client is built here
// and no YouCam key is read. The photos are the committed sample people's
// own bytes, taken from the recordings, so each request equals the
// recorded one.
//
// Two tests, in this order:
//
//   - TestLiveProbe makes exactly one judge call and one check call, to
//     prove the key and to price one call before the sweep runs.
//   - TestLiveEvaluation makes the whole sweep: one call per judge
//     recording (37) and one per check recording (4), 41 calls.
//
// One call per sample is enough. pipeline.Thresholds and pipeline.Lines
// change only how the raw answers are read - they do not change the
// request bytes, which is what T3.9's byte-equality replay proved - so
// each call goes out at the shipped lines and the raw probabilities come
// back with it through the same capturing seam the offline evaluation
// uses. Those cells are then scored at 0.5 and at the shipped lines from
// the one read, and the sweep stays at 41 calls.
//
// The keys come from the environment only, and no key value is ever
// printed: every log line and every error this file writes goes through
// liveSecrets.scrub. A transport guard refuses any request that is not an
// https call to the real Workers AI host. One cost budget sits under every
// call, so a runaway loop stops itself before it spends further.

package eval

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/dresscode/internal/config"
	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/pipeline"
	"github.com/nrynss/dresscode/internal/systemone"
	"github.com/nrynss/keel/cost"
)

// The live run's own numbers. None of them is a threshold: the lines this
// measures with are read from the committed settings file.
const (
	// liveWorkersBase is the only address this file hands the client, and
	// liveWorkersHost the only host its transport admits.
	liveWorkersBase = "https://api.cloudflare.com"
	liveWorkersHost = "api.cloudflare.com"

	// liveProbeCeiling covers the probe's two calls. liveSweepCeiling is
	// 8 cents, about four times the sweep's expected settle (~1,950
	// neurons, ~$0.022): the client reserves double its token estimate for
	// each call, so the ceiling sits above the expected settle, not at it.
	// The meter refuses a call whose reserve does not fit, which is the
	// guard that stops a runaway loop.
	liveProbeCeiling = cost.Cent
	liveSweepCeiling = 8 * cost.Cent

	// liveFreeDailyNeurons is the Workers AI free plan's day allowance
	// (docs/clef.md, Cost). The sweep is projected against it.
	liveFreeDailyNeurons = 10_000

	// liveJudgeCalls and liveCheckCalls are the sweep's expected calls:
	// every judge recording and every check recording, once each.
	liveJudgeCalls = 37
	liveCheckCalls = 4

	liveCallTimeout   = 2 * time.Minute
	liveProbeDeadline = 6 * time.Minute
	liveSweepDeadline = 32 * time.Minute

	// liveOrdering states what the sweep does not do: no t.Parallel, one
	// call after another, so the ledger and the budget tell one story.
	liveOrdering = "sequential calls, no t.Parallel, fail fast on the first call error"
)

// The rate card this run measures against - liveNanodollarsPerNeuron,
// liveTokenCap and liveNeuronsPerMillion, with the provenance of each
// number and the tie to the client's own table - lives in
// live_report_test.go, untagged, so the gate's pinned tests check it against
// the committed transcripts as well as the live sweep reading it here.
// liveSpend.record below is the tie: it fatals the run on the first call
// where this file's implied price and the ledger's booking disagree.

// The probe's two calls: the full-size family-function judge recording and
// the honest second-visit check on the shipped model.
const (
	liveProbeJudge = "judge-V04-family-function"
	liveProbeCheck = "check-clef-V04-to-V05"
)

// liveSecrets holds the two environment values. Only scrub reads them, so
// neither can reach a log line, an error or a failure message.
type liveSecrets struct {
	token   string
	account string
}

// scrub takes the token and the account id out of text about to print.
// The account id travels in the URL path, so a transport error can carry
// it; the token never should, and does not.
func (s liveSecrets) scrub(text string) string {
	for _, secret := range []string{s.token, s.account} {
		if secret != "" && strings.Contains(text, secret) {
			text = strings.ReplaceAll(text, secret, "redacted")
		}
	}
	return text
}

// liveLogf, liveFatalf and liveErrorf are the only ways this file writes
// to the test log or builds an error. All three scrub.
func liveLogf(t *testing.T, secrets liveSecrets, format string, args ...any) {
	t.Helper()
	t.Log(secrets.scrub(fmt.Sprintf(format, args...)))
}

func liveFatalf(t *testing.T, secrets liveSecrets, format string, args ...any) {
	t.Helper()
	t.Fatal(secrets.scrub(fmt.Sprintf(format, args...)))
}

// liveErrorf builds an error whose text is already safe to print. It goes
// to t.Error as well as to liveFatalf, so a soft finding is scrubbed too.
func liveErrorf(secrets liveSecrets, format string, args ...any) error {
	return errors.New(secrets.scrub(fmt.Sprintf(format, args...)))
}

// liveCredentials reads the two names from the environment. A missing one
// skips the test and names it (the mail test's pattern), so a keyless
// `-tags live` run spends nothing and fails nothing.
func liveCredentials(t *testing.T) liveSecrets {
	t.Helper()
	token := os.Getenv(config.VarWorkersAIAPIToken)
	account := os.Getenv(config.VarCloudflareAccountID)
	switch {
	case token == "" && account == "":
		t.Skipf("live evaluation needs %s and %s in the environment",
			config.VarWorkersAIAPIToken, config.VarCloudflareAccountID)
	case token == "":
		t.Skipf("live evaluation needs %s in the environment", config.VarWorkersAIAPIToken)
	case account == "":
		t.Skipf("live evaluation needs %s in the environment", config.VarCloudflareAccountID)
	}
	return liveSecrets{token: token, account: account}
}

// liveGuardTransport refuses every request that is not an https call to
// the real Workers AI host, before the wire sees it. The client is built
// with this transport and with no ambient proxy, so no setting, no
// environment name and no slip can point this test somewhere else.
type liveGuardTransport struct {
	inner http.RoundTripper
}

func (g liveGuardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := liveGuardURL(req.URL); err != nil {
		return nil, err
	}
	return g.inner.RoundTrip(req)
}

func liveGuardURL(u *url.URL) error {
	if u == nil {
		return errors.New("live eval: the request carries no url")
	}
	if u.Scheme != "https" || u.Host != liveWorkersHost {
		return fmt.Errorf("live eval: refusing to dial %s://%s; only https://%s is allowed",
			u.Scheme, u.Host, liveWorkersHost)
	}
	if u.User != nil {
		return errors.New("live eval: the request url carries user info")
	}
	if strings.Trim(u.Path, "/") == "" || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("live eval: the request url is not an ai run path")
	}
	return nil
}

// liveNewClient builds the one live client over the budget guard.
func liveNewClient(t *testing.T, secrets liveSecrets, ceiling cost.Price) (*systemone.WorkersAI, *cost.Budget, *cost.Ledger) {
	t.Helper()
	if secrets.token == "" || secrets.account == "" {
		liveFatalf(t, secrets, "live eval: no credentials reached the client")
	}
	base, err := url.Parse(liveWorkersBase)
	if err != nil || base.Host != liveWorkersHost || base.Scheme != "https" {
		liveFatalf(t, secrets, "live eval: the built-in base url is not the workers ai host")
	}
	budget, err := cost.NewBudget(ceiling)
	if err != nil {
		liveFatalf(t, secrets, "live eval: budget: %v", err)
	}
	ledger := cost.NewLedger()
	meter, err := cost.NewMeter(budget, ledger)
	if err != nil {
		liveFatalf(t, secrets, "live eval: meter: %v", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	httpClient := &http.Client{
		Timeout:   liveCallTimeout,
		Transport: liveGuardTransport{inner: transport},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("live eval: redirect refused")
		},
	}
	client, err := systemone.NewWorkersAI(systemone.WorkersAIConfig{
		BaseURL:   liveWorkersBase,
		AccountID: secrets.account,
		Token:     secrets.token,
		Meter:     meter,
		HTTP:      httpClient,
	})
	if err != nil {
		// NewWorkersAI's errors name the field and never the value;
		// liveFatalf scrubs the text again anyway.
		liveFatalf(t, secrets, "live eval: client: %v", err)
	}
	return client, budget, ledger
}

// liveCall, the rate card and liveImpliedPrice live in
// live_report_test.go; liveSpend below builds and totals them.

// liveSpend tracks the calls made through one ledger. The calls are
// sequential, so a finished call's charge is the ledger's last entry.
type liveSpend struct {
	ledger *cost.Ledger
	budget *cost.Budget
	calls  []liveCall
}

// record books one finished call against the ledger length taken before it
// ran. This reads the meter's own booking, never an estimate of it, and it
// is where this file's rate card is tied to the money: the price the
// documented rates imply for the call's reported tokens must equal the
// price the ledger booked. A disagreement is an error, which every caller
// turns into a fatal before the next call goes out.
func (s *liveSpend) record(label, model string, before int, usage systemone.Usage) error {
	perMillion, ok := liveNeuronsPerMillion[model]
	if !ok {
		return fmt.Errorf("live eval: no documented neuron rate for model %q", model)
	}
	charges := s.ledger.Charges()
	switch {
	case len(charges) == before+1:
		if kind := charges[before].Kind; kind != "workersai."+model {
			return fmt.Errorf("live eval: %s: charge kind %q, want workersai.%s", label, kind, model)
		}
		if units := charges[before].Units; units != 1 {
			return fmt.Errorf("live eval: %s: charge booked %d units, want 1", label, units)
		}
	case len(charges) == before:
		return fmt.Errorf("live eval: %s: the call booked no charge", label)
	default:
		return fmt.Errorf("live eval: %s: %d charges booked by one call, want 1", label, len(charges)-before)
	}
	booked := charges[before].UnitPrice
	if implied := liveImpliedPrice(usage.InputTokens, perMillion); implied != booked {
		return fmt.Errorf("live eval: %s: this file's rate card implies %s for %d tokens on %q, the ledger booked %s; "+
			"a rate here no longer matches internal/systemone/workersai.go", label, implied, usage.InputTokens, model, booked)
	}
	s.calls = append(s.calls, liveCall{
		label:   label,
		model:   model,
		tokens:  usage.InputTokens,
		output:  usage.OutputTokens,
		neurons: liveNeurons(usage.InputTokens, perMillion),
		booked:  booked,
	})
	return nil
}

// liveImpliedPrice and liveNeurons - the client's arithmetic and the
// documented rate, both in live_report_test.go - are what record above
// compares the ledger against; the pinned tests re-run the same comparison
// against the transcripts' booked prices.

// The per-pass totals delegate to the lifted sums in
// live_report_test.go, so the sweep's report and the pinned tests read the
// same arithmetic.
func (s *liveSpend) tokens() int64 {
	return liveCallsTokens(s.calls)
}

func (s *liveSpend) neurons() float64 {
	return liveCallsNeurons(s.calls)
}

func (s *liveSpend) output() int64 {
	return liveCallsOutputTokens(s.calls)
}

func (s *liveSpend) dollars() cost.Price {
	return liveCallsDollars(s.calls)
}

// liveWorld is everything this file reads before it dials: the ground
// truth, the occasions and the vocabulary, the shipped lines from the
// committed settings file, and the offline report the live numbers sit
// beside.
type liveWorld struct {
	secrets   liveSecrets
	clefDir   string
	gt        GroundTruth
	occasions map[string]*domain.Occasion
	vocab     *domain.Vocabulary
	byID      map[string]Sample
	settings  JudgeSettings
	lines     pipeline.Lines
	recorded  Report
}

// livePrepare loads all of it and spends nothing. It also runs the
// zero-spend consistency check on T4.4's landing: the shipped [judge] and
// [check] numbers must equal the chosen settings the offline evaluation
// reports. A mismatch fails the run here, before liveNewClient builds a
// client and before a call, because everything after it is paid for.
func livePrepare(t *testing.T, secrets liveSecrets) liveWorld {
	t.Helper()
	root := repoRoot(t)
	gt, err := LoadGroundTruth(filepath.Join(root, "testdata", "eval", "ground-truth.json"))
	if err != nil {
		liveFatalf(t, secrets, "live eval: ground truth: %v", err)
	}
	loaded, err := domain.Load(filepath.Join(root, "testdata", "occasions"))
	if err != nil {
		liveFatalf(t, secrets, "live eval: occasions: %v", err)
	}
	occasions := make(map[string]*domain.Occasion, len(loaded.Occasions))
	for i := range loaded.Occasions {
		occ := loaded.Occasions[i]
		occasions[occ.ID] = &occ
	}
	vocab := loaded.Vocabulary
	byID := make(map[string]Sample, len(gt.Samples))
	for _, s := range gt.Samples {
		byID[s.ID] = s
	}
	world := liveWorld{
		secrets:   secrets,
		clefDir:   filepath.Join(root, "testdata", "clef"),
		gt:        gt,
		occasions: occasions,
		vocab:     &vocab,
		byID:      byID,
	}

	// The shipped lines are read, not repeated. config.Load resolves every
	// key, so the lookup below hands it a stand-in for each of the ten
	// names: this reads the settings file's numbers and nothing else, and
	// no real key value ever enters the Settings struct.
	standins := make(map[string]string, len(config.EnvVars()))
	for i, name := range config.EnvVars() {
		standins[name] = fmt.Sprintf("live-standin-%02d", i+1)
	}
	shipped, _, err := config.Load(context.Background(), config.Options{
		Path: filepath.Join(root, "dresscode.toml.example"),
		LookupEnv: func(name string) (string, bool) {
			v, ok := standins[name]
			return v, ok
		},
		Keyring: func(context.Context, string) (string, error) {
			return "", errors.New("absent")
		},
	})
	if err != nil {
		liveFatalf(t, secrets, "live eval: settings file: %v", err)
	}
	world.settings = JudgeSettings{Met: shipped.Judge.Met, PerCriterion: shipped.Judge.PerCriterion}
	world.lines = pipeline.Lines{
		ChangeMade:   shipped.Check.ChangeMade,
		SamePerson:   shipped.Check.SamePerson,
		CriterionMet: shipped.Check.CriterionMet,
	}

	// The recorded side: the offline evaluation, no key, no network, no
	// spend. It is what the live tables print against.
	recorded, err := Run(root)
	if err != nil {
		liveFatalf(t, secrets, "live eval: offline run: %v", err)
	}
	world.recorded = recorded

	liveAssertShipped(t, secrets, shipped, recorded.Settings)
	liveLogf(t, secrets, "shipped lines read from dresscode.toml.example: judge met %.5f over %d overrides; "+
		"check change_made %.5f same_person %.5f criterion_met %.5f model %q",
		shipped.Judge.Met, len(shipped.Judge.PerCriterion),
		shipped.Check.ChangeMade, shipped.Check.SamePerson, shipped.Check.CriterionMet, shipped.Check.Model)
	return world
}

// liveAssertShipped is the zero-spend consistency check on T4.4's landing:
// the committed settings file must carry exactly the settings the offline
// evaluation chooses, because the live sweep measures the shipped lines
// and calls them the shipped lines.
//
// A mismatch is fatal, and fatal here: livePrepare runs before the client is
// built, so the run stops with all 41 sweep calls still unspent. As a
// t.Error it only warned, and the sweep went on to spend money and print
// numbers under a "the shipped line" heading that were not the shipped ones.
// Every mismatch found is named in the one failure.
func liveAssertShipped(t *testing.T, secrets liveSecrets, got config.Settings, want Settings) {
	t.Helper()
	var mismatch []string
	note := func(format string, args ...any) {
		mismatch = append(mismatch, fmt.Sprintf(format, args...))
	}
	if got.Judge.Met != want.JudgeMet {
		note("shipped [judge] met is %.5f, the offline evaluation chooses %.5f", got.Judge.Met, want.JudgeMet)
	}
	if !reflect.DeepEqual(got.Judge.PerCriterion, want.JudgePerCriterion) {
		note("shipped [judge.per_criterion] is %v, the offline evaluation chooses %v",
			got.Judge.PerCriterion, want.JudgePerCriterion)
	}
	for _, line := range []struct {
		name      string
		got, want float64
	}{
		{"change_made", got.Check.ChangeMade, want.CheckChangeMade},
		{"same_person", got.Check.SamePerson, want.CheckSamePerson},
		{"criterion_met", got.Check.CriterionMet, want.CheckCriterionMet},
	} {
		if line.got != line.want {
			note("shipped [check] %s is %.5f, the offline evaluation chooses %.5f", line.name, line.got, line.want)
		}
	}
	if got.Check.Model != want.CheckModel {
		note("shipped [check] model is %q, the offline evaluation chooses %q", got.Check.Model, want.CheckModel)
	}
	if len(mismatch) > 0 {
		liveFatalf(t, secrets, "the shipped settings are not the evaluated ones, so a live sweep would report "+
			"numbers that are not the shipped line's; stopped before any call: %s", strings.Join(mismatch, "; "))
	}
	liveLogf(t, secrets, "consistency: the shipped [judge] and [check] numbers equal the offline evaluation's chosen settings: true")
}

// liveJudgeRead runs one judge recording live, at the shipped lines, and
// returns the same JudgeRead shape the offline sweep builds: the raw
// probabilities the call returned, and the biggest issue it named. The
// lines do not change the request, so this stays one call per sample.
func liveJudgeRead(ctx context.Context, world liveWorld, sys systemone.SystemOne, spend *liveSpend,
	file string) (JudgeRead, error) {
	rec, err := readRecording(file)
	if err != nil {
		return JudgeRead{}, err
	}
	parts := strings.SplitN(rec.Exchange, "-", 3)
	if len(parts) != 3 {
		return JudgeRead{}, liveErrorf(world.secrets, "%s: exchange does not name its variant", rec.Exchange)
	}
	sample, ok := world.byID[parts[1]]
	if !ok {
		return JudgeRead{}, liveErrorf(world.secrets, "%s: no ground truth for variant %q", rec.Exchange, parts[1])
	}
	state, err := stateFields(rec.Request.Body.State, "occasion")
	if err != nil {
		return JudgeRead{}, liveErrorf(world.secrets, "%s: %v", rec.Exchange, err)
	}
	if state[0] != sample.Occasion {
		return JudgeRead{}, liveErrorf(world.secrets, "%s: state names %q, ground truth says %q",
			rec.Exchange, state[0], sample.Occasion)
	}
	occ, ok := world.occasions[sample.Occasion]
	if !ok {
		return JudgeRead{}, liveErrorf(world.secrets, "%s: no occasion %q", rec.Exchange, sample.Occasion)
	}
	photo, err := readImage(world.clefDir, rec.Request.Body.Images[0])
	if err != nil {
		return JudgeRead{}, liveErrorf(world.secrets, "%s: %v", rec.Exchange, err)
	}
	before := len(spend.ledger.Charges())
	capture := &captureClient{inner: sys}
	if _, err := pipeline.Judge(ctx, capture, photo, occ, thresholdsFor(occ, world.settings)); err != nil {
		return JudgeRead{}, liveErrorf(world.secrets, "%s: %v", rec.Exchange, err)
	}
	probabilities := make(map[string]float64, len(occ.Criteria))
	for _, c := range occ.Criteria {
		answer, found := capture.resp.Answers[c.ID]
		if !found || answer.Type != systemone.TypeNoul || answer.Noul == nil {
			return JudgeRead{}, liveErrorf(world.secrets, "%s: no yes probability for %q", rec.Exchange, c.ID)
		}
		probabilities[c.ID] = *answer.Noul
	}
	issue, found := capture.resp.Answers["biggest_issue"]
	if !found || issue.Type != systemone.TypeChoice {
		return JudgeRead{}, liveErrorf(world.secrets, "%s: no biggest issue", rec.Exchange)
	}
	if model := capture.resp.Model; model != systemone.ModelClef {
		return JudgeRead{}, liveErrorf(world.secrets, "%s: the reply is from %q; judge runs on %q",
			rec.Exchange, model, systemone.ModelClef)
	}
	if err := spend.record(rec.Exchange, systemone.ModelClef, before, capture.resp.Usage); err != nil {
		return JudgeRead{}, err
	}
	return JudgeRead{
		Sample: sample, Occasion: occ, Photo: photo,
		Probabilities: probabilities, Issue: issue.Choice,
	}, nil
}

// liveJudgeReads runs the whole judge set live, one call per recording,
// sorted by variant id the way the offline read sorts it.
func liveJudgeReads(ctx context.Context, t *testing.T, world liveWorld, sys systemone.SystemOne,
	spend *liveSpend) ([]JudgeRead, error) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(world.clefDir, "judge-*.json"))
	if err != nil {
		return nil, err
	}
	if len(files) != len(world.gt.Samples) {
		return nil, liveErrorf(world.secrets, "%d judge recordings, want %d samples", len(files), len(world.gt.Samples))
	}
	sort.Strings(files)
	reads := make([]JudgeRead, 0, len(files))
	for _, file := range files {
		read, err := liveJudgeRead(ctx, world, sys, spend, file)
		if err != nil {
			return nil, err
		}
		last := spend.calls[len(spend.calls)-1]
		liveLogf(t, world.secrets, "live judge %-32s issue=%-24s tokens=%d neurons=%.2f booked=%s",
			strings.TrimSuffix(filepath.Base(file), ".json"), read.Issue, last.tokens, last.neurons, last.booked)
		reads = append(reads, read)
	}
	sort.Slice(reads, func(i, j int) bool { return reads[i].Sample.ID < reads[j].Sample.ID })
	return reads, nil
}

// liveCheckOutcome is one live check read with the decision the real
// pipeline made on it at the shipped lines.
type liveCheckOutcome struct {
	read    CheckRead
	answers systemone.Response
	shown   bool
	words   string
}

// liveCheckRead runs one check recording live at the shipped lines and
// lifts the raw gates and the whole answer set, so the same gate sweep and
// the same vocabulary scoring run on it as on the recorded read.
func liveCheckRead(ctx context.Context, world liveWorld, sys systemone.SystemOne, spend *liveSpend,
	truth CheckTruth) (liveCheckOutcome, error) {
	file := filepath.Join(world.clefDir, truth.Exchange+".json")
	rec, err := readRecording(file)
	if err != nil {
		return liveCheckOutcome{}, err
	}
	if rec.Exchange != truth.Exchange {
		return liveCheckOutcome{}, liveErrorf(world.secrets, "%s: exchange %q", truth.Exchange, rec.Exchange)
	}
	if rec.Request.Body.Model != truth.Model {
		return liveCheckOutcome{}, liveErrorf(world.secrets, "%s: model %q, ground truth says %q",
			truth.Exchange, rec.Request.Body.Model, truth.Model)
	}
	state, err := stateFields(rec.Request.Body.State, "occasion", "requested_change", "criterion")
	if err != nil {
		return liveCheckOutcome{}, liveErrorf(world.secrets, "%s: %v", truth.Exchange, err)
	}
	occ, ok := world.occasions[state[0]]
	if !ok {
		return liveCheckOutcome{}, liveErrorf(world.secrets, "%s: no occasion %q", truth.Exchange, state[0])
	}
	if len(rec.Request.Body.Images) != 2 {
		return liveCheckOutcome{}, liveErrorf(world.secrets, "%s: %d images, want the original and the render",
			truth.Exchange, len(rec.Request.Body.Images))
	}
	original, err := readImage(world.clefDir, rec.Request.Body.Images[0])
	if err != nil {
		return liveCheckOutcome{}, liveErrorf(world.secrets, "%s: %v", truth.Exchange, err)
	}
	render, err := readImage(world.clefDir, rec.Request.Body.Images[1])
	if err != nil {
		return liveCheckOutcome{}, liveErrorf(world.secrets, "%s: %v", truth.Exchange, err)
	}
	fix := fixFor(occ, state[2])
	if fix == nil {
		return liveCheckOutcome{}, liveErrorf(world.secrets, "%s: no fix on %q addresses %q",
			truth.Exchange, occ.ID, state[2])
	}
	before := len(spend.ledger.Charges())
	capture := &captureClient{inner: sys}
	outcome, err := pipeline.Check(ctx, capture, truth.Model, original, render, occ, fix, state[2], state[1],
		world.vocab, world.lines)
	if err != nil {
		return liveCheckOutcome{}, liveErrorf(world.secrets, "%s: %v", truth.Exchange, err)
	}
	change, err := gate(capture.resp, "change_made")
	if err != nil {
		return liveCheckOutcome{}, liveErrorf(world.secrets, "%s: %v", truth.Exchange, err)
	}
	same, err := gate(capture.resp, "same_person")
	if err != nil {
		return liveCheckOutcome{}, liveErrorf(world.secrets, "%s: %v", truth.Exchange, err)
	}
	met, err := gate(capture.resp, "criterion_met")
	if err != nil {
		return liveCheckOutcome{}, liveErrorf(world.secrets, "%s: %v", truth.Exchange, err)
	}
	if model := capture.resp.Model; model != truth.Model {
		return liveCheckOutcome{}, liveErrorf(world.secrets, "%s: the reply is from %q, the recording is %q",
			truth.Exchange, model, truth.Model)
	}
	if err := spend.record(truth.Exchange, truth.Model, before, capture.resp.Usage); err != nil {
		return liveCheckOutcome{}, err
	}
	return liveCheckOutcome{
		read: CheckRead{
			Exchange: truth.Exchange, Model: truth.Model, Bad: truth.Bad,
			Change: change, Same: same, Met: met,
		},
		answers: capture.resp,
		shown:   outcome.Shown(),
		words:   outcome.Words,
	}, nil
}

// liveCheckReads runs every recorded check live, once each, in the same
// exchange order the offline read uses.
func liveCheckReads(ctx context.Context, t *testing.T, world liveWorld, sys systemone.SystemOne,
	spend *liveSpend) ([]CheckRead, map[string]systemone.Response, []liveCheckOutcome, error) {
	t.Helper()
	truths := append([]CheckTruth(nil), world.gt.Checks...)
	sort.Slice(truths, func(i, j int) bool { return truths[i].Exchange < truths[j].Exchange })
	reads := make([]CheckRead, 0, len(truths))
	answers := make(map[string]systemone.Response, len(truths))
	outcomes := make([]liveCheckOutcome, 0, len(truths))
	for _, truth := range truths {
		outcome, err := liveCheckRead(ctx, world, sys, spend, truth)
		if err != nil {
			return nil, nil, nil, err
		}
		last := spend.calls[len(spend.calls)-1]
		liveLogf(t, world.secrets, "live check %-36s change=%.4f same=%.4f met=%.4f shown=%-5v tokens=%d booked=%s",
			truth.Exchange, outcome.read.Change, outcome.read.Same, outcome.read.Met, outcome.shown,
			last.tokens, last.booked)
		reads = append(reads, outcome.read)
		answers[truth.Exchange] = outcome.answers
		outcomes = append(outcomes, outcome)
	}
	return reads, answers, outcomes, nil
}

// liveVocabularyCells scores the live look answers against each render's
// truth, in the vocabulary's own order - the same shape and the same rules
// the offline check report builds.
//
// Those rules are a copy, not a call: this mirrors internal/eval/check.go:
// 325-344 (sort the check truths by exchange, then one cell per attribute in
// the vocabulary's order, correct = answer equals the render's truth). The
// extraction that would remove the duplication is a product-file edit and
// outside this task, so the copy stands. Read it as a standing risk: an edit
// to check.go:325-344 that does not come here leaves the live vocabulary
// scored by different rules than the recorded one, and the only thing that
// shows it is this file's own cell-by-cell comparison against the recorded
// cells.
func liveVocabularyCells(world liveWorld, answers map[string]systemone.Response) ([]VocabularyCell, error) {
	truths := append([]CheckTruth(nil), world.gt.Checks...)
	sort.Slice(truths, func(i, j int) bool { return truths[i].Exchange < truths[j].Exchange })
	var cells []VocabularyCell
	for _, truth := range truths {
		resp, ok := answers[truth.Exchange]
		if !ok {
			return nil, liveErrorf(world.secrets, "%s: no captured answers", truth.Exchange)
		}
		for _, attr := range world.vocab.Attributes {
			answer, found := resp.Answers["look."+attr.ID]
			if !found || answer.Type != systemone.TypeChoice {
				return nil, liveErrorf(world.secrets, "%s: no choice for %q", truth.Exchange, attr.ID)
			}
			cells = append(cells, VocabularyCell{
				Exchange:  truth.Exchange,
				Attribute: attr.ID,
				Answer:    answer.Choice,
				Truth:     truth.Look[attr.ID],
				Correct:   answer.Choice == truth.Look[attr.ID],
				Note:      truth.Notes[attr.ID],
			})
		}
	}
	return cells, nil
}

// TestLiveProbe makes exactly two live calls - one judge and one check - to
// prove the key works and to price one call of each before the 41-call
// sweep. Run it first: its last lines project the sweep's cost against the
// sweep's ceiling and against the free plan's daily neuron allowance, and a
// projection that reaches either limit fails this test. That fail is the
// operator's precondition for running the sweep - a red probe says do not
// run it - while the sweep's own $0.08 meter ceiling is the backstop that
// bounds a run at ~7,272 neurons, under the 10,000-neuron free day, even if
// the sweep is run when the probe said otherwise.
//
// It reads the shipped lines and the recordings exactly as the sweep does,
// but reports nothing about agreement: two calls cannot measure that, and
// the sweep is the measurement.
func TestLiveProbe(t *testing.T) {
	secrets := liveCredentials(t)
	world := livePrepare(t, secrets)
	client, budget, ledger := liveNewClient(t, secrets, liveProbeCeiling)
	spend := &liveSpend{ledger: ledger, budget: budget}

	ctx, cancel := context.WithTimeout(context.Background(), liveProbeDeadline)
	defer cancel()

	judge, err := liveJudgeRead(ctx, world, client, spend, filepath.Join(world.clefDir, liveProbeJudge+".json"))
	if err != nil {
		liveFatalf(t, secrets, "live probe judge call: %v", err)
	}
	var checkTruth *CheckTruth
	for i := range world.gt.Checks {
		if world.gt.Checks[i].Exchange == liveProbeCheck {
			checkTruth = &world.gt.Checks[i]
		}
	}
	if checkTruth == nil {
		liveFatalf(t, secrets, "live probe: no ground truth for %q", liveProbeCheck)
	}
	check, err := liveCheckRead(ctx, world, client, spend, *checkTruth)
	if err != nil {
		liveFatalf(t, secrets, "live probe check call: %v", err)
	}

	if len(spend.calls) != 2 {
		liveFatalf(t, secrets, "the probe made %d calls, want 2", len(spend.calls))
	}
	liveLogf(t, secrets, "probe: two calls, one judge and one check, %s", liveOrdering)
	liveLogf(t, secrets, "PROBE COST (response usage -> documented rate -> ledger booking)")
	liveLogf(t, secrets, "  %-32s %-11s %8s %10s %14s", "call", "model", "tokens", "neurons", "booked")
	for _, c := range spend.calls {
		liveLogf(t, secrets, "  %-32s %-11s %8d %10.2f %14s", c.label, c.model, c.tokens, c.neurons, c.booked)
	}
	liveLogf(t, secrets, "probe judge %s read %d criteria and named the biggest issue %q",
		liveProbeJudge, len(judge.Probabilities), judge.Issue)
	liveLogf(t, secrets, "probe check %s gates %.4f / %.4f / %.4f; shown at the shipped lines: %v",
		liveProbeCheck, check.read.Change, check.read.Same, check.read.Met, check.shown)

	// The projection. The sweep is 37 judge calls on clef, 2 checks on
	// clef and 2 on clef-flash; the flash recordings send the same bytes
	// as the clef ones, so the probe's token count prices them at the
	// flash rate.
	judgeCall, checkCall := spend.calls[0], spend.calls[1]
	flashRate, ok := liveNeuronsPerMillion[systemone.ModelClefFlash]
	if !ok {
		liveFatalf(t, secrets, "no documented neuron rate for %q", systemone.ModelClefFlash)
	}
	flashCheck := liveNeurons(checkCall.tokens, flashRate)
	projectedNeurons, projectedDollars := projectLiveSweep(judgeCall.neurons, checkCall.neurons, flashCheck,
		liveJudgeCalls, liveCheckCalls/2, liveCheckCalls/2)
	liveLogf(t, secrets, "  projected sweep: %d judge x %.2f + 2 clef check x %.2f + 2 flash check x %.2f = %.0f neurons = %s",
		liveJudgeCalls, judgeCall.neurons, checkCall.neurons, flashCheck, projectedNeurons, projectedDollars)
	// The projection is checked against both limits here, and a reach fails
	// this test: that is the operator's precondition for the sweep, not a
	// lock on it. The sweep's meter ceiling is the backstop that bounds a
	// run whatever the probe printed.
	if projectedDollars >= liveSweepCeiling {
		liveFatalf(t, secrets, "the projected sweep %s reaches the sweep ceiling %s; the sweep does not run",
			projectedDollars, cost.Price(liveSweepCeiling))
	}
	liveLogf(t, secrets, "  sweep ceiling %s; the projection fits under it: true", cost.Price(liveSweepCeiling))
	if projectedNeurons >= float64(liveFreeDailyNeurons) {
		liveFatalf(t, secrets, "the projected sweep %.0f neurons reaches the free plan's %d neuron day; the sweep does not run",
			projectedNeurons, liveFreeDailyNeurons)
	}
	liveLogf(t, secrets, "  free plan allows %d neurons a day; the projection fits under it: true", liveFreeDailyNeurons)
	total, err := ledger.Total()
	if err != nil {
		liveFatalf(t, secrets, "ledger total: %v", err)
	}
	liveLogf(t, secrets, "  probe booked %d charges, total %s; budget spent %s, remaining %s",
		len(ledger.Charges()), total, budget.Spent(), liveRemaining(t, secrets, budget))
}

// liveRemaining reads the budget's headroom. It errors only on a
// denomination mismatch this test cannot cause.
func liveRemaining(t *testing.T, secrets liveSecrets, budget *cost.Budget) cost.Price {
	t.Helper()
	remaining, err := budget.Remaining()
	if err != nil {
		liveFatalf(t, secrets, "budget remaining: %v", err)
	}
	return remaining
}

// TestLiveEvaluation re-runs the judging and the render-check evaluation
// live, once: one call per judge recording (37) and one per check
// recording (4), then the side-by-side report against the offline
// evaluation's recorded numbers. Workers AI only; no YouCam call of any
// kind. Run TestLiveProbe first, and only run this when its projection
// fits the ceiling and the daily allowance.
func TestLiveEvaluation(t *testing.T) {
	secrets := liveCredentials(t)
	world := livePrepare(t, secrets)
	client, budget, ledger := liveNewClient(t, secrets, liveSweepCeiling)
	spend := &liveSpend{ledger: ledger, budget: budget}

	ctx, cancel := context.WithTimeout(context.Background(), liveSweepDeadline)
	defer cancel()

	liveLogf(t, secrets, "live sweep: %d judge calls and %d check calls on Workers AI, %s",
		liveJudgeCalls, liveCheckCalls, liveOrdering)
	judgeReads, err := liveJudgeReads(ctx, t, world, client, spend)
	if err != nil {
		liveFatalf(t, secrets, "live judging: %v", err)
	}
	checkReads, checkAnswers, checkOutcomes, err := liveCheckReads(ctx, t, world, client, spend)
	if err != nil {
		liveFatalf(t, secrets, "live render check: %v", err)
	}
	if len(spend.calls) != liveJudgeCalls+liveCheckCalls {
		liveFatalf(t, secrets, "the sweep made %d calls, want %d", len(spend.calls), liveJudgeCalls+liveCheckCalls)
	}
	vocabulary, err := liveVocabularyCells(world, checkAnswers)
	if err != nil {
		liveFatalf(t, secrets, "live vocabulary: %v", err)
	}

	liveReportJudge(t, world, judgeReads)
	liveReportCheck(t, world, checkReads, checkOutcomes, vocabulary)
	liveReportCost(t, world, spend, ledger)
}

// liveReportJudge prints the live judging beside the recorded, at 0.5 and
// at the shipped lines: the totals, each criterion's curve, every cell that
// moved, every cell that crossed the shipped line, and the biggest-issue
// count.
func liveReportJudge(t *testing.T, world liveWorld, reads []JudgeRead) {
	t.Helper()
	secrets := world.secrets
	recorded := world.recorded.Judge

	if len(reads) != liveJudgeCalls {
		t.Error(liveErrorf(secrets, "live judging read %d samples, want %d", len(reads), liveJudgeCalls))
	}
	liveCurves, err := buildCurves(reads, world.occasions)
	if err != nil {
		liveFatalf(t, secrets, "live curves: %v", err)
	}
	// The judging arithmetic is the lifted, pure one from
	// live_report_test.go: the same totals, the same named cells, the same
	// moved and flipped lines, and a live curve or cell with no recorded
	// twin an error the run stops on, not a zero that prints.
	nums, recValues, err := computeLiveJudgeNumbers(liveCurves, recorded, world.settings)
	if err != nil {
		liveFatalf(t, secrets, "live judging numbers: %v", err)
	}
	drift, err := measureLiveDrift(liveCurves, recValues)
	if err != nil {
		liveFatalf(t, secrets, "live drift: %v", err)
	}

	liveLogf(t, secrets, "")
	liveLogf(t, secrets, "JUDGING: live against recorded (%d samples, %d cells)", len(reads), recorded.Cells)
	liveLogf(t, secrets, "  %-46s %-14s %s", "reading", "recorded", "live")
	liveLogf(t, secrets, "  %-46s %-14s %d/%d", "cells agreeing at 0.5",
		fmt.Sprintf("%d/%d", recorded.AgreeAtHalf, recorded.Cells), nums.LiveHalf, recorded.Cells)
	liveLogf(t, secrets, "  %-46s %-14s %d/%d", "cells agreeing at the shipped lines",
		fmt.Sprintf("%d/%d", recorded.ChosenTotal, recorded.Cells), nums.LiveLines, recorded.Cells)
	liveLogf(t, secrets, "  %-46s %-14s %d/%d", "curves' best total (no line beats it)",
		fmt.Sprintf("%d/%d", nums.RecBestTotal, recorded.Cells), nums.LiveMaxTotal, recorded.Cells)
	liveLogf(t, secrets, "  %-46s %-14s %d/%d", "criteria separating perfectly",
		fmt.Sprintf("%d/%d", nums.RecApart, len(recorded.Curves)), nums.LiveApart, len(liveCurves))
	liveLogf(t, secrets, "  %-46s %-14s %d/%d", "biggest issue names a miss (or none)",
		fmt.Sprintf("%d/%d", recorded.Issues.Named, recorded.Issues.Of), liveIssues(reads), len(reads))
	liveLogf(t, secrets, "  cells whose live value differs from the recorded one: %d of %d",
		len(nums.Moved), recorded.Cells)
	liveLogf(t, secrets, "  cells that sit on the other side of the shipped line live than recorded: %d", len(nums.Flipped))
	liveLogf(t, secrets, "  drift: mean |move| %.4f over %d cells; %d inside 0.05, %d at 0.10 or more, %d past 0.45",
		drift.MeanMove, drift.Cells, drift.Inside05, drift.AtLeast10, drift.Past045)
	liveLogf(t, secrets, "  largest move: %s recorded %.4f, live %.4f", drift.LargestCell, drift.LargestFrom, drift.LargestTo)
	if len(drift.Identical) > 0 {
		liveLogf(t, secrets, "  cells identical live and recorded: %s", strings.Join(drift.Identical, ", "))
	}

	// Per criterion: the recorded curve's best against the live curve's
	// best, and the shipped line's agreement on each side.
	liveLogf(t, secrets, "")
	liveLogf(t, secrets, "  PER CRITERION (n = cells; best = the most agreement any line reads; line = shipped)")
	liveLogf(t, secrets, "  %-32s %3s %10s %10s %11s %11s %10s %11s",
		"criterion", "n", "rec best", "live best", "rec at line", "live at line", "line", "apart r/l")
	for _, c := range liveCurves {
		rec := nums.RecCurves[c.ID]
		line := world.settings.lineFor(c)
		recAt, liveAt := agreement(rec.Cells, line), agreement(c.Cells, line)
		mark := ""
		if recAt != liveAt || rec.MaxAgreement != c.MaxAgreement || rec.Apart != c.Apart {
			mark = "   <-- differs from recorded"
		}
		liveLogf(t, secrets, "  %-32s %3d %6d/%-3d %6d/%-3d %8d/%-3d %8d/%-3d %10.5f %5t/%-5t%s",
			c.ID, len(c.Cells),
			rec.MaxAgreement, len(c.Cells),
			c.MaxAgreement, len(c.Cells),
			recAt, len(c.Cells),
			liveAt, len(c.Cells),
			line, rec.Apart, c.Apart, mark)
	}

	// The named cells: the 0.5 anchors, the cells no line saves, every
	// value that moved, and every cell that crossed the shipped line.
	liveLogf(t, secrets, "")
	liveLogf(t, secrets, "  CELLS OFF THE EXPECTED SIDE AT 0.5 - recorded %d, live %d",
		len(recorded.OffAtHalf), len(nums.OffHalf))
	liveLogf(t, secrets, "    recorded (docs/clef.md's thirteen):")
	livePrintCells(t, secrets, "      ", recorded.OffAtHalf)
	liveLogf(t, secrets, "    live:")
	livePrintCells(t, secrets, "      ", nums.OffHalf)
	liveCompareCells(t, secrets, "0.5", recorded.OffAtHalf, nums.OffHalf)

	liveLogf(t, secrets, "")
	liveLogf(t, secrets, "  IRREDUCIBLE CELLS AT THE SHIPPED LINE - recorded %d, live %d (no line on this set saves them)",
		len(recorded.Wrong), len(nums.Wrong))
	liveLogf(t, secrets, "    recorded:")
	livePrintCells(t, secrets, "      ", recorded.Wrong)
	liveLogf(t, secrets, "    live:")
	livePrintCells(t, secrets, "      ", nums.Wrong)
	liveCompareCells(t, secrets, "the shipped line", recorded.Wrong, nums.Wrong)

	liveLogf(t, secrets, "")
	liveLogf(t, secrets, "  LIVE VALUES THAT DIFFER FROM THE RECORDED ONES: %d of %d cells", len(nums.Moved), recorded.Cells)
	for _, m := range nums.Moved {
		liveLogf(t, secrets, "    %s", m)
	}
	liveLogf(t, secrets, "  CELLS ON THE OTHER SIDE OF THE SHIPPED LINE LIVE THAN RECORDED: %d", len(nums.Flipped))
	for _, f := range nums.Flipped {
		liveLogf(t, secrets, "    %s", f)
	}

	// The raw evidence: every live cell beside its recorded twin and the
	// line it reads against. A probability never reaches a user-facing
	// screen; this is a test log.
	liveLogf(t, secrets, "")
	liveLogf(t, secrets, "  EVERY CELL, LIVE BESIDE RECORDED (line = the shipped line it reads against)")
	for _, c := range liveCurves {
		line := world.settings.lineFor(c)
		for _, cell := range c.Cells {
			liveLogf(t, secrets, "    %-30s %-28s recorded=%.4f live=%.4f line=%.5f expected-met=%-5v live-agrees=%-5v",
				cell.Sample, c.ID, recValues[cell.Sample+" "+c.ID], cell.Value, line, cell.Expected,
				(cell.Value >= line) == cell.Expected)
		}
	}
}

// livePrintCells logs one list of off-cells, each with the value that
// put it on the wrong side and what the ground truth expected.
func livePrintCells(t *testing.T, secrets liveSecrets, indent string, cells []OffCell) {
	t.Helper()
	if len(cells) == 0 {
		liveLogf(t, secrets, "%s(none)", indent)
		return
	}
	for _, c := range cells {
		liveLogf(t, secrets, "%s%s %-28s value=%.4f expected-met=%-5v", indent, c.Sample, c.Criterion, c.Value, c.Expected)
	}
}

// liveCompareCells says whether the live list names exactly the cells the
// recorded list names, and which side each list has that the other lacks.
func liveCompareCells(t *testing.T, secrets liveSecrets, where string, recorded, live []OffCell) {
	t.Helper()
	key := func(c OffCell) string { return c.Sample + " " + c.Criterion }
	inRecorded := make(map[string]OffCell, len(recorded))
	for _, c := range recorded {
		inRecorded[key(c)] = c
	}
	inLive := make(map[string]OffCell, len(live))
	for _, c := range live {
		inLive[key(c)] = c
	}
	var onlyLive, onlyRecorded []string
	for k, c := range inLive {
		if _, ok := inRecorded[k]; !ok {
			onlyLive = append(onlyLive, fmt.Sprintf("%s %s (live %.4f, expected met %v)", c.Sample, c.Criterion, c.Value, c.Expected))
		}
	}
	for k, c := range inRecorded {
		if _, ok := inLive[k]; !ok {
			onlyRecorded = append(onlyRecorded, fmt.Sprintf("%s %s (recorded %.4f, expected met %v)", c.Sample, c.Criterion, c.Value, c.Expected))
		}
	}
	sort.Strings(onlyLive)
	sort.Strings(onlyRecorded)
	reproduced := len(onlyLive) == 0 && len(onlyRecorded) == 0
	liveLogf(t, secrets, "    the cells off the expected side at %s reproduce exactly: %v", where, reproduced)
	for _, entry := range onlyLive {
		liveLogf(t, secrets, "      off at %s live only: %s", where, entry)
	}
	for _, entry := range onlyRecorded {
		liveLogf(t, secrets, "      off at %s recorded only: %s", where, entry)
	}
}

// liveReportCheck prints the live render check beside the recorded: the
// gate values per model and per read, whether each gate still separates the
// honest render from the known bad proxy, the margin each shipped line has
// against the live reads, the decisions at the shipped lines, and the look
// vocabulary with its misses named.
func liveReportCheck(t *testing.T, world liveWorld, reads []CheckRead,
	outcomes []liveCheckOutcome, vocabulary []VocabularyCell) {
	t.Helper()
	secrets := world.secrets
	recorded := world.recorded.Check

	recReads := map[string]CheckRead{}
	for _, r := range recorded.Reads {
		recReads[r.Exchange] = r
	}

	liveLogf(t, secrets, "")
	liveLogf(t, secrets, "RENDER CHECK: live against recorded (the honest pair and the known bad proxy, per model)")
	liveLogf(t, secrets, "  %-36s %-12s %6s %9s %9s %9s   %s",
		"read", "model", "bad", "change", "same", "met", "recorded change / same / met")
	for _, r := range reads {
		rec, ok := recReads[r.Exchange]
		if !ok {
			liveFatalf(t, secrets, "no recorded read for %q", r.Exchange)
		}
		liveLogf(t, secrets, "  %-36s %-12s %6v %9.4f %9.4f %9.4f   %.4f / %.4f / %.4f",
			r.Exchange, r.Model, r.Bad, r.Change, r.Same, r.Met, rec.Change, rec.Same, rec.Met)
	}

	// Per gate and model: does the shipped line still separate the honest
	// render from the known bad proxy, and with what margin against the
	// live reads? The live curves' own plateau midpoint prints beside it,
	// so a re-measurement can see where the live set would sit. The table's
	// arithmetic is the lifted one (live_report_test.go): a missing read
	// pair or recorded row is an error, fatal here, not a zero margin.
	rows, err := computeLiveGateRows(reads, recorded, world.lines)
	if err != nil {
		liveFatalf(t, secrets, "live gate rows: %v", err)
	}
	liveLogf(t, secrets, "")
	liveLogf(t, secrets, "  GATES (separates = the honest render at or above the line, the known bad render under it)")
	liveLogf(t, secrets, "  %-14s %-12s %8s %8s %10s %8s %10s %8s %10s %10s",
		"gate", "model", "honest", "bad", "line", "margin", "separates", "at 0.5", "live mid", "rec margin")
	for _, row := range rows {
		mark := ""
		if row.Differs {
			mark = "   <-- differs from recorded"
		}
		liveLogf(t, secrets, "  %-14s %-12s %8.4f %8.4f %10.5f %8.4f %10v %8v %10.5f %10.4f%s",
			row.Gate, row.Model, row.Honest, row.Bad, row.Line, row.Margin,
			row.Separates, row.SeparatesHalf, row.LiveMid, row.RecMargin, mark)
	}

	// The decisions at the shipped lines, as the real pipeline made them.
	decisions := make([]liveCheckDecision, 0, len(outcomes))
	for _, o := range outcomes {
		decisions = append(decisions, liveCheckDecision{
			Exchange: o.read.Exchange, Model: o.read.Model,
			Bad: o.read.Bad, Shown: o.shown, Words: o.words,
		})
	}
	badShown, goodDiscarded := liveDecisionCounts(decisions)
	liveLogf(t, secrets, "")
	liveLogf(t, secrets, "  DECISIONS AT THE SHIPPED LINES (through pipeline.Check on the live call)")
	liveLogf(t, secrets, "    recorded: known bad renders shown %d, honest renders discarded %d",
		recorded.BadShown, recorded.GoodDiscarded)
	liveLogf(t, secrets, "    live:     known bad renders shown %d, honest renders discarded %d",
		badShown, goodDiscarded)
	liveLogf(t, secrets, "    every known bad render rejected and no honest render lost - recorded %v, live %v",
		recorded.BadShown == 0 && recorded.GoodDiscarded == 0, badShown == 0 && goodDiscarded == 0)
	recOutcomes := map[string]CheckOutcome{}
	for _, o := range recorded.Outcomes {
		recOutcomes[o.Exchange] = o
	}
	for _, o := range outcomes {
		rec, ok := recOutcomes[o.read.Exchange]
		if !ok {
			liveFatalf(t, secrets, "no recorded outcome for %q", o.read.Exchange)
		}
		mark := ""
		if rec.Shown != o.shown || rec.Bad != o.read.Bad {
			mark = "   <-- differs from recorded"
		}
		liveLogf(t, secrets, "    %-36s %-12s bad=%-5v shown live=%-5v shown recorded=%-5v%s words=%q",
			o.read.Exchange, o.read.Model, o.read.Bad, o.shown, rec.Shown, mark, o.words)
	}

	// The look vocabulary: correct cells per model, every answer beside
	// the recorded one, and the misses named. The counting and the
	// cell-by-cell comparison are lifted (live_report_test.go): an answer
	// whose exchange has no ground truth check, or a live cell with no
	// recorded twin, is an error this run fatals on - the shape its
	// neighbours fatal in, not a silent skip or a blank printed beside.
	modelOf := liveModelByExchange(world.gt.Checks)
	tallies, err := tallyLiveVocabulary(liveTruthModels(world.gt.Checks), vocabulary, recorded.Vocabulary.Cells, modelOf)
	if err != nil {
		liveFatalf(t, secrets, "live vocabulary tally: %v", err)
	}
	diffs, err := diffLiveVocabulary(vocabulary, recorded.Vocabulary.Cells)
	if err != nil {
		liveFatalf(t, secrets, "live vocabulary diff: %v", err)
	}
	diffKeys := make(map[string]bool, len(diffs))
	for _, d := range diffs {
		diffKeys[d.Exchange+" "+d.Attribute] = true
	}
	recVocab := make(map[string]VocabularyCell, len(recorded.Vocabulary.Cells))
	for _, c := range recorded.Vocabulary.Cells {
		recVocab[c.Exchange+" "+c.Attribute] = c
	}
	liveLogf(t, secrets, "")
	liveLogf(t, secrets, "  LOOK VOCABULARY (%d cells: %d attributes over %d reads)",
		len(vocabulary), len(world.vocab.Attributes), len(world.gt.Checks))
	for _, ta := range tallies {
		liveLogf(t, secrets, "    %-12s live %d/%d correct, recorded %d/%d",
			ta.Model, ta.LiveCorrect, ta.LiveOf, ta.RecCorrect, ta.RecOf)
	}
	liveLogf(t, secrets, "    every attribute cell, live beside recorded:")
	for _, c := range vocabulary {
		key := c.Exchange + " " + c.Attribute
		mark := ""
		if diffKeys[key] {
			mark = "   <-- differs from recorded"
		}
		liveLogf(t, secrets, "      %-36s %-16s truth=%-18s live=%-18s recorded=%-18s correct=%-5v%s",
			c.Exchange, c.Attribute, c.Truth, c.Answer, recVocab[key].Answer, c.Correct, mark)
	}
	liveMisses := liveVocabularyMisses(vocabulary)
	recMisses := liveVocabularyMisses(recorded.Vocabulary.Cells)
	liveLogf(t, secrets, "    recorded misses:")
	for _, m := range recMisses {
		liveLogf(t, secrets, "      %s", m)
	}
	liveLogf(t, secrets, "    live misses:")
	for _, m := range liveMisses {
		liveLogf(t, secrets, "      %s", m)
	}
	if reflect.DeepEqual(liveMisses, recMisses) {
		liveLogf(t, secrets, "    the recorded vocabulary misses reproduce exactly")
	} else {
		liveLogf(t, secrets, "    the vocabulary misses differ from the recorded ones")
	}
}

// liveReportCost prints what the sweep cost: calls, the input tokens the
// responses reported, the neurons and dollars they imply, the ledger's
// total, and the budget the run finished inside.
func liveReportCost(t *testing.T, world liveWorld, spend *liveSpend, ledger *cost.Ledger) {
	t.Helper()
	secrets := world.secrets
	liveLogf(t, secrets, "")
	liveLogf(t, secrets, "COST: the live sweep (Workers AI only; no YouCam call was made, no YouCam key read)")
	liveLogf(t, secrets, "  %-36s %-12s %8s %10s %14s", "call", "model", "tokens", "neurons", "booked")
	for _, c := range spend.calls {
		liveLogf(t, secrets, "  %-36s %-12s %8d %10.2f %14s", c.label, c.model, c.tokens, c.neurons, c.booked)
	}
	total, err := ledger.Total()
	if err != nil {
		liveFatalf(t, secrets, "ledger total: %v", err)
	}
	liveLogf(t, secrets, "  calls made: %d (%s)", len(spend.calls), liveCounted(liveCallsByModel(spend.calls)))
	liveLogf(t, secrets, "  input tokens reported: %d, output tokens %d",
		spend.tokens(), spend.output())
	liveLogf(t, secrets, "  neurons implied by the documented rates: %.0f", spend.neurons())
	liveLogf(t, secrets, "  dollars booked by the ledger: %s (sum of the calls %s)", total, spend.dollars())
	liveLogf(t, secrets, "  budget ceiling %s, spent %s, remaining %s",
		cost.Price(liveSweepCeiling), spend.budget.Spent(), liveRemaining(t, secrets, spend.budget))
	liveLogf(t, secrets, "  free plan daily allowance %d neurons; the sweep stayed under it: %v",
		liveFreeDailyNeurons, spend.neurons() < liveFreeDailyNeurons)
	liveLogf(t, secrets, "  the offline evaluation replays the same recordings with zero calls and zero spend")
}
