// Occasions is this file: the dress-code reads and the opt-in pair,
// the routes a visitor answers before any look starts. The list and the
// one read come off the look runtime's loaded occasions, the same data
// the judge and the fix menu run on, so the menu the phone shows is the
// menu the pipeline scores against. The opt-in pair is the account's own
// rows in the store's opt_ins table, the rows a look start reads as its
// opted-in menu.
//
// The four routes are contract reads, not spends: no gate token, no
// lease seat, no budget reservation. A mount without the look routes
// refuses, because both halves read the runtime the look mount builds.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/keel/wire"
)

// The occasions routes and the opt-in pair.
const (
	routeOccasionList = "GET /api/occasions"
	routeOccasionOne  = "GET /api/occasions/{id}"
	routeOptInRead    = "GET /api/occasions/{id}/opt-in"
	routeOptInWrite   = "PUT /api/occasions/{id}/opt-in"
)

var (
	occasionsMu sync.Mutex
	occasionsBy = map[*Server]*lookRuntime{}
)

// MountOccasions attaches the occasion list, the dress-code read of one
// occasion, and the opt-in pair. Call it after MountLooks: the handlers
// read the look runtime's loaded occasions, and the opt-in rows a look
// start reads are the rows the put writes, so the look mount is a
// requirement, not an order of convenience. A second call refuses.
func (s *Server) MountOccasions() error {
	if s == nil || s.db == nil || s.mux == nil {
		return fmt.Errorf("server: occasions: nil server")
	}
	occasionsMu.Lock()
	defer occasionsMu.Unlock()
	if _, ok := occasionsBy[s]; ok {
		return fmt.Errorf("server: occasions: already mounted")
	}
	rt := s.looksOf()
	if rt == nil {
		return fmt.Errorf("server: occasions: the look routes are not mounted")
	}
	s.mux.Handle(routeOccasionList, http.HandlerFunc(rt.serveOccasions))
	s.mux.Handle(routeOccasionOne, http.HandlerFunc(rt.serveOccasion))
	s.mux.Handle(routeOptInRead, http.HandlerFunc(rt.serveOptInGet))
	s.mux.Handle(routeOptInWrite, http.HandlerFunc(rt.serveOptInPut))
	occasionsBy[s] = rt
	return nil
}

// ---------------------------------------------------------------------------
// The bodies

// occasionItem is one list entry: the id and the display name, nothing
// else, because the menu shows a name and the rest rides the one read.
type occasionItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type occasionListBody struct {
	Occasions []occasionItem `json:"occasions"`
}

// occasionCriterion is one question the judge asks, as the phone words
// it. The Clef wording, the thresholds and the kind's vocabulary are
// not on this response.
type occasionCriterion struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Suggestion string `json:"suggestion"`
}

// occasionFix is one menu entry: what it changes and the criteria it
// addresses. The YouCam task, the analysis and the vocabulary are not
// on this response.
type occasionFix struct {
	ID       string   `json:"id"`
	Changes  string   `json:"changes"`
	Criteria []string `json:"criteria"`
}

type occasionBody struct {
	ID       string              `json:"id"`
	Name     string              `json:"name"`
	Criteria []occasionCriterion `json:"criteria"`
	Fixes    []occasionFix       `json:"fixes"`
}

// optInBody is the account's ticked fixes for one occasion. Both routes
// answer it; the put's request carries the ticked list alone.
type optInBody struct {
	Occasion string   `json:"occasion"`
	Ticked   []string `json:"ticked"`
}

// ---------------------------------------------------------------------------
// The reads

// occasionOrder is the product order the list answers in, the order the
// contract names (docs/api.md, Occasions) and the recorded example
// carries. domain.Load sorts its occasions by id, which is a different
// order - festival before interview - so the list is ordered here, by
// the contract. An occasion the order does not name rides at the end,
// in loaded order, so new data still answers.
var occasionOrder = map[string]int{
	"date":            0,
	"family-function": 1,
	"interview":       2,
	"festival":        3,
	"funeral":         4,
}

// serveOccasions answers the loaded occasions, in the contract's product
// order: the order the menu shows.
func (rt *lookRuntime) serveOccasions(w http.ResponseWriter, r *http.Request) {
	listed := make([]*domain.Occasion, 0, len(rt.loaded.Occasions))
	for i := range rt.loaded.Occasions {
		listed = append(listed, &rt.loaded.Occasions[i])
	}
	sort.SliceStable(listed, func(i, j int) bool {
		pi, oki := occasionOrder[listed[i].ID]
		pj, okj := occasionOrder[listed[j].ID]
		if oki != okj {
			return oki
		}
		return oki && pi < pj
	})
	out := occasionListBody{Occasions: make([]occasionItem, 0, len(listed))}
	for _, occ := range listed {
		out.Occasions = append(out.Occasions, occasionItem{ID: occ.ID, Name: occ.Name})
	}
	if err := writeJSON(w, http.StatusOK, out); err != nil {
		rt.log.Warn("server: occasions: list", "err", err.Error())
	}
}

