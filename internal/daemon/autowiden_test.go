package daemon

import (
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
)

func TestWireAutoWiden(t *testing.T) {
	for name, tc := range map[string]struct {
		yaml string
		want bool
	}{
		"off by default":        {"", false},
		"human is off":          {"repos:\n  app:\n    widen: human\n", false},
		"on for one repo":       {"repos:\n  app:\n    widen: auto\n", true},
		"on in the defaults":    {"defaults:\n  widen: auto\n", true},
		"limits alone are off":  {"defaults:\n  widen_cap: 2\n", false},
		"protected list is off": {"defaults:\n  widen_protected: [\"x/**\"]\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := config.Parse([]byte(tc.yaml))
			if err != nil {
				t.Fatal(err)
			}
			s, _ := newServer(t)
			s.Config = cfg
			s.Runner = &dispatch.Runner{Store: s.Store, Config: cfg}
			s.wireAutoWiden()
			if got := s.Runner.Rescope != nil; got != tc.want {
				t.Errorf("wired = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWireAutoWidenNeedsARunner(t *testing.T) {
	s, _ := newServer(t)
	s.wireAutoWiden() // no runner: nothing to wire, and no panic
}
