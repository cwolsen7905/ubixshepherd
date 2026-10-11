package git

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Read-only helpers for showing what is in a worktree. Each uses only flags in git 2.31,
// and keeps a repo's own configuration from running programs (--no-ext-diff,
// --no-textconv) or taking the index lock (--no-optional-locks).

// readOnly is what every read here starts with.
var readOnly = []string{"--no-optional-locks", "-c", "core.quotepath=off"}

// diffFlags are the flags every diff here carries.
var diffFlags = []string{"--no-ext-diff", "--no-textconv", "--no-color", "-M"}

// FileStat is one file's change in a diff.
type FileStat struct {
	Path    string
	OldPath string // the path before a rename, else empty
	// Status is one letter: A added, M modified, D deleted, R renamed, C copied, T type
	// changed.
	Status  string
	Added   int
	Removed int
	Binary  bool
}

// DiffStat lists the files a diff changes, with line counts and status. args select the
// comparison: ["base...HEAD"], ["HEAD"], ["a", "b"].
func DiffStat(ctx context.Context, dir string, args ...string) ([]FileStat, error) {
	base := append(append([]string{}, readOnly...), "diff")
	base = append(base, diffFlags...)
	num, err := Run(ctx, dir, append(append(append([]string{}, base...), "--numstat", "-z"), args...)...)
	if err != nil {
		return nil, err
	}
	st, err := Run(ctx, dir, append(append(append([]string{}, base...), "--name-status", "-z"), args...)...)
	if err != nil {
		return nil, err
	}
	status := map[string]string{}
	old := map[string]string{}
	f := splitNUL(st)
	for i := 0; i < len(f); i++ {
		code := f[i]
		if code == "" {
			continue
		}
		switch code[0] {
		case 'R', 'C':
			if i+2 < len(f) {
				status[f[i+2]], old[f[i+2]] = code[:1], f[i+1]
				i += 2
			}
		default:
			if i+1 < len(f) {
				status[f[i+1]] = code[:1]
				i++
			}
		}
	}
	var out []FileStat
	f = splitNUL(num)
	for i := 0; i < len(f); i++ {
		parts := strings.SplitN(f[i], "\t", 3)
		if len(parts) != 3 {
			continue
		}
		fs := FileStat{Path: parts[2]}
		if parts[2] == "" && i+2 < len(f) { // a rename: the paths follow as two records
			fs.OldPath, fs.Path = f[i+1], f[i+2]
			i += 2
		}
		if parts[0] == "-" {
			fs.Binary = true
		} else {
			fs.Added, _ = strconv.Atoi(parts[0])
			fs.Removed, _ = strconv.Atoi(parts[1])
		}
		fs.Status = status[fs.Path]
		if fs.Status == "" {
			fs.Status = "M"
		}
		if fs.OldPath == "" {
			fs.OldPath = old[fs.Path]
		}
		out = append(out, fs)
	}
	return out, nil
}

// DiffHunks returns, per path, the @@ header lines of a diff with no context, capped at
// perFile headers each. It is the shape of a change without its bodies.
func DiffHunks(ctx context.Context, dir string, perFile int, args ...string) (map[string][]string, error) {
	// Bounded: a change to a huge file would otherwise be read whole for its headers.
	out, _, err := Patch(ctx, dir, 4<<20, append([]string{"-U0"}, args...)...)
	if err != nil {
		return nil, err
	}
	hunks := map[string][]string{}
	var cur string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "--- a/"):
			cur = strings.TrimSuffix(strings.TrimPrefix(line, "--- a/"), "\t")
		case strings.HasPrefix(line, "+++ b/"):
			cur = strings.TrimSuffix(strings.TrimPrefix(line, "+++ b/"), "\t")
		case strings.HasPrefix(line, "@@ ") && cur != "":
			if len(hunks[cur]) < perFile {
				if i := strings.Index(line[2:], "@@"); i >= 0 {
					line = line[:i+4]
				}
				hunks[cur] = append(hunks[cur], line)
			}
		}
	}
	return hunks, nil
}

