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
}

type NamespaceEntry struct {
	EdgeID         string        `json:"edge_id"`
	EdgeInstanceID string        `json:"edge_instance_id"`
	Jobs           []SnapshotJob `json:"jobs"`
}

type EdgeRegistration struct {
	EdgeID                   string  `json:"edge_id"`
	EdgeInstanceID           string  `json:"edge_instance_id"`
	EncryptionKeyFingerprint *string `json:"encryption_key_fingerprint"`
	AdvertisedURL            *string `json:"advertised_url"`
	FirstSeenAt              string  `json:"first_seen_at"`
	LastSeenAt               string  `json:"last_seen_at"`
	CredentialHash           *string `json:"credential_hash"`
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
			edge_id TEXT NOT NULL,
			edge_instance_id TEXT NOT NULL,
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
		`CREATE TABLE IF NOT EXISTS edge_registration (
			edge_id TEXT NOT NULL,
			edge_instance_id TEXT NOT NULL,
			encryption_key_fingerprint TEXT,
			advertised_url TEXT,
			first_seen_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			credential_hash TEXT
		)`,
		`ALTER TABLE edge_registration ADD COLUMN IF NOT EXISTS credential_hash TEXT`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_snapshot_index_namespace_pk
			ON snapshot_index (edge_id, edge_instance_id, job_name, stored_as)`,
		`CREATE INDEX IF NOT EXISTS idx_snapshot_index_namespace_sha
			ON snapshot_index (edge_id, edge_instance_id, job_name, archive_sha256)`,
		`CREATE INDEX IF NOT EXISTS idx_snapshot_index_namespace_mtime
			ON snapshot_index (edge_id, edge_instance_id, job_name, mtime DESC, stored_as DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_edge_registration_instance
			ON edge_registration (edge_id, edge_instance_id)`,
		`CREATE INDEX IF NOT EXISTS idx_edge_registration_credential_hash
			ON edge_registration (credential_hash)`,
	}
	for _, stmt := range stmts {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("schema: %w", err)
		}
	}
	return nil
}

func (s *SnapshotIndex) FindDuplicate(ctx context.Context, namespace, archiveSHA string) (*SnapshotEntry, error) {
	edgeID, instID, jobName, err := splitNamespace(namespace)
	if err != nil {
		return nil, err
	}
	var e SnapshotEntry
	err = s.pool.QueryRow(ctx, `
		SELECT stored_as, archive_sha256, fingerprint, snapshot_timestamp, size_bytes, mtime
		FROM snapshot_index
		WHERE edge_id = $1 AND edge_instance_id = $2 AND job_name = $3 AND archive_sha256 = $4
		ORDER BY updated_at DESC LIMIT 1`,
		edgeID, instID, jobName, archiveSHA).
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
	edgeID, instID, jobName, err := splitNamespace(namespace)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO snapshot_index
			(edge_id, edge_instance_id, job_name, stored_as, archive_sha256, fingerprint, snapshot_timestamp, size_bytes, mtime)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (edge_id, edge_instance_id, job_name, stored_as)
		DO UPDATE SET
			archive_sha256 = EXCLUDED.archive_sha256,
			fingerprint = EXCLUDED.fingerprint,
			snapshot_timestamp = EXCLUDED.snapshot_timestamp,
			size_bytes = EXCLUDED.size_bytes,
			mtime = EXCLUDED.mtime,
			updated_at = CURRENT_TIMESTAMP`,
		edgeID, instID, jobName, e.StoredAs, e.ArchiveSHA, e.Fingerprint, e.Timestamp, e.SizeBytes, e.Mtime)
	return err
}

type StorageFile struct {
	Filename  string
	SizeBytes int64
	Mtime     float64
}

