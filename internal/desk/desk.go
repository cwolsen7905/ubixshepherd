package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/waiting"
)

// ErrQueueFull refuses a turn when the queue is at its bound.
var ErrQueueFull = errors.New("the desk has too many turns waiting; wait for it, or interrupt it")

// ErrClosed refuses a turn once the daemon is stopping.
var ErrClosed = errors.New("the daemon is stopping")

// Tokens mint the token each turn's operator tools call the daemon with. The daemon
// provides them.
type Tokens interface {
	// MintDeskTurn returns a desk token for the turn with the given id; human says the
	// turn was started by the person, so its tools may record their answer to a
	// decision. The daemon files the turn under the topic its tools name.
	MintDeskTurn(human bool, session string, turn int64) (string, error)
	// RevokeToken ends one token.
	RevokeToken(token string)
	// URL is the daemon's API base, http://host:port.
	URL() string
}

// Options configure a Manager. Zero values take the defaults below.
type Options struct {
	Store  store.Store
	Agent  Agent
	Tokens Tokens
	// Spend records a turn's cost; nil writes it to the store directly.
	Spend func(ctx context.Context, sp store.Spend) error
	// Model is the model a turn uses: the desk.model setting, else config's.
	Model func(ctx context.Context) string
	// Wake is desk.wake: config.WakeAttached, WakeAlways or WakeNever.
	Wake func() string
	// Config is the live configuration: desk.rotate_*, desk.summary_* and
	// desk.tool_output_chars, and the repos' standing rules for the summary.
	Config func() config.Config
	Log    *slog.Logger
	// Grace is how long after the last client leaves the desk still wakes on its own.
	Grace time.Duration
	// QueueMax bounds the turns waiting; DigestMax the events waiting for a client.
	QueueMax, DigestMax int
	// Keep is how many events each workspace's conversation keeps.
	Keep int
}

// FeedRotated is the feed kind for a desk session rotated: ended at a turn's end once it
// grew past desk.rotate_tokens or desk.rotate_cost, the next turn starting a new one
// seeded with a summary.
const FeedRotated = "desk_rotated"

// Noter is an Agent that can also write the notes paragraph of a summary
// (desk.summary_model).
type Noter interface {
	Notes(ctx context.Context, model, dir, prompt string) (string, float64, error)
}

// notesTimeout bounds the notes call: the summary does without them.
const notesTimeout = 90 * time.Second

// Defaults.
const (
	DefaultGrace     = 30 * time.Second
	DefaultQueueMax  = 16
	DefaultDigestMax = 20
	DefaultKeep      = 5000
)

// Manager holds the desks, one per workspace, created when first used.
type Manager struct {
	o      Options
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	desks  map[int64]*Desk
	wg     sync.WaitGroup
	closed bool
}

