package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/version"
)

// The member's side of the cluster: enrolling with a hub, then holding one link to it
// and redialling when it drops. The member only ever dials out.

// LinkFile is where a member keeps what enrolment gave it, in the Shepherd home. It
// holds the link credential, so it is for its owner alone and never in the config.
const LinkFile = "cluster-link.json"

var (
	// linkBackoffBase and linkBackoffMax bound the redial wait: a random wait between
	// zero and the current step, which doubles from the base up to the max.
	linkBackoffBase = defaultBackoffBase
	linkBackoffMax  = defaultBackoffMax
	// linkIdleEvery is how often a member that is not joined looks again for a hub.
	linkIdleEvery = time.Second
	// linkRand is the jitter's source, a variable so a test can fix it.
	linkRand = rand.Float64
)

// linkFile is the file's content.
type linkFile struct {
	URL        string `json:"url"`
	Name       string `json:"name"`
	Credential string `json:"credential"`
	HubID      string `json:"hub_daemon_id"`
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

// linkState is a member's link as GET /v1/cluster shows it.
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
	if v.Hub == "" {
		v.Hub = s.LiveConfig().Cluster.Hub
		if f, ok := s.loadLink(); ok && v.Hub == "" {
			v.Hub = f.URL
		}
	}
	return v
}

func (s *Server) clusterClient() *http.Client {
	if s.ClusterClient != nil {
		return s.ClusterClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// clusterJoin is POST /v1/cluster/join: trade a hub's enrolment code for a link
// credential and keep it in the Shepherd home. The daemon then links to the hub.
func (s *Server) clusterJoin(w http.ResponseWriter, r *http.Request) {
	if s.LiveConfig().Cluster.IsHub() {
		writeError(w, http.StatusConflict, errors.New("this daemon is a hub: a hub does not join another"))
		return
	}
	var req api.ClusterJoin
	if !decode(w, r, &req) {
		return
	}
	hub, err := config.NormalizeURL(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("the hub's address %q %v", req.URL, err))
		return
	}
	if cfg := s.LiveConfig().Cluster.Hub; cfg != "" && cfg != hub {
		writeError(w, http.StatusConflict, fmt.Errorf("cluster.hub in the config is %s, not %s: change one so they agree", cfg, hub))
		return
	}
	if s.Home == "" {
		writeError(w, http.StatusServiceUnavailable, errors.New("this daemon has no Shepherd home to keep a credential in"))
		return
	}
	body, _ := json.Marshal(api.ClusterEnrol{Code: req.Code, DaemonID: s.DaemonID()})
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, hub+api.PathClusterEnrol, bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set(api.ClientHeader, "shepherd-daemon/"+version.Version)
	resp, err := s.clusterClient().Do(hr)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("could not reach the hub at %s: %w", hub, err))
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		var e api.Error
		json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = "the hub answered " + resp.Status
		}
		writeError(w, http.StatusBadGateway, errors.New("the hub refused the enrolment: "+e.Error))
		return
	}
	var got api.ClusterEnrolled
	if err := json.Unmarshal(raw, &got); err != nil || got.Credential == "" {
		writeError(w, http.StatusBadGateway, errors.New("the hub's answer was not an enrolment"))
		return
	}
	f := linkFile{URL: hub, Name: got.Name, Credential: got.Credential, HubID: got.HubID}
	b, _ := json.MarshalIndent(f, "", "  ")
	if err := writeFileAtomic(s.linkPath(), b); err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("joined a hub", "hub", hub, "as", got.Name)
	writeJSON(w, http.StatusOK, api.ClusterJoined{Name: got.Name, Hub: hub})
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

// StartCluster starts the member's link, which dials the hub while the config names one
// and a credential is held, and returns the function that ends it. On a hub or a daemon
// on its own it does nothing but watch the config.
func (s *Server) StartCluster(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.holdLink(ctx)
	}()
	return func() { cancel(); <-done }
}

