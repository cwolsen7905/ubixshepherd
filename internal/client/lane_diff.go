package client

import (
	"context"
	"net/http"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/fold"
)

// LaneDiff reads what a lane holds against its base (GET /v1/lanes/{id}/diff).
func (c *Client) LaneDiff(ctx context.Context, id int64, req fold.DiffRequest) (fold.LaneDiff, error) {
	var out fold.LaneDiff
	return out, c.do(ctx, http.MethodGet, api.PathLaneDiff(id, req), nil, &out)
}
