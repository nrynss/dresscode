package memory

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
// key that was asked for. A miss is never an empty history.
type Missing struct {
	Key string
}

func (e *Missing) Error() string {
	return fmt.Sprintf("memory: no recording for %q", e.Key)
}

// Ambiguous is one request key that matches more than one recording.
// The fake does not pick one. The repeated erase is this case: the same
// HTTP request was recorded twice, with different replies.
type Ambiguous struct {
	Key        string
	Recordings []string
}

func (e *Ambiguous) Error() string {
	return fmt.Sprintf("memory: request key %q matches %s", e.Key, strings.Join(e.Recordings, ", "))
}

// Recording is one stored exchange. Key is its exchange id, the request
// key that names that recording and no other.
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
	session  string
	erase    bool
	tool     string
	query    string
	confirm  string
	concepts []Concept
}

// Fake serves recorded Lambo exchanges. Reply uses the exchange id.
// Serve uses a hash of the HTTP method, path, and body. Remember, Recall,
// and Erase serve the exchange whose call matches, and fail when none
// does or when more than one does. The fake does not dial and does not
// invent a hit list.
type Fake struct {
	byExchange map[string]*stored
	byContent  map[string][]*stored
	order      []*stored
}

var _ Memory = (*Fake)(nil)

// Open reads every JSON exchange in dir. A missing directory has no
// recordings. A broken file, or two files that share an exchange id,
// fails the open.
func Open(dir string) (*Fake, error) {
	fake := &Fake{
		byExchange: map[string]*stored{},
		byContent:  map[string][]*stored{},
	}
	if dir == "" {
		return nil, errors.New("memory: recording directory is empty")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fake, nil
		}
		return nil, fmt.Errorf("memory: %s: %w", dir, err)
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
		if err := fake.load(dir, name); err != nil {
			return nil, err
		}
	}
	return fake, nil
}

func (f *Fake) load(dir, name string) error {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return fmt.Errorf("memory: %s: %w", name, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var env struct {
		Exchange string          `json:"exchange"`
		Request  json.RawMessage `json:"request"`
		Response json.RawMessage `json:"response"`
	}
	if err := dec.Decode(&env); err != nil {
		return fmt.Errorf("memory: %s: %w", name, err)
	}
	var trailing struct{}
	if err := dec.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("memory: %s: trailing JSON", name)
	}
	if env.Exchange == "" || rawEmpty(env.Request) || rawEmpty(env.Response) {
		return fmt.Errorf("memory: %s: exchange, request, and response are required", name)
	}
	if prev, ok := f.byExchange[env.Exchange]; ok {
		return fmt.Errorf("memory: request key %q is in %s and %s", env.Exchange, prev.file, name)
	}
	rec := &stored{
		file:     name,
		key:      env.Exchange,
		request:  bytes.Clone(env.Request),
		response: bytes.Clone(env.Response),
	}
	rec.noteCall()
	f.byExchange[rec.key] = rec
	f.order = append(f.order, rec)
	if err := f.indexContent(rec); err != nil {
		return fmt.Errorf("memory: %s: %w", name, err)
	}
	return nil
}

func (s *stored) noteCall() {
	var env struct {
		Method string          `json:"method"`
		Path   string          `json:"path"`
		Body   json.RawMessage `json:"body"`
	}
	if json.Unmarshal(s.request, &env) != nil {
		return
	}
	s.session, s.erase = sessionFromPath(env.Path)
	if rawEmpty(env.Body) {
		return
	}
	var body struct {
		Method  string `json:"method"`
		Confirm string `json:"confirm"`
		Params  struct {
			Name      string `json:"name"`
			Arguments struct {
				Concepts []Concept `json:"concepts"`
				Query    string    `json:"query"`
			} `json:"arguments"`
		} `json:"params"`
	}
	if json.Unmarshal(env.Body, &body) != nil {
		return
	}
	s.confirm = body.Confirm
	if body.Method == "tools/call" {
		s.tool = body.Params.Name
		s.query = body.Params.Arguments.Query
		s.concepts = body.Params.Arguments.Concepts
	}
}

func sessionFromPath(path string) (string, bool) {
	if rest, ok := strings.CutPrefix(path, "/mcp/s/"); ok && rest != "" && !strings.Contains(rest, "/") {
		return rest, false
	}
	if rest, ok := strings.CutPrefix(path, "/admin/s/"); ok {
		id, ok := strings.CutSuffix(rest, "/erase")
		if ok && ValidSession(id) {
			return id, true
		}
	}
	return "", false
}

