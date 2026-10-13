package client

import (
	"context"
	"net/http"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// Update reads the daemon's cached release check (GET /v1/update).
func (c *Client) Update(ctx context.Context) (api.UpdateStatus, error) {
	var out api.UpdateStatus
	return out, c.do(ctx, http.MethodGet, api.PathUpdate, nil, &out)
}

// DismissUpdate hides the notice for a version (POST /v1/update/dismiss).
func (c *Client) DismissUpdate(ctx context.Context, version string) (api.UpdateStatus, error) {
	var out api.UpdateStatus
	return out, c.do(ctx, http.MethodPost, api.PathUpdateDismiss, api.UpdateDismiss{Version: version}, &out)
}

// UpgradeUpdate installs the offered release (POST /v1/update/upgrade).
func (c *Client) UpgradeUpdate(ctx context.Context, version string, now bool) (api.UpdateStatus, error) {
	var out api.UpdateStatus
	return out, c.do(ctx, http.MethodPost, api.PathUpdateUpgrade, api.UpdateUpgrade{Version: version, Now: now}, &out)
}
