package fold

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/scope"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// PushRef is one line git gives a pre-push hook on stdin.
type PushRef struct {
	LocalRef  string `json:"local_ref"`
	LocalSHA  string `json:"local_sha"`
	RemoteRef string `json:"remote_ref"`
	RemoteSHA string `json:"remote_sha"`
}

// ParsePushRefs reads a pre-push hook's stdin.
func ParsePushRefs(in string) ([]PushRef, error) {
	var refs []PushRef
	for _, line := range strings.Split(strings.TrimSpace(in), "\n") {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 4 {
			return nil, fmt.Errorf("pre-push input %q: want 4 fields", line)
		}
		refs = append(refs, PushRef{f[0], f[1], f[2], f[3]})
	}
	return refs, nil
}

func zero(sha string) bool { return strings.Trim(sha, "0") == "" }

// Verdict answers a pre-push check.
type Verdict struct {
	OK bool `json:"ok"`
	// Lane is empty when the push is not from a lane, which Shepherd leaves alone.
	Lane     string   `json:"lane,omitempty"`
	Problems []string `json:"problems,omitempty"`
	Notes    []string `json:"notes,omitempty"`
}

// CheckPush decides whether a push may go ahead. From a lane: only the lane's branch,
// only changes inside its scope, and no push while an agent runs in it. From any
// checkout of a repo whose tags are reserved: release tags must be reserved, and contain
// their lane's merge. Everything else is left alone.
func (f *Fold) CheckPush(ctx context.Context, lane *store.Lane, repo *store.Repo, dir string, refs []PushRef) (Verdict, error) {
	v := Verdict{OK: true}
	if lane != nil {
		v.Lane = lane.Name
		if running, err := f.Store.Runs(ctx, lane.ID, store.RunRunning, 1); err == nil && len(running) > 0 {
			v.OK = false
			v.Problems = append(v.Problems, fmt.Sprintf("agent run %d (%s) is going in lane %s; agents Shepherd starts never push. Review the lane's commits when it ends, then push yourself",
				running[0].ID, running[0].Agent, lane.Name))
			return v, nil
		}
	}
	reserved := repo != nil && f.Config.Profile(repo.Name).Tags == config.TagsReserved
	for _, r := range refs {
		if zero(r.LocalSHA) {
			continue // deleting a remote ref
		}
		if strings.HasPrefix(r.RemoteRef, "refs/tags/") {
			if !reserved {
				continue
			}
			problem, err := f.checkTag(ctx, *repo, lane, dir, strings.TrimPrefix(r.RemoteRef, "refs/tags/"), r.LocalSHA)
			if err != nil {
				return v, err
			}
			if problem != "" {
				v.Problems = append(v.Problems, problem)
			}
			continue
		}
		if lane == nil {
			continue
		}
		if r.RemoteRef != "refs/heads/"+lane.Branch {
			v.Problems = append(v.Problems, fmt.Sprintf("lane %s pushes only its branch %s; this pushes to %s",
				lane.Name, lane.Branch, strings.TrimPrefix(r.RemoteRef, "refs/heads/")))
			continue
		}
		from, err := pushBase(ctx, dir, lane, r)
		if err != nil {
			return v, err
		}
		out, err := git.Run(ctx, dir, "diff", "--name-only", "--no-renames", from, r.LocalSHA)
		if err != nil {
			return v, err
		}
		var outside []string
		for _, file := range strings.Split(out, "\n") {
			if file != "" && !scope.Any(lane.Scope, file) {
				outside = append(outside, file)
			}
		}
		if len(outside) > 0 {
			v.Problems = append(v.Problems, fmt.Sprintf("changes outside lane %s's scope (%s):\n    %s",
				lane.Name, strings.Join(lane.Scope, ", "), strings.Join(firstLinesN(outside, 20), "\n    ")))
		}
	}
	v.OK = len(v.Problems) == 0
	return v, nil
}

// pushBase is where the pushed changes start: the remote's current tip when the branch
// exists there and is known locally, otherwise the fork point from the lane's base.
func pushBase(ctx context.Context, dir string, lane *store.Lane, r PushRef) (string, error) {
	if !zero(r.RemoteSHA) && git.Ok(ctx, dir, "cat-file", "-e", r.RemoteSHA+"^{commit}") {
		return r.RemoteSHA, nil
	}
	base := lane.Base
	if git.RefExists(ctx, dir, "refs/remotes/origin/"+base) {
		base = "origin/" + base
	}
	return git.Run(ctx, dir, "merge-base", base, r.LocalSHA)
}

