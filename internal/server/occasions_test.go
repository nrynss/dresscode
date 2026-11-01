package server

// The occasions routes are the composition's: Compose mounts them
// between the look routes and the samples, so these tests read them
// through a composed server, member for member against the recorded
// examples in testdata/api. The ids the fixtures carry are the data's
// own slugs, so no replay renames anything here. Nothing dials and
// nothing spends: the four routes take no gate token, no lease seat and
// no unit.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheComposedServerAnswersTheOccasionRoutes boots the composition
// and reads the four occasions routes on it: the list and the one
// against their recorded examples, the opt-in pair round its own
// stored set, and the one refusal the contract names for an unknown id.
func TestTheComposedServerAnswersTheOccasionRoutes(t *testing.T) {
	env := newComposeEnv(t, earringAnswers())
	_, cookie := bootUser(t, env.handler)

	list := do(t, env.handler, http.MethodGet, "/api/occasions", cookie, nil, nil)
	compare(t, "occasions", list, nil)

	one := do(t, env.handler, http.MethodGet, "/api/occasions/family-function", cookie, nil, nil)
	compare(t, "occasion", one, nil)

	missing := do(t, env.handler, http.MethodGet, "/api/occasions/no-such-occasion", cookie, nil, nil)
	assertErr(t, missing, http.StatusNotFound, "not_found", "No such resource exists.")

	// A fresh account reads the empty list: no row means not ticked, and
	// the menu does not fill itself in.
	empty := do(t, env.handler, http.MethodGet, "/api/occasions/family-function/opt-in", cookie, nil, nil)
	compare(t, "opt-in", empty, nil)

	// The put replaces the set and answers the stored set: the fixture's
	// three family fixes.
	put := do(t, env.handler, http.MethodPut, "/api/occasions/family-function/opt-in", cookie,
		strings.NewReader(`{"ticked":["family-gold-jhumkas","family-red-lip","family-festive-top"]}`), headerJSON)
	compare(t, "opt-in-put", put, nil)

	// What the put stored is what the get reads back, the rows a look
	// start reads as its opted-in menu.
	stored := do(t, env.handler, http.MethodGet, "/api/occasions/family-function/opt-in", cookie, nil, nil)
	compare(t, "opt-in-put", stored, nil)

	// An empty list is valid: the set goes away.
	cleared := do(t, env.handler, http.MethodPut, "/api/occasions/family-function/opt-in", cookie,
		strings.NewReader(`{"ticked":[]}`), headerJSON)
	compare(t, "opt-in", cleared, nil)
	after := do(t, env.handler, http.MethodGet, "/api/occasions/family-function/opt-in", cookie, nil, nil)
	if !strings.Contains(after.Body.String(), `"ticked":[]`) {
		t.Fatalf("after the empty put the get reads %s", after.Body.String())
	}

	// The composition mounts the occasions exactly once.
	if err := env.srv.MountOccasions(); err == nil {
		t.Fatal("the occasions routes mounted twice")
	}
}

// TestTheOptInRouteRefusesWhatTheContractRefuses pins the put's
// validation: an id that is no fix at all and a fix of another occasion
// are both invalid_request naming the field, a refusal changes nothing,
// the ticked member is required and must be a list, and an unknown
// occasion is not_found on the whole family of routes.
func TestTheOptInRouteRefusesWhatTheContractRefuses(t *testing.T) {
	env := newComposeEnv(t, earringAnswers())
	_, cookie := bootUser(t, env.handler)
	const path = "/api/occasions/family-function/opt-in"

	bogus := do(t, env.handler, http.MethodPut, path, cookie,
		strings.NewReader(`{"ticked":["no-such-fix"]}`), headerJSON)
	assertErr(t, bogus, http.StatusBadRequest, "invalid_request", msgInvalid)
	detailField(t, bogus, "ticked")

	// The date collar is a real fix, of another occasion: the menu is
	// per occasion, so it refuses here too.
	other := do(t, env.handler, http.MethodPut, path, cookie,
		strings.NewReader(`{"ticked":["date-open-collar"]}`), headerJSON)
	assertErr(t, other, http.StatusBadRequest, "invalid_request", msgInvalid)
	detailField(t, other, "ticked")

	// A refusal writes nothing: the set is still the empty one.
	after := do(t, env.handler, http.MethodGet, path, cookie, nil, nil)
	compare(t, "opt-in", after, nil)

	member := do(t, env.handler, http.MethodPut, path, cookie,
		strings.NewReader(`{}`), headerJSON)
	assertErr(t, member, http.StatusBadRequest, "invalid_request", msgInvalid)
	detailField(t, member, "ticked")

	notList := do(t, env.handler, http.MethodPut, path, cookie,
		strings.NewReader(`{"ticked":"family-gold-jhumkas"}`), headerJSON)
	assertErr(t, notList, http.StatusBadRequest, "invalid_request", msgInvalid)
	detailField(t, notList, "ticked")

	nullList := do(t, env.handler, http.MethodPut, path, cookie,
		strings.NewReader(`{"ticked":null}`), headerJSON)
	assertErr(t, nullList, http.StatusBadRequest, "invalid_request", msgInvalid)
	detailField(t, nullList, "ticked")

	unknownPut := do(t, env.handler, http.MethodPut, "/api/occasions/no-such-occasion/opt-in", cookie,
		strings.NewReader(`{"ticked":[]}`), headerJSON)
	assertErr(t, unknownPut, http.StatusNotFound, "not_found", msgNotFound)
	unknownGet := do(t, env.handler, http.MethodGet, "/api/occasions/no-such-occasion/opt-in", cookie, nil, nil)
	assertErr(t, unknownGet, http.StatusNotFound, "not_found", msgNotFound)
}

// TestOccasionsNeedTheLookRoutes pins the mount's one requirement: the
// handlers read the look runtime's loaded occasions and its opt-in read,
// so without the look routes the mount refuses.
func TestOccasionsNeedTheLookRoutes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	app := filepath.Join(dir, "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "index.html"), []byte(appIndex), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := Open(t.Context(), Config{
		DBPath:     filepath.Join(dir, "dresscode.db"),
		AppDir:     app,
		SigningKey: []byte("test-session-signing-key-32b!!!!"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Close() }()
	if err := srv.MountOccasions(); err == nil {
		t.Fatal("occasions mounted without the look routes")
	}
}

// detailField reads one error envelope's detail.field.
func detailField(t *testing.T, rec *httptest.ResponseRecorder, field string) {
	t.Helper()
	var body struct {
		Error struct {
			Detail struct {
				Field string `json:"field"`
			} `json:"detail"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Detail.Field != field {
		t.Fatalf("detail field %q, want %q (%s)", body.Error.Detail.Field, field, rec.Body.String())
	}
}
