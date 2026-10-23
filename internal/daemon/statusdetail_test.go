package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

const detailPath = api.PathStatus + "?" + api.StatusDetailQuery

func TestStatusDetail(t *testing.T) {
	s, ts := newServer(t)
	budget := 12.5
	s.Config.Daemon.Budget = &budget

	// Without the query the probe every command makes stays as small as it was.
	var plain api.Status
	if code := call(t, ts, s.Token, "GET", api.PathStatus, nil, &plain); code != http.StatusOK || plain.Detail != nil {
		t.Fatalf("plain status: %d, detail %+v", code, plain.Detail)
	}

	var st api.Status
	if code := call(t, ts, s.Token, "GET", detailPath, nil, &st); code != http.StatusOK || st.Detail == nil {
		t.Fatalf("detail status: %d, %+v", code, st)
	}
	d := st.Detail
	if d.OS == "" || d.Arch == "" || d.Now.IsZero() || d.BudgetUSD != budget || d.SpendUSD != 0 {
		t.Errorf("detail = %+v", d)
	}
	if !d.ConfigLoaded.Equal(s.started) || d.ReloadError != "" {
		t.Errorf("config loaded %v (started %v), reload error %q", d.ConfigLoaded, s.started, d.ReloadError)
	}
	// Nothing reports these here: absent, not "none".
	if d.QuotaHolds != nil || d.Forge != nil {
		t.Errorf("unreported fields set: %+v %+v", d.QuotaHolds, d.Forge)
	}

	// What the runner and the watcher report is passed on, an error on one line.
	until := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	poll := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	s.QuotaHolds = func() []api.QuotaHold { return []api.QuotaHold{{Agent: "claude", Until: until}} }
	s.ForgeWatch = func() api.ForgeWatch {
		return api.ForgeWatch{LastPoll: poll, Error: "glab: could not reach\ngitlab.example\ttoken=glpat-abcdefghij0123456789"}
	}
	st = api.Status{}
	call(t, ts, s.Token, "GET", detailPath, nil, &st)
	d = st.Detail
	if d.QuotaHolds == nil || len(*d.QuotaHolds) != 1 || (*d.QuotaHolds)[0].Agent != "claude" || !(*d.QuotaHolds)[0].Until.Equal(until) {
		t.Errorf("quota holds = %+v", d.QuotaHolds)
	}
	if d.Forge == nil || !d.Forge.LastPoll.Equal(poll) || strings.ContainsAny(d.Forge.Error, "\n\t") || strings.Contains(d.Forge.Error, "glpat-abcdefghij0123456789") {
		t.Errorf("forge = %+v", d.Forge)
	}
}

func TestStatusDetailReload(t *testing.T) {
	s, path := reloadServer(t)
	ctx := context.Background()
	if err := os.WriteFile(path, []byte("daemon:\n  max_runs: 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.ReloadConfig(ctx); err != nil {
		t.Fatal(err)
	}
	d, err := s.statusDetail(ctx)
	if err != nil || d.ReloadError != "" || !d.ConfigLoaded.After(s.started.Add(-time.Second)) {
		t.Fatalf("after a good reload: %+v, %v", d, err)
	}
	loaded := d.ConfigLoaded

	// A refused reload says why on one line, and the time the config was loaded stays.
	if err := os.WriteFile(path, []byte("daemon:\n  max_runs: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.ReloadConfig(ctx); err == nil {
		t.Fatal("a broken config was accepted")
	}
	d, _ = s.statusDetail(ctx)
	if d.ReloadError == "" || strings.Contains(d.ReloadError, "\n") || d.ReloadFailedAt.IsZero() || !d.ConfigLoaded.Equal(loaded) {
		t.Errorf("after a refused reload: %+v", d)
	}

	// The next good one clears it.
	if err := os.WriteFile(path, []byte("daemon:\n  max_runs: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.ReloadConfig(ctx); err != nil {
		t.Fatal(err)
	}
	if d, _ = s.statusDetail(ctx); d.ReloadError != "" || !d.ReloadFailedAt.IsZero() {
		t.Errorf("after the fix: %+v", d)
	}
}

// A browser session reads the detail like the other read endpoints; an agent's token
// gets the plain status; and no token, environment or path of the daemon's own is in it.
func TestStatusDetailAccess(t *testing.T) {
	s, base, _ := webDaemon(t)
	b := newBrowser(t, base)
	signIn(t, s, b)
	resp, body := b.do("GET", detailPath, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("web session: %d %s", resp.StatusCode, body)
	}
	var st api.Status
	if err := json.Unmarshal([]byte(body), &st); err != nil || st.Detail == nil {
		t.Fatalf("web session got no detail: %s", body)
	}
	if strings.Contains(body, s.Token) {
		t.Error("the operator token is in the status")
	}
	var raw map[string]map[string]any
	json.Unmarshal([]byte(body), &raw)
	for k := range raw["detail"] {
		switch k {
		case "commit", "built_at", "os", "arch", "host", "now", "config_loaded", "reload_error", "reload_failed_at",
			"running", "open_lanes", "decisions", "quota_holds", "spend_usd", "budget_usd", "forge":
		default:
			t.Errorf("unexpected detail field %q", k)
		}
	}

	// Signed out, nothing.
	out := newBrowser(t, base)
	if resp, _ := out.do("GET", detailPath, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no session: %d", resp.StatusCode)
	}

	worker, err := s.MintWorker(7)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", base+detailPath, nil)
	req.Header.Set("Authorization", "Bearer "+worker)
	wresp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer wresp.Body.Close()
	var ws api.Status
	if err := json.NewDecoder(wresp.Body).Decode(&ws); wresp.StatusCode != http.StatusOK || err != nil || ws.Detail != nil {
		t.Errorf("worker: %d, %v, detail %+v", wresp.StatusCode, err, ws.Detail)
	}
}

func TestStatusDetailForgeWatcher(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	root := t.TempDir()
	if _, err := s.Store.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: root}, []store.Repo{
		{Name: "a", Path: filepath.Join(root, "a"), Remote: "git@gl.example.com:g/a.git"},
		{Name: "b", Path: filepath.Join(root, "b"), Remote: "git@gl.example.com:g/b.git"},
		{Name: "c", Path: filepath.Join(root, "c")},
	}); err != nil {
		t.Fatal(err)
	}
	s.Config.Daemon.Poll = "90s"
	s.ForgeWatch = func() api.ForgeWatch { return api.ForgeWatch{} }
	var st api.Status
	call(t, ts, s.Token, "GET", detailPath, nil, &st)
	f := st.Detail.Forge
	if f == nil || f.Off || f.PollSeconds != 90 || !slices.Equal(f.Forges, []string{"gitlab"}) {
		t.Errorf("forge = %+v", f)
	}

	// With polling off there is no watcher to report on, and it says so.
	s.Config.Daemon.Poll = "off"
	s.ForgeWatch = nil
	st = api.Status{}
	call(t, ts, s.Token, "GET", detailPath, nil, &st)
	if f := st.Detail.Forge; f == nil || !f.Off || f.PollSeconds != 0 || len(f.Forges) != 0 {
		t.Errorf("forge with polling off = %+v", f)
	}
}
