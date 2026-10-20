package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/client"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// shepherd next: the list of work that is planned but not being acted on. Every command
// goes through the daemon's API; approving, deferring, dropping, finishing and ordering
// are the person's, and the daemon refuses them from the front desk outside a turn the
// person started.
func runNext(ctx context.Context, env Env, args []string) error {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := flags("next "+sub, env)
	asJSON := fs.Bool("json", false, "print JSON")
	all := fs.Bool("all", false, "list: include dropped and done items")
	approved := fs.Bool("approved", false, "add: start as approved")
	propose := fs.Bool("propose", false, "add: from the front desk, put it up for approval")
	repo := fs.String("repo", "", "the repo, by its name in the workspace")
	plan := fs.String("plan", "", "the plan doc, a path under the repo")
	lane := fs.String("lane", "", "the lane's name (a hint for add and update; the lane to link for link)")
	note := fs.String("note", "", "a note")
	title := fs.String("title", "", "update: a new title")
	state := fs.String("state", "", "update: planned, approved, deferred, dropped or done; list: states to show, comma separated (approved, started, planned, proposed, deferred, todo, blocked)")
	topic := fs.String("topic", "", "list: only items whose plan, lane or area contains this")
	text := fs.String("text", "", "list: only items whose title, note or done-when contains this")
	matrix := fs.String("matrix", "", "list: the read-only matrix rows to show: none, or a repo's name (default all)")
	compact := fs.Bool("compact", false, "list: one line per item")
	move := fs.String("move", "", "update: top, up or down")
	doneWhen := fs.String("done-when", "", "add, update: one line, how to tell the item is done")
	depends := fs.String("depends", "", "add, update: ids of the items this one needs done first, comma separated (empty clears)")
	dismiss := fs.Bool("dismiss", false, "update: dismiss the item's done? suggestion (still needed)")
	dry := fs.Bool("dry-run", false, "seed: say what would be added")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	switch sub {
	case "list", "ls":
		if len(pos) > 0 {
			return errUsage
		}
		l, err := c.NextItems(ctx, *all)
		if err != nil {
			return err
		}
		f := nextFilter{Repo: *repo, Topic: *topic, Text: *text, Matrix: *matrix}
		if f.States, err = parseNextStates(*state); err != nil {
			return err
		}
		l = f.apply(l)
		if *asJSON {
			return printJSON(env, l)
		}
		writeNextList(env.Stdout, l, *compact)
		return nil
	case "show":
		id, err := nextID(pos, 1)
		if err != nil {
			return err
		}
		v, err := c.NextItem(ctx, id)
		if err != nil {
			return err
		}
		if *asJSON {
			return printJSON(env, v)
		}
		writeNextItem(env.Stdout, v)
		return nil
	case "add":
		if len(pos) != 1 {
			return errUsage
		}
		req := api.NextAdd{Title: pos[0], Repo: *repo, Plan: *plan, Lane: *lane, Note: *note, Propose: *propose, DoneWhen: *doneWhen}
		if set["depends"] {
			if req.DependsOn, err = parseDepends(*depends); err != nil {
				return err
			}
		}
		if *approved {
			req.State = store.NextApproved
		}
		v, err := c.AddNextItem(ctx, req)
		if err != nil {
			return err
		}
		return printNextChange(env, v, *asJSON, "added")
	case "approve", "defer", "drop", "done":
		id, err := nextID(pos, 1)
		if err != nil {
			return err
		}
		to := map[string]string{"approve": store.NextApproved, "defer": store.NextDeferred,
			"drop": store.NextDropped, "done": store.NextDone}[sub]
		v, err := c.UpdateNextItem(ctx, id, api.NextUpdate{State: &to, Note: optional(set, "note", *note)})
		if err != nil {
			return err
		}
		return printNextChange(env, v, *asJSON, "now "+to)
	case "move":
		if len(pos) != 2 {
			return errUsage
		}
		id, err := nextID(pos[:1], 1)
		if err != nil {
			return err
		}
		v, err := c.UpdateNextItem(ctx, id, api.NextUpdate{Move: pos[1]})
		if err != nil {
			return err
		}
		return printNextChange(env, v, *asJSON, "moved "+pos[1])
	case "link":
		id, err := nextID(pos, 1)
		if err != nil {
			return err
		}
		if *lane == "" {
			return errors.New("which lane? pass --lane NAME (the lane's name, in the item's repo or --repo)")
		}
		h, err := locate(ctx, env, c, *repo)
		if err != nil {
			return err
		}
		target, err := findLane(ctx, c, h, *lane)
		if err != nil {
			return err
		}
		v, err := c.UpdateNextItem(ctx, id, api.NextUpdate{LaneID: &target.ID})
		if err != nil {
			return err
		}
		return printNextChange(env, v, *asJSON, "linked to lane "+target.Name)
	case "update", "edit":
		id, err := nextID(pos, 1)
		if err != nil {
			return err
		}
		req := api.NextUpdate{Move: *move,
			Title: optional(set, "title", *title), Repo: optional(set, "repo", *repo), Plan: optional(set, "plan", *plan),
			Lane: optional(set, "lane", *lane), Note: optional(set, "note", *note), State: optional(set, "state", *state),
			DoneWhen: optional(set, "done-when", *doneWhen)}
		if set["depends"] {
			deps, err := parseDepends(*depends)
			if err != nil {
				return err
			}
			if deps == nil {
				deps = []int64{}
			}
			req.DependsOn = &deps
		}
		if *dismiss {
			none := ""
			req.Suggestion = &none
		}
		v, err := c.UpdateNextItem(ctx, id, req)
		if err != nil {
			return err
		}
		return printNextChange(env, v, *asJSON, "updated")
	case "seed":
		if len(pos) > 0 {
			return errUsage
		}
		return nextSeed(ctx, env, c, *repo, *dry)
	}
	return errUsage
}

