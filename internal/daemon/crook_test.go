package daemon

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/crook"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// These tests pin the verdicts Crook gives at the daemon's call sites: kind, rule id and
// the text that the old inline checks wrote.

func withPrincipal(p principal) context.Context {
	return context.WithValue(context.Background(), principalKey{}, p)
}

func TestCrookKeepGoingGate(t *testing.T) {
	s, _, _, ws, lane := keepSetup(t)
	event := withPrincipal(principal{Role: RoleDesk})

	v := s.gateVerdict(event, ws.ID, KeepContinue, lane.ID)
	if v.Kind != crook.Refuse || v.Rule != crook.RuleKeepGoingLimits || v.Reason != "keep-going is off: it has not been turned on in workspace "+itoa(ws.ID) {
		t.Fatalf("off: %+v", v)
	}
	for _, p := range []principal{{Role: RoleDesk, Human: true}, {Role: RoleOperator}, {Role: RoleWeb}} {
		if v := s.gateVerdict(withPrincipal(p), ws.ID, KeepContinue, lane.ID); !v.OK() || v.Rule != crook.RuleDefault {
			t.Errorf("%s human=%v: %+v", p.Role, p.Human, v)
		}
	}
	if v := s.gateVerdict(event, ws.ID, "bogus", lane.ID); v.Kind != crook.Refuse || v.Reason != `"bogus" is not an action keep-going covers` {
		t.Errorf("unknown action: %+v", v)
	}

	keepOn(t, s, ws, api.KeepGoingSet{})
	if v := s.gateVerdict(event, ws.ID, KeepContinue, lane.ID); v.Kind != crook.Proceed || v.Rule != crook.RuleKeepGoingLimits {
		t.Errorf("on: %+v", v)
	}
	keepOn(t, s, ws, api.KeepGoingSet{MaxRuns: 1})
	// With the limit at one run and none running, a start still passes; past the lane's
	// daily starts it is handed back.
	max := s.LiveConfig().KeepGoing.StartsPerLane()
	var last crook.Verdict
	for i := 0; i <= max; i++ {
		last = s.gateVerdict(event, ws.ID, KeepContinue, lane.ID)
	}
	if last.Kind != crook.Refuse || last.Rule != crook.RuleKeepGoingLimits {
		t.Errorf("past the lane's starts: %+v", last)
	}
}

func TestCrookPersonOnly(t *testing.T) {
	actions := []crook.Action{crook.ActionDecisionAnswer, crook.ActionNextChoose, crook.ActionWidenScope, crook.ActionKeepGoingOn, crook.ActionBriefApprove}
	for _, a := range actions {
		for _, p := range []principal{{Role: RoleDesk}, {Role: RoleWorker, Run: 1}} {
			v := personVerdict(withPrincipal(p), a)
			if v.Kind != crook.Refuse || v.Rule != crook.RulePersonAnswers || v.Reason != crook.PersonOnlyReason(a) || v.Reason == "" {
				t.Errorf("%s by %s: %+v", a, p.Role, v)
			}
		}
		for _, p := range []principal{{Role: RoleDesk, Human: true}, {Role: RoleOperator}, {Role: RoleWeb}} {
			if v := personVerdict(withPrincipal(p), a); !v.OK() {
				t.Errorf("%s by %s: %+v", a, p.Role, v)
			}
		}
	}
	// The text is what the handlers said before.
	for a, want := range map[crook.Action]string{
		crook.ActionDecisionAnswer: "the front desk may record an answer only in a turn the person started; bring the decision to them",
		crook.ActionWidenScope:     "the front desk may widen a lane's scope only in a turn the person started; bring the request to them",
		crook.ActionBriefApprove:   "the front desk may approve a brief only in a turn the person started; bring the draft to them",
		crook.ActionNextChoose:     errNotPersons.Error(),
	} {
		if got := crook.PersonOnlyReason(a); got != want {
			t.Errorf("%s: %q", a, got)
		}
	}
	if errNotPersons.Error() != "the front desk may approve, defer, drop, finish or reorder an item only in a turn the person started; propose it and bring it to them" {
		t.Errorf("errNotPersons = %q", errNotPersons)
	}
	r := httptest.NewRequest("POST", "/", nil).WithContext(withPrincipal(principal{Role: RoleDesk}))
	if personsNext(r) {
		t.Error("an event-turn desk chose an item's state")
	}
}

