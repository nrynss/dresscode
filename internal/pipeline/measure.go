package pipeline

// Measure is this file: the YouCam analyses the chosen fix names, and no
// other work. Pick hands over the fix carried whole; this file reads that
// fix's own analysis list and runs exactly what it names, once per user,
// photo and task, through the T2.2 runner. Render (T3.4) reads the values
// it returns; the run (T3.6) inherits the mapping tables below unchanged.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/systemone"
	"github.com/nrynss/dresscode/internal/youcam"
)

// Photo is one normalised selfie as measure reads it: the bytes travel to
// YouCam only when a cache miss needs them, and the hash decides whether
// they travel at all.
type Photo struct {
	// User is the account the photo belongs to. The cache is scoped to
	// the user, so two accounts never share a face reading (the
	// analysis_cache table's own rule).
	User string
	// Hash is the normalised photo's hash, the store's photo_hash. It is
	// the analysis cache key: a look on a photo already measured spends
	// nothing, whatever fix it picks.
	Hash string
	// Image is the normalised selfie itself. An analysis that needs it
	// uploads it as the task's source; a cache hit never sees it.
	Image systemone.Image
}

// Cache is the analysis cache: the analysis_cache table's rows, one per
// user, photo hash and task, holding the four allowlist columns and
// nothing else. An empty field in a held row is a read that yielded
// nothing; the row existing is what makes the read cached, so a second
// look never pays to read it again. Age, gender, ratios and scores have
// no column and no method to arrive by.
type Cache interface {
	// Held returns the values one earlier read of task stored for this
	// user's photo, and whether the read is cached at all.
	Held(ctx context.Context, user, photoHash, task string) (domain.Allowlist, bool, error)
	// Keep stores one read's values under its cache key. It is called
	// once per run, after the task succeeded, never for a cache hit.
	Keep(ctx context.Context, user, photoHash, task string, values domain.Allowlist) error
}

// allowlistField is one attribute of the mapping's value side: how a
// measured value lands in the allowlist the picker receives, and how the
// same value reads back out of a held cache row. The get and the set are
// the analysis_cache column of the same name.
type allowlistField struct {
	get func(domain.Allowlist) string
	set func(*domain.Allowlist, string)
}

// allowlistFields is the mapping table keyed by what the fix changes:
// colour tones, face shape, hair type and length, the picker allowlist's
// four. T3.4's render and T3.6's run inherit it through Measure and
// Cache: these four are every value an analysis can put on the pick
// request, and every column a cache row can hold. An attribute absent
// here could not be stored, read back, or carried, so there is no fifth.
var allowlistFields = map[domain.PickerAttribute]allowlistField{
	domain.AttrFaceShape: {
		get: func(a domain.Allowlist) string { return a.FaceShape },
		set: func(a *domain.Allowlist, v string) { a.FaceShape = v },
	},
	domain.AttrHairType: {
		get: func(a domain.Allowlist) string { return a.HairType },
		set: func(a *domain.Allowlist, v string) { a.HairType = v },
	},
	domain.AttrHairLength: {
		get: func(a domain.Allowlist) string { return a.HairLength },
		set: func(a *domain.Allowlist, v string) { a.HairLength = v },
	},
	domain.AttrColourTones: {
		get: func(a domain.Allowlist) string { return a.ColourTones },
		set: func(a *domain.Allowlist, v string) { a.ColourTones = v },
	},
}

// analysisFields maps the use_fields name a fix's analysis carries onto
// the attribute it fills. It is this file's side of the bridge
// domain.Allowlist draws (its table is unexported, and its loader refuses
// a fix whose use_fields name anything else), so a name missing here is a
// fix built in code, never loaded data.
//
// Face shape is the only reply field a recorded YouCam analysis carries
// (the T2.2 client decodes faceshape and never reads age, gender or a
// ratio off the wire). Hair type and length and colour tones are on the
// picker allowlist, but no menu task returns them: hair-type-detection
// wants a front, a left and a right, and the product is one selfie
// (docs/occasions.md), and no fix runs a shade match. A fix whose
// use_fields name them is refused by planFields, never half-served.
var analysisFields = map[string]domain.PickerAttribute{
	"faceShape": domain.AttrFaceShape,
}

