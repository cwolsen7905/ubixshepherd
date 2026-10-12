package cli

import (
	"os"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/git"
)

func TestMain(m *testing.M) {
	git.ClearEnvConfig()
	// A test never asks GitHub for a release.
	os.Setenv("SHEPHERD_NO_UPDATE_CHECK", "1")
	os.Exit(m.Run())
}