// Commit is one commit of a range.
type Commit struct {
	SHA     string
	Author  string
	Date    time.Time
	Subject string
	Files   int
}

// LogRange lists the newest limit commits of a range (a..b), returned oldest first, and
// how many the range holds in all.
func LogRange(ctx context.Context, dir, rng string, limit int) ([]Commit, int, error) {
	n, err := Run(ctx, dir, append(append([]string{}, readOnly...), "rev-list", "--count", rng)...)
	if err != nil {
		return nil, 0, err
	}
	total, _ := strconv.Atoi(n)
	if total == 0 {
		return nil, 0, nil
	}
	out, err := Run(ctx, dir, append(append([]string{}, readOnly...), "log", "--no-color", "-M",
		"--format=%x1e%H%x1f%an%x1f%aI%x1f%s", "--name-only", "-n", strconv.Itoa(limit), rng)...)
	if err != nil {
		return nil, 0, err
	}
	var cs []Commit
	for _, rec := range strings.Split(out, "\x1e") {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		head, files, _ := strings.Cut(rec, "\n")
		p := strings.SplitN(head, "\x1f", 4)
		if len(p) != 4 {
			continue
		}
		c := Commit{SHA: p[0], Author: p[1], Subject: p[3]}
		c.Date, _ = time.Parse(time.RFC3339, p[2])
		for _, f := range strings.Split(files, "\n") {
			if strings.TrimSpace(f) != "" {
				c.Files++
			}
		}
		cs = append(cs, c)
	}
	for i, j := 0, len(cs)-1; i < j; i, j = i+1, j-1 {
		cs[i], cs[j] = cs[j], cs[i]
	}
	return cs, total, nil
}

// UntrackedFiles lists files git does not track and does not ignore, up to limit paths.
// more says the list was cut: it stops reading git's output at the cap, so a stray
// dependency tree is not walked to the end.
func UntrackedFiles(ctx context.Context, dir string, limit int, pathspec ...string) (files []string, more bool, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	args := append(append([]string{"-C", dir}, readOnly...), "ls-files", "--others", "--exclude-standard", "-z")
	if len(pathspec) > 0 {
		args = append(append(args, "--"), pathspec...)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	if err := cmd.Start(); err != nil {
		return nil, false, err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	sc.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, 0); i >= 0 {
			return i + 1, data[:i], nil
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	for sc.Scan() {
		if len(files) == limit {
			more = true
			cancel()
			break
		}
		files = append(files, sc.Text())
	}
	werr := cmd.Wait()
	if !more && werr != nil {
		return nil, false, fmt.Errorf("git ls-files --others: %w", werr)
	}
	return files, more, nil
}

// Patch returns the text diff of args (limited to a path with "--", "path"), reading at
// most max bytes of git's output. cut says there was more; the last, partial line is
// then dropped.
func Patch(ctx context.Context, dir string, max int, args ...string) (patch string, cut bool, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	a := append(append([]string{"-C", dir}, readOnly...), "diff")
	a = append(a, diffFlags...)
	cmd := exec.CommandContext(ctx, "git", append(a, args...)...)
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	var errb bytes.Buffer
	cmd.Stderr = &errb
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", false, err
	}
	if err := cmd.Start(); err != nil {
		return "", false, err
	}
	b, _ := io.ReadAll(io.LimitReader(out, int64(max)+1))
	if len(b) > max {
		cut = true
		b = b[:max]
		if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
			b = b[:i+1]
		}
		cancel()
	}
	werr := cmd.Wait()
	if !cut && werr != nil {
		return "", false, fmt.Errorf("git diff: %s", strings.TrimSpace(errb.String()))
	}
	return string(b), cut, nil
}

func splitNUL(s string) []string {
	return strings.Split(strings.TrimRight(s, "\x00"), "\x00")
}
