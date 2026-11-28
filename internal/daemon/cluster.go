package daemon

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/version"
	"github.com/ubixsys/ubixshepherd/internal/waiting"
)

// The cluster (docs/cluster.md), phase 1: a worker dials the controller over HTTPS,
// upgrades to a WebSocket and holds it, and the controller lists its workers and whether
// each is linked. Nothing crosses the link but a hello and a status. A daemon with no
// cluster config is neither, and never dials anywhere.

// RoleLink is a worker's credential on the controller: it opens the link and nothing else.
const RoleLink = "link"

const (
	// linkTokenPrefix starts every link credential, so one is never mistaken for a
	// device token, which has no such prefix, and the reverse.
	linkTokenPrefix = "shl_"
	// enrolWords is how many words an enrolment code has; with 1295 to pick from that is
	// about 81 bits.
	enrolWords = 8
	// enrolTTL is how long an enrolment code lives.
	enrolTTL = 5 * time.Minute
	// DaemonIDFile is where a daemon keeps its id, in the Shepherd home.
	DaemonIDFile = "daemon.id"
)

// The plan's timings of the link.
const (
	defaultPingEvery   = 20 * time.Second
	defaultBackoffBase = time.Second
	defaultBackoffMax  = 60 * time.Second
)

// Timings of the link, variables so a test can run it fast. A test sets them once,
// before it starts any daemon (see fastLink).
var (
	// linkPingEvery is how often each side pings, under the idle timeouts of common
	// proxies and load balancers.
	linkPingEvery = defaultPingEvery
	// linkMissedPongs is how many pings in a row may go unanswered before the link closes.
	linkMissedPongs = 2
	// linkHelloWait is how long the hello may take.
	linkHelloWait = 10 * time.Second
	// linkRefusedRetry is the Retry-After a controller gives a worker whose daemon id
	// changed.
	linkRefusedRetry = 60
)

var workerName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// clusterState is the daemon's cluster bookkeeping: the links up on a controller, and a
// worker's link to its controller.
type clusterState struct {
	mu    sync.Mutex
	links map[string]*liveLink
	idMu  sync.Mutex
	id    string
	// worker side (clusterlink.go)
	link linkState
}

// liveLink is one worker's link on the controller. What the worker reported, its counts,
// repos and agents, lives here and goes with the link: it is live data, never stored.
type liveLink struct {
	since     time.Time
	cancel    context.CancelFunc
	runs      int
	waiting   int
	repos     []api.LinkRepo
	agents    []string
	truncated int
	daemonID  string
	closeWith string
}

// apply records what a frame from the worker reports, under the cluster's lock. A report
// is kept to api.MaxReportRepos repos, whatever a worker sends.
func (l *liveLink) apply(f api.LinkFrame) {
	switch f.Type {
	case api.FrameStatus:
		l.runs, l.waiting = f.RunsGoing, f.Waiting
	case api.FrameRepos:
		l.repos, l.agents, l.truncated = f.Repos, f.Agents, f.ReposTruncated
		if n := len(l.repos); n > api.MaxReportRepos {
			l.repos, l.truncated = l.repos[:api.MaxReportRepos], l.truncated+n-api.MaxReportRepos
		}
	}
}

// DaemonID is this daemon's id: a random UUID written to the Shepherd home on first
// start. A server with no home (a test) keeps one for as long as it lives.
func (s *Server) DaemonID() string {
	c := &s.cluster
	c.idMu.Lock()
	defer c.idMu.Unlock()
	if c.id != "" {
		return c.id
	}
	path := ""
	if s.Home != "" {
		path = filepath.Join(s.Home, DaemonIDFile)
		if b, err := os.ReadFile(path); err == nil {
			if id := strings.TrimSpace(string(b)); id != "" {
				c.id = id
				return id
			}
		}
	}
	c.id = newUUID()
	if path != "" {
		if err := os.WriteFile(path, []byte(c.id+"\n"), 0o600); err != nil {
			s.Log.Warn("could not write the daemon id", "err", err)
		}
	}
	return c.id
}

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func hashOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// newEnrolCode is eight words from the EFF short list, joined by "-".
func newEnrolCode() (string, error) {
	words := make([]string, enrolWords)
	for i := range words {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(effShortWords))))
		if err != nil {
			return "", err
		}
		words[i] = effShortWords[n.Int64()]
	}
	return strings.Join(words, "-"), nil
}

