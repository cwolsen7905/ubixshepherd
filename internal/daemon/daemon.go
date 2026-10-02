// Package daemon is Shepherd's long-running process. It owns the store and serves the
// HTTP API; nothing else touches the state.
package daemon

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/version"
)

// Server serves the API over a store.
type Server struct {
	Store      store.Store
	Config     config.Config
	ConfigPath string
	Token      string
	Log        *slog.Logger
	Fold       *fold.Fold
	started    time.Time
	stop       chan struct{}
	stopOnce   sync.Once
}

// NewServer returns a Server with a fresh random token.
func NewServer(st store.Store, cfg config.Config, cfgPath string, log *slog.Logger) (*Server, error) {
	tok, err := newToken()
	if err != nil {
		return nil, err
	}
	return &Server{
		Store: st, Config: cfg, ConfigPath: cfgPath, Token: tok, Log: log,
		Fold:    &fold.Fold{Store: st, Config: cfg},
		started: time.Now().UTC(), stop: make(chan struct{}),
	}, nil
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// NewLogger returns a logger whose output is redacted before it is written.
func NewLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(redact.Writer(os.Stderr), nil))
}

// Handler is the API, behind token authentication.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+api.PathStatus, s.status)
	mux.HandleFunc("GET "+api.PathWorkspaces, s.listWorkspaces)
	mux.HandleFunc("POST "+api.PathWorkspaces, s.saveWorkspace)
	mux.HandleFunc("GET "+api.PathResolve, s.resolve)
	mux.HandleFunc("POST "+api.PathShutdown, s.shutdown)
	mux.HandleFunc("GET "+api.PathLanes, s.listLanes)
	mux.HandleFunc("POST "+api.PathLanes, s.openLane)
	mux.HandleFunc("POST "+api.PathLanes+"/{id}/close", s.closeLane)
	mux.HandleFunc("GET "+api.PathFoldGC, s.foldGC)
	mux.HandleFunc("POST "+api.PathPrePush, s.prePush)
	mux.HandleFunc("POST /v1/repos/{id}/hook", s.repoHook)
	return s.logRequests(s.auth(mux))
}

// statusWriter remembers the status a handler wrote.
type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

