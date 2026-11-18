package watch

import (
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/forge"
)

// kelpieFixture is an approved request under after_approval, with the follow-through
// owner named review-bot and the lane's open threads set by each test.
func kelpieFixture(t *testing.T) (*fixture, *mergingForge) {
	t.Helper()
	f, mf, _ := mergeFixture(t, config.AfterApproval)
	cfg := config.Default()
	cfg.Defaults.Autonomy.Merge = config.AfterApproval
	cfg.Defaults.Watch.FollowThrough = config.FollowThrough{Owner: config.OwnerExternal, Name: "reviewbot", Patience: "30m", Author: []string{"review-bot"}}
	f.run.SetConfig(cfg)
	f.f.mr.URL = "https://gl.example.com/g/app/-/merge_requests/7"
	return f, mf
}

func botThread(id string, note int64) forge.Thread {
	return forge.Thread{ID: id, Author: "review-bot", Body: "summary", NoteID: note, Notes: 1,
		URL:     "https://gl.example.com/g/app/-/merge_requests/7#note_" + id,
		Entries: []forge.Note{{ID: note, Author: "review-bot", Text: "summary"}}}
}

func raised(t *testing.T, f *fixture) int {
	t.Helper()
	return strings.Count(f.feed(t), "waits only on")
}

func blocking(t *testing.T, f *fixture) string {
	t.Helper()
	lf, err := f.st.LaneForge(f.ctx, f.lane.ID)
	if err != nil {
		t.Fatal(err)
	}
	return lf.KelpieBlocking
}

func TestApprovedMRWaitingOnlyOnOwnerThreadsIsRaisedOnce(t *testing.T) {
	f, mf := kelpieFixture(t)
	f.f.threads = []forge.Thread{botThread("1", 11), botThread("2", 12)}
	f.w.Check(f.ctx)
	f.w.Check(f.ctx)
	f.w.Check(f.ctx)
	if n := raised(t, f); n != 1 {
		t.Fatalf("raised %d times: %s", n, f.feed(t))
	}
	got := f.feed(t)
	for _, want := range []string{"!7", "approved by pat", "note_1", "note_2"} {
		if !strings.Contains(got, want) {
			t.Errorf("feed lacks %q: %s", want, got)
		}
	}
	if blocking(t, f) != "1,2" || len(mf.merges) != 0 {
		t.Errorf("blocking = %q, merges = %v", blocking(t, f), mf.merges)
	}
}

func TestOwnerAnsweredFindingCountsAsOwners(t *testing.T) {
	f, _ := kelpieFixture(t)
	th := forge.Thread{ID: "5", Author: "pat", Notes: 2, URL: "u#note_5",
		Entries: []forge.Note{{Author: "pat", Text: "finding"}, {Author: "review-bot", Text: "Dismissed: fine"}}}
	f.f.threads = []forge.Thread{th}
	f.w.Check(f.ctx)
	if raised(t, f) != 1 {
		t.Fatalf("feed = %s", f.feed(t))
	}
}

func TestNewOwnerThreadRaisesAgain(t *testing.T) {
	f, _ := kelpieFixture(t)
	f.f.threads = []forge.Thread{botThread("1", 11)}
	f.w.Check(f.ctx)
	f.f.threads = append(f.f.threads, botThread("2", 12))
	f.w.Check(f.ctx)
	f.w.Check(f.ctx)
	if n := raised(t, f); n != 2 {
		t.Fatalf("raised %d times: %s", n, f.feed(t))
	}
	if blocking(t, f) != "1,2" {
		t.Errorf("blocking = %q", blocking(t, f))
	}
}

func TestResolvedOwnerThreadsClearAndRaiseAgainLater(t *testing.T) {
	f, _ := kelpieFixture(t)
	f.f.threads = []forge.Thread{botThread("1", 11)}
	f.w.Check(f.ctx)
	f.f.threads = nil
	f.f.mr.MergeStatus = "checking" // not mergeable yet, so no merge is tried at this head
	f.w.Check(f.ctx)
	if blocking(t, f) != "" {
		t.Fatalf("not cleared: %q", blocking(t, f))
	}
	f.f.threads = []forge.Thread{botThread("1", 11)}
	f.w.Check(f.ctx)
	if n := raised(t, f); n != 2 {
		t.Fatalf("raised %d times", n)
	}
}

func TestMergedOrClosedRequestClearsTheRaise(t *testing.T) {
	f, _ := kelpieFixture(t)
	f.f.threads = []forge.Thread{botThread("1", 11)}
	f.w.Check(f.ctx)
	f.f.mr.State = "closed"
	f.w.Check(f.ctx)
	if blocking(t, f) != "" {
		t.Fatalf("not cleared: %q", blocking(t, f))
	}
}

func TestNotRaisedWithoutApprovalOrWithOtherThreads(t *testing.T) {
	cases := map[string]func(f *fixture, mf *mergingForge){
		"not approved": func(_ *fixture, mf *mergingForge) { mf.approvers = nil },
		"approval predates the head": func(_ *fixture, mf *mergingForge) {
			mf.stale = map[string]bool{"pat": true}
		},
		"a person's thread is open too": func(f *fixture, _ *mergingForge) {
			f.f.threads = append(f.f.threads, forge.Thread{ID: "9", Author: "pat", Body: "fix", Notes: 1,
				Entries: []forge.Note{{Author: "pat", Text: "fix"}}})
		},
		"no owner configured": func(f *fixture, _ *mergingForge) {
			cfg := config.Default()
			cfg.Defaults.Autonomy.Merge = config.AfterApproval
			f.run.SetConfig(cfg)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f, mf := kelpieFixture(t)
			f.f.threads = []forge.Thread{botThread("1", 11)}
			mutate(f, mf)
			f.w.Check(f.ctx)
			if n := raised(t, f); n != 0 || blocking(t, f) != "" {
				t.Fatalf("raised: %s / %q", f.feed(t), blocking(t, f))
			}
		})
	}
}