// parseDepends reads "3,7" as item ids; an empty list is none.
func parseDepends(s string) ([]int64, error) {
	var out []int64
	for _, f := range strings.Split(s, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(f, "#"), 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("--depends takes item ids like 3,7, not %q", f)
		}
		out = append(out, id)
	}
	return out, nil
}

// blockedLine is "blocked on #3, #7" for the items an item still waits on.
func blockedLine(v api.NextItemView) string {
	if len(v.BlockedOn) == 0 {
		return ""
	}
	parts := make([]string, len(v.BlockedOn))
	for i, b := range v.BlockedOn {
		parts[i] = fmt.Sprintf("#%d", b.ID)
		if b.State == store.NextDropped {
			parts[i] += " (dropped: re-plan or drop this too)"
		}
	}
	return "blocked on " + strings.Join(parts, ", ")
}

// optional is the flag's value when it was passed, nil when it was not, so an empty
// value can clear a field.
func optional(set map[string]bool, name, v string) *string {
	if !set[name] {
		return nil
	}
	return &v
}

func nextID(pos []string, n int) (int64, error) {
	if len(pos) != n {
		return 0, errUsage
	}
	id, err := strconv.ParseInt(pos[0], 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("item id %q is not a number", pos[0])
	}
	return id, nil
}

func printNextChange(env Env, v api.NextItemView, asJSON bool, what string) error {
	if asJSON {
		return printJSON(env, v)
	}
	fmt.Fprintf(env.Stdout, "item %d %s: %s\n", v.ID, what, v.Title)
	return nil
}

// nextMark is the state as the list shows it: "proposed" for an item the person has not
// approved that someone else added.
func nextMark(v api.NextItemView) string {
	if v.Proposed {
		return "proposed"
	}
	return v.State
}

// nextFilter narrows a next-up list: every field set must match. Matching is case
// insensitive and by substring, except States and the repo names, which are exact.
type nextFilter struct {
	Repo, Topic, Text string
	// Matrix is "" (all rows), "none", or the one repo whose matrix rows show.
	Matrix string
	States []string
}

var nextListStates = []string{"approved", "started", "planned", "proposed", "deferred", "dropped", "done", "todo", "blocked"}

func parseNextStates(s string) ([]string, error) {
	var out []string
	for _, st := range strings.Split(s, ",") {
		if st = strings.ToLower(strings.TrimSpace(st)); st == "" {
			continue
		}
		if !slices.Contains(nextListStates, st) {
			return nil, fmt.Errorf("unknown state %q: use %s", st, strings.Join(nextListStates, ", "))
		}
		out = append(out, st)
	}
	return out, nil
}