func firstLinesN(s []string, n int) []string {
	if len(s) > n {
		return append(s[:n:n], fmt.Sprintf("... and %d more", len(s)-n))
	}
	return s
}

// hookMarker identifies a hook Shepherd wrote, so it may replace or remove it.
const hookMarker = "Installed by uBixShepherd"

// HookScript is the pre-push hook. It prefers the binary that installed it and falls
// back to shepherd on PATH, so a moved binary degrades to PATH instead of breaking.
func HookScript(exe string) []byte {
	return []byte(`#!/bin/sh
# ` + hookMarker + ` (shepherd hook install). Refuses a lane's push that leaves its
# branch or its scope. Skip once with git push --no-verify; remove with shepherd hook uninstall.
SHEPHERD=` + shQuote(exe) + `
[ -x "$SHEPHERD" ] || SHEPHERD=shepherd
exec "$SHEPHERD" hook pre-push "$@"
`)
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// HookLine is what to add to an existing pre-push hook to chain Shepherd's check.
const HookLine = `shepherd hook pre-push "$@" || exit 1`

// HookState describes a repo's pre-push hook.
type HookState struct {
	Path string `json:"path"`
	// Ours: Shepherd's hook is installed. Foreign: another pre-push hook is there.
	Ours    bool `json:"ours"`
	Foreign bool `json:"foreign"`
	// Tracked: the hooks directory is outside .git (core.hooksPath), usually a tracked
	// directory in the work tree, so a hook written there is a change to commit.
	Tracked bool `json:"tracked"`
}

// Hook reports the state of a repo's pre-push hook.
func Hook(ctx context.Context, repo string) (HookState, error) {
	rel, err := git.Run(ctx, repo, "rev-parse", "--git-path", "hooks")
	if err != nil {
		return HookState{}, err
	}
	dir := rel
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repo, rel)
	}
	common, err := git.Run(ctx, repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return HookState{}, err
	}
	cdir, _ := paths.Canonical(dir)
	ccommon, _ := paths.Canonical(common)
	st := HookState{Path: filepath.Join(cdir, "pre-push"), Tracked: !paths.Within(ccommon, cdir)}
	b, err := os.ReadFile(st.Path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return st, err
	case bytes.Contains(b, []byte(hookMarker)):
		st.Ours = true
	default:
		st.Foreign = true
	}
	return st, nil
}

// InstallHook writes Shepherd's pre-push hook. It never replaces another hook; with
// another one there it returns an error saying what to add to it.
func InstallHook(ctx context.Context, repo, exe string) (HookState, error) {
	st, err := Hook(ctx, repo)
	if err != nil {
		return st, err
	}
	if st.Foreign {
		return st, refuse("%s already has a pre-push hook that is not Shepherd's. Add this line to it:\n    %s", st.Path, HookLine)
	}
	if err := os.MkdirAll(filepath.Dir(st.Path), 0o755); err != nil {
		return st, err
	}
	if err := os.WriteFile(st.Path, HookScript(exe), 0o755); err != nil {
		return st, err
	}
	st.Ours = true
	return st, nil
}

// UninstallHook removes Shepherd's hook, and nothing else.
func UninstallHook(ctx context.Context, repo string) (HookState, error) {
	st, err := Hook(ctx, repo)
	if err != nil || !st.Ours {
		return st, err
	}
	if err := os.Remove(st.Path); err != nil {
		return st, err
	}
	st.Ours = false
	return st, nil
}

// ensureHook installs the hook when that is safe without asking: no pre-push hook yet,
// in a hooks directory git keeps out of the work tree. Otherwise it says what to do.
func (f *Fold) ensureHook(ctx context.Context, repo string) string {
	if f.Exe == "" {
		return ""
	}
	st, err := Hook(ctx, repo)
	switch {
	case err != nil:
		return "could not check the pre-push hook: " + err.Error()
	case st.Ours:
		return ""
	case st.Foreign:
		return fmt.Sprintf("scope is not enforced on push: %s is another hook. Add to it: %s", st.Path, HookLine)
	case st.Tracked:
		return fmt.Sprintf("scope is not enforced on push: this repo's hooks live outside .git (%s), so Shepherd does not write there unasked. Run shepherd hook install, and commit the hook if that directory is tracked", filepath.Dir(st.Path))
	}
	if _, err := InstallHook(ctx, repo, f.Exe); err != nil {
		return "could not install the pre-push hook: " + err.Error()
	}
	return "installed the pre-push hook at " + st.Path
}