// NewManager returns a Manager; Close stops it.
func NewManager(o Options) *Manager {
	if o.Grace == 0 {
		o.Grace = DefaultGrace
	}
	if o.QueueMax == 0 {
		o.QueueMax = DefaultQueueMax
	}
	if o.DigestMax == 0 {
		o.DigestMax = DefaultDigestMax
	}
	if o.Keep == 0 {
		o.Keep = DefaultKeep
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.Model == nil {
		o.Model = func(context.Context) string { return "" }
	}
	if o.Wake == nil {
		o.Wake = func() string { return config.WakeAttached }
	}
	if o.Config == nil {
		o.Config = config.Default
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{o: o, ctx: ctx, cancel: cancel, desks: map[int64]*Desk{}}
}

// For returns the workspace's desk, starting it on first use. A desk starting after a
// daemon stopped mid-turn closes the turns it left open.
func (m *Manager) For(ws store.Workspace) (*Desk, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	if d, ok := m.desks[ws.ID]; ok {
		return d, nil
	}
	d := &Desk{m: m, ws: ws, kick: make(chan struct{}, 1), changed: make(chan struct{}), subs: map[*Sub]bool{}}
	if err := d.recover(m.ctx); err != nil {
		return nil, err
	}
	m.desks[ws.ID] = d
	m.wg.Add(1)
	go d.loop()
	return d, nil
}

// Wake tells every desk about new feed items; those that need someone to act wake it
// (see wakes), as desk.wake allows.
func (m *Manager) Wake(items []store.FeedItem) {
	var texts []pendingEvent
	for _, it := range items {
		if wakes(it.Kind) {
			texts = append(texts, pendingEvent{text: it.Text, topic: m.eventTopic(it)})
		}
	}
	if len(texts) == 0 || m.o.Wake() == config.WakeNever {
		return
	}
	m.mu.Lock()
	desks := make([]*Desk, 0, len(m.desks))
	for _, d := range m.desks {
		desks = append(desks, d)
	}
	m.mu.Unlock()
	for _, d := range desks {
		d.note(texts)
	}
}

// wakes says whether a feed item continues the desk on its own: a run ended, a decision
// waits for the person, or a request needs routing. The desk's own conversation never
// reaches the feed, so it cannot wake itself.
func wakes(kind string) bool {
	switch api.EventKind(kind) {
	case api.EventRunEnded, api.EventRunPassed, api.EventRunFailed, api.EventRunInterrupted,
		api.EventRunQuota, api.EventDecisionAsked, api.EventRequestAttention:
		return true
	}
	return false
}

// Close interrupts every turn, ends every stream and waits for the desks to stop.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	desks := m.desks
	m.mu.Unlock()
	m.cancel()
	for _, d := range desks {
		d.mu.Lock()
		d.wakeLocked()
		d.mu.Unlock()
	}
	m.wg.Wait()
}

// Done is closed when the manager is closing: streams end on it.
func (m *Manager) Done() <-chan struct{} { return m.ctx.Done() }

// Desk is one workspace's conversation.
type Desk struct {
	m  *Manager
	ws store.Workspace
	// carry is the message of a turn a daemon stop cut short, for the next turn: the
	// agent may never have saved it to its session.
	carry string

	mu    sync.Mutex
	queue []turn
	cur   *turn
	// stop interrupts the turn in progress.
	stop        context.CancelFunc
	interrupted bool
	// gen counts new conversations, so a turn cut short by one does not keep its session.
	gen int
	// pending are the swarm's events waiting for the desk; dropped counts the oldest
	// let go once there were DigestMax.
	pending []pendingEvent
	dropped int
	// acted are what the person did from a browser since the desk's last turn, for the
	// head of its next; actedDropped counts the oldest let go past DigestMax.
	acted        []string
	actedDropped int
	subs         map[*Sub]bool
	lastDetach   time.Time
	// changed is closed, and replaced, whenever an event is stored.
	changed chan struct{}
	kick    chan struct{}
}

type turn struct {
	id     int64
	origin string
	text   string
	// topic is the topic Shepherd filed the turn under from the events that woke the
	// desk, 0 when none; others are the further topics those events touch.
	topic  int64
	others []int64
	// how is how Shepherd filed a person's turn (fileViewing, fileNamed or fileLast),
	// empty for an event turn; what is the thing the message named, for fileNamed.
	how, what string
}

// pendingEvent is a waiting swarm event: its line, and the topic it is already tied to
// (0 for none).
type pendingEvent struct {
	text  string
	topic int64
}

// Sub is a client following the desk: it counts as attached while subscribed, and
// receives the partial replies, which are never stored.
type Sub struct {
	Live chan store.DeskEvent
}

func settingSession(ws int64) string { return fmt.Sprintf("desk.daemon.%d.session", ws) }
func settingCost(ws int64) string    { return fmt.Sprintf("desk.daemon.%d.cost", ws) }

// Workspace is the workspace whose conversation this is.
func (d *Desk) Workspace() store.Workspace { return d.ws }

// Session is the agent session the conversation resumes, "" before its first turn.
func (d *Desk) Session(ctx context.Context) string {
	s, _ := d.m.o.Store.Setting(ctx, settingSession(d.ws.ID))
	return s
}

// Send queues the person's message as a turn and returns the turn's id.
func (d *Desk) Send(ctx context.Context, text string) (int64, int, error) {
	return d.SendIn(ctx, text, 0)
}

// SendIn is Send for a message written from a topic (0 for none): Shepherd files the
// turn under that topic, or else under the topic of what it names, or else under the
// topic of the person's last turn, before the desk sees it.
func (d *Desk) SendIn(ctx context.Context, text string, topic int64) (int64, int, error) {
	if strings.TrimSpace(text) == "" {
		return 0, 0, errors.New("say something")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.m.ctx.Err() != nil {
		return 0, 0, ErrClosed
	}
	if len(d.queue) >= d.m.o.QueueMax {
		return 0, len(d.queue), ErrQueueFull
	}
	filed, how, what := d.chooseTopic(ctx, text, topic)
	e, err := d.appendLocked(ctx, store.DeskEvent{Kind: store.DeskUser, Text: text, Origin: store.DeskByHuman})
	if err != nil {
		return 0, 0, err
	}
	t := turn{id: e.Turn, origin: store.DeskByHuman, text: text}
	if filed != 0 {
		l := store.TopicLink{TopicID: filed, Kind: store.TopicTurn, Ref: strconv.FormatInt(e.Turn, 10), By: store.TopicLinkShepherd}
		if _, err := d.m.o.Store.LinkTopic(ctx, l); err != nil {
			d.m.o.Log.Warn("desk: file a person's turn", "turn", e.Turn, "topic", filed, "err", err)
		} else {
			t.topic, t.how, t.what = filed, how, what
		}
	}
	d.queue = append(d.queue, t)
	ahead := len(d.queue) - 1
	if d.cur != nil {
		ahead++
	}
	d.wakeLocked()
	return e.Turn, ahead, nil
}

// Interrupt stops the turn in progress, if any. Queued turns still run.
func (d *Desk) Interrupt() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cur == nil {
		return false
	}
	d.interrupted = true
	d.stop()
	return true
}

// New starts a new conversation: the turn in progress is interrupted, those queued are
// dropped, and the next turn starts a new session with the brief. The history stays.
func (d *Desk) New(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cur != nil {
		d.interrupted = true
		d.stop()
	}
	for _, t := range d.queue {
		d.appendLocked(ctx, store.DeskEvent{Kind: store.DeskTurnEnd, Turn: t.id, Origin: t.origin, Text: "dropped: a new conversation was started"})
	}
	d.queue = nil
	d.pending, d.dropped = nil, 0
	d.acted, d.actedDropped = nil, 0
	d.carry = ""
	d.gen++
	if err := d.m.o.Store.SetSetting(ctx, settingSession(d.ws.ID), ""); err != nil {
		return err
	}
	_, err := d.appendLocked(ctx, store.DeskEvent{Kind: store.DeskNew, Origin: store.DeskByHuman})
	return err
}

// Subscribe attaches a client. Events waiting as a digest are delivered now.
func (d *Desk) Subscribe() *Sub {
	s := &Sub{Live: make(chan store.DeskEvent, 256)}
	d.mu.Lock()
	d.subs[s] = true
	d.wakeLocked()
	d.mu.Unlock()
	return s
}

// Unsubscribe detaches a client.
func (d *Desk) Unsubscribe(s *Sub) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.subs[s] {
		delete(d.subs, s)
		d.lastDetach = time.Now()
	}
}

