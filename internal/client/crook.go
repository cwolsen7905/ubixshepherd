package client

import (
	"context"
	"net/http"
	"net/url"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// CrookVerdicts lists logged Crook verdicts (GET /v1/crook/verdicts); q carries the
// filters the route documents.
func (c *Client) CrookVerdicts(ctx context.Context, q url.Values) (api.CrookVerdicts, error) {
	var out api.CrookVerdicts
	return out, c.do(ctx, http.MethodGet, api.PathCrookVerdicts+deskQuery(0, q), nil, &out)
}

// CrookWhy reads one logged verdict with its path through the stages.
func (c *Client) CrookWhy(ctx context.Context, id int64) (api.CrookWhy, error) {
	var out api.CrookWhy
	return out, c.do(ctx, http.MethodGet, api.PathCrookVerdict(id), nil, &out)
}

// CrookRules counts verdicts per rule since a time (GET /v1/crook/rules).
func (c *Client) CrookRules(ctx context.Context, since string) (api.CrookRules, error) {
	var out api.CrookRules
	q := url.Values{}
	if since != "" {
		q.Set("since", since)
	}
	return out, c.do(ctx, http.MethodGet, api.PathCrookRules+deskQuery(0, q), nil, &out)
}
