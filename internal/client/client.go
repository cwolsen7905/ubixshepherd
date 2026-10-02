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
)

// ErrNoDaemon means no daemon is running for this Shepherd home.
var ErrNoDaemon = errors.New("the shepherd daemon is not running (start it with: shepherd daemon)")

// Client calls one daemon.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// New returns a client for the daemon at base (for example http://127.0.0.1:7400).
func New(base, token string) *Client {
	return &Client{base: base, token: token, http: &http.Client{Timeout: 30 * time.Second}}
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
