package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

const questionCols = `id, workspace_id, turn, topic_id, text, state, answered_turn, created, closed`

func scanQuestion(sc interface{ Scan(...any) error }) (store.Question, error) {
	var q store.Question
	var created, closed string
	err := sc.Scan(&q.ID, &q.WorkspaceID, &q.Turn, &q.TopicID, &q.Text, &q.State, &q.AnsweredTurn, &created, &closed)
	q.Created = parseTime(created)
	if closed != "" {
		c := parseTime(closed)
		q.Closed = &c
	}
	return q, err
}

func (s *DB) AddQuestion(ctx context.Context, q store.Question) (store.Question, error) {
	q.Text = strings.TrimSpace(q.Text)
	if q.Text == "" {
		return q, errors.New("a question needs text")
	}
	if len(q.Text) > store.MaxQuestionText {
		q.Text = q.Text[:store.MaxQuestionText]
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO desk_questions (workspace_id, turn, topic_id, text, state, created)
		VALUES (?, ?, ?, ?, ?, ?)`, q.WorkspaceID, q.Turn, q.TopicID, q.Text, store.QuestionOpen, now())
	if err != nil {
		return q, err
	}
	id, _ := res.LastInsertId()
	return s.Question(ctx, id)
}

func (s *DB) Question(ctx context.Context, id int64) (store.Question, error) {
	q, err := scanQuestion(s.db.QueryRowContext(ctx, `SELECT `+questionCols+` FROM desk_questions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return q, store.ErrNotFound
	}
	return q, err
}

func (s *DB) Questions(ctx context.Context, workspaceID int64, state string) ([]store.Question, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+questionCols+` FROM desk_questions
		WHERE (? = 0 OR workspace_id = ?) AND (? = '' OR state = ?) ORDER BY created, id`,
		workspaceID, workspaceID, state, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Question{}
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *DB) CloseQuestion(ctx context.Context, id int64, state string, answeredTurn int64) (store.Question, error) {
	if state != store.QuestionAnswered && state != store.QuestionSkipped {
		return store.Question{}, fmt.Errorf("a question closes as answered or skipped, not %q", state)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE desk_questions SET state = ?, answered_turn = ?, closed = ?
		WHERE id = ? AND state = 'open'`, state, answeredTurn, now(), id)
	if err != nil {
		return store.Question{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		q, err := s.Question(ctx, id)
		if err != nil {
			return q, err
		}
		return q, fmt.Errorf("%w: question %d is already %s", store.ErrConflict, id, q.State)
	}
	return s.Question(ctx, id)
}
