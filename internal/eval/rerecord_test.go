package eval

// T3.9e's re-record plan, projection and offline proof.
//
// The paid pass lives in live_rerecord_test.go, behind the `live` build
// tag. This file is the part the gate builds and runs on every commit:
// the exact call list, its projection against the rate card, the ceiling
// arithmetic that refuses an over-budget run before call one, and the
// whole plan driven offline into a scratch tree through the real
// recorder, the real meter and a real Workers AI client whose transport
// is a stub. Nothing here dials, spends, or needs a key.
//
// What the offline proof is for:
//
//   - every exchange name the pass would write matches the corpus it
//     replaces, in all three folders, which Open reads separately because
//     it does not descend into one;
//   - every request the pass would send is the request the corpus holds -
//     the same model, the same state, the same questions in the same
//     wording, the same image bytes - so the criteria wording travels
//     through the re-record untouched and the two corpora measure the same
//     thing;
//   - replay.Open serves each fresh request back key for key;
//   - the tree the pass writes is readable by the evaluation itself:
//     RunAt over it returns exactly the report the shipped corpus returns,
//     because the replies the proof hands back are the corpus's own;
//   - the ceiling stops a run that would overspend, before any call.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/nrynss/dresscode/internal/config"
	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/pipeline"
	"github.com/nrynss/dresscode/internal/systemone"
	"github.com/nrynss/keel/cost"
)

// The ceilings the paid pass runs inside, from the task's own budget: 78
// calls and 3,300 neurons, legitimate re-calls included. The plan is 55
// calls, so the headroom is the retries the client may make on a rate
// limit or a transient fault (internal/systemone/workersai.go retries
// those), not extra exchanges.
//
// rerecordBudgetCeiling is the same ceiling in money: 3,300 neurons at the
// client's own booking rate, which is liveNanodollarsPerNeuron and which
// TestRerecordRateCardIsTheClients proves against the client's bookings on
// every commit. It is what cost.NewBudget is built with, so the meter
// refuses a call whose reserve no longer fits and a runaway loop stops
// itself, and it is what the projection is compared against before the
// first Ask.
const (
	rerecordCallCeiling   = 78
	rerecordNeuronCeiling = 3300
	rerecordBudgetCeiling = cost.Price(rerecordNeuronCeiling * liveNanodollarsPerNeuron)
)

// The scratch tree the paid pass writes, the environment value that alone
// lets it spend, and the two stand-ins the offline proof hands the client
// where a key and an account id would otherwise be needed. The stand-ins
// never reach a wire: the proof's transport answers locally and refuses
// anything it did not enumerate.
const (
	// rerecordShippedDir is the corpus in the tree, the one docs/clef.md
	// transcribes; rerecordFreshDir is the scratch tree a paid pass records
	// into beside it, which becomes the shipped one when the swap lands.
	rerecordShippedDir = "clef"
	rerecordFreshDir   = "clef-fresh"
	rerecordPaidEnv    = "DRESSCODE_RERECORD"
	rerecordPaidValue  = "yes"

	// rerecordThinnestLine is the criterion docs/clef.md calls the thinnest
	// interval of the 36, the one place a rounded margin misrepresents how
	// little room the line has; its sentence is pinned to the corpus's curve.
	rerecordThinnestLine = "date-beard-even"

	rerecordProofPrefix  = "rerecord-proof-"
	rerecordStandToken   = "offline-proof-not-a-key"
	rerecordStandAccount = "offline-proof-not-an-account-id"

	// The one address the proof's client is built with, and the only one
	// its transport answers. Same host the tagged sweep guards to; the
	// proof never dials it.
	rerecordWorkersBase = "https://api.cloudflare.com"
	rerecordWorkersHost = "api.cloudflare.com"
)

// The three folders of the corpus and the three kinds of call it takes to
// write them. RelDir is "" for the top level, where the 37 judge and 4
// check exchanges sit.
const (
	rerecordTop       = ""
	rerecordStability = "stability"
	rerecordCost      = "cost"

	rerecordJudge   = "judge"
	rerecordCheck   = "check"
	rerecordNoImage = "judge-no-image"
)

// RerecordCall is one paid call of the pass: what it is named, where it
// lands, which model it goes to, how big its request is, and what it takes
// to rebuild it. Tokens is the input-token count the recording this call
// replaces reported - read out of the corpus, never retyped - so the
// projection prices the pass at the sizes the same requests measured at
// before.
type RerecordCall struct {
	Exchange string
	RelDir   string
	Model    string
	Tokens   int
	Kind     string
	Occasion string
	// Criterion and Requested carry a check's state fields: the fix the
	// check is asked about and the change the attempt requested.
	Criterion string
	Requested string
	// Photos are the image files the request sends, in the order it sends
	// them, as absolute paths. For rerecordNoImage it is the photo the
	// request would have carried, which the build then drops.
	Photos []string
	// Labels is the same list relative to the repo root, for the
	// enumeration the driver prints before it spends.
	Labels []string
}

// rerecordWorld is everything the plan reads before it spends: the shipped
// corpus, the sample photos a fresh recording points at instead of copying
// their bytes, the occasions, the vocabulary and the ground truth.
type rerecordWorld struct {
	root       string
	oldDir     string
	freshDir   string
	samplesDir string
	gt         GroundTruth
	occasions  map[string]*domain.Occasion
	vocab      *domain.Vocabulary
	byID       map[string]Sample
}

func loadRerecordWorld(t *testing.T) rerecordWorld {
	t.Helper()
	root := repoRoot(t)
	gt, err := LoadGroundTruth(filepath.Join(root, "testdata", "eval", "ground-truth.json"))
	if err != nil {
		t.Fatalf("rerecord: ground truth: %v", err)
	}
	loaded, err := domain.Load(filepath.Join(root, "testdata", "occasions"))
	if err != nil {
		t.Fatalf("rerecord: occasions: %v", err)
	}
	occasions := make(map[string]*domain.Occasion, len(loaded.Occasions))
	for i := range loaded.Occasions {
		occ := loaded.Occasions[i]
		occasions[occ.ID] = &occ
	}
	byID := make(map[string]Sample, len(gt.Samples))
	for _, s := range gt.Samples {
		byID[s.ID] = s
	}
	return rerecordWorld{
		root:       root,
		oldDir:     filepath.Join(root, "testdata", rerecordShippedDir),
		freshDir:   filepath.Join(root, "testdata", rerecordFreshDir),
		samplesDir: filepath.Join(root, "testdata", "samples"),
		gt:         gt,
		occasions:  occasions,
		vocab:      &loaded.Vocabulary,
		byID:       byID,
	}
}

// rerecordRecording is one committed exchange read for what the plan needs
// from it: its name, the HTTP call it stores, its image references, the
// tokens its reply reported, and the reply envelope itself, which is the
// answer the offline proof hands back.
type rerecordRecording struct {
	Exchange     string
	Method       string
	Path         string
	Body         json.RawMessage
	Images       []string
	State        map[string]string
	Model        string
	Tokens       int
	OutputTokens int
	Reply        json.RawMessage
}

