package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/daemon"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/store"
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

// operatorCall sends one request to the daemon with the operator's token. internal/client
// has no artifact methods yet (the lane's scope does not include that package), so the
// artifact commands call the routes themselves.
func operatorCall(ctx context.Context, env Env, method, path string, body, out any) error {
	if env.Remote != nil {
		return fmt.Errorf("artifact commands run on the machine the daemon is on")
	}
	if _, err := dial(ctx, env); err != nil {
		return err
	}
	rt, err := daemon.ReadRuntime(env.Layout.Runtime())
	if err != nil {
		return err
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, "http://"+rt.Addr+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+rt.Token)
	req.Header.Set(api.ClientHeader, env.Client)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach the daemon (%v)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		var e api.Error
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error != "" {
			return fmt.Errorf("daemon: %s", e.Error)
		}
		return fmt.Errorf("daemon: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// shepherd artifact list | show | pin | unpin | gc: what agents wrote for the person to
// read. The person's other choices (intent, move, discard) are made in the web.
func runArtifact(ctx context.Context, env Env, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	fs := flags("artifact "+args[0], env)
	lane := fs.Int64("lane", 0, "list: only this lane's artifacts (id)")
	state := fs.String("state", "", "list: live, kept, committed, discarded or gone")
	asJSON := fs.Bool("json", false, "print JSON")
	days := fs.Int("keep-days", 0, "gc: days a closed lane's snapshots are kept (default 30)")
	capMB := fs.Int64("cap-mb", 0, "gc: total size cap in MiB (default 500)")
	dry := fs.Bool("dry-run", false, "gc: say what would go, remove nothing")
	pos, err := parse(fs, args[1:])
	if err != nil {
		return err
	}
	w := env.Stdout
	id := func() (int64, error) {
		if len(pos) != 1 {
			return 0, errUsage
		}
		return strconv.ParseInt(pos[0], 10, 64)
	}
	switch args[0] {
	case "list", "ls":
		var list []store.Artifact
		if err := operatorCall(ctx, env, http.MethodGet, api.ArtifactListQuery(store.ArtifactFilter{LaneID: *lane, State: *state}), nil, &list); err != nil {
			return err
		}
		if *asJSON {
			return json.NewEncoder(w).Encode(list)
		}
		if len(list) == 0 {
			fmt.Fprintln(w, "No artifacts.")
			return nil
		}
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tLANE\tSTATE\tINTENT\tPINNED\tPATH")
		for _, a := range list {
			fmt.Fprintf(tw, "%d\t%d\t%s\t%s\t%v\t%s\n", a.ID, a.LaneID, a.State, a.Intent, a.Pinned, a.Path)
		}
		return tw.Flush()
	case "show":
		n, err := id()
		if err != nil {
			return err
		}
		var a store.Artifact
		if err := operatorCall(ctx, env, http.MethodGet, api.PathArtifact(n), nil, &a); err != nil {
			return err
		}
		if *asJSON {
			return json.NewEncoder(w).Encode(a)
		}
		fmt.Fprintf(w, "%s (artifact %d, lane %d)\n  state %s, intent %s (%s), %d bytes, pinned %v\n", a.Path, a.ID, a.LaneID, a.State, a.Intent, a.IntentBy, a.Size, a.Pinned)
		for _, v := range a.Versions {
			copyd := "no copy"
			if v.Snapshotted != nil {
				copyd = "copied"
			}
			fmt.Fprintf(w, "  version %s run %d %s\n", v.SHA256[:12], v.RunID, copyd)
		}
		return nil
	case "pin", "unpin":
		n, err := id()
		if err != nil {
			return err
		}
		pinned := args[0] == "pin"
		var a store.Artifact
		if err := operatorCall(ctx, env, http.MethodPatch, api.PathArtifact(n), map[string]any{"pinned": pinned}, &a); err != nil {
			return err
		}
		fmt.Fprintf(w, "%s: pinned %v\n", a.Path, a.Pinned)
		return nil
	case "gc":
		var res fold.GCResult
		body := map[string]any{"keep_days": *days, "cap_bytes": *capMB << 20, "dry_run": *dry}
		if err := operatorCall(ctx, env, http.MethodPost, api.PathArtifacts+"/gc", body, &res); err != nil {
			return err
		}
		if *asJSON {
			return json.NewEncoder(w).Encode(res)
		}
		verb := "removed"
		if *dry {
			verb = "would remove"
		}
		fmt.Fprintf(w, "%s %d expired, %d evicted over the cap, %d orphaned; %d bytes freed, %d kept\n",
			verb, res.Expired, res.Evicted, res.Orphans, res.FreedByte, res.KeptByte)
		return nil
	}
	return errUsage
}
