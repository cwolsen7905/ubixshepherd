package api

import (
	"encoding/json"
	"testing"
)

func TestWaitingSealVersion(t *testing.T) {
	a := Waiting{Items: []WaitingItem{{Kind: WaitingRequest, Action: "route", RequestID: 1, Title: "x"}}}
	a.Seal()
	b := Waiting{Items: []WaitingItem{{Kind: WaitingRequest, Action: "route", RequestID: 1, Title: "x"}}}
	b.Seal()
	if a.Version == "" || a.Version != b.Version || a.Count != 1 {
		t.Fatalf("same list, different version: %q %q", a.Version, b.Version)
	}
	b.Items[0].Title = "y"
	b.Seal()
	if a.Version == b.Version {
		t.Error("version did not move when an item changed")
	}
	raw, _ := json.Marshal(a)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	for _, k := range []string{"count", "counts", "items", "version"} {
		if _, ok := m[k]; !ok {
			t.Errorf("response lacks %q", k)
		}
	}
}
