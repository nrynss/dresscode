package systemone

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// The recorder writes what Open reads: one exchange per file,
// <dir>/<exchange>.json, in the envelope docs/clef.md records. See the
// Recorder type for the envelope, the refusals and the naming.

// recordedStatus is the status a stored exchange carries. Only a call
// the inner client could use is stored, and that is a success.
const recordedStatus = 200

// imageDir is the folder under the recording directory where the
// Recorder puts an image no existing file matches.
const imageDir = "images"

// tempPattern names the half-written exchange file. It is not a .json,
// so a directory holding one still opens; the name is only clutter a
// crash leaves behind.
const tempPattern = ".exchange-*.tmp"

// AlreadyRecorded is a recording the Recorder refused to replace. The
// default is refusal because a re-record is a paid run: a call that
// spends to overwrite what the last call paid for is a loss twice, and
// a run that clobbers half a corpus before it fails loses the corpus
// too. Set RecorderConfig.Overwrite to say the replacement is meant.
type AlreadyRecorded struct {
	Exchange string
	Path     string
}

func (e *AlreadyRecorded) Error() string {
	return fmt.Sprintf("systemone: %s is already recorded at %s; set Overwrite to replace it", e.Exchange, e.Path)
}

// ImageResolver names the file a recorded image should point at instead
// of carrying its bytes again. Ref is relative to dir, the recording's
// own directory, and slash separated. ok is false to let the Recorder
// write the bytes under <dir>/images/ itself.
//
// The Recorder reads the file a resolver names and compares its bytes
// with the ones the call sent before it stores the reference. A
// reference to other bytes would be a recording of a request that was
// never sent, and the replay would key it as one that was.
type ImageResolver func(dir, contentType string, data []byte) (ref string, ok bool)

// RecorderConfig is what a Recorder wraps and where it writes.
type RecorderConfig struct {
	// Inner answers the call. The Recorder returns exactly what it
	// returns and stores nothing when it fails. Required.
	Inner SystemOne
	// Dir is the recording directory, the one path Open is pointed at
	// afterwards. It is made absolute and created when the first
	// exchange is written. Required.
	Dir string
	// Images, when set, points an image at a file that already exists
	// instead of writing its bytes a second time. Nil writes every
	// image into Dir. See MatchImages.
	Images ImageResolver
	// Overwrite lets one call replace a recording that is already
	// there. The default refuses; see AlreadyRecorded.
	Overwrite bool
}

// Recorder is a SystemOne that passes every call to an inner client and
// stores the exchange, so the corpus writes itself instead of coming
// from a scratch script nobody committed. Ask answers as the inner
// client does: the same response on success, the same error on
// failure, nothing stored when the call did not work.
//
// It refuses before the inner client runs, so a wasted call is never
// paid for: a name that cannot be a file, a request the model would
// refuse, and a recording already on disk are each answered with no
// call made at all.
//
// Safe for concurrent use. Two calls naming one exchange are refused,
// and a file is published by link, so one recording is never read as a
// half-written one.
type Recorder struct {
	inner     SystemOne
	dir       string
	images    ImageResolver
	overwrite bool
	mu        sync.Mutex
	inFlight  map[string]bool
}

var _ SystemOne = (*Recorder)(nil)

// NewRecorder returns a recorder over cfg. It creates nothing and does
// not dial.
func NewRecorder(cfg RecorderConfig) (*Recorder, error) {
	if cfg.Inner == nil {
		return nil, errors.New("systemone: recorder has no inner client")
	}
	if strings.TrimSpace(cfg.Dir) == "" {
		return nil, errors.New("systemone: recorder directory is empty")
	}
	dir, err := filepath.Abs(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("systemone: recorder directory: %w", err)
	}
	return &Recorder{
		inner:     cfg.Inner,
		dir:       dir,
		images:    cfg.Images,
		overwrite: cfg.Overwrite,
		inFlight:  map[string]bool{},
	}, nil
}

// Dir returns the absolute recording directory the Recorder writes to.
func (r *Recorder) Dir() string { return r.dir }

// Ask answers through the inner client and records the exchange under
// the request's own key. That name is the honest fallback for a client
// this interface is handed to, and it is enough for one call per
// request: a repeat of the same request is the same key, so the second
// one is refused. A corpus with human-readable ids and deliberate
// repeats is written with Record.
func (r *Recorder) Ask(ctx context.Context, req Request) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	if err := req.Validate(); err != nil {
		return Response{}, err
	}
	key, err := req.key()
	if err != nil {
		return Response{}, err
	}
	return r.record(ctx, key, req)
}

