package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestSemverOrder(t *testing.T) {
	// Each version sorts strictly below the next.
	order := []string{"v0.1.0-beta.1", "v0.1.0-beta.2", "v0.1.0-beta.10", "v0.1.0-rc.1", "v0.1.0", "v0.1.1-beta.1", "v0.2.0", "v1.0.0"}
	for i := range order {
		for j := range order {
			a, _ := parseSemver(order[i])
			b, ok := parseSemver(order[j])
			if !ok {
				t.Fatalf("%s did not parse", order[j])
			}
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := a.compare(b); got != want {
				t.Errorf("%s vs %s = %d, want %d", order[i], order[j], got, want)
			}
		}
	}
	for _, bad := range []string{"", "dev", "v1.2", "1.2.3.4", "v01.2.3", "v1.2.3-", "(devel)", "v1.2.x"} {
		if _, ok := parseSemver(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
	if a, _ := parseSemver("1.2.3+build.5"); a.prerelease() {
		t.Error("build metadata made a pre-release")
	}
}

type fakeGitHub struct {
	body   string
	status int
	hits   atomic.Int32
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.hits.Add(1)
	if f.status != 0 {
		w.WriteHeader(f.status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(f.body))
}

const releases = `[
 {"tag_name":"v0.4.0-beta.2","html_url":"https://example.test/r/v0.4.0-beta.2","body":"## beta 2\n- thing","prerelease":true,"published_at":"2026-10-10T00:00:00Z",
  "assets":[{"name":"shepherd_linux_amd64.tar.gz","browser_download_url":"https://example.test/a.tgz","size":12}]},
 {"tag_name":"v0.4.0-beta.10","html_url":"https://example.test/r/v0.4.0-beta.10","prerelease":true},
 {"tag_name":"v0.3.0","html_url":"https://example.test/r/v0.3.0","body":"stable"},
 {"tag_name":"v9.9.9","draft":true},
 {"tag_name":"nightly","html_url":"x"}
]`

func newChecker(t *testing.T, f *fakeGitHub, current, channel string) (*Checker, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &Checker{
		URL: srv.URL, Current: current, StatePath: filepath.Join(t.TempDir(), "update.json"),
		Settings: func() (bool, string) { return true, channel },
	}, srv
}

func TestCheckPicksNewestOnChannel(t *testing.T) {
	c, _ := newChecker(t, &fakeGitHub{body: releases}, "v0.3.0", ChannelBeta)
	if err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := c.Status()
	if st.Latest == nil || st.Latest.Version != "v0.4.0-beta.10" || !st.Available || st.Dismissed {
		t.Fatalf("beta: %+v", st)
	}
	if len(st.Latest.Assets) != 0 || st.CheckedAt.IsZero() {
		t.Errorf("assets %v checked %v", st.Latest.Assets, st.CheckedAt)
	}

	// The same cached list answers the other channel without another call.
	c.Settings = func() (bool, string) { return true, ChannelStable }
	st = c.Status()
	if st.Latest == nil || st.Latest.Version != "v0.3.0" || st.Available {
		t.Fatalf("stable: %+v", st)
	}
}

func TestReleaseCarriesNotesAndAssets(t *testing.T) {
	c, _ := newChecker(t, &fakeGitHub{body: releases}, "v0.3.0", ChannelBeta)
	if err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, r := range c.releases {
		if r.Version == "v0.4.0-beta.2" {
			if r.Notes != "## beta 2\n- thing" || len(r.Assets) != 1 || r.Assets[0].Size != 12 || r.Published.IsZero() || !r.Prerelease {
				t.Errorf("%+v", r)
			}
			return
		}
	}
	t.Fatal("beta.2 not kept")
}

func TestUnknownCurrentNeverClaimsNewer(t *testing.T) {
	for _, cur := range []string{"dev", "", "(devel)"} {
		c, _ := newChecker(t, &fakeGitHub{body: releases}, cur, ChannelBeta)
		c.Check(context.Background())
		st := c.Status()
		if st.CurrentKnown || st.Available || st.Latest == nil || st.Latest.Version != "v0.4.0-beta.10" {
			t.Errorf("current %q: %+v", cur, st)
		}
	}
}

func TestCurrentAheadOrEqualIsNotAvailable(t *testing.T) {
	for _, cur := range []string{"v0.4.0-beta.10", "v0.4.0", "v1.0.0"} {
		c, _ := newChecker(t, &fakeGitHub{body: releases}, cur, ChannelBeta)
		c.Check(context.Background())
		if st := c.Status(); st.Available {
			t.Errorf("current %s: available %+v", cur, st.Latest)
		}
	}
}

func TestFailedCheckKeepsLastGoodAnswer(t *testing.T) {
	f := &fakeGitHub{body: releases}
	c, _ := newChecker(t, f, "v0.3.0", ChannelBeta)
	if err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []*fakeGitHub{{status: 500}, {status: 403}, {status: 429}, {body: `{not json`}} {
		f.status, f.body = bad.status, bad.body
		if err := c.Check(context.Background()); err == nil {
			t.Errorf("%+v: no error", bad)
		}
		st := c.Status()
		if st.Latest == nil || st.Latest.Version != "v0.4.0-beta.10" || st.LastError == "" {
			t.Errorf("%+v: %+v", bad, st)
		}
	}
	f.status, f.body = 0, releases
	c.Check(context.Background())
	if st := c.Status(); st.LastError != "" {
		t.Errorf("a good check left %q", st.LastError)
	}
}

func TestDismissSurvivesRestartUntilNewer(t *testing.T) {
	f := &fakeGitHub{body: releases}
	c, srv := newChecker(t, f, "v0.3.0", ChannelBeta)
	c.Check(context.Background())
	if err := c.Dismiss("v0.4.0-beta.10"); err != nil {
		t.Fatal(err)
	}
	if !c.Status().Dismissed {
		t.Fatal("not dismissed")
	}

	again := &Checker{URL: srv.URL, Current: "v0.3.0", StatePath: c.StatePath, Settings: c.Settings}
	again.Check(context.Background())
	if !again.Status().Dismissed {
		t.Error("the dismissal was lost on restart")
	}

	f.body = `[{"tag_name":"v0.4.0-beta.11","html_url":"u","prerelease":true},` + releases[1:]
	again.Check(context.Background())
	if st := again.Status(); st.Dismissed || st.Latest.Version != "v0.4.0-beta.11" {
		t.Errorf("a newer release stayed hidden: %+v", st)
	}
	if err := again.Dismiss("dev"); err == nil {
		t.Error("dismissed a non-version")
	}
}

func TestRunHonoursTheSwitchAndTheInterval(t *testing.T) {
	f := &fakeGitHub{body: releases}
	c, _ := newChecker(t, f, "v0.3.0", ChannelBeta)
	var on atomic.Bool
	c.Settings = func() (bool, string) { return on.Load(), ChannelBeta }
	c.FirstDelay, c.Tick, c.Interval = time.Millisecond, 5*time.Millisecond, time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	time.Sleep(60 * time.Millisecond)
	if f.hits.Load() != 0 {
		t.Fatal("a disabled check called out")
	}
	on.Store(true)
	deadline := time.Now().Add(3 * time.Second)
	for f.hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done
	if n := f.hits.Load(); n != 1 {
		t.Errorf("%d calls; want one, the interval being an hour", n)
	}
	if c.Status().Latest == nil {
		t.Error("no answer cached")
	}
}
