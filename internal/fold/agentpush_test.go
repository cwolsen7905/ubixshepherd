package fold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// agentProfile sets the repo's profile the way a parsed config would once
// autonomy.push: agent exists (Parse refuses the value until then).
func agentProfile(f *fixture, push string, forbid ...string) {
	p := config.Profile{Forbid: forbid}
	p.Autonomy.Push = push
	f.fold.Config.Repos = map[string]config.Profile{"app": p}
}

func TestAgentPushOfOwnBranch(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	l := f.open("work")
	f.commit(l.Worktree, "src/ok.go")
	if _, err := f.fold.Store.CreateRun(ctx, store.Run{LaneID: l.ID, Agent: "claude", Prompt: "x", State: store.RunRunning, Log: "l"}); err != nil {
		t.Fatal(err)
	}
	ref := []PushRef{pushRef(t, l.Worktree, "work")}
	check := func(remote string, refs []PushRef) Verdict {
		t.Helper()
		v, err := f.fold.CheckPushTo(ctx, &l, &f.repo, l.Worktree, remote, refs)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}

	if v := check("origin", ref); v.OK || !strings.Contains(v.Problems[0], "never push") {
		t.Errorf("default profile: %+v", v)
	}
	agentProfile(f, "shepherd")
	if v := check("origin", ref); v.OK {
		t.Errorf("push: shepherd does not let the agent push: %+v", v)
	}

	agentProfile(f, PushAgent)
	if v := check("origin", ref); !v.OK {
		t.Errorf("agent push of its own branch: %+v", v)
	}
	url := gitT(t, l.Worktree, "remote", "get-url", "origin")
	if v := check(url, ref); !v.OK {
		t.Errorf("agent push by origin's URL: %+v", v)
	}
	if v := check("", ref); v.OK {
		t.Errorf("unknown remote must be refused: %+v", v)
	}
	if v := check("elsewhere", ref); v.OK || !strings.Contains(v.Problems[0], "only to the repo's origin") {
		t.Errorf("other remote: %+v", v)
	}
	if v, err := f.fold.CheckPush(ctx, &l, &f.repo, l.Worktree, ref); err != nil || v.OK {
		t.Errorf("CheckPush without a remote: %+v %v", v, err)
	}

	other := pushRef(t, l.Worktree, "work")
	other.RemoteRef = "refs/heads/main"
	if v := check("origin", []PushRef{other}); v.OK || !strings.Contains(v.Problems[0], "pushes only its branch") {
		t.Errorf("agent push to main: %+v", v)
	}

	f.commit(l.Worktree, "docs/stray.md")
	if v := check("origin", []PushRef{pushRef(t, l.Worktree, "work")}); v.OK || !strings.Contains(v.Problems[0], "docs/stray.md") {
		t.Errorf("agent push outside scope: %+v", v)
	}
}

func TestPushRefusesForbiddenMessage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	l := f.open("work")
	agentProfile(f, "", "(?i)co-authored-by")
	f.commit(l.Worktree, "src/a.go")
	if v, err := f.fold.CheckPush(ctx, &l, &f.repo, l.Worktree, []PushRef{pushRef(t, l.Worktree, "work")}); err != nil || !v.OK {
		t.Fatalf("clean message: %+v %v", v, err)
	}
	if err := os.WriteFile(filepath.Join(l.Worktree, "src/b.go"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, l.Worktree, "add", ".")
	gitT(t, l.Worktree, "commit", "-q", "-m", "add b\n\nCo-authored-by: A Bot <bot@example.com>")
	v, err := f.fold.CheckPush(ctx, &l, &f.repo, l.Worktree, []PushRef{pushRef(t, l.Worktree, "work")})
	if err != nil || v.OK || !strings.Contains(v.Problems[0], "Co-authored-by") {
		t.Fatalf("forbidden trailer: %+v %v", v, err)
	}

	// Already on the remote: only the commits being pushed are checked.
	gitT(t, l.Worktree, "push", "-q", "--no-verify", "origin", "HEAD:work")
	remote := gitT(t, l.Worktree, "rev-parse", "HEAD")
	f.commit(l.Worktree, "src/c.go")
	r := pushRef(t, l.Worktree, "work")
	r.RemoteSHA = remote
	if v, err := f.fold.CheckPush(ctx, &l, &f.repo, l.Worktree, []PushRef{r}); err != nil || !v.OK {
		t.Errorf("old forbidden commit already pushed: %+v %v", v, err)
	}
}
