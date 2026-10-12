package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/update"
)

// fakeReleases serves a releases list like GitHub's.
func fakeReleases(t *testing.T, body *string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(*body))
	}))
	t.Cleanup(ts.Close)
	return ts
}

func updateChecker(t *testing.T, srv *httptest.Server, current string) *update.Checker {
	t.Helper()
	c := &update.Checker{
		URL: srv.URL, Current: current, StatePath: filepath.Join(t.TempDir(), "update.json"),
		Settings: func() (bool, string) { return true, "beta" },
	}
	if err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c
}

const twoReleases = `[
 {"tag_name":"v0.5.0-beta.2","html_url":"https://example.test/r/2","body":"notes two","prerelease":true},
 {"tag_name":"v0.5.0-beta.1","html_url":"https://example.test/r/1","prerelease":true}]`

func TestUpdateRoutesAndTokens(t *testing.T) {
	body := twoReleases
	s, ts := newServer(t)
	s.Updates = updateChecker(t, fakeReleases(t, &body), "v0.5.0-beta.1")

	var st api.UpdateStatus
	if code := call(t, ts, s.Token, "GET", api.PathUpdate, nil, &st); code != 200 {
		t.Fatalf("operator GET: %d", code)
	}
	if st.Latest != "v0.5.0-beta.2" || st.URL != "https://example.test/r/2" || st.Notes != "notes two" ||
		!st.Available || !st.Show || st.Dismissed || !st.CurrentKnown || st.Channel != "beta" {
		t.Fatalf("status: %+v", st)
	}

	// A version that is not the one offered is refused, and nothing is hidden.
	if code := call(t, ts, s.Token, "POST", api.PathUpdateDismiss, api.UpdateDismiss{Version: "v0.5.0-beta.1"}, nil); code != http.StatusConflict {
		t.Errorf("dismiss of a stale version: %d", code)
	}
	if code := call(t, ts, s.Token, "POST", api.PathUpdateDismiss, api.UpdateDismiss{Version: "v0.5.0-beta.2"}, &st); code != 200 || st.Show || !st.Dismissed {
		t.Errorf("dismiss: %d %+v", code, st)
	}

	// The agent's token and the front desk's get nothing.
	worker, _ := s.MintWorker(7)
	deskTok, _ := s.MintDesk(true, "sess")
	for name, tok := range map[string]string{"worker": worker, "desk": deskTok} {
		for _, r := range [][2]string{{"GET", api.PathUpdate}, {"POST", api.PathUpdateDismiss}} {
			if code := call(t, ts, tok, r[0], r[1], api.UpdateDismiss{Version: "v0.5.0-beta.2"}, nil); code != http.StatusForbidden {
				t.Errorf("%s %s %s: %d, want 403", name, r[0], r[1], code)
			}
		}
	}
	if code := call(t, ts, "", "GET", api.PathUpdate, nil, nil); code == 200 {
		t.Error("an unauthenticated read was answered")
	}
}

func TestUpdateForBrowserSessions(t *testing.T) {
	body := twoReleases
	var fake *httptest.Server
	s, base, _ := webDaemon(t, func(s *Server) {
		fake = fakeReleases(t, &body)
		s.Updates = updateChecker(t, fake, "dev")
	})
	b := newBrowser(t, base)
	signInWith(t, s, b, false)
	resp, out := b.do("GET", api.PathUpdate, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("session GET: %d %s", resp.StatusCode, out)
	}
	// A dev build is told of the latest release without a comparison.
	st := s.updateStatus()
	if st.CurrentKnown || st.Available || !st.Show || st.Latest != "v0.5.0-beta.2" {
		t.Errorf("dev build: %+v", st)
	}
	resp, out = b.do("POST", api.PathUpdateDismiss, api.UpdateDismiss{Version: "v0.5.0-beta.2"})
	if resp.StatusCode != 200 || s.updateStatus().Show {
		t.Errorf("session dismiss: %d %s", resp.StatusCode, out)
	}
}

func TestUpdateOffShowsNothing(t *testing.T) {
	body := twoReleases
	s, _ := newServer(t)
	c := updateChecker(t, fakeReleases(t, &body), "v0.1.0")
	c.Settings = func() (bool, string) { return false, "beta" }
	s.Updates = c
	if st := s.updateStatus(); st.Show || st.Enabled {
		t.Errorf("a disabled check shows a notice: %+v", st)
	}
}
