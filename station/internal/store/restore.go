package store

import (
	"context"

	"github.com/3to1go/shared/protocol"
)

func (s *SnapshotIndex) CreateRestoreRequest(ctx context.Context, r protocol.RestoreRequest) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO restore_requests (id, scout_id, scout_instance_id, job_name, filename, status, created_at) VALUES ($1,$2,$3,$4,$5,'pending',$6)`, r.ID, r.ScoutID, r.ScoutInstanceID, r.JobName, r.Filename, r.CreatedAt)
	return err
}

func (s *SnapshotIndex) ListRestoreRequests(ctx context.Context, scoutID, instanceID string) ([]protocol.RestoreRequest, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, scout_id, scout_instance_id, job_name, filename, status, created_at FROM restore_requests WHERE scout_id=$1 AND scout_instance_id=$2 AND status IN ('pending','accepted') ORDER BY created_at DESC`, scoutID, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []protocol.RestoreRequest{}
	for rows.Next() {
		var r protocol.RestoreRequest
		if err := rows.Scan(&r.ID, &r.ScoutID, &r.ScoutInstanceID, &r.JobName, &r.Filename, &r.Status, &r.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *SnapshotIndex) DecideRestoreRequest(ctx context.Context, scoutID, instanceID, id, status string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE restore_requests SET status=$4 WHERE scout_id=$1 AND scout_instance_id=$2 AND id=$3 AND ((status='pending' AND $4 IN ('accepted','rejected')) OR (status='accepted' AND $4 IN ('accepted','completed','rejected')))`, scoutID, instanceID, id, status)
	return tag.RowsAffected() == 1, err
}
