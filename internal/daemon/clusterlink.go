package daemon

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/version"
	"github.com/ubixsys/ubixshepherd/internal/workspace"
)

// The worker's side of the cluster: enrolling with a controller, then holding one link to
// it and redialling when it drops. The worker only ever dials out, and only once it has
// joined: a daemon with no cluster config and no link file dials nothing.

// LinkFile is where a worker keeps what enrolment gave it, in the Shepherd home. It
// holds the link credential, so it is for its owner alone and never in the config.
const LinkFile = "cluster-link.json"

var (
	// linkBackoffBase and linkBackoffMax bound the redial wait: a random wait between
	// zero and the current step, which doubles from the base up to the max.
	linkBackoffBase = defaultBackoffBase
	linkBackoffMax  = defaultBackoffMax
	// linkIdleEvery is how often a worker that is not joined looks again for a
	// controller.
	linkIdleEvery = time.Second
	// linkRand is the jitter's source, a variable so a test can fix it.
	linkRand = rand.Float64
)

// linkFile is the file's content. ControllerID keeps the key it was first written with,
// hub_daemon_id, so a file from before the rename still reads, and the reverse. Tenant is
// the tenant the controller first named for this credential; a later hello that names
// another is refused until the worker enrols again, which writes a new file without one.
type linkFile struct {
	URL          string `json:"url"`
	Name         string `json:"name"`
	Credential   string `json:"credential"`
	ControllerID string `json:"hub_daemon_id"`
	Tenant       string `json:"tenant,omitempty"`
}

func (s *Server) linkPath() string { return filepath.Join(s.Home, LinkFile) }

func (s *Server) loadLink() (linkFile, bool) {
	if s.Home == "" {
		return linkFile{}, false
	}
	b, err := os.ReadFile(s.linkPath())
	if err != nil {
		return linkFile{}, false
	}
	var f linkFile
	if json.Unmarshal(b, &f) != nil || f.Credential == "" || f.URL == "" {
		return linkFile{}, false
	}
	return f, true
}

func (s *Server) hasLinkFile() bool { _, ok := s.loadLink(); return ok }

// linkState is a worker's link as GET /v1/cluster shows it.
type linkState struct {
	mu sync.Mutex
	v  api.ClusterLink
}

func (l *linkState) set(f func(v *api.ClusterLink)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f(&l.v)
}

// setState changes the state and when it began, and sets or clears the error.
func (l *linkState) setState(state, errText string, next time.Time) {
	l.set(func(v *api.ClusterLink) {
		if v.State != state {
			v.Since = time.Now().UTC()
		}
		v.State, v.Error, v.NextTry = state, errText, next.UTC()
		if next.IsZero() {
			v.NextTry = time.Time{}
		}
	})
}

func (s *Server) linkView() api.ClusterLink {
	l := &s.cluster.link
	l.mu.Lock()
	defer l.mu.Unlock()
	v := l.v
	if v.State == "" {
		v.State = api.LinkNotJoined
	}
	f, joined := s.loadLink()
	if v.Controller == "" {
		v.Controller = s.LiveConfig().Cluster.Controller
		if joined && v.Controller == "" {
			v.Controller = f.URL
		}
	}
	if v.Tenant == "" && joined {
		v.Tenant = f.Tenant
	}
	return v
}

