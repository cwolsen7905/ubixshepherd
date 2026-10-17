package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// maxActionWords bounds the person's words in an action's text: the desk is told what
// was done, not handed a transcript.
const maxActionWords = 200

// actionDescriber says what a browser session did, from the request and the response of
// a call that succeeded: the workspace it belongs to and a short line, "" for nothing
// worth telling.
type actionDescriber func(ctx context.Context, r *http.Request, body, resp []byte) (workspace int64, text string)

// recorder keeps the status and body a handler wrote.
type recorder struct {
	http.ResponseWriter
	status int
	buf    bytes.Buffer
}

func (w *recorder) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *recorder) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.buf.Len() < 1<<20 {
		w.buf.Write(p)
	}
	return w.ResponseWriter.Write(p)
}

// acted wraps a handler so that, when a browser session calls it and it succeeds, the
// front desk records an action event: in the thread and the history for every client,
// held for the desk's next turn, and starting no turn. It is done here, in the daemon,
// so a client cannot forget it or forge it. Other callers (the CLI, the desk's own
// tools, an agent) are not recorded: the desk made or knows of those itself.
func (s *Server) acted(describe actionDescriber, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).Role != RoleWeb {
			h(w, r)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		rec := &recorder{ResponseWriter: w}
		h(rec, r)
		if rec.status < 200 || rec.status > 299 {
			return
		}
		ctx := context.WithoutCancel(r.Context())
		ws, text := describe(ctx, r, body, rec.buf.Bytes())
		if text == "" || ws == 0 {
			return
		}
		if err := s.recordAction(ctx, ws, text); err != nil {
			s.Log.Warn("record a web action for the desk", "text", text, "err", err)
		}
	}
}

// recordAction appends the action to the workspace's desk.
func (s *Server) recordAction(ctx context.Context, workspace int64, text string) error {
	wss, err := s.Store.Workspaces(ctx)
	if err != nil {
		return err
	}
	for _, ws := range wss {
		if ws.ID == workspace {
			d, err := s.desks.For(ws)
			if err != nil {
				return err
			}
			return d.Acted(ctx, text)
		}
	}
	return fmt.Errorf("no workspace %d", workspace)
}

// words is the person's text for an action line: redacted, on one line, trimmed.
func words(s string) string {
	s = strings.Join(strings.Fields(redact.String(s)), " ")
	if r := []rune(s); len(r) > maxActionWords {
		s = string(r[:maxActionWords]) + "..."
	}
	return strconv.Quote(s)
}

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

// workspaceOfLane is the workspace a lane's repo is in.
func (s *Server) workspaceOfLane(ctx context.Context, laneID int64) (int64, store.Lane) {
	lane, err := s.Store.Lane(ctx, laneID)
	if err != nil {
		return 0, lane
	}
	repo, err := s.Store.Repo(ctx, lane.RepoID)
	if err != nil {
		return 0, lane
	}
	return repo.WorkspaceID, lane
}

// workspaceOfRun is the workspace a run's lane is in.
func (s *Server) workspaceOfRun(ctx context.Context, runID int64) (int64, store.Lane) {
	run, err := s.Store.Run(ctx, runID)
	if err != nil {
		return 0, store.Lane{}
	}
	return s.workspaceOfLane(ctx, run.LaneID)
}

func (s *Server) describeAnswer(ctx context.Context, r *http.Request, body, _ []byte) (int64, string) {
	var req api.Answer
	json.Unmarshal(body, &req)
	d, err := s.Store.Decision(ctx, pathID(r))
	if err != nil {
		return 0, ""
	}
	ws, lane := s.workspaceOfRun(ctx, d.RunID)
	return ws, fmt.Sprintf("answered decision %d (lane %s) with %s", d.ID, lane.Name, words(req.Answer))
}

func (s *Server) describeStop(ctx context.Context, r *http.Request, _, _ []byte) (int64, string) {
	ws, lane := s.workspaceOfRun(ctx, pathID(r))
	return ws, fmt.Sprintf("stopped run %d (lane %s)", pathID(r), lane.Name)
}

func (s *Server) describeDismiss(dismiss bool) actionDescriber {
	return func(ctx context.Context, r *http.Request, _, _ []byte) (int64, string) {
		ws, lane := s.workspaceOfRun(ctx, pathID(r))
		if dismiss {
			return ws, fmt.Sprintf("dismissed run %d (lane %s) from the board", pathID(r), lane.Name)
		}
		return ws, fmt.Sprintf("put run %d (lane %s) back on the board", pathID(r), lane.Name)
	}
}

func (s *Server) describeFix(ctx context.Context, r *http.Request, body, _ []byte) (int64, string) {
	var req api.RunFix
	json.Unmarshal(body, &req)
	ws, lane := s.workspaceOfRun(ctx, pathID(r))
	return ws, fmt.Sprintf("took the suggested fix %s for run %d (lane %s)", words(req.Action), pathID(r), lane.Name)
}

func (s *Server) describeStart(ctx context.Context, _ *http.Request, body, resp []byte) (int64, string) {
	var req dispatch.StartRequest
	var run api.RunView
	if json.Unmarshal(body, &req) != nil || json.Unmarshal(resp, &run) != nil {
		return 0, ""
	}
	if run.Held != "" {
		return 0, ""
	}
	ws, _ := s.workspaceOfLane(ctx, run.LaneID)
	what := fmt.Sprintf("started run %d in lane %s", run.ID, run.Lane)
	if req.Continue > 0 {
		what = fmt.Sprintf("continued run %d in lane %s as run %d", req.Continue, run.Lane, run.ID)
	}
	if strings.TrimSpace(req.Prompt) != "" {
		what += " with " + words(req.Prompt)
	}
	return ws, what
}

func (s *Server) describeCloseLane(ctx context.Context, r *http.Request, body, _ []byte) (int64, string) {
	var req api.CloseLane
	json.Unmarshal(body, &req)
	ws, lane := s.workspaceOfLane(ctx, pathID(r))
	text := "closed lane " + lane.Name
	if req.Force {
		text += ", forced"
	}
	return ws, text
}

func (s *Server) describeShipLane(ctx context.Context, r *http.Request, _, _ []byte) (int64, string) {
	ws, lane := s.workspaceOfLane(ctx, pathID(r))
	return ws, "shipped lane " + lane.Name
}

// workspaceOfRequest is the workspace of the run that asked.
func (s *Server) workspaceOfRequest(ctx context.Context, id int64) (int64, store.Request) {
	q, err := s.Store.Request(ctx, id)
	if err != nil {
		return 0, q
	}
	ws, _ := s.workspaceOfRun(ctx, q.FromRun)
	return ws, q
}

func (s *Server) describeRoute(ctx context.Context, r *http.Request, body, _ []byte) (int64, string) {
	var req api.Route
	json.Unmarshal(body, &req)
	ws, q := s.workspaceOfRequest(ctx, pathID(r))
	text := fmt.Sprintf("routed request %d to lane %s", pathID(r), req.Lane)
	if req.Agent != "" {
		text += " (agent " + req.Agent + ")"
	}
	if q.Kind != "" {
		text += ", a " + q.Kind
	}
	return ws, text
}

func (s *Server) describeCloseRequest(ctx context.Context, r *http.Request, body, _ []byte) (int64, string) {
	var req api.CloseRequest
	json.Unmarshal(body, &req)
	ws, _ := s.workspaceOfRequest(ctx, pathID(r))
	text := fmt.Sprintf("closed request %d", pathID(r))
	if strings.TrimSpace(req.Why) != "" {
		text += ": " + words(req.Why)
	}
	return ws, text
}
