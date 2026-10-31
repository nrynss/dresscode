package pipeline

// Render is this file: the YouCam try-on for the chosen fix. Pick hands
// over the fix carried whole; this file reads the fix's own YouCam task,
// builds the recorded request shape for that task, and runs it through
// the T2.2 runner - which quotes, reserves and settles the rate-card
// line, resumes a recorded task under the job's key, and copies the
// result into mediastore the moment it is in hand, because the provider's
// result link expires in two hours. Check (T3.5) reads what this file
// kept; no probability, no score and no provider link leaves it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/systemone"
	"github.com/nrynss/dresscode/internal/youcam"
	"github.com/nrynss/keel/cost"
)

// Product is one product picture a render can composite: the product's id
// and its photo. The id is the catalog id of a basket entry, or the id of
// the registration the capture screen made for a product the user brought.
// It is part of the render's idempotency key, so it must be a name two
// different pictures never share.
type Product struct {
	ID    string
	Image systemone.Image
}

// Refused is a render the unit budget would not pay for. It carries no
// shame and no render: the look's suggestion stands untouched, and the
// fallback chain (T3.6) owns trying the runner-up and then the
// suggestion alone. Unwrap joins it to the budget's own refusal, so a
// caller branches on this type or on cost.ErrOverBudget and lands in the
// same place.
type Refused struct {
	// FixID and Task name the render the budget was asked for.
	FixID string
	Task  string
	// Units is the rate-card line the render quotes.
	Units cost.Price
	// Err is the budget's refusal, cost.ErrOverBudget.
	Err error
}

func (e *Refused) Error() string {
	return fmt.Sprintf("render: the budget refused %s for fix %q (%s)", e.Units, e.FixID, e.Task)
}

func (e *Refused) Unwrap() error { return e.Err }

// clothCategory is the garment_category the recorded cloth render used
// for a top (docs/youcam.md: the enum's upper_body). Every cloth fix on
// the five menus replaces a top; the recorded cloth request was answered
// with this value, and no other value is guessed. A fix that replaces
// something other than a top arrives with its own recording, and this
// constant widens into a table keyed by the garment value then.
const clothCategory = "upper_body"

// hairColorSrc sends hair_color "src", the user's own colour. The field
// defaults to ref, the template's colour, and hair colour is never
// rendered (docs/occasions.md), so the default must be overridden on
// every hair render.
const hairColorSrc = "src"

// lipPalettes maps the lip_colour value a lip fix sets onto the one
// palette a recorded makeup render painted (docs/youcam.md: #B0172F,
// matte, at 70 intensity). The catalog says a lip product photo is a
// shop photo only - makeup-vto paints a colour, it never composites the
// picture - so this table, not a product, is where a lip render's colour
// comes from. The nude fixes on the date, interview and funeral menus
// have no recorded render, and a hex invented here would be a guess the
// wire was never seen to accept, so those fixes are refused: the refusal
// is the hook where a recorded nude render adds its colour, in one line.
var lipPalettes = map[string]string{
	"red": "#B0172F",
}

// smileStyles maps the expression value a smile fix sets onto the style
// name the recorded smile render used. The data's only smile value is
// the closed-mouth smile; the documented toothy style is not used
// (docs/occasions.md). A second value arrives with its own recording, or
// it does not arrive.
var smileStyles = map[string]string{
	"closed-mouth-smile": "closed_mouth_smile",
}

// renderFlow is one YouCam flow's shape: whether the task composites a
// product photo, whether it renders from a template, which look
// attribute's value the body carries and through which table, and the
// body builder itself.
type renderFlow struct {
	// product says the task composites a product photo. The files are
	// the selfie then the product, and a render with no product is
	// refused: a jewellery or garment swap without the thing it swaps in
	// is not this fix.
	product bool
	// template says the task renders from fix.Template. The loader
	// refuses a hair or beard fix without one; an in-code fix is
	// refused here, before any call.
	template bool
	// value is the look attribute whose value the body carries, and
	// table maps that value onto the provider's own name. Both are
	// resolved in plan - before any call - so a fix whose value has no
	// recorded render is refused without an upload, and the value is
	// never looked up twice.
	value string
	table map[string]string
	// payload builds the task body from the uploaded file ids (the
	// selfie first, then the product when the flow takes one), the fix,
	// and the resolved table value.
	payload func(ids []string, fix *domain.Fix, value string) (json.RawMessage, error)
}