// rerecordRead reads one exchange file whole. readRecording will not do
// here: it requires at least one image, and cost-V04-family-function-noimage
// deliberately carries none.
func rerecordRead(path string) (rerecordRecording, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return rerecordRecording{}, err
	}
	var env struct {
		Exchange string `json:"exchange"`
		Request  struct {
			Method string          `json:"method"`
			Path   string          `json:"path"`
			Body   json.RawMessage `json:"body"`
		} `json:"request"`
		Response struct {
			Body json.RawMessage `json:"body"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return rerecordRecording{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	rec := rerecordRecording{
		Exchange: env.Exchange,
		Method:   env.Request.Method,
		Path:     env.Request.Path,
		Body:     env.Request.Body,
		Reply:    env.Response.Body,
	}
	if rec.Exchange == "" || rec.Method == "" || rec.Path == "" || len(rec.Body) == 0 || len(rec.Reply) == 0 {
		return rerecordRecording{}, fmt.Errorf("%s: exchange, request and response are required", filepath.Base(path))
	}
	var body struct {
		Model  string            `json:"model"`
		Images []imageRef        `json:"images"`
		State  map[string]string `json:"state"`
	}
	if err := json.Unmarshal(rec.Body, &body); err != nil {
		return rerecordRecording{}, fmt.Errorf("%s: request body: %w", filepath.Base(path), err)
	}
	rec.Model = body.Model
	rec.State = body.State
	for _, image := range body.Images {
		rec.Images = append(rec.Images, image.File)
	}
	var reply struct {
		Result struct {
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Reply, &reply); err != nil {
		return rerecordRecording{}, fmt.Errorf("%s: reply envelope: %w", filepath.Base(path), err)
	}
	rec.Tokens = reply.Result.Usage.InputTokens
	rec.OutputTokens = reply.Result.Usage.OutputTokens
	if rec.Tokens <= 0 {
		return rerecordRecording{}, fmt.Errorf("%s: the recorded reply reports no input tokens", filepath.Base(path))
	}
	return rec, nil
}

// rerecordPlan builds the exact call list out of the shipped corpus: every
// exchange it holds, in the folder it holds it in, once each. It reads
// files and nothing else, so the list is the corpus's own and not a number
// the driver retyped.
func rerecordPlan(w rerecordWorld) ([]RerecordCall, error) {
	var plan []RerecordCall
	// add turns one reading of a recording into a call. refBase is the
	// folder its image references are relative to, which is the folder the
	// exchange sits in - except for the no-image control, whose reference
	// comes from the judge recording it repeats.
	add := func(relDir, refBase string, rec rerecordRecording, kind, occasion, criterion, requested string, images []string) error {
		var photos, labels []string
		for _, ref := range images {
			abs := filepath.Join(w.oldDir, refBase, filepath.FromSlash(ref))
			if _, err := os.Stat(abs); err != nil {
				return fmt.Errorf("rerecord: %s: image %s: %w", rec.Exchange, ref, err)
			}
			rel, err := filepath.Rel(w.root, abs)
			if err != nil {
				return fmt.Errorf("rerecord: %s: image %s: %w", rec.Exchange, ref, err)
			}
			photos = append(photos, abs)
			labels = append(labels, filepath.ToSlash(rel))
		}
		plan = append(plan, RerecordCall{
			Exchange: rec.Exchange, RelDir: relDir, Model: rec.Model, Tokens: rec.Tokens,
			Kind: kind, Occasion: occasion, Criterion: criterion, Requested: requested,
			Photos: photos, Labels: labels,
		})
		return nil
	}

	// The top level: one judge call per sample recording, one per check.
	judges, err := filepath.Glob(filepath.Join(w.oldDir, "judge-*.json"))
	if err != nil {
		return nil, err
	}
	if len(judges) != len(w.gt.Samples) {
		return nil, fmt.Errorf("rerecord: %d judge recordings, want %d samples", len(judges), len(w.gt.Samples))
	}
	sort.Strings(judges)
	for _, file := range judges {
		rec, err := rerecordRead(file)
		if err != nil {
			return nil, err
		}
		parts := strings.SplitN(rec.Exchange, "-", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("rerecord: %s: exchange does not name its variant", rec.Exchange)
		}
		sample, ok := w.byID[parts[1]]
		if !ok {
			return nil, fmt.Errorf("rerecord: %s: no ground truth for variant %q", rec.Exchange, parts[1])
		}
		if rec.State["occasion"] != sample.Occasion {
			return nil, fmt.Errorf("rerecord: %s: state names %q, ground truth says %q",
				rec.Exchange, rec.State["occasion"], sample.Occasion)
		}
		if len(rec.Images) != 1 {
			return nil, fmt.Errorf("rerecord: %s: %d images, want the one photo", rec.Exchange, len(rec.Images))
		}
		if err := add(rerecordTop, rerecordTop, rec, rerecordJudge, sample.Occasion, "", "", rec.Images); err != nil {
			return nil, err
		}
	}

	checks := append([]CheckTruth(nil), w.gt.Checks...)
	sort.Slice(checks, func(i, j int) bool { return checks[i].Exchange < checks[j].Exchange })
	for _, truth := range checks {
		rec, err := rerecordRead(filepath.Join(w.oldDir, truth.Exchange+".json"))
		if err != nil {
			return nil, err
		}
		if rec.Exchange != truth.Exchange || rec.Model != truth.Model {
			return nil, fmt.Errorf("rerecord: %s: file names %q model %q, ground truth says %q model %q",
				truth.Exchange, rec.Exchange, rec.Model, truth.Exchange, truth.Model)
		}
		if len(rec.Images) != 2 {
			return nil, fmt.Errorf("rerecord: %s: %d images, want the original and the render",
				truth.Exchange, len(rec.Images))
		}
		if _, ok := w.occasions[rec.State["occasion"]]; !ok {
			return nil, fmt.Errorf("rerecord: %s: no occasion %q", truth.Exchange, rec.State["occasion"])
		}
		if err := add(rerecordTop, rerecordTop, rec, rerecordCheck, rec.State["occasion"],
			rec.State["criterion"], rec.State["requested_change"], rec.Images); err != nil {
			return nil, err
		}
	}

	// stability/: ten repeats of one request, each its own name, because
	// the replay refuses to choose between recordings of one key.
	repeats, err := listExchanges(filepath.Join(w.oldDir, rerecordStability))
	if err != nil {
		return nil, err
	}
	if len(repeats) != 10 {
		return nil, fmt.Errorf("rerecord: %d stability recordings, want the ten repeats", len(repeats))
	}
	source, err := rerecordRead(filepath.Join(w.oldDir, "judge-V04-family-function.json"))
	if err != nil {
		return nil, err
	}
	for _, name := range repeats {
		rec, err := rerecordRead(filepath.Join(w.oldDir, rerecordStability, name+".json"))
		if err != nil {
			return nil, err
		}
		if rec.State["occasion"] != source.State["occasion"] || len(rec.Images) != len(source.Images) {
			return nil, fmt.Errorf("rerecord: %s: not a repeat of the judge-V04-family-function request", name)
		}
		if err := add(rerecordStability, rerecordStability, rec, rerecordJudge, source.State["occasion"], "", "", rec.Images); err != nil {
			return nil, err
		}
	}

	// cost/: the same judge request at three resized photos plus the
	// no-image control. The resized bytes are read from the shipped tree -
	// they exist nowhere else - and the fresh recording publishes them by
	// digest, which is the one image shape this run means. Said once,
	// before call one: a sample photo is referenced by its own name out of
	// testdata/samples and never copied, a resized cost photo is written
	// under the fresh cost folder as images/<sha256>.jpg, and no exchange
	// mixes the two shapes for one picture.
	sizes, err := listExchanges(filepath.Join(w.oldDir, rerecordCost))
	if err != nil {
		return nil, err
	}
	if len(sizes) != 4 {
		return nil, fmt.Errorf("rerecord: %d cost recordings, want the three sizes and the no-image control", len(sizes))
	}
	for _, name := range sizes {
		rec, err := rerecordRead(filepath.Join(w.oldDir, rerecordCost, name+".json"))
		if err != nil {
			return nil, err
		}
		switch len(rec.Images) {
		case 0:
			if !strings.HasSuffix(name, "-noimage") {
				return nil, fmt.Errorf("rerecord: %s: no image and no -noimage name", name)
			}
			// The photo this request would have carried, so the build comes
			// out of the same product call as every other judge request and
			// only then loses its image.
			if err := add(rerecordCost, rerecordTop, rec, rerecordNoImage, source.State["occasion"], "", "", source.Images); err != nil {
				return nil, err
			}
		case 1:
			if err := add(rerecordCost, rerecordCost, rec, rerecordJudge, source.State["occasion"], "", "", rec.Images); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("rerecord: %s: %d images, want one resized photo", name, len(rec.Images))
		}
	}

	if len(plan) != 55 {
		return nil, fmt.Errorf("rerecord: %d calls planned, want the 55 recordings the corpus holds", len(plan))
	}
	return plan, nil
}

// build rebuilds one call's request through the product's own code:
// pipeline.Judge for every judge exchange, the resized ones and the ten
// repeats included, and pipeline.Check for the four checks. The driver
// never assembles a request body itself, which is how the criteria wording
// of the shipped corpus survives the re-record untouched.
//
// The one field the driver does set is the image list, and on one
// exchange: cost-…-noimage is the request pipeline.Judge builds with its
// photo dropped, because pipeline.Judge refuses an empty photo before it
// can ask for anything, and that recording is the evidence of the refusal
// it could not send.
func (w rerecordWorld) build(c RerecordCall) (systemone.Request, error) {
	builder := &rerecordBuilder{}
	// The builder always refuses, so the pipeline returns an error on every
	// build: that error is the capture working, not a failure. What a
	// failure looks like here is the pipeline refusing before it ever asked
	// for a call, and that is the reason the message names.
	var asked error
	switch c.Kind {
	case rerecordJudge:
		occ, ok := w.occasions[c.Occasion]
		if !ok {
			return systemone.Request{}, fmt.Errorf("rerecord: %s: no occasion %q", c.Exchange, c.Occasion)
		}
		photo, err := rerecordImage(c.Photos[0])
		if err != nil {
			return systemone.Request{}, fmt.Errorf("rerecord: %s: %w", c.Exchange, err)
		}
		_, asked = pipeline.Judge(context.Background(), builder, photo, occ, pipeline.Thresholds{Met: readingAt})
	case rerecordNoImage:
		occ, ok := w.occasions[c.Occasion]
		if !ok {
			return systemone.Request{}, fmt.Errorf("rerecord: %s: no occasion %q", c.Exchange, c.Occasion)
		}
		photo, err := rerecordImage(c.Photos[0])
		if err != nil {
			return systemone.Request{}, fmt.Errorf("rerecord: %s: %w", c.Exchange, err)
		}
		_, asked = pipeline.Judge(context.Background(), builder, photo, occ, pipeline.Thresholds{Met: readingAt})
	case rerecordCheck:
		occ, ok := w.occasions[c.Occasion]
		if !ok {
			return systemone.Request{}, fmt.Errorf("rerecord: %s: no occasion %q", c.Exchange, c.Occasion)
		}
		fix := fixFor(occ, c.Criterion)
		if fix == nil {
			return systemone.Request{}, fmt.Errorf("rerecord: %s: no fix on %q addresses %q",
				c.Exchange, occ.ID, c.Criterion)
		}
		original, err := rerecordImage(c.Photos[0])
		if err != nil {
			return systemone.Request{}, fmt.Errorf("rerecord: %s: %w", c.Exchange, err)
		}
		render, err := rerecordImage(c.Photos[1])
		if err != nil {
			return systemone.Request{}, fmt.Errorf("rerecord: %s: %w", c.Exchange, err)
		}
		lines := pipeline.Lines{ChangeMade: readingAt, SamePerson: readingAt, CriterionMet: readingAt}
		_, asked = pipeline.Check(context.Background(), builder, c.Model, original, render,
			occ, fix, c.Criterion, c.Requested, w.vocab, lines)
	default:
		return systemone.Request{}, fmt.Errorf("rerecord: %s: unknown call kind %q", c.Exchange, c.Kind)
	}
	if !builder.built {
		return systemone.Request{}, fmt.Errorf("rerecord: %s: the pipeline asked for no call: %v", c.Exchange, asked)
	}
	req := builder.req
	if c.Kind == rerecordNoImage {
		// The one field the driver sets: this exchange is the evidence of
		// the refusal pipeline.Judge makes before it can send anything, so
		// its request is the built one with the photo taken back out.
		req.Images = nil
		if err := req.Validate(); err != nil {
			return systemone.Request{}, fmt.Errorf("rerecord: %s: %w", c.Exchange, err)
		}
	}
	return req, nil
}

// rerecordImage reads one photo's bytes for a request. The content type
// comes from the file's own suffix, which is the one the shipped corpus
// recorded for it; the byte-identity assertions in TestRerecordPlanIsTheCorpus
// are what prove a mismatch would have been caught.
func rerecordImage(path string) (systemone.Image, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return systemone.Image{}, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return systemone.Image{ContentType: "image/png", Bytes: raw}, nil
	case ".webp":
		return systemone.Image{ContentType: "image/webp", Bytes: raw}, nil
	default:
		return systemone.Image{ContentType: "image/jpeg", Bytes: raw}, nil
	}
}

// rerecordBuilder is the capture seam: it keeps the request the pipeline
// built and asks nothing of anyone, so the driver holds the exact bytes the
// product would send and hands them to the recorder.
type rerecordBuilder struct {
	req   systemone.Request
	built bool
}

func (b *rerecordBuilder) Ask(_ context.Context, req systemone.Request) (systemone.Response, error) {
	b.req = req
	b.built = true
	return systemone.Response{}, errors.New("rerecord: the plan asks for a request, not for an answer")
}

// rerecordProjection is the pass priced before it runs.
type rerecordProjection struct {
	Calls   int
	Tokens  map[string]int64
	Neurons float64
	Dollars cost.Price
}

// projectRerecord prices a call list at internal/systemone's own rate card.
// The rates are liveNeuronsPerMillion, and TestRerecordRateCardIsTheClients
// proves that table against the client's bookings on every commit, so the
// projection is the client's arithmetic rather than a re-typed figure. The
// money is the sum of the per-call bookings, which is how the ledger books:
// one rounding per call, not one over the total.
func projectRerecord(calls []RerecordCall) (rerecordProjection, error) {
	p := rerecordProjection{Calls: len(calls), Tokens: map[string]int64{}}
	var neurons float64
	var dollars cost.Price
	for _, c := range calls {
		perMillion, ok := liveNeuronsPerMillion[c.Model]
		if !ok {
			return rerecordProjection{}, fmt.Errorf("rerecord: no documented neuron rate for model %q", c.Model)
		}
		p.Tokens[c.Model] += int64(c.Tokens)
		neurons += liveNeurons(c.Tokens, perMillion)
		dollars += liveImpliedPrice(c.Tokens, perMillion)
	}
	p.Neurons = neurons
	p.Dollars = dollars
	return p, nil
}

// ---- The spend the corpus booked ----

// rerecordSpend is one corpus's own bill: the usage every recorded reply
// carries, read back out of the tree and priced at the client's rate card. It
// is the measured half of the pass; projectRerecord is the planned half, priced
// from the request the driver would send. The two are asserted against each
// other, and against the figures docs/clef.md states as fact, in
// TestRerecordBookedSpendIsTheCorpus.
type rerecordSpend struct {
	Calls    int
	ByFolder map[string]int
	// Tokens and OutTokens are the reported usage by model; MicroNeurons is
	// the same sum the float Neurons holds, kept exact: millionths of a neuron,
	// so a pin never compares a float's last bit.
	Tokens       map[string]int64
	OutTokens    map[string]int64
	MicroNeurons int64
	Neurons      float64
	Dollars      cost.Price
}

// rerecordCorpusSpend walks one corpus, folder by folder the way Open reads it,
// and sums what its replies report. It reads only the recordings, so it costs
// nothing and needs no image on disk: the paid pass's bill is in the tree
// already, and the ceiling was all that had been asserted about it.
func rerecordCorpusSpend(dir string) (rerecordSpend, error) {
	spend := rerecordSpend{
		ByFolder:  map[string]int{},
		Tokens:    map[string]int64{},
		OutTokens: map[string]int64{},
	}
	for _, sub := range []string{rerecordTop, rerecordStability, rerecordCost} {
		names, err := listExchanges(filepath.Join(dir, sub))
		if err != nil {
			return rerecordSpend{}, err
		}
		spend.ByFolder[sub] = len(names)
		for _, name := range names {
			rec, err := rerecordRead(filepath.Join(dir, sub, name+".json"))
			if err != nil {
				return rerecordSpend{}, err
			}
			perMillion, ok := liveNeuronsPerMillion[rec.Model]
			if !ok {
				return rerecordSpend{}, fmt.Errorf("rerecord: %s: no documented neuron rate for model %q",
					rec.Exchange, rec.Model)
			}
			spend.Calls++
			spend.Tokens[rec.Model] += int64(rec.Tokens)
			spend.OutTokens[rec.Model] += int64(rec.OutputTokens)
			spend.MicroNeurons += int64(rec.Tokens) * perMillion
			// One rounding per call, which is how the ledger books a settle.
			spend.Dollars += liveImpliedPrice(rec.Tokens, perMillion)
		}
	}
	spend.Neurons = float64(spend.MicroNeurons) / 1e6
	return spend, nil
}

// rerecordGrouped renders a count with the thousands separators docs/clef.md
// writes, so a claim about the doc is built from a computed number instead of a
// retyped one.
func rerecordGrouped(v int64) string {
	if v < 1000 {
		return strconv.FormatInt(v, 10)
	}
	return rerecordGrouped(v/1000) + "," + fmt.Sprintf("%03d", v%1000)
}

// rerecordGrouped1dp renders a neuron total the way the doc's summed figure is
// written: grouped, with the one decimal it carries.
func rerecordGrouped1dp(v float64) string {
	tenths := int64(math.Round(v * 10))
	return rerecordGrouped(tenths/10) + "." + strconv.FormatInt(tenths%10, 10)
}

// rerecordDocCarries fails when one of the claims a corpus produced is missing
// from the document that states it as fact. The claims are rendered from the
// measured numbers, so a doc that keeps an old figure, or a corpus whose
// readings moved under it, names the difference instead of quietly disagreeing.
// reason is the test's own statement of what the corpus gives, so the failure
// reads as a comparison and not as a missing string.
func rerecordDocCarries(t *testing.T, path string, reason string, claims []string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("rerecord: %s: %v", filepath.Base(path), err)
	}
	for _, claim := range claims {
		if !strings.Contains(string(data), claim) {
			t.Errorf("rerecord: %s does not state %q, which is what the corpus gives: %s - the doc and the recordings are made of the same pass and must read the same numbers",
				filepath.Base(path), claim, reason)
		}
	}
}

// rerecordSpendReason states the booked pass in the figures the spend test
// asserts, for the failure message of the doc pin.
func rerecordSpendReason(spend rerecordSpend) string {
	return fmt.Sprintf("%d calls, %s input tokens on %s and %s on %s, %s neurons (%s) with the ledger's one rounding per call",
		spend.Calls,
		rerecordGrouped(spend.Tokens[systemone.ModelClef]), systemone.ModelClef,
		rerecordGrouped(spend.Tokens[systemone.ModelClefFlash]), systemone.ModelClefFlash,
		rerecordGrouped1dp(spend.Neurons), spend.Dollars)
}

// rerecordLimits is the budget one pass is measured against: the call
// count, the neuron count and the money the budget holds. rerecordCeiling
// is the re-record's own, from the task; a test lowers it to show the
// refusal biting.
type rerecordLimits struct {
	Calls   int
	Neurons int
	Budget  cost.Price
}

var rerecordCeiling = rerecordLimits{Calls: rerecordCallCeiling, Neurons: rerecordNeuronCeiling, Budget: rerecordBudgetCeiling}

// rerecordBreach names the limit a pass reaches, or "" when it fits all
// three. planned is the whole call list - a resumed run re-sends nothing,
// but the pass is still that size - and spendable is what still costs
// money. The driver turns a non-empty answer into a fatal before it builds
// a client, so a pass that would overspend never makes a call. A t.Error
// here would only warn, and the run would go on spending.
func rerecordBreach(spendable []RerecordCall, planned int, limits rerecordLimits) (string, error) {
	if planned > limits.Calls {
		return fmt.Sprintf("%d calls planned, over the %d call ceiling", planned, limits.Calls), nil
	}
	p, err := projectRerecord(spendable)
	if err != nil {
		return "", err
	}
	if p.Neurons > float64(limits.Neurons) {
		return fmt.Sprintf("%.0f neurons projected, over the %d neuron ceiling", p.Neurons, limits.Neurons), nil
	}
	if p.Dollars >= limits.Budget {
		return fmt.Sprintf("%s projected, over the %s budget", p.Dollars, limits.Budget), nil
	}
	return "", nil
}

// rerecordPaidGate is the driver's second refusal: the paid pass runs only
// when the environment says so, and the check comes before any client is
// built, so an unguarded `go test -tags live` cannot spend by accident.
func rerecordPaidGate(lookup func(string) (string, bool)) error {
	value, ok := lookup(rerecordPaidEnv)
	if ok && value == rerecordPaidValue {
		return nil
	}
	return fmt.Errorf("rerecord: refusing to re-record: %s is %q, want %q; no call was made",
		rerecordPaidEnv, value, rerecordPaidValue)
}

// rerecordAnswersAll is the driver's own completeness check. The recorder
// refuses a reply that answers a question the request did not ask; it does
// not refuse one that answers fewer than the request asked, and a
// half-answered recording is corpus poison of the quiet kind - the
// evaluation only finds it when a cell will not read. So the paid pass
// checks it per call and stops.
func rerecordAnswersAll(exchange string, req systemone.Request, resp systemone.Response) error {
	var missing []string
	for id := range req.Questions {
		if _, ok := resp.Answers[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("rerecord: %s: the reply left %d of %d questions unanswered: %s",
			exchange, len(missing), len(req.Questions), strings.Join(missing, ", "))
	}
	if resp.Model != req.Model {
		return fmt.Errorf("rerecord: %s: the reply is from %q, the request asked %q",
			exchange, resp.Model, req.Model)
	}
	return nil
}

// ---- The offline wire ----

// rerecordWire is the transport the offline proof puts under a real
// Workers AI client. It answers from the shipped corpus: a request whose
// content key is one the plan enumerated gets that recording's own reply,
// and anything else is an error, so the proof cannot pass by asking for a
// call it had not planned. It counts every hit, which is how the proof
// shows the meter admitted all 55 calls, that none was made twice, and that
// a refusal left the wire untouched.
type rerecordWire struct {
	replies map[string]json.RawMessage
	hits    map[string]int
	calls   int
	fails   int
}

// rerecordOldCorpus opens the shipped corpus's three folders with the
// reader the evaluation uses. Open is what turns a recorded image
// reference back into the bytes that went out, so a request keyed against
// it is keyed against what the wire actually carried.
func rerecordOldCorpus(w rerecordWorld) (map[string]*systemone.Replay, error) {
	out := map[string]*systemone.Replay{}
	for _, dir := range []string{rerecordTop, rerecordStability, rerecordCost} {
		opened, err := systemone.Open(filepath.Join(w.oldDir, dir))
		if err != nil {
			return nil, fmt.Errorf("rerecord: open %s: %w", dir, err)
		}
		out[dir] = opened
	}
	return out, nil
}

// rerecordStoredCall is one stored request envelope, after Open has put
// its image bytes back inline.
type rerecordStoredCall struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body"`
}

// rerecordStoredReply is one stored response envelope: the reply the
// recorder wrote, which is Cloudflare's own wrapper around the answers.
type rerecordStoredReply struct {
	Status    int             `json:"status"`
	ElapsedMS int64           `json:"elapsed_ms"`
	Body      json.RawMessage `json:"body"`
}

// newRerecordWire builds the answer table and, with it, proves the first
// thing the pass owes: every request the product's own code rebuilds for an
// exchange keys exactly as the recording it replaces. A reworded criterion,
// a different state field, a different photo - any of them changes this
// hash, and the proof stops with the exchange's name in the message.
func newRerecordWire(w rerecordWorld, plan []RerecordCall) (*rerecordWire, map[string]string, error) {
	corpus, err := rerecordOldCorpus(w)
	if err != nil {
		return nil, nil, err
	}
	wire := &rerecordWire{replies: map[string]json.RawMessage{}, hits: map[string]int{}}
	keys := map[string]string{}
	for _, c := range plan {
		req, err := w.build(c)
		if err != nil {
			return nil, nil, err
		}
		stored, err := corpus[c.RelDir].Reply(c.Exchange)
		if err != nil {
			return nil, nil, fmt.Errorf("rerecord: %s: %w", c.Exchange, err)
		}
		var call rerecordStoredCall
		if err := json.Unmarshal(stored.Request, &call); err != nil {
			return nil, nil, fmt.Errorf("rerecord: %s: stored request: %w", c.Exchange, err)
		}
		var reply rerecordStoredReply
		if err := json.Unmarshal(stored.Response, &reply); err != nil {
			return nil, nil, fmt.Errorf("rerecord: %s: stored response: %w", c.Exchange, err)
		}
		if len(reply.Body) == 0 {
			return nil, nil, fmt.Errorf("rerecord: %s: the stored response carries no body", c.Exchange)
		}
		body, err := json.Marshal(req)
		if err != nil {
			return nil, nil, fmt.Errorf("rerecord: %s: %w", c.Exchange, err)
		}
		key, err := systemone.ContentKey(call.Method, call.Path, json.RawMessage(body))
		if err != nil {
			return nil, nil, fmt.Errorf("rerecord: %s: %w", c.Exchange, err)
		}
		oldKey, err := systemone.ContentKey(call.Method, call.Path, call.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("rerecord: %s: recorded request: %w", c.Exchange, err)
		}
		if key != oldKey {
			return nil, nil, fmt.Errorf("rerecord: %s: the rebuilt request is not the recorded one: built %s, recorded %s",
				c.Exchange, key, oldKey)
		}
		keys[c.Exchange] = key
		wire.replies[key] = reply.Body
	}
	return wire, keys, nil
}

func (w *rerecordWire) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" || req.URL.Host != rerecordWorkersHost {
		w.fails++
		return nil, fmt.Errorf("rerecord: the offline proof dials %s://%s", req.URL.Scheme, req.URL.Host)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		w.fails++
		return nil, err
	}
	path := strings.TrimPrefix(req.URL.Path, "/client/v4/accounts/"+rerecordStandAccount)
	key, err := systemone.ContentKey(req.Method, path, json.RawMessage(body))
	if err != nil {
		w.fails++
		return nil, err
	}
	reply, ok := w.replies[key]
	if !ok {
		w.fails++
		return nil, fmt.Errorf("rerecord: the offline proof was asked for a request it did not enumerate (key %s)", key)
	}
	w.calls++
	w.hits[key]++
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(reply)),
	}, nil
}

