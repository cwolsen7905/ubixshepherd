package daemon

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

// node is one daemon: its store, home and loopback API.
type node struct {
	t    *testing.T
	home string
	st   *sqlite.DB
	s    *Server
	loop *httptest.Server
}

func newNode(t *testing.T, cfg config.Config, home string) *node {
	t.Helper()
	if home == "" {
		home = t.TempDir()
	}
	st, err := sqlite.Open(context.Background(), filepath.Join(home, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s, err := NewServer(st, cfg, "/cfg", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	s.Home = home
	s.DeskAgent = nopDesk{}
	n := &node{t: t, home: home, st: st, s: s, loop: httptest.NewServer(s.Handler())}
	t.Cleanup(n.loop.Close)
	t.Cleanup(s.Close)
	return n
}

// call is a request to the node's loopback API as the operator.
func (n *node) call(method, path string, body, out any) int {
	n.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, n.loop.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+n.s.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		n.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && resp.StatusCode == 200 {
		if err := json.Unmarshal(raw, out); err != nil {
			n.t.Fatalf("%s %s: %v: %s", method, path, err, raw)
		}
	}
	return resp.StatusCode
}

func (n *node) cluster() api.Cluster {
	n.t.Helper()
	var c api.Cluster
	if code := n.call("GET", api.PathCluster, nil, &c); code != 200 {
		n.t.Fatalf("GET cluster: %d", code)
	}
	return c
}

// controllerRig is a controller behind a TLS test server that names itself
// shepherd.example.com, and a client that trusts it. Workers dial it and nothing dials
// them.
type controllerRig struct {
	*node
	tls    *httptest.Server
	client *http.Client
	front  atomic.Pointer[http.Handler] // an optional handler in front of the controller's
}

func newControllerRig(t *testing.T) *controllerRig {
	t.Helper()
	dir := t.TempDir()
	certFile, keyFile, pool := selfSigned(t, dir)
	cfg := config.Default()
	cfg.Daemon.Public = &config.Public{Listen: "0.0.0.0:8443", URL: pubURL, TLS: config.PublicTLS{Cert: certFile, Key: keyFile}}
	cfg.Cluster.Role = config.ClusterRoleController
	n := newNode(t, cfg, "")
	if err := n.s.PreparePublic(context.Background(), n.home); err != nil {
		t.Fatal(err)
	}
	h := &controllerRig{node: n}
	inner := n.s.PublicHandler()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f := h.front.Load(); f != nil {
			(*f).ServeHTTP(w, r)
			return
		}
		inner.ServeHTTP(w, r)
	}))
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"http/1.1"}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	h.tls = srv
	h.client = tlsClient(pool, "shepherd.example.com", srv.Listener.Addr().String())
	return h
}

// worker makes a worker daemon whose config names the controller.
func (h *controllerRig) worker(t *testing.T, home string) *node {
	t.Helper()
	cfg := config.Default()
	cfg.Cluster.Controller = pubURL
	n := newNode(t, cfg, home)
	n.s.ClusterClient = h.client
	return n
}

func (h *controllerRig) addWorker(name string) string {
	h.t.Helper()
	var c api.WorkerCode
	if code := h.call("POST", api.PathClusterWorkers, api.WorkerAdd{Name: name}, &c); code != 200 {
		h.t.Fatalf("worker add: %d", code)
	}
	return c.Code
}

func (n *node) join(code string) int {
	return n.call("POST", api.PathClusterJoin, api.ClusterJoin{URL: pubURL, Code: code}, nil)
}

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

var fastOnce, refusedOnce sync.Once

// fastLink runs the link's timings fast for the rest of the package's tests. It sets them
// once: a link goroutine from an earlier test may still be reading them.
func fastLink(t *testing.T) {
	t.Helper()
	fastOnce.Do(func() {
		linkPingEvery, linkIdleEvery, linkBackoffBase, linkBackoffMax = 50*time.Millisecond, 20*time.Millisecond, 20*time.Millisecond, 200*time.Millisecond
	})
}

