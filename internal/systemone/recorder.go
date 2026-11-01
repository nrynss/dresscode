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

// imageTempPattern names a half-written image file. It is not a .json,
// and it cannot be a digest name, which is a bare hex string with a
// picture suffix, so the temporary file is never the file a reference
// reads.
const imageTempPattern = ".image-*.tmp"

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
// with the ones the call carries before the inner client runs and before
// it stores the reference. A reference to other bytes would be a
// recording of a request that was never sent, and the replay would key it
// as one that was.
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
// paid for, in this order: a context already cancelled, a name that
// cannot be a file, a request the model would refuse, a name another call
// is holding, a recording already on disk, and every image decision. The
// image decisions are the reference a resolver names, which must exist
// and must hold the bytes that went out, and the digest file a written
// image would land in, which must be either absent or exactly those
// bytes. What only a reply can decide waits for the call and is refused
// before a byte of it is written: an inner error stores nothing, a reply
// the replay could not serve is refused on the reader's own bytes, and so
// is a reply that answers a question the request never asked.
//
// Safe for concurrent use. Two calls naming one exchange are refused,
// and a recording and its images are each published by an exclusive
// link, so neither is ever read as a half-written one. An image another
// call wrote first is verified, not trusted: a file holding other bytes,
// or no bytes at all, is a refusal rather than a success. The sequential
// path is unaffected: one call at a time writes each digest once and
// reuses it thereafter.
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

	// Every image decision is made here, before the call: which reference
	// names which bytes, which digest file has to exist, and which file
	// will be written. A resolver that cannot be honoured is a refusal
	// that costs nothing, because the money has not moved yet.
	images, err := r.settleImages(req)
	if err != nil {
		return Response{}, err
	}

	start := time.Now()
	resp, err := r.inner.Ask(ctx, req)
	if err != nil {
		// A failed call stores nothing. An error-shaped recording in a
		// corpus is worse than a missing one: the next Open either fails
		// on it or serves a broken reply to a request that was never
		// answered.
		return Response{}, err
	}
	// A reply that answers a question this request never asked is corpus
	// poison of a quiet kind, and it is refused before a byte of it lands.
	if err := checkAnswers(exchange, req, resp); err != nil {
		return Response{}, err
	}
	payload, err := r.envelope(exchange, req, resp, time.Since(start), images.refs)
	if err != nil {
		return Response{}, err
	}
	if err := r.writeImages(images.pending); err != nil {
		return Response{}, err
	}
	if err := r.publish(exchange, payload); err != nil {
		return Response{}, err
	}
	return resp, nil
}

// imagePlan is the recording's whole image decision, settled before the
// inner client runs: the references the stored request carries, and the
// files that have to be written with it.
type imagePlan struct {
	refs    []recordedImage
	pending []pendingImage
}

// envelope builds the file's bytes around the references settleImages
// chose. Nothing is written here: a request the recorder cannot encode
// leaves the directory as it was.
func (r *Recorder) envelope(exchange string, req Request, resp Response, elapsed time.Duration, refs []recordedImage) ([]byte, error) {
	requestBody, err := json.Marshal(recordedBody{
		Model:     req.Model,
		Images:    refs,
		State:     req.State,
		Questions: req.Questions,
	})
	if err != nil {
		return nil, fmt.Errorf("systemone: %s: request: %w", exchange, err)
	}
	responseBody, err := clefEnvelope(resp)
	if err != nil {
		return nil, fmt.Errorf("systemone: %s: response: %w", exchange, err)
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
		return nil, fmt.Errorf("systemone: %s: response: %w", exchange, err)
	}
	if _, ok := decodeResponse(probe); !ok {
		return nil, fmt.Errorf("systemone: %s: the reply is not one the replay can serve", exchange)
	}
	payload, err := marshalIndent(envelopeFile{
		Exchange: exchange,
		Request:  storedRequest{Method: "POST", Path: runPath(req.Model), Body: requestBody},
		Response: response,
	})
	if err != nil {
		return nil, fmt.Errorf("systemone: %s: %w", exchange, err)
	}
	return payload, nil
}