func TestCrookRouteRoles(t *testing.T) {
	cases := []struct {
		name   string
		p      principal
		a      access
		path   string
		want   crook.Kind
		reason string
	}{
		{"operator any", principal{Role: RoleOperator}, operatorOnly, "", crook.Proceed, ""},
		{"desk on operator route", principal{Role: RoleDesk}, operatorOnly, "", crook.Refuse, "the front desk's token cannot do this; it is the person's"},
		{"desk on desk route", principal{Role: RoleDesk}, forDesk, "", crook.Proceed, ""},
		{"web on operator route", principal{Role: RoleWeb}, operatorOnly, "", crook.Refuse, "a browser session cannot do this; use the shepherd command"},
		{"web on web route", principal{Role: RoleWeb}, forWeb, "", crook.Proceed, ""},
		{"worker on desk route", principal{Role: RoleWorker, Run: 3}, forDesk, "", crook.Refuse, "an agent's token cannot do this; ask the person (ask_human) or another lane (ask_shepherd)"},
		{"worker own run", principal{Role: RoleWorker, Run: 3}, ownRun, "3", crook.Proceed, ""},
		{"worker other run", principal{Role: RoleWorker, Run: 3}, ownRun, "4", crook.Refuse, "an agent's token is for run 3 only"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil).WithContext(withPrincipal(c.p))
		r.SetPathValue("id", c.path)
		v, known := routeVerdict("GET /v1/x", c.a, r)
		if !known || v.Kind != c.want || v.Reason != c.reason {
			t.Errorf("%s: %+v known=%v", c.name, v, known)
		}
		if c.want == crook.Refuse && v.Rule != crook.RuleRouteRoles {
			t.Errorf("%s: rule %q", c.name, v.Rule)
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	if _, known := routeVerdict("GET /v1/x", forDesk, r); known {
		t.Error("a request with no role was known")
	}
}

// Verdicts are queued and written in the background; stopping the log writes what is
// queued and puts back the recorder that was there.
func TestCrookLogWritesInTheBackgroundAndRestores(t *testing.T) {
	s, _ := newServer(t)
	stop := s.startCrookLog(context.Background())
	crook.Check(context.Background(), crook.Request{Action: crook.ActionShip, Actor: crook.ActorDaemon, Facts: crook.Facts{Push: crook.AutonomyAgent, Repo: "r"}})
	stop()
	vs, err := s.Store.CrookVerdicts(context.Background(), store.CrookFilter{})
	if err != nil || len(vs) != 1 || vs[0].Kind != "refuse" || vs[0].Repo != "r" {
		t.Fatalf("queued verdict should be written on stop: %+v, %v", vs, err)
	}
	crook.Check(context.Background(), crook.Request{Action: crook.ActionShip, Actor: crook.ActorDaemon, Facts: crook.Facts{Push: crook.AutonomyAgent, Repo: "other"}})
	if vs, _ := s.Store.CrookVerdicts(context.Background(), store.CrookFilter{}); len(vs) != 1 {
		t.Fatalf("nothing is logged once stopped: %d", len(vs))
	}
}

func TestShadowRecorderWritesTheRowAtOnce(t *testing.T) {
	s, _ := newServer(t)
	err := ShadowRecorder(s.Store).Record(context.Background(), crook.Entry{
		Action: "merge", Actor: crook.ActorDaemon, Kind: crook.Ask, Rule: "r1", Stage: "shadow", Reason: "x", Repo: "app", Lane: "l",
	})
	if err != nil {
		t.Fatal(err)
	}
	vs, err := s.Store.CrookVerdicts(context.Background(), store.CrookFilter{})
	if err != nil || len(vs) != 1 || vs[0].Stage != "shadow" || vs[0].Rule != "r1" || vs[0].Repo != "app" {
		t.Fatalf("rows = %+v, %v", vs, err)
	}
}
