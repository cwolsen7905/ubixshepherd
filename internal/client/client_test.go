package client

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

func TestRedialPicksUpNewAddr(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.json")
	writeRT := func(addr string) {
		t.Helper()
		b, err := json.Marshal(api.Runtime{Addr: addr, Token: "tok-" + addr, PID: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeRT("127.0.0.1:7400")
	c, err := FromRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr() != "http://127.0.0.1:7400" {
		t.Fatalf("addr = %s", c.Addr())
	}
	writeRT("127.0.0.1:7501")
	if err := c.Redial(); err != nil {
		t.Fatal(err)
	}
	if c.Addr() != "http://127.0.0.1:7501" {
		t.Fatalf("after redial addr = %s", c.Addr())
	}
	if c.token != "tok-127.0.0.1:7501" {
		t.Fatalf("token not updated: %s", c.token)
	}
}
