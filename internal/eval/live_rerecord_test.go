//go:build live

// T3.9e's paid pass: re-record the whole corpus against live Cloudflare
// Workers AI, with T3.9d's recorder doing the storing.
//
// This file is the spending half. The plan, its projection, the ceiling
// arithmetic and the whole offline proof are in rerecord_test.go, untagged,
// so the gate runs them on every commit and this driver only has to dial.
// It reuses this package's existing live harness - liveCredentials,
// liveGuardTransport through liveNewClient, liveSpend's ledger tie - and
// adds no fourth way to reach Workers AI.
//
// Run it alone, and only through the live wrapper:
//
//	DRESSCODE_RERECORD=yes /tmp/dc-live-run.sh <worktree> \
//	  -tags live -run TestLiveRerecord -v -timeout 90m
//
// -run must name this test and nothing else: the T3.9b probe and sweep in
// live_test.go spend on their own ceilings, and a bare -run TestLive would
// run all three in one command.
//
// The order below is the stop rule, and every step before the client is
// zero-spend:
//
//  1. the credentials, or a skip;
//  2. the paid gate: without DRESSCODE_RERECORD=yes this refuses;
//  3. livePrepare: the shipped settings must equal the evaluated ones, so
//     the pass is not recorded against a corpus whose pins already moved;
//  4. the plan: the exact 55 calls, printed - exchange, folder, model, the
//     input tokens the recording being replaced reported;
//  5. the byte-identity proof, re-run here, so what is about to go out on
//     money is what the corpus holds, at spend time and not only at gate
//     time;
//  6. the projection, priced from internal/systemone's own rate card, and
//     the ceiling check - a fatal, before the client exists, so a pass that
//     would overspend makes no call at all;
//  7. the pass itself: sequential, no t.Parallel, fail fast.
//
// A call already on disk in testdata/clef-fresh is the resume signal: the
// recorder refuses it before the inner Ask, so a re-run of a half-finished
// pass spends nothing on what is already there and pays only for the rest.
// The ceiling is computed over the still-unrecorded calls for exactly that
// reason, while the call ceiling counts the whole pass either way.
//
// Nothing here dials except Workers AI: the transport guard lives in
// liveNewClient and refuses any other scheme, host, user info or redirect.
// No YouCam client is built, no YouCam key is read, no Resend, no
// OpenRouter. Every line this file writes goes through liveSecrets.scrub,
// so neither the token nor the account id can reach a log, an error or a
// committed transcript.

package eval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/dresscode/internal/systemone"
	"github.com/nrynss/keel/cost"
)

// liveRerecordDeadline bounds the whole pass. 55 sequential calls on
// models that answer in seconds do not need it; it is there so a stalled
// request stops the run rather than the run waiting on it. The command
// needs a longer -timeout than the default: use 90m.
const liveRerecordDeadline = 75 * time.Minute

// The print shapes of the pass. The enumeration is what the task asks for
// before any money moves - every exchange name, its model, its request size
// in tokens - and the rows are the spend log the transcript keeps.
const (
	rerecordEnumHeader = "  %-40s %-10s %-11s %8s %9s %13s  %s"
	rerecordEnumRow    = "  %-40s %-10s %-11s %8d %9.2f %13s  %s"
	rerecordCallRow    = "rerecord paid %2d/%d %-38s %-11s tokens=%6d neurons=%8.2f booked=%s"
	rerecordResumeRow  = "rerecord resum %2d    %-38s %-11s tokens=%6d neurons=%8.2f booked=%s (on disk, no call)"
	rerecordCostHeader = "  %-12s %6s %9s %11s %14s"
	rerecordCostRow    = "  %-12s %6d %9d %11.2f %14s"
)

// rerecordFolderName renders a call's RelDir for the enumeration: the top
// level of the corpus has no name of its own, and "(top)" reads better in a
// log than an empty column.
func rerecordFolderName(dir string) string {
	if dir == rerecordTop {
		return "(top)"
	}
	return dir
}

// rerecordPhotoNote says, per call, where its bytes come from: a sample
// photo is sent by reference out of testdata/samples, a resized cost photo
// is sent as bytes read from the shipped tree and published by digest.
// That is the one image shape this run means, said here rather than
// implied by a path column.
func rerecordPhotoNote(w rerecordWorld, c RerecordCall) string {
	if len(c.Labels) == 0 {
		return "no image (the refusal's evidence)"
	}
	var parts []string
	for _, label := range c.Labels {
		if strings.HasPrefix(label, "testdata/clef/") {
			parts = append(parts, filepath.Base(label)+" (bytes, published by digest)")
			continue
		}
		rel, err := filepath.Rel(w.samplesDir, filepath.Join(w.root, label))
		if err != nil || strings.HasPrefix(rel, "..") {
			parts = append(parts, label)
			continue
		}
		parts = append(parts, rel+" (reference)")
	}
	return strings.Join(parts, ", ")
}

