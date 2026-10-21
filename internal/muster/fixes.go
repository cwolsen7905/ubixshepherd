package muster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// undoableFix reports whether a fix can be taken back. A run that has been marked
// interrupted stays so, and acknowledging a finding changes nothing.
func undoableFix(fix string) bool { return fix != FixRunInterrupt && fix != FixAck }

type artifactBefore struct {
	State string `json:"state"`
	// Intents are for a batch: each artifact's intent and who chose it.
	Intents map[int64][2]string `json:"intents,omitempty"`
}

type nextBefore struct {
	State  string     `json:"state"`
	LaneID int64      `json:"lane_id"`
	DoneAt *time.Time `json:"done_at,omitempty"`
}

type scopeBefore struct {
	Scope []string `json:"scope"`
}

type stateBefore struct {
	State string `json:"state"`
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// do carries out a fix and returns what it changed, as JSON, for undo.
func (p *Pass) do(ctx context.Context, it store.MusterItem) (string, error) {
	st := p.Store
	switch it.Fix {
	case FixAck:
		return "", nil
	case FixArtifactGone, FixArtifactSettle:
		a, err := st.Artifact(ctx, it.SubjectID)
		if err != nil {
			return "", err
		}
		to := store.ArtifactGone
		if it.Fix == FixArtifactSettle {
			var arg struct{ State string }
			if err := json.Unmarshal([]byte(it.FixArg), &arg); err != nil || arg.State == "" {
				return "", fmt.Errorf("artifact %d: no state to settle to", a.ID)
			}
			to = arg.State
		}
		if err := st.SetArtifactState(ctx, a.ID, to, true); err != nil {
			return "", err
		}
		return jsonString(artifactBefore{State: a.State}), nil
	case FixArtifactKeepAll:
		var arg struct{ IDs []int64 }
		if err := json.Unmarshal([]byte(it.FixArg), &arg); err != nil {
			return "", err
		}
		b := artifactBefore{Intents: map[int64][2]string{}}
		for _, id := range arg.IDs {
			a, err := st.Artifact(ctx, id)
			if errors.Is(err, store.ErrNotFound) {
				continue // gone since
			}
			if err != nil {
				return "", err
			}
			if a.Intent != store.ArtifactUndecided {
				continue // the person decided it meanwhile
			}
			if err := st.SetArtifactIntent(ctx, id, store.ArtifactKeep, store.IntentByPerson); err != nil {
				return "", err
			}
			b.Intents[id] = [2]string{a.Intent, a.IntentBy}
		}
		return jsonString(b), nil
	case FixRunInterrupt:
		r, err := st.Run(ctx, it.SubjectID)
		if err != nil {
			return "", err
		}
		if r.State != store.RunRunning {
			return "", fmt.Errorf("run %d is %s, not running", r.ID, r.State)
		}
		t := p.now().UTC()
		r.State, r.Ended = store.RunInterrupted, &t
		if err := st.UpdateRun(ctx, r); err != nil {
			return "", err
		}
		return "", nil
	case FixLaneScope:
		var arg struct{ Paths []string }
		if err := json.Unmarshal([]byte(it.FixArg), &arg); err != nil {
			return "", err
		}
		l, err := st.Lane(ctx, it.SubjectID)
		if err != nil {
			return "", err
		}
		scope := append(append([]string{}, l.Scope...), arg.Paths...)
		if err := st.SetLaneScope(ctx, l.ID, scope); err != nil {
			return "", err
		}
		return jsonString(scopeBefore{Scope: l.Scope}), nil
	case FixNextReapprove, FixNextDone, FixNextDrop:
		n, err := st.NextItem(ctx, it.SubjectID)
		if err != nil {
			return "", err
		}
		to := map[string]string{FixNextReapprove: store.NextApproved, FixNextDone: store.NextDone, FixNextDrop: store.NextDropped}[it.Fix]
		patch := store.NextPatch{State: &to}
		if it.Fix == FixNextReapprove {
			zero := int64(0)
			patch.LaneID = &zero
		}
		if _, err := st.UpdateNextItem(ctx, n.ID, patch); err != nil {
			return "", err
		}
		return jsonString(nextBefore{State: n.State, LaneID: n.LaneID, DoneAt: n.DoneAt}), nil
	case FixQuestionClose, FixQuestionSkip:
		q, err := st.Question(ctx, it.SubjectID)
		if err != nil {
			return "", err
		}
		if q.State != store.QuestionOpen {
			return "", fmt.Errorf("question %d is %s, not open", q.ID, q.State)
		}
		to := store.QuestionOutdated
		if it.Fix == FixQuestionSkip {
			to = store.QuestionSkipped
		}
		if _, err := st.SetQuestionState(ctx, q.ID, to); err != nil {
			return "", err
		}
		return jsonString(stateBefore{State: q.State}), nil
	case FixDecisionClose:
		d, err := st.Decision(ctx, it.SubjectID)
		if err != nil {
			return "", err
		}
		if d.State != store.DecisionOpen {
			return "", fmt.Errorf("decision %d is %s, not open", d.ID, d.State)
		}
		if _, err := st.SetDecisionState(ctx, d.ID, store.DecisionOutdated); err != nil {
			return "", err
		}
		return jsonString(stateBefore{State: d.State}), nil
	}
	return "", fmt.Errorf("unknown fix %q", it.Fix)
}

// undo puts back what do changed.
func (p *Pass) undo(ctx context.Context, it store.MusterItem) error {
	st := p.Store
	switch it.Fix {
	case FixArtifactGone, FixArtifactSettle:
		var b artifactBefore
		if err := json.Unmarshal([]byte(it.Before), &b); err != nil {
			return err
		}
		closing := b.State == store.ArtifactGone || b.State == store.ArtifactKept || b.State == store.ArtifactInRepo || b.State == store.ArtifactDiscarded
		return st.SetArtifactState(ctx, it.SubjectID, b.State, closing)
	case FixArtifactKeepAll:
		var b artifactBefore
		if err := json.Unmarshal([]byte(it.Before), &b); err != nil {
			return err
		}
		for id, was := range b.Intents {
			if err := st.SetArtifactIntent(ctx, id, was[0], was[1]); err != nil {
				return err
			}
		}
		return nil
	case FixLaneScope:
		var b scopeBefore
		if err := json.Unmarshal([]byte(it.Before), &b); err != nil {
			return err
		}
		return st.SetLaneScope(ctx, it.SubjectID, b.Scope)
	case FixNextReapprove, FixNextDone, FixNextDrop:
		var b nextBefore
		if err := json.Unmarshal([]byte(it.Before), &b); err != nil {
			return err
		}
		patch := store.NextPatch{State: &b.State, LaneID: &b.LaneID, DoneAt: b.DoneAt}
		_, err := st.UpdateNextItem(ctx, it.SubjectID, patch)
		return err
	case FixQuestionClose, FixQuestionSkip:
		var b stateBefore
		if err := json.Unmarshal([]byte(it.Before), &b); err != nil {
			return err
		}
		_, err := st.SetQuestionState(ctx, it.SubjectID, b.State)
		return err
	case FixDecisionClose:
		var b stateBefore
		if err := json.Unmarshal([]byte(it.Before), &b); err != nil {
			return err
		}
		_, err := st.SetDecisionState(ctx, it.SubjectID, b.State)
		return err
	}
	return ErrNotUndoable
}
