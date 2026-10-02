package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/client"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// listFlag collects a repeatable flag; each value may also be comma-separated.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error {
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			*l = append(*l, p)
		}
	}
	return nil
}

func runLane(ctx context.Context, env Env, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "open":
		return laneOpen(ctx, env, args[1:])
	case "list", "ls":
		return laneList(ctx, env, args[1:])
	case "close":
		return laneClose(ctx, env, args[1:])
	case "run":
		return laneRun(ctx, env, args[1:])
	case "scope":
		return laneScope(ctx, env, args[1:])
	}
	return errUsage
}

func runFold(ctx context.Context, env Env, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "import":
		return foldImport(ctx, env, args[1:])
	case "view":
		return foldView(ctx, env, args[1:])
	case "gc":
	default:
		return errUsage
	}
	fs := flags("fold gc", env)
	asJSON := fs.Bool("json", false, "print JSON")
	if pos, err := parse(fs, args[1:]); err != nil {
		return err
	} else if len(pos) > 0 {
		return errUsage
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	here, err := locate(ctx, env, c, "")
	if err != nil {
		return err
	}
	stale, err := c.FoldGC(ctx, here.Workspace.ID)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(env, stale)
	}
	if len(stale) == 0 {
		fmt.Fprintln(env.Stdout, "No stale worktrees.")
		return nil
	}
	fmt.Fprintln(env.Stdout, "Stale worktrees (nothing removed; remote refs as of each repo's last fetch):")
	for _, s := range stale {
		label := s.Branch
		if s.Lane != "" {
			label = "lane " + s.Lane
		}
		fmt.Fprintf(env.Stdout, "  %s  %s\n      %s: %s\n", s.Repo, s.Path, orNone(label), s.Reason)
	}
	return nil
}

// here is where a command acts: always a workspace, and a repo when one was named or the
// command runs inside one.
type here struct {
	Workspace store.Workspace
	Repo      *store.Repo
	Lane      *store.Lane
	Repos     []store.Repo
}

// locate resolves the current directory, falling back to the only workspace when run
// from outside every workspace, and applies --repo.
func locate(ctx context.Context, env Env, c *client.Client, repoFlag string) (here, error) {
	res, err := c.Resolve(ctx, env.Cwd)
	if err != nil {
		return here{}, err
	}
	all, err := c.Workspaces(ctx)
	if err != nil {
		return here{}, err
	}
	var h here
	switch {
	case res.Workspace != nil:
		h.Workspace = *res.Workspace
	case len(all) == 1:
		h.Workspace = all[0].Workspace
	case len(all) == 0:
		return h, errors.New("no workspace yet; register one with: shepherd init ~/git")
	default:
		return h, errors.New("not inside a workspace; cd into one")
	}
	for _, w := range all {
		if w.ID == h.Workspace.ID {
			h.Repos = w.Repos
		}
	}
	if res.Workspace != nil && res.Workspace.ID == h.Workspace.ID {
		h.Repo, h.Lane = res.Repo, res.Lane
	}
	if repoFlag != "" {
		h.Repo, h.Lane = nil, nil
		for i := range h.Repos {
			if h.Repos[i].Name == repoFlag {
				h.Repo = &h.Repos[i]
			}
		}
		if h.Repo == nil {
			return h, fmt.Errorf("no repo %q in workspace %s", repoFlag, h.Workspace.Name)
		}
	}
	return h, nil
}

