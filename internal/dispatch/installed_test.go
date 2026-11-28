package dispatch

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
)

// InstalledAgents asks each adapter for its own executable, so a new adapter is listed
// without a list to keep.
func TestInstalledAgentsUsesTheAdaptersBinaries(t *testing.T) {
	r := &Runner{OpenCode: &OpenCodeHost{Settings: func() config.OpenCode { return config.OpenCode{} }}}
	var asked []string
	r.lookPath = func(b string) (string, error) {
		asked = append(asked, filepath.Base(b))
		if b == adapters["claude"].Bin || b == adapters["cursor"].Bin {
			return "/fake/" + b, nil
		}
		return "", errors.New("not found")
	}
	if got := r.InstalledAgents(); !slices.Equal(got, []string{"claude", "cursor"}) {
		t.Fatalf("installed %v", got)
	}
	for _, name := range AgentNames() {
		if !slices.Contains(asked, filepath.Base(adapters[name].Bin)) {
			t.Errorf("%s's executable %s was not looked for", name, adapters[name].Bin)
		}
	}
}
