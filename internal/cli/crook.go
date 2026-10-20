package cli

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// runCrook is `shepherd crook why|log|rules`: a read of Crook's verdict log
// (docs/crook-why.md). It is a client of the daemon's /v1/crook routes.
func runCrook(ctx context.Context, env Env, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	sub, args := args[0], args[1:]
	fs := flags("crook "+sub, env)
	asJSON := fs.Bool("json", false, "print JSON")
	var rule, kind, action, since *string
	var limit *int
	switch sub {
	case "why", "log":
		kind = fs.String("kind", "", "only proceed, ask, hold or refuse")
		since = fs.String("since", "", "only verdicts since a time: RFC 3339 or a number of days like 7d")
		limit = fs.Int("limit", 0, "how many to list (default 50)")
		if sub == "log" {
			rule = fs.String("rule", "", "only verdicts by this rule id")
			action = fs.String("action", "", "only this action, such as push.agent")
		}
	case "rules":
		since = fs.String("since", "", "from when: RFC 3339 or a number of days like 7d (default 7d)")
	default:
		return errUsage
	}
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	q := url.Values{}
	set := func(k string, v *string) {
		if v != nil && *v != "" {
			q.Set(k, *v)
		}
	}
	set("kind", kind)
	set("since", since)
	set("rule", rule)
	set("action", action)
	if limit != nil && *limit != 0 {
		q.Set("limit", strconv.Itoa(*limit))
	}
	if kind != nil && *kind != "" {
		switch *kind {
		case "proceed", "ask", "hold", "refuse":
		default:
			return fmt.Errorf("--kind is proceed, ask, hold or refuse")
		}
	}

	switch sub {
	case "rules":
		if len(pos) > 0 {
			return errUsage
		}
		c, err := dial(ctx, env)
		if err != nil {
			return err
		}
		out, err := c.CrookRules(ctx, *since)
		if err != nil {
			return err
		}
		if *asJSON {
			return printJSON(env, out)
		}
		writeCrookRules(env.Stdout, out)
		return nil
	case "log":
		if len(pos) > 0 {
			return errUsage
		}
	case "why":
		// why <id> | why run <id> | why lane <name>
		switch {
		case len(pos) == 1:
			id, err := strconv.ParseInt(pos[0], 10, 64)
			if err != nil || id <= 0 {
				return fmt.Errorf("%q is not a verdict id; use `crook why run <id>` or `crook why lane <name>` for a run or lane", pos[0])
			}
			c, err := dial(ctx, env)
			if err != nil {
				return err
			}
			out, err := c.CrookWhy(ctx, id)
			if err != nil {
				return err
			}
			if *asJSON {
				return printJSON(env, out)
			}
			writeCrookWhy(env.Stdout, out)
			return nil
		case len(pos) == 2 && pos[0] == "run":
			id, err := strconv.ParseInt(pos[1], 10, 64)
			if err != nil || id <= 0 {
				return fmt.Errorf("%q is not a run id", pos[1])
			}
			q.Set("run", pos[1])
		case len(pos) == 2 && pos[0] == "lane":
			q.Set("lane", pos[1])
		default:
			return errUsage
		}
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	out, err := c.CrookVerdicts(ctx, q)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(env, out)
	}
	if len(out.Verdicts) == 0 {
		fmt.Fprintln(env.Stdout, "no verdicts logged for that")
		return nil
	}
	for _, v := range out.Verdicts {
		fmt.Fprintf(env.Stdout, "#%d  %s  %-7s %s  %s  by %s  (%s@%d)\n", v.ID, v.At.Local().Format("Mon 15:04:05"), v.Kind, v.Action, crookSubject(v), v.Actor, v.Rule, v.RuleVersion)
		if v.Reason != "" {
			fmt.Fprintf(env.Stdout, "      %s\n", firstLine(v.Reason))
		}
	}
	return nil
}

func crookSubject(v store.CrookVerdict) string {
	var parts []string
	if v.Repo != "" {
		parts = append(parts, v.Repo)
	}
	if v.Lane != "" {
		parts = append(parts, "lane "+v.Lane)
	}
	if v.Run != 0 {
		parts = append(parts, "run "+strconv.FormatInt(v.Run, 10))
	}
	return strings.Join(parts, " ")
}

func firstLine(s string) string {
	line, _, more := strings.Cut(s, "\n")
	if more {
		line += " ..."
	}
	return line
}

func writeCrookWhy(w io.Writer, d api.CrookWhy) {
	v := d.Verdict
	fmt.Fprintf(w, "verdict #%d: %s\n", v.ID, v.Kind)
	fmt.Fprintf(w, "  when:    %s\n", v.At.Local().Format("Mon 2006-01-02 15:04:05"))
	fmt.Fprintf(w, "  action:  %s, asked by %s\n", v.Action, v.Actor)
	if s := crookSubject(v); s != "" {
		fmt.Fprintf(w, "  for:     %s\n", s)
	}
	fmt.Fprintf(w, "  rule:    %s (version %d)\n", v.Rule, v.RuleVersion)
	if v.Reason != "" {
		fmt.Fprintf(w, "  reason:  %s\n", strings.ReplaceAll(strings.TrimSpace(v.Reason), "\n", "\n           "))
	}
	if v.Facts != "" && v.Facts != "{}" {
		fmt.Fprintf(w, "  facts:   %s\n", v.Facts)
	}
	if len(d.Path) > 0 {
		fmt.Fprintln(w, "  path:")
		for i, s := range d.Path {
			fmt.Fprintf(w, "    %d. %-10s %s\n", i+1, s.Stage, s.Outcome)
		}
	}
}

func writeCrookRules(w io.Writer, r api.CrookRules) {
	if len(r.Rules) == 0 {
		fmt.Fprintf(w, "no verdicts logged since %s\n", r.Since)
		return
	}
	fmt.Fprintf(w, "verdicts by rule since %s\n", r.Since)
	fmt.Fprintf(w, "  %-32s %8s %5s %5s %7s\n", "rule", "proceed", "ask", "hold", "refuse")
	for _, c := range r.Rules {
		fmt.Fprintf(w, "  %-32s %8d %5d %5d %7d\n", c.Rule, c.Proceed, c.Ask, c.Hold, c.Refuse)
	}
}