func laneOpen(ctx context.Context, env Env, args []string) error {
	fs := flags("lane open", env)
	var scope listFlag
	fs.Var(&scope, "scope", "paths the lane's work stays inside, as globs relative to the repo (repeatable)")
	branch := fs.String("branch", "", "branch name (default: the lane name)")
	repo := fs.String("repo", "", "repo, by its name in the workspace (default: the one you are in)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errUsage
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	h, err := locate(ctx, env, c, *repo)
	if err != nil {
		return err
	}
	if h.Repo == nil {
		return errors.New("which repo? run this inside one, or pass --repo")
	}
	lane, err := c.OpenLane(ctx, fold.OpenRequest{RepoID: h.Repo.ID, Name: pos[0], Branch: *branch, Scope: scope})
	if err != nil {
		return err
	}
	w := env.Stdout
	fmt.Fprintf(w, "opened lane %s in %s\n", lane.Name, h.Repo.Name)
	fmt.Fprintf(w, "  branch    %s (from %s)\n", lane.Branch, lane.Base)
	fmt.Fprintf(w, "  worktree  %s\n", lane.Worktree)
	fmt.Fprintf(w, "  scope     %s\n", strings.Join(lane.Scope, ", "))
	if len(lane.Shared) > 0 {
		fmt.Fprintf(w, "  shared    %s (held by this lane until it closes)\n", strings.Join(lane.Shared, ", "))
	}
	for _, n := range lane.Notes {
		fmt.Fprintf(w, "  note      %s\n", n)
	}
	fmt.Fprintf(w, "cd %s\n", shellQuote(lane.Worktree))
	return nil
}

func laneList(ctx context.Context, env Env, args []string) error {
	fs := flags("lane list", env)
	all := fs.Bool("all", false, "every repo in the workspace, even from inside one")
	asJSON := fs.Bool("json", false, "print JSON")
	repo := fs.String("repo", "", "only this repo")
	if pos, err := parse(fs, args); err != nil {
		return err
	} else if len(pos) > 0 {
		return errUsage
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	h, err := locate(ctx, env, c, *repo)
	if err != nil {
		return err
	}
	var repoID int64
	if h.Repo != nil && !*all {
		repoID = h.Repo.ID
	}
	lanes, err := c.Lanes(ctx, h.Workspace.ID, repoID)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(env, lanes)
	}
	if len(lanes) == 0 {
		fmt.Fprintln(env.Stdout, "No open lanes. Open one with: shepherd lane open <name> --scope '<globs>'")
		return nil
	}
	w := env.Stdout
	fmt.Fprintf(w, "%-20s %-24s %-8s %-6s %s\n", "REPO", "LANE", "STATE", "AGE", "SCOPE")
	for _, l := range lanes {
		mark := " "
		if h.Lane != nil && h.Lane.ID == l.ID {
			mark = "*"
		}
		fmt.Fprintf(w, "%-20s %-24s %-8s %-6s %s\n", l.Repo, mark+l.Name, l.State, age(l.Created), strings.Join(l.Scope, ", "))
	}
	return nil
}

func laneClose(ctx context.Context, env Env, args []string) error {
	fs := flags("lane close", env)
	force := fs.Bool("force", false, "close although git cannot see the branch merged (after a squash merge), discarding uncommitted changes")
	repo := fs.String("repo", "", "repo, by its name in the workspace")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return errUsage
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	h, err := locate(ctx, env, c, *repo)
	if err != nil {
		return err
	}
	var target *api.LaneView
	if len(pos) == 0 {
		if h.Lane == nil {
			return errors.New("which lane? run this inside its worktree, or name it")
		}
		target = &api.LaneView{Lane: *h.Lane}
	} else {
		var repoID int64
		if h.Repo != nil {
			repoID = h.Repo.ID
		}
		lanes, err := c.Lanes(ctx, h.Workspace.ID, repoID)
		if err != nil {
			return err
		}
		for i := range lanes {
			if lanes[i].Name == pos[0] {
				if target != nil {
					return fmt.Errorf("lane %s is open in %s and %s; pass --repo", pos[0], target.Repo, lanes[i].Repo)
				}
				target = &lanes[i]
			}
		}
		if target == nil {
			return fmt.Errorf("no open lane %s", pos[0])
		}
	}
	res, err := c.CloseLane(ctx, target.ID, *force)
	if err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "closed lane %s; removed %s\n", res.Lane.Name, res.Lane.Worktree)
	if res.BranchDeleted {
		fmt.Fprintf(env.Stdout, "  deleted local branch %s (it is in %s)\n", res.Lane.Branch, res.Lane.Base)
	}
	for _, n := range res.Notes {
		fmt.Fprintf(env.Stdout, "  %s\n", n)
	}
	return nil
}

