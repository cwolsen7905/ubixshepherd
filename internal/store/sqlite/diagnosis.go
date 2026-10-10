package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

const diagCols = `id, run_id, kind, summary, evidence, command, exit_code, files, log, reset_at, fixes, created`

func scanDiagnosis(sc interface{ Scan(...any) error }) (store.Diagnosis, error) {
	var d store.Diagnosis
	var code *int
	var files, reset, fixes, created string
	if err := sc.Scan(&d.ID, &d.RunID, &d.Kind, &d.Summary, &d.Evidence, &d.Command, &code, &files, &d.Log, &reset, &fixes, &created); err != nil {
		return d, err
	}
	d.ExitCode = code
	json.Unmarshal([]byte(files), &d.Files)
	json.Unmarshal([]byte(fixes), &d.Fixes)
	if reset != "" {
		t := parseTime(reset)
		d.ResetAt = &t
	}
	d.Created = parseTime(created)
	return d, nil
}

// AddDiagnosis records a diagnosis of a run.
func (s *DB) AddDiagnosis(ctx context.Context, d store.Diagnosis) (store.Diagnosis, error) {
	files, _ := json.Marshal(append([]string{}, d.Files...))
	fixes, _ := json.Marshal(append([]store.Fix{}, d.Fixes...))
	reset := ""
	if d.ResetAt != nil {
		reset = d.ResetAt.UTC().Format(time.RFC3339Nano)
	}
	d.Created = parseTime(now())
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO run_diagnoses (run_id, kind, summary, evidence, command, exit_code, files, log, reset_at, fixes, created)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.RunID, d.Kind, d.Summary, d.Evidence, d.Command, d.ExitCode, string(files), d.Log, reset, string(fixes), d.Created.Format(time.RFC3339Nano))
	if err != nil {
		return d, err
	}
	d.ID, _ = res.LastInsertId()
	return d, nil
}

// Diagnoses returns a run's diagnoses, newest first.
func (s *DB) Diagnoses(ctx context.Context, runID int64) ([]store.Diagnosis, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+diagCols+` FROM run_diagnoses WHERE run_id = ? ORDER BY id DESC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Diagnosis
	for rows.Next() {
		d, err := scanDiagnosis(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// LatestDiagnoses returns the newest diagnosis of each run that has one.
func (s *DB) LatestDiagnoses(ctx context.Context, runIDs ...int64) (map[int64]store.Diagnosis, error) {
	out := map[int64]store.Diagnosis{}
	if len(runIDs) == 0 {
		return out, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(runIDs)), ",")
	args := make([]any, len(runIDs))
	for i, id := range runIDs {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+diagCols+` FROM run_diagnoses
		WHERE id IN (SELECT MAX(id) FROM run_diagnoses WHERE run_id IN (`+marks+`) GROUP BY run_id)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		d, err := scanDiagnosis(rows)
		if err != nil {
			return nil, err
		}
		out[d.RunID] = d
	}
	return out, rows.Err()
}
