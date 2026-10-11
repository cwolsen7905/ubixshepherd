package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/client"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/workspace"
)

func runInit(ctx context.Context, env Env, args []string) error {
	fs := flags("init", env)
	name := fs.String("name", "", "workspace name (default: the directory's name)")
	yes := fs.Bool("yes", false, "accept the suggested repos without asking")
	all := fs.Bool("all", false, "manage every repo found")
	only := fs.String("only", "", "manage these repos (comma-separated names), adding to those already managed")
	prune := fs.Bool("prune", false, "stop managing the repos in this workspace that were not picked")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 || countTrue(*yes, *all, *only != "") > 1 {
		return errUsage
	}
	in := bufio.NewScanner(env.Stdin)
	interactive := false
	dir := "."
	if len(pos) == 1 {
		dir = pos[0]
	}
	root, err := canonicalDir(env, dir)
	if err != nil {
		return err
	}
	if *name == "" {
		*name = filepath.Base(root)
	}

	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	// Repos already managed stay ticked when init runs again.
	managed := map[string]bool{}
	byPath := map[string]store.Repo{} // this workspace's managed repos
	existing, err := c.Workspaces(ctx)
	if err != nil {
		return err
	}
	for _, ws := range existing {
		for _, r := range ws.Repos {
			managed[r.Path] = true
			if ws.Path == root {
				byPath[r.Path] = r
			}
		}
	}

	found, err := workspace.Scan(ctx, root)
	if err != nil {
		return err
	}
	if len(found) == 0 {
		return fmt.Errorf("no git repos found under %s (looked %d levels down)", root, workspace.MaxDepth)
	}
	picked := make([]bool, len(found))
	for i, f := range found {
		picked[i] = f.Suggest || managed[f.Path]
	}

	switch {
	case *all:
		for i := range picked {
			picked[i] = true
		}
	case *only != "":
		if picked, err = pickNamed(found, *only); err != nil {
			return err
		}
	case *yes:
	case !env.Interactive:
		return errors.New("stdin is not a terminal: pass --yes, --all or --only to choose repos")
	default:
		if picked, err = prompt(env, in, found, picked, managed); err != nil {
			return err
		}
		// Unticking a managed repo stops managing it, once the person has said so.
		interactive = true
	}

	req := api.SaveWorkspace{Name: *name, Path: root}
	pickedPath := map[string]bool{}
	for i, f := range found {
		if picked[i] {
			pickedPath[f.Path] = true
			req.Repos = append(req.Repos, store.Repo{Name: f.Name, Path: f.Path, Remote: f.Remote, Stacks: f.Stacks})
		}
	}
	var unlisted []store.Repo
	for p, r := range byPath {
		if !pickedPath[p] {
			unlisted = append(unlisted, r)
		}
	}
	slices.SortFunc(unlisted, func(a, b store.Repo) int { return strings.Compare(a.Name, b.Name) })
	drop := unlisted
	if !*prune && !interactive {
		drop = nil
	}
	if len(drop) > 0 {
		// Check before saving anything, so a refusal leaves the workspace as it was.
		if err := checkNoOpenLanes(ctx, c, drop); err != nil {
			return err
		}
		if interactive && !*yes {
			names := repoNames(drop)
			fmt.Fprintf(env.Stdout, "This stops managing %s: %s. Their history is kept. Continue? [y/N] ", plural(len(drop), "repo", "repos"), strings.Join(names, ", "))
			if !in.Scan() || !strings.EqualFold(strings.TrimSpace(in.Text()), "y") {
				return errors.New("not confirmed; nothing registered")
			}
		}
	}
	got, err := c.SaveWorkspace(ctx, req)
	if err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Workspace %s at %s: %s managed.\n", got.Name, got.Path, plural(len(got.Repos), "repo", "repos"))
	for _, r := range got.Repos {
		fmt.Fprintf(env.Stdout, "  %s\n", r.Name)
	}
	for _, r := range drop {
		if _, err := c.UnmanageRepo(ctx, r.ID, "init --prune"); err != nil {
			return fmt.Errorf("stop managing %s: %w", r.Name, err)
		}
		fmt.Fprintf(env.Stdout, "Stopped managing %s (its history is kept).\n", r.Name)
	}
	if len(drop) == 0 && len(unlisted) > 0 {
		fmt.Fprintf(env.Stdout, "Also still managed, not listed: %s\n", strings.Join(repoNames(unlisted), ", "))
		fmt.Fprintln(env.Stdout, "To stop managing one: shepherd repo remove <name>   (or run init again with --prune)")
	}
	fmt.Fprintln(env.Stdout, "To add another repo later: shepherd repo add <name>")
	return nil
}

