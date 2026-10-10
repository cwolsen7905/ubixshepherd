package configedit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
)

const sample = `# my config
daemon:
  max_runs: 4   # keep it low
  poll: 60s

defaults:
  # the gate
  gate: make check
  autonomy:
    push: human

repos:
  api:
    base_branch: dev   # integration branch
  web: {}
`

func write(t *testing.T, s string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestApplyKeepsCommentsAndOrder(t *testing.T) {
	d, err := Read(write(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	out, err := d.Apply([]Op{
		{Path: []string{"daemon", "max_runs"}, Value: float64(6)},
		{Path: []string{"repos", "api", "gate"}, Value: "go test ./..."},
		{Path: []string{"repos", "api", "agent", "model", "claude"}, Value: "sonnet"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"# my config", "# keep it low", "# the gate", "# integration branch", "max_runs: 6", "gate: go test ./...", "claude: sonnet", "web: {}"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Index(s, "daemon:") > strings.Index(s, "defaults:") || strings.Index(s, "defaults:") > strings.Index(s, "repos:") {
		t.Errorf("order changed:\n%s", s)
	}
	cfg, err := config.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile("api").Agent.Model["claude"] != "sonnet" || cfg.Profile("api").Gate != "go test ./..." {
		t.Errorf("not applied: %+v", cfg.Profile("api"))
	}
}

func TestUnsetPrunesEmptySections(t *testing.T) {
	d, _ := Read(write(t, sample))
	out, err := d.Apply([]Op{{Path: []string{"repos", "api", "base_branch"}, Unset: true}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Repos["api"]; !ok {
		t.Errorf("the repo's entry must stay:\n%s", out)
	}
	out, err = (&Doc{Raw: out}).Apply([]Op{
		{Path: []string{"repos", "api", "agent", "model", "claude"}, Value: "x"},
		{Path: []string{"repos", "api", "agent", "model", "claude"}, Unset: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "agent:") {
		t.Errorf("emptied sections should go:\n%s", out)
	}
}

func TestInvalidConfigRejected(t *testing.T) {
	p := write(t, sample)
	d, _ := Read(p)
	for name, op := range map[string]Op{
		"bad enum":    {Path: []string{"defaults", "autonomy", "push"}, Value: "robot"},
		"unknown key": {Path: []string{"defaults", "nonsense"}, Value: "x"},
		"bad type":    {Path: []string{"daemon", "max_runs"}, Value: "many"},
	} {
		_, err := d.Apply([]Op{op})
		var ve *ValidationError
		if !errors.As(err, &ve) || len(ve.Problems) == 0 {
			t.Errorf("%s: err = %v, want a ValidationError", name, err)
		}
	}
	if got, _ := os.ReadFile(p); string(got) != sample {
		t.Error("the file must be untouched")
	}
}

func TestSaveConflictAndBackup(t *testing.T) {
	p := write(t, sample)
	d, _ := Read(p)
	out, err := d.Apply([]Op{{Path: []string{"daemon", "poll"}, Value: "2m"}})
	if err != nil {
		t.Fatal(err)
	}
	// A hand edit lands after the read.
	if err := os.WriteFile(p, []byte(sample+"# edited by hand\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(d, out); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if got, _ := os.ReadFile(p); !strings.Contains(string(got), "edited by hand") {
		t.Fatal("the hand edit was clobbered")
	}
	d, _ = Read(p)
	out, _ = d.Apply([]Op{{Path: []string{"daemon", "poll"}, Value: "2m"}})
	etag, err := Save(d, out)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != string(out) || etag != ETagOf(got) {
		t.Error("saved bytes or etag wrong")
	}
	baks, _ := filepath.Glob(p + ".bak.*")
	if len(baks) != 1 {
		t.Fatalf("backups = %v", baks)
	}
	if b, _ := os.ReadFile(baks[0]); !strings.Contains(string(b), "edited by hand") {
		t.Error("the backup is not the previous version")
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", st.Mode())
	}
}

func TestMissingFileStartsFromTemplate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	d, err := Read(p)
	if err != nil || !d.Missing {
		t.Fatalf("Read: %+v, %v", d, err)
	}
	out, err := d.Apply([]Op{{Path: []string{"defaults", "gate"}, Value: "make check"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "## uBixShepherd configuration.") || !strings.Contains(string(out), "gate: make check") {
		t.Errorf("template comments or the new key missing:\n%s", out)
	}
	if _, err := Save(d, out); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(d, out); !errors.Is(err, ErrConflict) {
		t.Errorf("a second create over the first must conflict: %v", err)
	}
}

func TestBackupsPruned(t *testing.T) {
	p := write(t, sample)
	for i := 0; i < Keep+3; i++ {
		d, _ := Read(p)
		out, err := d.Apply([]Op{{Path: []string{"daemon", "max_runs"}, Value: float64(i%5 + 1)}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Save(d, out); err != nil {
			t.Fatal(err)
		}
	}
	if baks, _ := filepath.Glob(p + ".bak.*"); len(baks) != Keep {
		t.Errorf("backups = %d, want %d", len(baks), Keep)
	}
}

func TestRisks(t *testing.T) {
	prev, _ := config.Parse([]byte("defaults:\n  gate: make check\n  forbid: [a, b]\nrepos:\n  api: {}\n"))
	next, err := config.Parse([]byte(`defaults:
  gate: ""
  forbid: [a]
  autonomy: {push: agent}
repos:
  api:
    agent: {permission_mode: bypassPermissions}
    autonomy: {merge: agent}
`))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range Risks(prev, next) {
		got[r.Path] = true
	}
	for _, want := range []string{"defaults.gate", "defaults.forbid", "defaults.autonomy.push", "repos.api.agent.permission_mode", "repos.api.autonomy.merge"} {
		if !got[want] {
			t.Errorf("missing risk %s; got %v", want, got)
		}
	}
	if got["repos.api.autonomy.push"] {
		t.Error("an inherited raise is named once, under defaults")
	}
	if len(Risks(next, prev)) != 0 {
		t.Error("lowering power is not a risk")
	}
}

func TestDiff(t *testing.T) {
	if Diff("a\n", "a\n") != "" {
		t.Error("same text, no diff")
	}
	d := Diff("a\nb\nc\n", "a\nB\nc\n")
	if !strings.Contains(d, "-b\n") || !strings.Contains(d, "+B\n") || !strings.Contains(d, " a\n") {
		t.Errorf("diff = %q", d)
	}
}
