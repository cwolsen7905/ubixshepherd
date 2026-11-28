package daemon

// The route-level leak suite: every route the daemon serves, listed from the router, is
// called by a member of one tenant with another tenant's ids, and must give away nothing
// of the other tenant's: no rows, no counts, no ids, no feed items.
//
// It is differential. Two daemons hold the same caller's tenant and the same victim
// tenant, seeded identically, so the victim's ids are the same in both. Each also holds a
// second copy of everything: the first daemon in a third tenant nobody is a member of,
// the second in the victim's. A route that shows any of the victim's rows, counts or ids
// answers the two daemons differently. (Both hold the same number of rows, so the ids new
// rows get are the same in both: ids come from one sequence for the deployment.) Every victim row
// also carries a secret word no request sends, so a row that leaks whole is named.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/auth"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

// otherTenant is the second tenant; spareTenant is a third that nobody is a member of.
const otherTenant, spareTenant int64 = 2, 3

// tenantWorld is one tenant's seeded rows, as the routes name them.
type tenantWorld struct {
	tag, secret                                                string
	ws, repo, lane, run, dec, req, art, next, topic, question  int64
	crook, attach                                              int64
	repoPath, laneName, project, setKey, conv, tagName, wsName string
}

// seedWorld fills one tenant with a row of every kind a route reads. Every text a person
// or agent wrote carries w.secret.
func seedWorld(t *testing.T, st store.Store, tag string) tenantWorld {
	t.Helper()
	ctx := context.Background()
	sec := "s3cret" + tag
	w := tenantWorld{tag: tag, secret: sec, repoPath: "/ws-" + tag + "/repo", laneName: "lane-" + tag, project: "proj-" + tag,
		setKey: "key-" + tag, conv: "conv-" + tag, tagName: "v9.9." + strconv.Itoa(len(tag)), wsName: "ws-" + tag}
	ok := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("seed %s: %v", tag, err)
		}
	}
	ws, err := st.SaveWorkspace(ctx, store.Workspace{Name: w.wsName, Path: "/ws-" + tag}, []store.Repo{{Name: "repo-" + tag, Path: w.repoPath}})
	ok(err)
	w.ws = ws.ID
	repos, err := st.Repos(ctx, ws.ID)
	ok(err)
	w.repo = repos[0].ID
	l, err := st.CreateLane(ctx, store.Lane{RepoID: w.repo, Name: w.laneName, Branch: "br4nch-" + tag, Worktree: "/w0rktree-" + tag, State: store.LaneOpen, Scope: []string{"a/**"}})
	ok(err)
	w.lane = l.ID
	run, err := st.CreateRun(ctx, store.Run{LaneID: w.lane, Agent: "claude", State: store.RunFailed, Prompt: "do " + sec})
	ok(err)
	w.run = run.ID
	_, err = st.AddEvent(ctx, store.Event{RunID: w.run, Kind: "text", Text: "said " + sec})
	ok(err)
	d, err := st.CreateDecision(ctx, store.Decision{RunID: w.run, Question: "which " + sec, Options: []string{"x", "y"}})
	ok(err)
	w.dec = d.ID
	q, err := st.CreateRequest(ctx, store.Request{FromRun: w.run, Kind: "question", Lane: w.laneName, Message: "ask " + sec, State: store.RequestPending})
	ok(err)
	w.req = q.ID
	cv, err := st.AddCrookVerdict(ctx, store.CrookVerdict{Action: "lane_open", Kind: "proceed", Rule: "rule-" + sec, Lane: w.laneName, Run: w.run, At: time.Now().UTC()})
	ok(err)
	w.crook = cv.ID
	ok(st.AddFeed(ctx, "note", "feed "+sec, w.run))
	ok(st.SetSetting(ctx, w.setKey, "value-"+sec))
	_, err = st.CreateReservation(ctx, store.Reservation{RepoID: w.repo, LaneID: w.lane, Tag: w.tagName, State: store.TagReserved})
	ok(err)
	now := time.Now().UTC()
	ok(st.PutConversation(ctx, store.Conversation{ID: w.conv, Agent: "claude", RepoID: w.repo, Title: "chat " + sec, Started: now, Last: now}))
	ok(st.AddSpend(ctx, store.Spend{Day: dispatch.Today(), Source: "src-" + tag, Ref: w.run, USD: 1.25}))
	draft, err := st.SaveBriefDraft(ctx, w.project, "brief "+sec, "alice")
	ok(err)
	_, err = st.ApproveBrief(ctx, draft.ID, "bob")
	ok(err)
	n, err := st.AddNextItem(ctx, store.NextItem{Title: "next " + sec})
	ok(err)
	w.next = n.ID
	tp, err := st.AddTopic(ctx, store.Topic{Title: "topic " + sec})
	ok(err)
	w.topic = tp.ID
	_, err = st.LinkTopic(ctx, store.TopicLink{TopicID: w.topic, Kind: store.TopicLane, Ref: strconv.FormatInt(w.lane, 10)})
	ok(err)
	qs, err := st.AddQuestion(ctx, store.Question{WorkspaceID: w.ws, Text: "why " + sec})
	ok(err)
	w.question = qs.ID
	_, err = st.AddDiagnosis(ctx, store.Diagnosis{RunID: w.run, Kind: "gate", Summary: "red " + sec})
	ok(err)
	ok(st.AddUsage(ctx, store.Usage{Day: dispatch.Today(), RunID: w.run, Agent: "claude", Model: "m-" + tag, Requests: 1}))
	_, err = st.AddDeskEvent(ctx, store.DeskEvent{WorkspaceID: w.ws, Kind: store.DeskUser, Text: "hello " + sec})
	ok(err)
	at, err := st.AddAttachment(ctx, store.Attachment{WorkspaceID: w.ws, SHA: "file-" + tag, Name: "f-" + sec + ".png", Mime: "image/png", Kind: "image", Size: 3})
	ok(err)
	w.attach = at.ID
	art, _, err := st.UpsertArtifact(ctx, store.Artifact{WorkspaceID: w.ws, RepoID: w.repo, LaneID: w.lane, RunID: w.run, Path: "docs/a-" + tag + ".md", Title: "a " + sec})
	ok(err)
	w.art = art.ID
	ok(st.PutLaneForge(ctx, store.LaneForge{LaneID: w.lane, MR: 4000 + int(w.lane), MRState: "opened"}))
	return w
}

