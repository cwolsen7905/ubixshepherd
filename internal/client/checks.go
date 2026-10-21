package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// Checks lists a repo's approved check files (GET /v1/checks); all adds released ones.
func (c *Client) Checks(ctx context.Context, repoID int64, all bool) (api.Checks, error) {
	var out api.Checks
	q := url.Values{"repo_id": {strconv.FormatInt(repoID, 10)}}
	if all {
		q.Set("all", "1")
	}
	return out, c.do(ctx, http.MethodGet, api.PathChecks+"?"+q.Encode(), nil, &out)
}

// ChecksStatus reads each live mark against the repo's base branch
// (GET /v1/checks?status=1).
func (c *Client) ChecksStatus(ctx context.Context, repoID int64) (api.Checks, error) {
	var out api.Checks
	q := url.Values{"repo_id": {strconv.FormatInt(repoID, 10)}, "status": {"1"}}
	return out, c.do(ctx, http.MethodGet, api.PathChecks+"?"+q.Encode(), nil, &out)
}

// ApproveChecks marks files already on the base branch (POST /v1/checks/approve). The
// daemon refuses it from an agent, and from the front desk without an answered decision.
func (c *Client) ApproveChecks(ctx context.Context, req api.ApproveChecks) (api.CheckChange, error) {
	var out api.CheckChange
	return out, c.do(ctx, http.MethodPost, api.PathChecksApprove, req, &out)
}

// ReleaseChecks lifts the marks on files, with a reason (POST /v1/checks/release).
func (c *Client) ReleaseChecks(ctx context.Context, req api.ReleaseChecks) (api.CheckChange, error) {
	var out api.CheckChange
	return out, c.do(ctx, http.MethodPost, api.PathChecksRelease, req, &out)
}

// RescopeLaneExcluding is RescopeLane that also returns the approved check files the new
// scope reaches but the lane may not change.
func (c *Client) RescopeLaneExcluding(ctx context.Context, id int64, add, remove []string) (api.Rescoped, error) {
	var out api.Rescoped
	return out, c.do(ctx, http.MethodPost, api.PathLaneScope(id), api.Rescope{Add: add, Remove: remove}, &out)
}