// logRequests logs every request but the status probe each command makes first, so the
// daemon's log shows what each client asked and how it went.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(sw, r)
		if r.Method == http.MethodGet && r.URL.Path == api.PathStatus {
			return
		}
		client := r.Header.Get(api.ClientHeader)
		if client == "" {
			client = "unknown"
		}
		level := slog.LevelInfo
		if sw.code >= 400 {
			level = slog.LevelWarn
		}
		s.Log.Log(r.Context(), level, "request", "client", client, "method", r.Method,
			"path", r.URL.Path, "status", sw.code, "ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) auth(next http.Handler) http.Handler {
	want := []byte("Bearer " + s.Token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			writeError(w, http.StatusUnauthorized, errors.New("missing or wrong token"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wss, err := s.Store.Workspaces(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	out := api.Status{
		Version: version.Version, PID: os.Getpid(), Started: s.started,
		Store: s.Store.Driver(), Config: s.ConfigPath, Workspaces: []api.WorkspaceSummary{},
	}
	for _, ws := range wss {
		repos, err := s.Store.Repos(ctx, ws.ID)
		if err != nil {
			s.fail(w, err)
			return
		}
		sum := api.WorkspaceSummary{Workspace: ws, Repos: len(repos)}
		for _, rp := range repos {
			lanes, err := s.Store.Lanes(ctx, rp.ID)
			if err != nil {
				s.fail(w, err)
				return
			}
			sum.Lanes += len(lanes)
		}
		out.Workspaces = append(out.Workspaces, sum)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	wss, err := s.Store.Workspaces(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	out := []api.WorkspaceDetail{}
	for _, ws := range wss {
		repos, err := s.Store.Repos(r.Context(), ws.ID)
		if err != nil {
			s.fail(w, err)
			return
		}
		out = append(out, api.WorkspaceDetail{Workspace: ws, Repos: repos})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) saveWorkspace(w http.ResponseWriter, r *http.Request) {
	var req api.SaveWorkspace
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := validateSave(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ws, err := s.Store.SaveWorkspace(r.Context(), store.Workspace{Name: req.Name, Path: req.Path}, req.Repos)
	if err != nil {
		s.fail(w, err)
		return
	}
	repos, err := s.Store.Repos(r.Context(), ws.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("workspace saved", "name", ws.Name, "path", ws.Path, "repos", len(repos))
	writeJSON(w, http.StatusOK, api.WorkspaceDetail{Workspace: ws, Repos: repos})
}

// validateSave checks a SaveWorkspace and canonicalises its paths. Every repo must sit
// inside the workspace, and its name must be its path relative to the workspace.
func validateSave(req *api.SaveWorkspace) error {
	if req.Name == "" || strings.ContainsAny(req.Name, `/\`) {
		return fmt.Errorf("workspace name %q: must be non-empty with no slashes", req.Name)
	}
	if !filepath.IsAbs(req.Path) {
		return fmt.Errorf("workspace path %q: must be absolute", req.Path)
	}
	root, err := paths.Canonical(req.Path)
	if err != nil {
		return err
	}
	req.Path = root
	for i := range req.Repos {
		rp := &req.Repos[i]
		if !filepath.IsAbs(rp.Path) {
			return fmt.Errorf("repo path %q: must be absolute", rp.Path)
		}
		if rp.Path, err = paths.Canonical(rp.Path); err != nil {
			return err
		}
		if rp.Path == root || !paths.Within(root, rp.Path) {
			return fmt.Errorf("repo %q is not inside workspace %s", rp.Path, root)
		}
		rel, _ := filepath.Rel(root, rp.Path)
		if rp.Name != filepath.ToSlash(rel) {
			return fmt.Errorf("repo name %q: must be its path in the workspace, %q", rp.Name, filepath.ToSlash(rel))
		}
	}
	return nil
}

func (s *Server) resolve(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if !filepath.IsAbs(p) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("path %q: must be absolute", p))
		return
	}
	res, err := Resolve(r.Context(), s.Store, s.Config, p)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// Resolve says which workspace, repo and lane a path belongs to, the way git finds
// .git: the deepest match wins at each level.
func Resolve(ctx context.Context, st store.Store, cfg config.Config, p string) (api.Resolution, error) {
	p, err := paths.Canonical(p)
	if err != nil {
		return api.Resolution{}, err
	}
	res := api.Resolution{Path: p}
	wss, err := st.Workspaces(ctx)
	if err != nil {
		return res, err
	}
	for i := range wss {
		ws := wss[i]
		if paths.Within(ws.Path, p) && (res.Workspace == nil || len(ws.Path) > len(res.Workspace.Path)) {
			res.Workspace = &ws
		}
	}
	if res.Workspace == nil {
		return res, nil
	}
	repos, err := st.Repos(ctx, res.Workspace.ID)
	if err != nil {
		return res, err
	}
	// A lane's worktree usually sits outside its repo, so check lanes of every repo.
	for i := range repos {
		rp := repos[i]
		lanes, err := st.Lanes(ctx, rp.ID)
		if err != nil {
			return res, err
		}
		for j := range lanes {
			l := lanes[j]
			if l.Worktree != "" && paths.Within(l.Worktree, p) && (res.Lane == nil || len(l.Worktree) > len(res.Lane.Worktree)) {
				res.Lane, res.Repo = &l, &rp
			}
		}
	}
	if res.Lane == nil {
		for i := range repos {
			rp := repos[i]
			if paths.Within(rp.Path, p) && (res.Repo == nil || len(rp.Path) > len(res.Repo.Path)) {
				res.Repo = &rp
			}
		}
	}
	if res.Repo != nil {
		prof := cfg.Profile(res.Repo.Name)
		res.Profile = &prof
	}
	return res, nil
}

func (s *Server) listLanes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsID, err1 := strconv.ParseInt(r.URL.Query().Get("workspace_id"), 10, 64)
	if err1 != nil {
		writeError(w, http.StatusBadRequest, errors.New("workspace_id is required"))
		return
	}
	repoID, _ := strconv.ParseInt(r.URL.Query().Get("repo_id"), 10, 64)
	repos, err := s.Store.Repos(ctx, wsID)
	if err != nil {
		s.fail(w, err)
		return
	}
	out := []api.LaneView{}
	for _, rp := range repos {
		if repoID != 0 && rp.ID != repoID {
			continue
		}
		lanes, err := s.Store.Lanes(ctx, rp.ID)
		if err != nil {
			s.fail(w, err)
			return
		}
		for _, l := range lanes {
			out = append(out, api.LaneView{Lane: l, Repo: rp.Name})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) openLane(w http.ResponseWriter, r *http.Request) {
	var req fold.OpenRequest
	if !decode(w, r, &req) {
		return
	}
	lane, err := s.Fold.Open(r.Context(), req)
	if err != nil {
		s.foldError(w, err)
		return
	}
	s.Log.Info("lane opened", "lane", lane.Name, "repo_id", lane.RepoID, "worktree", lane.Worktree)
	writeJSON(w, http.StatusOK, lane)
}

func (s *Server) closeLane(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var req api.CloseLane
	if !decode(w, r, &req) {
		return
	}
	res, err := s.Fold.Close(r.Context(), id, req.Force)
	if err != nil {
		s.foldError(w, err)
		return
	}
	s.Log.Info("lane closed", "lane", res.Lane.Name, "force", req.Force, "branch_deleted", res.BranchDeleted)
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) foldGC(w http.ResponseWriter, r *http.Request) {
	wsID, err := strconv.ParseInt(r.URL.Query().Get("workspace_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("workspace_id is required"))
		return
	}
	stale, err := s.Fold.GC(r.Context(), wsID)
	if err != nil {
		s.fail(w, err)
		return
	}
	if stale == nil {
		stale = []fold.Stale{}
	}
	writeJSON(w, http.StatusOK, stale)
}

func (s *Server) prePush(w http.ResponseWriter, r *http.Request) {
	var req api.PrePush
	if !decode(w, r, &req) {
		return
	}
	if !filepath.IsAbs(req.Path) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("path %q: must be absolute", req.Path))
		return
	}
	res, err := Resolve(r.Context(), s.Store, s.Config, req.Path)
	if err != nil {
		s.fail(w, err)
		return
	}
	v, err := s.Fold.CheckPush(r.Context(), res.Lane, req.Path, req.Refs)
	if err != nil {
		s.fail(w, err)
		return
	}
	if v.Lane != "" {
		s.Log.Info("pre-push checked", "lane", v.Lane, "ok", v.OK, "problems", len(v.Problems))
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) repoHook(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var req api.RepoHook
	if !decode(w, r, &req) {
		return
	}
	repo, err := s.Store.Repo(r.Context(), id)
	if err != nil {
		s.foldError(w, err)
		return
	}
	var st fold.HookState
	switch req.Action {
	case "status":
		st, err = fold.Hook(r.Context(), repo.Path)
	case "install":
		st, err = fold.InstallHook(r.Context(), repo.Path, s.Fold.Exe)
	case "uninstall":
		st, err = fold.UninstallHook(r.Context(), repo.Path)
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("action %q: want install, uninstall or status", req.Action))
		return
	}
	if err != nil {
		s.foldError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// foldError answers a refusal with 409 and its reason, a missing lane or repo with 404,
// and anything else as a failure.
func (s *Server) foldError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fold.ErrRefused):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	default:
		s.fail(w, err)
	}
}

// decode reads a JSON body strictly, answering 400 itself when it cannot.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return false
	}
	return true
}

// shutdown asks Run to stop. The daemon then exits cleanly, which a service manager
// configured to restart on failure only leaves stopped.
func (s *Server) shutdown(w http.ResponseWriter, r *http.Request) {
	s.Log.Info("shutdown requested")
	writeJSON(w, http.StatusAccepted, struct{}{})
	s.stopOnce.Do(func() { close(s.stop) })
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	s.Log.Error("request failed", "err", err)
	writeError(w, http.StatusInternalServerError, err)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, api.Error{Error: redact.String(err.Error())})
}

// Run takes the home's lock, listens, writes the runtime file, serves until ctx is done
// or a shutdown is requested, then removes the runtime file. The lock makes a second
// daemon on the same home refuse to start, even when two start at the same moment.
func (s *Server) Run(ctx context.Context, runtimePath string) error {
	lock, err := acquireLock(filepath.Join(filepath.Dir(runtimePath), "daemon.lock"))
	if err != nil {
		if rt, rerr := ReadRuntime(runtimePath); rerr == nil {
			return fmt.Errorf("a daemon is already running (pid %d at %s)", rt.PID, rt.Addr)
		}
		return fmt.Errorf("a daemon is already running (%v)", err)
	}
	defer lock.Close()

	ln, err := net.Listen("tcp", s.Config.Daemon.Listen)
	if err != nil {
		return err
	}
	rt := api.Runtime{
		Addr: ln.Addr().String(), PID: os.Getpid(), Token: s.Token,
		Version: version.Version, Started: s.started,
	}
	if err := writeRuntime(runtimePath, rt); err != nil {
		ln.Close()
		return err
	}
	defer os.Remove(runtimePath)

	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	s.Log.Info("shepherd daemon listening", "addr", rt.Addr, "version", rt.Version)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	case <-s.stop:
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.Log.Info("shepherd daemon stopping")
	return srv.Shutdown(shutdown)
}

// ReadRuntime reads a daemon's runtime file.
func ReadRuntime(path string) (api.Runtime, error) {
	var rt api.Runtime
	b, err := os.ReadFile(path)
	if err != nil {
		return rt, err
	}
	return rt, json.Unmarshal(b, &rt)
}

// writeRuntime writes the file readable by its owner only, since it holds the token, and
// atomically, so a client never reads half of it.
func writeRuntime(path string, rt api.Runtime) error {
	b, err := json.MarshalIndent(rt, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".daemon-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Alive reports whether the daemon described by rt answers.
func Alive(rt api.Runtime) bool {
	c := http.Client{Timeout: time.Second}
	req, err := http.NewRequest(http.MethodGet, "http://"+rt.Addr+api.PathStatus, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+rt.Token)
	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