func workerOf(c api.Cluster, name string) (api.ClusterWorker, bool) {
	for _, m := range c.Workers {
		if m.Name == name {
			return m, true
		}
	}
	return api.ClusterWorker{}, false
}

func TestEnrolmentCode(t *testing.T) {
	if len(effShortWords) != 1295 {
		t.Fatalf("the wordlist has %d words", len(effShortWords))
	}
	for _, w := range effShortWords {
		if w == "" || strings.ToLower(w) != w || strings.ContainsAny(w, "- ") {
			t.Fatalf("bad word %q", w)
		}
	}
	code, err := newEnrolCode()
	if err != nil {
		t.Fatal(err)
	}
	words := strings.Split(code, "-")
	if len(words) != 8 || code != strings.ToLower(code) {
		t.Fatalf("code %q is not eight lowercase words", code)
	}
}

func TestEnrolmentIsOnceAndFiveMinutes(t *testing.T) {
	h := newControllerRig(t)
	m := h.worker(t, "")
	code := h.addWorker("work")
	if n := len(strings.Split(code, "-")); n != 8 {
		t.Fatalf("code %q", code)
	}
	if got := m.join("wrong-code"); got != 502 {
		t.Fatalf("a wrong code joined: %d", got)
	}
	if got := m.join(strings.ToUpper(code)); got != 200 {
		t.Fatalf("join: %d", got)
	}
	if got := m.join(code); got != 502 {
		t.Fatalf("a spent code joined again: %d", got)
	}
	// Expired: the controller's clock is the store's end time; write one already past.
	if err := h.st.Workers().AddWorker(context.Background(), "late", hashOf("old-code"), time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := m.join("old-code"); got != 502 {
		t.Fatalf("an expired code joined: %d", got)
	}
	b, _ := os.ReadFile(filepath.Join(m.home, LinkFile))
	if !strings.Contains(string(b), linkTokenPrefix) {
		t.Fatalf("no credential kept: %s", b)
	}
	if st, _ := os.Stat(filepath.Join(m.home, LinkFile)); st.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("credential file is %v", st.Mode())
	}
}

func TestWorkerLinksToControllerOverTLS(t *testing.T) {
	fastLink(t)
	h := newControllerRig(t)
	m := h.worker(t, "")
	if got := m.join(h.addWorker("work")); got != 200 {
		t.Fatalf("join: %d", got)
	}
	stop := m.s.StartCluster(context.Background())
	defer stop()

	eventually(t, "the worker online", func() bool {
		mb, ok := workerOf(h.cluster(), "work")
		return ok && mb.Online
	})
	mb, _ := workerOf(h.cluster(), "work")
	if mb.DaemonID != m.s.DaemonID() || mb.APILevel != api.ClusterAPILevel || len(mb.Capabilities) == 0 || mb.Version == "" || mb.Since.IsZero() {
		t.Fatalf("worker as the controller sees it: %+v", mb)
	}
	if c := h.cluster(); c.Role != api.RoleController || c.DaemonID != h.s.DaemonID() {
		t.Fatalf("controller: %+v", c)
	}
	mc := m.cluster()
	if mc.Role != api.RoleWorker || mc.Link == nil || mc.Link.State != api.LinkConnected || mc.Link.ControllerID != h.s.DaemonID() || mc.Link.Controller != pubURL {
		t.Fatalf("worker: %+v %+v", mc, mc.Link)
	}

	// Pings flow and statuses follow: a question opened on the worker shows as waiting on
	// the controller's list, with no change to the link.
	if _, err := m.st.AddQuestion(context.Background(), store.Question{Text: "which one?", State: store.QuestionOpen}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the waiting count", func() bool {
		mb, _ := workerOf(h.cluster(), "work")
		return mb.Waiting == 1 && mb.Online
	})
}