func age(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// shellQuote quotes a path for pasting into a POSIX shell when it needs it.
func shellQuote(s string) string {
	if !strings.ContainsAny(s, " '\"$`\\") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func foldImport(ctx context.Context, env Env, args []string) error {
	fs := flags("fold import", env)
	repo := fs.String("repo", "", "repo, by its name in the workspace (default: the one you are in)")
	file := fs.String("file", "", "the coordination file (default: AGENTS-COORD.md in the repo)")
	apply := fs.Bool("apply", false, "import, rather than show what would be imported")
	if pos, err := parse(fs, args); err != nil {
		return err
	} else if len(pos) > 0 {
		return errUsage
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	h, err := locate(ctx, env, c, *repo)
	if err != nil {
		return err
	}
	if h.Repo == nil {
		return errors.New("which repo? run this inside one, or pass --repo")
	}
	plan, err := c.FoldImport(ctx, api.FoldImport{RepoID: h.Repo.ID, File: *file, Apply: *apply})
	if err != nil {
		return err
	}
	w := env.Stdout
	var imp, skip []fold.ImportItem
	for _, it := range plan.Items {
		if it.Action == "import" {
			imp = append(imp, it)
		} else {
			skip = append(skip, it)
		}
	}
	verb := "Would import"
	if plan.Applied {
		verb = "Imported"
	}
	fmt.Fprintf(w, "%s %d lane(s) into %s:\n", verb, len(imp), h.Repo.Name)
	for _, it := range imp {
		fmt.Fprintf(w, "  %-34s %s\n      scope %s\n", it.Lane, it.Agent, strings.Join(it.Scope, ", "))
		if it.Why != "" {
			fmt.Fprintf(w, "      note: %s\n", it.Why)
		}
	}
	if len(skip) > 0 {
		fmt.Fprintf(w, "\nSkipped %d worktree(s):\n", len(skip))
		for _, it := range skip {
			fmt.Fprintf(w, "  %-34s %s\n", orNone(it.Branch), it.Why)
		}
	}
	if len(plan.Unclaimed) > 0 {
		fmt.Fprintf(w, "\nRows with no worktree (claims with nothing in flight): %s\n", strings.Join(plan.Unclaimed, "; "))
	}
	if len(plan.Overlaps) > 0 {
		fmt.Fprintf(w, "\nOverlapping scopes among these lanes (the honour system allowed them; Shepherd will refuse new ones):\n")
		for _, o := range plan.Overlaps {
			fmt.Fprintf(w, "  %s\n", o)
		}
	}
	if !plan.Applied {
		fmt.Fprintln(w, "\nNothing changed. Run again with --apply to import.")
	}
	return nil
}

func foldView(ctx context.Context, env Env, args []string) error {
	fs := flags("fold view", env)
	repo := fs.String("repo", "", "repo, by its name in the workspace (default: the one you are in)")
	write := fs.Bool("write", false, "write it into the repo's coord_file, between its markers")
	if pos, err := parse(fs, args); err != nil {
		return err
	} else if len(pos) > 0 {
		return errUsage
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	h, err := locate(ctx, env, c, *repo)
	if err != nil {
		return err
	}
	if h.Repo == nil {
		return errors.New("which repo? run this inside one, or pass --repo")
	}
	v, err := c.FoldView(ctx, api.FoldView{RepoID: h.Repo.ID, Write: *write})
	if err != nil {
		return err
	}
	if v.Write {
		fmt.Fprintf(env.Stdout, "wrote the view into %s\n", v.File)
		return nil
	}
	fmt.Fprintln(env.Stdout, v.View)
	return nil
}

// laneScope widens or narrows a lane's scope: shepherd lane scope [lane] --add G --remove G.
func laneScope(ctx context.Context, env Env, args []string) error {
	fs := flags("lane scope", env)
	var add, remove listFlag
	fs.Var(&add, "add", "glob to add (repeatable); refused if another lane holds it")
	fs.Var(&remove, "remove", "glob to remove (repeatable)")
	repo := fs.String("repo", "", "repo, by its name in the workspace")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 || (len(add) == 0 && len(remove) == 0) {
		return errUsage
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	h, err := locate(ctx, env, c, *repo)
	if err != nil {
		return err
	}
	var id int64
	if len(pos) == 1 {
		l, err := findLane(ctx, c, h, pos[0])
		if err != nil {
			return err
		}
		id = l.ID
	} else if h.Lane != nil {
		id = h.Lane.ID
	} else {
		return errors.New("which lane? run this inside its worktree, or name it")
	}
	lane, err := c.RescopeLane(ctx, id, add, remove)
	if err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "lane %s scope: %s\n", lane.Name, strings.Join(lane.Scope, ", "))
	return nil
}
