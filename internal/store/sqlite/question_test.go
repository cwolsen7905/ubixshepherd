package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestQuestions(t *testing.T) {
	db, _ := open(t)
	ctx := context.Background()
	a, err := db.AddQuestion(ctx, store.Question{WorkspaceID: 1, Turn: 5, Text: "  ship it?  "})
	if err != nil {
		t.Fatal(err)
	}
	if a.State != store.QuestionOpen || a.Text != "ship it?" || a.Turn != 5 || a.Closed != nil {
		t.Errorf("added: %+v", a)
	}
	long, err := db.AddQuestion(ctx, store.Question{WorkspaceID: 2, Text: strings.Repeat("x", 5000)})
	if err != nil || len(long.Text) != store.MaxQuestionText {
		t.Fatalf("long: %d %v", len(long.Text), err)
	}
	if _, err := db.AddQuestion(ctx, store.Question{Text: " "}); err == nil {
		t.Error("an empty question was accepted")
	}

	if open, _ := db.Questions(ctx, 1, store.QuestionOpen); len(open) != 1 || open[0].ID != a.ID {
		t.Errorf("workspace 1 open: %+v", open)
	}
	if all, _ := db.Questions(ctx, 0, ""); len(all) != 2 {
		t.Errorf("all: %d", len(all))
	}

	c, err := db.CloseQuestion(ctx, a.ID, store.QuestionAnswered, 9)
	if err != nil || c.State != store.QuestionAnswered || c.AnsweredTurn != 9 || c.Closed == nil {
		t.Fatalf("answered: %+v %v", c, err)
	}
	if _, err := db.CloseQuestion(ctx, a.ID, store.QuestionSkipped, 0); !errors.Is(err, store.ErrConflict) {
		t.Errorf("closing twice: %v", err)
	}
	if _, err := db.CloseQuestion(ctx, 99, store.QuestionSkipped, 0); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	if _, err := db.CloseQuestion(ctx, long.ID, "open", 0); err == nil {
		t.Error("closing as open was accepted")
	}
	if open, _ := db.Questions(ctx, 0, store.QuestionOpen); len(open) != 1 {
		t.Errorf("open after close: %d", len(open))
	}
}