func normalizeCode(c string) string { return strings.ToLower(strings.TrimSpace(c)) }

func newLinkCredential() (string, error) {
	t, err := newToken()
	return linkTokenPrefix + t, err
}

// workers is the store's workers, or an error written to w when this daemon is not a
// controller or its store keeps none.
func (s *Server) workers(w http.ResponseWriter) store.WorkerStore {
	if !s.LiveConfig().Cluster.IsController() {
		writeError(w, http.StatusNotFound, errors.New("this daemon is not a controller: set cluster.role: controller in the config"))
		return nil
	}
	ws, ok := s.Store.(interface{ Workers() store.WorkerStore })
	if !ok {
		writeError(w, http.StatusNotImplemented, errors.New("this store does not keep cluster workers"))
		return nil
	}
	return ws.Workers()
}

// workerStore is the workers without a request to answer.
func (s *Server) workerStore() store.WorkerStore {
	if ws, ok := s.Store.(interface{ Workers() store.WorkerStore }); ok {
		return ws.Workers()
	}
	return nil
}

// clusterGet is GET /v1/cluster.
func (s *Server) clusterGet(w http.ResponseWriter, r *http.Request) {
	cfg := s.LiveConfig().Cluster
	out := api.Cluster{Role: api.RoleAlone, DaemonID: s.DaemonID()}
	switch {
	case cfg.IsController():
		out.Role = api.RoleController
		ws := s.workerStore()
		if ws == nil {
			break
		}
		list, err := ws.Workers(r.Context())
		if err != nil {
			s.fail(w, err)
			return
		}
		slugs := s.tenantSlugs(r.Context())
		out.Workers = []api.ClusterWorker{}
		for _, m := range list {
			v := s.workerView(m)
			v.Tenant = slugs[s.workerTenant(r.Context(), m.Name)]
			out.Workers = append(out.Workers, v)
		}
	case cfg.Controller != "" || s.hasLinkFile():
		out.Role = api.RoleWorker
		l := s.linkView()
		out.Link = &l
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) workerView(m store.Worker) api.ClusterWorker {
	v := api.ClusterWorker{
		Name: m.Name, DaemonID: m.DaemonID, Enrolled: m.Enrolled, Since: m.LastSeen, Version: m.Version,
		APILevel: m.APILevel, Capabilities: m.Capabilities, PendingID: m.PendingID, CodeExpires: m.CodeUntil,
	}
	if !m.CodeUntil.IsZero() && !m.CodeUntil.After(time.Now()) {
		v.CodeExpires = time.Time{}
	}
	s.cluster.mu.Lock()
	if l := s.cluster.links[m.Name]; l != nil {
		v.Online, v.Since, v.RunsGoing, v.Waiting = true, l.since, l.runs, l.waiting
		v.DaemonID = l.daemonID
		v.Repos, v.Agents, v.ReposTruncated = slices.Clone(l.repos), slices.Clone(l.agents), l.truncated
	}
	s.cluster.mu.Unlock()
	return v
}

// workerAdd is POST /v1/cluster/workers: a name, and a code that enrols one machine
// under it once.
func (s *Server) workerAdd(w http.ResponseWriter, r *http.Request) {
	if ok, why := canWriteConfig(principalOf(r.Context())); !ok {
		writeError(w, http.StatusForbidden, errors.New(why))
		return
	}
	ms := s.workers(w)
	if ms == nil {
		return
	}
	var req api.WorkerAdd
	if !decode(w, r, &req) {
		return
	}
	if !workerName.MatchString(req.Name) {
		writeError(w, http.StatusBadRequest, errors.New("a worker's name is lowercase letters, digits and dashes, up to 32, such as work or home"))
		return
	}
	code, err := newEnrolCode()
	if err != nil {
		s.fail(w, err)
		return
	}
	until := time.Now().Add(enrolTTL)
	switch err := ms.AddWorker(r.Context(), req.Name, hashOf(code), until); {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, fmt.Errorf("%s is already enrolled: revoke it first, or pick another name", req.Name))
		return
	case err != nil:
		s.fail(w, err)
		return
	}
	s.Log.Info("worker enrolment code issued", "worker", req.Name, "expires", until.UTC().Format(time.RFC3339))
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, api.WorkerCode{Name: req.Name, Code: code, Expires: until.UTC()})
}