func (f *Fake) indexContent(rec *stored) error {
	var env struct {
		Method string          `json:"method"`
		Path   string          `json:"path"`
		Body   json.RawMessage `json:"body"`
	}
	if json.Unmarshal(rec.request, &env) != nil || env.Method == "" || env.Path == "" {
		return nil
	}
	key, err := ContentKey(env.Method, env.Path, env.Body)
	if err != nil {
		return err
	}
	f.byContent[key] = append(f.byContent[key], rec)
	return nil
}

// Reply returns the recording stored under the exchange id.
func (f *Fake) Reply(key string) (Recording, error) {
	rec, ok := f.byExchange[key]
	if !ok {
		return Recording{}, &Missing{Key: key}
	}
	return rec.export(), nil
}

// Serve returns the recording whose HTTP method, path, and body hash to
// the key. Headers are not part of the key: the bearer is a secret, and
// the fixture redacts it. Two recordings with one request are an error.
func (f *Fake) Serve(method, path string, body json.RawMessage) (Recording, error) {
	key, err := ContentKey(method, path, body)
	if err != nil {
		return Recording{}, err
	}
	rec, err := one(f.byContent[key], key)
	if err != nil {
		return Recording{}, err
	}
	return rec.export(), nil
}

// Remember returns the recorded derive for this session and these concepts,
// in this order. Any other list is a miss.
func (f *Fake) Remember(ctx context.Context, session string, concepts []Concept) (Remembered, error) {
	if err := ctx.Err(); err != nil {
		return Remembered{}, err
	}
	if err := checkSession(session); err != nil {
		return Remembered{}, err
	}
	if err := validateConcepts(concepts); err != nil {
		return Remembered{}, err
	}
	var matched []*stored
	for _, rec := range f.order {
		if rec.tool == "lambo_derive" && rec.session == session && sameConcepts(rec.concepts, concepts) {
			matched = append(matched, rec)
		}
	}
	key, err := requestKey(rememberKey{Op: "remember", Session: session, Concepts: concepts})
	if err != nil {
		return Remembered{}, err
	}
	rec, err := one(matched, key)
	if err != nil {
		return Remembered{}, err
	}
	return decodeRemembered(rec)
}

// Recall returns the recorded recall for this session and query text.
// Two recordings of the same query are an error, not a chosen hit list.
// An erased session is ErrErased, not an empty hit list.
func (f *Fake) Recall(ctx context.Context, session, occasion string) (Recall, error) {
	if err := ctx.Err(); err != nil {
		return Recall{}, err
	}
	if err := checkSession(session); err != nil {
		return Recall{}, err
	}
	if occasion == "" {
		return Recall{}, errors.New("memory: occasion is empty")
	}
	var matched []*stored
	for _, rec := range f.order {
		if rec.tool == "lambo_recall" && rec.session == session && rec.query == occasion {
			matched = append(matched, rec)
		}
	}
	key, err := requestKey(recallKey{Op: "recall", Session: session, Occasion: occasion})
	if err != nil {
		return Recall{}, err
	}
	rec, err := one(matched, key)
	if err != nil {
		return Recall{}, err
	}
	return decodeRecall(rec)
}

// Erase returns the recorded erase for this session. A repeated erase is
// two recordings of one request, so Erase reports that instead of choosing.
func (f *Fake) Erase(ctx context.Context, session string) (EraseResult, error) {
	if err := ctx.Err(); err != nil {
		return EraseResult{}, err
	}
	if err := checkSession(session); err != nil {
		return EraseResult{}, err
	}
	var matched []*stored
	for _, rec := range f.order {
		if rec.erase && rec.session == session && rec.confirm == session {
			matched = append(matched, rec)
		}
	}
	key, err := requestKey(eraseKey{Op: "erase", Session: session})
	if err != nil {
		return EraseResult{}, err
	}
	rec, err := one(matched, key)
	if err != nil {
		return EraseResult{}, err
	}
	return decodeErase(rec)
}

type rememberKey struct {
	Op       string    `json:"op"`
	Session  string    `json:"session"`
	Concepts []Concept `json:"concepts"`
}

type recallKey struct {
	Op       string `json:"op"`
	Session  string `json:"session"`
	Occasion string `json:"occasion"`
}

type eraseKey struct {
	Op      string `json:"op"`
	Session string `json:"session"`
}

