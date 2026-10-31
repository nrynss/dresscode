package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nrynss/dresscode/internal/systemone"
)

// captureClient wraps the replay and keeps the response of the last
// call, so the sweep reads the raw probabilities the pipeline consumed
// and died on. The replay hit is itself the byte-equality proof: Ask
// serves a recording only to the identical request.
type captureClient struct {
	inner systemone.SystemOne
	resp  systemone.Response
}

func (c *captureClient) Ask(ctx context.Context, req systemone.Request) (systemone.Response, error) {
	resp, err := c.inner.Ask(ctx, req)
	if err != nil {
		return systemone.Response{}, err
	}
	c.resp = resp
	return resp, nil
}

// imageRef is one recorded image reference: a file relative to the
// recording's own folder, the format docs/clef.md records.
type imageRef struct {
	ContentType string `json:"content_type"`
	File        string `json:"file"`
}

// recordingFile is the replay envelope on disk, read for its exchange
// id, its state, and the image files its request names by reference.
type recordingFile struct {
	Exchange string `json:"exchange"`
	Request  struct {
		Body struct {
			Model  string          `json:"model"`
			State  json.RawMessage `json:"state"`
			Images []imageRef      `json:"images"`
		} `json:"body"`
	} `json:"request"`
}

func readRecording(path string) (recordingFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return recordingFile{}, err
	}
	var rec recordingFile
	if err := json.Unmarshal(data, &rec); err != nil {
		return recordingFile{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if rec.Exchange == "" || len(rec.Request.Body.Images) == 0 {
		return recordingFile{}, fmt.Errorf("%s: exchange and images are required", filepath.Base(path))
	}
	return rec, nil
}

// readImage reads one referenced photo. The bytes are the recording's
// own, so the replayed request keys exactly as the recorded one did.
func readImage(dir string, ref imageRef) (systemone.Image, error) {
	if ref.File == "" {
		return systemone.Image{}, fmt.Errorf("image names no file")
	}
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(ref.File)))
	if err != nil {
		return systemone.Image{}, err
	}
	return systemone.Image{ContentType: ref.ContentType, Bytes: raw}, nil
}

// stateFields decodes a recorded state and returns the named fields'
// values. The pipeline's own tests pin that these states carry no
// third field; this read only lifts what the replay needs to rebuild
// the identical request.
func stateFields(raw json.RawMessage, keys ...string) ([]string, error) {
	var fields map[string]string
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = fields[k]
	}
	return out, nil
}

// answerEnvelope decodes a stored response envelope: the recorded reply
// is Cloudflare's wrapper around the model's answers.
func answerEnvelope(raw json.RawMessage) (systemone.Response, error) {
	var env struct {
		Body struct {
			Result struct {
				Model   string                      `json:"model"`
				Answers map[string]systemone.Answer `json:"answers"`
			} `json:"result"`
		} `json:"body"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return systemone.Response{}, err
	}
	if env.Body.Result.Model == "" || len(env.Body.Result.Answers) == 0 {
		return systemone.Response{}, fmt.Errorf("the stored response carries no answers")
	}
	return systemone.Response{Model: env.Body.Result.Model, Answers: env.Body.Result.Answers}, nil
}