// Record answers through the inner client and stores the exchange under
// the name the caller chose. Exchange ids are what the corpus reads
// as, so they are written by the caller: judge-V04-family-function,
// stability-V04-family-function-01, cost-V04-family-function-512. Ten
// repeats of one request are ten names and one request; the name cannot
// come from the bytes.
func (r *Recorder) Record(ctx context.Context, exchange string, req Request) (Response, error) {
	return r.record(ctx, exchange, req)
}

func (r *Recorder) record(ctx context.Context, exchange string, req Request) (Response, error) {
	if r == nil {
		return Response{}, errors.New("systemone: no recorder")
	}
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	if err := validExchange(exchange); err != nil {
		return Response{}, err
	}
	if err := req.Validate(); err != nil {
		return Response{}, err
	}
	release, err := r.claim(exchange)
	if err != nil {
		return Response{}, err
	}
	defer release()

	start := time.Now()
	resp, err := r.inner.Ask(ctx, req)
	if err != nil {
		// A failed call stores nothing. An error-shaped recording in a
		// corpus is worse than a missing one: the next Open either fails
		// on it or serves a broken reply to a request that was never
		// answered.
		return Response{}, err
	}
	stored, err := r.envelope(exchange, req, resp, time.Since(start))
	if err != nil {
		return Response{}, err
	}
	if err := r.writeImages(stored.pending); err != nil {
		return Response{}, err
	}
	if err := r.publish(exchange, stored.payload); err != nil {
		return Response{}, err
	}
	return resp, nil
}

type exchangePlan struct {
	payload []byte
	pending []pendingImage
}

// envelope builds the file's bytes and the image files that have to be
// written with it. Nothing is written here: a request the recorder
// cannot encode leaves the directory as it was.
func (r *Recorder) envelope(exchange string, req Request, resp Response, elapsed time.Duration) (exchangePlan, error) {
	refs, pending, err := r.planImages(req.Images)
	if err != nil {
		return exchangePlan{}, err
	}
	requestBody, err := json.Marshal(recordedBody{
		Model:     req.Model,
		Images:    refs,
		State:     req.State,
		Questions: req.Questions,
	})
	if err != nil {
		return exchangePlan{}, fmt.Errorf("systemone: %s: request: %w", exchange, err)
	}
	responseBody, err := clefEnvelope(resp)
	if err != nil {
		return exchangePlan{}, fmt.Errorf("systemone: %s: response: %w", exchange, err)
	}
	response := storedResponse{
		Status:    recordedStatus,
		ElapsedMS: elapsed.Milliseconds(),
		Body:      responseBody,
	}
	// The stored reply must be one the replay can serve. This is the
	// reader's own rule, run on the reader's own bytes before any of
	// them reach the disk.
	probe, err := json.Marshal(response)
	if err != nil {
		return exchangePlan{}, fmt.Errorf("systemone: %s: response: %w", exchange, err)
	}
	if _, ok := decodeResponse(probe); !ok {
		return exchangePlan{}, fmt.Errorf("systemone: %s: the reply is not one the replay can serve", exchange)
	}
	payload, err := marshalIndent(envelopeFile{
		Exchange: exchange,
		Request:  storedRequest{Method: "POST", Path: runPath(req.Model), Body: requestBody},
		Response: response,
	})
	if err != nil {
		return exchangePlan{}, fmt.Errorf("systemone: %s: %w", exchange, err)
	}
	return exchangePlan{payload: payload, pending: pending}, nil
}

