package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/client"
)

// runRepo is `shepherd repo`: see, add and remove the repos Shepherd manages, without
// running init again. Adding takes effect at once, with no daemon restart.
func runRepo(ctx context.Context, env Env, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "list":
		return repoList(ctx, env, args[1:])
	case "add":
		return repoAdd(ctx, env, args[1:])
	case "remove":
		return repoRemove(ctx, env, args[1:])
	case "scan":
		return repoScan(ctx, env, args[1:])
	}
	return errUsage
}

func repoList(ctx context.Context, env Env, args []string) error {
	fs := flags("repo list", env)
	asJSON := fs.Bool("json", false, "print JSON")
	if pos, err := parse(fs, args); err != nil {
		return err
	} else if len(pos) > 0 {
		return errUsage
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	repos, err := c.Repos(ctx)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(env, repos)
	}
	if len(repos) == 0 {
		fmt.Fprintln(env.Stdout, "No repos. Run shepherd init <dir> to create a workspace.")
		return nil
	}
	printRepos(env, repos)
	return nil
}

func printRepos(env Env, repos []api.RepoView) {
	tw := tabwriter.NewWriter(env.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tWORKSPACE\tSTATE\tLANES\tPATH\tREMOTE")
	for _, r := range repos {
		state := r.State
		switch {
		case r.Removed != nil:
			state += " (removed)"
		case r.Reason != "":
			state += " (" + r.Reason + ")"
		}
		lanes := "-"
		if r.State == api.RepoManaged || r.State == api.RepoMissing {
			lanes = fmt.Sprint(r.OpenLanes)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Name, dash(r.Workspace), state, lanes, dash(r.Path), dash(r.Remote))
	}
	tw.Flush()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func repoAdd(ctx context.Context, env Env, args []string) error {
	fs := flags("repo add", env)
	ws := fs.String("workspace", "", "look names up in this workspace only")
	refs, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return errUsage
	}
	for i, ref := range refs {
		if refs[i], err = repoRef(env, ref); err != nil {
			return err
		}
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	got, err := c.AddRepos(ctx, api.AddRepos{Workspace: *ws, Repos: refs})
	if err != nil {
		return err
	}
	w := env.Stdout
	for _, r := range got {
		switch r.Outcome {
		case api.RepoAlreadyOn:
			fmt.Fprintf(w, "%s: already managed (workspace %s)\n", r.Name, r.Workspace)
			continue
		case api.RepoRestored:
			fmt.Fprintf(w, "%s: managed again, with its history (workspace %s)\n", r.Name, r.Workspace)
		default:
			fmt.Fprintf(w, "%s: added to workspace %s\n", r.Name, r.Workspace)
		}
		if r.Reason != "" {
			fmt.Fprintf(w, "  note: %s is not a clean first-time candidate (%s); added because you named it\n", r.Name, r.Reason)
		}
		fmt.Fprintf(w, "  artifacts: adds /.shepherd/artifacts/ to the repo's .git/info/exclude (local, never committed); refuse with the setting artifacts.exclude.%s=off\n", r.Name)
		if r.Profile {
			fmt.Fprintf(w, "  profile: repos.%s in config.yaml\n", r.Name)
		} else {
			fmt.Fprintf(w, "  no profile for %s in config.yaml: the defaults apply\n", r.Name)
		}
	}
	return nil
}

// repoRef turns a command-line argument into what the daemon takes: an absolute path,
// or a name. An argument is a path when it is absolute or begins with ., .. or ~;
// "group/lib" is a name.
func repoRef(env Env, arg string) (string, error) {
	switch {
	case strings.HasPrefix(arg, "~"):
		return "", fmt.Errorf("%s: the shell expands ~; pass an absolute path or a repo name", arg)
	case arg == "." || arg == ".." || strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") || strings.HasPrefix(arg, `.\`) || strings.HasPrefix(arg, `..\`):
		if env.Cwd == client.RemoteCwd {
			return "", fmt.Errorf("%s: on a remote host there is no current directory; pass an absolute path or a repo name", arg)
		}
		return canonicalDir(env, arg)
	case filepath.IsAbs(arg):
		if env.Remote != nil {
			return arg, nil
		}
		return canonicalDir(env, arg)
	}
	return arg, nil
}

func repoRemove(ctx context.Context, env Env, args []string) error {
	fs := flags("repo remove", env)
	why := fs.String("why", "", "why the repo is no longer managed")
	ws := fs.String("workspace", "", "the workspace the repo is in, when two have one by that name")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errUsage
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	all, err := c.Workspaces(ctx)
	if err != nil {
		return err
	}
	var hits []string
	var id int64
	for _, w := range all {
		if *ws != "" && w.Name != *ws {
			continue
		}
		for _, r := range w.Repos {
			if r.Name == pos[0] || r.Path == pos[0] {
				hits = append(hits, w.Name)
				id = r.ID
			}
		}
	}
	switch {
	case len(hits) == 0:
		return fmt.Errorf("no managed repo %q: see shepherd repo list", pos[0])
	case len(hits) > 1:
		return fmt.Errorf("%q is managed in more than one workspace (%s): pass --workspace", pos[0], strings.Join(hits, ", "))
	}
	if _, err := c.UnmanageRepo(ctx, id, *why); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "%s is no longer managed. Its directory, config.yaml profile and history are untouched; shepherd repo add %s manages it again.\n", pos[0], pos[0])
	return nil
}

// repoScan asks the daemon to look at the disk now, and prints what it is not
// managing. It adds nothing.
func repoScan(ctx context.Context, env Env, args []string) error {
	fs := flags("repo scan", env)
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return errUsage
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	repos, err := c.Repos(ctx)
	if err != nil {
		return err
	}
	var rest []api.RepoView
	for _, r := range repos {
		if r.State == api.RepoManaged || len(pos) == 1 && r.Workspace != pos[0] {
			continue
		}
		rest = append(rest, r)
	}
	if *asJSON {
		if rest == nil {
			rest = []api.RepoView{}
		}
		return printJSON(env, rest)
	}
	if len(rest) == 0 {
		fmt.Fprintln(env.Stdout, "Nothing found that Shepherd is not managing.")
		return nil
	}
	printRepos(env, rest)
	fmt.Fprintln(env.Stdout, "\nTo manage one: shepherd repo add <name>")
	return nil
}