// serveOccasion answers the dress code and the fix menu of one
// occasion. Every list is present even when empty: an occasion with no
// fixes answers an empty menu, not a missing one.
func (rt *lookRuntime) serveOccasion(w http.ResponseWriter, r *http.Request) {
	occ, ok := rt.occasionOf(w, r)
	if !ok {
		return
	}
	body := occasionBody{
		ID:       occ.ID,
		Name:     occ.Name,
		Criteria: make([]occasionCriterion, 0, len(occ.Criteria)),
		Fixes:    make([]occasionFix, 0, len(occ.Fixes)),
	}
	for _, c := range occ.Criteria {
		body.Criteria = append(body.Criteria, occasionCriterion{
			ID:         c.ID,
			Kind:       string(c.Kind),
			Suggestion: c.Suggestion,
		})
	}
	for i := range occ.Fixes {
		fix := &occ.Fixes[i]
		criteria := make([]string, 0, len(fix.Criteria))
		criteria = append(criteria, fix.Criteria...)
		body.Fixes = append(body.Fixes, occasionFix{ID: fix.ID, Changes: fix.Changes, Criteria: criteria})
	}
	if err := writeJSON(w, http.StatusOK, body); err != nil {
		rt.log.Warn("server: occasions: one", "err", err.Error())
	}
}

// occasionOf reads the occasion a path names. An unknown id is the same
// not_found the contract gives an id nothing serves; the trim keeps a
// padded id from reading as a different one.
func (rt *lookRuntime) occasionOf(w http.ResponseWriter, r *http.Request) (*domain.Occasion, bool) {
	name := strings.TrimSpace(r.PathValue("id"))
	occ, known := rt.occasion[name]
	if !known {
		writeNotFound(w)
		return nil, false
	}
	return occ, true
}

// ---------------------------------------------------------------------------
// The opt-in pair

// serveOptInGet answers the account's ticked fixes for one occasion. No
// row means not ticked, so a fresh account reads the empty list.
func (rt *lookRuntime) serveOptInGet(w http.ResponseWriter, r *http.Request) {
	occ, ok := rt.occasionOf(w, r)
	if !ok {
		return
	}
	user, ok := caller(w, r)
	if !ok {
		return
	}
	ticked, err := rt.optIns(r.Context(), user.ID, occ.ID)
	if err != nil {
		rt.log.Error("server: occasions: the opt-in read", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if ticked == nil {
		ticked = []string{}
	}
	if err := writeJSON(w, http.StatusOK, optInBody{Occasion: occ.ID, Ticked: ticked}); err != nil {
		rt.log.Warn("server: occasions: the opt-in read", "err", err.Error())
	}
}

// serveOptInPut replaces the account's ticked set for one occasion.
// Every id is checked against the occasion's own fix menu before
// anything is written, the old set and the new one land in one
// transaction, and the answer is the set as stored, read back the way
// the look start reads it.
func (rt *lookRuntime) serveOptInPut(w http.ResponseWriter, r *http.Request) {
	occ, ok := rt.occasionOf(w, r)
	if !ok {
		return
	}
	user, ok := caller(w, r)
	if !ok {
		return
	}
	fields, ok := rt.srv.readObject(w, r, "ticked")
	if !ok {
		return
	}
	raw, named := fields["ticked"]
	if !named {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "ticked", Reason: "is required"})
		return
	}
	var asked []string
	if err := json.Unmarshal(raw, &asked); err != nil || asked == nil {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "ticked", Reason: "must be a list of fix ids"})
		return
	}
	fixes := make(map[string]bool, len(occ.Fixes))
	for i := range occ.Fixes {
		fixes[occ.Fixes[i].ID] = true
	}
	ticked := make([]string, 0, len(asked))
	seen := make(map[string]bool, len(asked))
	for _, one := range asked {
		fix := strings.TrimSpace(one)
		if !fixes[fix] {
			writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "ticked", Reason: "names an id that is not a fix on this occasion"})
			return
		}
		if seen[fix] {
			continue
		}
		seen[fix] = true
		ticked = append(ticked, fix)
	}
	ctx := r.Context()
	tx, err := rt.srv.db.BeginWrite(ctx)
	if err != nil {
		rt.log.Error("server: occasions: the opt-in write", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM opt_ins WHERE user_id = ? AND occasion = ?`, user.ID, occ.ID); err != nil {
		rt.log.Error("server: occasions: the opt-in write", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	for _, fix := range ticked {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO opt_ins (user_id, occasion, fix_id) VALUES (?, ?, ?)`, user.ID, occ.ID, fix); err != nil {
			rt.log.Error("server: occasions: the opt-in write", "err", err.Error())
			writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		rt.log.Error("server: occasions: the opt-in write", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	stored, err := rt.optIns(ctx, user.ID, occ.ID)
	if err != nil {
		rt.log.Error("server: occasions: the opt-in read", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if stored == nil {
		stored = []string{}
	}
	if err := writeJSON(w, http.StatusOK, optInBody{Occasion: occ.ID, Ticked: stored}); err != nil {
		rt.log.Warn("server: occasions: the opt-in write", "err", err.Error())
	}
}