// analysisJob is one analysis run's inputs: the idempotency key, the
// photo, and the features the fix's data asked for.
type analysisJob struct {
	// key is the providertask idempotency key, derived from the user,
	// the photo hash and the task. One key is one task at the provider,
	// ever: a restart of the same look resumes the recorded task and
	// never creates - and never pays for - a second one.
	key string
	// photo is the normalised selfie, uploaded as the task's source.
	photo systemone.Image
	// features are the fields asked of the task, in the fix's data
	// order, after never_use's veto.
	features []string
}

// analysisRun carries one analysis task from upload to decoded values. It
// returns the attributes the reply yielded; the runner has already
// quoted, reserved and settled the task's rate-card line by the time it
// returns, and a refusal comes back as the seam's typed error.
type analysisRun func(ctx context.Context, runner *youcam.Runner, job analysisJob) (map[domain.PickerAttribute]string, error)

// analysisRuns is the mapping's runnable side, keyed by the analysis task
// name the fix's data carries (domain.Fix.Analysis[].Task). A task absent
// from this table has no run on this seam, and a fix that names one is
// refused before any call and any unit: skin-tone-analysis - the colour
// tones a shade match would read - is off the menu (docs/occasions.md: no
// fix runs a shade match), is not on the T2.2 rate card, and has no
// recorded reply to decode; hair-type-detection wants three views this
// one selfie cannot give. Refusing is the hook where a future task adds
// the run: a rate-card line, a decoded field, and a recording, then an
// entry here. Skipping silently would render a fix without the
// measurement it names, so it is never taken.
var analysisRuns = map[string]analysisRun{
	"face-attr-analysis": runFaceAttributes,
}

// runFaceAttributes asks Face Attributes once and reads the face shape
// alone. The body is the recorded shape, {"src_file_id","features"}, the
// source the only file; the features are the fix's own use_fields, which
// the shipped data limits to faceShape - never agegender, never
// facialratio, never face_quality, which the client never decodes.
func runFaceAttributes(ctx context.Context, runner *youcam.Runner, job analysisJob) (map[domain.PickerAttribute]string, error) {
	outcome, err := runner.Run(ctx, youcam.Job{
		Key:  job.key,
		Task: "face-attr-analysis",
		Files: []youcam.File{{
			Task:        "face-attr-analysis",
			ContentType: job.photo.ContentType,
			Bytes:       job.photo.Bytes,
		}},
		Payload: func(ids []string) (json.RawMessage, error) {
			// The job's Files is the one source, so ids is its one file id.
			return json.Marshal(struct {
				SrcFileID string   `json:"src_file_id"`
				Features  []string `json:"features"`
			}{SrcFileID: ids[0], Features: job.features})
		},
	})
	if err != nil {
		return nil, err
	}
	return map[domain.PickerAttribute]string{domain.AttrFaceShape: outcome.FaceShape}, nil
}

