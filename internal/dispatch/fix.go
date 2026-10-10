package dispatch

import (
	"context"
	"errors"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Who is taking a fix.
const (
	ByPerson = "person"
	ByDesk   = "desk"
)

// FixResult is what taking a fix did.
type FixResult struct {
	Fix store.Fix `json:"fix"`
	// Run is the run a continue or retry started.
	Run *store.Run `json:"run,omitempty"`
	// Shipped is what a ship did.
	Shipped *Shipped `json:"shipped,omitempty"`
	// Message says what happened, or what the caller must do: for a fix that is not
	// Shepherd's to run here (widen_scope belongs to the Fold, which owns scope leases;
	// raise_budget and open_mr are the person's own edits), Done is false and the
	// caller carries it out with Fix.Op and Fix.Args.
	Message string `json:"message,omitempty"`
	Done    bool   `json:"done"`
}

// ApplyFix takes one of a run's suggested fixes (an id from its newest diagnosis that
// has it). by is ByPerson or ByDesk: the desk may take only the fixes marked DeskOK, and
// never one marked NeedsPerson, whatever the person's standing instruction. overrides
// replace the fix's arguments (a prompt the person edited, the agent and model for a
// retry on another agent); a fix the desk takes may not be overridden.
func (r *Runner) ApplyFix(ctx context.Context, runID int64, fixID, by string, overrides map[string]string) (FixResult, error) {
	var res FixResult
	if by != ByPerson && by != ByDesk {
		return res, refuse("a fix is taken by %q or %q, not %q", ByPerson, ByDesk, by)
	}
	run, err := r.Store.Run(ctx, runID)
	if err != nil {
		return res, err
	}
	ds, err := r.Store.Diagnoses(ctx, runID)
	if err != nil {
		return res, err
	}
	found := false
	for _, d := range ds {
		for _, f := range d.Fixes {
			if f.ID == fixID {
				res.Fix, found = f, true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return res, refuse("run %d has no suggested fix %q", runID, fixID)
	}
	f := res.Fix
	if by == ByDesk {
		if f.NeedsPerson || !f.DeskOK {
			return res, refuse("fix %q for run %d is the person's call, not the desk's", fixID, runID)
		}
		if len(overrides) > 0 {
			return res, refuse("the desk takes a fix as suggested, not edited")
		}
	}
	args := map[string]string{}
	for k, v := range f.Args {
		args[k] = v
	}
	for k, v := range overrides {
		args[k] = v
	}
	res.Fix.Args = args
	switch f.Op {
	case store.FixContinue:
		next, err := r.Start(ctx, StartRequest{Continue: run.ID, Prompt: args["prompt"]})
		if err != nil {
			return res, err
		}
		res.Run, res.Done, res.Message = &next, true, "continuing run's session"
	case store.FixRetry, store.FixRetryOther:
		agent, model := run.Agent, run.Model
		if f.Op == store.FixRetryOther {
			if args["agent"] == "" && args["model"] == "" {
				return res, refuse("name the agent or the model to retry with")
			}
			if args["agent"] != "" {
				agent = args["agent"]
				if agent != run.Agent && args["model"] == "" {
					model = ""
				}
			}
			if args["model"] != "" {
				model = args["model"]
			}
		}
		next, err := r.Start(ctx, StartRequest{LaneID: run.LaneID, Agent: agent, Model: model, Prompt: run.Prompt, NewSession: true})
		if err != nil {
			return res, err
		}
		res.Run, res.Done, res.Message = &next, true, "started a new session"
	case store.FixShip:
		sh, err := r.Ship(ctx, run.LaneID)
		res.Shipped = &sh
		res.Done, res.Message = err == nil, sh.Message
		if err != nil {
			return res, err
		}
	case store.FixWaitQuota:
		res.Done = true
		res.Message = "nothing to do: runs resume when the agent's quota resets"
		if ds[0].ResetAt != nil {
			res.Message += " at " + ds[0].ResetAt.Local().Format("Jan 2 15:04")
		}
	case store.FixOpenLog:
		res.Done, res.Message = true, run.Log
	case store.FixWidenScope, store.FixRaiseBudget, store.FixOpenMR, store.FixFixConfigText:
		res.Message = "this is the person's change to make: " + f.Label
	default:
		return res, errors.New("unknown fix operation " + f.Op)
	}
	return res, nil
}
