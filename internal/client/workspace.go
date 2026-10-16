package client

import (
	"context"
	"net/http"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// RenameWorkspace changes a workspace's name; its path, repos, lanes and history stay.
// The daemon refuses an empty name and one another workspace uses.
func (c *Client) RenameWorkspace(ctx context.Context, id int64, name string) (api.WorkspaceDetail, error) {
	var out api.WorkspaceDetail
	return out, c.do(ctx, http.MethodPost, api.PathWorkspaceRename(id), api.RenameWorkspace{Name: name}, &out)
}
