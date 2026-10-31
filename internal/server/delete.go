package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/nrynss/dresscode/internal/privacy"
	"github.com/nrynss/keel/erase"
	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/identity"
	identitysql "github.com/nrynss/keel/identity/sqlitestore"
	"github.com/nrynss/keel/job"
	jobsql "github.com/nrynss/keel/job/sqlitestore"
	"github.com/nrynss/keel/stream"
	"github.com/nrynss/keel/wire"
)

const (
	// RouteDeleteAccount is Keel's account deletion. The body is the
	// address and a code entered in the last 10 minutes.
	RouteDeleteAccount = "POST /api/account/deletion"
	// RouteDeletePhoto deletes one photo and the looks that used it.
	RouteDeletePhoto = "POST /api/photos/{id}/deletion"
	// RouteDeleteLook deletes one look and its render blobs.
	RouteDeleteLook = "POST /api/looks/{id}/deletion"
	// RouteDeleteHistory clears style memory and turns the switch off.
	RouteDeleteHistory = "POST /api/history/deletion"
	// RouteJob is the job state read. A deletion job does not need the
	// session. The id is the capability.
	RouteJob = "GET /api/jobs/{id}"
	// RouteJobEvents is the job event stream. A deletion job does not
	// need the session.
	RouteJobEvents = "GET /api/jobs/{id}/events"
)

// DeletionConfig is the wiring MountDeletion adds on top of Open. The
// signing key must be the key Open used, or the cookie will not resolve.
// Open itself does not mount deletion: that file belongs to sessions, and
// its tests expect these paths to miss until this is called.
type DeletionConfig struct {
	// SigningKey signs the session cookie. It must be the key passed to
	// Open. Empty is refused.
	SigningKey []byte
	// CodeKey verifies the deletion code. Empty is refused. Account
	// deletion cannot run without it.
	CodeKey []byte
	// Mail is unused by deletion itself. It is accepted so one identity
	// service can also send codes when a later mount shares it. Nil is
	// allowed.
	Mail identity.Sender
	// Blobs is the mediastore. Nil is refused. Tests pass a fake.
	Blobs privacy.Blobs
	// Memory erases the Lambo session. Nil is refused. Tests pass a fake.
	// MountDeletion does not dial.
	Memory privacy.Memory
	// Now is the clock for the deletion code's ten minute life. Nil
	// means time.Now.
	Now func() time.Time
	// Log receives faults. Nil uses the server's logger.
	Log *slog.Logger
}

type deletionRuntime struct {
	srv    *Server
	eng    *privacy.Engine
	svc    *identity.Service
	runner *job.Runner
	broker *stream.Broker
}

var (
	deletionMu sync.Mutex
	deletionBy = map[*Server]*deletionRuntime{}
)

