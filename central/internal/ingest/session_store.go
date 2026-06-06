package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrSessionNotFound = errors.New("upload session not found")

type UploadSessionStore interface {
	Save(ctx context.Context, sess *UploadSession) error
	Load(ctx context.Context, uploadID string) (*UploadSession, error)
	LoadByIdempotencyKey(ctx context.Context, key string) (*UploadSession, error)
	Delete(ctx context.Context, sess *UploadSession) error
	DeleteExpired(ctx context.Context, cutoff time.Time) ([]*UploadSession, error)
	ReservedBytes(ctx context.Context) (int64, error)
	Lock(ctx context.Context, key string) (func(), error)
}

type sessionDB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type PGSessionStore struct {
	pool sessionDB
}

func NewPGSessionStore(pool sessionDB) *PGSessionStore {
	return &PGSessionStore{pool: pool}
}

func (s *PGSessionStore) EnsureSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS upload_sessions (
			upload_id TEXT PRIMARY KEY,
			idempotency_key TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL,
			archive_size_bytes BIGINT NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL,
			payload JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_upload_sessions_expires_at
			ON upload_sessions (expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_upload_sessions_reserved
			ON upload_sessions (status, expires_at)`,
	}
	for _, stmt := range stmts {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("upload session schema: %w", err)
		}
	}
	return nil
}

func (s *PGSessionStore) Save(ctx context.Context, sess *UploadSession) error {
	payload, err := jsonMarshal(sess)
	if err != nil {
		return err
	}
	expiresAt, err := parseSessionTime(sess.ExpiresAt)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO upload_sessions
			(upload_id, idempotency_key, status, archive_size_bytes, expires_at, payload, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, CURRENT_TIMESTAMP)
		ON CONFLICT (upload_id) DO UPDATE SET
			idempotency_key = EXCLUDED.idempotency_key,
			status = EXCLUDED.status,
			archive_size_bytes = EXCLUDED.archive_size_bytes,
			expires_at = EXCLUDED.expires_at,
			payload = EXCLUDED.payload,
			updated_at = CURRENT_TIMESTAMP`,
		sess.UploadID, sess.IdempotencyKey, sess.Status, sess.ArchiveSizeBytes, expiresAt, payload)
	return err
}

func (s *PGSessionStore) Load(ctx context.Context, uploadID string) (*UploadSession, error) {
	return s.loadByRow(s.pool.QueryRow(ctx,
		`SELECT payload FROM upload_sessions WHERE upload_id = $1`,
		uploadID))
}

func (s *PGSessionStore) LoadByIdempotencyKey(ctx context.Context, key string) (*UploadSession, error) {
	return s.loadByRow(s.pool.QueryRow(ctx,
		`SELECT payload FROM upload_sessions WHERE idempotency_key = $1`,
		key))
}

func (s *PGSessionStore) Delete(ctx context.Context, sess *UploadSession) error {
	if sess == nil || sess.UploadID == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM upload_sessions WHERE upload_id = $1`, sess.UploadID)
	return err
}

func (s *PGSessionStore) DeleteExpired(ctx context.Context, cutoff time.Time) ([]*UploadSession, error) {
	rows, err := s.pool.Query(ctx, `SELECT payload FROM upload_sessions WHERE expires_at <= $1`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var expired []*UploadSession
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		sess, err := decodeSession(raw)
		if err != nil {
			continue
		}
		expired = append(expired, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM upload_sessions WHERE expires_at <= $1`, cutoff); err != nil {
		return nil, err
	}
	return expired, nil
}

func (s *PGSessionStore) ReservedBytes(ctx context.Context) (int64, error) {
	var reserved int64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(archive_size_bytes), 0)
		FROM upload_sessions
		WHERE status = ANY($1) AND expires_at > CURRENT_TIMESTAMP`,
		[]string{"initiated", "in_progress", "uploaded", "checksum_retry_required"}).Scan(&reserved)
	return reserved, err
}

func (s *PGSessionStore) Lock(ctx context.Context, key string) (func(), error) {
	pool, ok := s.pool.(interface {
		Acquire(context.Context) (*pgxpool.Conn, error)
	})
	if !ok {
		return func() {}, nil
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	k1, k2 := advisoryLockKey(key)
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1, $2)`, k1, k2); err != nil {
		conn.Release()
		return nil, err
	}
	return func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1, $2)`, k1, k2)
		conn.Release()
	}, nil
}

func (s *PGSessionStore) loadByRow(row pgx.Row) (*UploadSession, error) {
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return decodeSession(raw)
}

type MemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*UploadSession
	keys     map[string]string
	locksMu  sync.Mutex
	locks    map[string]*sync.Mutex
}

func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{
		sessions: map[string]*UploadSession{},
		keys:     map[string]string{},
		locks:    map[string]*sync.Mutex{},
	}
}

func (s *MemorySessionStore) Save(ctx context.Context, sess *UploadSession) error {
	data, err := jsonMarshal(sess)
	if err != nil {
		return err
	}
	copy, err := decodeSession(data)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.UploadID] = copy
	s.keys[sess.IdempotencyKey] = sess.UploadID
	return nil
}

func (s *MemorySessionStore) Load(ctx context.Context, uploadID string) (*UploadSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[uploadID]
	if !ok {
		return nil, ErrSessionNotFound
	}
	copy := *sess
	return &copy, nil
}

func (s *MemorySessionStore) LoadByIdempotencyKey(ctx context.Context, key string) (*UploadSession, error) {
	s.mu.RLock()
	uploadID, ok := s.keys[key]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrSessionNotFound
	}
	return s.Load(ctx, uploadID)
}

func (s *MemorySessionStore) Delete(ctx context.Context, sess *UploadSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess == nil {
		return nil
	}
	delete(s.sessions, sess.UploadID)
	delete(s.keys, sess.IdempotencyKey)
	return nil
}

func (s *MemorySessionStore) DeleteExpired(ctx context.Context, cutoff time.Time) ([]*UploadSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var expired []*UploadSession
	for uploadID, sess := range s.sessions {
		expiresAt, err := parseSessionTime(sess.ExpiresAt)
		if err != nil || expiresAt.After(cutoff) {
			continue
		}
		copy := *sess
		expired = append(expired, &copy)
		delete(s.sessions, uploadID)
		delete(s.keys, sess.IdempotencyKey)
	}
	return expired, nil
}

func (s *MemorySessionStore) ReservedBytes(ctx context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now().UTC()
	var total int64
	for _, sess := range s.sessions {
		expiresAt, err := parseSessionTime(sess.ExpiresAt)
		if err != nil || !expiresAt.After(now) {
			continue
		}
		if activeSessionStatuses[sess.Status] {
			total += sess.ArchiveSizeBytes
		}
	}
	return total, nil
}

func (s *MemorySessionStore) Lock(ctx context.Context, key string) (func(), error) {
	s.locksMu.Lock()
	l := s.locks[key]
	if l == nil {
		l = &sync.Mutex{}
		s.locks[key] = l
	}
	s.locksMu.Unlock()

	l.Lock()
	return l.Unlock, nil
}

func decodeSession(raw []byte) (*UploadSession, error) {
	var sess UploadSession
	if err := json.Unmarshal(raw, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

func parseSessionTime(value string) (time.Time, error) {
	t, err := time.ParseInLocation(timeFormat, value, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse upload session expiry: %w", err)
	}
	return t, nil
}

func advisoryLockKey(key string) (int32, int32) {
	sum := sha256.Sum256([]byte(key))
	return int32(binary.BigEndian.Uint32(sum[0:4])), int32(binary.BigEndian.Uint32(sum[4:8]))
}
