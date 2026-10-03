package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/3to1go/shared/auth"
)

const (
	DefaultAdminUsername = auth.DefaultAdminUsername
	DefaultAdminPassword = auth.DefaultAdminPassword
	BootstrapAdminID     = auth.BootstrapAdminID
	SessionCookie        = "three_to_one_go_edge_session"
	sessionDays          = 7
)

// User represents an authenticated user.
type User = auth.User

// UserStore manages users and sessions in SQLite.
type UserStore struct {
	db *sql.DB

	cleanupMu   sync.Mutex
	lastCleanup time.Time
}

// Expired sessions are removed at most this often. Every signed-in request
// checks its session, and a write each time would queue requests behind each
// other on the single SQLite connection.
const sessionCleanupInterval = time.Minute

func NewUserStore(db *sql.DB) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS app_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			is_admin INTEGER NOT NULL DEFAULT 0,
			must_change_password INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS app_sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`)
	return err
}

func (s *UserStore) EnsureDefaultAdmin(ctx context.Context, initialPassword string) error {
	users, err := s.ListUsers(ctx)
	if err != nil {
		return err
	}
	if len(users) > 0 {
		return nil
	}
	if strings.TrimSpace(initialPassword) == "" {
		initialPassword = DefaultAdminPassword
	}
	user, err := s.CreateUser(ctx, DefaultAdminUsername, initialPassword, true)
	if err != nil {
		return err
	}
	t := true
	_, err = s.UpdateUser(ctx, user.ID, nil, nil, nil, &t)
	return err
}

func (s *UserStore) Authenticate(ctx context.Context, username, password string) (*User, error) {
	return auth.Authenticate(s, ctx, username, password)
}

func (s *UserStore) CreateSession(ctx context.Context, userID int) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	expiresAt := time.Now().UTC().Add(sessionDays * 24 * time.Hour).Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO app_sessions (token, user_id, expires_at) VALUES (?, ?, ?)`,
		token, userID, expiresAt)
	return token, err
}

func (s *UserStore) DeleteSession(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_sessions WHERE token = ?`, token)
	return err
}

func (s *UserStore) DeleteSessionsForUser(ctx context.Context, userID int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_sessions WHERE user_id = ?`, userID)
	return err
}

func (s *UserStore) UserForSession(ctx context.Context, token string) (*User, error) {
	if token == "" {
		return nil, nil
	}
	s.deleteExpiredSessions(ctx)
	row := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.password_hash, u.is_admin, u.must_change_password, u.created_at
		FROM app_sessions sess
		JOIN app_users u ON u.id = sess.user_id
		WHERE sess.token = ? AND sess.expires_at >= ?`,
		token, time.Now().UTC().Format(time.RFC3339))
	user, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	user, err = s.withDefaultPasswordChangeRequired(ctx, user)
	if err != nil {
		return nil, err
	}
	return publicUser(user), nil
}

func (s *UserStore) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, username, password_hash, is_admin, must_change_password, created_at
		FROM app_users ORDER BY lower(username)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []*User
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin, &u.MustChangePassword, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, publicUser(u))
	}
	return users, rows.Err()
}

func (s *UserStore) GetUserByID(ctx context.Context, id int) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, is_admin, must_change_password, created_at
		FROM app_users WHERE id = ?`, id)
	user, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return user, err
}

func (s *UserStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, is_admin, must_change_password, created_at
		FROM app_users WHERE username = ?`, strings.ToLower(strings.TrimSpace(username)))
	user, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return user, err
}

func (s *UserStore) CreateUser(ctx context.Context, username, password string, isAdmin bool) (*User, error) {
	normalized, err := normalizeUsername(username)
	if err != nil {
		return nil, err
	}
	if normalized == DefaultAdminUsername && !isAdmin {
		return nil, errors.New(`the "admin" username is reserved for admin users`)
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	adminInt := 0
	if isAdmin {
		adminInt = 1
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO app_users (username, password_hash, is_admin, must_change_password)
		VALUES (?, ?, ?, 0)`, normalized, hash, adminInt)
	if err != nil {
		return nil, fmt.Errorf("username already exists")
	}
	id, _ := res.LastInsertId()
	return s.GetUserByID(ctx, int(id))
}