// planImages turns every image into a reference. A resolver's answer is
// checked against the bytes that were sent; anything else goes into the
// recording directory under its own digest, which is why the same photo
// in a second exchange never appears there twice.
func (r *Recorder) planImages(images []Image) ([]recordedImage, []pendingImage, error) {
	if len(images) == 0 {
		return nil, nil, nil
	}
	refs := make([]recordedImage, 0, len(images))
	var pending []pendingImage
	for i, image := range images {
		ext, err := imageExtension(image.ContentType)
		if err != nil {
			return nil, nil, fmt.Errorf("systemone: image %d: %w", i, err)
		}
		if r.images != nil {
			if ref, ok := r.images(r.dir, image.ContentType, image.Bytes); ok {
				if err := r.checkRef(ref, image.Bytes); err != nil {
					return nil, nil, fmt.Errorf("systemone: image %d: %w", i, err)
				}
				refs = append(refs, recordedImage{ContentType: image.ContentType, File: ref})
				continue
			}
		}
		rel := imageDir + "/" + digestName(image.Bytes) + "." + ext
		abs := r.fileAt(rel)
		data, err := os.ReadFile(abs)
		switch {
		case err == nil:
			if !bytes.Equal(data, image.Bytes) {
				return nil, nil, fmt.Errorf("systemone: image %d: %s holds other bytes", i, rel)
			}
		case errors.Is(err, os.ErrNotExist):
			pending = append(pending, pendingImage{rel: rel, data: image.Bytes})
		default:
			return nil, nil, fmt.Errorf("systemone: image %d: %s: %w", i, rel, err)
		}
		refs = append(refs, recordedImage{ContentType: image.ContentType, File: rel})
	}
	return refs, pending, nil
}

// checkRef reads the file a resolver named, relative to the recording
// directory, and insists it holds the bytes that went out.
func (r *Recorder) checkRef(ref string, sent []byte) error {
	if ref == "" {
		return errors.New("resolver named an empty file")
	}
	if filepath.IsAbs(ref) {
		return fmt.Errorf("resolver named %s, not a path relative to the recording", ref)
	}
	data, err := os.ReadFile(r.fileAt(ref))
	if err != nil {
		return fmt.Errorf("resolver named %s: %w", ref, err)
	}
	if !bytes.Equal(data, sent) {
		return fmt.Errorf("resolver named %s, which holds bytes other than the call's", ref)
	}
	return nil
}

func (r *Recorder) writeImages(pending []pendingImage) error {
	for _, image := range pending {
		abs := r.fileAt(image.rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return fmt.Errorf("systemone: %s: %w", image.rel, err)
		}
		if err := writeFileExclusive(abs, image.data); err != nil {
			return fmt.Errorf("systemone: %s: %w", image.rel, err)
		}
	}
	return nil
}

// publish puts the exchange on disk. The bytes go to a temporary file
// first and reach their name by an exclusive link, so a reader sees a
// whole recording or nothing, and an existing recording is never
// replaced by surprise.
func (r *Recorder) publish(exchange string, payload []byte) error {
	path := r.path(exchange)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("systemone: %s: %w", exchange, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), tempPattern)
	if err != nil {
		return fmt.Errorf("systemone: %s: %w", exchange, err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("systemone: %s: %w", exchange, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("systemone: %s: %w", exchange, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("systemone: %s: %w", exchange, err)
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return fmt.Errorf("systemone: %s: %w", exchange, err)
	}
	if r.overwrite {
		if err := os.Rename(name, path); err != nil {
			return fmt.Errorf("systemone: %s: %w", exchange, err)
		}
		return nil
	}
	if err := os.Link(name, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return &AlreadyRecorded{Exchange: exchange, Path: path}
		}
		return fmt.Errorf("systemone: %s: %w", exchange, err)
	}
	return nil
}

// claim holds one exchange name for the length of one call and refuses
// a recording that is already on disk before the inner client runs.
func (r *Recorder) claim(exchange string) (func(), error) {
	path := r.path(exchange)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inFlight[exchange] {
		return nil, fmt.Errorf("systemone: %s is being recorded by another call", exchange)
	}
	if !r.overwrite {
		if _, err := os.Stat(path); err == nil {
			return nil, &AlreadyRecorded{Exchange: exchange, Path: path}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("systemone: %s: %w", exchange, err)
		}
	}
	r.inFlight[exchange] = true
	return func() {
		r.mu.Lock()
		delete(r.inFlight, exchange)
		r.mu.Unlock()
	}, nil
}

func (r *Recorder) path(exchange string) string {
	return filepath.Join(r.dir, exchange+".json")
}

func (r *Recorder) fileAt(rel string) string {
	return filepath.Join(r.dir, filepath.FromSlash(rel))
}

// validExchange rules out a name that could not be one file in the
// recording directory. The id is the file name, so the character set is
// the one the corpus already uses: no slash, no space, no dot-dot, and
// no name that carries its own file suffix, which would be one id read
// two ways by everything that globs the corpus.
func validExchange(name string) error {
	if name == "." || name == ".." || strings.HasSuffix(name, ".json") || !validID(name) {
		return fmt.Errorf("systemone: exchange %q is not a usable recording name", name)
	}
	return nil
}

// clefEnvelope is the reply in the shape Cloudflare gave it and
// decodeResponse reads: the answers inside a result, inside a body.
func clefEnvelope(resp Response) (json.RawMessage, error) {
	return json.Marshal(resultEnvelope{
		Result:   resp,
		Success:  true,
		Errors:   []string{},
		Messages: []string{},
	})
}

// resultEnvelope is Cloudflare's success wrapper. A stored exchange is
// one the inner client could use, so success is true and the two note
// lists are empty; keeping all four fields is what makes a fresh
// recording read like the T0.6 ones.
type resultEnvelope struct {
	Result   Response `json:"result"`
	Success  bool     `json:"success"`
	Errors   []string `json:"errors"`
	Messages []string `json:"messages"`
}

// envelopeFile is the recording as Open loads it.
type envelopeFile struct {
	Exchange string         `json:"exchange"`
	Request  storedRequest  `json:"request"`
	Response storedResponse `json:"response"`
}

type storedRequest struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body"`
}

