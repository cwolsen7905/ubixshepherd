package sqlite

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// The leak suite: a tenant's view of the store (ForTenant) touches nothing of another
// tenant's. Tenant 1 is seeded and every read is taken; tenant 2 is seeded and tenant 1's
// reads must not change; tenant 1's view is then given tenant 2's ids and keys in every
// read and write, and must find nothing and change nothing of tenant 2's. Every method of
// store.Store must be covered here or listed as the deployment's (leakExempt).

const leakDay = "2026-11-27"

// leakExempt are the methods that are the deployment's, not a tenant's.
var leakExempt = map[string]string{
	"Tenants":           "the list of tenants belongs to the deployment",
	"ForTenant":         "makes the views this suite tests",
	"Tenant":            "names the view's tenant",
	"Driver":            "the store's driver name",
	"Close":             "a view closes nothing (TestTenantViewClosesNothing)",
	"SaveWebSession":    "browser sessions are a user's, not a tenant's",
	"TouchWebSession":   "browser sessions are a user's, not a tenant's",
	"DeleteWebSession":  "browser sessions are a user's, not a tenant's",
	"DeleteWebSessions": "browser sessions are a user's, not a tenant's",
	"LiveWebSessions":   "browser sessions are a user's, not a tenant's",
}

// leakWorld is one tenant's seeded rows.
type leakWorld struct {
	tag                                                            string
	ws, repo, lane, run, event, dec, req, held, crook, next, topic int64
	art, question, attach, muster, brief, res                      int64
	wsPath, repoPath, setKey, project, musterKey, conv, topicRef   string
	artPath, gateSHA, verSHA, checkPath                            string
}

// seedT is the test seedTenant reports to; must and must2 take a call's results whole.
var seedT *testing.T

func must[T any](v T, err error) T {
	seedT.Helper()
	if err != nil {
		seedT.Fatalf("seed: %v", err)
	}
	return v
}

