package daemon

import (
	"context"
	"net/http"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Dismissing marks a failed run and nothing else; undismissing clears it; a run still
// going is refused; and the lane's next run is not dismissed.
func TestDismissRun(t *testing.T) {
	s, ts := newServer(t)
	a, _, la, _ := twoRuns(t, s)
	ctx := context.Background()

	// A run that is going is not on the board's attention group to dismiss.
	if code := call(t, ts, s.Token, "POST", api.PathRunDismiss(a.ID), nil, nil); code != http.StatusConflict {
		t.Errorf("dismiss a running run: %d, want 409", code)
	}
	if code := call(t, ts, s.Token, "POST", api.PathRunDismiss(9999), nil, nil); code != http.StatusNotFound {
		t.Errorf("dismiss no run: %d, want 404", code)
	}

	a.State = store.RunFailed
	if err := s.Store.UpdateRun(ctx, a); err != nil {
		t.Fatal(err)
	}
	var v api.RunView
	if code := call(t, ts, s.Token, "POST", api.PathRunDismiss(a.ID), nil, &v); code != http.StatusOK || v.Dismissed == nil {
		t.Fatalf("dismiss: %d, dismissed %v", code, v.Dismissed)
	}
	first := *v.Dismissed
	// A repeat keeps the first time.
	call(t, ts, s.Token, "POST", api.PathRunDismiss(a.ID), nil, &v)
	if v.Dismissed == nil || !v.Dismissed.Equal(first) {
		t.Errorf("repeat dismiss moved the time: %v, want %v", v.Dismissed, first)
	}
	// The run itself is untouched, and a later run of the lane is not dismissed.
	if v.State != store.RunFailed || v.Prompt != a.Prompt || v.Log != a.Log {
		t.Errorf("dismiss changed the run: %+v", v.Run)
	}
	next, err := s.Store.CreateRun(ctx, store.Run{LaneID: la.ID, Agent: "claude", Prompt: "again", State: store.RunRunning, Log: a.Log})
	if err != nil {
		t.Fatal(err)
	}
	var list []api.RunView
	call(t, ts, s.Token, "GET", api.PathRuns, nil, &list)
	got := map[int64]bool{}
	for _, r := range list {
		got[r.ID] = r.Dismissed != nil
	}
	if !got[a.ID] || got[next.ID] {
		t.Errorf("dismissed by run id = %v, want %d only", got, a.ID)
	}

	// Ending a run later does not bring a dismissal back or lose it.
	a.Error = "still failed"
	if err := s.Store.UpdateRun(ctx, a); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Store.Run(ctx, a.ID); r.Dismissed == nil {
		t.Error("UpdateRun cleared the dismissal")
	}

	var undone api.RunView
	if code := call(t, ts, s.Token, "POST", api.PathRunUndismiss(a.ID), nil, &undone); code != http.StatusOK || undone.Dismissed != nil {
		t.Errorf("undismiss: %d, dismissed %v", code, undone.Dismissed)
	}
	// Undismissing one that is not dismissed is fine.
	if code := call(t, ts, s.Token, "POST", api.PathRunUndismiss(a.ID), nil, &undone); code != http.StatusOK {
		t.Errorf("undismiss twice: %d", code)
	}
}
