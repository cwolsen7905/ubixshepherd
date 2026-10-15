package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// declareArtifact posts a declaration to the run's own route with the run's token. It
// is the one daemon call the CLI makes without internal/client, which has no method
// for it yet (the lane's scope does not include that package).
func declareArtifact(ctx context.Context, env Env, runID int64, d api.ArtifactDeclare) (api.ArtifactDeclared, error) {
	var out api.ArtifactDeclared
	tok, base, _, err := workerCreds(ctx, env)
	if err != nil {
		return out, err
	}
	body, err := json.Marshal(d)
	if err != nil {
		return out, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+api.PathRunArtifacts(runID), bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(api.ClientHeader, "worker")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return out, fmt.Errorf("cannot reach the daemon (%v)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		var e api.Error
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error != "" {
			return out, fmt.Errorf("daemon: %s", e.Error)
		}
		return out, fmt.Errorf("daemon: %s", resp.Status)
	}
	return out, json.NewDecoder(resp.Body).Decode(&out)
}