func seedTenant(t *testing.T, st store.Store, tag string) leakWorld {
	t.Helper()
	seedT = t
	ctx := context.Background()
	w := leakWorld{tag: tag, wsPath: "/ws-" + tag, repoPath: "/ws-" + tag + "/repo", setKey: "key-" + tag, project: "proj-" + tag,
		musterKey: "muster-" + tag, conv: "conv-" + tag, artPath: "docs/a-" + tag + ".md", gateSHA: "gate-" + tag, verSHA: "ver-" + tag, checkPath: "check-" + tag}
	ws := must(st.SaveWorkspace(ctx, store.Workspace{Name: "ws-" + tag, Path: w.wsPath}, []store.Repo{{Name: "repo-" + tag, Path: w.repoPath}}))
	w.ws = ws.ID
	repos := must(st.Repos(ctx, ws.ID))
	w.repo = repos[0].ID
	w.lane = must(st.CreateLane(ctx, store.Lane{RepoID: w.repo, Name: "lane-" + tag, Branch: "b-" + tag, Worktree: "/wt-" + tag, State: store.LaneOpen, Scope: []string{"a/**"}})).ID
	w.run = must(st.CreateRun(ctx, store.Run{LaneID: w.lane, Agent: "claude", State: store.RunFailed, Prompt: "do " + tag})).ID
	w.event = must(st.AddEvent(ctx, store.Event{RunID: w.run, Kind: "text", Text: "said " + tag})).ID
	w.dec = must(st.CreateDecision(ctx, store.Decision{RunID: w.run, Question: "which " + tag, Options: []string{"x", "y"}})).ID
	w.req = must(st.CreateRequest(ctx, store.Request{FromRun: w.run, Kind: "question", Lane: "lane-" + tag, Message: "ask " + tag, State: store.RequestPending})).ID
	w.held = must(st.AddHeldStart(ctx, store.HeldStart{Request: "req-" + tag, Why: "quota " + tag})).ID
	w.crook = must(st.AddCrookVerdict(ctx, store.CrookVerdict{Action: "lane_open", Kind: "proceed", Rule: "rule-" + tag, Lane: "lane-" + tag, Run: w.run})).ID
	if err := st.AddFeed(ctx, "note", "feed "+tag, w.run); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(ctx, w.setKey, "value-"+tag); err != nil {
		t.Fatal(err)
	}
	w.res = must(st.CreateReservation(ctx, store.Reservation{RepoID: w.repo, LaneID: w.lane, Tag: "v-" + tag, State: store.TagReserved})).ID
	at := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	if err := st.PutConversation(ctx, store.Conversation{ID: w.conv, Agent: "claude", RepoID: w.repo, Title: "chat " + tag, Started: at, Last: at}); err != nil {
		t.Fatal(err)
	}
	for _, sp := range []store.Spend{{Day: leakDay, Source: "src-" + tag, Ref: w.run, USD: 1}, {Day: leakDay, Source: store.OriginDesk, USD: 2}} {
		if err := st.AddSpend(ctx, sp); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetKeepGoing(ctx, store.KeepGoing{WorkspaceID: w.ws, On: true, By: "person-" + tag}); err != nil {
		t.Fatal(err)
	}
	must(st.CountLaneStart(ctx, w.lane, leakDay))
	draft := must(st.SaveBriefDraft(ctx, w.project, "brief "+tag, "alice"))
	w.brief = must(st.ApproveBrief(ctx, draft.ID, "bob")).ID
	must(st.SaveBriefDraft(ctx, w.project, "brief two "+tag, "alice"))
	w.next = must(st.AddNextItem(ctx, store.NextItem{Title: "next " + tag})).ID
	w.topic = must(st.AddTopic(ctx, store.Topic{Title: "topic " + tag})).ID
	w.topicRef = strconv.FormatInt(w.lane, 10)
	must(st.LinkTopic(ctx, store.TopicLink{TopicID: w.topic, Kind: store.TopicLane, Ref: w.topicRef}))
	w.question = must(st.AddQuestion(ctx, store.Question{WorkspaceID: w.ws, Text: "why " + tag})).ID
	must(st.AddDiagnosis(ctx, store.Diagnosis{RunID: w.run, Kind: "gate", Summary: "red " + tag}))
	if err := st.AddUsage(ctx, store.Usage{Day: leakDay, RunID: w.run, Agent: "claude", Model: "m", Requests: 1}); err != nil {
		t.Fatal(err)
	}
	must(st.AddDeskEvent(ctx, store.DeskEvent{WorkspaceID: w.ws, Kind: store.DeskUser, Text: "hello " + tag}))
	w.attach = must(st.AddAttachment(ctx, store.Attachment{WorkspaceID: w.ws, SHA: "file-" + tag, Name: "f.png", Mime: "image/png", Kind: "image", Size: 3})).ID
	if err := st.SaveDeskTurnLog(ctx, store.DeskTurnLog{WorkspaceID: w.ws, Turn: 1, StartedBy: "person"}); err != nil {
		t.Fatal(err)
	}
	must(st.AddGateResult(ctx, store.GateResult{LaneID: w.lane, SHA: w.gateSHA, Gate: "make check", Passed: true}))
	art := must2(st.UpsertArtifact(ctx, store.Artifact{WorkspaceID: w.ws, RepoID: w.repo, LaneID: w.lane, RunID: w.run, Path: w.artPath, Title: "a " + tag}))
	w.art = art.ID
	if _, err := st.AddArtifactVersion(ctx, store.ArtifactVersion{ArtifactID: w.art, SHA256: w.verSHA, Size: 5, RunID: w.run}); err != nil {
		t.Fatal(err)
	}
	if err := st.LinkArtifact(ctx, store.ArtifactLink{ArtifactID: w.art, Kind: store.ArtifactLinkDecision, Ref: strconv.FormatInt(w.dec, 10)}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutLaneForge(ctx, store.LaneForge{LaneID: w.lane, MR: 40 + int(w.lane), MRState: "opened"}); err != nil {
		t.Fatal(err)
	}
	m := must2(st.AddMuster(ctx, store.MusterItem{Key: w.musterKey, Kind: "lane.stale", SubjectKind: "lane", SubjectID: w.lane}))
	w.muster = m.ID
	must(st.ApproveCheckFiles(ctx, []store.CheckFile{{RepoID: w.repo, Path: w.checkPath, Blob: "blob", BaseCommit: "base", ApprovedBy: "bob"}}))
	return w
}

func must2[T any](v T, _ bool, err error) T {
	seedT.Helper()
	if err != nil {
		seedT.Fatalf("seed: %v", err)
	}
	return v
}

// leakRead is one read's answer, compared whole.
type leakRead struct {
	V   any
	Err string
}

func rd(v any, err error) leakRead {
	r := leakRead{V: v}
	if err != nil {
		r.Err = err.Error()
	}
	return r
}

// leakReads are every read of store.Store, taken with one tenant's own ids.
var leakReads = map[string]func(context.Context, store.Store, leakWorld) leakRead{
	"Workspaces":   func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Workspaces(c)) },
	"Repos":        func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Repos(c, w.ws)) },
	"AllRepos":     func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.AllRepos(c, w.ws)) },
	"Repo":         func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Repo(c, w.repo)) },
	"Lanes":        func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Lanes(c, w.repo)) },
	"Lane":         func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Lane(c, w.lane)) },
	"Run":          func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Run(c, w.run)) },
	"Runs":         func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Runs(c, 0, "", 100)) },
	"HeldStarts":   func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.HeldStarts(c)) },
	"CrookVerdict": func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.CrookVerdict(c, w.crook)) },
	"CrookVerdicts": func(c context.Context, s store.Store, w leakWorld) leakRead {
		return rd(s.CrookVerdicts(c, store.CrookFilter{Limit: 100}))
	},
	"CrookCounts":   func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.CrookCounts(c, time.Time{})) },
	"Events":        func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Events(c, w.run)) },
	"Decision":      func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Decision(c, w.dec)) },
	"Decisions":     func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Decisions(c, "")) },
	"Request":       func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Request(c, w.req)) },
	"Requests":      func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Requests(c)) },
	"Feed":          func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Feed(c, 0, 1000)) },
	"LastFeed":      func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.LastFeed(c)) },
	"Setting":       func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Setting(c, w.setKey)) },
	"Reservations":  func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Reservations(c, w.repo)) },
	"Conversations": func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Conversations(c, 0)) },
	"SpendOn":       func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.SpendOn(c, leakDay)) },
	"KeepGoing":     func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.KeepGoing(c, w.ws)) },
	"LaneStarts": func(c context.Context, s store.Store, w leakWorld) leakRead {
		return rd(s.LaneStarts(c, w.lane, leakDay))
	},
	"ProjectBrief":  func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.ProjectBrief(c, w.brief)) },
	"ApprovedBrief": func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.ApprovedBrief(c, w.project)) },
	"PendingBrief":  func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.PendingBrief(c, w.project)) },
	"BriefHistory":  func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.BriefHistory(c, w.project)) },
	"NextItem":      func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.NextItem(c, w.next)) },
	"NextItems":     func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.NextItems(c)) },
	"Topic":         func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Topic(c, w.topic)) },
	"Topics":        func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Topics(c)) },
	"TopicLinks":    func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.TopicLinks(c, w.topic)) },
	"TopicOf": func(c context.Context, s store.Store, w leakWorld) leakRead {
		return rd(s.TopicOf(c, store.TopicLane, w.topicRef))
	},
	"Question":  func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Question(c, w.question)) },
	"Questions": func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Questions(c, 0, "")) },
	"SpendRollup": func(c context.Context, s store.Store, w leakWorld) leakRead {
		return rd(s.SpendRollup(c, leakDay, leakDay))
	},
	"Diagnoses":       func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Diagnoses(c, w.run)) },
	"LatestDiagnoses": func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.LatestDiagnoses(c, w.run)) },
	"RunUsage":        func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.RunUsage(c, w.run)) },
	"UsageStats": func(c context.Context, s store.Store, w leakWorld) leakRead {
		return rd(s.UsageStats(c, leakDay, leakDay))
	},
	"DeskEvents": func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.DeskEvents(c, w.ws, 0, 100)) },
	"DeskHistory": func(c context.Context, s store.Store, w leakWorld) leakRead {
		return rd(s.DeskHistory(c, w.ws, 0, 100))
	},
	"Attachment":   func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Attachment(c, w.attach)) },
	"DeskTurnLogs": func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.DeskTurnLogs(c, w.ws)) },
	"LaneHistory": func(c context.Context, s store.Store, w leakWorld) leakRead {
		return rd(s.LaneHistory(c, store.LaneHistoryFilter{State: "all"}))
	},
	"RunHistory": func(c context.Context, s store.Store, w leakWorld) leakRead {
		return rd(s.RunHistory(c, store.RunHistoryFilter{}))
	},
	"GateResultFor": func(c context.Context, s store.Store, w leakWorld) leakRead {
		return rd(s.GateResultFor(c, w.lane, w.gateSHA))
	},
	"LastGateResult": func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.LastGateResult(c, w.lane)) },
	"Artifact":       func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.Artifact(c, w.art)) },
	"LaneArtifact": func(c context.Context, s store.Store, w leakWorld) leakRead {
		return rd(s.LaneArtifact(c, w.lane, w.artPath))
	},
	"ListArtifacts": func(c context.Context, s store.Store, w leakWorld) leakRead {
		return rd(s.ListArtifacts(c, store.ArtifactFilter{}))
	},
	"DecisionArtifacts": func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.DecisionArtifacts(c, w.dec)) },
	"LaneForge":         func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.LaneForge(c, w.lane)) },
	"LaneActivity":      func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.LaneActivity(c, w.repo)) },
	"MusterItem":        func(c context.Context, s store.Store, w leakWorld) leakRead { return rd(s.MusterItem(c, w.muster)) },
	"MusterItems": func(c context.Context, s store.Store, w leakWorld) leakRead {
		return rd(s.MusterItems(c, store.MusterFilter{}))
	},
	"CheckFiles": func(c context.Context, s store.Store, w leakWorld) leakRead {
		return rd(s.CheckFiles(c, w.repo, false))
	},
}

