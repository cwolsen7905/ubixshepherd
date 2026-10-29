package daemon

import (
	"slices"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
)

func TestMergeChoicesMatchWhatConfigAccepts(t *testing.T) {
	choices := profileMeta["autonomy.merge"].choices
	if !slices.Contains(choices, config.AfterApproval) {
		t.Fatalf("merge choices = %v, lack %s", choices, config.AfterApproval)
	}
	for _, c := range choices {
		if _, err := config.Parse([]byte("repos:\n  x:\n    autonomy:\n      merge: " + c + "\n")); err != nil {
			t.Errorf("choice %q is not accepted: %v", c, err)
		}
	}
}
