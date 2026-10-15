package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestArtifactMethodsUseTheDesignedRoutes(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		switch r.URL.Path {
		case "/v1/artifacts/7/content":
			w.Write([]byte("# plan\n"))
		case "/v1/artifacts":
			w.Write([]byte(`[{"id":7}]`))
		default:
			w.Write([]byte(`{"id":7}`))
		}
	}))
	defer srv.Close()
	c := &Client{base: srv.URL, http: srv.Client()}
	ctx := context.Background()
	if l, err := c.Artifacts(ctx, store.ArtifactFilter{LaneID: 3, State: "live"}); err != nil || len(l) != 1 {
		t.Fatalf("list = %v, %v", l, err)
	}
	if b, err := c.ArtifactContent(ctx, 7, "abc"); err != nil || string(b) != "# plan\n" {
		t.Fatalf("content = %q, %v", b, err)
	}
	keep := "keep"
	c.PatchArtifact(ctx, 7, api.ArtifactPatch{Intent: &keep})
	c.DiscardArtifact(ctx, 7)
	c.DeclareArtifact(ctx, 9, api.ArtifactDeclare{Path: "a.md"})
	c.ArtifactGC(ctx, api.ArtifactGC{DryRun: true})
	want := []string{
		"GET /v1/artifacts?lane_id=3&state=live",
		"GET /v1/artifacts/7/content?version=abc",
		"PATCH /v1/artifacts/7",
		"POST /v1/artifacts/7/discard",
		"POST /v1/runs/9/artifacts",
		"POST /v1/artifacts/gc",
	}
	if len(got) != len(want) {
		t.Fatalf("requests = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d = %s, want %s", i, got[i], want[i])
		}
	}
}
