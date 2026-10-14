package forge

import (
	"context"
	"fmt"
	"os"
	"testing"
)

// SHEPHERD_REAL_MR="host project iid" reads a real merge request's threads, read-only.
func TestRealThreads(t *testing.T) {
	var host, project string
	var iid int
	if v := os.Getenv("SHEPHERD_REAL_MR"); v == "" {
		t.Skip("set SHEPHERD_REAL_MR")
	} else if _, err := fmt.Sscan(v, &host, &project, &iid); err != nil {
		t.Fatal(err)
	}
	g := &GitLab{Host: host, Project: project}
	ts, err := g.Threads(context.Background(), iid)
	if err != nil {
		t.Fatal(err)
	}
	for _, th := range ts {
		t.Logf("%s %s:%d %.60q", th.ID, th.File, th.Line, th.Body)
	}
	t.Logf("%d unresolved", len(ts))
}
