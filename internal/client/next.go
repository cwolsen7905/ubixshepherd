package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
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

// Waiting reads everything that waits on the person (GET /v1/waiting); workspaceID 0
// is every workspace.
func (c *Client) Waiting(ctx context.Context, workspaceID int64) (api.Waiting, error) {
	var out api.Waiting
	p := api.PathWaiting
	if workspaceID != 0 {
		p += "?workspace_id=" + strconv.FormatInt(workspaceID, 10)
	}
	return out, c.do(ctx, http.MethodGet, p, nil, &out)
}

// Questions lists the front desk's questions to the person (GET /v1/questions);
// workspaceID 0 is every workspace and state "" every state.
func (c *Client) Questions(ctx context.Context, workspaceID int64, state string) ([]store.Question, error) {
	p := api.PathQuestions + "?state=" + url.QueryEscape(state)
	if workspaceID != 0 {
		p += "&workspace_id=" + strconv.FormatInt(workspaceID, 10)
	}
	var out api.QuestionList
	err := c.do(ctx, http.MethodGet, p, nil, &out)
	return out.Questions, err
}

// OpenQuestion records a question the desk asked (POST /v1/questions).
func (c *Client) OpenQuestion(ctx context.Context, req api.QuestionOpen) (store.Question, error) {
	var out store.Question
	return out, c.do(ctx, http.MethodPost, api.PathQuestions, req, &out)
}

// AnswerQuestion marks a question answered (POST /v1/questions/{id}/answer).
func (c *Client) AnswerQuestion(ctx context.Context, id int64, req api.QuestionAnswer) (store.Question, error) {
	var out store.Question
	return out, c.do(ctx, http.MethodPost, api.PathQuestion(id)+"/answer", req, &out)
}

// SkipQuestion clears a question without an answer (POST /v1/questions/{id}/skip).
func (c *Client) SkipQuestion(ctx context.Context, id int64) (store.Question, error) {
	var out store.Question
	return out, c.do(ctx, http.MethodPost, api.PathQuestion(id)+"/skip", struct{}{}, &out)
}
