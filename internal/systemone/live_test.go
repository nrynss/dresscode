//go:build live

package systemone

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/keel/cost"
)

// TestLiveJudgeAndCheckOnASamplePhoto makes the project's first live Clef
// calls: one judge request and one check request against the real Workers
// AI endpoint, both on the committed AI sample people in testdata/samples.
// The gate does not build this file; it runs only with
//
//	go test -tags live ./internal/systemone/
//
// The token and the account id come from the environment. With either
// missing the test skips, so the committed file never fails for a key.
// The key is never printed: the client scrubs it out of its own errors,
// and this test scrubs again before anything reaches the transcript.
//
// The two requests are the recorded T0.6 exchanges read back through the
// replay, so the wire shape is the recorded shape by construction and the
// live replies can be compared against the recorded replies field by
// field. The comparison pins shape only — the same answer ids, the same
// kind per id, the fields each kind carries, the probabilities' option
// set — never the numbers, which a fresh call answers fresh. Exactly two
// calls go out, the judge first; each call's usage and its settled charge
// are logged, and both subtests run even if the first finds a divergence.
func TestLiveJudgeAndCheckOnASamplePhoto(t *testing.T) {
	token := os.Getenv("WORKERS_AI_API_TOKEN")
	account := os.Getenv("CLOUDFLARE_ACCOUNT_ID")
	if token == "" || account == "" {
		t.Skip("live workers ai needs WORKERS_AI_API_TOKEN and CLOUDFLARE_ACCOUNT_ID in the environment")
	}

	// The ceiling is one dollar, far above what two Clef calls cost on
	// the free plan; the settled prices are logged with the charges.
	budget, err := cost.NewBudget(cost.Dollar)
	if err != nil {
		t.Fatal(err)
	}
	ledger := cost.NewLedger()
	meter, err := cost.NewMeter(budget, ledger)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewWorkersAI(WorkersAIConfig{AccountID: account, Token: token, Meter: meter})
	if err != nil {
		// New's errors name the field and never the value.
		t.Fatal("the live client was not built")
	}

	rep := openDir(t, clefDir(t))

	t.Run("judge", func(t *testing.T) {
		liveAsk(t, token, client, ledger, "judge-V04-family-function", rep)
	})
	t.Run("check", func(t *testing.T) {
		liveAsk(t, token, client, ledger, "check-clef-V04-to-V05", rep)
	})

	// The books: each call verified its own charge above, so the tally
	// is only echoed here. A partial run (`-run …/judge`) books one.
	t.Logf("charges=%d budget spent %s of %s", len(ledger.Charges()), budget.Spent(), cost.Dollar)
}

