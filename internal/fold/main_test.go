package fold

import (
	"os"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/git"
)

func TestMain(m *testing.M) {
	git.ClearEnvConfig()
	os.Exit(m.Run())
}
