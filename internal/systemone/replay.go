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
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Missing is a recording lookup that found nothing. Key is the request
// key that was asked for. A miss is never an empty response.
type Missing struct {
	Key string
}

func (e *Missing) Error() string {
	return fmt.Sprintf("systemone: no recording for %q", e.Key)
}

// Ambiguous is one request key that matches more than one recording.
// The replay does not pick one.
type Ambiguous struct {
	Key        string
	Recordings []string
}

func (e *Ambiguous) Error() string {
	return fmt.Sprintf("systemone: request key %q matches %s", e.Key, strings.Join(e.Recordings, ", "))
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

var _ SystemOne = (*Replay)(nil)

// Replay serves recordings from a directory of JSON exchanges. A directory
// that is not there yet is an empty set: T0.6 has not recorded Clef.
// Ask, Reply, and Serve fail with the key when the recording is missing.
// None of them dial.
type Replay struct {
	byExchange map[string]*stored
	byContent  map[string][]*stored
	byLogical  map[string][]*stored
}

// Open reads every JSON exchange in dir. A missing directory has no
// recordings. A file that does not parse, or two files that share an
// exchange id, fails the open. The exchange id is that recording's
// request key.
func Open(dir string) (*Replay, error) {
	replay := &Replay{
		byExchange: map[string]*stored{},
		byContent:  map[string][]*stored{},
		byLogical:  map[string][]*stored{},
	}
	if dir == "" {
		return nil, errors.New("systemone: recording directory is empty")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return replay, nil
		}
		return nil, fmt.Errorf("systemone: %s: %w", dir, err)
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
		return fmt.Errorf("systemone: %s: %w", name, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var env struct {
		Exchange string          `json:"exchange"`
		Request  json.RawMessage `json:"request"`
		Response json.RawMessage `json:"response"`
	}
	if err := dec.Decode(&env); err != nil {
		return fmt.Errorf("systemone: %s: %w", name, err)
	}
	var trailing struct{}
	if err := dec.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("systemone: %s: trailing JSON", name)
	}
	if env.Exchange == "" || rawEmpty(env.Request) || rawEmpty(env.Response) {
		return fmt.Errorf("systemone: %s: exchange, request, and response are required", name)
	}
	if prev, ok := r.byExchange[env.Exchange]; ok {
		return fmt.Errorf("systemone: request key %q is in %s and %s", env.Exchange, prev.file, name)
	}
	rec := &stored{
		file:     name,
		key:      env.Exchange,
		request:  append(json.RawMessage(nil), env.Request...),
		response: append(json.RawMessage(nil), env.Response...),
	}
	r.byExchange[rec.key] = rec
	if err := r.indexContent(rec); err != nil {
		return fmt.Errorf("systemone: %s: %w", name, err)
	}
	if err := r.indexLogical(rec); err != nil {
		return fmt.Errorf("systemone: %s: %w", name, err)
	}
	return nil
}

func (r *Replay) indexContent(rec *stored) error {
	var env struct {
		Method string          `json:"method"`
		Path   string          `json:"path"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(rec.request, &env); err != nil || env.Method == "" || env.Path == "" {
		return nil
	}
	key, err := ContentKey(env.Method, env.Path, env.Body)
	if err != nil {
		return err
	}
	r.byContent[key] = append(r.byContent[key], rec)
	return nil
}

func (r *Replay) indexLogical(rec *stored) error {
	add := func(req Request) error {
		if strings.TrimSpace(req.Model) == "" || len(req.Questions) == 0 {
			return nil
		}
		key, err := req.key()
		if err != nil {
			return err
		}
		appendOne(r.byLogical, key, rec)
		return nil
	}
	var direct Request
	if err := json.Unmarshal(rec.request, &direct); err == nil {
		if err := add(direct); err != nil {
			return err
		}
	}
	var httpReq struct {
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(rec.request, &httpReq); err != nil || rawEmpty(httpReq.Body) {
		return nil
	}
	var inner Request
	if err := json.Unmarshal(httpReq.Body, &inner); err != nil {
		return nil
	}
	return add(inner)
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
// key. The hash ignores headers, so a bearer token is not part of the key.
// Two recordings with one request are an error.
func (r *Replay) Serve(method, path string, body json.RawMessage) (Recording, error) {
	key, err := ContentKey(method, path, body)
	if err != nil {
		return Recording{}, err
	}
	return one(r.byContent[key], key)
}

// Ask returns the recorded response for req. The key covers the model,
// the state, the questions, and the image bytes. A Clef body inside an
// HTTP exchange matches the same key. A missing recording is Missing.
func (r *Replay) Ask(ctx context.Context, req Request) (Response, error) {
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
	rec, err := oneStored(r.byLogical[key], key)
	if err != nil {
		return Response{}, err
	}
	resp, ok := decodeResponse(rec.response)
	if !ok {
		return Response{}, fmt.Errorf("systemone: recording %q is not a system one response", key)
	}
	return resp, nil
}

func one(recs []*stored, key string) (Recording, error) {
	rec, err := oneStored(recs, key)
	if err != nil {
		return Recording{}, err
	}
	return rec.export(), nil
}

func oneStored(recs []*stored, key string) (*stored, error) {
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

func decodeResponse(raw json.RawMessage) (Response, bool) {
	candidates := []json.RawMessage{raw}
	var env struct {
		Result json.RawMessage `json:"result"`
		Body   json.RawMessage `json:"body"`
	}
	if json.Unmarshal(raw, &env) == nil {
		if !rawEmpty(env.Result) {
			candidates = append(candidates, env.Result)
		}
		if !rawEmpty(env.Body) {
			candidates = append(candidates, env.Body)
			var inner struct {
				Result json.RawMessage `json:"result"`
			}
			if json.Unmarshal(env.Body, &inner) == nil && !rawEmpty(inner.Result) {
				candidates = append(candidates, inner.Result)
			}
		}
	}
	for _, candidate := range candidates {
		var resp Response
		if json.Unmarshal(candidate, &resp) != nil {
			continue
		}
		if resp.Model == "" || len(resp.Answers) == 0 {
			continue
		}
		ok := true
		for _, answer := range resp.Answers {
			if !answer.complete() {
				ok = false
				break
			}
		}
		if ok {
			return resp, true
		}
	}
	return Response{}, false
}

// ContentKey is the stable hash of an HTTP call. Headers are not part
// of it. Body JSON is canonicalised, so key order does not change the key.
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

func (r Request) key() (string, error) {
	canon, err := r.canonical()
	if err != nil {
		return "", err
	}
	return requestKey(canon)
}

func (r Request) canonical() (Request, error) {
	state, err := canonicalJSON(r.State)
	if err != nil {
		return Request{}, err
	}
	questions := make(map[string]Question, len(r.Questions))
	for id, question := range r.Questions {
		criteria := bytes.TrimSpace(question.Criteria)
		if len(criteria) == 0 || string(criteria) == "null" {
			question.Criteria = nil
		} else {
			encoded, err := canonicalJSON(criteria)
			if err != nil {
				return Request{}, err
			}
			question.Criteria = encoded
		}
		questions[id] = question
	}
	var images []Image
	if len(r.Images) > 0 {
		images = make([]Image, len(r.Images))
		copy(images, r.Images)
	}
	return Request{
		Model:     strings.TrimSpace(r.Model),
		Images:    images,
		State:     state,
		Questions: questions,
	}, nil
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
		return nil, errors.New("systemone: empty JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	var trailing struct{}
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, errors.New("systemone: trailing JSON")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func rawEmpty(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || string(trimmed) == "null"
}