// Attached is how many clients follow the desk.
func (d *Desk) Attached() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.subs)
}

// Changed is closed when the next event is stored.
func (d *Desk) Changed() <-chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.changed
}

// Busy says whether a turn is in progress, and how many wait.
func (d *Desk) Busy() (bool, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cur != nil, len(d.queue)
}

// note keeps the swarm's events for the desk, newest winning past DigestMax.
func (d *Desk) note(texts []pendingEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pending = append(d.pending, texts...)
	if over := len(d.pending) - d.m.o.DigestMax; over > 0 {
		d.pending = d.pending[over:]
		d.dropped += over
	}
	d.wakeLocked()
}

// Acted records something the person did from a browser as an action event, which shows
// in the thread, the history and every client's stream. It starts no turn and costs
// nothing: the desk holds the text and puts it at the head of its next turn.
func (d *Desk) Acted(ctx context.Context, text string) error {
	text = strings.TrimSpace(redact.String(text))
	if text == "" {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.m.ctx.Err() != nil {
		return ErrClosed
	}
	if _, err := d.appendLocked(ctx, store.DeskEvent{Kind: store.DeskAction, Text: text, Origin: store.DeskByHuman}); err != nil {
		return err
	}
	d.acted = append(d.acted, text)
	if over := len(d.acted) - d.m.o.DigestMax; over > 0 {
		d.acted = d.acted[over:]
		d.actedDropped += over
	}
	return nil
}

// takeActed returns the held actions as the prefix of a turn's message, and forgets them.
func (d *Desk) takeActed() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	items, dropped := d.acted, d.actedDropped
	d.acted, d.actedDropped = nil, 0
	return actedPrefix(items, dropped)
}