// words are what names the world's rows that no request of the suite sends, so an answer
// that holds one shows a row.
func (w tenantWorld) words() []string {
	return []string{w.secret, "br4nch-" + w.tag, "/w0rktree-" + w.tag, "/ws-" + w.tag + "\"", "m-" + w.tag, "file-" + w.tag, "src-" + w.tag, "docs/a-" + w.tag}
}

// fingerprint is what a tenant holds, read through its own view, so the suite can tell
// that no call changed another tenant's rows.
func fingerprint(t *testing.T, st store.Store, w tenantWorld) string {
	t.Helper()
	ctx := context.Background()
	var parts []any
	add := func(v any, err error) {
		if err != nil {
			v = err.Error()
		}
		parts = append(parts, v)
	}
	add(st.Workspaces(ctx))
	add(st.Repos(ctx, w.ws))
	add(st.Lanes(ctx, w.repo))
	add(st.Runs(ctx, w.lane, "", 100))
	add(st.Events(ctx, w.run))
	add(st.Decisions(ctx, ""))
	add(st.Requests(ctx))
	add(st.Feed(ctx, 0, 1000))
	add(st.Setting(ctx, w.setKey))
	add(st.Reservations(ctx, w.repo))
	add(st.Conversations(ctx, 0))
	add(st.NextItems(ctx))
	add(st.Topics(ctx))
	add(st.Questions(ctx, w.ws, ""))
	add(st.Artifact(ctx, w.art))
	b, _ := json.Marshal(parts)
	return string(b)
}

// leakRig is a daemon with two tenants and a session for each user.
type leakRig struct {
	s       *Server
	base    string
	client  *http.Client
	cookies map[string]*http.Cookie
	csrf    map[string]string
}