// MountDeletion attaches photo, look, history, and account deletion, and
// the job read those routes return. It replaces the handler's identity
// middleware with one that can erase, using the same database and the
// same signing key. Call it before the process listens. A second call
// refuses, because the routes are already registered.
//
// The job read and stream are shared with MountLooks and dispatched by
// the job's kind (jobs.go), so either mount may come first. This runtime
// recovers only its own kinds: the two share one jobs table.
func (s *Server) MountDeletion(ctx context.Context, cfg DeletionConfig) error {
	if s == nil || s.db == nil || s.mux == nil {
		return fmt.Errorf("server: deletion: nil server")
	}
	if ctx == nil {
		return fmt.Errorf("server: deletion: nil context")
	}
	if len(cfg.SigningKey) == 0 {
		return fmt.Errorf("server: deletion: signing key is empty")
	}
	if len(cfg.CodeKey) == 0 {
		return fmt.Errorf("server: deletion: code key is empty")
	}
	if cfg.Blobs == nil || cfg.Memory == nil {
		return fmt.Errorf("server: deletion: blobs and memory are required")
	}
	deletionMu.Lock()
	defer deletionMu.Unlock()
	if _, ok := deletionBy[s]; ok {
		return fmt.Errorf("server: deletion: already mounted")
	}
	log := cfg.Log
	if log == nil {
		log = s.log
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	eng, err := privacy.New(privacy.Config{DB: s.db, Blobs: cfg.Blobs, Memory: cfg.Memory, Log: log})
	if err != nil {
		return fmt.Errorf("server: deletion: %w", err)
	}
	eraser, err := erase.New(eng.Source, erase.Config{Log: log})
	if err != nil {
		return fmt.Errorf("server: deletion: %w", err)
	}
	idStore, err := identitysql.Open(ctx, identitysql.Config{DB: s.db})
	if err != nil {
		return fmt.Errorf("server: deletion: %w", err)
	}
	key := append([]byte(nil), cfg.SigningKey...)
	codeKey := append([]byte(nil), cfg.CodeKey...)
	svc, err := identity.New(identity.Config{
		Store:      idStore,
		SigningKey: key,
		CodeKey:    codeKey,
		Mail:       cfg.Mail,
		Now:        now,
		Log:        log,
		Eraser:     eraser,
		Targets:    eng.Source,
	})
	if err != nil {
		return fmt.Errorf("server: deletion: %w", err)
	}
	jobStore, err := jobsql.Open(ctx, jobsql.Config{DB: s.db})
	if err != nil {
		return fmt.Errorf("server: deletion: %w", err)
	}
	kinds := svc.Kinds()
	kinds[privacy.KindName] = eng.Kind()
	broker := stream.New(stream.Config{})
	runner, err := job.Open(ctx, job.Config{
		Broker: broker,
		Store:  kindStore{Store: jobStore, kinds: kinds},
		Kinds:  kinds,
		Log:    log,
		Now:    now,
	})
	if err != nil {
		return fmt.Errorf("server: deletion: %w", err)
	}
	if err := svc.BindRunner(runner); err != nil {
		return fmt.Errorf("server: deletion: %w", err)
	}
	rt := &deletionRuntime{srv: s, eng: eng, svc: svc, runner: runner, broker: broker}
	s.mux.Handle(RouteDeleteAccount, svc.DeleteHandler())
	s.mux.Handle(RouteDeletePhoto, http.HandlerFunc(rt.deletePhoto))
	s.mux.Handle(RouteDeleteLook, http.HandlerFunc(rt.deleteLook))
	s.mux.Handle(RouteDeleteHistory, http.HandlerFunc(rt.deleteHistory))
	s.mountJobRoutes()
	s.handler = svc.Middleware(s.mux)
	s.identity = svc
	deletionBy[s] = rt
	return nil
}

func (s *Server) deletionOf() *deletionRuntime {
	deletionMu.Lock()
	defer deletionMu.Unlock()
	return deletionBy[s]
}

func (rt *deletionRuntime) deletePhoto(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r)
	if !ok {
		return
	}
	photoID := r.PathValue("id")
	if !id.Valid(photoID) {
		writeNotFound(w)
		return
	}
	jobID, err := rt.eng.StartPhoto(r.Context(), rt.runner, user.ID, photoID)
	writeDeletion(w, jobID, err)
}

func (rt *deletionRuntime) deleteLook(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r)
	if !ok {
		return
	}
	lookID := r.PathValue("id")
	if !id.Valid(lookID) {
		writeNotFound(w)
		return
	}
	jobID, err := rt.eng.StartLook(r.Context(), rt.runner, user.ID, lookID)
	writeDeletion(w, jobID, err)
}

func (rt *deletionRuntime) deleteHistory(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r)
	if !ok {
		return
	}
	jobID, err := rt.eng.StartHistory(r.Context(), rt.runner, user.ID)
	writeDeletion(w, jobID, err)
}