func repoNames(repos []store.Repo) []string {
	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.Name
	}
	return names
}

// checkNoOpenLanes refuses to stop managing a repo that has open lanes, naming them.
func checkNoOpenLanes(ctx context.Context, c *client.Client, repos []store.Repo) error {
	var busy []string
	for _, r := range repos {
		lanes, err := c.Lanes(ctx, r.WorkspaceID, r.ID)
		if err != nil {
			return err
		}
		var open []string
		for _, l := range lanes {
			if l.State != store.LaneClosed {
				open = append(open, l.Name)
			}
		}
		if len(open) > 0 {
			busy = append(busy, fmt.Sprintf("%s has %s: %s", r.Name, plural(len(open), "open lane", "open lanes"), strings.Join(open, ", ")))
		}
	}
	if len(busy) > 0 {
		return fmt.Errorf("cannot stop managing: %s; close them first (nothing was changed)", strings.Join(busy, "; "))
	}
	return nil
}

func countTrue(bs ...bool) int {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n
}

func pickNamed(found []workspace.Found, list string) ([]bool, error) {
	idx := map[string]int{}
	for i, f := range found {
		idx[f.Name] = i
	}
	picked := make([]bool, len(found))
	for _, n := range strings.Split(list, ",") {
		n = strings.TrimSpace(n)
		i, ok := idx[n]
		if !ok {
			return nil, fmt.Errorf("--only: no repo %q was found", n)
		}
		picked[i] = true
	}
	return picked, nil
}

// prompt shows the found repos and lets the person toggle them until they accept.
func prompt(env Env, in *bufio.Scanner, found []workspace.Found, picked []bool, managed map[string]bool) ([]bool, error) {
	w := env.Stdout
	for {
		fmt.Fprintln(w, "Repos found ([x] = Shepherd will manage it):")
		for i, f := range found {
			mark := " "
			if picked[i] {
				mark = "x"
			}
			note := joinOr(f.Stacks, "")
			switch {
			case managed[f.Path]:
				note = strings.TrimPrefix(note+", already managed", ", ")
			case !f.Suggest:
				note = strings.TrimPrefix(note+", "+f.Reason, ", ")
			}
			fmt.Fprintf(w, "  %3d [%s] %-28s %s\n", i+1, mark, f.Name, note)
		}
		fmt.Fprint(w, "Enter to accept; numbers or ranges to toggle (3 5-7); a = all, n = none, q = quit: ")
		if !in.Scan() {
			if err := in.Err(); err != nil {
				return nil, err
			}
			return nil, errors.New("no answer; nothing registered")
		}
		ans := strings.TrimSpace(in.Text())
		switch ans {
		case "":
			return picked, nil
		case "q":
			return nil, errors.New("quit; nothing registered")
		case "a", "n":
			for i := range picked {
				picked[i] = ans == "a"
			}
			continue
		}
		toggle, err := parseSelection(ans, len(found))
		if err != nil {
			fmt.Fprintln(w, err)
			continue
		}
		for _, i := range toggle {
			picked[i] = !picked[i]
		}
	}
}

// parseSelection turns "3 5-7" into zero-based indexes, each in [0, n).
func parseSelection(s string, n int) ([]int, error) {
	var out []int
	for _, tok := range strings.Fields(strings.ReplaceAll(s, ",", " ")) {
		lo, hi, isRange := strings.Cut(tok, "-")
		a, err := strconv.Atoi(lo)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number or range", tok)
		}
		b := a
		if isRange {
			if b, err = strconv.Atoi(hi); err != nil {
				return nil, fmt.Errorf("%q is not a number or range", tok)
			}
		}
		if a < 1 || b > n || a > b {
			return nil, fmt.Errorf("%q is outside 1-%d", tok, n)
		}
		for i := a; i <= b; i++ {
			out = append(out, i-1)
		}
	}
	return out, nil
}