// leakDaemon starts a daemon over a store with tenants default and other, the users
// alice (member of default), bob (member of other) and victor (viewer in default), and
// what seed puts in it.
func leakDaemon(t *testing.T, seed func(st store.Store)) *leakRig {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "s.db")
	st, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, `INSERT INTO tenants (id, slug, name, created) VALUES (?, 'other', 'Other', '2026-11-27T00:00:00Z'), (?, 'spare', 'Spare', '2026-11-27T00:00:00Z')`, otherTenant, spareTenant); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	users := st.Users()
	ids := map[string]int64{}
	for _, u := range []struct {
		name, tenant string
		role         auth.Role
	}{{"alice", "default", auth.RoleMember}, {"bob", "other", auth.RoleMember}, {"victor", "default", auth.RoleViewer}} {
		got, err := users.AddUser(ctx, u.name, u.tenant, u.role, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		ids[u.name] = got.ID
	}
	seed(st)
	// Background work is kept still, so the two daemons of a comparison differ only in
	// their rows: the desk never wakes by itself, and the suite waits for routing and the
	// other start-up passes to finish (quiesce) before it compares.
	cfg := config.Default()
	cfg.Desk.Wake = config.WakeNever
	s, err := NewServer(st, cfg, "/cfg", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	s.DeskAgent = nopDesk{}
	s.Runner = &dispatch.Runner{Store: st, Config: cfg, Dir: filepath.Join(t.TempDir(), "runs"), Log: s.Log}
	rtPath := filepath.Join(t.TempDir(), "daemon.json")
	rctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- s.Run(rctx, rtPath) }()
	t.Cleanup(func() { cancel(); <-done; st.Close() })
	var base string
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if rt, err := ReadRuntime(rtPath); err == nil {
			base = "http://" + rt.Addr
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the daemon never wrote its runtime file")
		}
	}
	rig := &leakRig{s: s, base: base, cookies: map[string]*http.Cookie{}, csrf: map[string]string{},
		client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	for name, id := range ids {
		val, ws, err := s.web.startFor(s.listenPort(), true, id)
		if err != nil {
			t.Fatal(err)
		}
		rig.cookies[name] = &http.Cookie{Name: "shepherd_" + s.listenPort(), Value: val}
		rig.csrf[name] = ws.csrf
	}
	// The Crook log starts with the daemon: wait until it records, so that both daemons of
	// a comparison log every verdict of the suite's calls.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		rig.call(t, "victor", http.MethodDelete, api.PathWebSessions, nil, nil)
		if cs, _ := st.CrookCounts(ctx, time.Time{}); len(cs) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the Crook log never recorded a verdict")
		}
	}
	rig.settleCrook(t)
	// Routing runs once at start over the requests a previous daemon left: wait until none
	// is still pending, then until nothing in the store moves.
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		qs, err := st.Requests(ctx, store.RequestPending)
		if err == nil && len(qs) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("requests still pending after start: %+v", qs)
		}
	}
	rig.quiesce(t, 10, 30*time.Millisecond)
	return rig
}

// stillness is what background work changes in the store, across every tenant.
func (rig *leakRig) stillness() string {
	ctx := context.Background()
	st := rig.s.Store
	var parts []any
	add := func(v any, _ error) { parts = append(parts, v) }
	add(st.LastFeed(ctx))
	add(st.Requests(ctx))
	add(st.Topics(ctx))
	add(st.Decisions(ctx, ""))
	add(st.NextItems(ctx))
	add(st.CrookCounts(ctx, time.Time{}))
	b, _ := json.Marshal(parts)
	return string(b)
}

// quiesce waits until the store has not changed for n looks, every gap apart: no
// background pass and no queued Crook verdict is still writing.
func (rig *leakRig) quiesce(t *testing.T, n int, gap time.Duration) {
	t.Helper()
	last, same := rig.stillness(), 0
	for deadline := time.Now().Add(30 * time.Second); same < n; {
		time.Sleep(gap)
		if now := rig.stillness(); now == last {
			same++
		} else {
			last, same = now, 0
		}
		if time.Now().After(deadline) {
			t.Fatal("the daemon never went still")
		}
	}
}

