package crook

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type memRecorder struct {
	got []Entry
	err error
}

func (m *memRecorder) Record(_ context.Context, e Entry) error {
	m.got = append(m.got, e)
	return m.err
}

func TestCheckRecordsEveryVerdictButRouteProceeds(t *testing.T) {
	rec := &memRecorder{}
	SetRecorder(rec, nil)
	defer SetRecorder(nil, nil)
	ctx := context.Background()

	v := Check(ctx, Request{Action: ActionPush, Actor: ActorWorker, Facts: Facts{Push: AutonomyShepherd, Run: 7, Agent: "claude", Lane: "feat-x", Repo: "r"}})
	if v.Kind != Refuse || v.Stage != "policy" {
		t.Fatalf("verdict: %+v", v)
	}
	Check(ctx, Request{Action: RouteAction("GET /v1/lanes"), Actor: ActorPerson, Facts: Facts{Route: &RouteFacts{}}})
	Check(ctx, Request{Action: RouteAction("POST /v1/x"), Actor: ActorWorker, Facts: Facts{Route: &RouteFacts{Denied: DeniedWorker}}})
	Check(ctx, Request{Action: ActionShip, Actor: ActorDaemon, Facts: Facts{Push: AutonomyShepherd}})

	if len(rec.got) != 3 {
		t.Fatalf("want the push, the denied route and the ship; got %d: %+v", len(rec.got), rec.got)
	}
	e := rec.got[0]
	if e.Action != ActionPush || e.Actor != ActorWorker || e.Kind != Refuse || e.Rule != RulePushAutonomy ||
		e.RuleVersion != RuleVersion || e.Stage != "policy" || e.Run != 7 || e.Lane != "feat-x" || e.Repo != "r" ||
		!strings.Contains(e.Reason, "never push") || !strings.Contains(e.Facts, `"push":"shepherd"`) || e.At.IsZero() {
		t.Fatalf("entry: %+v", e)
	}
	if rec.got[1].Kind != Refuse || rec.got[1].Stage != "floor" || !strings.Contains(rec.got[1].Facts, `"route_denied":"worker"`) {
		t.Fatalf("denied route: %+v", rec.got[1])
	}
	if rec.got[2].Kind != Proceed || rec.got[2].Rule != RulePushAutonomy {
		t.Fatalf("ship: %+v", rec.got[2])
	}
}

func TestCheckSurvivesARecorderError(t *testing.T) {
	rec := &memRecorder{err: errors.New("disk full")}
	var failed error
	SetRecorder(rec, func(err error) { failed = err })
	defer SetRecorder(nil, nil)
	v := Check(context.Background(), Request{Action: ActionShip, Actor: ActorDaemon, Facts: Facts{Push: AutonomyShepherd}})
	if !v.OK() || failed == nil {
		t.Fatalf("the verdict stands and the error is reported: %+v, %v", v, failed)
	}
}

func TestCheckWithNoRecorder(t *testing.T) {
	SetRecorder(nil, nil)
	if v := Check(context.Background(), Request{Action: ActionShip, Actor: ActorDaemon, Facts: Facts{Push: AutonomyShepherd}}); !v.OK() {
		t.Fatalf("%+v", v)
	}
}

func TestDecideNamesTheStageAndFactsAreRedacted(t *testing.T) {
	if v := Decide(context.Background(), Request{Action: ActionRunStart, Actor: ActorPerson}); v.Stage != "default" || v.Rule != RuleDefault {
		t.Fatalf("default: %+v", v)
	}
	if v := Decide(context.Background(), Request{Action: ActionDecisionAnswer, Actor: ActorWorker}); v.Stage != "floor" {
		t.Fatalf("floor: %+v", v)
	}
	if got := Stages(); got[0] != "floor" || got[len(got)-1] != "default" {
		t.Fatalf("stages: %v", got)
	}
	f := factsJSON(Facts{Remote: "https://u:hunter2hunter2@example.com/r.git"})
	if strings.Contains(f, "hunter2") {
		t.Fatalf("facts keep a secret: %s", f)
	}
}

func TestDecideAloneLogsNothingAndRecordLogsWhatItIsGiven(t *testing.T) {
	rec := &memRecorder{}
	SetRecorder(rec, nil)
	defer SetRecorder(nil, nil)
	req := Request{Action: ActionMergeArm, Actor: ActorWorker, Facts: Facts{Merge: AutonomyShepherd}}
	v := Decide(context.Background(), req)
	if len(rec.got) != 0 {
		t.Fatalf("Decide logged: %+v", rec.got)
	}
	Record(context.Background(), req, v)
	if len(rec.got) != 1 || rec.got[0].Kind != Refuse || rec.got[0].Rule != RuleMergeAutonomy {
		t.Fatalf("Record: %+v", rec.got)
	}
}

func TestRepeatedNonProceedsAreLoggedOncePerWindow(t *testing.T) {
	rec := &memRecorder{}
	SetRecorder(rec, nil)
	defer SetRecorder(nil, nil)
	deny := Request{Action: RouteAction("POST /v1/x"), Actor: ActorWorker, Facts: Facts{Route: &RouteFacts{Denied: DeniedWorker}}}
	for i := 0; i < 50; i++ {
		Check(context.Background(), deny)
	}
	other := deny
	other.Actor = ActorDesk
	Check(context.Background(), other)
	ship := Request{Action: ActionShip, Actor: ActorDaemon, Facts: Facts{Push: AutonomyShepherd}}
	Check(context.Background(), ship)
	Check(context.Background(), ship)
	if len(rec.got) != 4 {
		t.Fatalf("want one refusal per actor and every proceed; got %d: %+v", len(rec.got), rec.got)
	}
}