func caller(w http.ResponseWriter, r *http.Request) (identity.User, bool) {
	user, ok := identity.UserFromContext(r.Context())
	if !ok || !id.Valid(user.ID) {
		_ = wire.WriteError(w, http.StatusInternalServerError, wire.CodeInternal, "The request could not be finished.", nil)
		return identity.User{}, false
	}
	return user, true
}

func writeDeletion(w http.ResponseWriter, jobID string, err error) {
	if errors.Is(err, privacy.ErrAbsent) {
		writeNotFound(w)
		return
	}
	if err != nil {
		_ = wire.WriteError(w, http.StatusInternalServerError, wire.CodeInternal, "The request could not be finished.", nil)
		return
	}
	if err := writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID}); err != nil {
		return
	}
}

func (rt *deletionRuntime) serveJob(w http.ResponseWriter, r *http.Request) {
	view, ok := rt.visible(w, r)
	if !ok {
		return
	}
	if err := writeJSON(w, http.StatusOK, jobStateOf(r.PathValue("id"), view)); err != nil {
		return
	}
}

func (rt *deletionRuntime) serveJobEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := rt.visible(w, r); !ok {
		return
	}
	rt.broker.ServeTopic(w, r, job.Topic(r.PathValue("id")))
}

// jobView is the attempt the caller asked for. next is the replacement
// attempt when a restart has continued the same deletion under a new id.
type jobView struct {
	rec  job.Record
	next string
}

// visible reports the job a deletion id may be read as. The id is the
// capability for a deletion. Any other kind, including the inner erase
// the account job starts and does not return, is not_found. A malformed
// id is the same answer.
func (rt *deletionRuntime) visible(w http.ResponseWriter, r *http.Request) (jobView, bool) {
	jobID := r.PathValue("id")
	if !id.Valid(jobID) {
		writeNotFound(w)
		return jobView{}, false
	}
	attempts, err := rt.runner.Attempts(r.Context(), jobID)
	if err != nil || len(attempts) == 0 {
		writeNotFound(w)
		return jobView{}, false
	}
	var self job.Record
	var found bool
	newest := attempts[0]
	for _, attempt := range attempts {
		if attempt.ID == jobID {
			self = attempt
			found = true
		}
		if attempt.Attempt >= newest.Attempt {
			newest = attempt
		}
	}
	if !found || !deletionKind(self.Kind) {
		writeNotFound(w)
		return jobView{}, false
	}
	view := jobView{rec: self}
	if newest.ID != self.ID {
		view.next = newest.ID
		// The id the app holds reports the replacement while it is
		// still running. A finished replacement is read by following
		// next_id, so the held id keeps the interrupted status.
		switch newest.Status {
		case job.StatusQueued, job.StatusRunning:
			view.rec.Status = newest.Status
			view.rec.Progress = newest.Progress
		}
	}
	return view, true
}

func deletionKind(kind string) bool {
	return kind == identity.DeletionKindName || kind == privacy.KindName
}

type jobStateBody struct {
	JobID  string       `json:"job_id"`
	RootID string       `json:"root_id"`
	Status string       `json:"status"`
	Stage  string       `json:"stage,omitempty"`
	NextID string       `json:"next_id,omitempty"`
	Error  *jobStateErr `json:"error,omitempty"`
}

type jobStateErr struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Detail  json.RawMessage `json:"detail,omitempty"`
}

func jobStateOf(id string, view jobView) jobStateBody {
	rec := view.rec
	body := jobStateBody{
		JobID:  id,
		RootID: rec.RootID,
		Status: string(rec.Status),
		Stage:  rec.Progress.Stage,
		NextID: view.next,
	}
	if rec.Status != job.StatusError {
		return body
	}
	code := rec.Code
	if code == "" {
		code = "job_failed"
	}
	body.Error = &jobStateErr{
		Code:    code,
		Message: "The request could not be finished.",
		Detail:  rec.Detail,
	}
	return body
}
