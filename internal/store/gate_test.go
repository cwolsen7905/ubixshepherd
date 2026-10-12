package store

import (
	"testing"
	"time"
)

func TestBadgeFor(t *testing.T) {
	at := time.Now()
	pass := &GateResult{SHA: "h", Gate: "make check", Passed: true, At: at, Source: GateSourceShip}
	fail := &GateResult{SHA: "h", Gate: "make check", Passed: false, At: at}
	old := &GateResult{SHA: "o", Gate: "make check", Passed: true, At: at}
	for _, c := range []struct {
		name         string
		head         string
		onHead, last *GateResult
		want         string
	}{
		{"none", "h", nil, nil, GateNotRun},
		{"pass", "h", pass, pass, GatePassed},
		{"fail", "h", fail, fail, GateFailed},
		{"earlier", "h", nil, old, GateStale},
		{"unknown head", "", pass, pass, GateStale},
	} {
		if got := BadgeFor(c.head, c.onHead, c.last); got.State != c.want {
			t.Errorf("%s: %s, want %s", c.name, got.State, c.want)
		}
	}
	if b := BadgeFor("h", nil, nil); b.At != nil || b.SHA != "" {
		t.Errorf("not run carries a record: %+v", b)
	}
}
