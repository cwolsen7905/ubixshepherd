package client

import (
	"context"
	"io"
	"net/http"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// maxArtifactRead bounds what ArtifactContent reads; the daemon caps text and images
// below it already.
const maxArtifactRead = 16 << 20

// LaneArtifacts reads a lane's artifacts by discovery (GET /v1/lanes/{id}/artifacts).
func (c *Client) LaneArtifacts(ctx context.Context, laneID int64) (fold.LaneArtifacts, error) {
	var out fold.LaneArtifacts
	return out, c.do(ctx, http.MethodGet, api.PathLaneArtifacts(laneID), nil, &out)
}

// Artifacts lists stored artifacts, newest first (GET /v1/artifacts).
func (c *Client) Artifacts(ctx context.Context, f store.ArtifactFilter) ([]store.Artifact, error) {
	var out []store.Artifact
	return out, c.do(ctx, http.MethodGet, api.ArtifactListQuery(f), nil, &out)
}

// Artifact reads one artifact (GET /v1/artifacts/{id}).
func (c *Client) Artifact(ctx context.Context, id int64) (store.Artifact, error) {
	var out store.Artifact
	return out, c.do(ctx, http.MethodGet, api.PathArtifact(id), nil, &out)
}

// ArtifactContent reads an artifact's bytes (GET /v1/artifacts/{id}/content): live
// while its lane is open, else from its snapshot. version is a recorded sha-256, or ""
// for the newest. The text is already redacted by the daemon.
func (c *Client) ArtifactContent(ctx context.Context, id int64, version string) ([]byte, error) {
	resp, err := c.send(ctx, http.MethodGet, api.PathArtifactContent(id, version), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, maxArtifactRead))
}

// DeclareArtifact names a file of the calling run's lane (POST /v1/runs/{id}/artifacts).
// Only the run's own worker token may.
func (c *Client) DeclareArtifact(ctx context.Context, runID int64, req api.ArtifactDeclare) (api.ArtifactDeclared, error) {
	var out api.ArtifactDeclared
	return out, c.do(ctx, http.MethodPost, api.PathRunArtifacts(runID), req, &out)
}

// PatchArtifact applies the person's change (PATCH /v1/artifacts/{id}); the daemon
// accepts it from the web only.
func (c *Client) PatchArtifact(ctx context.Context, id int64, req api.ArtifactPatch) (store.Artifact, error) {
	var out store.Artifact
	return out, c.do(ctx, http.MethodPatch, api.PathArtifact(id), req, &out)
}

// DiscardArtifact deletes an artifact from its worktree and its snapshots
// (POST /v1/artifacts/{id}/discard); the daemon accepts it from the web only.
func (c *Client) DiscardArtifact(ctx context.Context, id int64) (store.Artifact, error) {
	var out store.Artifact
	return out, c.do(ctx, http.MethodPost, api.PathArtifactDiscard(id), struct{}{}, &out)
}

// ArtifactGC applies retention now (POST /v1/artifacts/gc); the daemon accepts it from
// the web only.
func (c *Client) ArtifactGC(ctx context.Context, req api.ArtifactGC) (fold.GCResult, error) {
	var out fold.GCResult
	return out, c.do(ctx, http.MethodPost, api.PathArtifactGC, req, &out)
}