func readAll(ctx context.Context, s store.Store, w leakWorld) map[string]leakRead {
	out := map[string]leakRead{}
	for name, f := range leakReads {
		out[name] = f(ctx, s, w)
	}
	return out
}

func diffReads(t *testing.T, what string, before, after map[string]leakRead) {
	t.Helper()
	names := make([]string, 0, len(before))
	for n := range before {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if !reflect.DeepEqual(before[n], after[n]) {
			t.Errorf("%s: %s changed:\n before %+v\n after  %+v", what, n, before[n], after[n])
		}
	}
}

// nothing says a read with another tenant's id found nothing: not found, or an empty
// answer.
func nothing(t *testing.T, name string, v any, err error) {
	t.Helper()
	if errors.Is(err, store.ErrNotFound) {
		return
	}
	if err != nil {
		t.Errorf("%s with another tenant's id: unexpected error %v", name, err)
		return
	}
	rv := reflect.ValueOf(v)
	switch {
	case !rv.IsValid():
	case rv.Kind() == reflect.Slice || rv.Kind() == reflect.Map:
		if rv.Len() != 0 {
			t.Errorf("%s with another tenant's id found %+v", name, v)
		}
	case rv.Kind() == reflect.Pointer:
		if !rv.IsNil() {
			t.Errorf("%s with another tenant's id found %+v", name, v)
		}
	default:
		if !rv.IsZero() {
			t.Errorf("%s with another tenant's id found %+v", name, v)
		}
	}
}

// notFound says a read by another tenant's id answered as for a missing row.
func notFound(t *testing.T, name string, err error) {
	t.Helper()
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("%s with another tenant's id: %v, want not found", name, err)
	}
}

// refused says a write with another tenant's id or key failed with one of the errors.
func refused(t *testing.T, name string, err error, want ...error) {
	t.Helper()
	for _, w := range want {
		if errors.Is(err, w) {
			return
		}
	}
	t.Errorf("%s with another tenant's id: %v, want one of %v", name, err, want)
}

