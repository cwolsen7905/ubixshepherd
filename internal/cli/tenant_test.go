package cli

import "testing"

// --tenant is a global option, read before the command like --host.
func TestTenantIsAGlobalOption(t *testing.T) {
	for _, args := range [][]string{{"--tenant", "team-a", "status"}, {"--tenant=team-a", "status"}, {"--local", "--tenant", "team-a", "status"}} {
		g, rest, err := splitGlobals(args)
		if err != nil || g.tenant != "team-a" || len(rest) != 1 || rest[0] != "status" {
			t.Errorf("%q: %+v %q %v", args, g, rest, err)
		}
	}
	if _, _, err := splitGlobals([]string{"--tenant"}); err == nil {
		t.Error("--tenant with no value was taken")
	}
}
