package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

const topicCols = `id, title, status, summary, source, created, updated, cleared_at, merged_into`

func scanTopic(sc interface{ Scan(...any) error }) (store.Topic, error) {
	var t store.Topic
	var created, updated, cleared string
	err := sc.Scan(&t.ID, &t.Title, &t.Status, &t.Summary, &t.Source, &created, &updated, &cleared, &t.MergedInto)
	t.Created, t.Updated = parseTime(created), parseTime(updated)
	if cleared != "" {
		c := parseTime(cleared)
		t.ClearedAt = &c
	}
	return t, err
}

func (s *DB) AddTopic(ctx context.Context, t store.Topic) (store.Topic, error) {
	t.Title = strings.TrimSpace(t.Title)
	if t.Title == "" {
		return t, errors.New("a topic needs a title")
	}
	if t.Source == "" {
		t.Source = store.TopicByPerson
	}
	n := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO topics (title, status, summary, source, created, updated)
		VALUES (?, ?, ?, ?, ?, ?)`, t.Title, store.TopicOpen, t.Summary, t.Source, n, n)
	if err != nil {
		return t, err
	}
	id, _ := res.LastInsertId()
	return s.Topic(ctx, id)
}

func (s *DB) Topic(ctx context.Context, id int64) (store.Topic, error) {
	// A merge chain is short; the bound only keeps a corrupt cycle from looping.
	for i := 0; i < 16; i++ {
		t, err := scanTopic(s.db.QueryRowContext(ctx, `SELECT `+topicCols+` FROM topics WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return t, store.ErrNotFound
		}
		if err != nil || t.MergedInto == 0 {
			return t, err
		}
		id = t.MergedInto
	}
	return store.Topic{}, fmt.Errorf("topic %d: merge chain too long", id)
}

func (s *DB) Topics(ctx context.Context) ([]store.Topic, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+topicCols+` FROM topics WHERE merged_into = 0 ORDER BY updated DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Topic{}
	for rows.Next() {
		t, err := scanTopic(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *DB) UpdateTopic(ctx context.Context, id int64, p store.TopicPatch) (store.Topic, error) {
	t, err := s.Topic(ctx, id)
	if err != nil {
		return t, err
	}
	if p.Title != nil {
		title := strings.TrimSpace(*p.Title)
		if title == "" {
			return t, errors.New("a topic needs a title")
		}
		t.Title = title
	}
	if p.Summary != nil {
		t.Summary = *p.Summary
	}
	cleared := ""
	if t.ClearedAt != nil {
		cleared = t.ClearedAt.UTC().Format(time.RFC3339Nano)
	}
	if p.Status != nil && *p.Status != t.Status {
		switch *p.Status {
		case store.TopicOpen:
			cleared = ""
		case store.TopicCleared:
			cleared = now()
		default:
			return t, fmt.Errorf("unknown status %q", *p.Status)
		}
		t.Status = *p.Status
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE topics SET title = ?, summary = ?, status = ?, cleared_at = ?, updated = ?
		WHERE id = ?`, t.Title, t.Summary, t.Status, cleared, now(), t.ID); err != nil {
		return t, err
	}
	return s.Topic(ctx, t.ID)
}

func (s *DB) MergeTopics(ctx context.Context, from, into int64) (store.Topic, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Topic{}, err
	}
	defer tx.Rollback()
	var f, i int64
	for _, c := range []struct {
		id  int64
		dst *int64
	}{{from, &f}, {into, &i}} {
		var merged int64
		err := tx.QueryRowContext(ctx, `SELECT id, merged_into FROM topics WHERE id = ?`, c.id).Scan(c.dst, &merged)
		if errors.Is(err, sql.ErrNoRows) {
			return store.Topic{}, store.ErrNotFound
		}
		if err != nil {
			return store.Topic{}, err
		}
		if merged != 0 {
			return store.Topic{}, fmt.Errorf("%w: topic %d was merged into %d", store.ErrConflict, c.id, merged)
		}
	}
	if f == i {
		return store.Topic{}, fmt.Errorf("%w: a topic cannot merge into itself", store.ErrConflict)
	}
	t := now()
	if _, err := tx.ExecContext(ctx, `UPDATE topic_links SET topic_id = ? WHERE topic_id = ?`, i, f); err != nil {
		return store.Topic{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE topics SET merged_into = ?, updated = ? WHERE id = ?`, i, t, f); err != nil {
		return store.Topic{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE topics SET updated = ? WHERE id = ?`, t, i); err != nil {
		return store.Topic{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.Topic{}, err
	}
	return s.Topic(ctx, i)
}

func (s *DB) LinkTopic(ctx context.Context, l store.TopicLink) (store.TopicLink, error) {
	if !store.ValidTopicKind(l.Kind) {
		return l, fmt.Errorf("unknown link kind %q", l.Kind)
	}
	if l.Ref == "" {
		return l, errors.New("a link needs a ref")
	}
	if l.By == "" {
		l.By = store.TopicLinkShepherd
	}
	topic, err := s.Topic(ctx, l.TopicID)
	if err != nil {
		return l, err
	}
	l.TopicID = topic.ID
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return l, err
	}
	defer tx.Rollback()
	var cur store.TopicLink
	var created string
	err = tx.QueryRowContext(ctx, `SELECT topic_id, kind, ref, by, created FROM topic_links WHERE kind = ? AND ref = ?`,
		l.Kind, l.Ref).Scan(&cur.TopicID, &cur.Kind, &cur.Ref, &cur.By, &created)
	switch {
	case err == nil:
		cur.Created = parseTime(created)
		if cur.TopicID == l.TopicID && cur.By == l.By || !store.TopicLinkWins(l.By, cur.By) {
			return cur, nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE topic_links SET topic_id = ?, by = ? WHERE kind = ? AND ref = ?`,
			l.TopicID, l.By, l.Kind, l.Ref); err != nil {
			return l, err
		}
		cur.TopicID, cur.By = l.TopicID, l.By
		l = cur
	case errors.Is(err, sql.ErrNoRows):
		n := now()
		if _, err := tx.ExecContext(ctx, `INSERT INTO topic_links (topic_id, kind, ref, by, created) VALUES (?, ?, ?, ?, ?)`,
			l.TopicID, l.Kind, l.Ref, l.By, n); err != nil {
			return l, err
		}
		l.Created = parseTime(n)
	default:
		return l, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE topics SET updated = ? WHERE id = ?`, now(), l.TopicID); err != nil {
		return l, err
	}
	return l, tx.Commit()
}

func (s *DB) TopicLinks(ctx context.Context, topicID int64) ([]store.TopicLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT topic_id, kind, ref, by, created FROM topic_links
		WHERE topic_id = ? ORDER BY created, rowid`, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.TopicLink{}
	for rows.Next() {
		var l store.TopicLink
		var created string
		if err := rows.Scan(&l.TopicID, &l.Kind, &l.Ref, &l.By, &created); err != nil {
			return nil, err
		}
		l.Created = parseTime(created)
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *DB) TopicOf(ctx context.Context, kind, ref string) (store.TopicLink, error) {
	var l store.TopicLink
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT topic_id, kind, ref, by, created FROM topic_links WHERE kind = ? AND ref = ?`,
		kind, ref).Scan(&l.TopicID, &l.Kind, &l.Ref, &l.By, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return l, store.ErrNotFound
	}
	l.Created = parseTime(created)
	return l, err
}
