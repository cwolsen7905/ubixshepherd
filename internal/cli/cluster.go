package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// runCluster is `shepherd cluster status | join`: what this daemon is in a cluster, and
// enrolling it with a controller (docs/cluster.md).
func runCluster(ctx context.Context, env Env, args []string) error {
	sub := "status"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := flags("cluster "+sub, env)
	asJSON := fs.Bool("json", false, "status: print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	switch sub {
	case "status":
		if len(pos) > 0 {
			return errUsage
		}
		cl, err := c.Cluster(ctx)
		if err != nil {
			return err
		}
		if *asJSON {
			return printJSON(env, cl)
		}
		writeCluster(env.Stdout, cl, time.Now())
		return nil
	case "join":
		if len(pos) != 2 {
			return errUsage
		}
		got, err := c.ClusterJoin(ctx, pos[0], pos[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(env.Stdout, "joined %s as %s; the daemon is linking now (shepherd cluster status)\n", got.Controller, got.Name)
		fmt.Fprintf(env.Stdout, "to keep it across a config reload, you may set cluster.controller: %s in the config\n", got.Controller)
		return nil
	}
	return errUsage
}

func writeCluster(w io.Writer, c api.Cluster, now time.Time) {
	switch c.Role {
	case api.RoleController:
		fmt.Fprintf(w, "controller %s\n", c.DaemonID)
		if len(c.Workers) == 0 {
			fmt.Fprintln(w, "  no workers yet: shepherd worker add <name>, then shepherd cluster join <url> <code> on the machine")
		}
		for _, m := range c.Workers {
			fmt.Fprintf(w, "  %-12s %s\n", m.Name, workerLine(m, now))
			writeWorkerReport(w, m)
		}
	case api.RoleWorker:
		l := c.Link
		fmt.Fprintf(w, "worker %s\n", c.DaemonID)
		if l == nil {
			return
		}
		fmt.Fprintf(w, "  controller: %s\n", l.Controller)
		fmt.Fprintf(w, "  link:       %s%s\n", l.State, since(l.Since, now))
		if l.Tenant != "" {
			fmt.Fprintf(w, "  tenant:     %s\n", l.Tenant)
		}
		if l.ControllerID != "" && l.State == api.LinkConnected {
			fmt.Fprintf(w, "  controller id %s, version %s, api %d\n", l.ControllerID, l.ControllerVersion, l.ControllerAPI)
		}
		if l.Error != "" {
			fmt.Fprintf(w, "  why:        %s\n", l.Error)
		}
		if !l.NextTry.IsZero() {
			fmt.Fprintf(w, "  next:       try again in %s\n", max(l.NextTry.Sub(now), 0).Round(time.Second))
		}
	default:
		fmt.Fprintln(w, "alone: not a controller and not linked to one (cluster.role: controller, or shepherd cluster join <url> <code>)")
	}
}

func workerLine(m api.ClusterWorker, now time.Time) string {
	var parts []string
	switch {
	case m.PendingID != "":
		parts = append(parts, "refused: its daemon id changed to "+m.PendingID+"; confirm with shepherd worker confirm "+m.Name)
	case m.Online:
		parts = append(parts, "online"+since(m.Since, now))
	case !m.Enrolled && !m.CodeExpires.IsZero():
		parts = append(parts, "waiting to enrol, code ends in "+max(m.CodeExpires.Sub(now), 0).Round(time.Second).String())
	case !m.Enrolled:
		parts = append(parts, "not enrolled (revoked, or its code ended): shepherd worker add "+m.Name)
	case m.Since.IsZero():
		parts = append(parts, "offline, never linked")
	default:
		parts = append(parts, "offline, last seen "+ago(m.Since, now))
	}
	if m.Online {
		if m.Version != "" {
			parts = append(parts, "v"+strings.TrimPrefix(m.Version, "v"))
		}
		parts = append(parts, fmt.Sprintf("api %d", m.APILevel), fmt.Sprintf("%d running, %d waiting", m.RunsGoing, m.Waiting))
	}
	if m.Tenant != "" {
		parts = append(parts, "tenant "+m.Tenant)
	}
	if m.DaemonID != "" {
		parts = append(parts, "id "+m.DaemonID)
	}
	return strings.Join(parts, "  ")
}

// writeWorkerReport lists what an online worker reported over its link: the agent CLIs it
// has and its repos, by origin and its own name for each.
func writeWorkerReport(w io.Writer, m api.ClusterWorker) {
	if !m.Online {
		return
	}
	const pad = "               "
	if !slices.Contains(m.Capabilities, api.CapRepos) {
		fmt.Fprintln(w, pad+"repos and agents not reported: the worker's version predates the report")
		return
	}
	agents := "none found"
	if len(m.Agents) > 0 {
		agents = strings.Join(m.Agents, ", ")
	}
	fmt.Fprintln(w, pad+"agents  "+agents)
	if len(m.Repos) == 0 && m.ReposTruncated == 0 {
		fmt.Fprintln(w, pad+"repos   none with an origin")
		return
	}
	for i, r := range m.Repos {
		label := "        "
		if i == 0 {
			label = "repos   "
		}
		fmt.Fprintf(w, "%s%s%s as %s\n", pad, label, r.Origin, r.Name)
	}
	if m.ReposTruncated > 0 {
		fmt.Fprintf(w, "%s        and %d more, left out of the report to keep it under the link's size limit\n", pad, m.ReposTruncated)
	}
}

func since(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	return " since " + t.Local().Format("15:04") + " (" + ago(t, now) + ")"
}

func ago(t, now time.Time) string {
	d := max(now.Sub(t), 0)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
