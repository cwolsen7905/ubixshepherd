package cli

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/client"
	"github.com/ubixsys/ubixshepherd/internal/mergerule"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// shadowRowLimit is how many rows of each action the shadow reads fetch; the daemon's
// own cap on a verdict list.
const shadowRowLimit = 1000

// shadowRows reads the shadow evidence in Crook's verdict log through the daemon: the
// would-have rows and the outcome rows, newest first. The daemon lists by action, so
// the two are fetched apart and told from live rows by their stage.
func shadowRows(ctx context.Context, c *client.Client, rule, since string) (would, outcomes []store.CrookVerdict, err error) {
	get := func(action string) ([]store.CrookVerdict, error) {
		q := url.Values{"action": {action}, "limit": {fmt.Sprint(shadowRowLimit)}}
		if rule != "" {
			q.Set("rule", rule)
		}
		if since != "" {
			q.Set("since", since)
		}
		out, err := c.CrookVerdicts(ctx, q)
		if err != nil {
			return nil, err
		}
		var rows []store.CrookVerdict
		for _, v := range out.Verdicts {
			if mergerule.IsShadowStage(v.Stage) {
				rows = append(rows, v)
			}
		}
		return rows, nil
	}
	if would, err = get(mergerule.ActionMerge); err != nil {
		return nil, nil, err
	}
	outcomes, err = get(mergerule.ActionOutcome)
	return would, outcomes, err
}

// shadowCount is what a shadow rule would have done, counted by verdict.
type shadowCount struct {
	Rule    string `json:"rule"`
	Proceed int    `json:"would_have_proceeded"`
	Ask     int    `json:"would_have_asked"`
	Hold    int    `json:"would_have_held"`
	Merged  int    `json:"person_merged"`
}

func countShadow(would, outcomes []store.CrookVerdict) []shadowCount {
	var out []shadowCount
	at := func(rule string) *shadowCount {
		for i := range out {
			if out[i].Rule == rule {
				return &out[i]
			}
		}
		out = append(out, shadowCount{Rule: rule})
		return &out[len(out)-1]
	}
	for _, v := range would {
		c := at(v.Rule)
		switch v.Kind {
		case "proceed":
			c.Proceed++
		case "hold":
			c.Hold++
		default:
			c.Ask++
		}
	}
	for _, v := range outcomes {
		at(v.Rule).Merged++
	}
	return out
}

// crookRulesOut is `crook rules --json`: the daemon's counts without the shadow rows,
// then the shadow rows counted apart.
type crookRulesOut struct {
	api.CrookRules
	Shadow []shadowCount `json:"shadow,omitempty"`
}

// crookRules prints per-rule counts with the shadow rows taken out of the live counts
// and shown in a table of their own: a shadow row is what a rule would have done, not
// something Shepherd did.
func crookRules(ctx context.Context, env Env, c *client.Client, since string, asJSON bool) error {
	if since == "" {
		since = "7d"
	}
	live, err := c.CrookRules(ctx, since)
	if err != nil {
		return err
	}
	would, outcomes, err := shadowRows(ctx, c, "", since)
	if err != nil {
		return err
	}
	// Take each shadow row back out of the counts the daemon made over every row.
	sub := func(v store.CrookVerdict) {
		for i := range live.Rules {
			if live.Rules[i].Rule != v.Rule {
				continue
			}
			n := &live.Rules[i]
			switch v.Kind {
			case "proceed":
				n.Proceed--
			case "ask":
				n.Ask--
			case "hold":
				n.Hold--
			case "refuse":
				n.Refuse--
			}
		}
	}
	for _, v := range would {
		sub(v)
	}
	for _, v := range outcomes {
		sub(v)
	}
	kept := live.Rules[:0]
	for _, r := range live.Rules {
		if r.Proceed+r.Ask+r.Hold+r.Refuse > 0 {
			kept = append(kept, r)
		}
	}
	live.Rules = kept
	shadow := countShadow(would, outcomes)
	if asJSON {
		return printJSON(env, crookRulesOut{live, shadow})
	}
	writeCrookRules(env.Stdout, live)
	if len(shadow) > 0 {
		fmt.Fprintln(env.Stdout)
		writeShadowCounts(env.Stdout, shadow)
	}
	return nil
}

func writeShadowCounts(w io.Writer, counts []shadowCount) {
	fmt.Fprintln(w, "shadow rules (what they would have done; nothing was acted on)")
	fmt.Fprintf(w, "  %-32s %17s %6s %6s %14s\n", "rule", "would have merged", "asked", "held", "person merged")
	for _, c := range counts {
		fmt.Fprintf(w, "  %-32s %17d %6d %6d %14d\n", c.Rule, c.Proceed, c.Ask, c.Hold, c.Merged)
	}
}

// shadowOut is `crook shadow --json`.
type shadowOut struct {
	Since    string               `json:"since"`
	Counts   []shadowCount        `json:"counts"`
	Verdicts []store.CrookVerdict `json:"verdicts"`
	Outcomes []store.CrookVerdict `json:"outcomes"`
}

// crookShadow is `shepherd crook shadow`: the merge requests a shadow rule had an
// opinion on, what it would have done, which conditions were short, and what the person
// then did.
func crookShadow(ctx context.Context, env Env, c *client.Client, rule, since string, limit int, asJSON bool) error {
	if since == "" {
		since = "14d"
	}
	would, outcomes, err := shadowRows(ctx, c, rule, since)
	if err != nil {
		return err
	}
	counts := countShadow(would, outcomes)
	if limit > 0 && len(would) > limit {
		would = would[:limit]
	}
	if asJSON {
		return printJSON(env, shadowOut{Since: since, Counts: counts, Verdicts: would, Outcomes: outcomes})
	}
	if len(would) == 0 {
		fmt.Fprintf(env.Stdout, "no shadow verdicts since %s\n", since)
		return nil
	}
	writeShadowCounts(env.Stdout, counts)
	fmt.Fprintln(env.Stdout)
	for _, v := range would {
		fmt.Fprintf(env.Stdout, "#%d  %s  would have %-7s %s  (%s)\n", v.ID, v.At.Local().Format("Mon 15:04"), shadowVerb(v.Kind), crookSubject(v), v.Rule)
		if v.Reason != "" {
			fmt.Fprintf(env.Stdout, "      %s\n", firstLine(v.Reason))
		}
	}
	for _, o := range outcomes {
		fmt.Fprintf(env.Stdout, "#%d  %s  %s  %s\n      %s\n", o.ID, o.At.Local().Format("Mon 15:04"), "outcome", crookSubject(o), firstLine(o.Reason))
	}
	return nil
}

func shadowVerb(kind string) string {
	switch kind {
	case "proceed":
		return "merged"
	case "hold":
		return "held"
	}
	return strings.TrimSpace(kind) // ask
}