// rerecordClient builds the real client over the proof transport and one
// budget, exactly as the paid driver does. The token and account are
// stand-ins the transport never lets reach a wire.
func rerecordClient(wire *rerecordWire, ceiling cost.Price) (*systemone.WorkersAI, *cost.Budget, *cost.Ledger, error) {
	budget, err := cost.NewBudget(ceiling)
	if err != nil {
		return nil, nil, nil, err
	}
	ledger := cost.NewLedger()
	meter, err := cost.NewMeter(budget, ledger)
	if err != nil {
		return nil, nil, nil, err
	}
	client, err := systemone.NewWorkersAI(systemone.WorkersAIConfig{
		BaseURL:   rerecordWorkersBase,
		AccountID: rerecordStandAccount,
		Token:     rerecordStandToken,
		Meter:     meter,
		HTTP:      &http.Client{Transport: wire},
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return client, budget, ledger, nil
}

// rerecordRecorders is the paid pass's own shape: one recorder per folder,
// because a recording name cannot carry a slash and Open does not descend
// into a folder, so stability/ and cost/ are written as separate
// directories at the same depth as the top level.
type rerecordRecorders struct {
	fresh string
	byDir map[string]*systemone.Recorder
}

func newRerecordRecorders(fresh string, inner systemone.SystemOne, samplesDir string) (*rerecordRecorders, error) {
	resolver, err := systemone.MatchImages(samplesDir)
	if err != nil {
		return nil, err
	}
	out := &rerecordRecorders{fresh: fresh, byDir: map[string]*systemone.Recorder{}}
	for _, dir := range []string{rerecordTop, rerecordStability, rerecordCost} {
		abs := fresh
		if dir != rerecordTop {
			abs = filepath.Join(fresh, dir)
			if err := os.MkdirAll(abs, 0o755); err != nil {
				return nil, err
			}
		}
		rec, err := systemone.NewRecorder(systemone.RecorderConfig{
			Inner:  inner,
			Dir:    abs,
			Images: resolver,
		})
		if err != nil {
			return nil, err
		}
		out.byDir[dir] = rec
	}
	return out, nil
}

func (r *rerecordRecorders) record(ctx context.Context, c RerecordCall, req systemone.Request) (systemone.Response, error) {
	return r.byDir[c.RelDir].Record(ctx, c.Exchange, req)
}

// rerecordHook sees every call the run makes, paid or resumed, so the paid
// driver can log and account for each one without a second copy of the
// loop. paid counts the calls that actually went out, 1-based; resumed is
// the exchange the recorder refused to rewrite, whose resp is empty.
type rerecordHook func(c RerecordCall, req systemone.Request, resp systemone.Response, paid int, resumed bool) error

// rerecordRun drives a call list through the recorders: build, record, and
// check the reply answered every question the request asked. A name already
// on disk is the resume signal, so it is counted and skipped, never re-sent.
// The loop is sequential and fails fast: the first call error stops the run,
// because a refusal, a 401 or a quota is not something to loop against.
func rerecordRun(ctx context.Context, w rerecordWorld, plan []RerecordCall, recorders *rerecordRecorders, hook rerecordHook) ([]systemone.Response, int, error) {
	responses := make([]systemone.Response, 0, len(plan))
	var resumed int
	var paid int
	for _, c := range plan {
		req, err := w.build(c)
		if err != nil {
			return nil, resumed, err
		}
		resp, err := recorders.record(ctx, c, req)
		var already *systemone.AlreadyRecorded
		if errors.As(err, &already) {
			if already.Exchange != c.Exchange {
				return nil, resumed, fmt.Errorf("rerecord: %s: the resume names %q", c.Exchange, already.Exchange)
			}
			resumed++
			if hook != nil {
				if err := hook(c, req, systemone.Response{}, paid, true); err != nil {
					return nil, resumed, err
				}
			}
			continue
		}
		if err != nil {
			return nil, resumed, fmt.Errorf("rerecord: %s: %w", c.Exchange, err)
		}
		if err := rerecordAnswersAll(c.Exchange, req, resp); err != nil {
			return nil, resumed, err
		}
		paid++
		responses = append(responses, resp)
		if hook != nil {
			if err := hook(c, req, resp, paid, false); err != nil {
				return nil, resumed, err
			}
		}
	}
	return responses, resumed, nil
}

// rerecordScratchTree makes the proof's tree inside testdata, at the same
// depth as the shipped corpus: a recorded reference is relative to its own
// folder, so a tree one level deeper could not name testdata/samples the
// way the corpus does. It is removed on cleanup and written by no other
// test.
func rerecordScratchTree(t *testing.T, root string) string {
	t.Helper()
	fresh, err := os.MkdirTemp(filepath.Join(root, "testdata"), rerecordProofPrefix+"*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(fresh); err != nil {
			t.Errorf("rerecord: cleanup %s: %v", fresh, err)
		}
	})
	return fresh
}

// ---- The offline proofs ----