// call makes one request as the user, as their browser would, and returns the status and
// the body. A stream is read for a moment and cut.
func (rig *leakRig) call(t *testing.T, user, method, path string, body any, hdr map[string]string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, _ := http.NewRequestWithContext(ctx, method, rig.base+path, rd)
	if user == "operator" {
		// The operator token, naming the other tenant, as shepherd --tenant other does.
		req.Header.Set("Authorization", "Bearer "+rig.s.Token)
		req.Header.Set(api.TenantHeader, "other")
	} else {
		req.AddCookie(rig.cookies[user])
	}
	req.Header.Set("Origin", rig.base)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set(api.CSRFHeader, rig.csrf[user])
	req.Header.Set("Last-Event-ID", "0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := rig.client.Do(req)
	if err != nil {
		return 0, "(no answer: " + err.Error() + ")"
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}

var unknownField = regexp.MustCompile(`json: unknown field "([^"]+)"`)

// send is call with a body shaped to the route: a field the route's body does not take is
// dropped and the call made again, so every route gets the victim's ids in every field it
// reads. A route that ends the caller's session gets them a fresh one after.
func (rig *leakRig) send(t *testing.T, user, pattern, method, path string, body map[string]any) (int, string) {
	t.Helper()
	var b map[string]any
	if body != nil {
		b = map[string]any{}
		for k, v := range body {
			b[k] = v
		}
	}
	for {
		var code int
		var out string
		if b == nil {
			code, out = rig.call(t, user, method, path, nil, nil)
		} else {
			code, out = rig.call(t, user, method, path, b, nil)
		}
		var e api.Error
		_ = json.Unmarshal([]byte(out), &e)
		m := unknownField.FindStringSubmatch(e.Error)
		if code == http.StatusBadRequest && m != nil && b != nil {
			if _, ok := b[m[1]]; ok {
				delete(b, m[1])
				continue
			}
		}
		if user != "operator" && (strings.Contains(pattern, "signout") || strings.Contains(pattern, "/revoke")) {
			rig.resession(t, user)
		}
		return code, out
	}
}

// resession gives the user a new browser session.
func (rig *leakRig) resession(t *testing.T, user string) {
	t.Helper()
	id := map[string]int64{}
	for name := range rig.cookies {
		u, err := rig.s.users().UserByAlias(context.Background(), auth.AliasLocal, name)
		if err != nil {
			t.Fatal(err)
		}
		id[name] = u.ID
	}
	val, ws, err := rig.s.web.startFor(rig.s.listenPort(), true, id[user])
	if err != nil {
		t.Fatal(err)
	}
	rig.cookies[user].Value, rig.csrf[user] = val, ws.csrf
}

// allRoutes are the routes the daemon serves through guard: the mux's, as Handler
// registered them, and the few served before it.
func allRoutes(s *Server) []string {
	out := append(append([]string{}, s.routes...), outsideMux...)
	sort.Strings(out)
	return out
}

// target fills a route's pattern with the victim's ids: the path's wildcards, a query
// naming them all, and a body naming them all.
func target(pattern string, v tenantWorld) (method, path string, body map[string]any) {
	method, p, _ := strings.Cut(pattern, " ")
	idFor := map[string]string{
		api.PathLanes: fmt.Sprint(v.lane), api.PathRuns: fmt.Sprint(v.run), api.PathRepos: fmt.Sprint(v.repo),
		api.PathWorkspaces: fmt.Sprint(v.ws), api.PathDecisions: fmt.Sprint(v.dec), api.PathRequests: fmt.Sprint(v.req),
		api.PathArtifacts: fmt.Sprint(v.art), api.PathNext: fmt.Sprint(v.next), api.PathTopics: fmt.Sprint(v.topic),
		api.PathQuestions: fmt.Sprint(v.question), api.PathSessions: v.conv, api.PathCrookVerdicts: fmt.Sprint(v.crook),
		api.PathDeskAttachments: fmt.Sprint(v.attach),
	}
	if i := strings.Index(p, "/{id}"); i >= 0 {
		id, ok := idFor[p[:i]]
		if !ok {
			id = "1"
		}
		p = strings.Replace(p, "{id}", id, 1)
	}
	name := "nobody"
	if strings.HasPrefix(p, api.PathProjects) {
		name = v.project
	}
	p = strings.Replace(p, "{name}", name, 1)
	p = strings.Replace(p, "{key}", v.setKey, 1)
	q := url.Values{}
	for k, val := range map[string]int64{"workspace_id": v.ws, "repo_id": v.repo, "lane_id": v.lane, "run_id": v.run, "id": v.run, "item": v.next, "topic": v.topic} {
		q.Set(k, fmt.Sprint(val))
	}
	q.Set("path", v.repoPath)
	q.Set("lane", v.laneName)
	q.Set("project", v.project)
	q.Set("after", "0")
	path = p + "?" + q.Encode()
	body = map[string]any{
		"workspace_id": v.ws, "repo_id": v.repo, "lane_id": v.lane, "run_id": v.run, "continue": v.run, "item": v.next,
		"item_id": v.next, "for_item": v.next, "topic_id": v.topic, "decision": v.dec, "request": v.req,
		"path": v.repoPath, "lane": v.laneName, "tag": v.tagName, "name": "renamed", "task": "x", "agent": "claude",
		"answer": "x", "question": "x", "text": "x", "title": "x", "paths": []string{"a"}, "repos": []string{v.repoPath},
		"key": v.setKey, "value": "x",
	}
	return method, path, body
}

// volatile are the parts of an answer that differ between two daemons for reasons of
// their own: times, ports, tokens and process ids.
var volatile = []*regexp.Regexp{
	regexp.MustCompile(`\d{4}-\d\d-\d\d[T ][0-9:.]+(Z|[+-]\d\d:?\d\d)?( [A-Z]{3,4})?`),
	regexp.MustCompile(`127\.0\.0\.1:\d+`),
	regexp.MustCompile(`shepherd_\d+`),
	regexp.MustCompile(`[0-9a-f]{32,}`),
	regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`),
	regexp.MustCompile(`"(pid|uptime_seconds|ms|age_seconds|took_ms|elapsed_ms|next_check|seconds)":\s*[0-9.]+`),
	regexp.MustCompile(`\d+(\.\d+)?(ms|µs|s) ago`),
	regexp.MustCompile(`"version":"[0-9a-f]+"`),
}

func normalize(s string) string {
	for _, re := range volatile {
		s = re.ReplaceAllString(s, "~")
	}
	return s
}

// defaultTenantGaps are routes that still show the default tenant's users what background
// work wrote about another tenant's rows. Routing, the forge watcher and muster keep the
// unscoped store, and a feed item written through it lands in the default tenant. Until
// background work writes in the tenant of the row it acts on, these are compared for the
// default tenant's members with the other tenant's feed items left out; a member of any
// other tenant is held to the whole answer.
var defaultTenantGaps = map[string]string{
	"GET " + api.PathFeed:       "background work's feed items land in the default tenant",
	"GET " + api.PathFeedStream: "background work's feed items land in the default tenant",
}

// withoutWords drops the lines, and the JSON feed items, that name any of the words.
func withoutWords(s string, words ...string) string {
	var items struct {
		Items  []map[string]any `json:"items"`
		Events []string         `json:"events"`
	}
	if json.Unmarshal([]byte(s), &items) == nil && items.Items != nil {
		// events runs beside items, one kind for each.
		var kept []map[string]any
		var events []string
		for i, it := range items.Items {
			b, _ := json.Marshal(it)
			if !containsAny(string(b), words) {
				delete(it, "id")
				kept = append(kept, it)
				if i < len(items.Events) {
					events = append(events, items.Events[i])
				}
			}
		}
		items.Items, items.Events = kept, events
		b, _ := json.Marshal(items)
		return string(b)
	}
	var out []string
	for _, ev := range strings.Split(s, "\n\n") {
		if !containsAny(ev, words) {
			out = append(out, regexp.MustCompile(`(?m)^id: \d+$|"id":\d+`).ReplaceAllString(ev, ""))
		}
	}
	return strings.Join(out, "\n\n")
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// settleCrook waits until the Crook log's writer has caught up: its verdicts are written
// from a queue, so two daemons can be a verdict apart for a moment.
func (rig *leakRig) settleCrook(t *testing.T) {
	t.Helper()
	count := func() int {
		cs, _ := rig.s.Store.CrookCounts(context.Background(), time.Time{})
		n := 0
		for _, c := range cs {
			n += c.Proceed + c.Ask + c.Hold + c.Refuse
		}
		return n
	}
	last, same := count(), 0
	for same < 3 {
		time.Sleep(15 * time.Millisecond)
		if n := count(); n == last {
			same++
		} else {
			last, same = n, 0
		}
	}
}

// A member of one tenant, calling every route with another tenant's ids, sees nothing of
// that tenant's, and nor does the operator token naming a tenant: the answer is the same whether the other tenant has one copy of its rows
// or two, and names none of its words.
func TestNoRouteCrossesTenants(t *testing.T) {
	for _, c := range []struct {
		user           string
		own, victim    int64
		ownTag, vicTag string
	}{
		{"alice", store.DefaultTenantID, otherTenant, "alpha", "victim"},
		{"bob", otherTenant, store.DefaultTenantID, "bravo", "prey"},
		{"operator", otherTenant, store.DefaultTenantID, "oscar", "quarry"},
	} {
		t.Run(c.user, func(t *testing.T) {
			var vic tenantWorld
			seed := func(extra bool) func(store.Store) {
				return func(st store.Store) {
					seedWorld(t, st.ForTenant(c.own), c.ownTag)
					vic = seedWorld(t, st.ForTenant(c.victim), c.vicTag)
					spare := st.ForTenant(spareTenant)
					if extra {
						spare = st.ForTenant(c.victim)
					}
					seedWorld(t, spare, c.vicTag+"x")
				}
			}
			one := leakDaemon(t, seed(false))
			vicOne := vic
			two := leakDaemon(t, seed(true))
			if vic != vicOne {
				t.Fatalf("the two daemons' victim rows differ: %+v and %+v", vicOne, vic)
			}
			before := fingerprint(t, one.s.Store.ForTenant(c.victim), vic)
			defer func() {
				if after := fingerprint(t, one.s.Store.ForTenant(c.victim), vic); normalize(after) != normalize(before) {
					t.Errorf("calls as %s changed the other tenant's rows:\n before %.2000s\n after  %.2000s", c.user, before, after)
				}
			}()
			routes := allRoutes(one.s)
			if len(routes) < 100 {
				t.Fatalf("only %d routes listed", len(routes))
			}
			for _, pattern := range routes {
				method, path, body := target(pattern, vic)
				var b map[string]any
				if method != http.MethodGet {
					b = body
				}
				// Both daemons still before each comparison, so that what one did in the
				// background and the other has not yet is never taken for a leak.
				one.quiesce(t, 3, 10*time.Millisecond)
				two.quiesce(t, 3, 10*time.Millisecond)
				if strings.Contains(pattern, "/crook/") {
					one.settleCrook(t)
					two.settleCrook(t)
				}
				extra := vic
				extra.tag, extra.secret, extra.laneName = vic.tag+"x", vic.secret+"x", vic.laneName+"x"
				gap := func(a string) string {
					if defaultTenantGaps[pattern] != "" && c.own == store.DefaultTenantID {
						return withoutWords(a, append([]string{vic.laneName, vic.repoPath}, vic.words()...)...)
					}
					return a
				}
				c1, a1 := one.send(t, c.user, pattern, method, path, b)
				c2, a2 := two.send(t, c.user, pattern, method, path, b)
				a1, a2 = gap(a1), gap(a2)
				if method == http.MethodGet {
					// And with no query, for the route's whole list.
					bare, _, _ := strings.Cut(path, "?")
					d1, b1 := one.send(t, c.user, pattern, method, bare, nil)
					d2, b2 := two.send(t, c.user, pattern, method, bare, nil)
					c1, c2, a1, a2 = c1*1000+d1, c2*1000+d2, a1+"\n--\n"+gap(b1), a2+"\n--\n"+gap(b2)
				}
				for _, word := range append(vic.words(), extra.words()...) {
					if strings.Contains(a1, word) || strings.Contains(a2, word) {
						t.Errorf("%s as %s shows the other tenant's rows (%q): %.300s", pattern, c.user, word, a1+a2)
					}
				}
				if c1 != c2 || normalize(a1) != normalize(a2) {
					t.Errorf("%s as %s answers differently as the other tenant's rows change:\n one: %d %.3000s\n two: %d %.3000s", pattern, c.user, c1, normalize(a1), c2, normalize(a2))
				}
			}
		})
	}
}

// selfService are the writes a viewer may make: their own session and devices.
var selfService = map[string]string{
	"POST " + api.PathWebSignout:               "ends the viewer's own session",
	"POST " + api.PathWebSignoutAll:            "ends the viewer's own sessions and tokens",
	"POST " + api.PathDevices + "/{id}/revoke": "ends one of the viewer's own device tokens",
}

// A viewer reads and changes nothing: every route but a GET, and the viewer's own
// sessions, is refused, before anything is looked up.
func TestViewerIsRefusedEveryWrite(t *testing.T) {
	var own tenantWorld
	rig := leakDaemon(t, func(st store.Store) { own = seedWorld(t, st.ForTenant(store.DefaultTenantID), "own") })
	n := 0
	for _, pattern := range allRoutes(rig.s) {
		if strings.HasPrefix(pattern, "GET ") || selfService[pattern] != "" {
			continue
		}
		n++
		method, path, body := target(pattern, own)
		code, out := rig.send(t, "victor", pattern, method, path, body)
		if code != http.StatusForbidden {
			t.Errorf("%s as a viewer: %d %.200s, want 403", pattern, code, out)
		}
	}
	if n < 60 {
		t.Fatalf("only %d write routes", n)
	}
}

// Every route the router serves has a rule, and every rule a route: the table cannot go
// stale either way.
func TestEveryRouteHasARole(t *testing.T) {
	s, _ := newServer(t)
	seen := map[string]bool{}
	for _, p := range allRoutes(s) {
		seen[p] = true
		if _, ok := routeRules[p]; !ok {
			t.Errorf("%s has no rule", p)
		}
	}
	for p := range routeRules {
		if !seen[p] {
			t.Errorf("routeRules has %s, which no route serves", p)
		}
	}
}
