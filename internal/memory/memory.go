// Package memory is the style-memory seam. Dresscode keeps one session
// per account. Remember writes a look as short facts, recall asks for an
// occasion, and erase removes the session.
//
// The live client talks to Lambo over MCP. The shapes it needs are in
// docs/lambo.md. This package does not dial.
package memory

import (
	"context"
	"errors"
	"strings"
)

const (
	// SessionPrefix is the start of every Dresscode session id. The id is
	// longer than the prefix. One account has one id: Session builds it.
	SessionPrefix = "dc-u-"

	// ErasedCode is the JSON-RPC code of a session an operator erased.
	// Nothing was read. The id does not come back, and the call is not retried.
	ErasedCode = -32003

	ConceptEntity      = "entity"
	ConceptLogic       = "logic"
	ConceptConstraint  = "constraint"
	ConceptResource    = "resource"
	ConceptObservation = "observation"
)

// ErrErased is a session that was erased. It is not an empty history.
var ErrErased = errors.New("memory: session was erased")

// Session builds the only session id for an account. The account is not
// already a session id. An erased id is not reused; that rule belongs to
// the live client, because a recording of the erased id must still replay.
func Session(account string) (string, error) {
	if account == "" || strings.ContainsAny(account, "/\\\x00") || strings.HasPrefix(account, SessionPrefix) {
		return "", errors.New("memory: account cannot be turned into a session id")
	}
	id := SessionPrefix + account
	if !ValidSession(id) {
		return "", errors.New("memory: account cannot be turned into a session id")
	}
	return id, nil
}

// ValidSession reports whether id is a Dresscode session id.
func ValidSession(id string) bool {
	return strings.HasPrefix(id, SessionPrefix) &&
		len(id) > len(SessionPrefix) &&
		!strings.ContainsAny(id, "/\\")
}

// Concept is one short fact. A look verdict is an observation.
type Concept struct {
	Content     string `json:"content"`
	ConceptType string `json:"concept_type"`
}

// Remembered is a derive the server accepted. State is pending until the
// write is applied. The recorded derive is pending; the stats exchange is
// a separate recording.
type Remembered struct {
	Receipt      string   `json:"receipt"`
	State        string   `json:"state"`
	Summary      string   `json:"summary,omitempty"`
	AgentID      string   `json:"agent_id,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
	Created      []string `json:"created,omitempty"`
	CreatedCount int      `json:"created_count,omitempty"`
	Embedded     int      `json:"embedded,omitempty"`
}

// Hit is one recalled concept. ConceptType may come back capitalised.
// Score is the server's rank, not an occasion match.
type Hit struct {
	NodeID      string  `json:"node_id"`
	Content     string  `json:"content"`
	ConceptType string  `json:"concept_type"`
	Score       float64 `json:"score"`
	IsCanonical bool    `json:"is_canonical"`
	BlastRadius *int    `json:"blast_radius"`
}

// Recall is the concepts a session returned for a query. The server does
// not filter to an occasion. The caller reads the text.
type Recall struct {
	Context  string   `json:"context"`
	Hits     []Hit    `json:"hits"`
	Warnings []string `json:"warnings"`
}

// Removed is what one erase deleted.
type Removed struct {
	Sessions           int `json:"sessions"`
	Interactions       int `json:"interactions"`
	Concepts           int `json:"concepts"`
	Vectors            int `json:"vectors"`
	Edges              int `json:"edges"`
	Synonyms           int `json:"synonyms"`
	CanonizationEvents int `json:"canonization_events"`
	Reservations       int `json:"reservations"`
	WriteIntents       int `json:"write_intents"`
	SessionStats       int `json:"session_stats"`
	LeaseRefusals      int `json:"lease_refusals"`
	Leases             int `json:"leases"`
}

// EraseResult is one erase. AlreadyAbsent means the session was already
// gone. A second erase is safe and reports that.
type EraseResult struct {
	Session       string  `json:"session"`
	AlreadyAbsent bool    `json:"already_absent"`
	Removed       Removed `json:"removed"`
	FenceToken    int     `json:"fence_token"`
}

// Memory remembers a look, recalls for an occasion, and erases a session.
// Occasion is the query text, the words of the occasion.
type Memory interface {
	Remember(ctx context.Context, session string, concepts []Concept) (Remembered, error)
	Recall(ctx context.Context, session, occasion string) (Recall, error)
	Erase(ctx context.Context, session string) (EraseResult, error)
}

func validConceptType(kind string) bool {
	switch kind {
	case ConceptEntity, ConceptLogic, ConceptConstraint, ConceptResource, ConceptObservation:
		return true
	default:
		return false
	}
}

func validateConcepts(concepts []Concept) error {
	if len(concepts) == 0 {
		return errors.New("memory: a look needs a concept")
	}
	for _, concept := range concepts {
		if concept.Content == "" {
			return errors.New("memory: concept content is empty")
		}
		if !validConceptType(concept.ConceptType) {
			return errors.New("memory: unknown concept type " + concept.ConceptType)
		}
	}
	return nil
}

func checkSession(id string) error {
	if !ValidSession(id) {
		return errors.New("memory: session id must start with dc-u- and be longer than that prefix")
	}
	return nil
}
