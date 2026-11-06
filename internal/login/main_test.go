package login

import (
	"os"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/auth"
)

func TestMain(m *testing.M) {
	auth.UseCheapHashingForTests()
	os.Exit(m.Run())
}
