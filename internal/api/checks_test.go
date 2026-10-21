package api

import (
	"encoding/json"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// A client that knows nothing of Excluded reads a rescope answer as a lane, and a lane
// view without exclusions does not carry the field.
func TestRescopedReadsAsALane(t *testing.T) {
	b, err := json.Marshal(Rescoped{Lane: store.Lane{ID: 4, Name: "build", Scope: []string{"src/**"}}, Excluded: []string{"src/a_test.go"}})
	if err != nil {
		t.Fatal(err)
	}
	var lane store.Lane
	if err := json.Unmarshal(b, &lane); err != nil || lane.ID != 4 || lane.Name != "build" {
		t.Fatalf("as a lane: %+v %v", lane, err)
	}
	var back Rescoped
	if err := json.Unmarshal(b, &back); err != nil || len(back.Excluded) != 1 {
		t.Fatalf("round trip: %+v %v", back, err)
	}
}

func TestLaneViewExcluded(t *testing.T) {
	plain, _ := json.Marshal(LaneView{Lane: store.Lane{ID: 1}})
	var m map[string]any
	json.Unmarshal(plain, &m)
	if _, ok := m["excluded"]; ok {
		t.Errorf("an empty exclusion is serialised: %s", plain)
	}
	with, _ := json.Marshal(LaneView{Lane: store.Lane{ID: 1}, Excluded: []string{"a_test.go"}})
	json.Unmarshal(with, &m)
	if got, _ := m["excluded"].([]any); len(got) != 1 {
		t.Errorf("excluded = %s", with)
	}
}