// workerAct is a person's change to one worker: revoke, remove or confirm.
func (s *Server) workerAct(act string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ok, why := canWriteConfig(principalOf(r.Context())); !ok {
			writeError(w, http.StatusForbidden, errors.New(why))
			return
		}
		ms := s.workers(w)
		if ms == nil {
			return
		}
		name := r.PathValue("name")
		var err error
		switch act {
		case "revoke":
			err = ms.RevokeWorker(r.Context(), name)
		case "remove":
			err = ms.RemoveWorker(r.Context(), name)
		case "confirm":
			err = ms.ConfirmID(r.Context(), name)
		}
		if errors.Is(err, store.ErrNotFound) {
			msg := "no worker " + name
			if act == "confirm" {
				msg = name + " has no changed id waiting to be confirmed"
			}
			writeError(w, http.StatusNotFound, errors.New(msg))
			return
		}
		if err != nil {
			s.fail(w, err)
			return
		}
		if act != "confirm" {
			s.dropLink(name, "the controller ended this link: "+act)
		}
		s.Log.Info("worker "+act, "worker", name)
		writeJSON(w, http.StatusOK, struct{}{})
	}
}

// clusterEnrol is POST /v1/cluster/enrol: a code for a credential. It takes no token.
func (s *Server) clusterEnrol(w http.ResponseWriter, r *http.Request) {
	ms := s.workers(w)
	if ms == nil {
		return
	}
	var req api.ClusterEnrol
	if !decode(w, r, &req) {
		return
	}
	code := normalizeCode(req.Code)
	if code == "" || req.DaemonID == "" {
		writeError(w, http.StatusBadRequest, errors.New("an enrolment needs a code and the worker's daemon id"))
		return
	}
	cred, err := newLinkCredential()
	if err != nil {
		s.fail(w, err)
		return
	}
	name, err := ms.Enrol(r.Context(), hashOf(code), req.DaemonID, newUUID(), hashOf(cred), time.Now())
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusForbidden, errors.New("that code is wrong, already used or expired: run shepherd worker add on the controller for a new one"))
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("worker enrolled", "worker", name, "daemon", req.DaemonID)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, api.NewClusterEnrolled(name, cred, s.DaemonID()))
}

// linkPrincipal resolves a link credential to a principal for auth. ok is false for one
// the controller does not know or that was revoked.
func (s *Server) linkPrincipal(r *http.Request, tok string) (principal, bool) {
	ms := s.workerStore()
	if ms == nil || !s.LiveConfig().Cluster.IsController() {
		return principal{}, false
	}
	name, err := ms.CredentialWorker(r.Context(), hashOf(tok))
	if err != nil {
		return principal{}, false
	}
	return principal{Role: RoleLink, member: name}, true
}

// dropLink closes a worker's live link, if it has one.
func (s *Server) dropLink(name, why string) {
	s.cluster.mu.Lock()
	l := s.cluster.links[name]
	if l != nil {
		l.closeWith = why
	}
	s.cluster.mu.Unlock()
	if l != nil {
		l.cancel()
	}
}

