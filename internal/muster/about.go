package muster

import (
	"context"
	"errors"
	"fmt"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// About is what a question, decision or next-up item is about, for the "About" label
// and for the checks that close what no longer matters.
type About struct {
	Repo   string `json:"repo,omitempty"`
	LaneID int64  `json:"lane_id,omitempty"`
	Lane   string `json:"lane,omitempty"`
	NextID int64  `json:"next_id,omitempty"`
}

// Label is the label's text: "repo / lane" or "repo / next-up 12".
func (a About) Label() string {
	switch {
	case a.Lane != "" && a.Repo != "":
		return a.Repo + " / " + a.Lane
	case a.NextID != 0 && a.Repo != "":
		return fmt.Sprintf("%s / next-up %d", a.Repo, a.NextID)
	}
	return a.Repo
}

// ErrNoRepo is why a next-up item is refused: every item is for a repo.
var ErrNoRepo = errors.New("a next-up item needs a repo")

// ValidateQuestion refuses a question that names nothing it is about.
func ValidateQuestion(q store.Question) error {
	if !q.HasAbout() {
		return store.ErrNoAbout
	}
	return nil
}

// ValidateNext refuses a next-up item with no repo.
func ValidateNext(it store.NextItem) error {
	if it.Repo == "" {
		return ErrNoRepo
	}
	return nil
}

// AboutQuestion reads what a question is about; the lane's name is filled in when the
// lane is still there.
func AboutQuestion(ctx context.Context, st store.Store, q store.Question) About {
	a := About{Repo: q.Repo, LaneID: q.LaneID, NextID: q.NextID}
	if q.LaneID != 0 {
		if l, err := st.Lane(ctx, q.LaneID); err == nil {
			a.Lane = l.Name
		}
	}
	return a
}

// AboutDecision reads what a decision is about: always the lane of the run that asked
// it, so a decision always has a link.
func AboutDecision(ctx context.Context, st store.Store, d store.Decision) (About, error) {
	run, err := st.Run(ctx, d.RunID)
	if err != nil {
		return About{}, err
	}
	l, err := st.Lane(ctx, run.LaneID)
	if err != nil {
		return About{}, err
	}
	a := About{LaneID: l.ID, Lane: l.Name}
	if rp, err := st.Repo(ctx, l.RepoID); err == nil {
		a.Repo = rp.Name
	}
	return a, nil
}

// AboutNext reads what a next-up item is about: its repo, and its lane if it has one.
func AboutNext(ctx context.Context, st store.Store, it store.NextItem) About {
	a := About{Repo: it.Repo, NextID: it.ID, LaneID: it.LaneID}
	if it.LaneID != 0 {
		if l, err := st.Lane(ctx, it.LaneID); err == nil {
			a.Lane = l.Name
		}
	}
	return a
}
