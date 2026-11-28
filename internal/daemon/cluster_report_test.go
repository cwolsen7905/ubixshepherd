package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

// The new frame and fields round-trip, and a frame as an older peer reads it still
// decodes: the fields it does not know are dropped, not an error.
func TestRepoFramesRoundTrip(t *testing.T) {
	for _, f := range []api.LinkFrame{
		{Type: api.FrameHello, DaemonID: "d1", Version: "1.2.0", APILevel: 1, Capabilities: []string{api.CapStatus, api.CapRepos},
			Repos: []api.LinkRepo{{Origin: "gitlab.example.com/g/app", Name: "app"}}, Agents: []string{"claude", "copilot"}},
		{Type: api.FrameHello, DaemonID: "c1", Version: "1.2.0", APILevel: 1, Capabilities: []string{api.CapStatus}, Tenant: "default"},
		{Type: api.FrameRepos, Repos: []api.LinkRepo{{Origin: "github.com/o/lib", Name: "lib"}}, Agents: []string{"opencode"}},
		{Type: api.FrameRepos},
	} {
		raw, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		var back api.LinkFrame
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatal(err)
		}
		again, _ := json.Marshal(back)
		if string(again) != string(raw) {
			t.Fatalf("round trip: %s, then %s", raw, again)
		}
		// An older build's frame type, without the new fields.
		var old struct {
			Type         string   `json:"type"`
			DaemonID     string   `json:"daemon_id,omitempty"`
			Capabilities []string `json:"capabilities,omitempty"`
			RunsGoing    int      `json:"runs_going,omitempty"`
		}
		if err := json.Unmarshal(raw, &old); err != nil || old.Type != f.Type {
			t.Fatalf("an older peer cannot read %s: %v", raw, err)
		}
	}
}

// saveRepos puts repos in a workspace of the node's store.
func (n *node) saveRepos(ws string, repos ...store.Repo) {
	n.t.Helper()
	root := filepath.Join(n.home, ws)
	for i := range repos {
		repos[i].Path = filepath.Join(root, repos[i].Name)
	}
	if _, err := n.st.SaveWorkspace(context.Background(), store.Workspace{Name: ws, Path: root}, repos); err != nil {
		n.t.Fatal(err)
	}
}

// fakeAgents gives the worker a runner that finds only the executables named, and
// returns how to change them while the link runs.
func fakeAgents(n *node, bins ...string) func(...string) {
	var have atomic.Pointer[[]string]
	have.Store(&bins)
	r := &dispatch.Runner{Store: n.s.Store, Config: config.Default(), Dir: filepath.Join(n.home, "runs"), Log: n.s.Log,
		OpenCode: &dispatch.OpenCodeHost{Settings: func() config.OpenCode { return config.OpenCode{} }}}
	dispatch.SetLookPath(r, func(b string) (string, error) {
		if slices.Contains(*have.Load(), filepath.Base(b)) {
			return "/fake/" + b, nil
		}
		return "", errors.New("not found")
	})
	n.s.Runner = r
	return func(b ...string) { have.Store(&b) }
}

func TestWorkerReportsReposAndAgents(t *testing.T) {
	fastLink(t)
	h := newControllerRig(t)
	m := h.worker(t, "")
	setAgents := fakeAgents(m, "claude", "copilot")
	// One origin in its SSH and HTTPS forms, in two workspaces under one name, is one
	// repo; a repo with no remote is left out.
	m.saveRepos("work", store.Repo{Name: "app", Remote: "git@GitLab.example.com:g/app.git"}, store.Repo{Name: "scratch"})
	m.saveRepos("home", store.Repo{Name: "app", Remote: "https://gitlab.example.com/g/app"})
	if got := m.join(h.addWorker("work")); got != 200 {
		t.Fatalf("join: %d", got)
	}
	stop := m.s.StartCluster(context.Background())
	defer stop()

	eventually(t, "the worker online", func() bool { mb, _ := workerOf(h.cluster(), "work"); return mb.Online })
	mb, _ := workerOf(h.cluster(), "work")
	want := []api.LinkRepo{{Origin: "gitlab.example.com/g/app", Name: "app"}}
	if !slices.Equal(mb.Repos, want) || !slices.Equal(mb.Agents, []string{"claude", "copilot"}) || mb.Tenant != "default" {
		t.Fatalf("worker as the controller sees it: repos %+v agents %v tenant %q", mb.Repos, mb.Agents, mb.Tenant)
	}
	if !slices.Contains(mb.Capabilities, api.CapRepos) {
		t.Fatalf("capabilities %v", mb.Capabilities)
	}
	if v := m.cluster().Link; v == nil || v.Tenant != "default" {
		t.Fatalf("the worker's link view: %+v", v)
	}
	if f, _ := m.s.loadLink(); f.Tenant != "default" {
		t.Fatalf("the link file did not record the tenant: %+v", f)
	}

	// A repo added and an agent installed on the worker show at the controller within
	// one status interval (with a margin for the test's scheduling).
	added := time.Now()
	m.saveRepos("work", store.Repo{Name: "lib", Remote: "ssh://git@github.com:22/o/lib.git"})
	setAgents("claude", "copilot", "opencode")
	eventually(t, "the new repo and agent at the controller", func() bool {
		mb, _ := workerOf(h.cluster(), "work")
		return len(mb.Repos) == 2 && slices.Contains(mb.Agents, "opencode")
	})
	if took := time.Since(added); took > 4*linkPingEvery+time.Second {
		t.Fatalf("the change took %v to show, over a status interval of %v", took, linkPingEvery)
	}
	mb, _ = workerOf(h.cluster(), "work")
	if mb.Repos[0] != (api.LinkRepo{Origin: "github.com/o/lib", Name: "lib"}) {
		t.Fatalf("repos %+v", mb.Repos)
	}

	// The report is live: gone when the link drops.
	stop()
	eventually(t, "offline", func() bool { mb, _ := workerOf(h.cluster(), "work"); return !mb.Online })
	if mb, _ := workerOf(h.cluster(), "work"); len(mb.Repos) != 0 || len(mb.Agents) != 0 {
		t.Fatalf("an offline worker still shows a report: %+v", mb)
	}
}