func (f nextFilter) match(v api.NextItemView) bool {
	if f.Repo != "" && !strings.EqualFold(v.Repo, f.Repo) {
		return false
	}
	if len(f.States) > 0 && !slices.Contains(f.States, nextMark(v)) {
		return false
	}
	has := func(q string, fields ...string) bool {
		for _, x := range fields {
			if strings.Contains(strings.ToLower(x), strings.ToLower(q)) {
				return true
			}
		}
		return false
	}
	if f.Topic != "" && !has(f.Topic, v.Plan, v.LaneName, v.Area) {
		return false
	}
	return f.Text == "" || has(f.Text, v.Title, v.Note, v.DoneWhen)
}

// apply returns the list with only the items and matrix rows the filter keeps. NextUp
// stays only if its item does.
func (f nextFilter) apply(l api.NextList) api.NextList {
	var out api.NextList
	for _, v := range l.Items {
		if f.match(v) {
			out.Items = append(out.Items, v)
			if v.ID == l.NextUp {
				out.NextUp = l.NextUp
			}
		}
	}
	if f.Matrix == "none" {
		return out
	}
	m := f
	if f.Matrix != "" {
		m.Repo = f.Matrix
	}
	for _, v := range l.Matrix {
		if m.match(v) {
			out.Matrix = append(out.Matrix, v)
		}
	}
	return out
}

func writeNextList(w io.Writer, l api.NextList, compact bool) {
	if len(l.Items) == 0 && len(l.Matrix) == 0 {
		fmt.Fprintln(w, "Nothing on the next-up list. Add one with: shepherd next add \"title\"")
		return
	}
	if len(l.Items) == 0 {
		fmt.Fprintln(w, "Nothing stored on the next-up list. Add one with: shepherd next add \"title\"")
	}
	for _, v := range l.Items {
		mark := " "
		if v.ID == l.NextUp {
			mark = ">"
		}
		fmt.Fprintf(w, "%s %3d  %-9s %s", mark, v.ID, nextMark(v), v.Title)
		if v.Repo != "" {
			fmt.Fprintf(w, "  (%s)", v.Repo)
		}
		fmt.Fprintln(w)
		if compact {
			continue
		}
		if v.LaneName != "" {
			line := "lane " + v.LaneName + " " + v.LaneState
			if v.Running {
				line += ", running"
			}
			if v.MR != "" {
				line += ", MR " + v.MR
				if v.MRState != "" {
					line += " " + v.MRState
				}
			}
			fmt.Fprintf(w, "        %s\n", line)
		}
		if v.Why != "" {
			fmt.Fprintf(w, "        %s: decide whether it is deferred or dropped\n", v.Why)
		}
		if line := blockedLine(v); line != "" {
			fmt.Fprintf(w, "        %s\n", line)
		}
		if v.Suggestion != "" {
			fmt.Fprintf(w, "        suggestion: %s (accept with: shepherd next done %d; dismiss with: shepherd next update %d --dismiss)\n", v.Suggestion, v.ID, v.ID)
		}
	}
	if l.NextUp != 0 {
		fmt.Fprintln(w, "\n> is next: the first approved item with no lane.")
	}
	if len(l.Matrix) > 0 {
		fmt.Fprintln(w, "\nFrom work matrices (docs/WORK.md, read-only; edit the file):")
		for _, v := range l.Matrix {
			fmt.Fprintf(w, "  %-9s %s  %s  (%s)", v.State, v.RowID, v.Title, v.Repo)
			if v.Lane != "" {
				fmt.Fprintf(w, "  lane %s", v.Lane)
			}
			if v.MR != "" {
				fmt.Fprintf(w, "  PR %s", v.MR)
			}
			fmt.Fprintln(w)
		}
	}
}

func writeNextItem(w io.Writer, v api.NextItemView) {
	fmt.Fprintf(w, "item %d  %s\n  %s\n", v.ID, nextMark(v), v.Title)
	row := func(k, val string) {
		if val != "" {
			fmt.Fprintf(w, "  %-9s %s\n", k, val)
		}
	}
	row("repo", v.Repo)
	row("plan", v.Plan)
	row("lane hint", v.Lane)
	if v.LaneName != "" {
		row("lane", fmt.Sprintf("%s (%s, %s)", v.LaneName, v.LaneRepo, v.LaneState))
	}
	row("MR", strings.TrimSpace(v.MR+" "+v.MRState))
	row("MR link", v.MRURL)
	row("chosen", v.Stored)
	row("why", v.Why)
	row("done when", v.DoneWhen)
	if len(v.DependsOn) > 0 {
		ids := make([]string, len(v.DependsOn))
		for i, d := range v.DependsOn {
			ids[i] = "#" + strconv.FormatInt(d, 10)
		}
		row("depends", strings.Join(ids, ", "))
	}
	row("blocked", strings.TrimPrefix(blockedLine(v), "blocked on "))
	row("suggest", v.Suggestion)
	row("note", v.Note)
	row("source", v.Source)
	row("added", age(v.Created)+" ago")
	if v.DoneAt != nil {
		row("done", age(*v.DoneAt)+" ago")
	}
}

