package youcam

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Missing is a recording lookup that found nothing. Key is the request
// key that was asked for. A miss is never an empty slot or a success status.
type Missing struct {
	Key string
}

func (e *Missing) Error() string {
	return fmt.Sprintf("youcam: no recording for %q", e.Key)
}

// Ambiguous is one request key that matches more than one recording.
// The replay does not pick one.
type Ambiguous struct {
	Key        string
	Recordings []string
}

func (e *Ambiguous) Error() string {
	return fmt.Sprintf("youcam: request key %q matches %s", e.Key, strings.Join(e.Recordings, ", "))
}

// Recording is one stored exchange. Key is its exchange id.
type Recording struct {
	Key      string
	Request  json.RawMessage
	Response json.RawMessage
}

type stored struct {
	file     string
	key      string
	request  json.RawMessage
	response json.RawMessage
}

var _ YouCam = (*Replay)(nil)

// Replay serves recordings from a directory of JSON exchanges. A directory
// that is not there yet is an empty set: T0.5 has not recorded YouCam.
// A missing recording fails with its key. Replay does not dial.
type Replay struct {
	byExchange map[string]*stored
	byContent  map[string][]*stored
	byUpload   map[string][]*stored
	byCreate   map[string][]*stored
	byPoll     map[string][]*stored
	byFetch    map[string][]*stored
}

// Open reads every JSON exchange in dir. A missing directory has no
// recordings. A broken file, or two files that share an exchange id,
// fails the open. The exchange id is that recording's request key.
func Open(dir string) (*Replay, error) {
	replay := &Replay{
		byExchange: map[string]*stored{},
		byContent:  map[string][]*stored{},
		byUpload:   map[string][]*stored{},
		byCreate:   map[string][]*stored{},
		byPoll:     map[string][]*stored{},
		byFetch:    map[string][]*stored{},
	}
	if dir == "" {
		return nil, errors.New("youcam: recording directory is empty")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return replay, nil
		}
		return nil, fmt.Errorf("youcam: %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		if err := replay.load(dir, name); err != nil {
			return nil, err
		}
	}
	return replay, nil
}

func (r *Replay) load(dir, name string) error {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return fmt.Errorf("youcam: %s: %w", name, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var env struct {
		Exchange string          `json:"exchange"`
		Request  json.RawMessage `json:"request"`
		Response json.RawMessage `json:"response"`
	}
	if err := dec.Decode(&env); err != nil {
		return fmt.Errorf("youcam: %s: %w", name, err)
	}
	var trailing struct{}
	if err := dec.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("youcam: %s: trailing JSON", name)
	}
	if env.Exchange == "" || rawEmpty(env.Request) || rawEmpty(env.Response) {
		return fmt.Errorf("youcam: %s: exchange, request, and response are required", name)
	}
	if prev, ok := r.byExchange[env.Exchange]; ok {
		return fmt.Errorf("youcam: request key %q is in %s and %s", env.Exchange, prev.file, name)
	}
	rec := &stored{
		file:     name,
		key:      env.Exchange,
		request:  bytes.Clone(env.Request),
		response: bytes.Clone(env.Response),
	}
	r.byExchange[rec.key] = rec
	if err := r.index(rec); err != nil {
		return fmt.Errorf("youcam: %s: %w", name, err)
	}
	return nil
}

func (r *Replay) index(rec *stored) error {
	var httpReq struct {
		Method string          `json:"method"`
		Path   string          `json:"path"`
		Body   json.RawMessage `json:"body"`
	}
	if json.Unmarshal(rec.request, &httpReq) == nil && httpReq.Method != "" && httpReq.Path != "" {
		key, err := ContentKey(httpReq.Method, httpReq.Path, httpReq.Body)
		if err != nil {
			return err
		}
		r.byContent[key] = append(r.byContent[key], rec)
	}
	blobs := []json.RawMessage{rec.request}
	if json.Unmarshal(rec.request, &httpReq) == nil && !rawEmpty(httpReq.Body) {
		blobs = append(blobs, httpReq.Body)
	}
	for _, blob := range blobs {
		if file, ok := decodeAs[File](blob); ok && file.validate() == nil {
			key, err := requestKey(file)
			if err != nil {
				return err
			}
			appendOne(r.byUpload, key, rec)
		}
		if task, ok := decodeAs[Task](blob); ok && task.validate() == nil {
			key, err := task.key()
			if err != nil {
				return err
			}
			appendOne(r.byCreate, key, rec)
		}
		if ref, ok := decodeAs[PollRef](blob); ok && ref.validate() == nil {
			key, err := requestKey(ref)
			if err != nil {
				return err
			}
			appendOne(r.byPoll, key, rec)
		}
		if ref, ok := decodeAs[ResultRef](blob); ok && ref.validate() == nil {
			key, err := requestKey(ref)
			if err != nil {
				return err
			}
			appendOne(r.byFetch, key, rec)
		}
	}
	return nil
}

func decodeAs[T any](raw json.RawMessage) (T, bool) {
	var zero T
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var value T
	if err := dec.Decode(&value); err != nil {
		return zero, false
	}
	var trailing struct{}
	if err := dec.Decode(&trailing); err != io.EOF {
		return zero, false
	}
	return value, true
}

func appendOne(index map[string][]*stored, key string, rec *stored) {
	for _, existing := range index[key] {
		if existing == rec {
			return
		}
	}
	index[key] = append(index[key], rec)
}

// Reply returns the recording stored under the exchange id.
func (r *Replay) Reply(key string) (Recording, error) {
	rec, ok := r.byExchange[key]
	if !ok {
		return Recording{}, &Missing{Key: key}
	}
	return rec.export(), nil
}

