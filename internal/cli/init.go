package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/workspace"
)

func runInit(ctx context.Context, env Env, args []string) error {
	fs := flags("init", env)
	name := fs.String("name", "", "workspace name (default: the directory's name)")
	yes := fs.Bool("yes", false, "accept the suggested repos without asking")
	all := fs.Bool("all", false, "manage every repo found")
	only := fs.String("only", "", "manage exactly these repos (comma-separated names)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 || countTrue(*yes, *all, *only != "") > 1 {
		return errUsage
	}
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

	c, err := dial(env)
	if err != nil {
		return err
	}
	// Repos already managed stay ticked when init runs again.
	managed := map[string]bool{}
	existing, err := c.Workspaces(ctx)
	if err != nil {
		return err
	}
	for _, ws := range existing {
		for _, r := range ws.Repos {
			managed[r.Path] = true
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
		if picked, err = prompt(env, found, picked, managed); err != nil {
			return err
		}
	}

	req := api.SaveWorkspace{Name: *name, Path: root}
	for i, f := range found {
		if picked[i] {
			req.Repos = append(req.Repos, store.Repo{Name: f.Name, Path: f.Path, Remote: f.Remote, Stacks: f.Stacks})
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
func prompt(env Env, found []workspace.Found, picked []bool, managed map[string]bool) ([]bool, error) {
	in := bufio.NewScanner(env.Stdin)
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