// Measure runs, for the chosen fix only, the analyses the fix's data
// names. A fix that names none - every clothes, jewellery, lip, beard and
// smile fix on the five menus - returns an empty allowlist and makes no
// call of any kind, spends no unit, and touches no cache row. A fix that
// names one runs it once per user, photo and task: the first look pays
// the task's rate-card line through the runner, every later look on the
// same photo reads the cache and spends nothing.
//
// The returned allowlist holds only what this fix's analyses yielded. It
// is the value the pick request may carry on a later look and the render
// step reads; an attribute the analysis did not produce stays empty, and
// no probability, score, age, gender or ratio has a way into it.
//
// An analysis whose task has no run on this seam, or whose fields are all
// withheld or unknown, is refused with an error before any call: the look
// stops at measure and the fallback chain (T3.6) owns what happens next.
// An empty photo is refused as the judge refuses one - a photo that lost
// its bytes must not be measured, judged or rendered.
func Measure(ctx context.Context, runner *youcam.Runner, cache Cache, photo Photo, fix *domain.Fix) (domain.Allowlist, error) {
	if runner == nil {
		return domain.Allowlist{}, errors.New("measure: no youcam runner")
	}
	if cache == nil {
		return domain.Allowlist{}, errors.New("measure: no analysis cache")
	}
	if fix == nil {
		return domain.Allowlist{}, errors.New("measure: no fix")
	}
	if len(fix.Analysis) == 0 {
		return domain.Allowlist{}, nil
	}
	if photo.User == "" {
		return domain.Allowlist{}, errors.New("measure: no user")
	}
	if photo.Hash == "" {
		return domain.Allowlist{}, errors.New("measure: no photo hash")
	}
	if len(photo.Image.Bytes) == 0 {
		return domain.Allowlist{}, errors.New("measure: the photo is empty")
	}
	if photo.Image.ContentType == "" {
		return domain.Allowlist{}, errors.New("measure: the photo has no content type")
	}
	var out domain.Allowlist
	for _, a := range fix.Analysis {
		run, ok := analysisRuns[a.Task]
		if !ok {
			return domain.Allowlist{}, fmt.Errorf("measure: fix %q names analysis %q, which no task on this seam runs", fix.ID, a.Task)
		}
		features, attrs, err := planFields(fix, a)
		if err != nil {
			return domain.Allowlist{}, err
		}
		held, ok, err := cache.Held(ctx, photo.User, photo.Hash, a.Task)
		if err != nil {
			return domain.Allowlist{}, fmt.Errorf("measure: fix %q analysis %s: %w", fix.ID, a.Task, err)
		}
		if !ok {
			measured, err := run(ctx, runner, analysisJob{
				key:      analysisKey(photo.User, photo.Hash, a.Task),
				photo:    photo.Image,
				features: features,
			})
			if err != nil {
				return domain.Allowlist{}, fmt.Errorf("measure: fix %q analysis %s: %w", fix.ID, a.Task, err)
			}
			held = domain.Allowlist{}
			for attr, value := range measured {
				field, ok := allowlistFields[attr]
				if !ok {
					return domain.Allowlist{}, fmt.Errorf("measure: fix %q analysis %s yielded %v, which is not on the allowlist", fix.ID, a.Task, attr)
				}
				field.set(&held, value)
			}
			if err := cache.Keep(ctx, photo.User, photo.Hash, a.Task, held); err != nil {
				return domain.Allowlist{}, fmt.Errorf("measure: fix %q analysis %s: %w", fix.ID, a.Task, err)
			}
		}
		for _, attr := range attrs {
			field, ok := allowlistFields[attr]
			if !ok {
				return domain.Allowlist{}, fmt.Errorf("measure: fix %q analysis %s fills %v, which is not on the allowlist", fix.ID, a.Task, attr)
			}
			field.set(&out, field.get(held))
		}
	}
	return out, nil
}

// planFields resolves one analysis entry into the features the task is
// asked for and the attributes the answer fills, in the fix's data order.
// never_use vetoes a field even where use_fields names it; a use_field
// the mapping does not carry is an error, never a silent drop, because a
// quietly skipped field would measure less than the fix names.
func planFields(fix *domain.Fix, a domain.Analysis) (features []string, attrs []domain.PickerAttribute, err error) {
	for _, name := range a.UseFields {
		if slices.Contains(a.NeverUse, name) {
			continue
		}
		attr, ok := analysisFields[name]
		if !ok {
			return nil, nil, fmt.Errorf("measure: fix %q analysis %s asks for %q, which is not on the allowlist", fix.ID, a.Task, name)
		}
		features = append(features, name)
		attrs = append(attrs, attr)
	}
	if len(features) == 0 {
		return nil, nil, fmt.Errorf("measure: fix %q analysis %s names no field the pipeline may ask", fix.ID, a.Task)
	}
	return features, attrs, nil
}

// analysisKey is the analysis job's idempotency key: one analysis task
// per user, photo and task, ever. Two fixes that name the same analysis
// on one photo share it, so the second fix spends nothing either.
func analysisKey(user, photoHash, task string) string {
	return "analysis/" + user + "/" + photoHash + "/" + task
}
