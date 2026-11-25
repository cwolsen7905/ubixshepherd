package client

import (
	"context"
	"net/http"
	"net/url"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// Cluster reads what this daemon is in a cluster (GET /v1/cluster).
func (c *Client) Cluster(ctx context.Context) (api.Cluster, error) {
	var out api.Cluster
	return out, c.do(ctx, http.MethodGet, api.PathCluster, nil, &out)
}

// WorkerAdd names a worker on a controller and returns its one-time enrolment code.
func (c *Client) WorkerAdd(ctx context.Context, name string) (api.WorkerCode, error) {
	var out api.WorkerCode
	return out, c.do(ctx, http.MethodPost, api.PathClusterWorkers, api.WorkerAdd{Name: name}, &out)
}

// WorkerAct revokes, removes or confirms a worker (act is "revoke", "remove" or "confirm").
func (c *Client) WorkerAct(ctx context.Context, name, act string) error {
	var ok struct{}
	return c.do(ctx, http.MethodPost, api.PathClusterWorkers+"/"+url.PathEscape(name)+"/"+act, nil, &ok)
}

// ClusterJoin has this daemon enrol with the controller at controllerURL using a code
// from `shepherd worker add`.
func (c *Client) ClusterJoin(ctx context.Context, controllerURL, code string) (api.ClusterJoined, error) {
	var out api.ClusterJoined
	return out, c.do(ctx, http.MethodPost, api.PathClusterJoin, api.ClusterJoin{URL: controllerURL, Code: code}, &out)
}
