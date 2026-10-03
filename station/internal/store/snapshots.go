package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type SnapshotEntry struct {
	StoredAs    string  `json:"stored_as"`
	ArchiveSHA  string  `json:"archive_sha256"`
	Fingerprint string  `json:"fingerprint"`
	Timestamp   string  `json:"timestamp"`
	SizeBytes   int64   `json:"size_bytes"`
	Mtime       float64 `json:"mtime"`
	// Unusual explains why this archive's size differed sharply from the job's history.
	Unusual string `json:"unusual,omitempty"`
}

type SnapshotJob struct {
	JobName       string         `json:"job_name"`
	SnapshotCount int            `json:"snapshot_count"`
	Snapshots     []SnapshotMini `json:"snapshots"`
}

type SnapshotMini struct {
	Name      string  `json:"name"`
	SizeBytes int64   `json:"size_bytes"`
	Mtime     float64 `json:"mtime"`
	Unusual   string  `json:"unusual,omitempty"`
}

type NamespaceEntry struct {
	ScoutID         string        `json:"scout_id"`
	ScoutInstanceID string        `json:"scout_instance_id"`
	Jobs            []SnapshotJob `json:"jobs"`
}

type ScoutRegistration struct {
	ScoutID                  string  `json:"scout_id"`
	ScoutInstanceID          string  `json:"scout_instance_id"`
	EncryptionKeyFingerprint *string `json:"encryption_key_fingerprint"`
	AdvertisedURL            *string `json:"advertised_url"`
	FirstSeenAt              string  `json:"first_seen_at"`
	LastSeenAt               string  `json:"last_seen_at"`
	CredentialHash           *string `json:"credential_hash"`
	LastUploadTLS            *bool   `json:"last_upload_tls"`
}

type SnapshotIndex struct {
	pool dbPool
}

func NewSnapshotIndex(pool dbPool) *SnapshotIndex {
	return &SnapshotIndex{pool: pool}
}

