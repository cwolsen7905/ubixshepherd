package daemon

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/waiting"
)

// The keep-going drive: while the mode is on, every tick looks for work and starts it, so
// keeping going never depends on a client being attached or on the desk choosing to act.
// Every start passes through the same gate as the desk's (permit), so the spend cap, run
// cap, idle time, day end and per-lane starts hold as they always did. The drive starts
// agents and nothing else: it never answers a decision, approves, merges, pushes for a
// person, tags, releases or deploys.

// DriveStart is one run the drive asks for: the approved item, the lane to continue
// (the zero Lane means open a new one for the item) and the prompt built from the item.
type DriveStart struct {
	WorkspaceID int64
	Item        store.NextItem
	Lane        store.Lane
	Prompt      string
	// Action is KeepStartItem, KeepContinue or KeepRerun.
	Action string
}

// QuestionLink is what a question or decision is linked to when it is created. All
// zero means unlinked, and an unlinked question blocks nothing.
type QuestionLink struct {
	ItemID, LaneID, TopicID int64
}

// DriveSeams are the hooks the drive uses to reach the rest of the daemon.
type DriveSeams struct {
	// StartRun opens the lane if needed, links it to the item and starts the run. Unset,
	// the drive uses the daemon's own once it has a runner, and starts nothing before.
	StartRun func(ctx context.Context, req DriveStart) (store.Run, error)
	// LinkOf says what an open question is linked to. Unset, a question is linked to the
	// topic it was asked in, if any.
	LinkOf func(q store.Question) QuestionLink
	// Ready says whether an item can be started as it is; a non-nil error is why not,
	// and holds only that item. Unset with StartRun, every item is ready.
	Ready func(it store.NextItem) error
}

// Drive returns the controller's seams.
func (k *KeepGoing) Drive() *DriveSeams { return &k.drive }

// errVague is why an item is not started: nothing says what to do or when it is done.
var errVague = errors.New("it has no plan doc and no acceptance line: add one, so an agent knows what to do")

// driveItem is an approved item the drive considers, with what stops it, if anything.
type driveItem struct {
	item store.NextItem
	// lane is the item's open lane; the zero Lane means the item has none yet.
	lane   store.Lane
	action string
	// reasons are the item's own blockers, each saying what the person does about it.
	reasons []string
}

func (d driveItem) startable() bool { return d.action != "" && len(d.reasons) == 0 }

// linkOf reads a question's link through the seam, or from its topic.
func (k *KeepGoing) linkOf(q store.Question) QuestionLink {
	if f := k.drive.LinkOf; f != nil {
		return f(q)
	}
	return QuestionLink{TopicID: q.TopicID}
}

// assess lists the workspace's approved items that have no run going, in rank order,
// each with its own blockers: a dependency not done, an open decision or held request on
// its lane, a question linked to it, its lane or its topic. A question linked to nothing
// blocks nothing. An item with a lane is listed when it can be continued or is held;
// otherwise the lane's own work is under way.
func (k *KeepGoing) assess(ctx context.Context, ws int64) ([]driveItem, error) {
	s := k.s
	all, err := s.Store.NextItems(ctx)
	if err != nil {
		return nil, err
	}
	repos, err := s.Store.Repos(ctx, ws)
	if err != nil {
		return nil, err
	}
	mine := map[string]bool{}
	for _, r := range repos {
		mine[r.Name] = true
	}
	// known is every repo in any workspace: an item for one of another workspace's
	// repos is not this workspace's, but one for a repo in none is nobody's to start.
	known := map[string]bool{}
	wss, err := s.Store.Workspaces(ctx)
	if err != nil {
		return nil, err
	}
	for _, w := range wss {
		rs, err := s.Store.Repos(ctx, w.ID)
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			known[r.Name] = true
		}
	}
	var cands []store.NextItem
	for _, it := range all {
		if it.State == store.NextApproved && (it.Repo == "" || mine[it.Repo] || !known[it.Repo]) {
			cands = append(cands, it)
		}
	}
	if len(cands) == 0 {
		return nil, nil
	}
	states := s.effectiveStates(ctx, all)
	w, err := waiting.Collect(ctx, s.Store, s.LiveConfig(), ws)
	if err != nil {
		return nil, err
	}
	qs, err := s.Store.Questions(ctx, ws, store.QuestionOpen)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].Rank < cands[j].Rank })
	var out []driveItem
	for _, it := range cands {
		d := driveItem{item: it}
		if it.LaneID != 0 {
			lane, err := s.Store.Lane(ctx, it.LaneID)
			if err != nil || lane.State != store.LaneOpen {
				continue // the lane is gone or closed: the person's to look at, not the drive's
			}
			if running, err := s.Store.Runs(ctx, lane.ID, store.RunRunning, 1); err != nil || len(running) > 0 {
				continue
			}
			d.lane = lane
			d.action = k.laneAction(ctx, lane)
		} else {
			d.action = KeepStartItem
		}
		switch {
		case it.Repo == "":
			d.reasons = append(d.reasons, "it names no repo: set one")
		case !mine[it.Repo]:
			d.reasons = append(d.reasons, "repo "+it.Repo+" is not registered: register it")
		}
		for _, b := range states.blockedOn(it) {
			d.reasons = append(d.reasons, fmt.Sprintf("it depends on item %d %q, which is %s: finish it first", b.ID, b.Title, b.State))
		}
		if d.lane.ID != 0 {
			for _, wi := range w.Items {
				if wi.LaneID == d.lane.ID && wi.Kind != api.WaitingQuestion && wi.Kind != api.WaitingNext {
					d.reasons = append(d.reasons, stallReason(wi))
				}
			}
		}
		itemTopic, laneTopic := k.topicOf(ctx, store.TopicNextItem, it.ID), k.topicOf(ctx, store.TopicLane, d.lane.ID)
		for _, q := range qs {
			l := k.linkOf(q)
			if l.ItemID == it.ID || (d.lane.ID != 0 && l.LaneID == d.lane.ID) ||
				(l.TopicID != 0 && (l.TopicID == itemTopic || l.TopicID == laneTopic)) {
				d.reasons = append(d.reasons, fmt.Sprintf("question %d is waiting on you: reply to it", q.ID))
			}
		}
		if len(d.reasons) == 0 && d.action == "" {
			continue // an open lane with its work under way and nothing to drive
		}
		out = append(out, d)
	}
	return out, nil
}