// nextSeedItem is one item known from docs/backlog-next.md, section 5.
type nextSeedItem struct {
	title, state, plan string
}

// nextSeeds are the items the plan lists for the first seeding, in list order. Seeding
// is the person's step after the core lands: nothing adds them but `shepherd next seed`.
var nextSeeds = []nextSeedItem{
	{"Lane diff step 2: the web Changes tab and editor link", store.NextApproved, "docs/lane-diff.md"},
	{"Lane diff step 3: gate record, badge and the run page's range view", store.NextApproved, "docs/lane-diff.md"},
	{"This backlog: the Next up core (table, API, CLI, next_* tools)", store.NextApproved, "docs/backlog-next.md"},
	{"Desk tool to widen a lane's scope when the person approves it in a decision", store.NextPlanned, ""},
	{"Answered decisions read with no time bound", store.NextPlanned, ""},
	{"Repo add part 2: findings kept in memory", store.NextPlanned, "docs/repo-add.md"},
	{"Timeline leftovers: g t in the Help dialog", store.NextPlanned, "docs/swarm-timeline.md"},
	{"Timeline leftovers: arcs between runs", store.NextPlanned, "docs/swarm-timeline.md"},
	{"Timeline leftovers: tokens in the tooltip", store.NextPlanned, "docs/swarm-timeline.md"},
	{"Timeline leftovers: arrow-key walking", store.NextPlanned, "docs/swarm-timeline.md"},
	{"Timeline phase 2: the daemon endpoint", store.NextPlanned, "docs/swarm-timeline.md"},
	{"Lane diff: the in-app editor", store.NextDeferred, "docs/lane-diff.md"},
	{"Lane diff: two-lane comparison", store.NextDeferred, "docs/lane-diff.md"},
	{"Lane diff: shepherd lane gate to run and record the gate in a lane", store.NextDeferred, "docs/lane-diff.md"},
	{"Backlog actions: ship from a card, first run of a never-run lane, per-lane fix-try count, multi-select", store.NextDeferred, "docs/backlog-actions.md"},
	{"Rail: run progress bar, per-project grouping, per-lane spend, board feed strip", store.NextDeferred, "docs/web-chat.md"},
	{"Terminal dock grouped by project", store.NextDeferred, "docs/web-projects.md"},
	{"Workspace-wide leases", store.NextPlanned, "docs/backlog-next.md"},
	{"GitHub mirror proofs; typed cross-repo work orders and triage", store.NextPlanned, "docs/backlog-next.md"},
}

// nextSeed adds the items the plan lists, skipping any whose title is already on the
// list (done and dropped ones too), so it can be run again.
func nextSeed(ctx context.Context, env Env, c *client.Client, repoFlag string, dry bool) error {
	repo := repoFlag
	if repo == "" {
		if h, err := locate(ctx, env, c, ""); err == nil && h.Repo != nil {
			repo = h.Repo.Name
		}
	}
	have, err := c.NextItems(ctx, true)
	if err != nil {
		return err
	}
	titles := map[string]bool{}
	for _, v := range have.Items {
		titles[v.Title] = true
	}
	added := 0
	for _, s := range nextSeeds {
		if titles[s.title] {
			continue
		}
		added++
		if dry {
			fmt.Fprintf(env.Stdout, "would add (%s) %s\n", s.state, s.title)
			continue
		}
		if _, err := c.AddNextItem(ctx, api.NextAdd{Title: s.title, State: s.state, Plan: s.plan, Repo: repo}); err != nil {
			return fmt.Errorf("%s: %w", s.title, err)
		}
	}
	verb := "added"
	if dry {
		verb = "would add"
	}
	fmt.Fprintf(env.Stdout, "%s %s; %d already on the list.\n", verb, plural(added, "item", "items"), len(nextSeeds)-added)
	return nil
}