// leakProbes give tenant 1's view (a) tenant 2's ids (b) and check it finds nothing; a
// write's harm to tenant 2 is caught by comparing tenant 2's reads before and after.
var leakProbes = map[string]func(*testing.T, context.Context, store.Store, leakWorld, leakWorld){
	// Reads by another tenant's id or key.
	"Repos": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.Repos(c, b.ws)
		nothing(t, "Repos", v, err)
	},
	"AllRepos": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.AllRepos(c, b.ws)
		nothing(t, "AllRepos", v, err)
	},
	"Repo": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.Repo(c, b.repo)
		notFound(t, "Repo", err)
	},
	"Lanes": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.Lanes(c, b.repo)
		nothing(t, "Lanes", v, err)
	},
	"Lane": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.Lane(c, b.lane)
		notFound(t, "Lane", err)
	},
	"Run": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.Run(c, b.run)
		notFound(t, "Run", err)
	},
	"Runs": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.Runs(c, b.lane, "", 100)
		nothing(t, "Runs", v, err)
	},
	"Events": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.Events(c, b.run)
		nothing(t, "Events", v, err)
	},
	"Decision": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.Decision(c, b.dec)
		notFound(t, "Decision", err)
	},
	"Request": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.Request(c, b.req)
		notFound(t, "Request", err)
	},
	"CrookVerdict": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.CrookVerdict(c, b.crook)
		notFound(t, "CrookVerdict", err)
	},
	"CrookVerdicts": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.CrookVerdicts(c, store.CrookFilter{Rule: "rule-" + b.tag})
		nothing(t, "CrookVerdicts", v, err)
	},
	"Setting": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.Setting(c, b.setKey)
		nothing(t, "Setting", v, err)
	},
	"Reservations": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.Reservations(c, b.repo)
		nothing(t, "Reservations", v, err)
	},
	"Conversations": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.Conversations(c, b.repo)
		nothing(t, "Conversations", v, err)
	},
	"KeepGoing": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.KeepGoing(c, b.ws)
		if err != nil || v.On || v.By != "" {
			t.Errorf("KeepGoing of another tenant's workspace: %+v %v", v, err)
		}
	},
	"LaneStarts": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.LaneStarts(c, b.lane, leakDay)
		nothing(t, "LaneStarts", v, err)
	},
	"ProjectBrief": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.ProjectBrief(c, b.brief)
		notFound(t, "ProjectBrief", err)
	},
	"ApprovedBrief": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.ApprovedBrief(c, b.project)
		notFound(t, "ApprovedBrief", err)
	},
	"PendingBrief": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.PendingBrief(c, b.project)
		notFound(t, "PendingBrief", err)
	},
	"BriefHistory": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.BriefHistory(c, b.project)
		nothing(t, "BriefHistory", v, err)
	},
	"NextItem": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.NextItem(c, b.next)
		notFound(t, "NextItem", err)
	},
	"Topic": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.Topic(c, b.topic)
		notFound(t, "Topic", err)
	},
	"TopicLinks": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.TopicLinks(c, b.topic)
		nothing(t, "TopicLinks", v, err)
	},
	"TopicOf": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.TopicOf(c, store.TopicLane, b.topicRef)
		notFound(t, "TopicOf", err)
	},
	"Question": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.Question(c, b.question)
		notFound(t, "Question", err)
	},
	"Questions": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.Questions(c, b.ws, "")
		nothing(t, "Questions", v, err)
	},
	"Diagnoses": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.Diagnoses(c, b.run)
		nothing(t, "Diagnoses", v, err)
	},
	"LatestDiagnoses": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.LatestDiagnoses(c, b.run)
		nothing(t, "LatestDiagnoses", v, err)
	},
	"RunUsage": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.RunUsage(c, b.run)
		notFound(t, "RunUsage", err)
	},
	"DeskEvents": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.DeskEvents(c, b.ws, 0, 100)
		nothing(t, "DeskEvents", v, err)
	},
	"DeskHistory": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.DeskHistory(c, b.ws, 0, 100)
		nothing(t, "DeskHistory", v, err)
	},
	"Attachment": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.Attachment(c, b.attach)
		notFound(t, "Attachment", err)
	},
	"DeskTurnLogs": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.DeskTurnLogs(c, b.ws)
		nothing(t, "DeskTurnLogs", v, err)
	},
	"LaneHistory": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.LaneHistory(c, store.LaneHistoryFilter{LaneID: b.lane, State: "all"})
		nothing(t, "LaneHistory", v, err)
	},
	"RunHistory": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.RunHistory(c, store.RunHistoryFilter{WorkspaceID: b.ws})
		if err != nil || !reflect.DeepEqual(v, store.RunHistory{}) {
			bare, _ := a.ForTenant(3).RunHistory(c, store.RunHistoryFilter{WorkspaceID: b.ws})
			if !reflect.DeepEqual(v, bare) {
				t.Errorf("RunHistory of another tenant's workspace: %+v %v", v, err)
			}
		}
	},
	"GateResultFor": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.GateResultFor(c, b.lane, b.gateSHA)
		nothing(t, "GateResultFor", v, err)
	},
	"LastGateResult": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.LastGateResult(c, b.lane)
		nothing(t, "LastGateResult", v, err)
	},
	"Artifact": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.Artifact(c, b.art)
		notFound(t, "Artifact", err)
	},
	"LaneArtifact": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.LaneArtifact(c, b.lane, b.artPath)
		notFound(t, "LaneArtifact", err)
	},
	"ListArtifacts": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.ListArtifacts(c, store.ArtifactFilter{LaneID: b.lane})
		nothing(t, "ListArtifacts", v, err)
	},
	"DecisionArtifacts": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.DecisionArtifacts(c, b.dec)
		nothing(t, "DecisionArtifacts", v, err)
	},
	"LaneForge": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.LaneForge(c, b.lane)
		if err != nil || v.MR != 0 || v.MRState != "" {
			t.Errorf("LaneForge of another tenant's lane: %+v %v", v, err)
		}
	},
	"LaneActivity": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.LaneActivity(c, b.repo)
		nothing(t, "LaneActivity", v, err)
	},
	"MusterItem": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.MusterItem(c, b.muster)
		notFound(t, "MusterItem", err)
	},
	"CheckFiles": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		v, err := a.CheckFiles(c, b.repo, false)
		nothing(t, "CheckFiles", v, err)
	},

	// Writes with another tenant's ids or keys: refused, or a no-op; tenant 2's reads are
	// compared after all of them.
	"SaveWorkspace": func(t *testing.T, c context.Context, a store.Store, b, own leakWorld) {
		_, err := a.SaveWorkspace(c, store.Workspace{Name: "taken", Path: b.wsPath}, nil)
		refused(t, "SaveWorkspace (another team's path)", err, store.ErrConflict)
		_, err = a.SaveWorkspace(c, store.Workspace{Name: "ws-" + own.tag, Path: own.wsPath}, []store.Repo{{Name: "stolen", Path: b.repoPath}})
		refused(t, "SaveWorkspace (another team's repo path)", err, store.ErrConflict)
	},
	"RenameWorkspace": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.RenameWorkspace(c, b.ws, "renamed-by-a")
		refused(t, "RenameWorkspace", err, store.ErrNotFound)
	},
	"SetRepoRemoved": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		now := time.Now()
		refused(t, "SetRepoRemoved", a.SetRepoRemoved(c, b.repo, &now), store.ErrNotFound)
	},
	"CreateLane": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.CreateLane(c, store.Lane{RepoID: b.repo, Name: "intruder", Worktree: "/wt-intruder", State: store.LaneOpen})
		refused(t, "CreateLane", err, store.ErrNotFound)
	},
	"SetLaneState": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		refused(t, "SetLaneState", a.SetLaneState(c, b.lane, store.LaneClosed), store.ErrNotFound)
	},
	"SetLaneScope": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		a.SetLaneScope(c, b.lane, []string{"**"})
	},
	"DeleteLane": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) { a.DeleteLane(c, b.lane) },
	"CreateRun": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.CreateRun(c, store.Run{LaneID: b.lane, Agent: "claude", State: store.RunRunning})
		refused(t, "CreateRun", err, store.ErrNotFound)
	},
	"UpdateRun": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		a.UpdateRun(c, store.Run{ID: b.run, State: store.RunSucceeded, Error: "overwritten"})
	},
	"DismissRun": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		now := time.Now()
		refused(t, "DismissRun", a.DismissRun(c, b.run, &now), store.ErrNotFound)
	},
	"AddHeldStart": func(t *testing.T, c context.Context, a store.Store, _, own leakWorld) {
		if _, err := a.AddHeldStart(c, store.HeldStart{Request: "mine", Why: "mine"}); err != nil {
			t.Errorf("AddHeldStart: %v", err)
		}
	},
	"DeleteHeldStart": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) { a.DeleteHeldStart(c, b.held) },
	"AddCrookVerdict": func(t *testing.T, c context.Context, a store.Store, _, _ leakWorld) {
		if _, err := a.AddCrookVerdict(c, store.CrookVerdict{Action: "x", Kind: "proceed", Rule: "mine"}); err != nil {
			t.Errorf("AddCrookVerdict: %v", err)
		}
	},
	"PruneCrookProceeds": func(t *testing.T, c context.Context, a store.Store, _, _ leakWorld) {
		a.PruneCrookProceeds(c, time.Now().Add(time.Hour))
	},
	"SetRunReason": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		a.SetRunReason(c, b.run, "overwritten")
	},
	"AddEvent": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.AddEvent(c, store.Event{RunID: b.run, Kind: "text", Text: "intruder"})
		refused(t, "AddEvent", err, store.ErrNotFound)
	},
	"CreateDecision": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.CreateDecision(c, store.Decision{RunID: b.run, Question: "intruder"})
		refused(t, "CreateDecision", err, store.ErrNotFound)
	},
	"AnswerDecision": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.AnswerDecision(c, b.dec, "x")
		refused(t, "AnswerDecision", err, store.ErrNotFound)
	},
	"SetDecisionRun": func(t *testing.T, c context.Context, a store.Store, b, own leakWorld) {
		a.SetDecisionRun(c, b.dec, own.run)
	},
	"SetDecisionState": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.SetDecisionState(c, b.dec, "closed")
		refused(t, "SetDecisionState", err, store.ErrNotFound)
	},
	"CreateRequest": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.CreateRequest(c, store.Request{FromRun: b.run, Kind: "question", State: store.RequestPending})
		refused(t, "CreateRequest", err, store.ErrNotFound)
	},
	"UpdateRequest": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		a.UpdateRequest(c, store.Request{ID: b.req, State: store.RequestRouted, Reply: "overwritten"})
	},
	"AddFeed": func(t *testing.T, c context.Context, a store.Store, _, _ leakWorld) {
		if err := a.AddFeed(c, "note", "mine", 0); err != nil {
			t.Errorf("AddFeed: %v", err)
		}
	},
	"SetSetting": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		refused(t, "SetSetting", a.SetSetting(c, b.setKey, "overwritten"), store.ErrConflict)
	},
	"CreateReservation": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.CreateReservation(c, store.Reservation{RepoID: b.repo, Tag: "v-intruder", State: store.TagReserved})
		refused(t, "CreateReservation", err, store.ErrNotFound)
	},
	"SetReservation": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		a.SetReservation(c, b.res, store.TagReleased, "sha")
	},
	"PutConversation": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		err := a.PutConversation(c, store.Conversation{ID: b.conv, Agent: "claude", Title: "overwritten", Started: time.Now(), Last: time.Now()})
		refused(t, "PutConversation", err, store.ErrConflict)
	},
	"AddSpend": func(t *testing.T, c context.Context, a store.Store, _, _ leakWorld) {
		if err := a.AddSpend(c, store.Spend{Day: leakDay, Source: "mine", USD: 9}); err != nil {
			t.Errorf("AddSpend: %v", err)
		}
	},
	"SetKeepGoing": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		refused(t, "SetKeepGoing", a.SetKeepGoing(c, store.KeepGoing{WorkspaceID: b.ws, On: false, By: "intruder"}), store.ErrNotFound)
	},
	"CountLaneStart": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.CountLaneStart(c, b.lane, leakDay)
		refused(t, "CountLaneStart", err, store.ErrNotFound)
	},
	"AddDeskEvent": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.AddDeskEvent(c, store.DeskEvent{WorkspaceID: b.ws, Kind: store.DeskUser, Text: "intruder"})
		refused(t, "AddDeskEvent", err, store.ErrNotFound)
	},
	"SaveBriefDraft": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.SaveBriefDraft(c, b.project, "intruder", "mallory")
		refused(t, "SaveBriefDraft", err, store.ErrConflict)
	},
	"ApproveBrief": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.ApproveBrief(c, b.brief, "mallory")
		refused(t, "ApproveBrief", err, store.ErrNotFound)
	},
	"AddNextItem": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.AddNextItem(c, store.NextItem{Title: "waits on theirs", DependsOn: []int64{b.next}})
		refused(t, "AddNextItem (another team's item as a dependency)", err, store.ErrConflict)
	},
	"UpdateNextItem": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		title := "overwritten"
		_, err := a.UpdateNextItem(c, b.next, store.NextPatch{Title: &title})
		refused(t, "UpdateNextItem", err, store.ErrNotFound)
	},
	"MoveNextItem": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.MoveNextItem(c, b.next, "top")
		refused(t, "MoveNextItem", err, store.ErrNotFound)
	},
	"AddTopic": func(t *testing.T, c context.Context, a store.Store, _, _ leakWorld) {
		if _, err := a.AddTopic(c, store.Topic{Title: "mine"}); err != nil {
			t.Errorf("AddTopic: %v", err)
		}
	},
	"UpdateTopic": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		title := "overwritten"
		_, err := a.UpdateTopic(c, b.topic, store.TopicPatch{Title: &title})
		refused(t, "UpdateTopic", err, store.ErrNotFound)
	},
	"MergeTopics": func(t *testing.T, c context.Context, a store.Store, b, own leakWorld) {
		_, err := a.MergeTopics(c, b.topic, own.topic)
		refused(t, "MergeTopics (theirs into mine)", err, store.ErrNotFound)
		_, err = a.MergeTopics(c, own.topic, b.topic)
		refused(t, "MergeTopics (mine into theirs)", err, store.ErrNotFound)
	},
	"LinkTopic": func(t *testing.T, c context.Context, a store.Store, b, own leakWorld) {
		_, err := a.LinkTopic(c, store.TopicLink{TopicID: b.topic, Kind: store.TopicRun, Ref: "1"})
		refused(t, "LinkTopic (another team's topic)", err, store.ErrNotFound)
		_, err = a.LinkTopic(c, store.TopicLink{TopicID: own.topic, Kind: store.TopicLane, Ref: b.topicRef, By: "person"})
		refused(t, "LinkTopic (another team's link)", err, store.ErrConflict)
	},
	"AddQuestion": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.AddQuestion(c, store.Question{WorkspaceID: b.ws, Text: "intruder"})
		refused(t, "AddQuestion", err, store.ErrNotFound)
	},
	"CloseQuestion": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.CloseQuestion(c, b.question, store.QuestionAnswered, 1)
		refused(t, "CloseQuestion", err, store.ErrNotFound)
	},
	"SetQuestionState": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.SetQuestionState(c, b.question, store.QuestionAnswered)
		refused(t, "SetQuestionState", err, store.ErrNotFound)
	},
	"AddDiagnosis": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.AddDiagnosis(c, store.Diagnosis{RunID: b.run, Kind: "intruder"})
		refused(t, "AddDiagnosis", err, store.ErrNotFound)
	},
	"AddUsage": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		refused(t, "AddUsage", a.AddUsage(c, store.Usage{Day: leakDay, RunID: b.run, Requests: 99}), store.ErrNotFound)
	},
	"TrimDeskEvents": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		if err := a.TrimDeskEvents(c, b.ws, 0); err != nil {
			t.Errorf("TrimDeskEvents of another team's workspace: %v, want a no-op", err)
		}
	},
	"AddDeskEventWithFiles": func(t *testing.T, c context.Context, a store.Store, b, own leakWorld) {
		_, err := a.AddDeskEventWithFiles(c, store.DeskEvent{WorkspaceID: b.ws, Kind: store.DeskUser, Text: "intruder"}, nil)
		refused(t, "AddDeskEventWithFiles (another team's workspace)", err, store.ErrNotFound)
		_, err = a.AddDeskEventWithFiles(c, store.DeskEvent{WorkspaceID: own.ws, Kind: store.DeskUser, Text: "takes theirs"}, []int64{b.attach})
		refused(t, "AddDeskEventWithFiles (another team's attachment)", err, store.ErrConflict)
	},
	"AddAttachment": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.AddAttachment(c, store.Attachment{WorkspaceID: b.ws, SHA: "x", Name: "x"})
		refused(t, "AddAttachment", err, store.ErrNotFound)
	},
	"TakeAttachments": func(t *testing.T, c context.Context, a store.Store, b, own leakWorld) {
		_, err := a.TakeAttachments(c, own.ws, 1, []int64{b.attach})
		refused(t, "TakeAttachments", err, store.ErrConflict)
	},
	"SweepAttachments": func(t *testing.T, c context.Context, a store.Store, _, _ leakWorld) {
		a.SweepAttachments(c, time.Now().Add(time.Hour))
	},
	"SaveDeskTurnLog": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		refused(t, "SaveDeskTurnLog", a.SaveDeskTurnLog(c, store.DeskTurnLog{WorkspaceID: b.ws, Turn: 1, StartedBy: "intruder"}), store.ErrNotFound)
	},
	"AddGateResult": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.AddGateResult(c, store.GateResult{LaneID: b.lane, SHA: "x", Gate: "g"})
		refused(t, "AddGateResult", err, store.ErrNotFound)
	},
	"UpsertArtifact": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, _, err := a.UpsertArtifact(c, store.Artifact{LaneID: b.lane, Path: b.artPath, Title: "overwritten"})
		refused(t, "UpsertArtifact", err, store.ErrNotFound)
	},
	"AddArtifactVersion": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.AddArtifactVersion(c, store.ArtifactVersion{ArtifactID: b.art, SHA256: "intruder"})
		refused(t, "AddArtifactVersion", err, store.ErrNotFound)
	},
	"LinkArtifact": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		refused(t, "LinkArtifact", a.LinkArtifact(c, store.ArtifactLink{ArtifactID: b.art, Kind: "run", Ref: "1"}), store.ErrNotFound)
	},
	"SetArtifactIntent": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		refused(t, "SetArtifactIntent", a.SetArtifactIntent(c, b.art, store.ArtifactUndecided, store.IntentByPerson), store.ErrNotFound)
	},
	"SetArtifactPinned": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		refused(t, "SetArtifactPinned", a.SetArtifactPinned(c, b.art, true), store.ErrNotFound)
	},
	"SetArtifactState": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		refused(t, "SetArtifactState", a.SetArtifactState(c, b.art, "closed", true), store.ErrNotFound)
	},
	"SetArtifactTitle": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		refused(t, "SetArtifactTitle", a.SetArtifactTitle(c, b.art, "overwritten"), store.ErrNotFound)
	},
	"RenameArtifact": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		refused(t, "RenameArtifact", a.RenameArtifact(c, b.art, "docs/moved.md"), store.ErrNotFound)
	},
	"MarkVersionSnapshotted": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		a.MarkVersionSnapshotted(c, b.art, b.verSHA)
	},
	"PutLaneForge": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		refused(t, "PutLaneForge", a.PutLaneForge(c, store.LaneForge{LaneID: b.lane, MR: 999}), store.ErrNotFound)
	},
	"AddMuster": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, _, err := a.AddMuster(c, store.MusterItem{Key: b.musterKey, Kind: "lane.stale", SubjectKind: "lane"})
		refused(t, "AddMuster", err, store.ErrConflict)
	},
	"UpdateMuster": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		state := "dismissed"
		_, err := a.UpdateMuster(c, b.muster, store.MusterPatch{State: &state})
		refused(t, "UpdateMuster", err, store.ErrNotFound)
	},
	"ApproveCheckFiles": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.ApproveCheckFiles(c, []store.CheckFile{{RepoID: b.repo, Path: "intruder", Blob: "b", BaseCommit: "c", ApprovedBy: "x"}})
		refused(t, "ApproveCheckFiles", err, store.ErrNotFound)
	},
	"ReleaseCheckFiles": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.ReleaseCheckFiles(c, b.repo, []string{b.checkPath}, "intruder")
		refused(t, "ReleaseCheckFiles", err, store.ErrNotFound)
	},
}