// A worker from before the report sends a hello with only the status capability and no
// repos frame: the controller links it, and lists it with no repos or agents.
func TestOlderWorkerWithoutReposStillLinks(t *testing.T) {
	fastLink(t)
	h := newControllerRig(t)
	m := h.worker(t, "")
	m.join(h.addWorker("work"))
	f, _ := m.s.loadLink()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, f.URL+api.PathClusterLink, &websocket.DialOptions{
		HTTPClient: h.client,
		HTTPHeader: http.Header{"Authorization": {"Bearer " + f.Credential}, api.DaemonIDHeader: {m.s.DaemonID()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"hello","daemon_id":"`+m.s.DaemonID()+`","version":"0.9.0","api_level":1,"capabilities":["status"]}`)); err != nil {
		t.Fatal(err)
	}
	var hello api.LinkFrame
	if err := readFrame(ctx, conn, &hello); err != nil || hello.Type != api.FrameHello || hello.Tenant != "default" {
		t.Fatalf("the controller's hello: %+v %v", hello, err)
	}
	go func() { // the old worker's reader, so pings are answered
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()
	conn.Write(ctx, websocket.MessageText, []byte(`{"type":"status","runs_going":1}`))
	eventually(t, "the old worker online with its status", func() bool {
		mb, _ := workerOf(h.cluster(), "work")
		return mb.Online && mb.RunsGoing == 1
	})
	mb, _ := workerOf(h.cluster(), "work")
	if len(mb.Repos) != 0 || len(mb.Agents) != 0 || slices.Contains(mb.Capabilities, api.CapRepos) {
		t.Fatalf("an old worker: %+v", mb)
	}
}

// tenantStore is a controller's store that keeps a tenant per worker, as a store that
// reads cluster_members.tenant_id would.
type tenantStore struct {
	*sqlite.DB
	tenant atomic.Int64
}

func (s *tenantStore) WorkerTenant(context.Context, string) (int64, error) {
	return s.tenant.Load(), nil
}

func (s *tenantStore) Tenants(ctx context.Context) ([]store.Tenant, error) {
	ts, err := s.DB.Tenants(ctx)
	return append(ts, store.Tenant{ID: 2, Slug: "acme", Name: "Acme"}), err
}

// A worker holds to the tenant it was first linked in: a hello that names another is
// refused until it enrols again, which starts it over.
func TestChangedTenantIsRefused(t *testing.T) {
	fastLink(t)
	h := newControllerRig(t)
	ts := &tenantStore{DB: h.st}
	ts.tenant.Store(store.DefaultTenantID)
	h.s.Store = ts
	m := h.worker(t, "")
	m.join(h.addWorker("work"))
	stop := m.s.StartCluster(context.Background())
	defer stop()
	eventually(t, "online in default", func() bool { v := m.cluster().Link; return v.State == api.LinkConnected && v.Tenant == "default" })

	ts.tenant.Store(2)
	h.s.dropLink("work", "test")
	eventually(t, "refused", func() bool {
		v := m.cluster().Link
		return v.State == api.LinkRefused && strings.Contains(v.Error, "tenant default") && strings.Contains(v.Error, "acme")
	})
	if f, _ := m.s.loadLink(); f.Tenant != "default" {
		t.Fatalf("the refusal changed the recorded tenant: %+v", f)
	}
	if mb, _ := workerOf(h.cluster(), "work"); mb.Tenant != "acme" {
		t.Fatalf("the controller lists the worker in %q", mb.Tenant)
	}
	stop()

	// A new enrolment writes a new link file, and the worker links in the new tenant.
	h.call("POST", api.PathClusterWorkers+"/work/revoke", nil, nil)
	if got := m.join(h.addWorker("work")); got != 200 {
		t.Fatalf("re-enrol: %d", got)
	}
	stop = m.s.StartCluster(context.Background())
	defer stop()
	eventually(t, "online in acme", func() bool { v := m.cluster().Link; return v.State == api.LinkConnected && v.Tenant == "acme" })
}

// A link file from before the tenant was recorded still reads, and a controller from
// before tenants were on the link (no tenant in its hello) is not refused.
func TestNoTenantIsNotAChange(t *testing.T) {
	n := newNode(t, config.Default(), "")
	f := linkFile{URL: pubURL, Name: "work", Credential: linkTokenPrefix + "x", ControllerID: "c"}
	b, _ := json.Marshal(f)
	os.WriteFile(n.s.linkPath(), b, 0o600)
	if err := n.s.checkLinkTenant(f, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := n.s.loadLink(); got.Tenant != "" {
		t.Fatalf("recorded %q from an empty hello", got.Tenant)
	}
	got, _ := n.s.loadLink()
	if err := n.s.checkLinkTenant(got, "default"); err != nil {
		t.Fatal(err)
	}
	got, _ = n.s.loadLink()
	if err := n.s.checkLinkTenant(got, "other"); !errors.Is(err, errRefused) {
		t.Fatalf("a changed tenant: %v", err)
	}
}
