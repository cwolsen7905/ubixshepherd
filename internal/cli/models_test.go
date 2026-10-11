package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/paths"
)

func TestModelsCommandShowsThePolicy(t *testing.T) {
	l := paths.Layout{Home: t.TempDir()}
	cfg := `
models:
  claude:
    default: sonnet
    catalogue:
      sonnet: {use: "everyday work", efforts: [low, high]}
      opus: {use: "design"}
repos:
  app:
    agent:
      models: {deny: {claude: [opus]}}
`
	if err := os.WriteFile(l.Config(), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	env := Env{Stdout: &out, Stderr: &out, Layout: l, Cwd: l.Home}
	if err := runModels(context.Background(), env, []string{"--repo", "app", "--agent", "claude"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"default model   sonnet (models.claude.default)", "sonnet: everyday work [effort: low, high]",
		`not permitted   opus: denied by repos.app.agent.models.deny.claude ("opus")`} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "opus: design") {
		t.Errorf("a denied model is listed as permitted:\n%s", got)
	}
}