// leakRefProbes write another tenant's id into each reference column a tenant's view
// checks (tableTenancy's refs), through tenant 1's view and its own parent rows, and
// expect not found; TestTenantsSeeNothingOfEachOther then checks tenant 2's reads did not
// change. TestLeakSuiteCoversEveryRef fails on a declared ref with no probe here.
var leakRefProbes = map[string]func(*testing.T, context.Context, store.Store, leakWorld, leakWorld){
	"spend.ref": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		refused(t, "AddSpend ref", a.AddSpend(c, store.Spend{Day: leakDay, Source: "intruder", Ref: b.run, USD: 9}), store.ErrNotFound)
	},
	"conversations.repo_id": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		refused(t, "PutConversation repo_id", a.PutConversation(c, store.Conversation{ID: "conv-intruder", Agent: "claude", RepoID: b.repo}), store.ErrNotFound)
	},
	"usage.run_id": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		refused(t, "AddUsage run_id", a.AddUsage(c, store.Usage{Day: leakDay, RunID: b.run, Agent: "claude", Requests: 1}), store.ErrNotFound)
	},
	"usage.workspace_id": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		refused(t, "AddUsage workspace_id", a.AddUsage(c, store.Usage{Day: leakDay, WorkspaceID: b.ws, Agent: "desk", Requests: 1}), store.ErrNotFound)
	},
	"next_items.lane_id": func(t *testing.T, c context.Context, a store.Store, b, w leakWorld) {
		_, err := a.AddNextItem(c, store.NextItem{Title: "intruder", LaneID: b.lane})
		refused(t, "AddNextItem lane_id", err, store.ErrNotFound)
		_, err = a.UpdateNextItem(c, w.next, store.NextPatch{LaneID: &b.lane})
		refused(t, "UpdateNextItem lane_id", err, store.ErrNotFound)
	},
	"topics.merged_into": func(t *testing.T, c context.Context, a store.Store, b, w leakWorld) {
		_, err := a.MergeTopics(c, w.topic, b.topic)
		refused(t, "MergeTopics into", err, store.ErrNotFound)
	},
	"crook_verdicts.run_id": func(t *testing.T, c context.Context, a store.Store, b, _ leakWorld) {
		_, err := a.AddCrookVerdict(c, store.CrookVerdict{Action: "lane_open", Kind: "proceed", Rule: "intruder", Run: b.run})
		refused(t, "AddCrookVerdict run_id", err, store.ErrNotFound)
	},
	"lanes.origin_run": func(t *testing.T, c context.Context, a store.Store, b, w leakWorld) {
		_, err := a.CreateLane(c, store.Lane{RepoID: w.repo, Name: "intruder", Branch: "intruder", Worktree: "/wt-intruder", State: store.LaneOpen, Origin: store.Origin{Run: b.run}})
		refused(t, "CreateLane origin_run", err, store.ErrNotFound)
	},
	"runs.parent": func(t *testing.T, c context.Context, a store.Store, b, w leakWorld) {
		_, err := a.CreateRun(c, store.Run{LaneID: w.lane, Agent: "claude", State: store.RunRunning, Parent: b.run})
		refused(t, "CreateRun parent", err, store.ErrNotFound)
	},
	"decisions.answer_run": func(t *testing.T, c context.Context, a store.Store, b, w leakWorld) {
		refused(t, "SetDecisionRun answer_run", a.SetDecisionRun(c, w.dec, b.run), store.ErrNotFound)
	},
	"requests.target_run": func(t *testing.T, c context.Context, a store.Store, b, w leakWorld) {
		q, err := a.Request(c, w.req)
		if err != nil {
			t.Fatal(err)
		}
		q.TargetRun = b.run
		refused(t, "UpdateRequest target_run", a.UpdateRequest(c, q), store.ErrNotFound)
	},
	"requests.reply_run": func(t *testing.T, c context.Context, a store.Store, b, w leakWorld) {
		q, err := a.Request(c, w.req)
		if err != nil {
			t.Fatal(err)
		}
		q.ReplyRun = b.run
		refused(t, "UpdateRequest reply_run", a.UpdateRequest(c, q), store.ErrNotFound)
	},
	"artifacts.workspace_id": func(t *testing.T, c context.Context, a store.Store, b, w leakWorld) {
		_, _, err := a.UpsertArtifact(c, store.Artifact{WorkspaceID: b.ws, RepoID: w.repo, LaneID: w.lane, Path: "docs/intruder-ws.md"})
		refused(t, "UpsertArtifact workspace_id", err, store.ErrNotFound)
	},
	"artifacts.repo_id": func(t *testing.T, c context.Context, a store.Store, b, w leakWorld) {
		_, _, err := a.UpsertArtifact(c, store.Artifact{WorkspaceID: w.ws, RepoID: b.repo, LaneID: w.lane, Path: "docs/intruder-repo.md"})
		refused(t, "UpsertArtifact repo_id", err, store.ErrNotFound)
	},
	"artifacts.run_id": func(t *testing.T, c context.Context, a store.Store, b, w leakWorld) {
		_, _, err := a.UpsertArtifact(c, store.Artifact{WorkspaceID: w.ws, RepoID: w.repo, LaneID: w.lane, RunID: b.run, Path: "docs/intruder-run.md"})
		refused(t, "UpsertArtifact run_id", err, store.ErrNotFound)
	},
	"artifact_versions.run_id": func(t *testing.T, c context.Context, a store.Store, b, w leakWorld) {
		_, err := a.AddArtifactVersion(c, store.ArtifactVersion{ArtifactID: w.art, SHA256: "intruder", Size: 1, RunID: b.run})
		refused(t, "AddArtifactVersion run_id", err, store.ErrNotFound)
	},
	"reservations.lane_id": func(t *testing.T, c context.Context, a store.Store, b, w leakWorld) {
		_, err := a.CreateReservation(c, store.Reservation{RepoID: w.repo, LaneID: b.lane, Tag: "v-intruder", State: store.TagReserved})
		refused(t, "CreateReservation lane_id", err, store.ErrNotFound)
	},
	"check_files.item_id": func(t *testing.T, c context.Context, a store.Store, b, w leakWorld) {
		_, err := a.ApproveCheckFiles(c, []store.CheckFile{{RepoID: w.repo, Path: "check-intruder", Blob: "b", BaseCommit: "c", ApprovedBy: "x", ItemID: b.next}})
		refused(t, "ApproveCheckFiles item_id", err, store.ErrNotFound)
	},
	"desk_questions.topic_id": func(t *testing.T, c context.Context, a store.Store, b, w leakWorld) {
		_, err := a.AddQuestion(c, store.Question{WorkspaceID: w.ws, Text: "intruder", TopicID: b.topic})
		refused(t, "AddQuestion topic_id", err, store.ErrNotFound)
	},
	"desk_questions.lane_id": func(t *testing.T, c context.Context, a store.Store, b, w leakWorld) {
		_, err := a.AddQuestion(c, store.Question{WorkspaceID: w.ws, Text: "intruder", LaneID: b.lane})
		refused(t, "AddQuestion lane_id", err, store.ErrNotFound)
	},
	"desk_questions.next_id": func(t *testing.T, c context.Context, a store.Store, b, w leakWorld) {
		_, err := a.AddQuestion(c, store.Question{WorkspaceID: w.ws, Text: "intruder", NextID: b.next})
		refused(t, "AddQuestion next_id", err, store.ErrNotFound)
	},
}

