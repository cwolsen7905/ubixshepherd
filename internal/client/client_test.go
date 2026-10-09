package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestFeedFillsEventsForOlderDaemon(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[{"id":1,"kind":"pipeline","text":"x","created":"2026-10-08T00:00:00Z"},{"id":2,"kind":"session","text":"y","created":"2026-10-08T00:00:00Z"}],"last":2}`))
	}))
	defer srv.Close()
	c := &Client{base: srv.URL, http: srv.Client()}
	f, err := c.Feed(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Events) != 2 || f.Events[0] != api.EventPipeline || f.Events[1] != api.EventInfo {
		t.Errorf("events = %v", f.Events)
	}
}
