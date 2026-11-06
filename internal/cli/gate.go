package cli

import (
	"context"
	"fmt"
)

// laneGate runs the repo's gate on the lane's committed head, in the daemon, and records
// the result. The pre-push hook lets an agent push only a commit with a passing record.
func laneGate(ctx context.Context, env Env, args []string) error {
	fs := flags("lane gate", env)
	repo := fs.String("repo", "", "repo, by its name in the workspace")
	asJSON := fs.Bool("json", false, "print JSON")
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
	target, err := pickLane(ctx, c, h, pos)
	if err != nil {
		return err
	}
	if !*asJSON {
		fmt.Fprintf(env.Stderr, "running the repo's gate on lane %s's head...\n", target.Name)
	}
	res, err := c.GateLane(ctx, target.ID)
	if err != nil {
		return err
	}
	if *asJSON {
		if err := printJSON(env, res); err != nil {
			return err
		}
		if !res.Passed {
			return errSilent
		}
		return nil
	}
	sha := res.SHA
	if len(sha) > 12 {
		sha = sha[:12]
	}
	if res.Passed {
		fmt.Fprintf(env.Stdout, "the gate, `%s`, passed on %s; you may push this commit\n", res.Command, sha)
		return nil
	}
	fmt.Fprintf(env.Stdout, "the gate, `%s`, failed on %s; not recorded as a pass. It ended:\n%s\n(whole output: %s)\n", res.Command, sha, res.Tail, res.Log)
	return errSilent
}