// liveAsk sends one recorded exchange to the real endpoint and logs the
// transcript: the request's shape, the reply's answers, and the usage the
// reply reports — the only cost the Workers AI API exposes, token counts
// the client prices at the model's documented rate. The reply's shape is
// then compared against the recording's answer set, so a field the live
// API added or dropped fails here and lands in the report as the finding
// it is.
func liveAsk(t *testing.T, token string, client *WorkersAI, ledger *cost.Ledger, exchange string, rep *Replay) {
	t.Helper()
	rec := recorded(t, rep, exchange)
	req := recordedRequest(t, rec)
	want, _ := recordedAnswers(t, rec)

	t.Logf("%s: request POST /client/v4/accounts/<account>%s", exchange, runPath(req.Model))
	t.Logf("%s: state %s", exchange, req.State)
	for i, img := range req.Images {
		t.Logf("%s: image %d content_type=%s bytes=%d", exchange, i, img.ContentType, len(img.Bytes))
	}
	for _, id := range slices.Sorted(maps.Keys(req.Questions)) {
		q := req.Questions[id]
		line := fmt.Sprintf("%s: question %s type=%s instructions=%q", exchange, id, q.Type, q.Instructions)
		if len(q.Criteria) > 0 {
			line += " criteria=" + string(q.Criteria)
		}
		t.Log(line)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	before := len(ledger.Charges())
	start := time.Now()
	got, err := client.Ask(ctx, req)
	if err != nil {
		t.Fatalf("%s: %s", exchange, scrubLiveKey(token, err.Error()))
	}
	t.Logf("%s: took %s", exchange, time.Since(start).Round(time.Millisecond))
	t.Logf("%s: reply model=%s answers=%d usage.input_tokens=%d usage.output_tokens=%d",
		exchange, got.Model, len(got.Answers), got.Usage.InputTokens, got.Usage.OutputTokens)

	// The meter settled once, at the token count this live reply itself
	// reported, priced at the model's documented rate — the same figure
	// the free-plan dashboard bills neurons for.
	charges := ledger.Charges()
	if len(charges) != before+1 {
		t.Fatalf("%s: %d charges after the call, want %d", exchange, len(charges), before+1)
	}
	charge := charges[before]
	wantPrice := tokenPrice(int64(got.Usage.InputTokens), neuronsPerMillionInputTokens[req.Model])
	if charge.Kind != "workersai."+req.Model || charge.Units != 1 || charge.UnitPrice != wantPrice {
		t.Fatalf("%s: charge %+v, want kind workersai.%s, one unit at %d", exchange, charge, req.Model, wantPrice)
	}
	t.Logf("%s: charge kind=%s price=%s settled from usage.input_tokens=%d at %d neurons per million input tokens",
		exchange, charge.Kind, charge.UnitPrice, got.Usage.InputTokens, neuronsPerMillionInputTokens[req.Model])
	for _, id := range slices.Sorted(maps.Keys(got.Answers)) {
		a := got.Answers[id]
		switch a.Type {
		case TypeNoul:
			t.Logf("%s: answer %s type=noul noul=%v", exchange, id, *a.Noul)
		case TypeChoice:
			t.Logf("%s: answer %s type=choice choice=%q confidence=%v probabilities=%s",
				exchange, id, a.Choice, *a.Confidence, probabilityLine(a.Probabilities))
		case TypeScore:
			t.Logf("%s: answer %s type=score score=%v confidence=%v legend=%s probabilities=%s",
				exchange, id, *a.Score, *a.Confidence, legendLine(a.Legend), probabilityLine(a.Probabilities))
		default:
			t.Fatalf("%s: answer %s has unknown type %q", exchange, id, a.Type)
		}
	}

	compareLiveShape(t, exchange, req, got, want)
}

// compareLiveShape pins the live reply's shape to the recording's: the
// same answer ids, the same kind per id, and the fields each kind
// carries — a noul is {type, noul} and nothing else, a choice adds
// choice, probabilities over the recorded option set, and confidence, a
// score adds score, legend, probabilities and confidence. The numbers are
// the model's own and are never compared.
func compareLiveShape(t *testing.T, exchange string, req Request, got Response, want map[string]Answer) {
	t.Helper()
	if got.Model != req.Model {
		t.Fatalf("%s: live model %q, want the model the request named, %q", exchange, got.Model, req.Model)
	}
	if got.Usage.InputTokens <= 0 {
		t.Fatalf("%s: usage reports no input tokens: %+v", exchange, got.Usage)
	}
	if len(got.Answers) != len(want) {
		t.Fatalf("%s: the live reply has %d answers, the recording holds %d", exchange, len(got.Answers), len(want))
	}
	for id, wantAnswer := range want {
		gotAnswer, ok := got.Answers[id]
		if !ok {
			t.Fatalf("%s: the live reply has no answer %q", exchange, id)
		}
		if gotAnswer.Type != wantAnswer.Type {
			t.Fatalf("%s: answer %s is type %q, the recording holds %q", exchange, id, gotAnswer.Type, wantAnswer.Type)
		}
		if len(gotAnswer.Probabilities) != len(wantAnswer.Probabilities) {
			t.Fatalf("%s: answer %s has %d probabilities, the recording holds %d",
				exchange, id, len(gotAnswer.Probabilities), len(wantAnswer.Probabilities))
		}
		for option := range wantAnswer.Probabilities {
			if _, ok := gotAnswer.Probabilities[option]; !ok {
				t.Fatalf("%s: answer %s dropped the option %q the recording holds", exchange, id, option)
			}
		}
		switch wantAnswer.Type {
		case TypeNoul:
			if gotAnswer.Noul == nil {
				t.Fatalf("%s: answer %s carries no noul", exchange, id)
			}
			if gotAnswer.Choice != "" || gotAnswer.Score != nil || gotAnswer.Confidence != nil ||
				len(gotAnswer.Probabilities) != 0 || len(gotAnswer.Legend) != 0 {
				t.Fatalf("%s: answer %s carries fields the recorded noul shape does not: %+v", exchange, id, gotAnswer)
			}
		case TypeChoice:
			if gotAnswer.Choice == "" || gotAnswer.Confidence == nil || len(gotAnswer.Probabilities) == 0 {
				t.Fatalf("%s: answer %s is not a complete choice", exchange, id)
			}
			if gotAnswer.Noul != nil || gotAnswer.Score != nil || len(gotAnswer.Legend) != 0 {
				t.Fatalf("%s: answer %s carries fields the recorded choice shape does not: %+v", exchange, id, gotAnswer)
			}
		case TypeScore:
			if gotAnswer.Score == nil || gotAnswer.Confidence == nil || len(gotAnswer.Probabilities) == 0 || len(gotAnswer.Legend) == 0 {
				t.Fatalf("%s: answer %s is not a complete score", exchange, id)
			}
			if gotAnswer.Noul != nil || gotAnswer.Choice != "" {
				t.Fatalf("%s: answer %s carries fields the recorded score shape does not: %+v", exchange, id, gotAnswer)
			}
		}
	}
}

// probabilityLine renders a probabilities map with its options sorted, so
// the transcript is stable run to run.
func probabilityLine(probabilities map[string]float64) string {
	parts := make([]string, 0, len(probabilities))
	for _, option := range slices.Sorted(maps.Keys(probabilities)) {
		parts = append(parts, fmt.Sprintf("%s=%v", option, probabilities[option]))
	}
	return strings.Join(parts, " ")
}

// legendLine renders a score legend with its levels sorted.
func legendLine(legend map[string]json.RawMessage) string {
	parts := make([]string, 0, len(legend))
	for _, level := range slices.Sorted(maps.Keys(legend)) {
		parts = append(parts, fmt.Sprintf("%s=%s", level, legend[level]))
	}
	return strings.Join(parts, " ")
}

// scrubLiveKey keeps the token out of the transcript even if the client's
// own scrubbing missed a path.
func scrubLiveKey(token, text string) string {
	if token == "" {
		return text
	}
	return strings.ReplaceAll(text, token, "redacted")
}
