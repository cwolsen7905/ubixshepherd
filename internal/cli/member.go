package cli

import (
	"context"
	"fmt"
	"time"
)

// runMember is `shepherd member add | list | revoke | remove | confirm`, run on the hub:
// who may link in (docs/cluster.md).
func runMember(ctx context.Context, env Env, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	sub, args := args[0], args[1:]
	fs := flags("member "+sub, env)
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
			return printJSON(env, cl.Members)
		}
		for _, m := range cl.Members {
			fmt.Fprintf(env.Stdout, "%-12s %s\n", m.Name, memberLine(m, time.Now()))
		}
		return nil
	case "add":
		if len(pos) != 1 {
			return errUsage
		}
		code, err := c.MemberAdd(ctx, pos[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(env.Stdout, "%s\n\n", code.Code)
		fmt.Fprintf(env.Stdout, "This code enrols one machine as %q, once, until %s.\n", code.Name, code.Expires.Local().Format("15:04"))
		fmt.Fprintf(env.Stdout, "On that machine: shepherd cluster join <the hub's https address> %s\n", code.Code)
		return nil
	case "revoke", "remove", "confirm":
		if len(pos) != 1 {
			return errUsage
		}
		if err := c.MemberAct(ctx, pos[0], sub); err != nil {
			return err
		}
		verb := map[string]string{"revoke": "revoked: its link is closed and its credential gone", "remove": "removed", "confirm": "confirmed: its new daemon id is accepted"}[sub]
		fmt.Fprintf(env.Stdout, "%s %s\n", pos[0], verb)
		return nil
	}
	return errUsage
}