// clusterLink is GET /v1/cluster/link: the worker's connection, upgraded to a WebSocket.
func (s *Server) clusterLink(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r.Context())
	if p.Role != RoleLink {
		writeError(w, http.StatusForbidden, errors.New("the link is opened with a worker's link credential"))
		return
	}
	ms := s.workers(w)
	if ms == nil {
		return
	}
	ctx := r.Context()
	list, err := ms.Workers(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	var m store.Worker
	for _, c := range list {
		if c.Name == p.member {
			m = c
		}
	}
	id := r.Header.Get(api.DaemonIDHeader)
	switch {
	case id == "":
		writeError(w, http.StatusBadRequest, errors.New("the link needs the worker's daemon id in "+api.DaemonIDHeader))
		return
	case m.DaemonID != id:
		// A reinstall, or a name now used by another machine: hold it until the person says.
		if err := ms.SetPendingID(ctx, m.Name, id); err != nil {
			s.fail(w, err)
			return
		}
		s.Log.Warn("worker link refused: its daemon id changed", "worker", m.Name)
		w.Header().Set("Retry-After", strconv.Itoa(linkRefusedRetry))
		writeError(w, http.StatusForbidden, fmt.Errorf("%s presented a different daemon id than it enrolled with; confirm it on the controller with: shepherd worker confirm %s", m.Name, m.Name))
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept has answered
	}
	conn.SetReadLimit(api.LinkReadLimit)
	lctx, cancel := context.WithCancel(ctx)
	defer cancel()
	l := &liveLink{since: time.Now().UTC(), cancel: cancel, daemonID: id}
	reason := s.runControllerLink(lctx, conn, ms, m, l)
	s.cluster.mu.Lock()
	if s.cluster.links[m.Name] == l {
		delete(s.cluster.links, m.Name)
	}
	why := l.closeWith
	s.cluster.mu.Unlock()
	if why != "" {
		reason = why
	}
	if len(reason) > 100 {
		reason = reason[:100]
	}
	conn.Close(websocket.StatusNormalClosure, reason)
	ms.Seen(context.Background(), m.Name, time.Now())
	s.Log.Info("worker link closed", "worker", m.Name, "why", reason)
}

// runControllerLink is the controller's side once the connection is up: the hello, then reading
// statuses and pinging until the link ends. It returns why it ended.
func (s *Server) runControllerLink(ctx context.Context, conn *websocket.Conn, ms store.WorkerStore, m store.Worker, l *liveLink) string {
	var hello api.LinkFrame
	hctx, hcancel := context.WithTimeout(ctx, linkHelloWait)
	err := readFrame(hctx, conn, &hello)
	hcancel()
	if err != nil || hello.Type != api.FrameHello {
		return "no hello"
	}
	if hello.DaemonID != l.daemonID {
		return "the hello's daemon id is not the header's"
	}
	// The tenant is the default one until the store records a worker's tenant
	// (workerTenant).
	err = writeFrame(ctx, conn, api.LinkFrame{
		Type: api.FrameHello, DaemonID: s.DaemonID(), Version: version.Version, APILevel: api.ClusterAPILevel,
		Capabilities: []string{api.CapStatus}, Tenant: s.tenantSlugs(ctx)[s.workerTenant(ctx, m.Name)],
	})
	if err != nil {
		return "could not answer the hello"
	}
	// The worker checks the controller's hello (its tenant) before it sends anything
	// more, and every worker sends a frame at once when it accepts. Until that first
	// frame the worker is not listed: one that refuses the hello never shows as online,
	// in this tenant or any other.
	var first api.LinkFrame
	fctx, fcancel := context.WithTimeout(ctx, linkHelloWait)
	err = readFrame(fctx, conn, &first)
	fcancel()
	if err != nil {
		return "the worker did not accept the hello"
	}
	if err := ms.Handshake(ctx, m.Name, hello.Version, hello.APILevel, hello.Capabilities, time.Now()); err != nil {
		s.Log.Error("record a worker's handshake", "worker", m.Name, "err", err)
	}
	s.cluster.mu.Lock()
	l.apply(first)
	if s.cluster.links == nil {
		s.cluster.links = map[string]*liveLink{}
	}
	old := s.cluster.links[m.Name]
	s.cluster.links[m.Name] = l
	if old != nil {
		old.closeWith = "another link from this worker took over"
	}
	s.cluster.mu.Unlock()
	if old != nil {
		old.cancel()
	}
	s.Log.Info("worker linked", "worker", m.Name, "version", hello.Version, "api_level", hello.APILevel)

	frames := make(chan api.LinkFrame)
	readErr := make(chan error, 1)
	go func() {
		for {
			var f api.LinkFrame
			if err := readFrame(ctx, conn, &f); err != nil {
				readErr <- err
				return
			}
			select {
			case frames <- f:
			case <-ctx.Done():
				return
			}
		}
	}()
	pinged := make(chan error, 1)
	go pingLoop(ctx, conn, pinged)
	for {
		select {
		case f := <-frames:
			s.cluster.mu.Lock()
			l.apply(f)
			s.cluster.mu.Unlock()
		case err := <-readErr:
			if ctx.Err() != nil {
				return "link closed"
			}
			return "the worker's connection ended: " + closeText(err)
		case <-pinged:
			return "the worker stopped answering pings"
		case <-ctx.Done():
			return "link closed"
		}
	}
}

