package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/ubixsys/ubixshepherd/internal/forge"
)

// linkForge builds the forge for a remote; tests replace it with a fake API.
var linkForge = func(remote string) (forge.Linker, error) {
	f, err := forge.For(remote)
	if err != nil {
		return nil, err
	}
	l, ok := f.(forge.Linker)
	if !ok {
		return nil, fmt.Errorf("%s cannot build links yet", f.Name())
	}
	return l, nil
}

// runLinks is `shepherd links`: clickable URLs for the forge things a report names, so
// nobody builds them by hand. A review thread's URL is its first note's: GitLab cannot
// link a discussion id, so the forge is asked for the note.
func runLinks(ctx context.Context, env Env, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	kind, args := args[0], args[1:]
	fs := flags("links "+kind, env)
	repo := fs.String("repo", "", "the repo (default: the current directory's)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	want := 1
	if kind == "thread" {
		want = 2
	}
	if len(pos) != want || (kind != "thread" && kind != "mr" && kind != "pipeline" && kind != "job") {
		return errUsage
	}
	n, err := strconv.ParseInt(pos[0], 10, 64)
	if err != nil || n <= 0 {
		return fmt.Errorf("%q is not a merge request, pipeline or job number", pos[0])
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	h, err := locate(ctx, env, c, *repo)
	if err != nil {
		return err
	}
	if h.Repo == nil {
		return errors.New("name the repo with --repo, or run this inside one")
	}
	l, err := linkForge(h.Repo.Remote)
	if err != nil {
		return err
	}
	var out string
	switch kind {
	case "thread":
		out, err = l.ThreadURL(ctx, int(n), pos[1])
		if err != nil {
			return err
		}
	case "mr":
		out = l.MRURL(int(n))
	case "pipeline":
		out = l.PipelineURL(n)
	case "job":
		out = l.JobURL(n)
	}
	fmt.Fprintln(env.Stdout, out)
	return nil
}