func (s *UserStore) UpdateUser(ctx context.Context, userID int, username, password *string, isAdmin, mustChangePassword *bool) (*User, error) {
	existing, err := s.GetUserByID(ctx, userID)
	if err != nil || existing == nil {
		return nil, errors.New("user not found")
	}

	nextUsername := existing.Username
	if username != nil {
		nextUsername, err = normalizeUsername(*username)
		if err != nil {
			return nil, err
		}
	}

	nextHash := existing.PasswordHash
	if password != nil && *password != "" {
		nextHash, err = hashPassword(*password)
		if err != nil {
			return nil, err
		}
	}

	nextAdmin := existing.IsAdmin
	if isAdmin != nil {
		nextAdmin = *isAdmin
	}
	nextMustChange := existing.MustChangePassword
	if mustChangePassword != nil {
		nextMustChange = *mustChangePassword
	}

	if verifyPassword(DefaultAdminPassword, nextHash) {
		nextMustChange = true
	}
	if existing.ID == BootstrapAdminID {
		nextAdmin = true
	}

	if existing.Username == DefaultAdminUsername && nextUsername != DefaultAdminUsername {
		return nil, errors.New(`the "admin" user cannot be renamed`)
	}
	if nextUsername == DefaultAdminUsername && !nextAdmin {
		return nil, errors.New(`the "admin" username is reserved for admin users`)
	}

	if !nextAdmin {
		admins, err := s.ListUsers(ctx)
		if err != nil {
			return nil, err
		}
		count := 0
		for _, u := range admins {
			if u.IsAdmin {
				count++
			}
		}
		if count == 1 {
			for _, u := range admins {
				if u.IsAdmin && u.ID == userID {
					return nil, errors.New("at least one admin is required")
				}
			}
		}
	}

	adminInt := 0
	if nextAdmin {
		adminInt = 1
	}
	mustInt := 0
	if nextMustChange {
		mustInt = 1
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE app_users
		SET username = ?, password_hash = ?, is_admin = ?, must_change_password = ?
		WHERE id = ?`, nextUsername, nextHash, adminInt, mustInt, userID)
	if err != nil {
		return nil, fmt.Errorf("username already exists")
	}
	return s.GetUserByID(ctx, userID)
}

func (s *UserStore) DeleteUser(ctx context.Context, userID int) error {
	existing, err := s.GetUserByID(ctx, userID)
	if err != nil || existing == nil {
		return errors.New("user not found")
	}
	if existing.ID == BootstrapAdminID {
		return errors.New("the bootstrap admin user cannot be removed")
	}
	admins, _ := s.ListUsers(ctx)
	adminCount := 0
	for _, u := range admins {
		if u.IsAdmin {
			adminCount++
		}
	}
	if existing.IsAdmin && adminCount == 1 {
		return errors.New("at least one admin is required")
	}
	s.db.ExecContext(ctx, `DELETE FROM app_sessions WHERE user_id = ?`, userID)
	_, err = s.db.ExecContext(ctx, `DELETE FROM app_users WHERE id = ?`, userID)
	return err
}

func (s *UserStore) ChangePassword(ctx context.Context, userID int, currentPassword, newPassword string) (*User, error) {
	return auth.ChangePassword(s, ctx, userID, currentPassword, newPassword)
}

// Session lookups already ignore expired rows; this only keeps the table small.
func (s *UserStore) deleteExpiredSessions(ctx context.Context) {
	s.cleanupMu.Lock()
	if time.Since(s.lastCleanup) < sessionCleanupInterval {
		s.cleanupMu.Unlock()
		return
	}
	s.lastCleanup = time.Now()
	s.cleanupMu.Unlock()
	s.db.ExecContext(ctx, `DELETE FROM app_sessions WHERE expires_at < ?`,
		time.Now().UTC().Format(time.RFC3339))
}

func (s *UserStore) withDefaultPasswordChangeRequired(ctx context.Context, user *User) (*User, error) {
	return auth.WithDefaultPasswordChangeRequired(s, ctx, user)
}

func scanUser(row *sql.Row) (*User, error) {
	u := &User{}
	var isAdminInt, mustChangeInt int
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &isAdminInt, &mustChangeInt, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.IsAdmin = isAdminInt != 0
	u.MustChangePassword = mustChangeInt != 0
	return u, nil
}
