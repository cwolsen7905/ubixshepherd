package dispatch

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// escalationAgent is the agent a follow-up round moves to when the round before it,
// run on a cheaper agent or model, did not close what it was sent to close.
const escalationAgent = "claude"

// EscalationTarget says whether the next follow-up round on a merge request should
// leave the agent of last, the run that took the previous round, and where it goes.
// A round escalates when last ran on any agent but Claude Code, or on Claude Code with
// a model other than the repo's default Claude model. The target is Claude Code on
// that default model ("" when the repo names none, which is Claude's own default).
// A Claude run on the default model, or on no model when the repo names none, is
// already the top of the ladder: there is nowhere to go.
func EscalationTarget(cfg config.Config, repo string, last store.Run) (agent, model string, escalate bool) {
	def := cfg.ModelPolicy(repo, escalationAgent).Default
	switch {
	case last.Agent != escalationAgent:
		return escalationAgent, def, true
	case def != "" && last.Model != def:
		return escalationAgent, def, true
	}
	return escalationAgent, def, false
}

// escalate rewrites a follow-up start that asked to escalate: when the continued run
// was cheaper than Claude on the repo's default model, the round runs on Claude
// instead. It resumes the lane's Claude conversation when there is one (start finds the lane's latest Claude session
// when no run is continued), else begins one; the prompt already carries what is open, so it only gains a line saying why the
// agent changed. A start that cannot escalate (the repo turned it off, Claude is not
// installed or is out of quota, the previous run is already Claude on its default)
// goes on as asked. The returned note says why it did not, or "".
func (r *Runner) escalate(ctx context.Context, req StartRequest) (StartRequest, string) {
	req.Escalate = false
	cfg := r.Conf()
	last, err := r.Store.Run(ctx, req.Continue)
	if err != nil {
		return req, "could not read run: " + err.Error()
	}
	if last.State == store.RunRunning || last.Session == "" {
		// Let start refuse it as it would a plain continue.
		return req, "run cannot be continued"
	}
	lane, err := r.Store.Lane(ctx, last.LaneID)
	if err != nil {
		return req, "could not read lane: " + err.Error()
	}
	repo, err := r.Store.Repo(ctx, lane.RepoID)
	if err != nil {
		return req, "could not read repo: " + err.Error()
	}
	if !cfg.Profile(repo.Name).Watch.Escalates() {
		return req, ""
	}
	agent, model, ok := EscalationTarget(cfg, repo.Name, last)
	if !ok {
		return req, ""
	}
	look := r.lookPath
	if look == nil {
		look = exec.LookPath
	}
	if _, err := look("claude"); err != nil {
		return req, "claude is not installed here"
	}
	r.mu.Lock()
	why := r.quotaHeld(agent)
	r.mu.Unlock()
	if why != "" {
		return req, why
	}
	was := last.Agent
	if last.Model != "" {
		was += " (" + last.Model + ")"
	}
	req.Prompt = fmt.Sprintf("[Shepherd] The previous round on this merge request, by %s, did not close what it was sent to close, so this round is yours.\n\n%s", was, req.Prompt)
	if last.Agent == agent {
		// Same conversation, stronger model.
		req.Model = model
		return req, ""
	}
	req.Continue, req.LaneID, req.Agent, req.Model = 0, last.LaneID, agent, model
	return req, ""
}
