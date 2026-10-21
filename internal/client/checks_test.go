package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestChecksCalls(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.RequestURI()
		gotBody = nil
		if r.Method == http.MethodPost {
			json.NewDecoder(r.Body).Decode(&gotBody)
		}
		switch r.URL.Path {
		case api.PathChecks:
			json.NewEncoder(w).Encode(api.Checks{RepoID: 3, Repo: "app", Files: []store.CheckFile{{Path: "t/a_test.go"}}})
		case api.PathLaneScope(5):
			json.NewEncoder(w).Encode(api.Rescoped{Lane: store.Lane{ID: 5, Name: "x"}, Excluded: []string{"t/a_test.go"}})
		default:
			json.NewEncoder(w).Encode(api.CheckChange{Files: []store.CheckFile{{Path: "t/a_test.go"}}})
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")
	ctx := context.Background()

	if got, err := c.Checks(ctx, 3, true); err != nil || got.Repo != "app" || gotMethod != "GET" || gotPath != "/v1/checks?all=1&repo_id=3" {
		t.Errorf("Checks: %+v %v %s %s", got, err, gotMethod, gotPath)
	}
	if _, err := c.ChecksStatus(ctx, 3); err != nil || gotPath != "/v1/checks?repo_id=3&status=1" {
		t.Errorf("ChecksStatus: %v %s", err, gotPath)
	}
	if got, err := c.ApproveChecks(ctx, api.ApproveChecks{RepoID: 3, Paths: []string{"t/a_test.go"}, ItemID: 7, Decision: 9}); err != nil ||
		len(got.Files) != 1 || gotPath != "/v1/checks/approve" || gotBody["item_id"] != float64(7) || gotBody["decision"] != float64(9) {
		t.Errorf("ApproveChecks: %+v %v %s %v", got, err, gotPath, gotBody)
	}
	if _, err := c.ReleaseChecks(ctx, api.ReleaseChecks{RepoID: 3, Paths: []string{"t/a_test.go"}, Why: "wrong", Force: true}); err != nil ||
		gotPath != "/v1/checks/release" || gotBody["why"] != "wrong" || gotBody["force"] != true {
		t.Errorf("ReleaseChecks: %v %s %v", err, gotPath, gotBody)
	}
	if got, err := c.RescopeLaneExcluding(ctx, 5, []string{"a"}, nil); err != nil || got.Name != "x" || len(got.Excluded) != 1 {
		t.Errorf("RescopeLaneExcluding: %+v %v", got, err)
	}
}