// topicOf is the topic a thing belongs to, 0 when it is in none.
func (k *KeepGoing) topicOf(ctx context.Context, kind string, id int64) int64 {
	if id == 0 {
		return 0
	}
	l, err := k.s.Store.TopicOf(ctx, kind, fmt.Sprint(id))
	if err != nil {
		return 0
	}
	return l.TopicID
}

// laneAction is what the drive does for an open lane with no run going: start the first
// run of a lane that never had one, or re-run one a crash interrupted. A lane whose last
// run ended any other way is waiting on someone else, and gets "".
func (k *KeepGoing) laneAction(ctx context.Context, lane store.Lane) string {
	runs, err := k.s.Store.Runs(ctx, lane.ID, "", 1)
	if err != nil {
		return ""
	}
	if len(runs) == 0 {
		return KeepContinue
	}
	if r := runs[0]; r.State == store.RunInterrupted && r.Reason == "" {
		return KeepRerun
	}
	return ""
}

// drivePrompt is what the agent is asked to do for an item. The repo's brief is added
// by the runner, as for every run.
func drivePrompt(it store.NextItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Work on Next-up item %d: %s\n", it.ID, it.Title)
	if it.Plan != "" {
		fmt.Fprintf(&b, "\nThe plan is %s in the repo: read it first.\n", it.Plan)
	}
	if it.DoneWhen != "" {
		fmt.Fprintf(&b, "\nIt is done when: %s\n", it.DoneWhen)
	}
	if it.Note != "" {
		fmt.Fprintf(&b, "\nNote: %s\n", it.Note)
	}
	b.WriteString("\nStay inside the item. If you need a decision from the person, ask for it rather than guessing.")
	return b.String()
}

// driveOnce starts what it can in one workspace while the mode is on: continuations first
// (a lane's next step is already approved), then new items by rank, until the run slots
// are full or something refuses. One item in a repo per pass, as lanes in a repo would
// share its scope.
func (k *KeepGoing) driveOnce(ctx context.Context, ws int64) {
	start, ready := k.drive.StartRun, k.drive.Ready
	if start == nil {
		// The daemon's own start, once it has a runner and a fold to start with.
		if k.s.Runner == nil || k.s.Fold == nil {
			return
		}
		start, ready = k.s.driveStart, driveReady
	}
	k.driveMu.Lock()
	defer k.driveMu.Unlock()
	items, err := k.assess(ctx, ws)
	if err != nil {
		k.s.Log.Warn("keep-going: look for work", "workspace", ws, "err", err)
		return
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].lane.ID != 0 && items[j].lane.ID == 0 })
	usedRepo := map[string]bool{}
	for _, c := range items {
		if !c.startable() {
			continue
		}
		if c.lane.ID == 0 && usedRepo[c.item.Repo] {
			continue
		}
		if f := ready; f != nil && c.lane.ID == 0 {
			if err := f(c.item); err != nil {
				k.noteRefused(ctx, c, err.Error())
				continue
			}
		}
		if d := k.permit(ctx, ws, c.action, c.lane.ID); !d.OK {
			if c.lane.ID != 0 {
				continue // this lane is out of starts; another may not be
			}
			k.s.Log.Debug("keep-going: drive stopped", "workspace", ws, "reason", d.Reason)
			return
		}
		run, err := start(ctx, DriveStart{WorkspaceID: ws, Item: c.item, Lane: c.lane, Prompt: drivePrompt(c.item), Action: c.action})
		if err != nil {
			k.noteRefused(ctx, c, err.Error())
			continue
		}
		usedRepo[c.item.Repo] = true
		k.clearRefused(c.item.ID)
		k.recordAction(ctx, ws, c, run)
	}
}