func (s *Server) clusterClient() *http.Client {
	if s.ClusterClient != nil {
		return s.ClusterClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// clusterJoin is POST /v1/cluster/join: trade a controller's enrolment code for a link
// credential and keep it in the Shepherd home. The daemon then links to the controller.
func (s *Server) clusterJoin(w http.ResponseWriter, r *http.Request) {
	if s.LiveConfig().Cluster.IsController() {
		writeError(w, http.StatusConflict, errors.New("this daemon is a controller: a controller does not join another"))
		return
	}
	var req api.ClusterJoin
	if !decode(w, r, &req) {
		return
	}
	ctl, err := config.NormalizeURL(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("the controller's address %q %v", req.URL, err))
		return
	}
	if cfg := s.LiveConfig().Cluster.Controller; cfg != "" && cfg != ctl {
		writeError(w, http.StatusConflict, fmt.Errorf("cluster.controller in the config is %s, not %s: change one so they agree", cfg, ctl))
		return
	}
	if s.Home == "" {
		writeError(w, http.StatusServiceUnavailable, errors.New("this daemon has no Shepherd home to keep a credential in"))
		return
	}
	body, _ := json.Marshal(api.ClusterEnrol{Code: req.Code, DaemonID: s.DaemonID()})
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, ctl+api.PathClusterEnrol, bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set(api.ClientHeader, "shepherd-daemon/"+version.Version)
	resp, err := s.clusterClient().Do(hr)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("could not reach the controller at %s: %w", ctl, err))
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		var e api.Error
		json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = "the controller answered " + resp.Status
		}
		writeError(w, http.StatusBadGateway, errors.New("the controller refused the enrolment: "+e.Error))
		return
	}
	var got api.ClusterEnrolled
	if err := json.Unmarshal(raw, &got); err != nil || got.Credential == "" {
		writeError(w, http.StatusBadGateway, errors.New("the controller's answer was not an enrolment"))
		return
	}
	f := linkFile{URL: ctl, Name: got.Name, Credential: got.Credential, ControllerID: got.ID()}
	b, _ := json.MarshalIndent(f, "", "  ")
	if err := writeFileAtomic(s.linkPath(), b); err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("joined a controller", "controller", ctl, "as", got.Name)
	writeJSON(w, http.StatusOK, api.ClusterJoined{Name: got.Name, Controller: ctl})
}

// writeFileAtomic writes a file readable by its owner only.
func writeFileAtomic(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".link-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// StartCluster starts the worker's link, which dials the controller while a credential
// is held, and returns the function that ends it. On a controller or a daemon on its own
// it does nothing but watch the config and the Shepherd home.
func (s *Server) StartCluster(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.holdLink(ctx)
	}()
	return func() { cancel(); <-done }
}

// holdLink is the worker's loop: dial, hold, and redial after a wait.
func (s *Server) holdLink(ctx context.Context) {
	ls := &s.cluster.link
	step := linkBackoffBase
	for ctx.Err() == nil {
		cfg := s.LiveConfig().Cluster
		f, joined := s.loadLink()
		switch {
		case cfg.IsController():
			ls.setState(api.LinkNotJoined, "", time.Time{})
			sleep(ctx, linkIdleEvery)
			continue
		case !joined:
			ls.set(func(v *api.ClusterLink) { v.Controller = cfg.Controller })
			ls.setState(api.LinkNotJoined, "", time.Time{})
			sleep(ctx, linkIdleEvery)
			continue
		case cfg.Controller != "" && cfg.Controller != f.URL:
			ls.set(func(v *api.ClusterLink) { v.Controller = cfg.Controller })
			ls.setState(api.LinkRefused, fmt.Sprintf("the credential is for %s but cluster.controller is %s: run shepherd cluster join again", f.URL, cfg.Controller), time.Time{})
			sleep(ctx, linkIdleEvery)
			continue
		}
		ls.set(func(v *api.ClusterLink) { v.Controller = f.URL })
		ls.setState(api.LinkConnecting, "", time.Time{})
		up, retryAfter, err := s.linkOnce(ctx, f)
		if ctx.Err() != nil {
			return
		}
		if up {
			step = linkBackoffBase // it linked: the next drop starts the backoff again
		}
		wait := linkWait(step, linkRand(), retryAfter)
		step = min(step*2, linkBackoffMax)
		state, text := api.LinkWaiting, ""
		if err != nil {
			text = err.Error()
			if errors.Is(err, errRefused) {
				state = api.LinkRefused
			}
		}
		ls.setState(state, text, time.Now().Add(wait))
		s.Log.Info("controller link down", "controller", f.URL, "why", text, "retry_in", wait.Round(time.Millisecond).String())
		sleep(ctx, wait)
	}
}