func (s *SnapshotIndex) ReconcileNamespace(ctx context.Context, namespace string, files []StorageFile) error {
	edgeID, instID, jobName, err := splitNamespace(namespace)
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
			WHERE edge_id = $1 AND edge_instance_id = $2 AND job_name = $3
			AND NOT (stored_as = ANY($4))`,
			edgeID, instID, jobName, filenames)
	} else {
		_, err = s.pool.Exec(ctx, `
			DELETE FROM snapshot_index
			WHERE edge_id = $1 AND edge_instance_id = $2 AND job_name = $3`,
			edgeID, instID, jobName)
	}
	return err
}

func (s *SnapshotIndex) ListNamespaces(ctx context.Context) ([]NamespaceEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT edge_id, edge_instance_id, job_name, stored_as, size_bytes, mtime
		FROM snapshot_index
		ORDER BY lower(edge_id), lower(edge_instance_id), lower(job_name), mtime DESC, stored_as DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type instanceKey struct{ edgeID, instID string }
	var instances []NamespaceEntry
	instMap := map[instanceKey]*NamespaceEntry{}
	jobMap := map[instanceKey]map[string]*SnapshotJob{}

	for rows.Next() {
		var edgeID, instID, jobName, storedAs string
		var sizeBytes int64
		var mtime float64
		if err := rows.Scan(&edgeID, &instID, &jobName, &storedAs, &sizeBytes, &mtime); err != nil {
			return nil, err
		}
		key := instanceKey{edgeID, instID}
		inst := instMap[key]
		if inst == nil {
			instances = append(instances, NamespaceEntry{EdgeID: edgeID, EdgeInstanceID: instID})
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
		job.Snapshots = append(job.Snapshots, SnapshotMini{Name: storedAs, SizeBytes: sizeBytes, Mtime: mtime})
	}
	return instances, rows.Err()
}

func (s *SnapshotIndex) GetEdgeRegistration(ctx context.Context, edgeID, instID string) (*EdgeRegistration, error) {
	var r EdgeRegistration
	err := s.pool.QueryRow(ctx, `
		SELECT edge_id, edge_instance_id, encryption_key_fingerprint, advertised_url,
		       first_seen_at, last_seen_at, credential_hash
		FROM edge_registration
		WHERE edge_id = $1 AND edge_instance_id = $2`,
		edgeID, instID).
		Scan(&r.EdgeID, &r.EdgeInstanceID, &r.EncryptionKeyFingerprint, &r.AdvertisedURL,
			&r.FirstSeenAt, &r.LastSeenAt, &r.CredentialHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

func (s *SnapshotIndex) UpsertEdgeRegistration(ctx context.Context, r *EdgeRegistration) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO edge_registration
			(edge_id, edge_instance_id, encryption_key_fingerprint, advertised_url,
			 first_seen_at, last_seen_at, credential_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (edge_id, edge_instance_id) DO UPDATE SET
			encryption_key_fingerprint = EXCLUDED.encryption_key_fingerprint,
			advertised_url = EXCLUDED.advertised_url,
			first_seen_at = EXCLUDED.first_seen_at,
			last_seen_at = EXCLUDED.last_seen_at,
			credential_hash = EXCLUDED.credential_hash`,
		r.EdgeID, r.EdgeInstanceID, r.EncryptionKeyFingerprint, r.AdvertisedURL,
		r.FirstSeenAt, r.LastSeenAt, r.CredentialHash)
	return err
}

func (s *SnapshotIndex) DeleteEdgeRegistration(ctx context.Context, edgeID, instID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM edge_registration WHERE edge_id = $1 AND edge_instance_id = $2`,
		edgeID, instID)
	return err
}

func (s *SnapshotIndex) ListEdgeRegistrations(ctx context.Context, edgeIDFilter *string) ([]EdgeRegistration, error) {
	var (
		query string
		args  []interface{}
	)
	if edgeIDFilter != nil {
		query = `SELECT edge_id, edge_instance_id, encryption_key_fingerprint, advertised_url,
			first_seen_at, last_seen_at, credential_hash
			FROM edge_registration WHERE edge_id = $1
			ORDER BY lower(edge_instance_id)`
		args = []interface{}{*edgeIDFilter}
	} else {
		query = `SELECT edge_id, edge_instance_id, encryption_key_fingerprint, advertised_url,
			first_seen_at, last_seen_at, credential_hash
			FROM edge_registration
			ORDER BY lower(edge_id), lower(edge_instance_id)`
	}

	pgRows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer pgRows.Close()

	var result []EdgeRegistration
	for pgRows.Next() {
		var r EdgeRegistration
		if err := pgRows.Scan(&r.EdgeID, &r.EdgeInstanceID, &r.EncryptionKeyFingerprint, &r.AdvertisedURL,
			&r.FirstSeenAt, &r.LastSeenAt, &r.CredentialHash); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, pgRows.Err()
}

func (s *SnapshotIndex) ListNamespaceEntries(ctx context.Context, namespace string) ([]SnapshotEntry, error) {
	edgeID, instID, jobName, err := splitNamespace(namespace)
	if err != nil {
		return nil, err
	}
	pgRows, err := s.pool.Query(ctx, `
		SELECT stored_as, archive_sha256, fingerprint, snapshot_timestamp, size_bytes, mtime
		FROM snapshot_index
		WHERE edge_id = $1 AND edge_instance_id = $2 AND job_name = $3
		ORDER BY mtime DESC, stored_as DESC`,
		edgeID, instID, jobName)
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

func (s *SnapshotIndex) HasNamespaceEntries(ctx context.Context, edgeID, instID string) (bool, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM snapshot_index
		WHERE edge_id = $1 AND edge_instance_id = $2`,
		edgeID, instID).Scan(&count)
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
