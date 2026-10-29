package configedit

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/config"
)

// Risk is a change that gives agents more power or takes a guard away. A write that
// carries one is refused until the person confirms its Path.
type Risk struct {
	Path string `json:"path"`
	From string `json:"from"`
	To   string `json:"to"`
	Why  string `json:"why"`
}

// power ranks who may act: a person alone, Shepherd after its checks, then the agent.
func power(who string) int {
	switch who {
	case config.Agent:
		return 2
	case config.Shepherd, config.AfterApproval:
		return 1
	}
	return 0
}

// Risks lists what next grants over prev, by effective values: a repo that inherits a
// raised default is named only once, under defaults.
func Risks(prev, next config.Config) []Risk {
	out := profileRisks("defaults", prev.Defaults, next.Defaults)
	inherited := map[string]bool{}
	for _, r := range out {
		inherited[strings.TrimPrefix(r.Path, "defaults.")+"|"+r.To] = true
	}
	names := map[string]bool{}
	for n := range prev.Repos {
		names[n] = true
	}
	for n := range next.Repos {
		names[n] = true
	}
	for _, n := range slices.Sorted(maps.Keys(names)) {
		for _, r := range profileRisks("repos."+n, prev.Profile(n), next.Profile(n)) {
			if !inherited[strings.TrimPrefix(r.Path, "repos."+n+".")+"|"+r.To] {
				out = append(out, r)
			}
		}
	}
	return out
}

func profileRisks(at string, prev, next config.Profile) []Risk {
	var out []Risk
	add := func(field, from, to, why string) {
		out = append(out, Risk{Path: at + "." + field, From: from, To: to, Why: why})
	}
	for _, a := range []struct{ name, from, to string }{
		{"autonomy.push", prev.Autonomy.Push, next.Autonomy.Push},
		{"autonomy.merge", prev.Autonomy.Merge, next.Autonomy.Merge},
		{"autonomy.tag", prev.Autonomy.Tag, next.Autonomy.Tag},
		{"autonomy.deploy", prev.Autonomy.Deploy, next.Autonomy.Deploy},
	} {
		if power(a.to) > power(a.from) {
			add(a.name, a.from, a.to, fmt.Sprintf("lets %s act without the person", a.to))
		}
	}
	if planFirst(prev) && !planFirst(next) {
		add("autonomy.plan_first", "true", "false", "agents will change things without proposing a plan first")
	}
	if next.Agent.PermissionMode == config.PermBypass && prev.Agent.PermissionMode != config.PermBypass {
		add("agent.permission_mode", prev.Agent.PermissionMode, next.Agent.PermissionMode, "agents skip permission checks")
	}
	if prev.Gate != "" && next.Gate == "" {
		add("gate", prev.Gate, "", "no gate: work counts as green without a check")
	}
	if gone := missing(prev.Forbid, next.Forbid); len(gone) > 0 {
		add("forbid", fmt.Sprint(prev.Forbid), fmt.Sprint(next.Forbid), "commit messages are no longer checked for "+fmt.Sprint(gone))
	}
	return out
}

func planFirst(p config.Profile) bool { return p.Autonomy.PlanFirst == nil || *p.Autonomy.PlanFirst }

func missing(prev, next []string) []string {
	var out []string
	for _, p := range prev {
		if !slices.Contains(next, p) {
			out = append(out, p)
		}
	}
	return out
}
