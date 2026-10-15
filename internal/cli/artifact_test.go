package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func workerTool(t *testing.T, name string) mcpTool {
	t.Helper()
	for _, tl := range workerTools() {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("no %s tool", name)
	return mcpTool{}
}

// The artifact tool maps onto `worker artifact`, and ask_human's artifacts onto
// repeated --artifact flags.
func TestArtifactToolMapsOntoWorkerCommand(t *testing.T) {
	args, err := workerTool(t, "artifact").args(map[string]any{"path": "docs/plan.md", "title": "Plan", "intent": "keep"})
	if want := []string{"worker", "artifact", "docs/plan.md", "--title", "Plan", "--intent", "keep"}; err != nil || !slices.Equal(args, want) {
		t.Errorf("artifact args = %q, %v", args, err)
	}
	args, _ = workerTool(t, "artifact").args(map[string]any{"path": "a.md"})
	if want := []string{"worker", "artifact", "a.md"}; !slices.Equal(args, want) {
		t.Errorf("bare artifact args = %q", args)
	}
	args, err = workerTool(t, "ask_human").args(map[string]any{"question": "q", "recommendation": "r", "artifacts": []any{"a.md", "b.md"}})
	if err != nil || !slices.Contains(args, "--artifact") || strings.Count(strings.Join(args, " "), "--artifact") != 2 {
		t.Errorf("ask_human args = %q, %v", args, err)
	}
	if _, err := workerTool(t, "ask_human").args(map[string]any{"question": "q", "artifacts": "a.md"}); err == nil {
		t.Error("a string where a list belongs was accepted")
	}
}

// `shepherd worker artifact` posts the declaration to the run's own route with the
// run's token, and `worker ask-human --artifact` carries the paths with the question.
func TestWorkerArtifactCommandPostsDeclaration(t *testing.T) {
	var gotPath, gotAuth string
	var declared api.ArtifactDeclare
	var asked store.Decision
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		switch {
		case strings.HasSuffix(r.URL.Path, "/artifacts"):
			json.NewDecoder(r.Body).Decode(&declared)
			json.NewEncoder(w).Encode(api.ArtifactDeclared{New: true, Artifact: store.Artifact{ID: 4, Path: declared.Path, Kind: "markdown", Intent: declared.Intent}})
		case strings.HasSuffix(r.URL.Path, "/decisions"):
			json.NewDecoder(r.Body).Decode(&asked)
			json.NewEncoder(w).Encode(store.Decision{ID: 9})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	t.Setenv(dispatch.EnvToken, "run-token")
	t.Setenv(dispatch.EnvURL, ts.URL)
	t.Setenv(dispatch.EnvRun, "17")

	var out bytes.Buffer
	env := Env{Stdout: &out, Stderr: &out}
	if err := runWorker(context.Background(), env, []string{"artifact", "docs/plan.md", "--title", "Plan", "--intent", "commit"}); err != nil {
		t.Fatal(err)
	}
	if gotPath != api.PathRunArtifacts(17) || !strings.Contains(gotAuth, "run-token") {
		t.Errorf("request: %s %q", gotPath, gotAuth)
	}
	if declared != (api.ArtifactDeclare{Path: "docs/plan.md", Title: "Plan", Intent: "commit"}) {
		t.Errorf("declared = %+v", declared)
	}
	if !strings.Contains(out.String(), "docs/plan.md") || !strings.Contains(out.String(), "commit") {
		t.Errorf("output = %q", out.String())
	}

	out.Reset()
	if err := runWorker(context.Background(), env, []string{"ask-human", "ok?", "--recommendation", "yes", "--artifact", "a.md", "--artifact", "b.md"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(asked.ArtifactPaths, []string{"a.md", "b.md"}) {
		t.Errorf("asked = %+v", asked)
	}
}
