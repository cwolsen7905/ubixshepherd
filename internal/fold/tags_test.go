package fold

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestNext(t *testing.T) {
	tags := []string{"v0.9.0", "v0.10.0", "v0.10.1-beta.1", "release-2", "v0.2.7"}
	cases := map[string]string{"major": "1.0.0", "minor": "0.11.0", "patch": "0.10.1"}
	for bump, want := range cases {
		v, err := Next("v", bump, tags, nil)
		if err != nil || v.String() != want {
			t.Errorf("Next %s = %v, %v; want %s", bump, v, err, want)
		}
	}
	if v, _ := Next("v", "minor", tags, []string{"v0.11.0"}); v.String() != "0.12.0" {
		t.Errorf("a reservation is skipped past: %v", v)
	}
	if _, err := Next("v", "huge", nil, nil); err == nil {
		t.Error("bad bump accepted")
	}
	if v, ok := ParseTag("v", "v1.2.3"); !ok || v.String() != "1.2.3" {
		t.Errorf("ParseTag = %v %v", v, ok)
	}
}

func reservedConfig(t *testing.T) config.Config {
	c, err := config.Parse([]byte("repos:\n  app:\n    tags: reserved\n"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestReserveRespectsTheRemoteAndNeverRepeats(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// The remote already has v0.44.0, which this clone has not fetched.
	other := strings.TrimSuffix(f.ws, "/ws") + "/tagger"
	gitT(t, strings.TrimSuffix(f.ws, "/ws"), "clone", "-q", f.origin, other)
	gitT(t, other, "tag", "v0.44.0")
	gitT(t, other, "push", "-q", "origin", "v0.44.0")

	r, err := f.fold.Reserve(ctx, f.repo.ID, 0, "minor")
	if err != nil || r.Tag != "v0.45.0" {
		t.Fatalf("reserve = %+v, %v", r, err)
	}
	// Ten at once: ten different versions.
	var mu sync.Mutex
	seen := map[string]bool{r.Tag: true}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := f.fold.Reserve(ctx, f.repo.ID, 0, "patch")
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if seen[r.Tag] {
				t.Errorf("%s handed out twice", r.Tag)
			}
			seen[r.Tag] = true
		}()
	}
	wg.Wait()
	if len(seen) != 11 {
		t.Errorf("%d distinct versions, want 11", len(seen))
	}
	if err := f.fold.ReleaseTag(ctx, f.repo.ID, "v0.45.0"); err != nil {
		t.Fatal(err)
	}
	if err := f.fold.ReleaseTag(ctx, f.repo.ID, "v9.9.9"); !errors.Is(err, ErrRefused) {
		t.Errorf("releasing an unreserved tag: %v", err)
	}
}

func tagRef(t *testing.T, dir, tag string) PushRef {
	sha := gitT(t, dir, "rev-parse", "HEAD")
	return PushRef{LocalRef: "refs/tags/" + tag, LocalSHA: sha, RemoteRef: "refs/tags/" + tag, RemoteSHA: strings.Repeat("0", 40)}
}

func TestTagPushRules(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.open("a")
	b, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "b", Scope: []string{"docs/**"}})
	if err != nil {
		t.Fatal(err)
	}

	// Free repo: tags are not Shepherd's business.
	if v, _ := f.fold.CheckPush(ctx, &a, &f.repo, a.Worktree, []PushRef{tagRef(t, a.Worktree, "v1.0.0")}); !v.OK {
		t.Errorf("free repo refused a tag: %+v", v)
	}

	f.fold.Config = reservedConfig(t)
	check := func(lane *store.Lane, dir, tag string) Verdict {
		v, err := f.fold.CheckPush(ctx, lane, &f.repo, dir, []PushRef{tagRef(t, dir, tag)})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if v := check(&a, a.Worktree, "v0.1.0"); v.OK || !strings.Contains(v.Problems[0], "not reserved") {
		t.Errorf("unreserved tag: %+v", v)
	}
	ra, _ := f.fold.Reserve(ctx, f.repo.ID, a.ID, "minor")
	if v := check(&b.Lane, b.Worktree, ra.Tag); v.OK || !strings.Contains(v.Problems[0], "reserved by lane a") {
		t.Errorf("another lane's tag: %+v", v)
	}
	if v := check(&a, a.Worktree, ra.Tag); !v.OK {
		t.Errorf("own tag refused: %+v", v)
	}
	rs, _ := f.fold.Store.Reservations(ctx, f.repo.ID)
	if rs[0].State != store.TagPushed || rs[0].SHA == "" {
		t.Errorf("reservation after push = %+v", rs[0])
	}
	// From the main checkout (no lane), a reserved tag may go; a non-release tag always may.
	if v := check(nil, f.repo.Path, ra.Tag); !v.OK {
		t.Errorf("reserved tag from the main checkout: %+v", v)
	}
	if v := check(nil, f.repo.Path, "nightly"); !v.OK {
		t.Errorf("non-release tag: %+v", v)
	}
}

func TestTagMustContainItsLanesMerge(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.fold.Config = reservedConfig(t)
	l := f.open("rel")
	before := gitT(t, f.repo.Path, "rev-parse", "HEAD")
	f.commit(l.Worktree, "src/feature.go")
	merge := gitT(t, l.Worktree, "rev-parse", "HEAD")
	gitT(t, l.Worktree, "push", "-q", "origin", "HEAD:main")
	r, _ := f.fold.Reserve(ctx, f.repo.ID, l.ID, "minor")
	f.fold.Store.PutLaneForge(ctx, store.LaneForge{LaneID: l.ID, MR: 1, MRState: "merged", MergeSHA: merge})

	early := PushRef{LocalRef: "refs/tags/" + r.Tag, LocalSHA: before, RemoteRef: "refs/tags/" + r.Tag, RemoteSHA: strings.Repeat("0", 40)}
	if v, _ := f.fold.CheckPush(ctx, nil, &f.repo, f.repo.Path, []PushRef{early}); v.OK || !strings.Contains(v.Problems[0], "does not contain its lane's merge") {
		t.Errorf("tag before the merge: %+v", v)
	}
	good := early
	good.LocalSHA = merge
	if v, _ := f.fold.CheckPush(ctx, nil, &f.repo, f.repo.Path, []PushRef{good}); !v.OK {
		t.Errorf("tag on the merge: %+v", v)
	}
	if rs, _ := f.fold.Store.Reservations(ctx, f.repo.ID); rs[0].State != store.TagVerified {
		t.Errorf("reservation = %+v", rs[0])
	}
}
