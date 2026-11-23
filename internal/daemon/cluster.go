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

// The cluster (docs/cluster.md), phase 1: a member dials the hub over HTTPS, upgrades to
// a WebSocket and holds it, and the hub lists its members and whether each is linked.
// Nothing crosses the link but a hello and a status.

// RoleLink is a member's credential on the hub: it opens the link and nothing else.
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
	// linkRefusedRetry is the Retry-After a hub gives a member whose daemon id changed.
	linkRefusedRetry = 60
)

var memberName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// clusterState is the daemon's cluster bookkeeping: the links up on a hub, and a
// member's link to its hub.
type clusterState struct {
	mu    sync.Mutex
	links map[string]*liveLink
	idMu  sync.Mutex
	id    string
	// member side (clusterlink.go)
	link linkState
}

// liveLink is one member's link on the hub.
type liveLink struct {
	since     time.Time
	cancel    context.CancelFunc
	runs      int
	waiting   int
	daemonID  string
	closeWith string
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

// members is the store's members, or an error written to w when this daemon is not a hub
// or its store keeps none.
func (s *Server) members(w http.ResponseWriter) store.MemberStore {
	if !s.LiveConfig().Cluster.IsHub() {
		writeError(w, http.StatusNotFound, errors.New("this daemon is not a hub: set cluster.role: hub in the config"))
		return nil
	}
	ms, ok := s.Store.(interface{ Members() store.MemberStore })
	if !ok {
		writeError(w, http.StatusNotImplemented, errors.New("this store does not keep cluster members"))
		return nil
	}
	return ms.Members()
}

// memberStore is the members without a request to answer.
func (s *Server) memberStore() store.MemberStore {
	if ms, ok := s.Store.(interface{ Members() store.MemberStore }); ok {
		return ms.Members()
	}
	return nil
}

// clusterGet is GET /v1/cluster.
func (s *Server) clusterGet(w http.ResponseWriter, r *http.Request) {
	cfg := s.LiveConfig().Cluster
	out := api.Cluster{Role: api.RoleAlone, DaemonID: s.DaemonID()}
	switch {
	case cfg.IsHub():
		out.Role = api.RoleHub
		ms := s.memberStore()
		if ms == nil {
			break
		}
		list, err := ms.Members(r.Context())
		if err != nil {
			s.fail(w, err)
			return
		}
		out.Members = []api.ClusterMember{}
		for _, m := range list {
			out.Members = append(out.Members, s.memberView(m))
		}
	case cfg.Hub != "" || s.hasLinkFile():
		out.Role = api.RoleMember
		l := s.linkView()
		out.Link = &l
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) memberView(m store.Member) api.ClusterMember {
	v := api.ClusterMember{
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
	}
	s.cluster.mu.Unlock()
	return v
}

// memberAdd is POST /v1/cluster/members: a name, and a code that enrols one machine
// under it once.
func (s *Server) memberAdd(w http.ResponseWriter, r *http.Request) {
	if ok, why := canWriteConfig(principalOf(r.Context())); !ok {
		writeError(w, http.StatusForbidden, errors.New(why))
		return
	}
	ms := s.members(w)
	if ms == nil {
		return
	}
	var req api.MemberAdd
	if !decode(w, r, &req) {
		return
	}
	if !memberName.MatchString(req.Name) {
		writeError(w, http.StatusBadRequest, errors.New("a member's name is lowercase letters, digits and dashes, up to 32, such as work or home"))
		return
	}
	code, err := newEnrolCode()
	if err != nil {
		s.fail(w, err)
		return
	}
	until := time.Now().Add(enrolTTL)
	switch err := ms.AddMember(r.Context(), req.Name, hashOf(code), until); {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, fmt.Errorf("%s is already enrolled: revoke it first, or pick another name", req.Name))
		return
	case err != nil:
		s.fail(w, err)
		return
	}
	s.Log.Info("member enrolment code issued", "member", req.Name, "expires", until.UTC().Format(time.RFC3339))
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, api.MemberCode{Name: req.Name, Code: code, Expires: until.UTC()})
}

// memberAct is a person's change to one member: revoke, remove or confirm.
func (s *Server) memberAct(act string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ok, why := canWriteConfig(principalOf(r.Context())); !ok {
			writeError(w, http.StatusForbidden, errors.New(why))
			return
		}
		ms := s.members(w)
		if ms == nil {
			return
		}
		name := r.PathValue("name")
		var err error
		switch act {
		case "revoke":
			err = ms.RevokeMember(r.Context(), name)
		case "remove":
			err = ms.RemoveMember(r.Context(), name)
		case "confirm":
			err = ms.ConfirmID(r.Context(), name)
		}
		if errors.Is(err, store.ErrNotFound) {
			msg := "no member " + name
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
			s.dropLink(name, "the hub ended this link: "+act)
		}
		s.Log.Info("member "+act, "member", name)
		writeJSON(w, http.StatusOK, struct{}{})
	}
}