// TestRerecordPlanIsTheCorpus pins the call list against the shipped
// corpus: 55 exchanges, the names the corpus holds, the folders they sit
// in, the model and request size of each, and every request rebuilt by the
// product's own pipeline keying exactly as the recording it replaces.
// That key equality is the wording proof: a reworded criterion changes the
// question bytes and so changes the key.
func TestRerecordPlanIsTheCorpus(t *testing.T) {
	w := loadRerecordWorld(t)
	plan, err := rerecordPlan(w)
	if err != nil {
		t.Fatalf("rerecord: plan: %v", err)
	}
	if len(plan) != 55 {
		t.Errorf("%d calls planned, want 55", len(plan))
	}
	counts := map[string]int{}
	for _, c := range plan {
		counts[c.RelDir]++
	}
	if counts[rerecordTop] != 41 || counts[rerecordStability] != 10 || counts[rerecordCost] != 4 {
		t.Errorf("plan by folder: top %d, stability %d, cost %d; want 41, 10 and 4",
			counts[rerecordTop], counts[rerecordStability], counts[rerecordCost])
	}
	byName := map[string]RerecordCall{}
	for _, c := range plan {
		if _, dup := byName[c.Exchange]; dup {
			t.Errorf("%s is planned twice", c.Exchange)
		}
		byName[c.Exchange] = c
	}
	// The names are the corpus's, folder for folder - nothing added,
	// nothing dropped.
	for _, dir := range []string{rerecordTop, rerecordStability, rerecordCost} {
		names, err := listExchanges(filepath.Join(w.oldDir, dir))
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			c, ok := byName[name]
			if !ok {
				t.Errorf("%s: the corpus holds it in %q and the plan does not", name, dir)
				continue
			}
			if c.RelDir != dir {
				t.Errorf("%s: planned in %q, the corpus holds it in %q", name, c.RelDir, dir)
			}
		}
		if len(names) != counts[dir] {
			t.Errorf("folder %q: %d recordings, %d planned", dir, len(names), counts[dir])
		}
	}
	for _, c := range plan {
		rec, err := rerecordRead(filepath.Join(w.oldDir, c.RelDir, c.Exchange+".json"))
		if err != nil {
			t.Fatalf("rerecord: %s: %v", c.Exchange, err)
		}
		if rec.Model != c.Model || rec.Tokens != c.Tokens {
			t.Errorf("%s: the plan says %s/%d tokens, the recording says %s/%d",
				c.Exchange, c.Model, c.Tokens, rec.Model, rec.Tokens)
		}
	}
	wire, _, err := newRerecordWire(w, plan)
	if err != nil {
		t.Fatal(err)
	}
	// 55 calls over 45 distinct requests: the judge recording of
	// V04-family-function and its ten stability repeats are one request,
	// which is why the repeats are a folder of their own.
	if len(wire.replies) != 45 {
		t.Errorf("%d distinct request keys, want 45", len(wire.replies))
	}
	t.Logf("plan: %d calls, %d distinct requests", len(plan), len(wire.replies))
}

// TestRerecordProjectionFitsTheCeiling is the enumeration the paid pass
// owes before it spends: every call, its model and its request size,
// priced at the client's own rate card, against the ceiling it has to fit.
func TestRerecordProjectionFitsTheCeiling(t *testing.T) {
	w := loadRerecordWorld(t)
	plan, err := rerecordPlan(w)
	if err != nil {
		t.Fatalf("rerecord: plan: %v", err)
	}
	p, err := projectRerecord(plan)
	if err != nil {
		t.Fatalf("rerecord: project: %v", err)
	}
	t.Logf("re-record pass: %d calls, %.0f neurons projected (%s), ceiling %d calls / %d neurons / %s",
		p.Calls, p.Neurons, p.Dollars, rerecordCeiling.Calls, rerecordCeiling.Neurons, rerecordCeiling.Budget)
	for _, model := range []string{systemone.ModelClef, systemone.ModelClefFlash} {
		t.Logf("  %s: %d input tokens", model, p.Tokens[model])
	}
	if breach, err := rerecordBreach(plan, len(plan), rerecordCeiling); err != nil {
		t.Fatal(err)
	} else if breach != "" {
		t.Errorf("the planned pass does not fit its ceiling: %s", breach)
	}
	if p.Neurons >= float64(liveFreeDailyNeurons) {
		t.Errorf("%.0f neurons projected reaches the free plan's %d-neuron day", p.Neurons, liveFreeDailyNeurons)
	}
	// What this test logs is the pass's bill, so it is asserted rather than
	// stated: TestRerecordBookedSpendIsTheCorpus reads the same 55 recordings'
	// usage and ties this projection to the figures docs/clef.md books.
}

// TestRerecordBookedSpendIsTheCorpus asserts the paid pass's spend against the
// recordings it left behind, instead of logging it. docs/clef.md's Spend section
// states 2,365 neurons and $0.026018869 as fact; until now only the 3,300-neuron
// ceiling was asserted, so a doc figure and a corpus could part company with
// nothing in the repository noticing. Everything the assertion needs is committed:
// each of the 55 replies carries response.body.result.usage, and
// internal/systemone's rate card is read offline.
//
// The arithmetic, stated because the doc rounds and the reader has to be able to
// re-derive the rounding from the test:
//
//   - the exact sum is 107,121 x 21,818 + 8,157 x 3,455 = 2,365,348,413
//     millionths of a neuron, i.e. 2,365.348413 neurons, with output_tokens 0 on
//     every one of the 55 calls (Clef bills input only);
//   - the doc's whole-neuron figure, 2,365, is that sum rounded to the nearest
//     neuron, and its 2,365.3 is the same sum at one decimal. The doc's number is
//     a rounding of the true sum, not the true sum: the difference is 0.348413
//     neurons, about 0.0000038 dollars;
//   - the doc's $0.026018869 is the true ledger sum: the client rounds each call's
//     nanodollars up (workersai.go's tokenPrice) and the meter adds those, so it
//     is not 2,365.348413 x 11,000 = 26,018,832.543 nanodollars, which the
//     per-call roundings lift by 36.457 to 26,018,869.
func TestRerecordBookedSpendIsTheCorpus(t *testing.T) {
	const (
		// The usage the 55 shipped recordings report, summed by model.
		rerecordBookedClefTokens  = 107_121
		rerecordBookedFlashTokens = 8_157
		// The same pass in millionths of a neuron, exact: the two totals above
		// at 21,818 and 3,455 neurons per million tokens.
		rerecordBookedMicroNeurons = 2_365_348_413
		// And in nanodollars, one round up per call: $0.026018869.
		rerecordBookedDollars = cost.Price(26_018_869)
	)
	w := loadRerecordWorld(t)
	spend, err := rerecordCorpusSpend(w.oldDir)
	if err != nil {
		t.Fatalf("rerecord: spend: %v", err)
	}
	if spend.Calls != 55 || spend.ByFolder[rerecordTop] != 41 ||
		spend.ByFolder[rerecordStability] != 10 || spend.ByFolder[rerecordCost] != 4 {
		t.Errorf("the corpus books %d calls - %d at the top level, %d in %s, %d in %s - want 55: 41, 10 and 4",
			spend.Calls, spend.ByFolder[rerecordTop], spend.ByFolder[rerecordStability], rerecordStability,
			spend.ByFolder[rerecordCost], rerecordCost)
	}
	for _, model := range []string{systemone.ModelClef, systemone.ModelClefFlash} {
		if spend.OutTokens[model] != 0 {
			t.Errorf("%s: the corpus reports %d output tokens across its calls, and docs/clef.md states 0 on every one of the 55",
				model, spend.OutTokens[model])
		}
	}
	clef, flash := spend.Tokens[systemone.ModelClef], spend.Tokens[systemone.ModelClefFlash]
	if clef != rerecordBookedClefTokens {
		t.Errorf("%s: the recordings sum to %s input tokens, the %s the pass was billed and %s states",
			systemone.ModelClef, rerecordGrouped(clef), rerecordGrouped(rerecordBookedClefTokens),
			filepath.Join("docs", "clef.md"))
	}
	if flash != rerecordBookedFlashTokens {
		t.Errorf("%s: the recordings sum to %s input tokens, the %s the pass was billed and %s states",
			systemone.ModelClefFlash, rerecordGrouped(flash), rerecordGrouped(rerecordBookedFlashTokens),
			filepath.Join("docs", "clef.md"))
	}
	if spend.MicroNeurons != rerecordBookedMicroNeurons {
		t.Errorf("the 55 recordings sum to %s neurons (%d millionths), not the %s booked: the difference is %.6f neurons",
			rerecordGrouped1dp(spend.Neurons), spend.MicroNeurons,
			rerecordGrouped1dp(float64(rerecordBookedMicroNeurons)/1e6),
			float64(spend.MicroNeurons-rerecordBookedMicroNeurons)/1e6)
	}
	if math.Abs(spend.Neurons-2365.348413) > 1e-9 {
		t.Errorf("the summed neurons read %.9f, want 2,365.348413 exactly: the doc's 2,365 is this value rounded to the nearest whole neuron and its 2,365.3 rounded to one decimal",
			spend.Neurons)
	}
	if spend.Dollars != rerecordBookedDollars {
		t.Errorf("the pass's calls, each rounded up to the nanodollar as the ledger books them, sum to %s, not the %s booked and stated in %s",
			spend.Dollars, rerecordBookedDollars, filepath.Join("docs", "clef.md"))
	}

	// The plan priced the same money from the requests it would send: projection
	// and bill are two reads of one pass, and a recording the plan does not list
	// or a call the plan enumerates that the tree never wrote shows up here.
	plan, err := rerecordPlan(w)
	if err != nil {
		t.Fatalf("rerecord: plan: %v", err)
	}
	p, err := projectRerecord(plan)
	if err != nil {
		t.Fatalf("rerecord: project: %v", err)
	}
	if p.Calls != spend.Calls || p.Dollars != spend.Dollars || math.Abs(p.Neurons-spend.Neurons) > 1e-9 {
		t.Errorf("the planned pass and the booked one differ: %d calls, %.9f neurons, %s planned against %d calls, %.9f neurons, %s booked",
			p.Calls, p.Neurons, p.Dollars, spend.Calls, spend.Neurons, spend.Dollars)
	}
	for model, tokens := range spend.Tokens {
		if p.Tokens[model] != tokens {
			t.Errorf("%s: the plan prices %d input tokens, the recordings report %d", model, p.Tokens[model], tokens)
		}
	}

	// The doc's claim and this assertion read the same numbers: every figure
	// below is rendered out of the sums above, none is retyped.
	rerecordDocCarries(t, filepath.Join(w.root, "docs", "clef.md"), rerecordSpendReason(spend), []string{
		// The booked pass, whole neurons and nanodollars.
		fmt.Sprintf("%s neurons, %s", rerecordGrouped(int64(math.Round(spend.Neurons))), spend.Dollars),
		// The per-model input tokens and the neuron sum beside each, in the order
		// the doc's sentence gives them.
		fmt.Sprintf("%s tokens on `%s`", rerecordGrouped(clef), systemone.ModelClef),
		fmt.Sprintf("(%s neurons)", rerecordGrouped1dp(liveNeurons(int(clef), liveNeuronsPerMillion[systemone.ModelClef]))),
		// The flash claim carries its parenthetical because the tokens alone do
		// not identify it: the T0.6 round states the same 8,157 on `clef-flash`,
		// so a bare token count matches either paragraph and pins neither.
		fmt.Sprintf("%s on `%s` (%s neurons)", rerecordGrouped(flash), systemone.ModelClefFlash,
			rerecordGrouped1dp(liveNeurons(int(flash), liveNeuronsPerMillion[systemone.ModelClefFlash]))),
		// The summed total at the one decimal the doc states it with.
		fmt.Sprintf("%s neurons summed", rerecordGrouped1dp(spend.Neurons)),
	})
	t.Logf("booked pass: %d calls, %s + %s input tokens, %d millionths of a neuron = %.6f, %s; the doc books %s whole neurons and %s",
		spend.Calls, rerecordGrouped(clef), rerecordGrouped(flash), spend.MicroNeurons, spend.Neurons,
		spend.Dollars, rerecordGrouped(int64(math.Round(spend.Neurons))), spend.Dollars)
}

