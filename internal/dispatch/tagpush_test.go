package dispatch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

const tagProfile = "repos:\n  app:\n    gate: \"true\"\n    tags: reserved\n    tag_prefix: v\n    autonomy:\n      push: shepherd\n      tag: agent\n"

func tagGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func originTags(t *testing.T, f *fixture) string {
	return tagGit(t, f.origin, "tag", "--list")
}

// tagCase reserves tag for the lane and runs pushTags under profile.
func tagCase(t *testing.T, profile, tag string, laneID func(store.Lane) int64) (*fixture, store.Reservation, []string) {
	t.Helper()
	f, _ := shipFixture(t, "ok", profile)
	ctx := context.Background()
	repo, err := f.st.Repo(ctx, f.lane.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.st.CreateReservation(ctx, store.Reservation{RepoID: repo.ID, LaneID: laneID(f.lane), Tag: tag, State: store.TagReserved})
	if err != nil {
		t.Fatal(err)
	}
	return f, res, nil
}

func (f *fixture) pushTagsNow(t *testing.T) []string {
	t.Helper()
	ctx := context.Background()
	repo, _ := f.st.Repo(ctx, f.lane.RepoID)
	return f.runner.pushTags(ctx, f.lane, repo, f.runner.Conf().Profile(repo.Name))
}

func (f *fixture) state(t *testing.T, id int64) string {
	t.Helper()
	live, _ := f.st.Reservations(context.Background(), f.lane.RepoID)
	for _, r := range live {
		if r.ID == id {
			return r.State
		}
	}
	return "gone"
}

func own(l store.Lane) int64 { return l.ID }

func TestPushTagsPushesAReservedTagOnBase(t *testing.T) {
	f, res, _ := tagCase(t, tagProfile, "v0.1.0", own)
	tagGit(t, f.lane.Worktree, "tag", "v0.1.0", "origin/main")
	said := f.pushTagsNow(t)
	if originTags(t, f) != "v0.1.0" || len(said) != 1 || !strings.Contains(said[0], "pushed tag v0.1.0") {
		t.Errorf("tags = %q, said = %q", originTags(t, f), said)
	}
	if s := f.state(t, res.ID); s != store.TagPushed {
		t.Errorf("state = %s", s)
	}
}

func TestPushTagsLeavesWhatIsNotTheAgentsToPush(t *testing.T) {
	cases := map[string]struct {
		profile, tag string
		lane         func(store.Lane) int64
		said         bool
	}{
		"tag human":     {strings.Replace(tagProfile, "tag: agent", "tag: human", 1), "v0.1.0", own, false},
		"push agent":    {strings.Replace(tagProfile, "push: shepherd", "push: agent", 1), "v0.1.0", own, false},
		"other lane":    {tagProfile, "v0.1.0", func(l store.Lane) int64 { return l.ID + 100 }, false},
		"not a release": {tagProfile, "nightly", own, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f, res, _ := tagCase(t, c.profile, c.tag, c.lane)
			tagGit(t, f.lane.Worktree, "tag", c.tag, "origin/main")
			f.pushTagsNow(t)
			if got := originTags(t, f); got != "" {
				t.Errorf("pushed %q", got)
			}
			if s := f.state(t, res.ID); s != store.TagReserved {
				t.Errorf("state = %s", s)
			}
		})
	}
}

func TestPushTagsRefusesACommitOffBase(t *testing.T) {
	f, res, _ := tagCase(t, tagProfile, "v0.1.0", own)
	os.MkdirAll(filepath.Join(f.lane.Worktree, "src"), 0o755)
	os.WriteFile(filepath.Join(f.lane.Worktree, "src", "x.go"), []byte("x"), 0o644)
	tagGit(t, f.lane.Worktree, "add", ".")
	tagGit(t, f.lane.Worktree, "commit", "-q", "-m", "work")
	tagGit(t, f.lane.Worktree, "tag", "v0.1.0")
	said := f.pushTagsNow(t)
	if originTags(t, f) != "" || len(said) != 1 || !strings.Contains(said[0], "not on origin/main") {
		t.Errorf("tags = %q, said = %q", originTags(t, f), said)
	}
	if s := f.state(t, res.ID); s != store.TagReserved {
		t.Errorf("state = %s", s)
	}
	waitFeed(t, f, "not pushing tag v0.1.0")
}

func TestPushTagsRefusesAnotherCommitOnOrigin(t *testing.T) {
	f, _, _ := tagCase(t, tagProfile, "v0.1.0", own)
	tagGit(t, f.lane.Worktree, "tag", "v0.1.0", "origin/main")
	// origin has the tag, on a different commit.
	tmp := filepath.Join(t.TempDir(), "c")
	tagGit(t, filepath.Dir(tmp), "clone", "-q", f.origin, tmp)
	os.WriteFile(filepath.Join(tmp, "o.txt"), []byte("o"), 0o644)
	tagGit(t, tmp, "add", ".")
	tagGit(t, tmp, "-c", "user.name=t", "-c", "user.email=t@e", "commit", "-q", "-m", "other")
	tagGit(t, tmp, "tag", "v0.1.0")
	tagGit(t, tmp, "push", "-q", "origin", "v0.1.0")
	said := f.pushTagsNow(t)
	if len(said) != 1 || !strings.Contains(said[0], "different commit") {
		t.Errorf("said = %q", said)
	}
}

func TestPushTagsReportsAFailedPush(t *testing.T) {
	f, res, _ := tagCase(t, tagProfile, "v0.1.0", own)
	tagGit(t, f.lane.Worktree, "tag", "v0.1.0", "origin/main")
	hook := filepath.Join(f.origin, "hooks", "pre-receive")
	os.WriteFile(hook, []byte("#!/bin/sh\necho no tags here >&2\nexit 1\n"), 0o755)
	said := f.pushTagsNow(t)
	if len(said) != 1 || !strings.Contains(said[0], "failed") {
		t.Errorf("said = %q", said)
	}
	if s := f.state(t, res.ID); s != store.TagReserved {
		t.Errorf("state = %s", s)
	}
}