// settleImages turns every image into a reference and works out what
// still has to be written. It reads the request only, never a reply, so
// the recorder runs it before the inner client and spends nothing on a
// picture it cannot store. A resolver's answer is checked against the
// bytes that were sent; anything else goes into the recording directory
// under its own digest, which is why the same photo in a second exchange
// never appears there twice.
func (r *Recorder) settleImages(req Request) (imagePlan, error) {
	if len(req.Images) == 0 {
		return imagePlan{}, nil
	}
	var plan imagePlan
	for i, image := range req.Images {
		ext, err := imageExtension(image.ContentType)
		if err != nil {
			return imagePlan{}, fmt.Errorf("systemone: image %d: %w", i, err)
		}
		if r.images != nil {
			if ref, ok := r.images(r.dir, image.ContentType, image.Bytes); ok {
				if err := r.checkRef(ref, image.Bytes); err != nil {
					return imagePlan{}, fmt.Errorf("systemone: image %d: %w", i, err)
				}
				plan.refs = append(plan.refs, recordedImage{ContentType: image.ContentType, File: ref})
				continue
			}
		}
		rel := imageDir + "/" + digestName(image.Bytes) + "." + ext
		abs := r.fileAt(rel)
		data, err := os.ReadFile(abs)
		switch {
		case err == nil:
			if !bytes.Equal(data, image.Bytes) {
				return imagePlan{}, fmt.Errorf("systemone: image %d: %s holds other bytes", i, rel)
			}
		case errors.Is(err, os.ErrNotExist):
			plan.pending = append(plan.pending, pendingImage{rel: rel, data: image.Bytes})
		default:
			return imagePlan{}, fmt.Errorf("systemone: image %d: %s: %w", i, rel, err)
		}
		plan.refs = append(plan.refs, recordedImage{ContentType: image.ContentType, File: rel})
	}
	return plan, nil
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

// checkAnswers refuses a reply the request did not ask for. Two things
// are refused: an answer keyed to an id the request never sent, and an
// answer that picks a choice outside the options that id carried. The
// decode probe does not catch either, because both replay cleanly — a
// corpus full of answers nobody asked scores silently wrong downstream,
// which is the quieter poison.
//
// The committed corpus passes it, and the test that pins that runs the
// check over every recording: 41 top-level, 10 in stability/ and 4 in
// cost/, every answer id asked and every choice one of its own question's
// criteria. A reply's declared answer types are not judged here beyond
// the choice; decodeResponse reads those, on the reader's own bytes.
func checkAnswers(exchange string, req Request, resp Response) error {
	for id := range resp.Answers {
		if _, asked := req.Questions[id]; !asked {
			return fmt.Errorf("systemone: %s: the reply answers %q, which the request did not ask", exchange, id)
		}
	}
	for id, answer := range resp.Answers {
		if answer.Type != TypeChoice && answer.Choice == "" {
			continue
		}
		question := req.Questions[id]
		if question.Type != TypeChoice {
			return fmt.Errorf("systemone: %s: %q answers %q, but the request did not ask it as a choice",
				exchange, id, answer.Choice)
		}
		if !criteriaOption(question.Criteria, answer.Choice) {
			return fmt.Errorf("systemone: %s: %q answers %q, which is not one of the options the request sent",
				exchange, id, answer.Choice)
		}
	}
	return nil
}

// criteriaOption reports whether option names one of a choice question's
// criteria. A criteria that is not an object, which is a score question's
// array or a noul's absence, names none.
func criteriaOption(criteria json.RawMessage, option string) bool {
	var options map[string]json.RawMessage
	if err := json.Unmarshal(criteria, &options); err != nil {
		return false
	}
	_, ok := options[option]
	return ok
}

func (r *Recorder) writeImages(pending []pendingImage) error {
	for _, image := range pending {
		abs := r.fileAt(image.rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return fmt.Errorf("systemone: %s: %w", image.rel, err)
		}
		if err := publishImage(abs, image.data); err != nil {
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
//
// A paid driver owes this function two decisions, both about what the
// index is built over, and neither belongs mid-run.
//
// Point it at the samples tree only, never at the old testdata/clef. The
// task that re-records replaces those files, so a fresh recording that
// references them points at bytes its own run is about to rewrite, and
// the corpus it is meant to replace stays load-bearing under the new one.
//
// Decide what the resized cost photos do before the first call, too. The
// intent here is to write them into the tree the index is built over
// first, so every reference in the corpus is a name a person reads. A
// photo the index does not know falls through instead to
// images/<sha256>.<ext> under the recording, which the reader handles as
// well as any reference but which nobody would recognise in a listing. Say
// which of the two the run means: both are correct output, and a run that
// only finds out afterwards leaves two shapes in one corpus.
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

// publishImage puts one picture on disk the way an exchange gets there:
// the bytes go to a temporary file, are fsynced, and reach their name by
// an exclusive link, so a reader sees a whole image or nothing.
//
// A name already taken is verified, never trusted. Two calls that share
// an image neither of them has seen both plan to write it, and one of
// them loses the link; the loser checks the bytes that arrived. An empty
// file or a different picture is a refusal, because replay.Open fails an
// entire directory on one empty image and a reference to other bytes
// records a request that was never sent.
func publishImage(path string, data []byte) error {
	if err := verifyImage(path, data); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), imageTempPattern)
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return err
	}
	if err := os.Link(name, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return verifyImage(path, data)
		}
		return err
	}
	return nil
}

// verifyImage reads an image already sitting at path and insists it holds
// exactly the bytes this recording would have written. An empty one is
// refused on its own terms: the reader fails the whole directory on it.
func verifyImage(path string, want []byte) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("the file there is empty, and the replay refuses an empty image")
	}
	if !bytes.Equal(data, want) {
		return errors.New("the file there holds other bytes")
	}
	return nil
}

func marshalIndent(v any) ([]byte, error) {
	raw, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}
