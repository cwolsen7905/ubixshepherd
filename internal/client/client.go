// Package client talks to a running daemon over the HTTP API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/daemon"
	"github.com/ubixsys/ubixshepherd/internal/fold"
)

// ErrNoDaemon means no daemon is running for this Shepherd home.
var ErrNoDaemon = errors.New("the shepherd daemon is not running (start it with: shepherd daemon start)")

// Client calls one daemon.
type Client struct {
	// Name says who is calling (cli, mcp, hook), for the daemon's log.
	Name  string
	base  string
	token string
	http  *http.Client
}

// New returns a client for the daemon at base (for example http://127.0.0.1:7400).
func New(base, token string) *Client {
	// Lane calls fetch from remotes, so the ceiling is generous; callers that only probe
	// pass a context with a short deadline.
	return &Client{base: base, token: token, http: &http.Client{Timeout: 5 * time.Minute}}
}

// FromRuntime returns a client for the daemon described by the runtime file at path.
func FromRuntime(path string) (*Client, error) {
	rt, err := daemon.ReadRuntime(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoDaemon
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return New("http://"+rt.Addr, rt.Token), nil
}

func (c *Client) Status(ctx context.Context) (api.Status, error) {
	var out api.Status
	return out, c.do(ctx, http.MethodGet, api.PathStatus, nil, &out)
}

// Shutdown asks the daemon to stop.
func (c *Client) Shutdown(ctx context.Context) error {
	var out struct{}
	return c.do(ctx, http.MethodPost, api.PathShutdown, nil, &out)
}

func (c *Client) Lanes(ctx context.Context, workspaceID, repoID int64) ([]api.LaneView, error) {
	var out []api.LaneView
	q := fmt.Sprintf("%s?workspace_id=%d", api.PathLanes, workspaceID)
	if repoID != 0 {
		q += fmt.Sprintf("&repo_id=%d", repoID)
	}
	return out, c.do(ctx, http.MethodGet, q, nil, &out)
}

func (c *Client) OpenLane(ctx context.Context, req fold.OpenRequest) (fold.Opened, error) {
	var out fold.Opened
	return out, c.do(ctx, http.MethodPost, api.PathLanes, req, &out)
}

func (c *Client) CloseLane(ctx context.Context, id int64, force bool) (fold.CloseResult, error) {
	var out fold.CloseResult
	return out, c.do(ctx, http.MethodPost, api.PathLaneClose(id), api.CloseLane{Force: force}, &out)
}

func (c *Client) FoldGC(ctx context.Context, workspaceID int64) ([]fold.Stale, error) {
	var out []fold.Stale
	return out, c.do(ctx, http.MethodGet, fmt.Sprintf("%s?workspace_id=%d", api.PathFoldGC, workspaceID), nil, &out)
}

func (c *Client) PrePush(ctx context.Context, req api.PrePush) (fold.Verdict, error) {
	var out fold.Verdict
	return out, c.do(ctx, http.MethodPost, api.PathPrePush, req, &out)
}

func (c *Client) RepoHook(ctx context.Context, repoID int64, action string) (fold.HookState, error) {
	var out fold.HookState
	return out, c.do(ctx, http.MethodPost, api.PathRepoHook(repoID), api.RepoHook{Action: action}, &out)
}

func (c *Client) Workspaces(ctx context.Context) ([]api.WorkspaceDetail, error) {
	var out []api.WorkspaceDetail
	return out, c.do(ctx, http.MethodGet, api.PathWorkspaces, nil, &out)
}

func (c *Client) SaveWorkspace(ctx context.Context, req api.SaveWorkspace) (api.WorkspaceDetail, error) {
	var out api.WorkspaceDetail
	return out, c.do(ctx, http.MethodPost, api.PathWorkspaces, req, &out)
}

func (c *Client) Resolve(ctx context.Context, path string) (api.Resolution, error) {
	var out api.Resolution
	return out, c.do(ctx, http.MethodGet, api.PathResolve+"?path="+url.QueryEscape(path), nil, &out)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if c.Name != "" {
		req.Header.Set(api.ClientHeader, c.Name)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// A stale runtime file from a daemon that died without cleaning up.
		return fmt.Errorf("%w (%v)", ErrNoDaemon, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		var e api.Error
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error != "" {
			return fmt.Errorf("daemon: %s", e.Error)
		}
		return fmt.Errorf("daemon: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
