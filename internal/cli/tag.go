package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// shepherd tag reserve | list | release: release versions handed out one at a time.
func runTag(ctx context.Context, env Env, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	fs := flags("tag "+args[0], env)
	repo := fs.String("repo", "", "repo, by its name in the workspace (default: the one you are in)")
	lane := fs.String("lane", "", "reserve: the lane it is for (default: the lane you are in)")
	noLane := fs.Bool("no-lane", false, "reserve: for no lane (a release cut outside any lane)")
	pos, err := parse(fs, args[1:])
	if err != nil {
		return err
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
	switch args[0] {
	case "reserve":
		if len(pos) != 1 {
			return errUsage
		}
		var laneID int64
		switch {
		case *noLane:
		case *lane != "":
			l, err := findLane(ctx, c, h, *lane)
			if err != nil {
				return err
			}
			laneID = l.ID
		case h.Lane != nil:
			laneID = h.Lane.ID
		}
		res, err := c.ReserveTag(ctx, api.Reserve{RepoID: h.Repo.ID, LaneID: laneID, Bump: pos[0]})
		if err != nil {
			return err
		}
		fmt.Fprintf(env.Stdout, "%s is yours; tag it when the work is merged\n", res.Tag)
		return nil
	case "list", "ls":
		if len(pos) > 0 {
			return errUsage
		}
		rs, err := c.Tags(ctx, h.Repo.ID)
		if err != nil {
			return err
		}
		if len(rs) == 0 {
			fmt.Fprintf(env.Stdout, "No tags reserved in %s.\n", h.Repo.Name)
			return nil
		}
		for _, r := range rs {
			who := r.Lane
			if who == "" {
				who = "(no lane)"
			}
			fmt.Fprintf(env.Stdout, "%-12s %-9s %s\n", r.Tag, r.State, who)
		}
		return nil
	case "release":
		if len(pos) != 1 {
			return errUsage
		}
		if err := c.ReleaseTag(ctx, h.Repo.ID, pos[0]); err != nil {
			return err
		}
		fmt.Fprintf(env.Stdout, "released %s\n", pos[0])
		return nil
	}
	return errUsage
}