// clusterEnrol is POST /v1/cluster/enrol: a code for a credential. It takes no token.
func (s *Server) clusterEnrol(w http.ResponseWriter, r *http.Request) {
	ms := s.members(w)
	if ms == nil {
		return
	}
	var req api.ClusterEnrol
	if !decode(w, r, &req) {
		return
	}
	code := normalizeCode(req.Code)
	if code == "" || req.DaemonID == "" {
		writeError(w, http.StatusBadRequest, errors.New("an enrolment needs a code and the member's daemon id"))
		return
	}
	cred, err := newLinkCredential()
	if err != nil {
		s.fail(w, err)
		return
	}
	name, err := ms.Enrol(r.Context(), hashOf(code), req.DaemonID, newUUID(), hashOf(cred), time.Now())
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusForbidden, errors.New("that code is wrong, already used or expired: run shepherd member add on the hub for a new one"))
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("member enrolled", "member", name, "daemon", req.DaemonID)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, api.ClusterEnrolled{Name: name, Credential: cred, HubID: s.DaemonID()})
}

// linkPrincipal resolves a link credential to a principal for auth. ok is false for one
// the hub does not know or that was revoked.
func (s *Server) linkPrincipal(r *http.Request, tok string) (principal, bool) {
	ms := s.memberStore()
	if ms == nil || !s.LiveConfig().Cluster.IsHub() {
		return principal{}, false
	}
	name, err := ms.CredentialMember(r.Context(), hashOf(tok))
	if err != nil {
		return principal{}, false
	}
	return principal{Role: RoleLink, member: name}, true
}

// dropLink closes a member's live link, if it has one.
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

// clusterLink is GET /v1/cluster/link: the member's connection, upgraded to a WebSocket.
func (s *Server) clusterLink(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r.Context())
	if p.Role != RoleLink {
		writeError(w, http.StatusForbidden, errors.New("the link is opened with a member's link credential"))
		return
	}
	ms := s.members(w)
	if ms == nil {
		return
	}
	ctx := r.Context()
	list, err := ms.Members(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	var m store.Member
	for _, c := range list {
		if c.Name == p.member {
			m = c
		}
	}
	id := r.Header.Get(api.DaemonIDHeader)
	switch {
	case id == "":
		writeError(w, http.StatusBadRequest, errors.New("the link needs the member's daemon id in "+api.DaemonIDHeader))
		return
	case m.DaemonID != id:
		// A reinstall, or a name now used by another machine: hold it until the person says.
		if err := ms.SetPendingID(ctx, m.Name, id); err != nil {
			s.fail(w, err)
			return
		}
		s.Log.Warn("member link refused: its daemon id changed", "member", m.Name)
		w.Header().Set("Retry-After", strconv.Itoa(linkRefusedRetry))
		writeError(w, http.StatusForbidden, fmt.Errorf("%s presented a different daemon id than it enrolled with; confirm it on the hub with: shepherd member confirm %s", m.Name, m.Name))
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept has answered
	}
	conn.SetReadLimit(64 << 10)
	lctx, cancel := context.WithCancel(ctx)
	defer cancel()
	l := &liveLink{since: time.Now().UTC(), cancel: cancel, daemonID: id}
	reason := s.runHubLink(lctx, conn, ms, m, l)
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
	s.Log.Info("member link closed", "member", m.Name, "why", reason)
}

// runHubLink is the hub's side once the connection is up: the hello, then reading
// statuses and pinging until the link ends. It returns why it ended.
func (s *Server) runHubLink(ctx context.Context, conn *websocket.Conn, ms store.MemberStore, m store.Member, l *liveLink) string {
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
	err = writeFrame(ctx, conn, api.LinkFrame{
		Type: api.FrameHello, DaemonID: s.DaemonID(), Version: version.Version, APILevel: api.ClusterAPILevel,
		Capabilities: []string{"status"},
	})
	if err != nil {
		return "could not answer the hello"
	}
	if err := ms.Handshake(ctx, m.Name, hello.Version, hello.APILevel, hello.Capabilities, time.Now()); err != nil {
		s.Log.Error("record a member's handshake", "member", m.Name, "err", err)
	}
	s.cluster.mu.Lock()
	if s.cluster.links == nil {
		s.cluster.links = map[string]*liveLink{}
	}
	old := s.cluster.links[m.Name]
	s.cluster.links[m.Name] = l
	if old != nil {
		old.closeWith = "another link from this member took over"
	}
	s.cluster.mu.Unlock()
	if old != nil {
		old.cancel()
	}
	s.Log.Info("member linked", "member", m.Name, "version", hello.Version, "api_level", hello.APILevel)

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
			if f.Type == api.FrameStatus {
				s.cluster.mu.Lock()
				l.runs, l.waiting = f.RunsGoing, f.Waiting
				s.cluster.mu.Unlock()
			}
		case err := <-readErr:
			if ctx.Err() != nil {
				return "link closed"
			}
			return "the member's connection ended: " + closeText(err)
		case <-pinged:
			return "the member stopped answering pings"
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

// linkCounts is what a member reports to its hub: runs going, and items waiting on the
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