func (s *SnapshotIndex) EnsureSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS snapshot_index (
			scout_id TEXT NOT NULL,
			scout_instance_id TEXT NOT NULL,
			job_name TEXT NOT NULL,
			stored_as TEXT NOT NULL,
			archive_sha256 TEXT NOT NULL,
			fingerprint TEXT,
			snapshot_timestamp TEXT,
			size_bytes BIGINT NOT NULL DEFAULT 0,
			mtime DOUBLE PRECISION NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS scout_registration (
			scout_id TEXT NOT NULL,
			scout_instance_id TEXT NOT NULL,
			encryption_key_fingerprint TEXT,
			advertised_url TEXT,
			first_seen_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			credential_hash TEXT
		)`,
		`ALTER TABLE scout_registration ADD COLUMN IF NOT EXISTS credential_hash TEXT`,
		`ALTER TABLE scout_registration ADD COLUMN IF NOT EXISTS last_upload_tls BOOLEAN`,
		`ALTER TABLE snapshot_index ADD COLUMN IF NOT EXISTS unusual TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS archive_size_history (
			id BIGSERIAL PRIMARY KEY,
			scout_id TEXT NOT NULL,
			scout_instance_id TEXT NOT NULL,
			job_name TEXT NOT NULL,
			size_bytes BIGINT NOT NULL,
			recorded_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_archive_size_history_namespace
			ON archive_size_history (scout_id, scout_instance_id, job_name, id DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_snapshot_index_namespace_pk
			ON snapshot_index (scout_id, scout_instance_id, job_name, stored_as)`,
		`CREATE INDEX IF NOT EXISTS idx_snapshot_index_namespace_sha
			ON snapshot_index (scout_id, scout_instance_id, job_name, archive_sha256)`,
		`CREATE INDEX IF NOT EXISTS idx_snapshot_index_namespace_mtime
			ON snapshot_index (scout_id, scout_instance_id, job_name, mtime DESC, stored_as DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_scout_registration_instance
			ON scout_registration (scout_id, scout_instance_id)`,
		`CREATE INDEX IF NOT EXISTS idx_scout_registration_credential_hash
			ON scout_registration (credential_hash)`,
	}
	for _, stmt := range stmts {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("schema: %w", err)
		}
	}
	return nil
}

func (s *SnapshotIndex) FindDuplicate(ctx context.Context, namespace, archiveSHA string) (*SnapshotEntry, error) {
	scoutID, instID, jobName, err := splitNamespace(namespace)
	if err != nil {
		return nil, err
	}
	var e SnapshotEntry
	err = s.pool.QueryRow(ctx, `
		SELECT stored_as, archive_sha256, fingerprint, snapshot_timestamp, size_bytes, mtime
		FROM snapshot_index
		WHERE scout_id = $1 AND scout_instance_id = $2 AND job_name = $3 AND archive_sha256 = $4
		ORDER BY updated_at DESC LIMIT 1`,
		scoutID, instID, jobName, archiveSHA).
		Scan(&e.StoredAs, &e.ArchiveSHA, &e.Fingerprint, &e.Timestamp, &e.SizeBytes, &e.Mtime)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

func (s *SnapshotIndex) UpsertSnapshot(ctx context.Context, namespace string, e SnapshotEntry) error {
	scoutID, instID, jobName, err := splitNamespace(namespace)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO snapshot_index
			(scout_id, scout_instance_id, job_name, stored_as, archive_sha256, fingerprint, snapshot_timestamp, size_bytes, mtime, unusual)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (scout_id, scout_instance_id, job_name, stored_as)
		DO UPDATE SET
			archive_sha256 = EXCLUDED.archive_sha256,
			fingerprint = EXCLUDED.fingerprint,
			snapshot_timestamp = EXCLUDED.snapshot_timestamp,
			size_bytes = EXCLUDED.size_bytes,
			mtime = EXCLUDED.mtime,
			unusual = EXCLUDED.unusual,
			updated_at = CURRENT_TIMESTAMP`,
		scoutID, instID, jobName, e.StoredAs, e.ArchiveSHA, e.Fingerprint, e.Timestamp, e.SizeBytes, e.Mtime, e.Unusual)
	return err
}

type StorageFile struct {
	Filename  string
	SizeBytes int64
	Mtime     float64
}

func (s *SnapshotIndex) ReconcileNamespace(ctx context.Context, namespace string, files []StorageFile) error {
	scoutID, instID, jobName, err := splitNamespace(namespace)
	if err != nil {
		return err
	}
	if len(files) > 0 {
		filenames := make([]string, len(files))
		for i, f := range files {
			filenames[i] = f.Filename
		}
		_, err = s.pool.Exec(ctx, `
			DELETE FROM snapshot_index
			WHERE scout_id = $1 AND scout_instance_id = $2 AND job_name = $3
			AND NOT (stored_as = ANY($4))`,
			scoutID, instID, jobName, filenames)
	} else {
		_, err = s.pool.Exec(ctx, `
			DELETE FROM snapshot_index
			WHERE scout_id = $1 AND scout_instance_id = $2 AND job_name = $3`,
			scoutID, instID, jobName)
	}
	return err
}

func (s *SnapshotIndex) ListNamespaces(ctx context.Context) ([]NamespaceEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT scout_id, scout_instance_id, job_name, stored_as, size_bytes, mtime, unusual
		FROM snapshot_index
		ORDER BY lower(scout_id), lower(scout_instance_id), lower(job_name), mtime DESC, stored_as DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type instanceKey struct{ scoutID, instID string }
	var instances []NamespaceEntry
	instMap := map[instanceKey]*NamespaceEntry{}
	jobMap := map[instanceKey]map[string]*SnapshotJob{}

	for rows.Next() {
		var scoutID, instID, jobName, storedAs string
		var sizeBytes int64
		var mtime float64
		var unusual string
		if err := rows.Scan(&scoutID, &instID, &jobName, &storedAs, &sizeBytes, &mtime, &unusual); err != nil {
			return nil, err
		}
		key := instanceKey{scoutID, instID}
		inst := instMap[key]
		if inst == nil {
			instances = append(instances, NamespaceEntry{ScoutID: scoutID, ScoutInstanceID: instID})
			inst = &instances[len(instances)-1]
			instMap[key] = inst
			jobMap[key] = map[string]*SnapshotJob{}
		}
		jm := jobMap[key]
		job := jm[jobName]
		if job == nil {
			inst.Jobs = append(inst.Jobs, SnapshotJob{JobName: jobName})
			job = &inst.Jobs[len(inst.Jobs)-1]
			jm[jobName] = job
		}
		job.SnapshotCount++
		job.Snapshots = append(job.Snapshots, SnapshotMini{Name: storedAs, SizeBytes: sizeBytes, Mtime: mtime, Unusual: unusual})
	}
	return instances, rows.Err()
}

// RecentArchiveSizes returns up to limit of a job's most recent archive sizes,
// oldest first. Retention keeps only a few snapshots, so this separate history
// is what tells Station how big the job's archives usually are.
func (s *SnapshotIndex) RecentArchiveSizes(ctx context.Context, namespace string, limit int) ([]int64, error) {
	scoutID, instID, jobName, err := splitNamespace(namespace)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT size_bytes FROM (
			SELECT id, size_bytes FROM archive_size_history
			WHERE scout_id = $1 AND scout_instance_id = $2 AND job_name = $3
			ORDER BY id DESC LIMIT $4
		) recent ORDER BY id`,
		scoutID, instID, jobName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sizes []int64
	for rows.Next() {
		var size int64
		if err := rows.Scan(&size); err != nil {
			return nil, err
		}
		sizes = append(sizes, size)
	}
	return sizes, rows.Err()
}

// RecordArchiveSize adds an archive size to the job's history and keeps only
// the most recent keep entries.
func (s *SnapshotIndex) RecordArchiveSize(ctx context.Context, namespace string, size int64, keep int) error {
	scoutID, instID, jobName, err := splitNamespace(namespace)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO archive_size_history (scout_id, scout_instance_id, job_name, size_bytes)
		VALUES ($1, $2, $3, $4)`, scoutID, instID, jobName, size); err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		DELETE FROM archive_size_history
		WHERE scout_id = $1 AND scout_instance_id = $2 AND job_name = $3
		AND id NOT IN (
			SELECT id FROM archive_size_history
			WHERE scout_id = $1 AND scout_instance_id = $2 AND job_name = $3
			ORDER BY id DESC LIMIT $4
		)`, scoutID, instID, jobName, keep)
	return err
}

func (s *SnapshotIndex) GetScoutRegistration(ctx context.Context, scoutID, instID string) (*ScoutRegistration, error) {
	var r ScoutRegistration
	err := s.pool.QueryRow(ctx, `
		SELECT scout_id, scout_instance_id, encryption_key_fingerprint, advertised_url,
		       first_seen_at, last_seen_at, credential_hash, last_upload_tls
		FROM scout_registration
		WHERE scout_id = $1 AND scout_instance_id = $2`,
		scoutID, instID).
		Scan(&r.ScoutID, &r.ScoutInstanceID, &r.EncryptionKeyFingerprint, &r.AdvertisedURL,
			&r.FirstSeenAt, &r.LastSeenAt, &r.CredentialHash, &r.LastUploadTLS)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

func (s *SnapshotIndex) UpsertScoutRegistration(ctx context.Context, r *ScoutRegistration) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO scout_registration
			(scout_id, scout_instance_id, encryption_key_fingerprint, advertised_url,
			 first_seen_at, last_seen_at, credential_hash, last_upload_tls)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (scout_id, scout_instance_id) DO UPDATE SET
			encryption_key_fingerprint = EXCLUDED.encryption_key_fingerprint,
			advertised_url = EXCLUDED.advertised_url,
			first_seen_at = EXCLUDED.first_seen_at,
			last_seen_at = EXCLUDED.last_seen_at,
			credential_hash = EXCLUDED.credential_hash,
			last_upload_tls = EXCLUDED.last_upload_tls`,
		r.ScoutID, r.ScoutInstanceID, r.EncryptionKeyFingerprint, r.AdvertisedURL,
		r.FirstSeenAt, r.LastSeenAt, r.CredentialHash, r.LastUploadTLS)
	return err
}

