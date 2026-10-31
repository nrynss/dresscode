package server

import (
	"context"
	"net/http"
	"sync"

	"github.com/nrynss/dresscode/internal/pipeline"
	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/job"
)

// The job read and the job stream are one pair of paths, and two mounts
// answer them: looks for a look job, deletion for a deletion job. This
// file registers the pair once and dispatches by the job's kind, so
// neither mount order is special and neither runtime has to know the
// other's routes. A second registration of one pattern panics the mux.

var (
	jobRouteMu    sync.Mutex
	jobRouteTaken = map[*Server]bool{}
)

// mountJobRoutes registers the job pair the first time either mount asks.
// Its lock is a leaf: a mount holds its own registry lock while calling,
// so taking the other mount's lock here would invert the order.
func (s *Server) mountJobRoutes() {
	jobRouteMu.Lock()
	defer jobRouteMu.Unlock()
	if jobRouteTaken[s] {
		return
	}
	jobRouteTaken[s] = true
	s.mux.Handle(RouteJob, http.HandlerFunc(s.serveJobRoute))
	s.mux.Handle(RouteJobEvents, http.HandlerFunc(s.serveJobEventsRoute))
}

func (s *Server) serveJobRoute(w http.ResponseWriter, r *http.Request) {
	s.jobRoute(w, r, false)
}

func (s *Server) serveJobEventsRoute(w http.ResponseWriter, r *http.Request) {
	s.jobRoute(w, r, true)
}

// jobRoute hands the request to the runtime that owns the job's kind.
// Each runtime's own handler carries its visibility - a look is read by
// its owner, a deletion id is the capability - so this reads the kind and
// nothing else, and an id no runtime owns is not_found.
func (s *Server) jobRoute(w http.ResponseWriter, r *http.Request, events bool) {
	jobID := r.PathValue("id")
	if !id.Valid(jobID) {
		writeNotFound(w)
		return
	}
	kind, ok := s.jobKind(r.Context(), jobID)
	if !ok {
		writeNotFound(w)
		return
	}
	if kind == pipeline.KindName {
		rt := s.looksOf()
		if rt == nil {
			writeNotFound(w)
			return
		}
		if events {
			rt.serveJobEvents(w, r)
			return
		}
		rt.serveJob(w, r)
		return
	}
	del := s.deletionOf()
	if del == nil || !deletionKind(kind) {
		writeNotFound(w)
		return
	}
	if events {
		del.serveJobEvents(w, r)
		return
	}
	del.serveJob(w, r)
}

// jobKind reads the kind of the attempt this id names. Both runtimes
// record into one jobs table, so either can answer; the kind is what
// decides which one serves the request.
func (s *Server) jobKind(ctx context.Context, jobID string) (string, bool) {
	var runner *job.Runner
	if rt := s.looksOf(); rt != nil {
		runner = rt.jobs
	} else if del := s.deletionOf(); del != nil {
		runner = del.runner
	}
	if runner == nil {
		return "", false
	}
	attempts, err := runner.Attempts(ctx, jobID)
	if err != nil {
		return "", false
	}
	for _, attempt := range attempts {
		if attempt.ID == jobID {
			return attempt.Kind, true
		}
	}
	return "", false
}

// kindStore shows a runner only its own kinds' unfinished rows. The two
// mounts share one jobs table, and Open's recovery interrupts every
// unfinished row it is shown but resumes only its own kinds, so a runner
// shown the other's rows would interrupt a job it cannot resume and leave
// it there. Every other read is by id or by root, which one kind owns.
type kindStore struct {
	job.Store
	kinds map[string]job.Kind
}

func (s kindStore) Unfinished(ctx context.Context) ([]job.Record, error) {
	all, err := s.Store.Unfinished(ctx)
	if err != nil {
		return nil, err
	}
	mine := make([]job.Record, 0, len(all))
	for _, rec := range all {
		if _, ok := s.kinds[rec.Kind]; ok {
			mine = append(mine, rec)
		}
	}
	return mine, nil
}
