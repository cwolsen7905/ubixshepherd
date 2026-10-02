package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/client"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// laneRun starts an agent in a lane: shepherd lane run [lane] --agent A "task".
func laneRun(ctx context.Context, env Env, args []string) error {
	fs := flags("lane run", env)
	agent := fs.String("agent", "", "agent to start: "+strings.Join(dispatch.AgentNames(), ", "))
	model := fs.String("model", "", "model, if not the agent's default")
	repo := fs.String("repo", "", "repo, by its name in the workspace")
	detach := fs.Bool("detach", false, "start it and return; follow later with shepherd run logs -f")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if *agent == "" || len(pos) < 1 || len(pos) > 2 {
		return errUsage
	}
	task := pos[len(pos)-1]
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	h, err := locate(ctx, env, c, *repo)
	if err != nil {
		return err
	}
	var laneID int64
	if len(pos) == 2 {
		l, err := findLane(ctx, c, h, pos[0])
		if err != nil {
			return err
		}
		laneID = l.ID
	} else {
		if h.Lane == nil {
			return errors.New("which lane? run this inside its worktree, or name it: shepherd lane run <lane> --agent ... \"task\"")
		}
		laneID = h.Lane.ID
	}
	run, err := c.StartRun(ctx, dispatch.StartRequest{LaneID: laneID, Agent: *agent, Model: *model, Prompt: task})
	if err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "started run %d: %s in lane %s (%s)\n", run.ID, run.Agent, run.Lane, run.Repo)
	if *detach {
		fmt.Fprintf(env.Stdout, "follow it: shepherd run logs -f %d\n", run.ID)
		return nil
	}
	fmt.Fprintln(env.Stdout, "following its output; Ctrl-C detaches, the run keeps going")
	fmt.Fprintln(env.Stdout)
	return follow(ctx, env, c, run.ID)
}

func findLane(ctx context.Context, c *client.Client, h here, name string) (api.LaneView, error) {
	var repoID int64
	if h.Repo != nil {
		repoID = h.Repo.ID
	}
	lanes, err := c.Lanes(ctx, h.Workspace.ID, repoID)
	if err != nil {
		return api.LaneView{}, err
	}
	var found *api.LaneView
	for i := range lanes {
		if lanes[i].Name == name {
			if found != nil {
				return api.LaneView{}, fmt.Errorf("lane %s is open in %s and %s; pass --repo", name, found.Repo, lanes[i].Repo)
			}
			found = &lanes[i]
		}
	}
	if found == nil {
		return api.LaneView{}, fmt.Errorf("no open lane %s", name)
	}
	return *found, nil
}

