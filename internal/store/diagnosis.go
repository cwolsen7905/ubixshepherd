package store

import "time"

// Diagnosis kinds: the closed set of reasons a run, or what Shepherd did with its work,
// went wrong. A diagnosis is worked out deterministically from what Shepherd saw; no
// model is asked.
const (
	DiagStartFailed      = "start_failed"      // the agent never started: binary missing, spawn error, worktree gone
	DiagAgentError       = "agent_error"       // the agent exited non-zero
	DiagHung             = "hung"              // printed nothing for the idle timeout, so it was stopped
	DiagQuota            = "quota"             // the agent's account is out of quota or past a usage limit
	DiagNoChange         = "no_change"         // exited 0 but left the lane as it found it
	DiagGateFailed       = "gate_failed"       // the repo's gate failed before a push
	DiagStandardsFailed  = "standards_failed"  // a standards `must` check failed before a push
	DiagCommitsForbidden = "commits_forbidden" // a commit message matches the repo's forbidden patterns
	DiagOutsideScope     = "outside_scope"     // the run changed files outside the lane's scope
	DiagPushRefused      = "push_refused"      // a push was refused (the pre-push hook, or the remote)
	DiagMergeRefused     = "merge_refused"     // the forge refused the merge request or the merge
	DiagStopped          = "stopped"           // stopped by the person or the desk
	DiagInterrupted      = "interrupted"       // the daemon stopped or crashed while the run was going
	DiagBudgetCap        = "budget_cap"        // a hard spend cap, or the agent's context, was reached
	DiagUnknown          = "unknown"           // failed, and Shepherd could not tell why
)

// DiagnosisKinds lists every kind.
var DiagnosisKinds = []string{
	DiagStartFailed, DiagAgentError, DiagHung, DiagQuota, DiagNoChange, DiagGateFailed,
	DiagStandardsFailed, DiagCommitsForbidden, DiagOutsideScope, DiagPushRefused,
	DiagMergeRefused, DiagStopped, DiagInterrupted, DiagBudgetCap, DiagUnknown,
}

// Fix operations: what a suggested action does, each an existing Shepherd operation.
const (
	FixContinue      = "continue"        // continue the run's session with Args["prompt"]
	FixRetry         = "retry"           // a new session, same agent, the run's prompt
	FixRetryOther    = "retry_other"     // a new session with Args["agent"] and/or Args["model"]
	FixWaitQuota     = "wait_quota"      // nothing to do until the agent's quota resets
	FixWidenScope    = "widen_scope"     // add Args["paths"] to the lane's scope; the person's call
	FixOpenLog       = "open_log"        // read the run's log (Args["path"])
	FixRaiseBudget   = "raise_budget"    // lift a hard cap; the person's call
	FixShip          = "ship"            // run the gate and push again
	FixOpenMR        = "open_mr"         // open the merge request on the forge by hand
	FixFixConfigText = "fix_config_text" // change the repo's rules; the person's call
)

// Fix is one suggested action for a diagnosis, as a button a client can offer.
type Fix struct {
	// ID names the fix within its diagnosis ("continue", "retry", ...).
	ID    string `json:"id"`
	Label string `json:"label"`
	Op    string `json:"op"`
	// Args holds what the operation needs (a prompt, an agent, paths, a log path).
	Args map[string]string `json:"args,omitempty"`
	// NeedsPerson marks a fix only the person may choose: it widens what an agent may
	// touch, spends beyond a cap or changes rules. DeskOK marks one the front desk may
	// take on work the person already asked for.
	NeedsPerson bool `json:"needs_person"`
	DeskOK      bool `json:"desk_ok"`
}

// Diagnosis says why a run, or the push of its work, went wrong. A run may have several:
// the agent's own failure when it exits, and later what Shepherd found when it checked a
// run that succeeded. The newest is the headline.
type Diagnosis struct {
	ID    int64  `json:"id"`
	RunID int64  `json:"run_id"`
	Kind  string `json:"kind"`
	// Summary is one plain line, fit for the feed.
	Summary string `json:"summary"`
	// Evidence is the relevant redacted output, capped in size: the end of the agent's
	// output, or the failing command's tail.
	Evidence string `json:"evidence,omitempty"`
	// Command and ExitCode are the failing command, for a gate or check failure.
	Command  string   `json:"command,omitempty"`
	ExitCode *int     `json:"exit_code,omitempty"`
	Files    []string `json:"files,omitempty"`
	// Log is where the whole output is, when Evidence is only the end of it.
	Log     string     `json:"log,omitempty"`
	ResetAt *time.Time `json:"reset_at,omitempty"`
	Fixes   []Fix      `json:"fixes,omitempty"`
	Created time.Time  `json:"created"`
}
