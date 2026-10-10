package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/standards"
)

// standardsRoot is where Shepherd's packs are: the daemon reads the same directory.
func standardsRoot(env Env) string { return filepath.Join(env.Layout.Home, "standards") }

// runStandards is `shepherd standards`: the packs on this machine, the rules in force
// for a repo, and the example pack. It reads files; it does not need the daemon except
// to find a repo's profile.
func runStandards(ctx context.Context, env Env, args []string) error {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := flags("standards "+sub, env)
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		if len(pos) > 0 {
			return errUsage
		}
		return standardsList(env, *asJSON)
	case "show":
		if len(pos) > 1 {
			return errUsage
		}
		dir := env.Cwd
		if len(pos) == 1 {
			dir = pos[0]
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(env.Cwd, dir)
			}
		}
		return standardsShow(ctx, env, dir, *asJSON)
	case "init":
		if len(pos) > 0 {
			return errUsage
		}
		wrote, err := standards.SeedHouse(standardsRoot(env))
		if err != nil {
			return err
		}
		if wrote {
			fmt.Fprintf(env.Stdout, "Wrote the example house pack to %s\nUse it in a repo with: standards: [house]\n", filepath.Join(standardsRoot(env), "house"))
		} else {
			fmt.Fprintf(env.Stdout, "%s is already there; left as it is.\n", filepath.Join(standardsRoot(env), "house"))
		}
		return nil
	}
	return errUsage
}

type packView struct {
	Pack     string           `json:"pack"`
	Rules    []standards.Rule `json:"rules"`
	Problems []string         `json:"problems,omitempty"`
}

func standardsList(env Env, asJSON bool) error {
	entries, err := os.ReadDir(standardsRoot(env))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	out := []packView{}
	for _, e := range entries {
		if !e.IsDir() || !standards.ValidID(e.Name()) {
			continue
		}
		rules, probs, err := standards.LoadDir(filepath.Join(standardsRoot(env), e.Name()), e.Name())
		if err != nil {
			probs = append(probs, err.Error())
		}
		out = append(out, packView{Pack: e.Name(), Rules: rules, Problems: probs})
	}
	if asJSON {
		return printJSON(env, out)
	}
	w := env.Stdout
	if len(out) == 0 {
		fmt.Fprintf(w, "No packs in %s. `shepherd standards init` writes the example house pack.\n", standardsRoot(env))
		return nil
	}
	for _, p := range out {
		fmt.Fprintf(w, "%s  (%d rules)\n", p.Pack, len(p.Rules))
		for _, r := range p.Rules {
			fmt.Fprintf(w, "  %-6s %-24s %s\n", r.Level, r.ID, r.Statement)
		}
		for _, pr := range p.Problems {
			fmt.Fprintf(w, "  ! %s\n", pr)
		}
	}
	return nil
}

func standardsShow(ctx context.Context, env Env, dir string, asJSON bool) error {
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	res, err := c.Resolve(ctx, dir)
	if err != nil {
		return err
	}
	if res.Repo == nil || res.Profile == nil {
		return errors.New("which repo? run this inside one, or pass its directory")
	}
	p := *res.Profile
	tree := res.Repo.Path
	var lane standards.Lane
	if res.Lane != nil {
		tree = res.Lane.Worktree
		lane.Scope = res.Lane.Scope
	}
	repoDir := ""
	if p.StandardsPackDir != "" {
		repoDir = filepath.Join(tree, filepath.FromSlash(p.StandardsPackDir))
	}
	set := standards.Resolve(standardsRoot(env), p.Standards, repoDir)
	docs := standards.ExistingDocs(tree, p.StandardsDocs)
	if asJSON {
		return printJSON(env, struct {
			standards.Set
			Docs []string `json:"docs"`
		}{set, docs})
	}
	w := env.Stdout
	fmt.Fprintf(w, "Packs       %s\n", joinOr(p.Standards, "none"))
	fmt.Fprintf(w, "Repo docs   %s\n", joinOr(docs, "none found"))
	for _, r := range set.Rules {
		src := r.Pack
		check := ""
		if r.Check != "" {
			check = "  [check]"
		}
		fmt.Fprintf(w, "  %-6s %-24s %s  (%s)%s\n", r.Level, r.ID, r.Statement, src, check)
	}
	if len(set.Disabled) > 0 {
		fmt.Fprintf(w, "Switched off by the repo: %s\n", strings.Join(set.Disabled, ", "))
	}
	for _, pr := range set.Problems {
		fmt.Fprintf(w, "! %s\n", pr)
	}
	if res.Lane != nil {
		lane.Tags = p.StandardsWhen
		fmt.Fprintf(w, "\nIn lane %s the brief carries:\n\n%s", res.Lane.Name, standards.Digest(set, lane, standards.DigestOptions{Docs: docs, RepoPackDir: p.StandardsPackDir}))
	}
	return nil
}