// linkWait is the wait before a redial: full jitter, a random wait between zero and the
// step (rnd is in [0, 1)), and never less than the controller's Retry-After.
func linkWait(step time.Duration, rnd float64, retryAfter time.Duration) time.Duration {
	return max(time.Duration(rnd*float64(step)), retryAfter)
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// errRefused marks an answer from the controller that retrying alone will not fix: a
// revoked credential, a changed id. The worker still retries, slowly, because the person
// may fix it on the controller.
var errRefused = errors.New("refused")

// linkOnce dials the controller and holds the link until it ends. up says it got as far
// as the controller's hello; retryAfter is the controller's floor for the next try.
func (s *Server) linkOnce(ctx context.Context, f linkFile) (up bool, retryAfter time.Duration, err error) {
	id := s.DaemonID()
	conn, resp, err := websocket.Dial(ctx, f.URL+api.PathClusterLink, &websocket.DialOptions{
		HTTPClient: s.clusterClient(),
		HTTPHeader: http.Header{
			"Authorization":    {"Bearer " + f.Credential},
			api.DaemonIDHeader: {id},
			api.ClientHeader:   {"shepherd-daemon/" + version.Version},
		},
	})
	if err != nil {
		if resp == nil {
			return false, 0, fmt.Errorf("could not reach the controller: %w", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
		var e api.Error
		json.Unmarshal(raw, &e)
		if secs, perr := strconv.Atoi(resp.Header.Get("Retry-After")); perr == nil && secs > 0 {
			retryAfter = time.Duration(secs) * time.Second
		}
		msg := e.Error
		if msg == "" {
			msg = "the controller answered " + resp.Status
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			// A refusal is slow to retry whatever its Retry-After.
			retryAfter = max(retryAfter, linkBackoffMax/2)
			return false, retryAfter, fmt.Errorf("%w: %s", errRefused, msg)
		}
		return false, retryAfter, errors.New(msg)
	}
	conn.SetReadLimit(api.LinkReadLimit)
	lctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer conn.CloseNow()
	err = writeFrame(lctx, conn, api.LinkFrame{
		Type: api.FrameHello, DaemonID: id, Version: version.Version, APILevel: api.ClusterAPILevel,
		Capabilities: []string{api.CapStatus, api.CapRepos},
	})
	if err != nil {
		return false, 0, err
	}
	var hello api.LinkFrame
	hctx, hcancel := context.WithTimeout(lctx, linkHelloWait)
	err = readFrame(hctx, conn, &hello)
	hcancel()
	if err != nil || hello.Type != api.FrameHello {
		return false, 0, errors.New("the controller did not say hello")
	}
	if err := s.checkLinkTenant(f, hello.Tenant); err != nil {
		conn.Close(websocket.StatusPolicyViolation, "the worker refuses a changed tenant")
		return false, linkBackoffMax / 2, err
	}
	s.cluster.link.set(func(v *api.ClusterLink) {
		v.ControllerID, v.ControllerAPI, v.ControllerVersion = hello.DaemonID, hello.APILevel, hello.Version
		v.Tenant = cmp.Or(hello.Tenant, f.Tenant)
	})
	s.cluster.link.setState(api.LinkConnected, "", time.Time{})
	s.Log.Info("linked to the controller", "controller", f.URL, "version", hello.Version, "api_level", hello.APILevel)

	readErr := make(chan error, 1)
	go func() {
		for {
			var fr api.LinkFrame
			if err := readFrame(lctx, conn, &fr); err != nil {
				readErr <- err
				return
			}
		}
	}()
	pinged := make(chan error, 1)
	go pingLoop(lctx, conn, pinged)
	// The report goes first, then the status: the controller lists the worker at the
	// first frame after the hellos. A report that cannot be read now is sent at a later
	// tick; until then, and after a failed read, the last one sent stands.
	var sent *api.LinkFrame
	report := func() {
		r, err := s.linkReport(lctx)
		if err != nil {
			s.Log.Warn("read this worker's repos for the controller", "err", err)
			return
		}
		if sent != nil && slices.Equal(r.Repos, sent.Repos) && slices.Equal(r.Agents, sent.Agents) && r.ReposTruncated == sent.ReposTruncated {
			return
		}
		if writeFrame(lctx, conn, r) == nil {
			sent = &r
		}
	}
	report()
	s.sendStatus(lctx, conn)
	t := time.NewTicker(linkPingEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "the worker is stopping")
			return true, 0, nil
		case err := <-readErr:
			if cs := websocket.CloseStatus(err); cs != -1 {
				return true, 0, fmt.Errorf("the controller closed the link: %s", closeReason(err))
			}
			return true, 0, errors.New("the link dropped")
		case <-pinged:
			return true, 0, errors.New("the controller stopped answering pings")
		case <-t.C:
			s.sendStatus(lctx, conn)
			report()
		}
	}
}

func closeReason(err error) string {
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		return ce.Reason
	}
	return ""
}

