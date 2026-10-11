package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/fold"
)

// laneDiff shows what a lane holds against its base: shepherd lane diff [lane]
// [--files | --file PATH] [--json]. It reads through the daemon and changes nothing.
func laneDiff(ctx context.Context, env Env, args []string) error {
	fs := flags("lane diff", env)
	repo := fs.String("repo", "", "repo, by its name in the workspace")
	stat := fs.Bool("stat", false, "commits and one line per file (the default)")
	files := fs.Bool("files", false, "the stat plus each file's hunk headers")
	file := fs.String("file", "", "one file's patch, or an untracked file's start")
	since := fs.String("since", "", "narrow to the commits after this SHA")
	until := fs.String("until", "", "with --since, end at this SHA (and leave out uncommitted work)")
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 || (*stat && *files) || (*stat && *file != "") || (*files && *file != "") {
		return errUsage
	}
	req := fold.DiffRequest{Mode: fold.DiffStat, Since: *since, Until: *until}
	switch {
	case *file != "":
		req.Mode, req.File = fold.DiffFile, *file
	case *files:
		req.Mode = fold.DiffFiles
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	h, err := locate(ctx, env, c, *repo)
	if err != nil {
		return err
	}
	target, err := pickLane(ctx, c, h, pos)
	if err != nil {
		return err
	}
	d, err := c.LaneDiff(ctx, target.ID, req)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(env, d)
	}
	writeLaneDiff(env.Stdout, d)
	return nil
}

// writeLaneDiff prints a diff read as text: the heads, the commits, then each group of
// files with its scope mark, or a file's patch.
func writeLaneDiff(w io.Writer, d fold.LaneDiff) {
	fmt.Fprintf(w, "lane %s (%s)  head %s  against %s at %s  read %s\n", d.Lane, d.Repo, short(d.Head),
		d.BaseRef, short(d.MergeBase), d.ReadAt.Local().Format("15:04:05"))
	if d.Mode == fold.DiffFile {
		fmt.Fprintf(w, "file %s\n", d.File)
		for _, n := range d.Notes {
			fmt.Fprintf(w, "note: %s\n", n)
		}
		if d.Patch != "" {
			fmt.Fprintln(w)
			fmt.Fprint(w, d.Patch)
			if !strings.HasSuffix(d.Patch, "\n") {
				fmt.Fprintln(w)
			}
		}
		return
	}
	fmt.Fprintf(w, "%d commit(s); files: %d committed, %d uncommitted, %d untracked",
		d.CommitsTotal, len(d.Committed), len(d.Uncommitted), len(d.Untracked))
	if d.OutOfScope > 0 {
		fmt.Fprintf(w, "; %d OUTSIDE the lane's scope", d.OutOfScope)
	}
	fmt.Fprintln(w)
	if len(d.Commits) > 0 {
		fmt.Fprintln(w, "\ncommits, oldest first:")
		for _, c := range d.Commits {
			fmt.Fprintf(w, "  %s  %s  (%s, %s ago, %s)\n", short(c.SHA), c.Subject, c.Author, age(c.Date),
				plural(c.Files, "file", "files"))
		}
	}
	group := func(title string, fs []fold.DiffFileInfo) {
		if len(fs) == 0 {
			return
		}
		fmt.Fprintf(w, "\n%s:\n", title)
		for _, f := range fs {
			mark := "   "
			if !f.InScope {
				mark = "OUT"
			}
			path := f.Path
			if f.OldPath != "" {
				path = f.OldPath + " -> " + f.Path
			}
			var count string
			switch {
			case f.Binary:
				count = fmt.Sprintf("binary, %d bytes", f.Size)
			case f.Status == "untracked":
				count = fmt.Sprintf("%d bytes", f.Size)
			default:
				count = fmt.Sprintf("+%d -%d", f.Added, f.Removed)
			}
			fmt.Fprintf(w, "  %-10s %s  %-14s %s\n", f.Status, mark, count, path)
			for _, h := range f.Hunks {
				fmt.Fprintf(w, "      %s\n", h)
			}
		}
	}
	group("committed", d.Committed)
	group("uncommitted (in the worktree, not committed)", d.Uncommitted)
	group("untracked (listed, never expanded: --file reads one)", d.Untracked)
	if d.CommitsTotal == 0 && len(d.Committed)+len(d.Uncommitted)+len(d.Untracked) == 0 {
		fmt.Fprintln(w, "\nnothing: the lane holds no changes against its base")
	}
	for _, n := range d.Notes {
		fmt.Fprintf(w, "note: %s\n", n)
	}
	if d.Truncated {
		fmt.Fprintln(w, "note: this read was cut by a limit (see above); --file PATH reads one file")
	}
}