// renderFlows is the fix's YouCam task onto the flow that renders it.
// It is this file's mapping table, the shape Measure keeps its tables
// in, and T3.5 and T3.6 inherit it unchanged. The bodies are the
// recorded shapes (docs/youcam.md), built only from the fix's own data:
// a template the fix names, a palette or style its vocabulary names, a
// garment category the recording used. The necklace carries no placement
// parameters, because the pipeline measures no anchor points - the one
// recorded render that placed a chain by hand named coordinates picked
// for one photo, and a misplacement is what the render check discards,
// never a guessed coordinate here.
var renderFlows = map[string]renderFlow{
	"2d-vto/earring":  {product: true, payload: vtoPayload},
	"2d-vto/necklace": {product: true, payload: vtoPayload},
	"cloth-v4":        {product: true, payload: clothPayload},
	"makeup-vto":      {value: "lip_colour", table: lipPalettes, payload: lipPayload},
	"hair-transfer":   {template: true, payload: hairPayload},
	"beard-style":     {template: true, payload: beardPayload},
	"ai-smile":        {value: "expression", table: smileStyles, payload: smilePayload},
}

// vtoName is one named side of a 2D VTO body: the selfie in source_info,
// the product in object_infos. The page wants the ids twice.
type vtoName struct {
	Name string `json:"name"`
}

// vtoBody is the 2D VTO request shape the recorded earring and necklace
// renders carry: the ids in the file list and again as names.
type vtoBody struct {
	SrcFileID  string    `json:"src_file_id"`
	RefFileIDs []string  `json:"ref_file_ids"`
	SourceInfo vtoName   `json:"source_info"`
	ObjectInfo []vtoName `json:"object_infos"`
}

func vtoPayload(ids []string, _ *domain.Fix, _ string) (json.RawMessage, error) {
	raw, err := json.Marshal(vtoBody{
		SrcFileID:  ids[0],
		RefFileIDs: []string{ids[1]},
		SourceInfo: vtoName{Name: ids[0]},
		ObjectInfo: []vtoName{{Name: ids[1]}},
	})
	if err != nil {
		return nil, fmt.Errorf("render: 2d-vto body: %w", err)
	}
	return raw, nil
}

// clothBody is the cloth-v4 request shape the recorded garment swap
// carries: the selfie, the product, and the category of the swap.
type clothBody struct {
	SrcFileID       string `json:"src_file_id"`
	RefFileID       string `json:"ref_file_id"`
	GarmentCategory string `json:"garment_category"`
}

func clothPayload(ids []string, _ *domain.Fix, _ string) (json.RawMessage, error) {
	raw, err := json.Marshal(clothBody{
		SrcFileID:       ids[0],
		RefFileID:       ids[1],
		GarmentCategory: clothCategory,
	})
	if err != nil {
		return nil, fmt.Errorf("render: cloth body: %w", err)
	}
	return raw, nil
}

// lipVersion, lipShapeOriginal, lipTexture, lipIntensity and
// lipStyleFull are the recorded makeup render's palette settings: the
// lip painted full within its own outline, one matte palette. The shape
// is "original" and there is no morphology - a lip fix must not redraw
// the mouth (docs/occasions.md).
const (
	lipVersion       = "1.0"
	lipShapeOriginal = "original"
	lipTexture       = "matte"
	lipIntensity     = 70
	lipStyleFull     = "full"
)

// lipBody is the makeup-vto request shape the recorded lip render
// carries: version 1.0, and exactly two effects.
type lipBody struct {
	SrcFileID string `json:"src_file_id"`
	Version   string `json:"version"`
	Effects   []any  `json:"effects"`
}

// lipSmooth is the skin_smooth effect, and it must be sent at zero:
// omitted, the provider smooths at 50 and the render is discarded for
// it (docs/youcam.md). The zero value is the point, so the fields carry
// no omitempty.
type lipSmooth struct {
	Category                 string `json:"category"`
	SkinSmoothStrength       int    `json:"skinSmoothStrength"`
	SkinSmoothColorIntensity int    `json:"skinSmoothColorIntensity"`
}

type lipColour struct {
	Category string       `json:"category"`
	Shape    lipShapeName `json:"shape"`
	Palettes []lipPalette `json:"palettes"`
	Style    lipStyleType `json:"style"`
}

type lipShapeName struct {
	Name string `json:"name"`
}

type lipPalette struct {
	Color          string `json:"color"`
	Texture        string `json:"texture"`
	ColorIntensity int    `json:"colorIntensity"`
}

type lipStyleType struct {
	Type string `json:"type"`
}