func TestLinkRedialsAfterADrop(t *testing.T) {
	fastLink(t)
	h := newControllerRig(t)
	m := h.worker(t, "")
	m.join(h.addWorker("work"))
	stop := m.s.StartCluster(context.Background())
	defer stop()
	online := func() bool { mb, ok := workerOf(h.cluster(), "work"); return ok && mb.Online }
	eventually(t, "online", online)
	first, _ := workerOf(h.cluster(), "work")

	h.s.dropLink("work", "test")
	eventually(t, "offline then online again", func() bool {
		mb, _ := workerOf(h.cluster(), "work")
		return mb.Online && mb.Since.After(first.Since)
	})

	// The controller asks for a wait with Retry-After: the worker honours it as a floor.
	deny := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, io.ErrUnexpectedEOF)
	}))
	h.front.Store(&deny)
	h.s.dropLink("work", "test")
	var v *api.ClusterLink
	eventually(t, "waiting to retry", func() bool {
		v = m.cluster().Link
		return v != nil && v.State == api.LinkWaiting && !v.NextTry.IsZero() && strings.Contains(v.Error, "unexpected EOF")
	})
	if wait := v.NextTry.Sub(v.Since); wait < 900*time.Millisecond {
		t.Fatalf("redial in %v, under the controller's Retry-After", wait)
	}
	h.front.Store(nil)
	eventually(t, "online again after the Retry-After", online)
}

func TestLinkWaitIsFullJitterCappedAndFloored(t *testing.T) {
	if got := linkWait(8*time.Second, 0.5, 0); got != 4*time.Second {
		t.Fatalf("got %v", got)
	}
	if got := linkWait(time.Second, 0, 0); got != 0 {
		t.Fatalf("full jitter starts at zero, got %v", got)
	}
	if got := linkWait(time.Second, 0.5, 30*time.Second); got != 30*time.Second {
		t.Fatalf("Retry-After is a floor, got %v", got)
	}
	step := time.Second
	for range 10 {
		step = min(step*2, 60*time.Second)
	}
	if step != 60*time.Second {
		t.Fatal("the step is capped at 60 seconds")
	}
	if defaultBackoffBase != time.Second || defaultBackoffMax != 60*time.Second || defaultPingEvery != 20*time.Second {
		t.Fatal("the link's timings are not the plan's")
	}
}

func TestLinkCredentialOpensTheLinkAndNothingElse(t *testing.T) {
	h := newControllerRig(t)
	m := h.worker(t, "")
	m.join(h.addWorker("work"))
	f, _ := m.s.loadLink()
	do := func(method, path, tok string, body any) int {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, pubURL+path, rd)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := h.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for _, rt := range []struct{ method, path string }{
		{"GET", api.PathStatus}, {"GET", api.PathCluster}, {"GET", api.PathWaiting}, {"GET", api.PathLanes},
		{"POST", api.PathLanes}, {"POST", api.PathDecisions + "/1/answer"}, {"POST", api.PathRuns},
		{"POST", api.PathClusterWorkers}, {"POST", api.PathClusterWorkers + "/work/revoke"},
		{"PUT", api.PathConfig}, {"POST", api.PathShutdown}, {"GET", "/v1/nothing"},
	} {
		if got := do(rt.method, rt.path, f.Credential, map[string]string{}); got != http.StatusForbidden {
			t.Errorf("%s %s with a link credential: %d, want 403", rt.method, rt.path, got)
		}
	}
	// A token that is not a link credential is not one at the link, and a made-up link
	// credential is not known.
	if got := do("GET", api.PathClusterLink, "deadbeef", nil); got != http.StatusUnauthorized {
		t.Errorf("a non-link token at the link: %d, want 401", got)
	}
	if got := do("GET", api.PathClusterLink, linkTokenPrefix+"deadbeef", nil); got != http.StatusUnauthorized {
		t.Errorf("an unknown link credential: %d, want 401", got)
	}
	// The operator token is never a link credential: the public side answers it 404.
	if got := do("GET", api.PathClusterLink, h.s.Token, nil); got != http.StatusNotFound {
		t.Errorf("the operator token at the link: %d, want 404", got)
	}
}

