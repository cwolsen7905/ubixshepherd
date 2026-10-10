package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/client"
)

// runWhy prints why a run failed: the newest diagnosis's reason, its evidence and the
// fixes on offer.
func runWhy(ctx context.Context, env Env, c *client.Client, id int64) error {
	ds, err := c.RunDiagnoses(ctx, id)
	if err != nil {
		return err
	}
	if len(ds) == 0 {
		run, err := c.Run(ctx, id)
		if err != nil {
			return err
		}
		fmt.Fprintf(env.Stdout, "run %d is %s and has no diagnosis: nothing went wrong that Shepherd saw.\n", id, run.State)
		return nil
	}
	w := env.Stdout
	d := ds[0].Diagnosis
	fmt.Fprintf(w, "run %d: %s\n", id, d.Summary)
	fmt.Fprintf(w, "  kind      %s\n", d.Kind)
	if d.Command != "" {
		fmt.Fprintf(w, "  command   %s", d.Command)
		if d.ExitCode != nil {
			fmt.Fprintf(w, " (exit %d)", *d.ExitCode)
		}
		fmt.Fprintln(w)
	}
	if len(d.Files) > 0 {
		fmt.Fprintf(w, "  files     %s\n", strings.Join(d.Files, ", "))
	}
	if d.ResetAt != nil {
		fmt.Fprintf(w, "  resets    %s\n", d.ResetAt.Local().Format("Jan 2 15:04"))
	}
	if d.Log != "" {
		fmt.Fprintf(w, "  log       %s\n", d.Log)
	}
	if d.Evidence != "" {
		fmt.Fprintln(w, "\nevidence:")
		for _, l := range strings.Split(strings.TrimRight(d.Evidence, "\n"), "\n") {
			fmt.Fprintln(w, "  | "+l)
		}
	}
	if len(d.Fixes) > 0 {
		fmt.Fprintln(w, "\nfixes (shepherd run fix "+fmt.Sprint(id)+" <action>):")
		for _, f := range d.Fixes {
			who := ""
			switch {
			case f.NeedsPerson:
				who = "  [your call]"
			case f.DeskOK:
				who = "  [the desk may]"
			}
			fmt.Fprintf(w, "  %-14s %s%s\n", f.ID, f.Label, who)
		}
	}
	if len(ds) > 1 {
		fmt.Fprintf(w, "\n%d earlier %s for this run are in the API: GET %s\n", len(ds)-1, plural(len(ds)-1, "diagnosis", "diagnoses"), api.PathRunDiagnoses(id))
	}
	return nil
}

// runFix takes one of a run's suggested fixes and says what it did.
func runFix(ctx context.Context, env Env, c *client.Client, id int64, action string, overrides map[string]string) error {
	res, err := c.FixRun(ctx, id, api.RunFix{Action: action, Overrides: overrides})
	if err != nil {
		return err
	}
	w := env.Stdout
	switch {
	case res.Run != nil:
		fmt.Fprintf(w, "%s: started run %d (shepherd run logs -f %d)\n", res.Fix.Label, res.Run.ID, res.Run.ID)
	case res.Shipped != nil:
		fmt.Fprintf(w, "%s: %s\n", res.Fix.Label, res.Message)
	case res.Done:
		fmt.Fprintf(w, "%s: %s\n", res.Fix.Label, res.Message)
	default:
		fmt.Fprintf(w, "%s: not done here. %s\n", res.Fix.Label, res.Message)
		return errSilent
	}
	return nil
}

// fixOverrides collects the flags a retry on another agent takes.
func fixOverrides(agent, model, prompt string) map[string]string {
	out := map[string]string{}
	for k, v := range map[string]string{"agent": agent, "model": model, "prompt": prompt} {
		if v != "" {
			out[k] = v
		}
	}
	return out
}