// holdLink is the member's loop: dial, hold, and redial after a wait.
func (s *Server) holdLink(ctx context.Context) {
	ls := &s.cluster.link
	step := linkBackoffBase
	for ctx.Err() == nil {
		cfg := s.LiveConfig().Cluster
		f, joined := s.loadLink()
		switch {
		case cfg.IsHub():
			ls.setState(api.LinkNotJoined, "", time.Time{})
			sleep(ctx, linkIdleEvery)
			continue
		case !joined:
			ls.set(func(v *api.ClusterLink) { v.Hub = cfg.Hub })
			ls.setState(api.LinkNotJoined, "", time.Time{})
			sleep(ctx, linkIdleEvery)
			continue
		case cfg.Hub != "" && cfg.Hub != f.URL:
			ls.set(func(v *api.ClusterLink) { v.Hub = cfg.Hub })
			ls.setState(api.LinkRefused, fmt.Sprintf("the credential is for %s but cluster.hub is %s: run shepherd cluster join again", f.URL, cfg.Hub), time.Time{})
			sleep(ctx, linkIdleEvery)
			continue
		}
		ls.set(func(v *api.ClusterLink) { v.Hub = f.URL })
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
		s.Log.Info("hub link down", "hub", f.URL, "why", text, "retry_in", wait.Round(time.Millisecond).String())
		sleep(ctx, wait)
	}
}

// linkWait is the wait before a redial: full jitter, a random wait between zero and the
// step (rnd is in [0, 1)), and never less than the hub's Retry-After.
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

// errRefused marks an answer from the hub that retrying alone will not fix: a revoked
// credential, a changed id. The member still retries, slowly, because the person may fix
// it on the hub.
var errRefused = errors.New("refused")

// linkOnce dials the hub and holds the link until it ends. up says it got as far as the
// hub's hello; retryAfter is the hub's floor for the next try.
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
			return false, 0, fmt.Errorf("could not reach the hub: %w", err)
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
			msg = "the hub answered " + resp.Status
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			// A refusal is slow to retry whatever its Retry-After.
			retryAfter = max(retryAfter, linkBackoffMax/2)
			return false, retryAfter, fmt.Errorf("%w: %s", errRefused, msg)
		}
		return false, retryAfter, errors.New(msg)
	}
	conn.SetReadLimit(64 << 10)
	lctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer conn.CloseNow()
	err = writeFrame(lctx, conn, api.LinkFrame{
		Type: api.FrameHello, DaemonID: id, Version: version.Version, APILevel: api.ClusterAPILevel,
		Capabilities: []string{"status"},
	})
	if err != nil {
		return false, 0, err
	}
	var hello api.LinkFrame
	hctx, hcancel := context.WithTimeout(lctx, linkHelloWait)
	err = readFrame(hctx, conn, &hello)
	hcancel()
	if err != nil || hello.Type != api.FrameHello {
		return false, 0, errors.New("the hub did not say hello")
	}
	s.cluster.link.set(func(v *api.ClusterLink) {
		v.HubID, v.HubAPI, v.HubVersion = hello.DaemonID, hello.APILevel, hello.Version
	})
	s.cluster.link.setState(api.LinkConnected, "", time.Time{})
	s.Log.Info("linked to the hub", "hub", f.URL, "version", hello.Version, "api_level", hello.APILevel)

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
	s.sendStatus(lctx, conn)
	t := time.NewTicker(linkPingEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "the member is stopping")
			return true, 0, nil
		case err := <-readErr:
			if cs := websocket.CloseStatus(err); cs != -1 {
				return true, 0, fmt.Errorf("the hub closed the link: %s", closeReason(err))
			}
			return true, 0, errors.New("the link dropped")
		case <-pinged:
			return true, 0, errors.New("the hub stopped answering pings")
		case <-t.C:
			s.sendStatus(lctx, conn)
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

// sendStatus tells the hub how many runs are going here and how many items wait on the
// person.
func (s *Server) sendStatus(ctx context.Context, conn *websocket.Conn) {
	runs, wait := s.linkCounts(ctx)
	writeFrame(ctx, conn, api.LinkFrame{Type: api.FrameStatus, RunsGoing: runs, Waiting: wait})
}