// Every reference a tenant's view checks has a probe.
func TestLeakSuiteCoversEveryRef(t *testing.T) {
	for table, ten := range tableTenancy {
		for _, r := range ten.refs {
			if _, ok := leakRefProbes[table+"."+r.column]; !ok {
				t.Errorf("declared ref %s.%s has no probe in leakRefProbes", table, r.column)
			}
		}
	}
	for k := range leakRefProbes {
		table, col, _ := strings.Cut(k, ".")
		found := false
		for _, r := range tableTenancy[table].refs {
			found = found || r.column == col
		}
		if !found {
			t.Errorf("leakRefProbes has %s, which is not a declared ref", k)
		}
	}
}

func TestTenantsSeeNothingOfEachOther(t *testing.T) {
	ctx := context.Background()
	db, _ := open(t)
	if _, err := db.db.ExecContext(ctx, `INSERT INTO tenants (id, slug, name, created) VALUES (2, 'other', 'Other', '2026-11-27T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	a, b := db.ForTenant(1), db.ForTenant(2)
	if a.Tenant() != 1 || b.Tenant() != 2 || db.Tenant() != 0 {
		t.Fatalf("Tenant() = %d, %d, %d", a.Tenant(), b.Tenant(), db.Tenant())
	}

	wa := seedTenant(t, a, "a")
	readsA := readAll(ctx, a, wa)
	wb := seedTenant(t, b, "b")
	// Tenant 2's rows are not in any of tenant 1's reads: lists, counts and totals are
	// what they were before tenant 2 had any.
	diffReads(t, "tenant 1 after tenant 2 was seeded", readsA, readAll(ctx, a, wa))
	// And each tenant's reads see its own rows.
	for name, r := range readsA {
		if r.Err != "" && name != "PendingBrief" {
			t.Errorf("tenant 1's own %s: %s", name, r.Err)
		}
	}

	readsB := readAll(ctx, b, wb)
	names := make([]string, 0, len(leakProbes))
	for n := range leakProbes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		t.Run(n, func(t *testing.T) { leakProbes[n](t, ctx, a, wb, wa) })
	}
	refNames := make([]string, 0, len(leakRefProbes))
	for n := range leakRefProbes {
		refNames = append(refNames, n)
	}
	sort.Strings(refNames)
	for _, n := range refNames {
		t.Run("ref "+n, func(t *testing.T) { leakRefProbes[n](t, ctx, a, wb, wa) })
	}
	diffReads(t, "tenant 2 after tenant 1's view was given its ids", readsB, readAll(ctx, b, wb))

	// The unscoped store still sees both tenants.
	all, err := db.Workspaces(ctx)
	if err != nil || len(all) < 2 {
		t.Fatalf("unscoped workspaces = %+v, %v", all, err)
	}
}

// Every method of store.Store is checked by the leak suite or named as the deployment's.
func TestLeakSuiteCoversEveryMethod(t *testing.T) {
	st := reflect.TypeOf((*store.Store)(nil)).Elem()
	var missing []string
	for i := range st.NumMethod() {
		n := st.Method(i).Name
		_, r := leakReads[n]
		_, p := leakProbes[n]
		_, x := leakExempt[n]
		if !r && !p && !x {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("store.Store methods with no leak check: %v; add a read to leakReads, a probe to leakProbes, or a reason to leakExempt", missing)
	}
	for n := range leakExempt {
		if _, ok := st.MethodByName(n); !ok {
			t.Errorf("leakExempt names %s, which store.Store does not have", n)
		}
	}
}

func TestTenantViewClosesNothing(t *testing.T) {
	db, _ := open(t)
	if err := db.ForTenant(1).Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Workspaces(context.Background()); err != nil {
		t.Fatalf("the store after a view closed: %v", err)
	}
}
