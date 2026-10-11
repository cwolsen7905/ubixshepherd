package client

import (
	"context"
	"net/http"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// NextItems reads the next-up list; all adds dropped and done items (GET /v1/next).
func (c *Client) NextItems(ctx context.Context, all bool) (api.NextList, error) {
	var out api.NextList
	p := api.PathNext
	if all {
		p += "?all=1"
	}
	return out, c.do(ctx, http.MethodGet, p, nil, &out)
}

// NextItem reads one item (GET /v1/next/{id}).
func (c *Client) NextItem(ctx context.Context, id int64) (api.NextItemView, error) {
	var out api.NextItemView
	return out, c.do(ctx, http.MethodGet, api.PathNextItem(id), nil, &out)
}

// AddNextItem adds an item (POST /v1/next).
func (c *Client) AddNextItem(ctx context.Context, req api.NextAdd) (api.NextItemView, error) {
	var out api.NextItemView
	return out, c.do(ctx, http.MethodPost, api.PathNext, req, &out)
}

// UpdateNextItem changes an item (PATCH /v1/next/{id}).
func (c *Client) UpdateNextItem(ctx context.Context, id int64, req api.NextUpdate) (api.NextItemView, error) {
	var out api.NextItemView
	return out, c.do(ctx, http.MethodPatch, api.PathNextItem(id), req, &out)
}