// actedPrefix is what the person did from the web page, told to the desk ahead of its
// next message. It asks for nothing: these are done, not requests.
func actedPrefix(items []string, dropped int) string {
	if len(items) == 0 {
		return ""
	}
	msg := "[Shepherd] Since your last turn the person did this from the web page:\n- " + strings.Join(items, "\n- ")
	if dropped > 0 {
		msg += fmt.Sprintf("\n(and %d earlier action(s), left out)", dropped)
	}
	return msg + "\nThese are done; do not repeat them. The person's message, if any, follows.\n\n"
}

// mayWakeLocked says whether desk.wake lets the desk take a turn on its own now.
func (d *Desk) mayWakeLocked() bool {
	switch d.m.o.Wake() {
	case config.WakeAlways:
		return true
	case config.WakeNever:
		return false
	}
	return len(d.subs) > 0 || (!d.lastDetach.IsZero() && time.Since(d.lastDetach) < d.m.o.Grace)
}

func (d *Desk) wakeLocked() {
	select {
	case d.kick <- struct{}{}:
	default:
	}
}

// loop runs turns one at a time until the manager closes.
func (d *Desk) loop() {
	defer d.m.wg.Done()
	for {
		t, ok := d.next()
		if !ok {
			return
		}
		d.run(t)
	}
}

