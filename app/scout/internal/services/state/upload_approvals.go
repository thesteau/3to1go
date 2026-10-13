package state

import (
	"context"
	"database/sql"
	"errors"
)

// UploadApproval remembers an operator's large-upload approval for one job and destination.
type UploadApproval struct {
	ID         string `json:"id"`
	JobPath    string `json:"job_path"`
	JobName    string `json:"job_name"`
	ScoutID    string `json:"scout_id"`
	StationURL string `json:"station_url"`
	ApprovedAt string `json:"approved_at"`
}

type UploadApprovalStore struct{ db *sql.DB }

func NewUploadApprovalStore(db *sql.DB) *UploadApprovalStore {
	return &UploadApprovalStore{db: db}
}

func (s *UploadApprovalStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS upload_size_approvals (
		id TEXT PRIMARY KEY,
		job_path TEXT NOT NULL,
		job_name TEXT NOT NULL,
		scout_id TEXT NOT NULL,
		station_url TEXT NOT NULL,
		approved_at TEXT NOT NULL
	)`)
	return err
}

func (s *UploadApprovalStore) Get(ctx context.Context, id string) (*UploadApproval, error) {
	var a UploadApproval
	err := s.db.QueryRowContext(ctx, `SELECT id, job_path, job_name, scout_id, station_url, approved_at FROM upload_size_approvals WHERE id = ?`, id).
		Scan(&a.ID, &a.JobPath, &a.JobName, &a.ScoutID, &a.StationURL, &a.ApprovedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *UploadApprovalStore) Save(ctx context.Context, a UploadApproval) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO upload_size_approvals (id, job_path, job_name, scout_id, station_url, approved_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET approved_at = excluded.approved_at`, a.ID, a.JobPath, a.JobName, a.ScoutID, a.StationURL, a.ApprovedAt)
	return err
}

func (s *UploadApprovalStore) List(ctx context.Context) ([]UploadApproval, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, job_path, job_name, scout_id, station_url, approved_at FROM upload_size_approvals ORDER BY job_name, job_path, station_url, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	approvals := []UploadApproval{}
	for rows.Next() {
		var a UploadApproval
		if err := rows.Scan(&a.ID, &a.JobPath, &a.JobName, &a.ScoutID, &a.StationURL, &a.ApprovedAt); err != nil {
			return nil, err
		}
		approvals = append(approvals, a)
	}
	return approvals, rows.Err()
}

func (s *UploadApprovalStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM upload_size_approvals WHERE id = ?`, id)
	return err
}
