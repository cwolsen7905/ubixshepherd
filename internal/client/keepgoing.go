package client

import (
	"context"
	"net/http"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// KeepGoing reads keep-going mode (GET /v1/keep-going). workspaceID 0 means the
// daemon's only workspace.
func (c *Client) KeepGoing(ctx context.Context, workspaceID int64) (api.KeepGoingStatus, error) {
	var out api.KeepGoingStatus
	return out, c.do(ctx, http.MethodGet, api.PathKeepGoing+deskQuery(workspaceID, nil), nil, &out)
}

// SetKeepGoing turns the mode on or off (POST /v1/keep-going). The daemon refuses a
// turn-on that did not come from the person.
func (c *Client) SetKeepGoing(ctx context.Context, req api.KeepGoingSet) (api.KeepGoingStatus, error) {
	var out api.KeepGoingStatus
	return out, c.do(ctx, http.MethodPost, api.PathKeepGoing, req, &out)
}