// sendStatus tells the controller how many runs are going here and how many items wait on the
// person.
func (s *Server) sendStatus(ctx context.Context, conn *websocket.Conn) {
	runs, wait := s.linkCounts(ctx)
	writeFrame(ctx, conn, api.LinkFrame{Type: api.FrameStatus, RunsGoing: runs, Waiting: wait})
}

// checkLinkTenant holds a worker to the tenant its credential was first linked in. The
// first hello that names a tenant records it in the link file; one that names another
// is refused, since a credential does not move between tenants: the person enrols the
// worker again. A controller that names none (one from before tenants were on the link)
// is not refused.
func (s *Server) checkLinkTenant(f linkFile, tenant string) error {
	switch {
	case tenant == "" || tenant == f.Tenant:
		return nil
	case f.Tenant != "":
		return fmt.Errorf("%w: this worker was linked in tenant %s, and the controller now names tenant %s; enrol it again with shepherd worker add on the controller and shepherd cluster join here", errRefused, f.Tenant, tenant)
	}
	// Record it, unless a join replaced the file since it was read.
	cur, ok := s.loadLink()
	if !ok || cur.Credential != f.Credential {
		return nil
	}
	cur.Tenant = tenant
	b, _ := json.MarshalIndent(cur, "", "  ")
	if err := writeFileAtomic(s.linkPath(), b); err != nil {
		s.Log.Warn("could not record the link's tenant", "err", err)
	}
	return nil
}

// linkReport is the repos frame a worker tells its controller what it has in: its managed
// repos with an origin, by normalised origin and local name, and the agent CLIs it can
// start. A repo with no remote cannot be matched to another machine's, so it is left out.
// The repos are capped (api.MaxReportRepos, api.MaxReportBytes) so the frame always fits
// the link's read limit, and the rest counted. A store error is returned, not taken for
// no repos.
func (s *Server) linkReport(ctx context.Context) (api.LinkFrame, error) {
	f := api.LinkFrame{Type: api.FrameRepos}
	wss, err := s.Store.Workspaces(ctx)
	if err != nil {
		return f, err
	}
	var repos []api.LinkRepo
	for _, ws := range wss {
		rs, err := s.Store.Repos(ctx, ws.ID)
		if err != nil {
			return f, err
		}
		for _, r := range rs {
			if o := workspace.NormalizeRemote(r.Remote); o != "" {
				repos = append(repos, api.LinkRepo{Origin: o, Name: r.Name})
			}
		}
	}
	slices.SortFunc(repos, func(a, b api.LinkRepo) int {
		return cmp.Or(cmp.Compare(a.Origin, b.Origin), cmp.Compare(a.Name, b.Name))
	})
	repos = slices.Compact(repos)
	size := 0
	for i, r := range repos {
		b, _ := json.Marshal(r)
		if size += len(b) + 1; i >= api.MaxReportRepos || size > api.MaxReportBytes {
			f.ReposTruncated = len(repos) - i
			repos = repos[:i]
			break
		}
	}
	f.Repos, f.Agents = repos, s.Runner.InstalledAgents()
	return f, nil
}