// next waits for the next turn: the person's first, then a digest of the swarm's events
// when the desk may wake.
func (d *Desk) next() (turn, bool) {
	for {
		d.mu.Lock()
		if d.m.ctx.Err() != nil {
			d.mu.Unlock()
			return turn{}, false
		}
		if len(d.queue) > 0 {
			t := d.queue[0]
			d.queue = d.queue[1:]
			d.mu.Unlock()
			return t, true
		}
		if len(d.pending) > 0 && d.mayWakeLocked() {
			lines := make([]string, len(d.pending))
			topics := make([]int64, len(d.pending))
			for i, p := range d.pending {
				lines[i], topics[i] = p.text, p.topic
			}
			text := digest(lines, d.dropped, d.waitingLine(), d.keepGoingOn(d.m.ctx))
			d.pending, d.dropped = nil, 0
			e, err := d.appendLocked(d.m.ctx, store.DeskEvent{Kind: store.DeskSystem, Text: text, Origin: store.DeskBySystem})
			d.mu.Unlock()
			if err != nil {
				d.m.o.Log.Error("desk: record a wake-up", "workspace", d.ws.Name, "err", err)
				continue
			}
			t := turn{id: e.Turn, origin: store.DeskBySystem, text: text}
			t.topic, t.others = d.fileEventTurn(d.m.ctx, e.Turn, topics)
			return t, true
		}
		d.mu.Unlock()
		// Waiting events may become deliverable when the grace period after the last
		// client left runs out (they then wait for the next) or a client attaches.
		select {
		case <-d.kick:
		case <-d.m.ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

// digest is the one message the swarm's waiting events become.
//
// It ends with the tagged "Waiting on you (N)" line when anything waits, as context for
// the desk and never to be pasted into its reply; waiting is "" when nothing does.
func digest(items []string, dropped int, waiting string, keepGoing bool) string {
	msg := "[Shepherd] Since your last turn:\n- " + strings.Join(items, "\n- ")
	if dropped > 0 {
		msg += fmt.Sprintf("\n(and %d earlier event(s), left out)", dropped)
	}
	msg += "\n" + EventInstruction(keepGoing)
	if waiting != "" {
		msg += "\n" + waiting + "\n" + WaitingInstruction
	}
	return msg
}

// waitingLine is the tagged line of what waits on the person in this workspace, or ""
// when nothing does or it cannot be read.
func (d *Desk) waitingLine() string {
	o := d.m.o
	if o.Config == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(d.m.ctx, 5*time.Second)
	defer cancel()
	w, err := waiting.Collect(ctx, o.Store, o.Config(), d.ws.ID)
	if err != nil {
		o.Log.Warn("desk: read what waits on the person", "workspace", d.ws.Name, "err", err)
		return ""
	}
	if qs, err := o.Store.Questions(ctx, d.ws.ID, store.QuestionOpen); err == nil {
		w.AddQuestions(qs)
	}
	if held, err := waiting.MergedArtifactItems(ctx, o.Store, d.ws.ID); err != nil {
		o.Log.Warn("desk: merged lanes with artifacts", "workspace", d.ws.Name, "err", err)
	} else if len(held) > 0 {
		w.AddItems(held)
	}
	return w.Line()
}

// run runs one turn and records what it produced.
func (d *Desk) run(t turn) {
	o := d.m.o
	ctx, stop := context.WithCancel(d.m.ctx)
	defer stop()
	d.mu.Lock()
	d.cur, d.stop, d.interrupted = &t, stop, false
	gen := d.gen
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.cur, d.stop = nil, nil
		d.mu.Unlock()
	}()
	bg := context.WithoutCancel(ctx)
	add := func(kind, text string) {
		if _, err := d.append(bg, store.DeskEvent{Kind: kind, Text: text, Turn: t.id, Origin: t.origin}); err != nil {
			o.Log.Error("desk: record", "kind", kind, "err", err)
		}
	}
	end := func(how string) {
		add(store.DeskTurnEnd, how)
		if err := o.Store.TrimDeskEvents(bg, d.ws.ID, o.Keep); err != nil {
			o.Log.Error("desk: trim history", "err", err)
		}
	}
	add(store.DeskTurnStart, "")

	session := d.Session(bg)
	fresh := session == ""
	seed := ""
	if fresh {
		var err error
		if session, err = newUUID(); err != nil {
			add(store.DeskError, err.Error())
			end("failed")
			return
		}
		// A new session after earlier turns, rotated or started by the person, starts
		// from a summary.
		if seed, err = d.seed(ctx, t.id); err != nil {
			o.Log.Error("desk: build the summary", "workspace", d.ws.Name, "err", err)
		}
	}
	tok, err := o.Tokens.MintDeskTurn(t.origin == store.DeskByHuman, session, t.id)
	if err != nil {
		add(store.DeskError, "the desk's token: "+err.Error())
		end("failed")
		return
	}
	defer o.Tokens.RevokeToken(tok)

	var partial strings.Builder
	produced := false
	tokens, total := 0, 0.0
	var stats *store.Usage
	spent := 0.0 // what this turn added to the desk's cost
	emit := func(l Line) {
		switch l.Kind {
		case KindStats:
			var u store.Usage
			if json.Unmarshal([]byte(l.Text), &u) == nil {
				stats = &u
			}
			return
		case KindUsage:
			tokens, _ = strconv.Atoi(l.Text)
			return
		case KindPartial:
			partial.WriteString(l.Text)
			d.live(store.DeskEvent{WorkspaceID: d.ws.ID, Kind: KindPartial, Text: l.Text, Turn: t.id, Origin: t.origin, Created: time.Now().UTC()})
			return
		case store.DeskCost:
			total, _ = strconv.ParseFloat(l.Text, 64)
			spent = d.cost(bg, session, l.Text, add)
			return
		}
		partial.Reset()
		produced = true
		add(l.Kind, redact.String(l.Text))
	}
	model := o.Model(bg)
	cfg := o.Config().Desk
	message := t.text
	d.mu.Lock()
	if d.carry != "" && !fresh {
		message = d.carry + message
	}
	d.carry = ""
	d.mu.Unlock()
	message = d.takeActed() + d.topicHead(bg, t) + message
	err = o.Agent.Turn(ctx, Spec{Session: session, New: fresh, Message: message, Dir: d.ws.Path, Model: model,
		Env: []string{dispatch.EnvToken + "=" + tok, dispatch.EnvURL + "=" + o.Tokens.URL()}, Seed: seed, ToolChars: cfg.ToolOutputCap()}, emit)
	if rest := strings.TrimSpace(partial.String()); rest != "" {
		// The stream ended without the whole reply: keep what came.
		add(store.DeskAssistant, redact.String(rest))
		produced = true
	}
	if stats != nil {
		d.usage(bg, t.id, *stats, spent)
	}
	d.mu.Lock()
	interrupted, renewed := d.interrupted, d.gen != gen
	d.mu.Unlock()
	// A new session exists once the agent has said anything in it.
	if fresh && !renewed && (err == nil || produced) {
		if serr := o.Store.SetSetting(bg, settingSession(d.ws.ID), session); serr != nil {
			o.Log.Error("desk: keep the session", "err", serr)
		}
	}
	switch {
	case interrupted:
		end("interrupted")
	case d.m.ctx.Err() != nil:
		end(stoppedEnd)
	case err != nil:
		add(store.DeskError, "the desk: "+redact.String(err.Error()))
		end("failed")
	default:
		// The agent has exited: rotate before the turn is seen to end, so the next turn
		// finds the session already ended.
		if why := rotateWhy(o.Config().Desk, tokens, total); why != "" {
			d.rotate(bg, gen, session, why)
		}
		end("done")
	}
}

// rotateWhy says why a session that has just finished a turn should end, or "".
func rotateWhy(c config.Desk, tokens int, total float64) string {
	if at := c.RotateAt(); at > 0 && tokens > at {
		return fmt.Sprintf("its context reached %d tokens (desk.rotate_tokens %d)", tokens, at)
	}
	if usd := c.RotateUSD(); usd > 0 && total > usd {
		return fmt.Sprintf("it has cost $%.2f (desk.rotate_cost $%.2f)", total, usd)
	}
	return ""
}

// rotate ends the session at the end of a turn: the next turn, queued or still to come,
// starts a new one seeded with a summary. The thread, the queue and the swarm's waiting
// events are left as they are.
func (d *Desk) rotate(ctx context.Context, gen int, session, why string) {
	o := d.m.o
	d.mu.Lock()
	defer d.mu.Unlock()
	// A new conversation started meanwhile has already ended this session.
	if d.gen != gen {
		return
	}
	if cur, _ := o.Store.Setting(ctx, settingSession(d.ws.ID)); cur != session {
		return
	}
	if err := o.Store.SetSetting(ctx, settingSession(d.ws.ID), ""); err != nil {
		o.Log.Error("desk: rotate", "workspace", d.ws.Name, "err", err)
		return
	}
	o.Log.Info("desk: rotated its session", "workspace", d.ws.Name, "why", why)
	text := fmt.Sprintf("front desk (%s): started a new session, seeded with a summary, because %s", d.ws.Name, why)
	if err := o.Store.AddFeed(ctx, FeedRotated, text, d.ws.ID); err != nil {
		o.Log.Error("desk: record the rotation", "err", err)
	}
}

// seed is the summary a new session starts from, or "" when the conversation has no
// earlier turn to continue.
func (d *Desk) seed(ctx context.Context, turn int64) (string, error) {
	// What the person did from the page is not an earlier turn: a first session after
	// only those has nothing to be summarised from.
	earlier, err := d.m.o.Store.DeskHistory(ctx, d.ws.ID, turn, 50)
	if err != nil || !slices.ContainsFunc(earlier, func(e store.DeskEvent) bool { return e.Kind != store.DeskAction }) {
		return "", err
	}
	in, err := d.gather(ctx, turn)
	if err != nil {
		return "", err
	}
	c := in.cfg.Desk
	notes := d.notes(ctx, c.SummaryModel, in)
	out := in.render(c.SummaryCap(), notes)
	d.m.o.Log.Info("desk: seeded a new session with a summary", "workspace", d.ws.Name, "chars", len(out), "notes", notes != "")
	return out, nil
}

// notes asks desk.summary_model for a paragraph on the conversation's open threads, from
// the summary's own records and quoted turns. Any failure leaves them out.
func (d *Desk) notes(ctx context.Context, model string, in summaryInput) string {
	o := d.m.o
	n, ok := o.Agent.(Noter)
	if model == "" || !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, notesTimeout)
	defer cancel()
	prompt := "Below is a summary of the state of a coordinator's work and the last turns of its conversation with a person. " +
		"Write one short paragraph (at most 120 words) of notes for the coordinator's successor: what the person is trying to get done, " +
		"commitments made and not yet kept, and open threads. Facts only, no advice, no greeting, no headings.\n\n" + in.render(in.cfg.Desk.SummaryCap(), "")
	text, usd, err := n.Notes(ctx, model, d.ws.Path, prompt)
	if usd > 0 {
		d.spend(context.WithoutCancel(ctx), usd)
	}
	if err != nil {
		o.Log.Warn("desk: the summary's notes", "workspace", d.ws.Name, "err", err)
		return ""
	}
	return redact.String(text)
}

// cost records what the turn added to the session's cost. Claude Code reports the
// session's total, so the turn's own is the difference from the last total recorded for
// this session (dispatch.SessionDelta).
func (d *Desk) cost(ctx context.Context, session, total string, add func(kind, text string)) float64 {
	o := d.m.o
	usd, err := strconv.ParseFloat(total, 64)
	if err != nil || usd <= 0 {
		return 0
	}
	before := 0.0
	if last, _ := o.Store.Setting(ctx, settingCost(d.ws.ID)); last != "" {
		if s, v, ok := strings.Cut(last, " "); ok && s == session {
			before, _ = strconv.ParseFloat(v, 64)
		}
	}
	o.Store.SetSetting(ctx, settingCost(d.ws.ID), session+" "+total)
	delta := dispatch.SessionDelta(usd, before)
	if delta <= 0 {
		return 0
	}
	d.spend(ctx, delta)
	add(store.DeskCost, strconv.FormatFloat(delta, 'f', -1, 64))
	return delta
}

// usage records a turn's tokens and context beside what it cost.
func (d *Desk) usage(ctx context.Context, turn int64, u store.Usage, usd float64) {
	o := d.m.o
	u.Day, u.Kind, u.WorkspaceID, u.Turn, u.Agent, u.CostUSD = dispatch.Today(), store.UsageDesk, d.ws.ID, turn, "claude", usd
	if err := o.Store.AddUsage(ctx, u); err != nil {
		o.Log.Error("desk: record usage", "err", err)
	}
}

// spend records what the desk cost.
func (d *Desk) spend(ctx context.Context, usd float64) {
	o := d.m.o
	sp := store.Spend{Source: store.OriginDesk, USD: usd}
	var err error
	if o.Spend != nil {
		err = o.Spend(ctx, sp)
	} else {
		sp.Day = dispatch.Today()
		err = o.Store.AddSpend(ctx, sp)
	}
	if err != nil {
		o.Log.Error("desk: record spend", "err", err)
	}
}

func (d *Desk) append(ctx context.Context, e store.DeskEvent) (store.DeskEvent, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.appendLocked(ctx, e)
}

// appendLocked stores an event and tells the streams.
func (d *Desk) appendLocked(ctx context.Context, e store.DeskEvent) (store.DeskEvent, error) {
	e.WorkspaceID = d.ws.ID
	e, err := d.m.o.Store.AddDeskEvent(ctx, e)
	if err != nil {
		return e, err
	}
	close(d.changed)
	d.changed = make(chan struct{})
	return e, nil
}

// live sends a partial reply to every client following, dropping it for one too slow
// to keep up: the whole reply is stored and follows.
func (d *Desk) live(e store.DeskEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for s := range d.subs {
		select {
		case s.Live <- e:
		default:
		}
	}
}

// stoppedEnd ends a turn the daemon's stop cut short.
const stoppedEnd = "interrupted: the daemon stopped"

// recover closes the turns a stopped daemon left open, so a client never waits on one,
// and keeps the message of the last one for the next turn.
func (d *Desk) recover(ctx context.Context) error {
	recent, err := d.m.o.Store.DeskHistory(ctx, d.ws.ID, 0, 200)
	if err != nil {
		return err
	}
	type opening struct{ text, origin, end string }
	turns := map[int64]*opening{}
	var all, order []int64
	for _, e := range recent {
		switch e.Kind {
		case store.DeskUser, store.DeskSystem:
			turns[e.Turn] = &opening{text: e.Text, origin: e.Origin}
			all, order = append(all, e.Turn), append(order, e.Turn)
		case store.DeskTurnEnd:
			if t := turns[e.Turn]; t != nil {
				t.end = e.Text
			}
		case store.DeskNew:
			order = nil // what came before belongs to another session
		}
	}
	for _, id := range all {
		if t := turns[id]; t.end == "" {
			if _, err := d.append(ctx, store.DeskEvent{Kind: store.DeskTurnEnd, Turn: id, Origin: t.origin, Text: stoppedEnd}); err != nil {
				return err
			}
			t.end = stoppedEnd
		}
	}
	// The last turn, cut short by the stop: the agent was killed and may never have
	// recorded the message in its session, so the next turn brings it along.
	if n := len(order); n > 0 {
		if t := turns[order[n-1]]; t.end == stoppedEnd {
			who := "the person"
			if t.origin == store.DeskBySystem {
				who = "Shepherd"
			}
			d.carry = fmt.Sprintf("[Shepherd] The daemon restarted during your previous turn, so you may not have seen this message from %s, or not finished answering it:\n%s\n\nThe new message follows. Take both into account.\n\n", who, t.text)
		}
	}
	return nil
}
