package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// runWorkspace is `shepherd workspace`: act on a registered workspace as a whole.
func runWorkspace(ctx context.Context, env Env, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "rename":
		return workspaceRename(ctx, env, args[1:])
	}
	return errUsage
}

// workspaceRename changes a workspace's name, found by its current name or its path.
// Nothing else changes, and init is not run again.
func workspaceRename(ctx context.Context, env Env, args []string) error {
	fs := flags("workspace rename", env)
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return errUsage
	}
	oldRef, name := pos[0], strings.TrimSpace(pos[1])
	if name == "" || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("workspace name %q: must be non-empty with no slashes", name)
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	all, err := c.Workspaces(ctx)
	if err != nil {
		return err
	}
	var hit *api.WorkspaceDetail
	var names []string
	for i, w := range all {
		names = append(names, w.Name)
		if w.Name == oldRef || w.Path == oldRef {
			hit = &all[i]
		}
	}
	if hit == nil {
		return fmt.Errorf("no workspace %q (known: %s)", oldRef, strings.Join(names, ", "))
	}
	if hit.Name == name {
		fmt.Fprintf(env.Stdout, "The workspace at %s is already named %s.\n", hit.Path, name)
		return nil
	}
	for _, w := range all {
		if w.ID != hit.ID && w.Name == name {
			return fmt.Errorf("the name %q is already used by the workspace at %s", name, w.Path)
		}
	}
	old := hit.Name
	got, err := c.RenameWorkspace(ctx, hit.ID, name)
	if err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Renamed workspace %s to %s (%s). Its repos, lanes and history are unchanged.\n", old, got.Name, got.Path)
	fmt.Fprintf(env.Stdout, "Anything that names it %q (--workspace, $%s, \"workspace\" in hosts.yaml) needs the new name.\n", old, WorkspaceEnv)
	return nil
}
