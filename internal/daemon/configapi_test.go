package daemon

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

const apiConfig = `# hand-written
daemon:
  max_runs: 3 # three at a time
defaults:
  gate: make check
repos:
  api:
    base_branch: dev
`

// configDaemon is a daemon on a config file the test can read back.
func configDaemon(t *testing.T) (*Server, string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(apiConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	s, base, _ := webDaemon(t, func(s *Server) { s.ConfigPath = path })
	return s, base, path
}

func signInWith(t *testing.T, s *Server, b *browser, allow bool) {
	t.Helper()
	var link api.WebSignin
	req, _ := http.NewRequest("POST", b.base+api.PathWebSignin, strings.NewReader(`{"allow_settings":`+map[bool]string{true: "true", false: "false"}[allow]+`}`))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	json.NewDecoder(resp.Body).Decode(&link)
	resp.Body.Close()
	origin, site := b.origin, b.site
	b.origin, b.site = "", "none"
	r, _ := b.do("GET", link.Path, nil)
	if r.StatusCode != http.StatusSeeOther {
		t.Fatalf("exchange: %d", r.StatusCode)
	}
	var ws api.WebSession
	_, body := b.do("GET", api.PathWebSession, nil)
	b.origin, b.site = origin, site
	if json.Unmarshal([]byte(body), &ws) != nil || ws.AllowSettings != allow {
		t.Fatalf("session %s, want allow_settings %v", body, allow)
	}
	b.csrf = ws.CSRF
}

func getDoc(t *testing.T, b *browser) api.ConfigDoc {
	t.Helper()
	resp, body := b.do("GET", api.PathConfig, nil)
	var doc api.ConfigDoc
	if resp.StatusCode != http.StatusOK || json.Unmarshal([]byte(body), &doc) != nil {
		t.Fatalf("GET config: %d %s", resp.StatusCode, body)
	}
	return doc
}

func TestConfigReadShowsRawAndInherited(t *testing.T) {
	s, base, _ := configDaemon(t)
	b := newBrowser(t, base)
	signInWith(t, s, b, false)
	doc := getDoc(t, b)
	if doc.ETag == "" || doc.CanWrite || doc.Why == "" {
		t.Errorf("a plain session reads only: %+v", doc)
	}
	api := doc.Repos["api"]
	if api.Raw["base_branch"] != "dev" || len(api.Overridden) != 1 || api.Overridden[0] != "base_branch" {
		t.Errorf("raw/overridden: %+v", api)
	}
	if api.Effective["gate"] != "make check" || api.Effective["base_branch"] != "dev" {
		t.Errorf("effective should inherit the gate: %+v", api.Effective)
	}
	if doc.Defaults.Overridden[0] != "gate" {
		t.Errorf("defaults overridden: %v", doc.Defaults.Overridden)
	}
	var keys []string
	for _, sec := range doc.Sections {
		for _, f := range sec.Fields {
			keys = append(keys, sec.ID+":"+strings.Join(f.Key, "."))
		}
	}
	for _, want := range []string{"repo:autonomy.push", "repo:standards", "repo:agent.model", "daemon:max_runs", "project:budget.amount"} {
		found := false
		for _, k := range keys {
			found = found || k == want
		}
		if !found {
			t.Errorf("field %s missing from the description", want)
		}
	}
}

func TestConfigWriteNeedsAnAllowedSession(t *testing.T) {
	s, base, path := configDaemon(t)
	b := newBrowser(t, base)
	signInWith(t, s, b, false)
	doc := getDoc(t, b)
	ch := api.ConfigChange{ETag: doc.ETag, Ops: []api.ConfigOp{{Path: []string{"daemon", "max_runs"}, Value: 5}}}
	// A preview is allowed and writes nothing.
	resp, body := b.do("POST", api.PathConfigPreview, ch)
	var pv api.ConfigPreview
	if resp.StatusCode != http.StatusOK || json.Unmarshal([]byte(body), &pv) != nil || !strings.Contains(pv.Diff, "+  max_runs: 5") {
		t.Fatalf("preview: %d %s", resp.StatusCode, body)
	}
	if resp, body = b.do("PUT", api.PathConfig, ch); resp.StatusCode != http.StatusForbidden || !strings.Contains(body, "--allow-settings") {
		t.Errorf("a session not signed in for settings saved: %d %s", resp.StatusCode, body)
	}
	if got, _ := os.ReadFile(path); string(got) != apiConfig {
		t.Fatal("the file changed")
	}
	// The same person, signed in to allow it.
	b2 := newBrowser(t, base)
	signInWith(t, s, b2, true)
	resp, body = b2.do("PUT", api.PathConfig, ch)
	var saved api.ConfigSaved
	if resp.StatusCode != http.StatusOK || json.Unmarshal([]byte(body), &saved) != nil || !saved.Reloaded {
		t.Fatalf("save: %d %s", resp.StatusCode, body)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "max_runs: 5 # three at a time") || !strings.Contains(string(got), "# hand-written") {
		t.Errorf("file:\n%s", got)
	}
	if s.LiveConfig().Daemon.MaxRuns != 5 {
		t.Errorf("the daemon did not reload: %d", s.LiveConfig().Daemon.MaxRuns)
	}
	if baks, _ := filepath.Glob(path + ".bak.*"); len(baks) != 1 {
		t.Errorf("backups: %v", baks)
	}
}

func TestConfigSaveRefusals(t *testing.T) {
	s, base, path := configDaemon(t)
	b := newBrowser(t, base)
	signInWith(t, s, b, true)
	doc := getDoc(t, b)
	put := func(ch api.ConfigChange) (int, api.ConfigRefusal) {
		resp, body := b.do("PUT", api.PathConfig, ch)
		var r api.ConfigRefusal
		json.Unmarshal([]byte(body), &r)
		return resp.StatusCode, r
	}
	// Invalid: 422 with the parser's reasons.
	code, r := put(api.ConfigChange{ETag: doc.ETag, Ops: []api.ConfigOp{{Path: []string{"defaults", "autonomy", "push"}, Value: "robot"}}})
	if code != http.StatusUnprocessableEntity || len(r.Problems) == 0 {
		t.Errorf("invalid: %d %+v", code, r)
	}
	// Raising autonomy: 428 until its path is confirmed.
	raise := api.ConfigChange{ETag: doc.ETag, Ops: []api.ConfigOp{{Path: []string{"repos", "api", "autonomy", "push"}, Value: "agent"}}}
	code, r = put(raise)
	if code != http.StatusPreconditionRequired || len(r.Risky) != 1 || r.Risky[0].Path != "repos.api.autonomy.push" {
		t.Errorf("unconfirmed: %d %+v", code, r)
	}
	raise.Confirm = []string{"defaults.autonomy.push"}
	if code, _ = put(raise); code != http.StatusPreconditionRequired {
		t.Errorf("confirming another path let it through: %d", code)
	}
	// A stale etag: the file was edited by hand.
	if err := os.WriteFile(path, []byte(apiConfig+"# edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	raise.Confirm = []string{"repos.api.autonomy.push"}
	if code, _ = put(raise); code != http.StatusConflict {
		t.Errorf("stale etag: %d", code)
	}
	if got, _ := os.ReadFile(path); !strings.HasSuffix(string(got), "# edited\n") {
		t.Error("the hand edit was clobbered")
	}
	// Confirmed on a fresh read, it saves.
	raise.ETag = getDoc(t, b).ETag
	if code, r = put(raise); code != http.StatusOK {
		t.Errorf("confirmed save: %d %+v", code, r)
	}
	if s.LiveConfig().Profile("api").Autonomy.Push != "agent" {
		t.Error("not in force")
	}
}

// The desk, a worker and a caller with no credentials never reach the config editor; the
// operator token may, as the CLI does.
func TestConfigAuth(t *testing.T) {
	s, base, _ := configDaemon(t)
	call := func(tok, method, path string) int {
		req, _ := http.NewRequest(method, base+path, strings.NewReader(`{}`))
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	deskTok, _ := s.tokens.mint(principal{Role: RoleDesk, Human: true})
	workerTok, _ := s.tokens.mint(principal{Role: RoleWorker, Run: 1})
	for name, tok := range map[string]string{"desk": deskTok, "worker": workerTok} {
		for _, rt := range [][2]string{{"GET", api.PathConfig}, {"POST", api.PathConfigPreview}, {"PUT", api.PathConfig}} {
			if c := call(tok, rt[0], rt[1]); c != http.StatusForbidden {
				t.Errorf("%s %s %s: %d, want 403", name, rt[0], rt[1], c)
			}
		}
	}
	if c := call("", "GET", api.PathConfig); c != http.StatusUnauthorized {
		t.Errorf("no credentials: %d", c)
	}
	if c := call(s.Token, "GET", api.PathConfig); c != http.StatusOK {
		t.Errorf("operator read: %d", c)
	}
	if c := call(s.Token, "PUT", api.PathConfig); c != http.StatusConflict && c != http.StatusBadRequest {
		// An empty change from the operator is judged on its merits, not refused for who sent it.
		t.Errorf("operator write: %d", c)
	}
}
