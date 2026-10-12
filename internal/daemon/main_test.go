package daemon

import (
	"os"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/git"
)

func TestMain(m *testing.M) {
	git.ClearEnvConfig()
	// A test never asks GitHub for a release.
	os.Setenv(NoUpdateCheckEnv, "1")
	os.Exit(m.Run())
}
