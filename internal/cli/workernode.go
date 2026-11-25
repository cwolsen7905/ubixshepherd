package cli

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// workerNodeSubs are the machine subcommands of `shepherd worker`. The same command's
// other subcommands (report, ask-human, and the rest, in decision.go) are tools for the
// agents Shepherd starts; the names do not overlap.
var workerNodeSubs = map[string]bool{"add": true, "list": true, "revoke": true, "remove": true, "confirm": true}

// runWorkerCmd is `shepherd worker`: the machine subcommands, or an agent's tool.
func runWorkerCmd(ctx context.Context, env Env, args []string) error {
	if len(args) > 0 && workerNodeSubs[args[0]] {
		return runWorkerNode(ctx, env, args)
	}
	return runWorker(ctx, env, args)
}

// runMember is `shepherd member`, the old name of the machine subcommands of `shepherd
// worker`, kept for one release. It says so on stderr and runs the same thing.
func runMember(ctx context.Context, env Env, args []string) error {
	fmt.Fprintln(env.Stderr, "shepherd: member is now worker: shepherd worker "+strings.Join(args, " "))
	return runWorkerNode(ctx, env, args)
}

// runWorkerNode is `shepherd worker add | list | revoke | remove | confirm`, run on the
// controller: which machines may link in (docs/cluster.md).
func runWorkerNode(ctx context.Context, env Env, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	sub, args := args[0], args[1:]
	fs := flags("worker "+sub, env)
	asJSON := fs.Bool("json", false, "list: print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		if len(pos) > 0 {
			return errUsage
		}
		cl, err := c.Cluster(ctx)
		if err != nil {
			return err
		}
		if *asJSON {
			return printJSON(env, cl.Workers)
		}
		for _, m := range cl.Workers {
			fmt.Fprintf(env.Stdout, "%-12s %s\n", m.Name, workerLine(m, time.Now()))
		}
		return nil
	case "add":
		if len(pos) != 1 {
			return errUsage
		}
		code, err := c.WorkerAdd(ctx, pos[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(env.Stdout, "%s\n\n", code.Code)
		fmt.Fprintf(env.Stdout, "This code enrols one machine as %q, once, until %s.\n", code.Name, code.Expires.Local().Format("15:04"))
		fmt.Fprintf(env.Stdout, "On that machine: shepherd cluster join <the controller's https address> %s\n", code.Code)
		return nil
	case "revoke", "remove", "confirm":
		if len(pos) != 1 {
			return errUsage
		}
		if err := c.WorkerAct(ctx, pos[0], sub); err != nil {
			return err
		}
		verb := map[string]string{"revoke": "revoked: its link is closed and its credential gone", "remove": "removed", "confirm": "confirmed: its new daemon id is accepted"}[sub]
		fmt.Fprintf(env.Stdout, "%s %s\n", pos[0], verb)
		return nil
	}
	return errUsage
}