func sameConcepts(a, b []Concept) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func decodeRemembered(rec *stored) (Remembered, error) {
	msg, err := message(rec.response)
	if err != nil {
		return Remembered{}, fmt.Errorf("memory: recording %q: %w", rec.key, err)
	}
	sc, err := structured(msg)
	if err != nil {
		return Remembered{}, fmt.Errorf("memory: recording %q: %w", rec.key, err)
	}
	var body struct {
		AgentID      string   `json:"agent_id"`
		Receipt      string   `json:"receipt"`
		ReceiptState string   `json:"receipt_state"`
		Summary      string   `json:"summary"`
		Warnings     []string `json:"warnings"`
		Created      []string `json:"created"`
		CreatedCount int      `json:"created_count"`
		Embedded     int      `json:"embedded"`
	}
	if err := json.Unmarshal(sc, &body); err != nil || body.Receipt == "" || body.ReceiptState == "" {
		return Remembered{}, fmt.Errorf("memory: recording %q is not a derive", rec.key)
	}
	return Remembered{
		Receipt:      body.Receipt,
		State:        body.ReceiptState,
		Summary:      body.Summary,
		AgentID:      body.AgentID,
		Warnings:     body.Warnings,
		Created:      body.Created,
		CreatedCount: body.CreatedCount,
		Embedded:     body.Embedded,
	}, nil
}

func decodeRecall(rec *stored) (Recall, error) {
	msg, err := message(rec.response)
	if err != nil {
		return Recall{}, fmt.Errorf("memory: recording %q: %w", rec.key, err)
	}
	if code, ok := rpcCode(msg); ok {
		if code == ErasedCode {
			return Recall{}, ErrErased
		}
		return Recall{}, fmt.Errorf("memory: recording %q: json-rpc error %d", rec.key, code)
	}
	sc, err := structured(msg)
	if err != nil {
		return Recall{}, fmt.Errorf("memory: recording %q: %w", rec.key, err)
	}
	var body struct {
		Context  string          `json:"context"`
		Hits     json.RawMessage `json:"hits"`
		Warnings []string        `json:"warnings"`
	}
	if err := json.Unmarshal(sc, &body); err != nil || len(bytes.TrimSpace(body.Hits)) == 0 {
		return Recall{}, fmt.Errorf("memory: recording %q is not a recall", rec.key)
	}
	var hits []Hit
	if err := json.Unmarshal(body.Hits, &hits); err != nil {
		return Recall{}, fmt.Errorf("memory: recording %q: %w", rec.key, err)
	}
	if hits == nil {
		hits = []Hit{}
	}
	warnings := body.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	return Recall{Context: body.Context, Hits: hits, Warnings: warnings}, nil
}

func decodeErase(rec *stored) (EraseResult, error) {
	msg, err := message(rec.response)
	if err != nil {
		return EraseResult{}, fmt.Errorf("memory: recording %q: %w", rec.key, err)
	}
	var result EraseResult
	if err := json.Unmarshal(msg, &result); err != nil || result.Session == "" {
		return EraseResult{}, fmt.Errorf("memory: recording %q is not an erase", rec.key)
	}
	return result, nil
}

func message(response json.RawMessage) (json.RawMessage, error) {
	var env struct {
		Body json.RawMessage `json:"body"`
		SSE  []struct {
			Data json.RawMessage `json:"data"`
		} `json:"sse"`
	}
	if err := json.Unmarshal(response, &env); err != nil {
		return nil, err
	}
	if len(env.SSE) > 0 {
		var last json.RawMessage
		for _, event := range env.SSE {
			if rawEmpty(event.Data) {
				continue
			}
			last = event.Data
		}
		if last == nil {
			return nil, errors.New("no json-rpc message")
		}
		return last, nil
	}
	if !rawEmpty(env.Body) {
		return env.Body, nil
	}
	return nil, errors.New("no json body")
}

func structured(msg json.RawMessage) (json.RawMessage, error) {
	var rpc struct {
		Result *struct {
			IsError           bool            `json:"isError"`
			StructuredContent json.RawMessage `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(msg, &rpc); err != nil {
		return nil, err
	}
	if rpc.Result == nil || rpc.Result.IsError || rawEmpty(rpc.Result.StructuredContent) {
		return nil, errors.New("no structured content")
	}
	return rpc.Result.StructuredContent, nil
}

func rpcCode(msg json.RawMessage) (int, bool) {
	var rpc struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(msg, &rpc) != nil || rpc.Error == nil {
		return 0, false
	}
	return rpc.Error.Code, true
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
		return nil, errors.New("memory: empty JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	var trailing struct{}
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, errors.New("memory: trailing JSON")
	}
	return json.Marshal(value)
}

func rawEmpty(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || string(trimmed) == "null"
}