// rerecordOnDisk is the resume probe: a name the recorder would refuse to
// rewrite. It reads the same path the recorder's claim would.
func rerecordOnDisk(fresh string, c RerecordCall) bool {
	_, err := os.Stat(filepath.Join(fresh, c.RelDir, c.Exchange+".json"))
	return err == nil
}

// TestLiveRerecord re-records the corpus. It writes testdata/clef-fresh at
// the same depth as testdata/clef; it never writes into testdata/clef, and
// the two trees only meet in one later commit that moves one into the other.
func TestLiveRerecord(t *testing.T) {
	secrets := liveCredentials(t)

	// The paid gate, before anything else: no flag, no call.
	if err := rerecordPaidGate(os.LookupEnv); err != nil {
		liveFatalf(t, secrets, "%v", err)
	}

	// Zero spend, and a real precondition: the shipped lines must be the
	// evaluated ones, or this pass would record against a corpus whose
	// pins have already moved underneath it. Its recorded report is kept
	// only to print the two trees side by side at the end.
	shipped := livePrepare(t, secrets)

	w := loadRerecordWorld(t)
	plan, err := rerecordPlan(w)
	if err != nil {
		liveFatalf(t, secrets, "rerecord: plan: %v", err)
	}

	// The enumeration, in full, before the projection and far before the
	// first call: every exchange, its folder, its model, and its request
	// size in the input tokens the recording it replaces reported.
	liveLogf(t, secrets, "rerecord plan: %d calls, the whole corpus, into %s at the depth of testdata/clef",
		len(plan), filepath.Base(w.freshDir))
	liveLogf(t, secrets, rerecordEnumHeader, "exchange", "folder", "model", "tokens", "neurons", "implied", "image source")
	perModel := map[string]int{}
	for _, c := range plan {
		perModel[c.Model]++
		liveLogf(t, secrets, rerecordEnumRow, c.Exchange, rerecordFolderName(c.RelDir), c.Model,
			c.Tokens, liveNeurons(c.Tokens, liveNeuronsPerMillion[c.Model]),
			liveImpliedPrice(c.Tokens, liveNeuronsPerMillion[c.Model]), rerecordPhotoNote(w, c))
	}
	var modelNames []string
	for name := range perModel {
		modelNames = append(modelNames, name)
	}
	sort.Strings(modelNames)
	for _, name := range modelNames {
		liveLogf(t, secrets, "  %d calls on %s", perModel[name], name)
	}
	folders := map[string]int{}
	for _, c := range plan {
		folders[rerecordFolderName(c.RelDir)]++
	}
	for _, name := range []string{"(top)", rerecordStability, rerecordCost} {
		liveLogf(t, secrets, "  %d exchanges in %s; each folder is its own recorder, because Open does not descend and a name carries no slash",
			folders[name], name)
	}

	// Byte identity, re-proved at spend time rather than only at gate
	// time: every request the product's own code rebuilds keys exactly as
	// the recording it replaces. This reads the shipped corpus and asks
	// nothing of the network; a criterion reworded since the corpus was
	// written fails here, with the exchange's name, before a call.
	if _, keys, err := newRerecordWire(w, plan); err != nil {
		liveFatalf(t, secrets, "rerecord: the requests to be sent are not the requests recorded: %v", err)
	} else if len(keys) != len(plan) {
		liveFatalf(t, secrets, "rerecord: %d exchanges key apart, want one key per call", len(keys))
	} else {
		liveLogf(t, secrets, "byte identity: all %d rebuilt requests key exactly as the recordings they replace; %d distinct keys over the %d calls",
			len(plan), len(keys), len(plan))
	}

	// The projection and the ceiling. Everything still unrecorded is what
	// costs money; the pass size is what the call ceiling counts.
	var spendable []RerecordCall
	for _, c := range plan {
		if !rerecordOnDisk(w.freshDir, c) {
			spendable = append(spendable, c)
		}
	}
	liveLogf(t, secrets, "resume: %d of %d exchanges are already on disk and will not be re-sent; %d still cost money",
		len(plan)-len(spendable), len(plan), len(spendable))

	p, err := projectRerecord(spendable)
	if err != nil {
		liveFatalf(t, secrets, "rerecord: projection: %v", err)
	}
	liveLogf(t, secrets, "projection (internal/systemone's own rate card, not a re-typed number): "+
		"%d calls, %.0f neurons, %s; ceiling %d calls, %d neurons, %s; free plan day %d neurons",
		p.Calls, p.Neurons, p.Dollars, rerecordCeiling.Calls, rerecordCeiling.Neurons,
		rerecordCeiling.Budget, liveFreeDailyNeurons)
	for _, name := range modelNames {
		liveLogf(t, secrets, "  %s: %d input tokens projected", name, p.Tokens[name])
	}

	// The ceiling, fatally, and before the client exists. A t.Error here
	// would only warn and the run would go on spending, which is the whole
	// thing this line exists to prevent; TestRerecordRefusesAnOverBudgetRun
	// shows the same refusal biting at a lowered ceiling, offline.
	breach, err := rerecordBreach(spendable, len(plan), rerecordCeiling)
	if err != nil {
		liveFatalf(t, secrets, "rerecord: ceiling: %v", err)
	}
	if breach != "" {
		liveFatalf(t, secrets, "rerecord: over the ceiling, stopped before the first call: %s", breach)
	}
	if float64(p.Neurons) >= float64(liveFreeDailyNeurons) {
		liveFatalf(t, secrets, "rerecord: %.0f neurons projected reaches the free plan's %d neuron day, stopped before the first call",
			p.Neurons, liveFreeDailyNeurons)
	}
	liveLogf(t, secrets, "within the ceiling: %d of %d calls, %.0f of %d neurons, %s of %s - the pass runs",
		len(spendable), rerecordCeiling.Calls, p.Neurons, rerecordCeiling.Neurons, p.Dollars, rerecordCeiling.Budget)

	client, budget, ledger := liveNewClient(t, secrets, rerecordCeiling.Budget)
	spend := &liveSpend{ledger: ledger, budget: budget}
	recorders, err := newRerecordRecorders(w.freshDir, client, w.samplesDir)
	if err != nil {
		liveFatalf(t, secrets, "rerecord: recorders: %v", err)
	}
	liveLogf(t, secrets, "ordering: %s; %d calls in this run", liveOrdering, len(spendable))

	// The per-call log. A resumed exchange logs what it cost, which is
	// nothing, and the ledger's own length is what says so: the recorder
	// refused it before the inner Ask, so no charge could have been booked.
	hook := func(c RerecordCall, _ systemone.Request, resp systemone.Response, paid int, resumed bool) error {
		perCall, ok := liveNeuronsPerMillion[c.Model]
		if !ok {
			return fmt.Errorf("rerecord: %s: no documented neuron rate for model %q", c.Exchange, c.Model)
		}
		if resumed {
			if booked := len(ledger.Charges()); booked != paid {
				return fmt.Errorf("rerecord: %s: resumed on disk and %d charges booked, want the %d paid calls",
					c.Exchange, booked, paid)
			}
			liveLogf(t, secrets, rerecordResumeRow, paid, c.Exchange, c.Model, c.Tokens,
				liveNeurons(c.Tokens, perCall), liveImpliedPrice(c.Tokens, perCall))
			return nil
		}
		if err := spend.record(c.Exchange, c.Model, paid-1, resp.Usage); err != nil {
			return err
		}
		last := spend.calls[len(spend.calls)-1]
		liveLogf(t, secrets, rerecordCallRow, paid, len(plan), c.Exchange, c.Model,
			last.tokens, last.neurons, last.booked)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), liveRerecordDeadline)
	defer cancel()

	responses, resumed, err := rerecordRun(ctx, w, plan, recorders, hook)
	if err != nil {
		// Fail fast is the rule: a 401, a quota or a refusal is not
		// something to loop against. What is on disk stays on disk - the
		// tree is a resume point, and the line below says where it stopped.
		liveFatalf(t, secrets, "rerecord: the pass stopped at %d paid calls and %d resumed of %d; "+
			"rerun to resume from the tree, do not retry this call: %v",
			len(spend.calls), resumed, len(plan), err)
	}
	if len(responses)+resumed != len(plan) {
		liveFatalf(t, secrets, "rerecord: %d responses and %d resumed for %d calls",
			len(responses), resumed, len(plan))
	}
	if len(spend.calls) != len(spendable) {
		liveFatalf(t, secrets, "rerecord: %d paid calls, want the %d unrecorded ones",
			len(spend.calls), len(spendable))
	}

	// The measured cost, against the projection. Per model first, so a
	// surprise in one model's token count shows as its own row rather than
	// as a total nobody can attribute.
	measuredCalls := map[string]int{}
	measuredTokens := map[string]int64{}
	measuredNeurons := map[string]float64{}
	measuredMoney := map[string]cost.Price{}
	for _, call := range spend.calls {
		measuredCalls[call.model]++
		measuredTokens[call.model] += int64(call.tokens)
		measuredNeurons[call.model] += call.neurons
		measuredMoney[call.model] += call.booked
	}
	liveLogf(t, secrets, "rerecord cost by model (measured, from the responses' own usage)")
	liveLogf(t, secrets, rerecordCostHeader, "model", "calls", "tokens", "neurons", "booked")
	for _, name := range modelNames {
		liveLogf(t, secrets, rerecordCostRow, name,
			measuredCalls[name], measuredTokens[name], measuredNeurons[name], measuredMoney[name])
	}
	total, err := ledger.Total()
	if err != nil {
		liveFatalf(t, secrets, "rerecord: ledger total: %v", err)
	}
	spent := budget.Spent()
	remaining, err := budget.Remaining()
	if err != nil {
		liveFatalf(t, secrets, "rerecord: budget remaining: %v", err)
	}
	liveLogf(t, secrets, "measured: %d calls, %d input tokens, %d output tokens, %.0f neurons, %s booked by the ledger",
		len(spend.calls), spend.tokens(), spend.output(), spend.neurons(), total)
	liveLogf(t, secrets, "projected for the calls that ran: %d calls, %.0f neurons, %s",
		p.Calls, p.Neurons, p.Dollars)
	liveLogf(t, secrets, "budget: %s spent of the %s ceiling, %s left", spent, rerecordCeiling.Budget, remaining)
	if spend.dollars() != total {
		liveFatalf(t, secrets, "rerecord: the calls sum to %s and the ledger booked %s",
			spend.dollars(), total)
	}
	if spend.neurons() > float64(rerecordCeiling.Neurons) {
		liveFatalf(t, secrets, "rerecord: the pass measured %.0f neurons, over the %d neuron ceiling",
			spend.neurons(), rerecordCeiling.Neurons)
	}

	// Zero-spend read of what the pass wrote, so the spend log closes with
	// the proof that the tree is whole: every exchange hit, nothing missed.
	fresh, err := RunAt(w.root, w.freshDir)
	if err != nil {
		liveFatalf(t, secrets, "rerecord: the fresh tree does not evaluate: %v", err)
	}
	liveLogf(t, secrets, "fresh tree reads: %d samples, %d cells, %d agreeing at the chosen lines, %d off at 0.50, "+
			"%d check reads, %d stability answers, %d size variants, %s",
		fresh.Judge.Samples, fresh.Judge.Cells, fresh.Judge.ChosenTotal, len(fresh.Judge.OffAtHalf),
		len(fresh.Check.Reads), fresh.Stability.Answers, len(fresh.Sizes.Variants), fresh.Settings.CheckModel)
	if fresh.Judge.Samples != 37 || fresh.Judge.Cells != 273 {
		liveFatalf(t, secrets, "rerecord: the fresh tree read %d samples over %d cells, want 37 over 273: a recording was missed",
			fresh.Judge.Samples, fresh.Judge.Cells)
	}
	if len(fresh.Check.Reads) != 4 || fresh.Stability.Answers != 11 || len(fresh.Sizes.Variants) != 3 {
		liveFatalf(t, secrets, "rerecord: %d check reads, %d stability answers, %d size variants, want 4, 11 and 3",
			len(fresh.Check.Reads), fresh.Stability.Answers, len(fresh.Sizes.Variants))
	}
	if fresh.Check.Vocabulary.Of == 0 {
		liveFatalf(t, secrets, "rerecord: the fresh tree scored no vocabulary cell")
	}
	liveLogf(t, secrets, "the shipped tree reads %d of %d cells at its chosen lines, the fresh tree %d of %d; "+
		"TestRerecordReadsBothCorpora prints both curves apart, cell by cell",
		shipped.recorded.Judge.ChosenTotal, shipped.recorded.Judge.Cells,
		fresh.Judge.ChosenTotal, fresh.Judge.Cells)
}
