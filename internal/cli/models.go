package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
)

// modelView is one agent's policy in a repo, as `shepherd models` prints it.
type modelView struct {
	Agent      string            `json:"agent"`
	Catalogued bool              `json:"catalogued"`
	Default    string            `json:"default,omitempty"`
	DefaultAt  string            `json:"default_at,omitempty"`
	Effort     string            `json:"effort,omitempty"`
	Models     []modelEntryView  `json:"models,omitempty"`
	Removed    map[string]string `json:"removed,omitempty"`
}

type modelEntryView struct {
	Name    string   `json:"name"`
	Use     string   `json:"use,omitempty"`
	Efforts []string `json:"efforts,omitempty"`
	Effort  string   `json:"effort,omitempty"`
}

// runModels is `shepherd models`: which models each agent may run in a repo, what each
// is for, and the defaults, from config.yaml's policy. Read-only.
func runModels(ctx context.Context, env Env, args []string) error {
	fs := flags("models", env)
	repo := fs.String("repo", "", "repo, by its name in the workspace (default: the one you are in)")
	agent := fs.String("agent", "", "only this agent: "+strings.Join(dispatch.AgentNames(), ", "))
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return errUsage
	}
	agents := dispatch.AgentNames()
	if *agent != "" {
		if !slices.Contains(agents, *agent) {
			return fmt.Errorf("unknown agent %q; Shepherd knows %s", *agent, strings.Join(agents, ", "))
		}
		agents = []string{*agent}
	}
	name := *repo
	if name == "" {
		c, err := dial(ctx, env)
		if err != nil {
			return err
		}
		h, err := locate(ctx, env, c, "")
		if err != nil {
			return err
		}
		if h.Repo == nil {
			return errors.New("which repo? name it with --repo, or run this inside one")
		}
		name = h.Repo.Name
	}
	cfg, _, err := config.LoadWarn(env.Layout.Config())
	if err != nil {
		return err
	}
	var views []modelView
	for _, a := range agents {
		p := cfg.ModelPolicy(name, a)
		v := modelView{Agent: a, Catalogued: p.Catalogued, Default: p.Default, DefaultAt: p.DefaultAt, Effort: p.Effort, Removed: p.Removed()}
		if p.Default != "" {
			if err := p.Check(p.Default); err != nil {
				v.Removed[p.Default] = err.Error()
				v.Default, v.DefaultAt = "", ""
			}
		}
		for _, m := range p.Permitted() {
			e, _ := p.Entry(m)
			v.Models = append(v.Models, modelEntryView{Name: m, Use: e.Use, Efforts: e.Efforts, Effort: e.Effort})
		}
		views = append(views, v)
	}
	if *asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(views)
	}
	fmt.Fprintf(env.Stdout, "Models in repo %s (rules: workspace catalogue, then project, defaults and repo deny and allow):\n", name)
	for _, v := range views {
		fmt.Fprintf(env.Stdout, "\n%s\n", v.Agent)
		switch {
		case v.Default != "":
			fmt.Fprintf(env.Stdout, "  default model   %s (%s)\n", v.Default, v.DefaultAt)
		case v.Catalogued:
			fmt.Fprintf(env.Stdout, "  default model   none: name one when you start a run\n")
		default:
			fmt.Fprintf(env.Stdout, "  default model   the agent's own\n")
		}
		if v.Effort != "" {
			fmt.Fprintf(env.Stdout, "  default effort  %s\n", v.Effort)
		}
		if !v.Catalogued {
			fmt.Fprintf(env.Stdout, "  no catalogue: any model name is accepted unless a rule below removes it\n")
		}
		for _, m := range v.Models {
			line := "  " + m.Name
			if m.Use != "" {
				line += ": " + m.Use
			}
			if len(m.Efforts) > 0 {
				line += " [effort: " + strings.Join(m.Efforts, ", ") + "]"
			} else {
				line += " [no effort setting]"
			}
			fmt.Fprintln(env.Stdout, line)
		}
		for _, m := range sortedKeys(v.Removed) {
			fmt.Fprintf(env.Stdout, "  not permitted   %s: %s\n", m, v.Removed[m])
		}
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