func TestRevokedCredentialDropsTheLink(t *testing.T) {
	fastLink(t)
	h := newControllerRig(t)
	m := h.worker(t, "")
	m.join(h.addWorker("work"))
	stop := m.s.StartCluster(context.Background())
	defer stop()
	eventually(t, "online", func() bool { mb, _ := workerOf(h.cluster(), "work"); return mb.Online })
	if code := h.call("POST", api.PathClusterWorkers+"/work/revoke", nil, nil); code != 200 {
		t.Fatalf("revoke: %d", code)
	}
	eventually(t, "offline and refused", func() bool {
		mb, _ := workerOf(h.cluster(), "work")
		v := m.cluster().Link
		return !mb.Online && !mb.Enrolled && v.State == api.LinkRefused && strings.Contains(v.Error, "revoked")
	})
	// A new code enrols the same name again.
	if got := m.join(h.addWorker("work")); got != 200 {
		t.Fatalf("re-enrol: %d", got)
	}
	eventually(t, "online again", func() bool { mb, _ := workerOf(h.cluster(), "work"); return mb.Online })
}

func TestChangedDaemonIDIsRefusedUntilConfirmed(t *testing.T) {
	fastLink(t)
	refusedOnce.Do(func() { linkRefusedRetry = 1 })
	h := newControllerRig(t)
	m := h.worker(t, "")
	m.join(h.addWorker("work"))
	// A reinstall: the same credential file, a new Shepherd home and so a new daemon id.
	other := h.worker(t, "")
	b, _ := os.ReadFile(filepath.Join(m.home, LinkFile))
	os.WriteFile(filepath.Join(other.home, LinkFile), b, 0o600)
	if other.s.DaemonID() == m.s.DaemonID() {
		t.Fatal("two homes, one id")
	}
	stop := other.s.StartCluster(context.Background())
	defer stop()
	eventually(t, "refused", func() bool {
		mb, _ := workerOf(h.cluster(), "work")
		v := other.cluster().Link
		return mb.PendingID == other.s.DaemonID() && !mb.Online && v.State == api.LinkRefused && strings.Contains(v.Error, "confirm")
	})
	if code := h.call("POST", api.PathClusterWorkers+"/work/confirm", nil, nil); code != 200 {
		t.Fatalf("confirm: %d", code)
	}
	eventually(t, "online under the new id", func() bool {
		mb, _ := workerOf(h.cluster(), "work")
		return mb.Online && mb.DaemonID == other.s.DaemonID() && mb.PendingID == ""
	})
}

func TestDaemonIDIsKeptInTheHome(t *testing.T) {
	cfg := config.Default()
	a := newNode(t, cfg, "")
	id := a.s.DaemonID()
	if len(id) != 36 {
		t.Fatalf("id %q", id)
	}
	st, _ := os.Stat(filepath.Join(a.home, DaemonIDFile))
	if st == nil {
		t.Fatal("no id file in the home")
	}
	b := newNode(t, cfg, a.home)
	if b.s.DaemonID() != id {
		t.Fatal("a restart changed the daemon id")
	}
	var status api.Status
	if a.call("GET", api.PathStatus, nil, &status); status.DaemonID != id {
		t.Fatalf("status shows %q", status.DaemonID)
	}
}

func TestClusterRoutesNeedTheControllerRole(t *testing.T) {
	n := newNode(t, config.Default(), "")
	if got := n.call("POST", api.PathClusterWorkers, api.WorkerAdd{Name: "work"}, nil); got != http.StatusNotFound {
		t.Fatalf("worker add on a daemon that is no controller: %d", got)
	}
	if c := n.cluster(); c.Role != api.RoleAlone {
		t.Fatalf("role %q", c.Role)
	}
	h := newControllerRig(t)
	if got := h.call("POST", api.PathClusterWorkers, api.WorkerAdd{Name: "Bad Name"}, nil); got != http.StatusBadRequest {
		t.Fatalf("a bad name: %d", got)
	}
}

