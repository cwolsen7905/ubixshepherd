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

// MemberAdd names a member on a hub and returns its one-time enrolment code.
func (c *Client) MemberAdd(ctx context.Context, name string) (api.MemberCode, error) {
	var out api.MemberCode
	return out, c.do(ctx, http.MethodPost, api.PathClusterMembers, api.MemberAdd{Name: name}, &out)
}

// MemberAct revokes, removes or confirms a member (act is "revoke", "remove" or "confirm").
func (c *Client) MemberAct(ctx context.Context, name, act string) error {
	var ok struct{}
	return c.do(ctx, http.MethodPost, api.PathClusterMembers+"/"+url.PathEscape(name)+"/"+act, nil, &ok)
}

// ClusterJoin has this daemon enrol with the hub at hubURL using a code from `shepherd
// member add`.
func (c *Client) ClusterJoin(ctx context.Context, hubURL, code string) (api.ClusterJoined, error) {
	var out api.ClusterJoined
	return out, c.do(ctx, http.MethodPost, api.PathClusterJoin, api.ClusterJoin{URL: hubURL, Code: code}, &out)
}