type storedResponse struct {
	Status    int             `json:"status"`
	ElapsedMS int64           `json:"elapsed_ms"`
	Body      json.RawMessage `json:"body"`
}

// recordedBody mirrors workersBody, the body the live client sends, with
// each image replaced by the file it references. Same fields, same
// omissions: a call with no image stores no images key, so the recording
// and the request hash alike.
type recordedBody struct {
	Model     string              `json:"model"`
	Images    []recordedImage     `json:"images,omitempty"`
	State     json.RawMessage     `json:"state"`
	Questions map[string]Question `json:"questions"`
}

type recordedImage struct {
	ContentType string `json:"content_type"`
	File        string `json:"file"`
}

type pendingImage struct {
	rel  string
	data []byte
}

// MatchImages indexes every file under root by its bytes and returns a
// resolver that names the match relative to the recording directory.
// It is how a re-record points at the sample photos that are already
// committed instead of writing their bytes a second time: testdata/clef
// is 0.6 MB because 15 calls shared one photo by reference.
//
// The index is built once, here, so a missing or unreadable root is
// reported before the first paid call. Two files with one digest are
// named by the lexicographically first path, which makes a re-run's
// references stable.
func MatchImages(root string) (ImageResolver, error) {
	if root == "" {
		return nil, errors.New("systemone: no image root to match")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("systemone: image root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("systemone: image root %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("systemone: image root %s is not a directory", root)
	}
	index, err := indexByContent(abs)
	if err != nil {
		return nil, fmt.Errorf("systemone: image root %s: %w", root, err)
	}
	return func(dir, contentType string, data []byte) (string, bool) {
		found, ok := index[digestName(data)]
		if !ok {
			return "", false
		}
		rel, err := filepath.Rel(dir, found)
		if err != nil {
			return "", false
		}
		return filepath.ToSlash(rel), true
	}, nil
}

// indexByContent digests every regular file under root, walking in name
// order so the first name for a digest is the one a re-run gets.
func indexByContent(root string) (map[string]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	index := make(map[string]string, len(files))
	for _, path := range files {
		name, ok := digestFile(path)
		if !ok {
			continue
		}
		if _, seen := index[name]; !seen {
			index[name] = path
		}
	}
	return index, nil
}

func digestFile(path string) (string, bool) {
	opened, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer opened.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, opened); err != nil {
		return "", false
	}
	return hex.EncodeToString(hash.Sum(nil)[:]), true
}

func digestName(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func imageExtension(contentType string) (string, error) {
	switch contentType {
	case "image/jpeg":
		return "jpg", nil
	case "image/png":
		return "png", nil
	case "image/webp":
		return "webp", nil
	default:
		return "", fmt.Errorf("content type %q is not a picture", contentType)
	}
}

// writeFileExclusive creates a file only when it is not there. An
// existing one is the same bytes under the same digest name, so it is
// left alone.
func writeFileExclusive(path string, data []byte) error {
	opened, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		return err
	}
	defer opened.Close()
	if _, err := opened.Write(data); err != nil {
		return err
	}
	if err := opened.Sync(); err != nil {
		return err
	}
	return opened.Chmod(0o644)
}

func marshalIndent(v any) ([]byte, error) {
	raw, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}