// follow prints a run's log as it grows until the run ends, then its outcome. A
// cancelled context (Ctrl-C) detaches without stopping the run.
func follow(ctx context.Context, env Env, c *client.Client, id int64) error {
	var offset int64
	for {
		chunk, err := c.RunLog(ctx, id, offset)
		if err != nil {
			if ctx.Err() != nil {
				fmt.Fprintf(env.Stdout, "\ndetached; run %d keeps going (shepherd run logs -f %d)\n", id, id)
				return nil
			}
			return err
		}
		fmt.Fprint(env.Stdout, chunk.Data)
		offset = chunk.Offset
		if chunk.Done {
			break
		}
		if chunk.Data == "" {
			select {
			case <-ctx.Done():
				fmt.Fprintf(env.Stdout, "\ndetached; run %d keeps going (shepherd run logs -f %d)\n", id, id)
				return nil
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
	run, err := c.Run(ctx, id)
	if err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout)
	printRun(env, run)
	if run.State != store.RunSucceeded {
		return errSilent
	}
	return nil
}

func printRun(env Env, r api.RunView) {
	w := env.Stdout
	fmt.Fprintf(w, "run %d  %s  in lane %s (%s)\n", r.ID, r.State, r.Lane, r.Repo)
	model := r.Model
	if model == "" {
		model = "default model"
	}
	fmt.Fprintf(w, "  agent     %s (%s)\n", r.Agent, model)
	fmt.Fprintf(w, "  task      %s\n", oneLine(r.Prompt, 100))
	took := "running for " + time.Since(r.Started).Round(time.Second).String()
	if r.Ended != nil {
		took = r.Ended.Sub(r.Started).Round(time.Second).String()
	}
	fmt.Fprintf(w, "  time      %s, started %s\n", took, r.Started.Local().Format("15:04"))
	if r.ExitCode != nil {
		fmt.Fprintf(w, "  exit      %d\n", *r.ExitCode)
	}
	if r.State != store.RunRunning {
		fmt.Fprintf(w, "  commits   %d", r.Commits)
		if r.Commits > 0 {
			fmt.Fprintf(w, " (git log %s..%s)", short(r.StartSHA), short(r.EndSHA))
		}
		fmt.Fprintln(w)
	}
	if len(r.Outside) > 0 {
		fmt.Fprintf(w, "  OUTSIDE   %s (the push hook will refuse these)\n", strings.Join(r.Outside, ", "))
	}
	if r.Error != "" {
		fmt.Fprintf(w, "  error     %s\n", r.Error)
	}
	fmt.Fprintf(w, "  log       %s\n", r.Log)
}

// printLog prints a run's whole log as it is now.
func printLog(ctx context.Context, env Env, c *client.Client, id int64) error {
	var offset int64
	for {
		chunk, err := c.RunLog(ctx, id, offset)
		if err != nil {
			return err
		}
		fmt.Fprint(env.Stdout, chunk.Data)
		if chunk.Data == "" || chunk.Offset == offset {
			return nil
		}
		offset = chunk.Offset
	}
}

func short(sha string) string {
	if len(sha) > 9 {
		return sha[:9]
	}
	return sha
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

func runRun(ctx context.Context, env Env, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	sub := args[0]
	fs := flags("run "+sub, env)
	followFlag := fs.Bool("f", false, "follow until the run ends")
	all := fs.Bool("all", false, "every lane's runs, not only this lane's")
	withLog := fs.Bool("with-log", false, "show: also print the run's log")
	pos, err := parse(fs, args[1:])
	if err != nil {
		return err
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	if sub == "list" || sub == "ls" {
		if len(pos) > 0 {
			return errUsage
		}
		var laneID int64
		if !*all {
			if h, err := locate(ctx, env, c, ""); err == nil && h.Lane != nil {
				laneID = h.Lane.ID
			}
		}
		runs, err := c.Runs(ctx, laneID, "", 20)
		if err != nil {
			return err
		}
		if len(runs) == 0 {
			fmt.Fprintln(env.Stdout, "No runs yet. Start one with: shepherd lane run <lane> --agent claude \"task\"")
			return nil
		}
		fmt.Fprintf(env.Stdout, "%-5s %-8s %-22s %-12s %-7s %-6s %s\n", "ID", "AGENT", "LANE", "STATE", "COMMITS", "AGE", "TASK")
		for _, r := range runs {
			fmt.Fprintf(env.Stdout, "%-5d %-8s %-22s %-12s %-7d %-6s %s\n", r.ID, r.Agent, r.Lane, r.State, r.Commits, age(r.Started), oneLine(r.Prompt, 40))
		}
		return nil
	}
	if len(pos) != 1 {
		return errUsage
	}
	id, err := strconv.ParseInt(pos[0], 10, 64)
	if err != nil {
		return fmt.Errorf("run id %q is not a number", pos[0])
	}
	switch sub {
	case "show":
		r, err := c.Run(ctx, id)
		if err != nil {
			return err
		}
		printRun(env, r)
		if *withLog {
			fmt.Fprintln(env.Stdout, "\n--- log ---")
			return printLog(ctx, env, c, id)
		}
		return nil
	case "logs":
		if *followFlag {
			return follow(ctx, env, c, id)
		}
		return printLog(ctx, env, c, id)
	case "stop":
		if err := c.StopRun(ctx, id); err != nil {
			return err
		}
		fmt.Fprintf(env.Stdout, "asked run %d to stop\n", id)
		return nil
	}
	return errUsage
}