// recordAction counts a start as activity and names it in the feed: the action, the
// item and the lane.
func (k *KeepGoing) recordAction(ctx context.Context, ws int64, c driveItem, run store.Run) {
	k.mu.Lock()
	if row, err := k.s.Store.KeepGoing(ctx, ws); err == nil {
		row.LastAction = k.Now().UTC()
		if err := k.s.Store.SetKeepGoing(ctx, row); err != nil {
			k.s.Log.Warn("keep-going: record activity", "err", err)
		}
	}
	k.mu.Unlock()
	name := fmt.Sprint(run.LaneID)
	if lane, err := k.s.Store.Lane(ctx, run.LaneID); err == nil {
		name = lane.Name
	}
	verb := map[string]string{KeepStartItem: "started", KeepContinue: "continued", KeepRerun: "re-ran an interrupted run of"}[c.action]
	k.s.postFeed(ctx, FeedKeepGoing, run.LaneID, redact.String(fmt.Sprintf("keep-going %s item %d %q on lane %s (run %d)", verb, c.item.ID, c.item.Title, name, run.ID)))
}

// noteRefused remembers why an item could not be started, for the stall alert, and
// tells the feed once per reason.
func (k *KeepGoing) noteRefused(ctx context.Context, c driveItem, why string) {
	why = redact.String(why)
	k.stallMu.Lock()
	again := k.refused[c.item.ID] == why
	k.refused[c.item.ID] = why
	k.stallMu.Unlock()
	if !again {
		k.s.postFeed(ctx, FeedKeepGoing, c.lane.ID, fmt.Sprintf("keep-going could not start item %d %q: %s", c.item.ID, c.item.Title, why))
	}
}

func (k *KeepGoing) clearRefused(id int64) {
	k.stallMu.Lock()
	delete(k.refused, id)
	k.stallMu.Unlock()
}

func (k *KeepGoing) refusedWhy(id int64) string {
	k.stallMu.Lock()
	defer k.stallMu.Unlock()
	return k.refused[id]
}

// driveReady holds an item no one has said what to do about: with neither a plan doc nor
// an acceptance line, an agent would guess.
func driveReady(it store.NextItem) error {
	if it.Plan == "" && it.DoneWhen == "" {
		return errVague
	}
	return nil
}

// driveStart is the daemon's StartRun: it opens the item's lane when it has none (scoped
// to the whole repo, so a second lane in that repo is refused by the lease and waits),
// links the item, and starts the run as a Shepherd-started one, so the daily budget holds
// it.
func (s *Server) driveStart(ctx context.Context, req DriveStart) (store.Run, error) {
	lane := req.Lane
	if lane.ID == 0 {
		var err error
		if lane, err = s.driveOpen(ctx, req); err != nil {
			return store.Run{}, err
		}
	}
	return s.Runner.Start(ctx, dispatch.StartRequest{LaneID: lane.ID, Prompt: req.Prompt, Auto: true})
}

// driveOpen opens the lane for an item and links them. If the run then fails to start,
// the lane stays linked and the next pass continues it.
func (s *Server) driveOpen(ctx context.Context, req DriveStart) (store.Lane, error) {
	repos, err := s.Store.Repos(ctx, req.WorkspaceID)
	if err != nil {
		return store.Lane{}, err
	}
	var repo store.Repo
	for _, r := range repos {
		if r.Name == req.Item.Repo {
			repo = r
		}
	}
	if repo.ID == 0 {
		return store.Lane{}, fmt.Errorf("repo %q is not registered", req.Item.Repo)
	}
	opened, err := s.Fold.Open(ctx, fold.OpenRequest{
		RepoID: repo.ID, Name: driveLaneName(req.Item), Scope: []string{"**"},
		Origin: store.Origin{Via: "keep-going", Detail: fmt.Sprintf("item %d", req.Item.ID)},
	})
	if err != nil {
		return store.Lane{}, err
	}
	lane := opened.Lane
	if _, err := s.linkLaneItem(ctx, lane, repo.Name, req.Item.ID); err != nil {
		s.Log.Warn("link a next item to its lane", "lane", lane.Name, "err", err)
	}
	s.Store.AddFeed(ctx, store.FeedLaneOpened, fmt.Sprintf("lane %s opened via keep-going (scope %s)", lane.Name, strings.Join(lane.Scope, ", ")), lane.ID)
	s.refreshView(lane.RepoID)
	return lane, nil
}

// driveLaneName is "item-<id>-<words of the title>": lowercase, short, unique by id.
func driveLaneName(it store.NextItem) string {
	var words []string
	for _, f := range strings.FieldsFunc(strings.ToLower(it.Title), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if words = append(words, f); len(words) == 4 {
			break
		}
	}
	name := fmt.Sprintf("item-%d", it.ID)
	if len(words) > 0 {
		name += "-" + strings.Join(words, "-")
	}
	if len(name) > 48 {
		name = strings.TrimRight(name[:48], "-")
	}
	return name
}

// SinceYouLeft is a placeholder that always errors until the summary lands in the next
// change. It is the one summary for the person coming back: items started, runs
// finished, MRs opened or merged, what failed, and why and when the mode stopped.
func (k *KeepGoing) SinceYouLeft(ctx context.Context, ws int64) (string, error) {
	return "", errors.New("keep-going summary: not implemented")
}