func (s *SnapshotIndex) DeleteScoutRegistration(ctx context.Context, scoutID, instID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM scout_registration WHERE scout_id = $1 AND scout_instance_id = $2`,
		scoutID, instID)
	return err
}

func (s *SnapshotIndex) DeleteInstanceEntries(ctx context.Context, scoutID, instID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM snapshot_index WHERE scout_id = $1 AND scout_instance_id = $2`,
		scoutID, instID)
	return err
}

// DeleteArchiveSizes forgets a Scout instance's archive size history, for
// every job, when the instance is deleted.
func (s *SnapshotIndex) DeleteArchiveSizes(ctx context.Context, scoutID, instID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM archive_size_history WHERE scout_id = $1 AND scout_instance_id = $2`,
		scoutID, instID)
	return err
}

func (s *SnapshotIndex) ListScoutRegistrations(ctx context.Context, scoutIDFilter *string) ([]ScoutRegistration, error) {
	var (
		query string
		args  []any
	)
	if scoutIDFilter != nil {
		query = `SELECT scout_id, scout_instance_id, encryption_key_fingerprint, advertised_url,
			first_seen_at, last_seen_at, credential_hash, last_upload_tls
			FROM scout_registration WHERE scout_id = $1
			ORDER BY lower(scout_instance_id)`
		args = []any{*scoutIDFilter}
	} else {
		query = `SELECT scout_id, scout_instance_id, encryption_key_fingerprint, advertised_url,
			first_seen_at, last_seen_at, credential_hash, last_upload_tls
			FROM scout_registration
			ORDER BY lower(scout_id), lower(scout_instance_id)`
	}

	pgRows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer pgRows.Close()

	var result []ScoutRegistration
	for pgRows.Next() {
		var r ScoutRegistration
		if err := pgRows.Scan(&r.ScoutID, &r.ScoutInstanceID, &r.EncryptionKeyFingerprint, &r.AdvertisedURL,
			&r.FirstSeenAt, &r.LastSeenAt, &r.CredentialHash, &r.LastUploadTLS); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, pgRows.Err()
}

func (s *SnapshotIndex) ListNamespaceEntries(ctx context.Context, namespace string) ([]SnapshotEntry, error) {
	scoutID, instID, jobName, err := splitNamespace(namespace)
	if err != nil {
		return nil, err
	}
	pgRows, err := s.pool.Query(ctx, `
		SELECT stored_as, archive_sha256, fingerprint, snapshot_timestamp, size_bytes, mtime
		FROM snapshot_index
		WHERE scout_id = $1 AND scout_instance_id = $2 AND job_name = $3
		ORDER BY mtime DESC, stored_as DESC`,
		scoutID, instID, jobName)
	if err != nil {
		return nil, err
	}
	defer pgRows.Close()
	var entries []SnapshotEntry
	for pgRows.Next() {
		var e SnapshotEntry
		if err := pgRows.Scan(&e.StoredAs, &e.ArchiveSHA, &e.Fingerprint, &e.Timestamp, &e.SizeBytes, &e.Mtime); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, pgRows.Err()
}

func (s *SnapshotIndex) HasNamespaceEntries(ctx context.Context, scoutID, instID string) (bool, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM snapshot_index
		WHERE scout_id = $1 AND scout_instance_id = $2`,
		scoutID, instID).Scan(&count)
	return count > 0, err
}

func UTCNow() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

func splitNamespace(namespace string) (string, string, string, error) {
	parts := strings.SplitN(namespace, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", fmt.Errorf("invalid namespace: %q", namespace)
	}
	return parts[0], parts[1], parts[2], nil
}