func lipPayload(ids []string, _ *domain.Fix, palette string) (json.RawMessage, error) {
	raw, err := json.Marshal(lipBody{
		SrcFileID: ids[0],
		Version:   lipVersion,
		Effects: []any{
			lipSmooth{Category: "skin_smooth"},
			lipColour{
				Category: "lip_color",
				Shape:    lipShapeName{Name: lipShapeOriginal},
				Palettes: []lipPalette{{Color: palette, Texture: lipTexture, ColorIntensity: lipIntensity}},
				Style:    lipStyleType{Type: lipStyleFull},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("render: makeup body: %w", err)
	}
	return raw, nil
}

// hairBody is the hair-transfer request shape the recorded hairstyle
// render carries: the template the fix names, and the user's own hair
// colour.
type hairBody struct {
	SrcFileID  string `json:"src_file_id"`
	TemplateID string `json:"template_id"`
	HairColor  string `json:"hair_color"`
}

func hairPayload(ids []string, fix *domain.Fix, _ string) (json.RawMessage, error) {
	raw, err := json.Marshal(hairBody{
		SrcFileID:  ids[0],
		TemplateID: fix.Template,
		HairColor:  hairColorSrc,
	})
	if err != nil {
		return nil, fmt.Errorf("render: hair body: %w", err)
	}
	return raw, nil
}

// beardBody is the beard-style request shape the recorded beard render
// carries: the template the fix names, and nothing else.
type beardBody struct {
	SrcFileID  string `json:"src_file_id"`
	TemplateID string `json:"template_id"`
}

func beardPayload(ids []string, fix *domain.Fix, _ string) (json.RawMessage, error) {
	raw, err := json.Marshal(beardBody{
		SrcFileID:  ids[0],
		TemplateID: fix.Template,
	})
	if err != nil {
		return nil, fmt.Errorf("render: beard body: %w", err)
	}
	return raw, nil
}

// smileBody is the ai-smile request shape the recorded smile render
// carries.
type smileBody struct {
	SrcFileID      string `json:"src_file_id"`
	ExpressionType string `json:"expression_type"`
}

func smilePayload(ids []string, _ *domain.Fix, style string) (json.RawMessage, error) {
	raw, err := json.Marshal(smileBody{
		SrcFileID:      ids[0],
		ExpressionType: style,
	})
	if err != nil {
		return nil, fmt.Errorf("render: smile body: %w", err)
	}
	return raw, nil
}

// vocabularyValue is the value a fix sets on one look attribute. The
// fix's vocabulary is the data's own word for what the render paints,
// and the body builders read it from there, never from a fix id.
func vocabularyValue(fix *domain.Fix, attribute string) (string, error) {
	for _, ch := range fix.Vocabulary {
		if ch.Attribute == attribute {
			return ch.To, nil
		}
	}
	return "", fmt.Errorf("render: fix %q names no %s value to render with", fix.ID, attribute)
}

// plan builds the YouCam job one render runs under: the idempotency key,
// the files in upload order, and the payload builder. Every refusal it
// can make is made here, before any call - the payload's own table
// lookups included - so a fix whose render has no recorded shape is
// refused with nothing uploaded and nothing spent.
func plan(photo Photo, fix *domain.Fix, brought, basket *Product) (youcam.Job, error) {
	task := fix.YouCam.Task
	flow, ok := renderFlows[task]
	if !ok {
		return youcam.Job{}, fmt.Errorf("render: fix %q renders by %q, which no flow on this seam builds", fix.ID, task)
	}
	if flow.template && fix.Template == "" {
		return youcam.Job{}, fmt.Errorf("render: fix %q renders by %q and names no template", fix.ID, task)
	}
	value := ""
	if flow.value != "" {
		v, err := vocabularyValue(fix, flow.value)
		if err != nil {
			return youcam.Job{}, err
		}
		name, ok := flow.table[v]
		if !ok {
			return youcam.Job{}, fmt.Errorf("render: fix %q sets %s %q, which no recorded render on this seam used", fix.ID, flow.value, v)
		}
		value = name
	}
	product := brought
	if product == nil {
		product = basket
	}
	if flow.product {
		if product == nil {
			return youcam.Job{}, fmt.Errorf("render: fix %q composites a product photo, and none was brought or found in the basket", fix.ID)
		}
		if product.ID == "" {
			return youcam.Job{}, fmt.Errorf("render: fix %q's product has no id to carry in its render key", fix.ID)
		}
		if len(product.Image.Bytes) == 0 {
			return youcam.Job{}, fmt.Errorf("render: fix %q's product %q has no picture to composite", fix.ID, product.ID)
		}
		if product.Image.ContentType == "" {
			return youcam.Job{}, fmt.Errorf("render: fix %q's product %q has no content type", fix.ID, product.ID)
		}
	}
	files := []youcam.File{{
		Task:        task,
		ContentType: photo.Image.ContentType,
		Bytes:       photo.Image.Bytes,
	}}
	if flow.product {
		files = append(files, youcam.File{
			Task:        task,
			ContentType: product.Image.ContentType,
			Bytes:       product.Image.Bytes,
		})
	}
	return youcam.Job{
		Key:   renderKey(photo.User, photo.Hash, fix, product, flow.product),
		Task:  task,
		Files: files,
		Payload: func(ids []string) (json.RawMessage, error) {
			return flow.payload(ids, fix, value)
		},
		Owner: photo.User,
		Group: renderGroup(photo.User, photo.Hash),
	}, nil
}

// renderKey is the render job's idempotency key: one render task at the
// provider, ever. Measure's discipline is analysis/<user>/<hash>/<task>;
// this is its render side, render/<user>/<hash>/<fix>, with one reasoned
// addition: a flow that composites a product names the product as a
// fifth segment. The product's bytes are part of what the provider
// renders, so the same user, photo and fix with a different product is a
// different picture - and if they shared a key, the second render would
// resume the first's recorded task and put the wrong jhumkas on the
// user's face. The flows that composite nothing (lip, hair, beard,
// smile) render from the fix's own data alone, so their key is exactly
// the suggested shape.
func renderKey(user, photoHash string, fix *domain.Fix, product *Product, composited bool) string {
	key := "render/" + user + "/" + photoHash + "/" + fix.ID
	if composited {
		key += "/" + product.ID
	}
	return key
}

// renderGroup is the mediastore eviction unit one user's renders of one
// photo join. A look is one selfie, so deleting the photo - or the
// account - can drop every render of it in one group delete, and no
// render outlives its photo.
func renderGroup(user, photoHash string) string {
	return "render/" + user + "/" + photoHash
}

// Render runs the YouCam try-on for one fix on one photo, with the
// product the user brought or one from the basket, and returns the
// render copied into mediastore. Check is left nil: Clef's read of the
// render is the next stage's (T3.5), and a render is shown only after
// that read passes.
//
// Quote before running: the rate-card line is read here, and the runner
// reserves it before anything uploads - a budget that cannot afford the
// line refuses the render with zero calls and zero spend, as a typed
// *Refused whose unwrap is cost.ErrOverBudget. The look's suggestion is
// the caller's and stands untouched; the fallback chain (T3.6) owns
// what a refused render hands the user.
//
// A fix that renders by a task no flow on this seam builds, a reference
// render with no product to composite, and a lip or smile whose value
// no recorded render used, are all refused before any call: refusing is
// where a future recording adds them, and skipping silently would show
// the user something the recordings never saw.
func Render(ctx context.Context, runner *youcam.Runner, photo Photo, fix *domain.Fix, brought, basket *Product) (domain.Render, error) {
	if runner == nil {
		return domain.Render{}, errors.New("render: no youcam runner")
	}
	if fix == nil {
		return domain.Render{}, errors.New("render: no fix")
	}
	if photo.User == "" {
		return domain.Render{}, errors.New("render: no user")
	}
	if photo.Hash == "" {
		return domain.Render{}, errors.New("render: no photo hash")
	}
	if len(photo.Image.Bytes) == 0 {
		return domain.Render{}, errors.New("render: the photo is empty")
	}
	if photo.Image.ContentType == "" {
		return domain.Render{}, errors.New("render: the photo has no content type")
	}
	job, err := plan(photo, fix, brought, basket)
	if err != nil {
		return domain.Render{}, err
	}
	units, ok := youcam.UnitsFor(job.Task)
	if !ok {
		return domain.Render{}, fmt.Errorf("render: fix %q renders by %q, which is not on the rate card", fix.ID, job.Task)
	}
	outcome, err := runner.Run(ctx, job)
	if err != nil {
		if errors.Is(err, cost.ErrOverBudget) {
			return domain.Render{}, &Refused{FixID: fix.ID, Task: job.Task, Units: units, Err: err}
		}
		return domain.Render{}, fmt.Errorf("render: fix %q %s: %w", fix.ID, job.Task, err)
	}
	return domain.Render{MediaID: outcome.MediaID, FixID: fix.ID}, nil
}
