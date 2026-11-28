package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// A client with a tenant names it on every request; one without names none.
func TestClientSendsTheTenant(t *testing.T) {
	var got []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get(api.TenantHeader))
		json.NewEncoder(w).Encode(api.Status{})
	}))
	defer ts.Close()
	c := New(ts.URL, "tok")
	if _, err := c.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.Tenant = "team-a"
	if _, err := c.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "" || got[1] != "team-a" {
		t.Errorf("tenant headers %q, want none then team-a", got)
	}
}