// The workers' routes still answer under their old prefix, /v1/cluster/members, for one
// release.
func TestOldMembersRoutesStillWork(t *testing.T) {
	h := newControllerRig(t)
	var c api.WorkerCode
	if code := h.call("POST", api.PathClusterMembersOld, api.WorkerAdd{Name: "work"}, &c); code != 200 || c.Code == "" {
		t.Fatalf("add under the old prefix: %d %+v", code, c)
	}
	if _, ok := workerOf(h.cluster(), "work"); !ok {
		t.Fatal("the worker added under the old prefix is not listed")
	}
	if code := h.call("POST", api.PathClusterMembersOld+"/work/remove", nil, nil); code != 200 {
		t.Fatalf("remove under the old prefix: %d", code)
	}
	if _, ok := workerOf(h.cluster(), "work"); ok {
		t.Fatal("the worker removed under the old prefix is still listed")
	}
}

// A controller and a worker are upgraded at different times, so what crosses between
// them must read either way. The enrolment answer carries the controller's id under its
// new and its old name, a worker reads either, and the link's frames carry no role at
// all: a frame exactly as a daemon from before the rename wrote it still reads.
func TestClusterWireReadsAcrossTheRename(t *testing.T) {
	var got api.ClusterEnrolled
	if err := json.Unmarshal([]byte(`{"name":"work","credential":"shl_x","hub_daemon_id":"old-id"}`), &got); err != nil || got.ID() != "old-id" {
		t.Fatalf("an old controller's enrolment answer: %+v %v", got, err)
	}
	raw, _ := json.Marshal(api.NewClusterEnrolled("work", "shl_x", "new-id"))
	var old struct {
		HubID string `json:"hub_daemon_id"`
	}
	if err := json.Unmarshal(raw, &old); err != nil || old.HubID != "new-id" || !strings.Contains(string(raw), `"controller_daemon_id":"new-id"`) {
		t.Fatalf("a new controller's answer, as an old worker reads it: %s", raw)
	}
	for _, frame := range []string{
		`{"type":"hello","daemon_id":"d1","version":"0.9.0","api_level":1,"capabilities":["status"]}`,
		`{"type":"status","runs_going":2,"waiting":3}`,
	} {
		var f api.LinkFrame
		if err := json.Unmarshal([]byte(frame), &f); err != nil || f.Type == "" {
			t.Fatalf("an old frame %s: %+v %v", frame, f, err)
		}
		back, _ := json.Marshal(f)
		if string(back) != frame {
			t.Fatalf("an old frame does not survive a round trip: %s, then %s", frame, back)
		}
	}
}

// A worker that joins a controller from before the rename keeps the controller's id
// from its old-style answer, in a link file an older build also reads.
func TestJoinAnOldController(t *testing.T) {
	h := newControllerRig(t)
	w := h.worker(t, "")
	old := http.Handler(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != api.PathClusterEnrol {
			http.NotFound(rw, r)
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		io.WriteString(rw, `{"name":"work","credential":"`+linkTokenPrefix+`abc","hub_daemon_id":"old-controller"}`)
	}))
	h.front.Store(&old)
	if got := w.join("any-code"); got != 200 {
		t.Fatalf("join an old controller: %d", got)
	}
	f, ok := w.s.loadLink()
	if !ok || f.ControllerID != "old-controller" || f.Name != "work" {
		t.Fatalf("link file: %+v %v", f, ok)
	}
	b, _ := os.ReadFile(filepath.Join(w.home, LinkFile))
	if !strings.Contains(string(b), `"hub_daemon_id": "old-controller"`) {
		t.Fatalf("the link file is not one an older build reads: %s", b)
	}
}
