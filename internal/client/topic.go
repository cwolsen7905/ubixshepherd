package client

import (
	"context"
	"net/http"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// Topics reads the topics; all adds cleared ones (GET /v1/topics).
func (c *Client) Topics(ctx context.Context, all bool) (api.TopicList, error) {
	var out api.TopicList
	p := api.PathTopics
	if all {
		p += "?all=1"
	}
	return out, c.do(ctx, http.MethodGet, p, nil, &out)
}

// Topic reads one topic with its links (GET /v1/topics/{id}).
func (c *Client) Topic(ctx context.Context, id int64) (api.TopicView, error) {
	var out api.TopicView
	return out, c.do(ctx, http.MethodGet, api.PathTopic(id), nil, &out)
}

// AddTopic makes a topic (POST /v1/topics).
func (c *Client) AddTopic(ctx context.Context, req api.TopicAdd) (api.TopicView, error) {
	var out api.TopicView
	return out, c.do(ctx, http.MethodPost, api.PathTopics, req, &out)
}

// UpdateTopic changes a topic (PATCH /v1/topics/{id}).
func (c *Client) UpdateTopic(ctx context.Context, id int64, req api.TopicUpdate) (api.TopicView, error) {
	var out api.TopicView
	return out, c.do(ctx, http.MethodPatch, api.PathTopic(id), req, &out)
}
