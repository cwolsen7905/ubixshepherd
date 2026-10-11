package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// deskHistory reads the desk's stored conversation as the web page does.
func deskHistory(t *testing.T, s *Server, base string) []store.DeskEvent {
	t.Helper()
	var h api.DeskHistory
	if code := operatorCall(t, s, base, "GET", api.PathDeskHistory, &h); code != http.StatusOK {
		t.Fatalf("desk history: %d", code)
	}
	return h.Events
}

func actionTexts(es []store.DeskEvent) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Kind+": "+e.Text)
	}
	return out
}

func tokenPost(t *testing.T, s *Server, base, path string, body any) int {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", base+path, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// What a browser session does through the API lands in the desk's conversation as action
// events, redacted, with no turn started; the same calls by the CLI, and calls that
// fail, leave nothing.
func TestWebSessionActionsReachTheDesk(t *testing.T) {
	s, base, _ := webDaemon(t, func(s *Server) {
		s.Runner = &dispatch.Runner{Store: s.Store, Config: config.Default(), Dir: filepath.Join(t.TempDir(), "runs"), Log: s.Log}
	})
	ctx := context.Background()
	a, b, _, _ := twoRuns(t, s)
	a.State = store.RunFailed
	if err := s.Store.UpdateRun(ctx, a); err != nil {
		t.Fatal(err)
	}
	dec, err := s.Store.CreateDecision(ctx, store.Decision{RunID: b.ID, Question: "which?", State: store.DecisionOpen})
	if err != nil {
		t.Fatal(err)
	}
	dec2, _ := s.Store.CreateDecision(ctx, store.Decision{RunID: b.ID, Question: "and?", State: store.DecisionOpen})
	req, err := s.Store.CreateRequest(ctx, store.Request{FromRun: b.ID, Kind: store.RequestQuestion, Message: "m", State: store.RequestNeedsRouting})
	if err != nil {
		t.Fatal(err)
	}
	req2, _ := s.Store.CreateRequest(ctx, store.Request{FromRun: b.ID, Kind: store.RequestQuestion, Message: "m", State: store.RequestNeedsRouting})

	br := newBrowser(t, base)
	signIn(t, s, br)
	post := func(path string, body any) int {
		t.Helper()
		resp, out := br.do("POST", path, body)
		if resp.StatusCode >= 300 {
			t.Logf("%s: %d %s", path, resp.StatusCode, out)
		}
		return resp.StatusCode
	}

	if code := post(api.PathDecisions+"/"+itoa(dec.ID)+"/answer", api.Answer{Answer: "  Option B  "}); code != http.StatusOK {
		t.Fatalf("answer: %d", code)
	}
	if code := post(api.PathRunDismiss(a.ID), nil); code != http.StatusOK {
		t.Fatalf("dismiss: %d", code)
	}
	if code := post(api.PathRequests+"/"+itoa(req.ID)+"/close", api.CloseRequest{Why: "stale, see ghp_0123456789abcdefghijABCDEFGHIJ012345"}); code != http.StatusOK {
		t.Fatalf("close request: %d", code)
	}
	// Refused or failed calls say nothing.
	post(api.PathDecisions+"/"+itoa(dec.ID)+"/answer", api.Answer{Answer: "again"}) // answered already
	post(api.PathDecisions+"/9999/answer", api.Answer{Answer: "x"})
	post(api.PathRuns+"/"+itoa(b.ID)+"/stop", nil) // no process: refused

	got := actionTexts(deskHistory(t, s, base))
	if len(got) != 3 {
		t.Fatalf("history = %q, want three actions", got)
	}
	for i, want := range []string{
		`action: answered decision ` + itoa(dec.ID) + ` (lane b) with "Option B"`,
		`action: dismissed run ` + itoa(a.ID) + ` (lane a) from the board`,
		`action: closed request ` + itoa(req.ID) + `: "stale, see `,
	} {
		if !strings.HasPrefix(got[i], want) {
			t.Errorf("event %d = %q, want %q", i, got[i], want)
		}
	}
	if strings.Contains(strings.Join(got, "\n"), "ghp_0123") {
		t.Errorf("a secret reached the history: %q", got)
	}
	for _, e := range deskHistory(t, s, base) {
		if e.Origin != store.DeskByHuman || e.Turn != 0 {
			t.Errorf("action %+v: want origin human and no turn", e)
		}
	}

	// The CLI's token, or the desk's, records nothing: they are not the page.
	if code := tokenPost(t, s, base, api.PathDecisions+"/"+itoa(dec2.ID)+"/answer", api.Answer{Answer: "yes"}); code != http.StatusOK {
		t.Fatalf("operator answer: %d", code)
	}
	if code := tokenPost(t, s, base, api.PathRequests+"/"+itoa(req2.ID)+"/close", api.CloseRequest{}); code != http.StatusOK {
		t.Fatalf("operator close: %d", code)
	}
	if code := tokenPost(t, s, base, api.PathRunUndismiss(a.ID), nil); code != http.StatusOK {
		t.Fatalf("operator undismiss: %d", code)
	}
	if n := len(deskHistory(t, s, base)); n != 3 {
		t.Errorf("history has %d events after operator calls, want still 3", n)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// Each handler's line, for the calls that need a process or git to succeed for real:
// the wrapper is given a handler that answers as the real one does.
func TestActionLines(t *testing.T) {
	s, _ := newServer(t)
	ctx := context.Background()
	a, _, la, _ := twoRuns(t, s)
	q, err := s.Store.CreateRequest(ctx, store.Request{FromRun: a.ID, Kind: store.RequestHandoff, Message: "m", State: store.RequestNeedsRouting})
	if err != nil {
		t.Fatal(err)
	}
	runView, _ := json.Marshal(api.RunView{Run: store.Run{ID: 77, LaneID: la.ID}, Lane: la.Name})
	cases := []struct {
		name     string
		describe actionDescriber
		path     string
		body     any
		resp     []byte
		pattern  string
		want     string
	}{
		{"stop", s.describeStop, "/v1/runs/{id}/stop", nil, nil, "id=" + itoa(a.ID), "stopped run " + itoa(a.ID) + " (lane a)"},
		{"start", s.describeStart, "/v1/runs", dispatch.StartRequest{LaneID: la.ID, Prompt: "Where does this\nstand?"}, runView, "", `started run 77 in lane a with "Where does this stand?"`},
		{"continue", s.describeStart, "/v1/runs", dispatch.StartRequest{Continue: a.ID, Prompt: "Carry on."}, runView, "", "continued run " + itoa(a.ID) + " in lane a as run 77 with \"Carry on.\""},
		{"close", s.describeCloseLane, "/v1/lanes/{id}/close", api.CloseLane{Force: true}, nil, "id=" + itoa(la.ID), "closed lane a, forced"},
		{"route", s.describeRoute, "/v1/requests/{id}/route", api.Route{Lane: "b", Agent: "claude"}, nil, "id=" + itoa(q.ID), "routed request " + itoa(q.ID) + " to lane b (agent claude), a handoff"},
		{"fix", s.describeFix, "/v1/runs/{id}/fix", api.RunFix{Action: "wait"}, nil, "id=" + itoa(a.ID), `took the suggested fix "wait" for run ` + itoa(a.ID) + " (lane a)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body, _ := json.Marshal(c.body)
			if c.body == nil {
				body = nil
			}
			r := httptest.NewRequest("POST", "/x", bytes.NewReader(body))
			if c.pattern != "" {
				r.SetPathValue("id", strings.TrimPrefix(c.pattern, "id="))
			}
			ws, text := c.describe(ctx, r, body, c.resp)
			if ws == 0 || text != c.want {
				t.Errorf("workspace %d, text %q, want %q", ws, text, c.want)
			}
		})
	}
}

// The wrapper records for a web session on success only, and the handler's own answer is
// untouched, body and status.
func TestActedWrapper(t *testing.T) {
	s, _ := newServer(t)
	a, _, _, _ := twoRuns(t, s)
	h := func(code int) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var in api.Answer
			json.NewDecoder(r.Body).Decode(&in) // the wrapper left the body readable
			w.WriteHeader(code)
			w.Write([]byte(in.Answer))
		}
	}
	for _, c := range []struct {
		name string
		role string
		code int
		want int
	}{
		{"web ok", RoleWeb, 200, 1},
		{"web accepted", RoleWeb, 202, 1},
		{"web refused", RoleWeb, 409, 0},
		{"operator", RoleOperator, 200, 0},
		{"desk", RoleDesk, 200, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := len(deskEventsOf(t, s))
			r := httptest.NewRequest("POST", "/x", strings.NewReader(`{"answer":"hi"}`))
			r.SetPathValue("id", itoa(a.ID))
			r = r.WithContext(context.WithValue(r.Context(), principalKey{}, principal{Role: c.role, Human: true}))
			w := httptest.NewRecorder()
			s.acted(s.describeStop, h(c.code))(w, r)
			if w.Code != c.code || w.Body.String() != "hi" {
				t.Errorf("response %d %q changed", w.Code, w.Body.String())
			}
			if n := len(deskEventsOf(t, s)) - before; n != c.want {
				t.Errorf("%d actions recorded, want %d", n, c.want)
			}
		})
	}
}

func deskEventsOf(t *testing.T, s *Server) []store.DeskEvent {
	t.Helper()
	wss, _ := s.Store.Workspaces(context.Background())
	var out []store.DeskEvent
	for _, ws := range wss {
		es, err := s.Store.DeskEvents(context.Background(), ws.ID, 0, 1000)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, es...)
	}
	return out
}