// TestRerecordRefusesAnOverBudgetRun shows the ceiling biting twice: once
// as the pre-call arithmetic the paid driver turns into a fatal before it
// builds a client, and once as the meter refusing the run partway through a
// budget it cannot pay its way across. The second is the one that matters:
// a check that only warns lets the run go on spending, and this shows the
// money stopping at the ceiling with the tree left at a resume point.
func TestRerecordRefusesAnOverBudgetRun(t *testing.T) {
	w := loadRerecordWorld(t)
	plan, err := rerecordPlan(w)
	if err != nil {
		t.Fatalf("rerecord: plan: %v", err)
	}
	p, err := projectRerecord(plan)
	if err != nil {
		t.Fatal(err)
	}

	// The pre-call check with the ceiling set below the pass's own cost.
	tight := rerecordLimits{
		Calls:   rerecordCeiling.Calls,
		Neurons: int(p.Neurons / 2),
		Budget:  cost.Price(int64(p.Dollars) / 2),
	}
	breach, err := rerecordBreach(plan, len(plan), tight)
	if err != nil {
		t.Fatal(err)
	}
	if breach == "" {
		t.Fatalf("a ceiling of %d neurons / %s under a %s projection let the pass through",
			tight.Neurons, tight.Budget, p.Dollars)
	}
	t.Logf("refused before any call: %s", breach)
	if tooMany, err := rerecordBreach(plan, rerecordCeiling.Calls+1, rerecordCeiling); err != nil {
		t.Fatal(err)
	} else if tooMany == "" {
		t.Errorf("%d planned calls slipped past the %d call ceiling", rerecordCeiling.Calls+1, rerecordCeiling.Calls)
	}

	// The meter's own refusal, on a budget below the pass's cost. A budget
	// bounds the whole run, not one call, so the demonstration is the run
	// itself: the same plan, the same recorders, the same client, a budget
	// it cannot pay its way through - and the pass stops partway, having
	// booked less than its ceiling and written nothing for the call it
	// could not pay for.
	below := cost.Price(int64(p.Dollars) / 4)
	wire, _, err := newRerecordWire(w, plan)
	if err != nil {
		t.Fatal(err)
	}
	client, budget, ledger, err := rerecordClient(wire, below)
	if err != nil {
		t.Fatal(err)
	}
	fresh := rerecordScratchTree(t, w.root)
	recorders, err := newRerecordRecorders(fresh, client, w.samplesDir)
	if err != nil {
		t.Fatal(err)
	}
	_, resumed, err := rerecordRun(context.Background(), w, plan, recorders, nil)
	if err == nil {
		t.Fatalf("the pass completed under a %s budget it cannot pay, so the ceiling is not enforced", below)
	}
	t.Logf("the meter stopped the pass: %v", err)
	if resumed != 0 {
		t.Errorf("%d exchanges resumed on a tree that started empty", resumed)
	}
	booked := len(ledger.Charges())
	if booked >= len(plan) {
		t.Errorf("the run made %d calls under a budget below its cost, want it stopped short of %d", booked, len(plan))
	}
	if wire.calls != booked {
		t.Errorf("%d requests reached the transport and %d charges booked; a refused call must not be sent",
			wire.calls, booked)
	}
	if spent := budget.Spent(); spent > below {
		t.Errorf("the run spent %s over a %s budget", spent, below)
	}
	// Nothing was written for the call the budget refused: a failed call
	// stores nothing, so the tree is a resume point, not a broken corpus.
	var written int
	if err := filepath.WalkDir(fresh, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".json") {
			written++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if written != booked {
		t.Errorf("%d exchanges on disk after %d paid calls", written, booked)
	}
}

// TestRerecordRateCardIsTheClients ties liveNeuronsPerMillion, the rate
// card the projection is built on, to internal/systemone's own bookings:
// both documented models are priced by a real client over a stubbed
// transport, and a rate that drifted from the client fails here, offline,
// before a paid run projects anything with it.
func TestRerecordRateCardIsTheClients(t *testing.T) {
	w := loadRerecordWorld(t)
	plan, err := rerecordPlan(w)
	if err != nil {
		t.Fatalf("rerecord: plan: %v", err)
	}
	seen := map[string]bool{}
	for _, c := range plan {
		if seen[c.Model] {
			continue
		}
		seen[c.Model] = true
		wire, _, err := newRerecordWire(w, []RerecordCall{c})
		if err != nil {
			t.Fatal(err)
		}
		client, _, ledger, err := rerecordClient(wire, 10*cost.Cent)
		if err != nil {
			t.Fatal(err)
		}
		req, err := w.build(c)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Ask(context.Background(), req); err != nil {
			t.Fatalf("rerecord: %s: %v", c.Exchange, err)
		}
		charges := ledger.Charges()
		if len(charges) != 1 {
			t.Fatalf("%s: %d charges booked, want 1", c.Exchange, len(charges))
		}
		perMillion, ok := liveNeuronsPerMillion[c.Model]
		if !ok {
			t.Fatalf("rerecord: no documented rate for %q", c.Model)
		}
		if want := liveImpliedPrice(c.Tokens, perMillion); charges[0].UnitPrice != want {
			t.Errorf("%s: the client booked %s for %d tokens on %q, this file's rate card implies %s",
				c.Exchange, charges[0].UnitPrice, c.Tokens, c.Model, want)
		}
		t.Logf("rate card tied: %s books %s for %d tokens, as liveNeuronsPerMillion implies",
			c.Model, charges[0].UnitPrice, c.Tokens)
	}
	if len(seen) != 2 {
		t.Errorf("the pass runs on %d models, want clef and clef-flash", len(seen))
	}
}

// TestRerecordWritesTheCorpusOffline runs the whole paid pass offline: the
// real recorder, the real meter and budget at the run's own ceiling, a real
// Workers AI client on a stubbed transport, the corpus's own replies handed
// back for the corpus's own requests. Then it reads what landed.
func TestRerecordWritesTheCorpusOffline(t *testing.T) {
	w := loadRerecordWorld(t)
	plan, err := rerecordPlan(w)
	if err != nil {
		t.Fatalf("rerecord: plan: %v", err)
	}
	wire, keys, err := newRerecordWire(w, plan)
	if err != nil {
		t.Fatal(err)
	}
	client, budget, ledger, err := rerecordClient(wire, rerecordCeiling.Budget)
	if err != nil {
		t.Fatal(err)
	}
	fresh := rerecordScratchTree(t, w.root)
	recorders, err := newRerecordRecorders(fresh, client, w.samplesDir)
	if err != nil {
		t.Fatal(err)
	}
	responses, resumed, err := rerecordRun(context.Background(), w, plan, recorders, nil)
	if err != nil {
		t.Fatalf("rerecord: offline pass: %v", err)
	}
	if resumed != 0 || len(responses) != len(plan) {
		t.Fatalf("%d responses for %d calls, %d resumed", len(responses), len(plan), resumed)
	}
	if wire.calls != len(plan) || wire.fails != 0 {
		t.Errorf("the transport served %d calls and refused %d, want %d and 0", wire.calls, wire.fails, len(plan))
	}

	// Every name the corpus holds is on disk, in its own folder, with its
	// own exchange id inside the file.
	for _, c := range plan {
		rec, err := rerecordRead(filepath.Join(fresh, c.RelDir, c.Exchange+".json"))
		if err != nil {
			t.Fatalf("rerecord: %s: %v", c.Exchange, err)
		}
		if rec.Exchange != c.Exchange {
			t.Errorf("%s: the file at that path names %q", c.Exchange, rec.Exchange)
		}
	}

	// The image shape decided before call one, seen in what landed: the
	// sample photos are referenced by their own names out of
	// testdata/samples, exactly as the shipped corpus names them, and the
	// three resized cost photos are the recorder's digest files.
	for _, c := range plan {
		freshRec, err := rerecordRead(filepath.Join(fresh, c.RelDir, c.Exchange+".json"))
		if err != nil {
			t.Fatal(err)
		}
		oldRec, err := rerecordRead(filepath.Join(w.oldDir, c.RelDir, c.Exchange+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if c.Kind == rerecordNoImage {
			if len(freshRec.Images) != 0 || len(oldRec.Images) != 0 {
				t.Errorf("%s: the no-image control carries %d images", c.Exchange, len(freshRec.Images))
			}
			continue
		}
		if c.RelDir == rerecordCost {
			for _, ref := range freshRec.Images {
				if !strings.HasPrefix(ref, "images/") || !rerecordIsDigest(ref) {
					t.Errorf("%s: the resized photo is referenced %q, want a digest file under images/", c.Exchange, ref)
				}
			}
			continue
		}
		if strings.Join(freshRec.Images, ",") != strings.Join(oldRec.Images, ",") {
			t.Errorf("%s: references %v, the corpus holds %v", c.Exchange, freshRec.Images, oldRec.Images)
		}
	}
	var digests []string
	entries, err := os.ReadDir(filepath.Join(fresh, rerecordCost, "images"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		digests = append(digests, e.Name())
	}
	sort.Strings(digests)
	if len(digests) != 3 {
		t.Fatalf("%d files under cost/images, want the three resized photos: %v", len(digests), digests)
	}
	for _, name := range digests {
		if !rerecordIsDigest("images/" + name) {
			t.Errorf("cost/images/%s is not a digest file name", name)
		}
	}

	// No payload and no credential anywhere in the tree the pass writes.
	rerecordWalk(t, fresh, func(path string, data []byte) {
		for _, forbidden := range []string{
			"base64", rerecordStandToken, rerecordStandAccount,
			"authorization", "Authorization", "Bearer", "bearer",
			"http://", "https://", rerecordWorkersHost, "/client/v4/accounts/",
			"workers_ai_api_token", "cloudflare_account_id",
		} {
			if strings.Contains(string(data), forbidden) {
				t.Fatalf("%s holds %q", path, forbidden)
			}
		}
	})

	// The reader's own proof: Open on each of the three fresh folders names
	// every exchange the corpus names and serves every request the plan
	// built, key for key, out of the file that request wrote.
	for _, dir := range []string{rerecordTop, rerecordStability, rerecordCost} {
		opened, err := systemone.Open(filepath.Join(fresh, dir))
		if err != nil {
			t.Fatalf("rerecord: open %q: %v", dir, err)
		}
		names, err := listExchanges(filepath.Join(fresh, dir))
		if err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, c := range plan {
			if c.RelDir == dir {
				want = append(want, c.Exchange)
			}
		}
		sort.Strings(want)
		if strings.Join(names, ",") != strings.Join(want, ",") {
			t.Errorf("folder %q holds %v, want %v", dir, names, want)
		}
		for _, name := range names {
			stored, err := opened.Reply(name)
			if err != nil {
				t.Errorf("rerecord: %s: Reply: %v", name, err)
				continue
			}
			var call struct {
				Method string          `json:"method"`
				Path   string          `json:"path"`
				Body   json.RawMessage `json:"body"`
			}
			if err := json.Unmarshal(stored.Request, &call); err != nil {
				t.Errorf("rerecord: %s: stored request: %v", name, err)
				continue
			}
			key, err := systemone.ContentKey(call.Method, call.Path, call.Body)
			if err != nil {
				t.Errorf("rerecord: %s: %v", name, err)
				continue
			}
			if key != keys[name] {
				t.Errorf("%s: the fresh recording keys its own request %s, the pass built %s", name, key, keys[name])
			}
		}
		if dir == rerecordStability {
			continue
		}
		for _, c := range plan {
			if c.RelDir != dir {
				continue
			}
			req := mustBuild(t, w, c)
			if _, err := opened.Ask(context.Background(), req); err != nil {
				t.Errorf("rerecord: %s: the fresh corpus does not serve the request it recorded: %v", c.Exchange, err)
			}
		}
	}

	// The ten repeats share one request, which is why they are a folder of
	// their own: the replay refuses to pick between them.
	repeats, err := systemone.Open(filepath.Join(fresh, rerecordStability))
	if err != nil {
		t.Fatal(err)
	}
	var ambiguous *systemone.Ambiguous
	if _, err := repeats.Ask(context.Background(), mustBuild(t, w, plan[41])); err == nil {
		t.Error("the stability folder served one request ten ways; want the ambiguity the folder exists to avoid")
	} else if !errors.As(err, &ambiguous) {
		t.Errorf("stability Ask: %v, want Ambiguous", err)
	} else if len(ambiguous.Recordings) != 10 {
		t.Errorf("stability Ask is ambiguous between %d recordings, want the ten", len(ambiguous.Recordings))
	}

	// The meter admitted the whole pass inside its own ceiling, and booked
	// exactly what the projection priced: one charge per call, the sum of
	// the per-call bookings.
	charges := ledger.Charges()
	if len(charges) != len(plan) {
		t.Fatalf("%d charges booked for %d calls", len(charges), len(plan))
	}
	total, err := ledger.Total()
	if err != nil {
		t.Fatal(err)
	}
	p, err := projectRerecord(plan)
	if err != nil {
		t.Fatal(err)
	}
	spent := budget.Spent()
	if spent > rerecordCeiling.Budget {
		t.Errorf("the pass spent %s, over the %s ceiling", spent, rerecordCeiling.Budget)
	}
	t.Logf("offline pass: %d calls, ledger total %s, budget spent %s of ceiling %s, projection %s",
		len(charges), total, spent, rerecordCeiling.Budget, p.Dollars)
	if total != p.Dollars {
		t.Errorf("the pass booked %s, the projection priced %s", total, p.Dollars)
	}
}

// TestRerecordProofTreeEvaluatesIdentical reads the tree the offline pass
// wrote through the evaluation itself. The replies the proof handed back
// are the corpus's own, so the report must equal the shipped one cell for
// cell: that is the proof the fresh layout - three folders, digest-named
// cost photos, referenced samples - is one RunAt can score, and that the
// pass misses no recording. A recording the evaluation could not replay
// would have failed the run instead of differing in a cell.
func TestRerecordProofTreeEvaluatesIdentical(t *testing.T) {
	w := loadRerecordWorld(t)
	plan, err := rerecordPlan(w)
	if err != nil {
		t.Fatalf("rerecord: plan: %v", err)
	}
	wire, _, err := newRerecordWire(w, plan)
	if err != nil {
		t.Fatal(err)
	}
	client, _, _, err := rerecordClient(wire, rerecordCeiling.Budget)
	if err != nil {
		t.Fatal(err)
	}
	fresh := rerecordScratchTree(t, w.root)
	recorders, err := newRerecordRecorders(fresh, client, w.samplesDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := rerecordRun(context.Background(), w, plan, recorders, nil); err != nil {
		t.Fatalf("rerecord: offline pass: %v", err)
	}
	shipped, err := Run(w.root)
	if err != nil {
		t.Fatalf("rerecord: shipped corpus: %v", err)
	}
	freshReport, err := RunAt(w.root, fresh)
	if err != nil {
		t.Fatalf("rerecord: evaluation over the fresh tree: %v", err)
	}
	if !equalReports(shipped, freshReport) {
		t.Errorf("the fresh tree evaluates differently from the shipped corpus")
	}
	if freshReport.Judge.Samples != 37 || freshReport.Judge.Cells != 273 {
		t.Errorf("fresh tree: %d samples, %d cells, want 37 and 273", freshReport.Judge.Samples, freshReport.Judge.Cells)
	}
	if freshReport.Check.Vocabulary.Of != shipped.Check.Vocabulary.Of {
		t.Errorf("fresh tree scored %d vocabulary cells, the corpus %d", freshReport.Check.Vocabulary.Of, shipped.Check.Vocabulary.Of)
	}
	if len(freshReport.Stability.Flips) != len(shipped.Stability.Flips) || len(freshReport.Sizes.Variants) != len(shipped.Sizes.Variants) {
		t.Errorf("fresh tree: %d stability flips over %d size variants, the corpus %d over %d",
			len(freshReport.Stability.Flips), len(freshReport.Sizes.Variants),
			len(shipped.Stability.Flips), len(shipped.Sizes.Variants))
	}
	t.Logf("fresh tree: %d judge cells agreeing %d at the chosen lines, %d check reads, %d size variants",
		freshReport.Judge.Cells, freshReport.Judge.ChosenTotal, len(freshReport.Check.Reads), len(freshReport.Sizes.Variants))
}

// TestRerecordResumesFromHalfAPlan is the resume signal the paid run
// depends on: a pass that stopped halfway is finished by running it again,
// and the second run makes no call and books no charge for a recording
// already on disk.
func TestRerecordResumesFromHalfAPlan(t *testing.T) {
	w := loadRerecordWorld(t)
	plan, err := rerecordPlan(w)
	if err != nil {
		t.Fatalf("rerecord: plan: %v", err)
	}
	wire, _, err := newRerecordWire(w, plan)
	if err != nil {
		t.Fatal(err)
	}
	client, _, ledger, err := rerecordClient(wire, rerecordCeiling.Budget)
	if err != nil {
		t.Fatal(err)
	}
	fresh := rerecordScratchTree(t, w.root)
	recorders, err := newRerecordRecorders(fresh, client, w.samplesDir)
	if err != nil {
		t.Fatal(err)
	}
	first := plan[:23]
	if _, _, err := rerecordRun(context.Background(), w, first, recorders, nil); err != nil {
		t.Fatalf("rerecord: part pass: %v", err)
	}
	if got := len(ledger.Charges()); got != len(first) {
		t.Fatalf("%d charges after %d calls", got, len(first))
	}
	// The resumed run still fits: it re-sends nothing, and the ceiling
	// counts the pass's size all the same.
	if breach, err := rerecordBreach(plan[len(first):], len(plan), rerecordCeiling); err != nil {
		t.Fatal(err)
	} else if breach != "" {
		t.Errorf("the remaining %d calls breach the ceiling: %s", len(plan)-len(first), breach)
	}
	responses, resumed, err := rerecordRun(context.Background(), w, plan, recorders, nil)
	if err != nil {
		t.Fatalf("rerecord: resumed pass: %v", err)
	}
	if resumed != len(first) {
		t.Errorf("%d exchanges resumed, want the %d already recorded", resumed, len(first))
	}
	if len(responses) != len(plan)-len(first) {
		t.Errorf("%d new responses, want %d", len(responses), len(plan)-len(first))
	}
	if made := len(ledger.Charges()) - len(first); made != len(plan)-len(first) {
		t.Errorf("the resumed run made %d calls, want %d: a re-send is a second payment",
			made, len(plan)-len(first))
	}
}

// TestRerecordGateRefusesWithoutTheFlag is the paid driver's own
// precondition, checked offline: no environment value, no call, and the
// refusal says so.
func TestRerecordGateRefusesWithoutTheFlag(t *testing.T) {
	missing := rerecordPaidGate(func(string) (string, bool) { return "", false })
	if missing == nil {
		t.Fatal("the paid driver accepted a run with no gate value")
	}
	wrong := rerecordPaidGate(func(string) (string, bool) { return "no", true })
	if wrong == nil {
		t.Fatal("the paid driver accepted DRESSCODE_RERECORD=no")
	}
	if err := rerecordPaidGate(func(string) (string, bool) { return rerecordPaidValue, true }); err != nil {
		t.Fatalf("the paid driver refused its own gate value: %v", err)
	}
	t.Logf("refused without the flag: %v", missing)
}

// ---- Both corpora, read ----

// TestRerecordReadsBothCorpora is the measurement the re-record exists for:
// the shipped corpus and, when a second tree sits beside it, that one too -
// read by the same rules and printed side by side. It is untagged and spends
// nothing: both sides are recordings.
//
// The half that always runs is the doc guard. docs/clef.md transcribes the
// corpus in testdata/clef, and since the swap that corpus is the re-recorded
// one, so the doc-vs-corpus comparison has no reason to be conditional: every
// commit reads it (rerecordReportTranscription). It asserts rather than logs -
// a doc cell that contradicts the corpus it claims to transcribe, on the
// ground-truth letter, on which side of 0.5 it sits or in the digits it prints,
// fails the test and names the cell.
//
// The half that needs two trees is the side-by-side sweep the paid pass owed
// while its scratch tree sat next to the shipped one; it runs when
// testdata/clef-fresh exists, so a future hand that records a second corpus
// gets it back, and its own doc comparison is reported rather than asserted
// because mid-re-record the doc transcribes the old tree while the fresh one is
// the new observation. With no second tree the test logs that and passes - it
// does not skip, because a skip would hide the guard with it.
//
// What it prints, in order: the shipped corpus's own measurement against
// docs/clef.md's transcription, then, when a second corpus is present, its hit
// rate stated as a measured result, its plateau sweep with every chosen line
// beside the curve that chose it, the [check] gates and the model the same way,
// the shipped lines read against the fresh bytes, and every cell whose fresh
// reading contradicts the transcription, named. The contradictions are named
// and left alone: this test does not re-transcribe the doc, and nothing here
// adjusts a recorded number to match a live reading.
func TestRerecordReadsBothCorpora(t *testing.T) {
	w := loadRerecordWorld(t)
	// RunAt on w.oldDir, not Run, so the corpus this test reads and the directory
	// it reports itself under come from one constant: Run names testdata/clef
	// itself, and a label that disagrees with the tree it scored is worse than none.
	shipped, err := RunAt(w.root, w.oldDir)
	if err != nil {
		t.Fatalf("rerecord: shipped corpus: %v", err)
	}

	// docs/clef.md's transcription against the corpus it transcribes. This is
	// the shipped tree on every commit, and the assertions below are what keeps
	// the doc's cells right rather than merely self-consistent.
	docPath := filepath.Join(w.root, "docs", "clef.md")
	tr := rerecordTranscription(t, docPath)
	rerecordReportTranscription(t, tr, shipped.Judge.Curves, filepath.Join("testdata", rerecordShippedDir), true)

	// The doc's account of the thinnest line in the product, in the figures its
	// own curve produces: the chosen line, the plateau that bounds it and the
	// margin on either side. Those digits are rendered here from the measurement
	// rather than retyped, so a figure rounded the wrong way - or a curve that
	// moved under the sentence - names the difference instead of aging quietly.
	// Five decimals carries the line and its margin exactly: the chosen value is
	// the plateau midpoint rounded to five, and the readings either side of it
	// carry four.
	thinnest, ok := rerecordCurveOf(shipped.Judge.Curves, rerecordThinnestLine)
	if !ok {
		t.Fatalf("rerecord: no curve for %s in the shipped corpus, so the doc's claim about it cannot be checked",
			rerecordThinnestLine)
	}
	rerecordDocCarries(t, docPath, fmt.Sprintf("%s chosen %.5f, plateau (%.4f, %.4f), margin %.5f either side",
		rerecordThinnestLine, thinnest.Chosen, thinnest.PlateauLow, thinnest.PlateauHigh, thinnest.Margin),
		[]string{fmt.Sprintf("%.5f on a plateau running %.4f to %.4f, a margin of %.5f either side",
			thinnest.Chosen, thinnest.PlateauLow, thinnest.PlateauHigh, thinnest.Margin)})

	if _, err := os.Stat(w.freshDir); err != nil {
		t.Logf("rerecord: no second corpus beside the shipped one (%v), so the side-by-side half below has"+
			" nothing to compare; the doc guard above ran against %s, which is the corpus %s transcribes",
			err, filepath.Join("testdata", rerecordShippedDir), tr.path)
		return
	}
	fresh, err := RunAt(w.root, w.freshDir)
	if err != nil {
		t.Fatalf("rerecord: fresh corpus: %v (a miss here is a recording the evaluation could not hit)", err)
	}

	// Every recording hit, zero misses, on the fresh tree: the whole corpus
	// read back through replay.Ask and Reply - 37 judge samples, 273 cells,
	// 4 check reads, the eleven stability answers, the three size variants
	// and the no-image note, and a vocabulary scored over all of them.
	t.Logf("fresh corpus hit rate: %d judge samples, %d cells, %d check reads, %d stability answers, "+
		"%d size variants, %d vocabulary cells scored, no miss",
		fresh.Judge.Samples, fresh.Judge.Cells, len(fresh.Check.Reads), fresh.Stability.Answers,
		len(fresh.Sizes.Variants), fresh.Check.Vocabulary.Of)
	if fresh.Judge.Samples != 37 || fresh.Judge.Cells != 273 {
		t.Errorf("the fresh tree read %d samples over %d cells, want 37 over 273",
			fresh.Judge.Samples, fresh.Judge.Cells)
	}
	if len(fresh.Check.Reads) != 4 || fresh.Stability.Answers != 11 || len(fresh.Sizes.Variants) != 3 {
		t.Errorf("the fresh tree read %d checks, %d stability answers, %d size variants, want 4, 11, 3",
			len(fresh.Check.Reads), fresh.Stability.Answers, len(fresh.Sizes.Variants))
	}
	if fresh.Sizes.Note == "" {
		t.Errorf("the fresh tree has no no-image note, so cost-V04-family-function-noimage was not read")
	}
	if fresh.Check.Vocabulary.Of == 0 || fresh.Check.Vocabulary.Of != shipped.Check.Vocabulary.Of {
		t.Errorf("the fresh tree scored %d vocabulary cells, the shipped corpus %d",
			fresh.Check.Vocabulary.Of, shipped.Check.Vocabulary.Of)
	}
	for _, flip := range fresh.Stability.Flips {
		t.Logf("fresh stability: a repeat moved a cell: %s", flip)
	}
	if len(fresh.Stability.Flips) != len(shipped.Stability.Flips) {
		t.Errorf("fresh stability moved %d cells, the shipped corpus %d",
			len(fresh.Stability.Flips), len(shipped.Stability.Flips))
	}

	// The shipped lines, read from the settings file rather than assumed,
	// and checked against what the shipped corpus chose: if they differ, the
	// comparison below would be measuring the wrong line.
	fileSettings, fileLines, fileModel := rerecordShippedLines(t, w.root)
	if fileSettings.Met != shipped.Settings.JudgeMet ||
		!reflect.DeepEqual(fileSettings.PerCriterion, shipped.Settings.JudgePerCriterion) ||
		fileLines.ChangeMade != shipped.Settings.CheckChangeMade ||
		fileLines.SamePerson != shipped.Settings.CheckSamePerson ||
		fileLines.CriterionMet != shipped.Settings.CheckCriterionMet ||
		fileModel != shipped.Settings.CheckModel {
		t.Fatalf("the shipped settings file is not what the shipped corpus chooses, so the comparison "+
			"below would read the wrong line: file met %.5f over %d overrides, corpus met %.5f over %d overrides",
			fileSettings.Met, len(fileSettings.PerCriterion), shipped.Settings.JudgeMet,
			len(shipped.Settings.JudgePerCriterion))
	}

	// The fresh sweep, every chosen line beside the curve that chose it.
	t.Logf("fresh plateau sweep: %d criteria, %d cells, %d agreeing at the chosen lines, %d at 0.50, "+
		"%d of %d criteria apart",
		len(fresh.Judge.Curves), fresh.Judge.Cells, fresh.Judge.ChosenTotal, fresh.Judge.AgreeAtHalf,
		rerecordApart(fresh.Judge.Curves), len(fresh.Judge.Curves))
	t.Logf("  %-32s %5s %6s %7s %8s %8s %8s %8s  %s",
		"criterion", "cells", "max", "chosen", "apart", "low", "high", "margin", "off at 0.50")
	for _, c := range fresh.Judge.Curves {
		t.Logf("  %-32s %5d %6d %.5f  %-7t %8.4f %8.4f %8.4f  %s",
			c.ID, len(c.Cells), c.MaxAgreement, c.Chosen, c.Apart, c.PlateauLow, c.PlateauHigh,
			c.Margin, strings.Join(c.OffAtHalf, " "))
	}
	t.Logf("fresh chosen settings: met %.5f, %d overrides", fresh.Settings.JudgeMet, len(fresh.Settings.JudgePerCriterion))
	for _, id := range rerecordSortedIDs(fresh.Settings.JudgePerCriterion) {
		t.Logf("  per_criterion %-32s %.5f", id, fresh.Settings.JudgePerCriterion[id])
	}
	t.Logf("fresh cells the chosen lines still misread (no line on this set saves them): %s",
		rerecordNameCells(fresh.Judge.Wrong))

	// Every moved line, old against new, each beside the curve that chose it.
	t.Logf("lines moved, shipped to fresh, per criterion:")
	for _, c := range fresh.Judge.Curves {
		old := fileSettings.lineFor(c)
		new := rerecordLineOf(fresh.Settings, c)
		shape := "  "
		if old != new {
			shape = "MOVED"
		}
		t.Logf("  %s %-32s shipped %.5f reads %3d of %3d, fresh %.5f reads %3d of %3d, curve best %3d",
			shape, c.ID, old, agreement(c.Cells, old), len(c.Cells),
			new, agreement(c.Cells, new), len(c.Cells), c.MaxAgreement)
	}

	// The two numbers that matter, both corpora, both lines. The report's
	// chosen settings carry the same two fields a JudgeSettings reads its
	// line from, so rerecordJudgeSettingsOf projects them and one
	// arithmetic measures all four readings.
	freshChosen := rerecordJudgeSettingsOf(fresh.Settings)
	shippedAtShipped, _ := rerecordAgreement(shipped.Judge.Curves, fileSettings)
	freshAtShipped, _ := rerecordAgreement(fresh.Judge.Curves, fileSettings)
	freshAtFresh, _ := rerecordAgreement(fresh.Judge.Curves, freshChosen)
	shippedAtFresh, _ := rerecordAgreement(shipped.Judge.Curves, freshChosen)
	t.Logf("agreement at the shipped lines: the shipped corpus %d of %d, the fresh corpus %d of %d",
		shippedAtShipped, shipped.Judge.Cells, freshAtShipped, fresh.Judge.Cells)
	t.Logf("agreement at the fresh lines: the fresh corpus %d of %d, the shipped corpus read at them %d of %d",
		freshAtFresh, fresh.Judge.Cells, shippedAtFresh, shipped.Judge.Cells)
	t.Logf("perfect separations: the shipped corpus %d of %d criteria, the fresh corpus %d of %d",
		rerecordApart(shipped.Judge.Curves), len(shipped.Judge.Curves),
		rerecordApart(fresh.Judge.Curves), len(fresh.Judge.Curves))
	t.Logf("the default line: shipped corpus best single line %.5f reads %d, fresh corpus %.5f reads %d",
		shipped.Settings.JudgeMet, shipped.Judge.DefaultTotal, fresh.Settings.JudgeMet, fresh.Judge.DefaultTotal)
	for _, c := range fresh.Judge.Curves {
		if sc, ok := rerecordCurveOf(shipped.Judge.Curves, c.ID); ok && sc.Chosen != c.Chosen {
			t.Logf("  curve moved %-32s shipped plateau [%.4f, %.4f] chosen %.5f -> fresh plateau [%.4f, %.4f] chosen %.5f",
				c.ID, sc.PlateauLow, sc.PlateauHigh, sc.Chosen, c.PlateauLow, c.PlateauHigh, c.Chosen)
		}
	}

	// The [check] gates and the model, each line beside the interval that
	// chose it, on the fresh bytes.
	t.Logf("fresh check gates (each chosen line beside the pair of reads that bounds its interval):")
	t.Logf("  %-14s %-12s %8s %8s %8s %8s %8s %8s %6s",
		"gate", "model", "bad", "good", "low", "high", "chosen", "margin", "correct")
	for _, g := range fresh.Check.Gates {
		t.Logf("  %-14s %-12s %8.4f %8.4f %8.4f %8.4f %.5f %8.4f %6d",
			g.Gate, g.Model, g.Bad, g.Good, g.PlateauLow, g.PlateauHigh, g.Chosen, g.Margin, g.Correct)
	}
	for _, jp := range fresh.Check.JointPlateaus {
		t.Logf("  joint %-14s [%.4f, %.4f]", jp.Gate, jp.Low, jp.High)
	}
	t.Logf("fresh chosen [check] lines: change_made %.5f, same_person %.5f, criterion_met %.5f, model %q",
		fresh.Settings.CheckChangeMade, fresh.Settings.CheckSamePerson, fresh.Settings.CheckCriterionMet,
		fresh.Settings.CheckModel)
	t.Logf("shipped [check] lines on fresh reads: change_made %.5f, same_person %.5f, criterion_met %.5f, model %q",
		fileLines.ChangeMade, fileLines.SamePerson, fileLines.CriterionMet, fileModel)
	t.Logf("fresh vocabulary %d of %d attributes, check decisions %d of %d correct, good discarded %d, bad shown %d"+
		" (shipped corpus: %d of %d, %d of %d, %d, %d)",
		fresh.Check.Vocabulary.Correct, fresh.Check.Vocabulary.Of, fresh.Settings.CheckCorrect, fresh.Settings.CheckOf,
		fresh.Check.GoodDiscarded, fresh.Check.BadShown, shipped.Check.Vocabulary.Correct, shipped.Check.Vocabulary.Of,
		shipped.Settings.CheckCorrect, shipped.Settings.CheckOf, shipped.Check.GoodDiscarded, shipped.Check.BadShown)
	// The shipped check lines read against the fresh gates: the decisions
	// the file would make on the fresh bytes, per read.
	for _, name := range []string{"change_made", "same_person", "criterion_met"} {
		oldLine, newLine := rerecordCheckLineOf(fileLines, name), rerecordCheckLineOf(rerecordLinesOf(fresh.Check.Lines), name)
		inside := 0
		for _, g := range fresh.Check.Gates {
			if g.Gate != name || g.Chosen != newLine {
				continue
			}
			if oldLine > g.PlateauLow && oldLine < g.PlateauHigh {
				inside++
			}
			t.Logf("  %-14s %-12s shipped %.5f, fresh %.5f, interval [%.4f, %.4f], shipped line inside for this model: %t",
				name, g.Model, oldLine, g.Chosen, g.PlateauLow, g.PlateauHigh,
				oldLine > g.PlateauLow && oldLine < g.PlateauHigh)
		}
		if inside == 0 && len(fresh.Check.Gates) > 0 {
			t.Logf("  %s: the shipped line is inside no fresh model's interval", name)
		}
	}
	t.Logf("fresh reads decided at the shipped check lines: %s",
		strings.Join(rerecordCheckDecisions(fresh.Check.Reads, fileLines, w.gt), "; "))
	t.Logf("fresh reads decided at the fresh check lines: %s",
		strings.Join(rerecordCheckDecisions(fresh.Check.Reads, rerecordLinesOf(fresh.Check.Lines), w.gt), "; "))

	// The biggest-issue choice and the applied flags, both trees.
	t.Logf("biggest issue named on a variant that misses: fresh %d of %d (shipped %d of %d); missed: %s",
		fresh.Judge.Issues.Named, fresh.Judge.Issues.Of, shipped.Judge.Issues.Named, shipped.Judge.Issues.Of,
		strings.Join(fresh.Judge.Issues.Missed, " "))

	// The same transcription against the second corpus, reported: mid-re-record
	// the doc still transcribes the shipped tree, so a cell the fresh reading
	// puts elsewhere is a finding to name, not a doc to fail.
	rerecordReportTranscription(t, tr, fresh.Judge.Curves, filepath.Join("testdata", rerecordFreshDir), false)
}

// rerecordApart counts the criteria whose cells some line reads all
// correct - the perfect separations docs/clef.md calls "apart".
func rerecordApart(curves []Curve) int {
	var n int
	for _, c := range curves {
		if c.Apart {
			n++
		}
	}
	return n
}

// rerecordCurveOf finds one criterion's curve in a corpus's reading, so the
// two corpora's plateaus can be put beside each other by name. The curves
// are built in criterion order, so a miss means the criterion is in one
// corpus and not the other.
func rerecordCurveOf(curves []Curve, id string) (Curve, bool) {
	for _, c := range curves {
		if c.ID == id {
			return c, true
		}
	}
	return Curve{}, false
}

// rerecordAgreement reads one corpus's curves at one set of lines: the
// shipped lines against fresh bytes and against shipped bytes are the same
// arithmetic here, which is the point of running it twice.
func rerecordAgreement(curves []Curve, s JudgeSettings) (int, map[string]int) {
	total := 0
	per := make(map[string]int, len(curves))
	for _, c := range curves {
		n := agreement(c.Cells, s.lineFor(c))
		per[c.ID] = n
		total += n
	}
	return total, per
}

// rerecordJudgeSettingsOf projects a report's chosen [judge] settings onto
// the JudgeSettings shape rerecordAgreement reads. The report flattens the
// settings for JSON, so the two fields a line is read from are carried in
// Settings.JudgeMet and Settings.JudgePerCriterion; Total is a measurement,
// not a line, and belongs to the report rather than to the settings, so it
// stays unset here and rerecordAgreement computes the total it reports.
func rerecordJudgeSettingsOf(s Settings) JudgeSettings {
	return JudgeSettings{Met: s.JudgeMet, PerCriterion: s.JudgePerCriterion}
}

// rerecordLineOf is JudgeSettings.lineFor for a settings shape that is not
// a JudgeSettings - the report's chosen settings, which carry the same two
// fields the line is read from.
func rerecordLineOf(s Settings, c Curve) float64 {
	if v, ok := s.JudgePerCriterion[c.ID]; ok {
		return v
	}
	return s.JudgeMet
}

// rerecordLinesOf is the check's chosen lines in the shape the pipeline
// takes: the same three fields, and the same conversion evaluateCheck does
// when it applies them. The report holds them as a ChosenLines, so the two
// sets of lines are only comparable after this.
func rerecordLinesOf(l ChosenLines) pipeline.Lines {
	return pipeline.Lines{
		ChangeMade:   l.ChangeMade,
		SamePerson:   l.SamePerson,
		CriterionMet: l.CriterionMet,
	}
}

func rerecordCheckLineOf(l pipeline.Lines, name string) float64 {
	switch name {
	case "change_made":
		return l.ChangeMade
	case "same_person":
		return l.SamePerson
	case "criterion_met":
		return l.CriterionMet
	}
	panic("rerecord: no such gate " + name)
}

// rerecordCheckDecisions applies one set of check lines to a set of reads
// and names the decision, so the shipped lines can be read against the
// fresh bytes exactly as the pipeline reads them: all three gates must pass
// for the render to be shown.
func rerecordCheckDecisions(reads []CheckRead, lines pipeline.Lines, gt GroundTruth) []string {
	bad := map[string]bool{}
	for _, truth := range gt.Checks {
		bad[truth.Exchange] = truth.Bad
	}
	var out []string
	for _, r := range reads {
		shown := r.Change >= lines.ChangeMade && r.Same >= lines.SamePerson && r.Met >= lines.CriterionMet
		call := "shown"
		if !shown {
			call = "discarded"
		}
		verdict := "right"
		if shown == bad[r.Exchange] {
			verdict = "WRONG"
		}
		out = append(out, fmt.Sprintf("%s %s (%s, %s)", r.Exchange, call, map[bool]string{true: "known bad", false: "honest"}[bad[r.Exchange]], verdict))
	}
	return out
}

func rerecordNameCells(cells []OffCell) string {
	if len(cells) == 0 {
		return "none"
	}
	out := make([]string, 0, len(cells))
	for _, c := range cells {
		out = append(out, fmt.Sprintf("%s/%s %.4f want met %t", c.Sample, c.Criterion, c.Value, c.Expected))
	}
	return strings.Join(out, ", ")
}

func rerecordSortedIDs(m map[string]float64) []string {
	out := make([]string, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// ---- docs/clef.md's transcription ----

// rerecordCellKey keys one cell the way this package's other pins key them:
// the variant and the criterion, space separated.
func rerecordCellKey(sample, criterion string) string {
	return sample + " " + criterion
}

// rerecordLetter is docs/clef.md's expected answer for a cell: Y is met, N
// is flagged.
func rerecordLetter(met bool) string {
	if met {
		return "Y"
	}
	return "N"
}

// rerecordCell is one cell as docs/clef.md's judge tables transcribe it: the
// expected answer the doc lifts from testdata/samples/README.md, the
// probability the shipped corpus recorded against it, and the mark the doc
// writes when that probability sits on the other side of 0.5.
type rerecordCell struct {
	Sample    string
	Criterion string
	Expected  bool
	Value     float64
	Marked    bool
}

// rerecordTranscribed is those tables read whole.
type rerecordTranscribed struct {
	path  string
	cells map[string]rerecordCell
}

// rerecordNotEqual is the mark docs/clef.md puts on a cell whose probability
// sits on the other side of 0.5 from its expected answer.
const rerecordNotEqual = "≠"

// rerecordTranscription reads docs/clef.md's transcription of the sample set:
// the five per-occasion tables under "The judge on the sample set", each
// headed | Variant | Person | <criterion columns> | Biggest issue (p) |,
// every criterion cell written "<Y|N> <probability>" with the ≠ mark after it
// where the recorded probability and the expected answer disagree at 0.5.
//
// This is the transcription the charter says does not move with the service:
// the letters are the sample set's ground truth, the same T0.4 reading
// LoadGroundTruth carries in testdata/eval/ground-truth.json, and the
// probabilities are the shipped corpus's own. A re-record changes the second,
// never the first.
//
// The parse is strict, because a comparison that quietly read half the table
// would report no contradiction at all. A row whose cell count does not match
// its header, a column that is not a backticked criterion id, a cell that is
// not a letter and a probability and at most one ≠, a cell transcribed twice:
// each is fatal, with the row's name, rather than skipped.
func rerecordTranscription(t *testing.T, path string) rerecordTranscribed {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("rerecord: transcription: %v", err)
	}
	tr := rerecordTranscribed{path: path, cells: map[string]rerecordCell{}}
	// columns holds the criterion ids of the table being read, in its column
	// order, and is nil between tables: a header that is not the sample
	// tables' opens one, any line that is not a table row closes it.
	var columns []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			columns = nil
			continue
		}
		cols := rerecordRow(trimmed)
		if len(cols) >= 4 && cols[0] == "Variant" && cols[1] == "Person" &&
			strings.HasPrefix(cols[len(cols)-1], "Biggest issue") {
			columns = make([]string, len(cols))
			for i, name := range cols[2 : len(cols)-1] {
				if !strings.HasPrefix(name, "`") || !strings.HasSuffix(name, "`") || len(name) < 3 {
					t.Fatalf("%s: criterion column %d is %q, want a criterion id in backticks",
						path, i+2, name)
				}
				columns[i+2] = strings.Trim(name, "`")
			}
			continue
		}
		if columns == nil || rerecordDividerRow(cols) {
			continue
		}
		if len(cols) != len(columns) {
			t.Fatalf("%s: the row for %q has %d cells, its table has %d",
				path, cols[0], len(cols), len(columns))
		}
		for i, id := range columns {
			if id == "" {
				continue // the Variant, Person and biggest-issue columns
			}
			cell, err := rerecordParseCell(cols[i])
			if err != nil {
				t.Fatalf("%s: %s/%s: %v", path, cols[0], id, err)
			}
			cell.Sample, cell.Criterion = cols[0], id
			key := rerecordCellKey(cell.Sample, cell.Criterion)
			if _, dup := tr.cells[key]; dup {
				t.Fatalf("%s: %s is transcribed twice", path, key)
			}
			// The doc's own mark must follow from the doc's own letter and
			// number. A table that breaks that is either misread here or
			// mis-written there, and either way the comparison below would
			// be measuring the wrong cells.
			if cell.Marked != ((cell.Value >= readingAt) != cell.Expected) {
				t.Fatalf("%s: %s marks %s %s %.4f, and its own numbers put it on the %s side of %.2f",
					path, key, rerecordLetter(cell.Expected), rerecordMark(cell.Marked), cell.Value,
					map[bool]string{true: "other", false: "expected"}[cell.Marked], readingAt)
			}
			tr.cells[key] = cell
		}
	}
	if len(tr.cells) == 0 {
		t.Fatalf("%s: no judge cell read, so the sample tables are not the shape this reads", path)
	}
	return tr
}

// rerecordRow splits one markdown table row into its trimmed cells. The
// leading and trailing pipes frame the row and are not cells.
func rerecordRow(trimmed string) []string {
	body := strings.Trim(trimmed, "|")
	parts := strings.Split(body, "|")
	out := make([]string, len(parts))
	for i, part := range parts {
		out[i] = strings.TrimSpace(part)
	}
	return out
}

// rerecordDividerRow reports the |---|---| row under a table header: every
// cell is dashes, colons or both.
func rerecordDividerRow(cols []string) bool {
	if len(cols) == 0 {
		return false
	}
	for _, col := range cols {
		if col == "" || strings.Trim(col, "-:") != "" {
			return false
		}
	}
	return true
}

// rerecordMark renders a cell's ≠ mark for the log, so a printed line reads
// the way the doc's cell does.
func rerecordMark(marked bool) string {
	if !marked {
		return ""
	}
	return " marked " + rerecordNotEqual
}

// rerecordParseCell reads one transcribed cell: "Y 0.8095", "N 0.0403", and
// either of those followed by the doc's bold ≠ mark.
func rerecordParseCell(text string) (rerecordCell, error) {
	var cell rerecordCell
	cell.Marked = strings.Contains(text, rerecordNotEqual)
	if strings.Count(text, rerecordNotEqual) > 1 {
		return rerecordCell{}, fmt.Errorf("%q carries the %s mark twice", text, rerecordNotEqual)
	}
	stripped := strings.NewReplacer("**", "", rerecordNotEqual, "").Replace(text)
	fields := strings.Fields(stripped)
	if len(fields) != 2 {
		return rerecordCell{}, fmt.Errorf("%q is not an expected answer and a probability", text)
	}
	switch fields[0] {
	case "Y":
		cell.Expected = true
	case "N":
		cell.Expected = false
	default:
		return rerecordCell{}, fmt.Errorf("%q: expected answer %q, want Y or N", text, fields[0])
	}
	value, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return rerecordCell{}, fmt.Errorf("%q: probability: %w", text, err)
	}
	if value < 0 || value > 1 {
		return rerecordCell{}, fmt.Errorf("%q: probability %v is not between 0 and 1", text, value)
	}
	// The cell carries no more digits than the doc's own precision, or the value
	// comparison would be rounding away the difference it exists to name.
	if dot := strings.Index(fields[1], "."); dot >= 0 {
		if digits := len(fields[1]) - dot - 1; digits > rerecordDocDecimals {
			return rerecordCell{}, fmt.Errorf("%q: probability carries %d decimals, these tables carry at most %d",
				text, digits, rerecordDocDecimals)
		}
	}
	cell.Value = value
	return cell, nil
}

// rerecordDocDecimals is the precision docs/clef.md's probabilities carry: the
// service returned at most four decimals on every call (its Response section
// says so), and every cell in the sample tables is written that way.
const rerecordDocDecimals = 4

// rerecordDocValue renders a probability as docs/clef.md carries it, so the two
// sides of the value comparison are compared at the doc's own precision rather
// than at the last bit of a float64. A doc cell written 0.368 and a recording
// reading 0.36800000000000005 are the same number here; 0.368 and 0.3681 are
// not.
func rerecordDocValue(v float64) string {
	return fmt.Sprintf("%.*f", rerecordDocDecimals, v)
}

// rerecordReportTranscription names every cell of one corpus's reading that
// contradicts docs/clef.md's transcription, and leaves it named: the ground
// truth does not move with the service, so a contradiction is a finding about
// the corpus, not a line to re-transcribe, and nothing here adjusts a recorded
// number or a doc cell to make the two agree. Three counts close the report:
// the cells whose side of 0.5 moved, the cells whose printed digits differ from
// the reading at the precision the doc carries, and the cells whose expected
// answer the doc and the committed ground truth disagree about.
//
// corpus names the reading in the failure messages, and transcribes says
// whether this is the corpus the doc means: the shipped tree, on every commit,
// so a stale digit there is a broken claim and fails. A second tree beside it
// is the re-record's own observation while it waits for the swap, and the same
// comparison there is evidence rather than a failure - it is the report the
// charter asks the paid pass to state. Coverage and ground truth are fatal in
// both, because neither depends on which corpus is being read: a cell the doc
// does not transcribe or a transcribed cell the corpus does not read means the
// comparison silently lost a cell, which is the one way this report could
// under-report, and a letter that is not the committed ground truth is never
// right.
func rerecordReportTranscription(t *testing.T, tr rerecordTranscribed, curves []Curve, corpus string, transcribes bool) {
	t.Helper()
	samples := map[string]bool{}
	criteria := map[string]bool{}
	marked := 0
	for _, cell := range tr.cells {
		samples[cell.Sample] = true
		criteria[cell.Criterion] = true
		if cell.Marked {
			marked++
		}
	}
	t.Logf("%s transcribes %d cells over %d samples and %d criteria, %d of them marked %s at %.2f",
		tr.path, len(tr.cells), len(samples), len(criteria), marked, rerecordNotEqual, readingAt)

	side, value, letter, absent := 0, 0, 0, 0
	var lines []string
	read := map[string]bool{}
	for _, c := range curves {
		for _, cell := range c.Cells {
			key := rerecordCellKey(cell.Sample, c.ID)
			doc, ok := tr.cells[key]
			if !ok {
				absent++
				lines = append(lines, fmt.Sprintf("  no transcription for %s: %s %.4f expected met %t",
					key, rerecordLetter(cell.Expected), cell.Value, cell.Expected))
				continue
			}
			read[key] = true
			switch {
			case doc.Expected != cell.Expected:
				letter++
				lines = append(lines, fmt.Sprintf("  ground truth %s: the doc transcribes %s, the committed ground truth says %s (%.4f recorded, %.4f read)",
					key, rerecordLetter(doc.Expected), rerecordLetter(cell.Expected), doc.Value, cell.Value))
			case (doc.Value >= readingAt) != (cell.Value >= readingAt):
				side++
				lines = append(lines, fmt.Sprintf("  side of %.2f %s: doc %s %.4f%s, read %s %.4f%s",
					readingAt, key, rerecordLetter(doc.Expected), doc.Value, rerecordMark(doc.Marked),
					rerecordLetter(cell.Expected), cell.Value,
					rerecordMark((cell.Value >= readingAt) != cell.Expected)))
			case rerecordDocValue(doc.Value) != rerecordDocValue(cell.Value):
				// The digits alone are wrong: same letter, same side of 0.5, so
				// the two counts above and the doc's own mark rule all agree and
				// only the printed probability disagrees with the recording. This
				// is the case that used to pass.
				value++
				lines = append(lines, fmt.Sprintf("  value %s: doc %s %.4f%s, read %s %.4f%s: same expected answer, same side of %.2f, so the printed digits are what disagree at the doc's %d-dp precision",
					key, rerecordLetter(doc.Expected), doc.Value, rerecordMark(doc.Marked),
					rerecordLetter(cell.Expected), cell.Value,
					rerecordMark((cell.Value >= readingAt) != cell.Expected), readingAt,
					rerecordDocDecimals))
			}
		}
	}
	var unread []string
	for key := range tr.cells {
		if !read[key] {
			unread = append(unread, key)
		}
	}
	sort.Strings(unread)
	sort.Strings(lines)
	for _, line := range lines {
		t.Log(line)
	}
	for _, key := range unread {
		t.Errorf("rerecord: %s is transcribed in %s and read by no curve of %s", key, tr.path, corpus)
	}
	if absent > 0 {
		t.Errorf("rerecord: %d cells of %s are not transcribed by %s, so the comparison lost them",
			absent, corpus, tr.path)
	}
	if letter > 0 {
		t.Errorf("rerecord: %d cells contradict the ground truth as %s transcribes it, and the ground truth does not move with the service",
			letter, tr.path)
	}
	if side > 0 && transcribes {
		t.Errorf("rerecord: %d of the %d cells %s transcribes sit on the other side of %.2f in %s than in the corpus it transcribes, so the doc is stale where it claims to be a measurement",
			side, len(tr.cells), tr.path, readingAt, corpus)
	}
	if value > 0 && transcribes {
		t.Errorf("rerecord: %d of the %d cells %s transcribes print a probability %s does not read, at the doc's own %d decimals, on a letter and a side of %.2f both corpora agree about: a mis-transcribed digit, not a drift",
			value, len(tr.cells), tr.path, corpus, rerecordDocDecimals, readingAt)
	}
	t.Logf("transcription of %s against %s: %d cells contradict it on which side of %.2f they sit, "+
		"%d on the printed value at %d decimals, %d on the expected answer, %d transcribed and not read, "+
		"%d read and not transcribed (asserted: %t)",
		corpus, tr.path, side, readingAt, value, rerecordDocDecimals, letter, len(unread), absent, transcribes)
}

// rerecordShippedLines reads the shipped lines out of the committed
// settings file, the way the live harness does: every key is handed
// config.Load a stand-in, so the file's numbers are what resolve and no real
// key value ever enters the Settings struct.
func rerecordShippedLines(t *testing.T, root string) (JudgeSettings, pipeline.Lines, string) {
	t.Helper()
	standins := make(map[string]string, len(config.EnvVars()))
	for i, name := range config.EnvVars() {
		standins[name] = fmt.Sprintf("rerecord-standin-%02d", i+1)
	}
	settings, _, err := config.Load(context.Background(), config.Options{
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
		t.Fatalf("rerecord: settings file: %v", err)
	}
	return JudgeSettings{Met: settings.Judge.Met, PerCriterion: settings.Judge.PerCriterion},
		pipeline.Lines{
			ChangeMade:   settings.Check.ChangeMade,
			SamePerson:   settings.Check.SamePerson,
			CriterionMet: settings.Check.CriterionMet,
		}, settings.Check.Model
}

// ---- shared by the offline proofs and the paid driver ----

// rerecordIsDigest reports whether a reference names the recorder's own
// digest file: images/ plus 64 hex digits and a picture suffix.
func rerecordIsDigest(ref string) bool {
	name := strings.TrimPrefix(ref, "images/")
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if len(stem) != 64 || len(name) <= len(stem) {
		return false
	}
	for _, r := range stem {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// rerecordWalk reads every regular file under root and hands it to fn,
// which fails the test itself. Read-only, like the rest of this file.
func rerecordWalk(t *testing.T, root string, fn func(path string, data []byte)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fn(path, data)
		return nil
	})
	if err != nil {
		t.Fatalf("rerecord: walk %s: %v", root, err)
	}
}

func mustBuild(t *testing.T, w rerecordWorld, c RerecordCall) systemone.Request {
	t.Helper()
	req, err := w.build(c)
	if err != nil {
		t.Fatalf("rerecord: %s: %v", c.Exchange, err)
	}
	return req
}

// equalReports compares two evaluation reports by their measured content.
// A re-run of the same recordings has no reason to differ in any of it, so
// this is the whole report, printed field by field.
func equalReports(a, b Report) bool {
	return fmt.Sprintf("%+v", a) == fmt.Sprintf("%+v", b)
}