func closeText(err error) string {
	if cs := websocket.CloseStatus(err); cs != -1 {
		return fmt.Sprintf("closed (%d)", cs)
	}
	return "dropped"
}

// pingLoop pings every linkPingEvery and reports on done when linkMissedPongs pings in
// a row went unanswered. The connection's reader must be running, or no pong is seen.
func pingLoop(ctx context.Context, conn *websocket.Conn, done chan<- error) {
	t := time.NewTicker(linkPingEvery)
	defer t.Stop()
	missed := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		pctx, cancel := context.WithTimeout(ctx, linkPingEvery)
		err := conn.Ping(pctx)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if missed++; missed >= linkMissedPongs {
				done <- err
				return
			}
			continue
		}
		missed = 0
	}
}

func readFrame(ctx context.Context, conn *websocket.Conn, f *api.LinkFrame) error {
	typ, b, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	if typ != websocket.MessageText {
		return errors.New("a link frame is a JSON text message")
	}
	return json.Unmarshal(b, f)
}

func writeFrame(ctx context.Context, conn *websocket.Conn, f api.LinkFrame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b)
}

// linkCounts is what a worker reports to its controller: runs going, and items waiting on the
// person.
func (s *Server) linkCounts(ctx context.Context) (runs, wait int) {
	if rs, err := s.Store.Runs(ctx, 0, store.RunRunning, 1000); err == nil {
		runs = len(rs)
	}
	if w, err := waiting.Collect(ctx, s.Store, s.LiveConfig(), 0); err == nil {
		if qs, err := s.Store.Questions(ctx, 0, store.QuestionOpen); err == nil {
			w.AddQuestions(qs)
		}
		wait = w.Count
	}
	return
}

// workerTenant is the id of the tenant a worker's credential belongs to: the store's
// answer when it keeps one per worker, else the default tenant, which every worker is in
// until a store says otherwise.
func (s *Server) workerTenant(ctx context.Context, name string) int64 {
	if wt, ok := s.Store.(interface {
		WorkerTenant(ctx context.Context, name string) (int64, error)
	}); ok {
		if id, err := wt.WorkerTenant(ctx, name); err == nil && id > 0 {
			return id
		}
	}
	return store.DefaultTenantID
}

// tenantSlugs maps the store's tenants' ids to their slugs.
func (s *Server) tenantSlugs(ctx context.Context) map[int64]string {
	out := map[int64]string{}
	ts, err := s.Store.Tenants(ctx)
	if err != nil {
		s.Log.Warn("list tenants", "err", err)
	}
	for _, t := range ts {
		out[t.ID] = t.Slug
	}
	return out
}
