package crook

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/redact"
)

// RuleVersion is the version every built-in rule carries. A later layer's rules bring
// their own; a built-in's changes only with a release that changes its answer.
const RuleVersion = 1

// Entry is one logged verdict: what was asked, who asked, what Crook said and which rule
// said it.
type Entry struct {
	At          time.Time
	Action      Action
	Actor       Actor
	Kind        Kind
	Rule        string
	RuleVersion int
	Stage       string
	Reason      string
	// Repo and Lane are names, Run an id; each is empty or 0 when the request did not
	// name one.
	Repo, Lane string
	Run        int64
	// Facts is a JSON object of the facts that mattered, redacted.
	Facts string
}

// Recorder keeps entries. The daemon sets one at start; with none set, Check only
// decides.
type Recorder interface {
	Record(ctx context.Context, e Entry) error
}

var (
	recorder atomic.Pointer[Recorder]
	onError  atomic.Pointer[func(error)]
)

// SetRecorder sets where Check logs (nil: nowhere). fail is called with a recording
// error, which never changes a verdict; it may be nil. It returns a func that puts back
// what was set before, so a caller that stops its recorder does not clear another's.
func SetRecorder(r Recorder, fail func(error)) (restore func()) {
	prevR, prevF := recorder.Load(), onError.Load()
	seenMu.Lock()
	clear(seen)
	seenMu.Unlock()
	if r == nil {
		recorder.Store(nil)
	} else {
		recorder.Store(&r)
	}
	if fail == nil {
		onError.Store(nil)
	} else {
		onError.Store(&fail)
	}
	return func() {
		recorder.Store(prevR)
		onError.Store(prevF)
	}
}

// Check is Decide plus the log: it decides, records the verdict and returns it. Every
// call site that acts on a verdict uses it. A route that proceeds is not recorded: it is
// a request answered, not an act, and the web polls constantly.
func Check(ctx context.Context, req Request) Verdict {
	v := Decide(ctx, req)
	Record(ctx, req, v)
	return v
}

// Record logs a verdict Decide gave. Use it with Decide when a caller acts on a verdict
// only some of the time: a probe or a classification is not an act and is not logged, and
// a hold that a queue retries is logged once.
func Record(ctx context.Context, req Request, v Verdict) {
	if p := recorder.Load(); p != nil && !(v.Kind == Proceed && strings.HasPrefix(string(req.Action), "route ")) {
		e := entryFor(req, v)
		if v.Kind != Proceed && repeated(e) {
			return
		}
		if err := (*p).Record(context.WithoutCancel(ctx), e); err != nil {
			if f := onError.Load(); f != nil {
				(*f)(err)
			}
		}
	}
}

// repeatWindow is how long an identical hold, ask or refusal is logged only once, so a
// caller that keeps hitting the same answer cannot grow the log without bound. Refusals
// are kept for good, which is why the repeats are dropped here.
const repeatWindow = time.Minute

var (
	seenMu sync.Mutex
	seen   = map[Entry]time.Time{}
)

// repeated reports whether the same non-proceed verdict was logged within repeatWindow,
// and otherwise notes this one.
func repeated(e Entry) bool {
	key := Entry{Action: e.Action, Actor: e.Actor, Kind: e.Kind, Rule: e.Rule, Reason: e.Reason, Repo: e.Repo, Lane: e.Lane, Run: e.Run}
	seenMu.Lock()
	defer seenMu.Unlock()
	if at, ok := seen[key]; ok && e.At.Sub(at) < repeatWindow {
		return true
	}
	if len(seen) >= 1024 {
		for k, at := range seen {
			if e.At.Sub(at) >= repeatWindow {
				delete(seen, k)
			}
		}
		if len(seen) >= 1024 {
			clear(seen)
		}
	}
	seen[key] = e.At
	return false
}

func entryFor(req Request, v Verdict) Entry {
	f := req.Facts
	return Entry{
		At: time.Now().UTC(), Action: req.Action, Actor: req.Actor,
		Kind: v.Kind, Rule: v.Rule, RuleVersion: RuleVersion, Stage: v.Stage,
		Reason: redact.String(v.Reason),
		Repo:   f.Repo, Lane: f.Lane, Run: f.Run,
		Facts: factsJSON(f),
	}
}

// factsJSON is the facts a caller gave, as a JSON object with the empty ones left out.
// Funcs are not facts; a keep-going check is recorded by its verdict's reason.
func factsJSON(f Facts) string {
	m := map[string]any{}
	set := func(k string, v any) { m[k] = v }
	if f.Budget != "" {
		set("budget", f.Budget)
		set("budget_daily", f.BudgetDaily)
	}
	if f.Runs != nil {
		set("runs", f.Runs)
	}
	if f.Route != nil {
		set("route_denied", string(f.Route.Denied))
	}
	if len(f.Problems) > 0 {
		set("problems", f.Problems)
	}
	for k, v := range map[string]string{"push": f.Push, "tag": f.Tag, "merge": f.Merge, "agent": f.Agent, "remote": f.Remote} {
		if v != "" {
			set(k, v)
		}
	}
	if f.TagsReserved {
		set("tags_reserved", true)
	}
	if f.GitLab {
		set("gitlab", true)
	}
	if f.ToOrigin {
		set("to_origin", true)
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return redact.String(string(b))
}