// Serve returns the recording whose HTTP method, path, and body hash to
// the key. Headers are not part of the key. Two recordings with one
// request are an error.
func (r *Replay) Serve(method, path string, body json.RawMessage) (Recording, error) {
	key, err := ContentKey(method, path, body)
	if err != nil {
		return Recording{}, err
	}
	rec, err := one(r.byContent[key], key)
	if err != nil {
		return Recording{}, err
	}
	return rec.export(), nil
}

// Upload returns the recorded slot for file.
func (r *Replay) Upload(ctx context.Context, file File) (Slot, error) {
	if err := ctx.Err(); err != nil {
		return Slot{}, err
	}
	if err := file.validate(); err != nil {
		return Slot{}, err
	}
	key, err := requestKey(file)
	if err != nil {
		return Slot{}, err
	}
	rec, err := one(r.byUpload[key], key)
	if err != nil {
		return Slot{}, err
	}
	if provider, ok := providerError(rec.response); ok {
		return Slot{}, provider
	}
	slot, ok := decodeAs[Slot](rec.response)
	if !ok || slot.FileID == "" {
		return Slot{}, fmt.Errorf("youcam: recording %q is not an upload", key)
	}
	return slot, nil
}

// Create returns the recorded task.
func (r *Replay) Create(ctx context.Context, task Task) (Created, error) {
	if err := ctx.Err(); err != nil {
		return Created{}, err
	}
	if err := task.validate(); err != nil {
		return Created{}, err
	}
	key, err := task.key()
	if err != nil {
		return Created{}, err
	}
	rec, err := one(r.byCreate[key], key)
	if err != nil {
		return Created{}, err
	}
	if provider, ok := providerError(rec.response); ok {
		return Created{}, provider
	}
	created, ok := decodeAs[Created](rec.response)
	if !ok || created.TaskID == "" {
		return Created{}, fmt.Errorf("youcam: recording %q is not a created task", key)
	}
	return created, nil
}

// Poll returns the recorded status. A recorded refusal is the error.
func (r *Replay) Poll(ctx context.Context, ref PollRef) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	if err := ref.validate(); err != nil {
		return Status{}, err
	}
	key, err := requestKey(ref)
	if err != nil {
		return Status{}, err
	}
	rec, err := one(r.byPoll[key], key)
	if err != nil {
		return Status{}, err
	}
	if provider, ok := providerError(rec.response); ok {
		return Status{}, provider
	}
	status, ok := decodeAs[Status](rec.response)
	if !ok || status.TaskID == "" {
		return Status{}, fmt.Errorf("youcam: recording %q is not a poll", key)
	}
	if status.Error != nil {
		if !status.Error.Kind.Valid() {
			return Status{}, fmt.Errorf("youcam: recording %q has unknown error kind %q", key, status.Error.Kind)
		}
		return Status{}, status.Error
	}
	switch status.Status {
	case StatusRunning, StatusSuccess:
		return status, nil
	case StatusError:
		return Status{}, fmt.Errorf("youcam: recording %q has no error", key)
	default:
		return Status{}, fmt.Errorf("youcam: recording %q is not a poll", key)
	}
}

// Fetch returns the recorded result bytes. It does not dial the URL.
func (r *Replay) Fetch(ctx context.Context, ref ResultRef) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := ref.validate(); err != nil {
		return Result{}, err
	}
	key, err := requestKey(ref)
	if err != nil {
		return Result{}, err
	}
	rec, err := one(r.byFetch[key], key)
	if err != nil {
		return Result{}, err
	}
	if provider, ok := providerError(rec.response); ok {
		return Result{}, provider
	}
	result, ok := decodeAs[Result](rec.response)
	if !ok || result.TaskID == "" {
		return Result{}, fmt.Errorf("youcam: recording %q is not a result", key)
	}
	return result, nil
}

func (t Task) key() (string, error) {
	payload, err := canonicalJSON(t.Payload)
	if err != nil {
		return "", err
	}
	return requestKey(Task{Name: t.Name, Payload: payload})
}

func providerError(raw json.RawMessage) (error, bool) {
	errValue, ok := decodeAs[Error](raw)
	if !ok || errValue.Kind == "" {
		return nil, false
	}
	if !errValue.Kind.Valid() {
		return fmt.Errorf("youcam: unknown error kind %q", errValue.Kind), true
	}
	return &errValue, true
}

func one(recs []*stored, key string) (*stored, error) {
	switch len(recs) {
	case 0:
		return nil, &Missing{Key: key}
	case 1:
		return recs[0], nil
	default:
		names := make([]string, len(recs))
		for i, rec := range recs {
			names[i] = rec.key
		}
		sort.Strings(names)
		return nil, &Ambiguous{Key: key, Recordings: names}
	}
}

func (s *stored) export() Recording {
	return Recording{
		Key:      s.key,
		Request:  bytes.Clone(s.request),
		Response: bytes.Clone(s.response),
	}
}

// ContentKey is the stable hash of an HTTP call. Headers are not part of it.
func ContentKey(method, path string, body json.RawMessage) (string, error) {
	if rawEmpty(body) {
		body = json.RawMessage("null")
	}
	return requestKey(httpCall{Method: method, Path: path, Body: body})
}

type httpCall struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body"`
}

// requestKey hashes v as canonical JSON. Object keys are sorted, so two
// encodings of one request share a key. The digest is lowercase hex SHA-256.
func requestKey(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	canon, err := canonicalJSON(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalJSON(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("youcam: empty JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	var trailing struct{}
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, errors.New("youcam: trailing JSON")
	}
	return json.Marshal(value)
}

func rawEmpty(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || string(trimmed) == "null"
}
