package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/crook"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// crookKeepProceeds is how long a logged proceed is kept. Holds, asks and refusals are
// kept for good.
const crookKeepProceeds = 90 * 24 * time.Hour

// crookQueue is how many verdicts wait to be written. A verdict is logged off the request
// path, so a slow or locked database never delays an answer; when the queue is full the
// verdict is dropped and counted.
const crookQueue = 512

// crookRecorder queues Crook's verdicts and a goroutine writes them to the store.
type crookRecorder struct {
	st      store.Store
	q       chan crook.Entry
	dropped atomic.Int64
}

func (r *crookRecorder) Record(_ context.Context, e crook.Entry) error {
	select {
	case r.q <- e:
		return nil
	default:
		return fmt.Errorf("crook log queue full: %d verdicts dropped so far", r.dropped.Add(1))
	}
}

func (r *crookRecorder) write(ctx context.Context, e crook.Entry) error {
	_, err := r.st.AddCrookVerdict(ctx, store.CrookVerdict{
		At: e.At, Action: string(e.Action), Actor: string(e.Actor), Kind: string(e.Kind),
		Rule: e.Rule, RuleVersion: e.RuleVersion, Stage: e.Stage, Reason: e.Reason,
		Repo: e.Repo, Lane: e.Lane, Run: e.Run, Facts: e.Facts,
	})
	return err
}

// startCrookLog sends Crook's verdicts to the store and prunes old proceeds once a day.
// The returned func stops both, after the queue is written, and puts back the recorder
// that was set before.
func (s *Server) startCrookLog(ctx context.Context) func() {
	rec := &crookRecorder{st: s.Store, q: make(chan crook.Entry, crookQueue)}
	restore := crook.SetRecorder(rec, func(err error) { s.Log.Warn("log a crook verdict", "err", err) })
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for e := range rec.q {
			if err := rec.write(context.Background(), e); err != nil {
				s.Log.Warn("write a crook verdict", "err", err)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			if n, err := s.Store.PruneCrookProceeds(ctx, time.Now().Add(-crookKeepProceeds)); err != nil {
				if ctx.Err() == nil {
					s.Log.Warn("prune crook verdicts", "err", err)
				}
			} else if n > 0 {
				s.Log.Info("pruned crook proceeds", "count", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(24 * time.Hour):
			}
		}
	}()
	return func() {
		cancel()
		restore()
		close(rec.q)
		wg.Wait()
	}
}

// parseSince reads a time as RFC 3339 or "Nd" for N days ago; "" gives def.
func parseSince(v string, def time.Duration) (time.Time, error) {
	if v == "" {
		return time.Now().Add(-def), nil
	}
	if n, ok := strings.CutSuffix(v, "d"); ok {
		if days, err := strconv.Atoi(n); err == nil && days >= 0 {
			return time.Now().Add(-time.Duration(days) * 24 * time.Hour), nil
		}
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return t, fmt.Errorf("%q is not a time (want RFC 3339 or a number of days like 7d)", v)
	}
	return t, nil
}

func (s *Server) crookVerdicts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.CrookFilter{Rule: q.Get("rule"), Kind: q.Get("kind"), Action: q.Get("action"), Lane: q.Get("lane")}
	if v := q.Get("run"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("run %q is not a run id", v))
			return
		}
		f.Run = id
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 1000 {
			writeError(w, http.StatusBadRequest, errors.New("limit must be 1 to 1000"))
			return
		}
		f.Limit = n
	}
	if v := q.Get("since"); v != "" {
		t, err := parseSince(v, 0)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		f.Since = t
	}
	vs, err := s.Store.CrookVerdicts(r.Context(), f)
	if err != nil {
		s.fail(w, err)
		return
	}
	if vs == nil {
		vs = []store.CrookVerdict{}
	}
	writeJSON(w, http.StatusOK, api.CrookVerdicts{Verdicts: vs})
}

func (s *Server) crookWhy(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%q is not a verdict id", r.PathValue("id")))
		return
	}
	v, err := s.Store.CrookVerdict(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	out := api.CrookWhy{Verdict: v}
	if v.Stage != "" {
		decided := false
		for _, name := range crook.Stages() {
			if name == v.Stage {
				out.Path = append(out.Path, api.CrookStep{Stage: name, Outcome: "decided"})
				decided = true
				break
			}
			out.Path = append(out.Path, api.CrookStep{Stage: name, Outcome: "no opinion"})
		}
		if !decided {
			out.Path = append(out.Path, api.CrookStep{Stage: v.Stage, Outcome: "decided"})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) crookRules(w http.ResponseWriter, r *http.Request) {
	since, err := parseSince(r.URL.Query().Get("since"), 7*24*time.Hour)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	rules, err := s.Store.CrookCounts(r.Context(), since)
	if err != nil {
		s.fail(w, err)
		return
	}
	if rules == nil {
		rules = []store.CrookRuleCount{}
	}
	writeJSON(w, http.StatusOK, api.CrookRules{Since: since.UTC().Format(time.RFC3339), Rules: rules})
}

// ShadowRecorder writes the merge rules' shadow rows straight to the store. The shadow
// pass runs apart from the poll and is one write per change, so it needs no queue; a
// failed write is returned and tried again on the next poll.
func ShadowRecorder(st store.Store) crook.Recorder { return &crookRecorder{st: st} }
