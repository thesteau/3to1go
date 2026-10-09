package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/3to1go/scout/internal/config"
	"github.com/3to1go/scout/internal/testutil"
	"github.com/DATA-DOG/go-sqlmock"
)

func testUserStore(t *testing.T) (*UserStore, sqlmock.Sqlmock) {
	t.Helper()
	db, mock := testutil.MockDB(t)
	return NewUserStore(db), mock
}

func userFixture(t *testing.T, id int, name, password string, admin, mustChange bool) *User {
	t.Helper()
	hash, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	return &User{ID: id, Username: name, PasswordHash: hash, IsAdmin: admin, MustChangePassword: mustChange, CreatedAt: "2026-10-02T00:00:00Z"}
}

func userRows(users ...*User) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"id", "username", "password_hash", "is_admin", "must_change_password", "created_at"})
	for _, u := range users {
		admin, change := 0, 0
		if u.IsAdmin {
			admin = 1
		}
		if u.MustChangePassword {
			change = 1
		}
		rows.AddRow(u.ID, u.Username, u.PasswordHash, admin, change, u.CreatedAt)
	}
	return rows
}

func expectGet(mock sqlmock.Sqlmock, id int, users ...*User) {
	mock.ExpectQuery("FROM app_users WHERE id =").WithArgs(id).WillReturnRows(userRows(users...))
}

func expectList(mock sqlmock.Sqlmock, users ...*User) {
	mock.ExpectQuery("FROM app_users ORDER BY").WillReturnRows(userRows(users...)).RowsWillBeClosed()
}

type passwordHashArg string

func (p passwordHashArg) Match(value driver.Value) bool {
	hash, ok := value.(string)
	return ok && hash != string(p) && verifyPassword(string(p), hash)
}

type expiryArg struct{}

func (expiryArg) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if !ok {
		return false
	}
	expiry, err := time.Parse(time.RFC3339, raw)
	return err == nil && expiry.After(time.Now().UTC().Add(6*24*time.Hour)) && expiry.Before(time.Now().UTC().Add(8*24*time.Hour))
}

func TestOpenCreatesParentDirectory(t *testing.T) {
	db, _ := testutil.MockDB(t)
	path := filepath.Join(t.TempDir(), "nested", "scout.db")
	got, err := openWith(path, func(name, dsn string) (*sql.DB, error) {
		if name != "sqlite" || dsn != path+"?_foreign_keys=on&_journal_mode=WAL" {
			t.Fatalf("open: %s %s", name, dsn)
		}
		return db, nil
	})
	if err != nil || got != db {
		t.Fatalf("Open: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mock created database file: %v", err)
	}
}

func TestOpenErrors(t *testing.T) {
	dbErr := errors.New("driver unavailable")
	if _, err := openWith(filepath.Join(t.TempDir(), "scout.db"), func(string, string) (*sql.DB, error) { return nil, dbErr }); !errors.Is(err, dbErr) {
		t.Fatalf("Open: %v", err)
	}
	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openWith(filepath.Join(parent, "scout.db"), func(string, string) (*sql.DB, error) { t.Fatal("opened driver after mkdir failed"); return nil, nil }); err == nil {
		t.Fatal("expected directory error")
	}
}

func TestSettingsStoreRoundTrip(t *testing.T) {
	db, mock := testutil.MockDB(t)
	s := NewSettingsStore(db)
	ctx := context.Background()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS app_settings").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT payload FROM app_settings").WillReturnRows(sqlmock.NewRows([]string{"payload"}))
	if got, err := s.Load(ctx); err != nil || got != nil {
		t.Fatalf("empty: %+v, %v", got, err)
	}
	payload := &config.SettingsPayload{ScoutID: "scout-1", ScanRoot: "/data", StationURL: "https://station.example", CronSchedule: "*/30 * * * *", UploadChunkSizeMB: 16, NtfyTopic: "backups"}
	for _, name := range []string{"scout-1", "scout-2"} {
		payload.ScoutID = name
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		mock.ExpectExec("INSERT INTO app_settings").WithArgs(string(raw)).WillReturnResult(sqlmock.NewResult(1, 1))
		if err := s.Save(ctx, payload); err != nil {
			t.Fatal(err)
		}
		mock.ExpectQuery("SELECT payload FROM app_settings").WillReturnRows(sqlmock.NewRows([]string{"payload"}).AddRow(string(raw)))
		if got, err := s.Load(ctx); err != nil || !reflect.DeepEqual(got, payload) {
			t.Fatalf("Load: %+v, %v", got, err)
		}
	}
}

func TestSettingsStoreErrors(t *testing.T) {
	db, mock := testutil.MockDB(t)
	s := NewSettingsStore(db)
	ctx := context.Background()
	dbErr := errors.New("database unavailable")
	mock.ExpectExec("CREATE TABLE").WillReturnError(dbErr)
	if err := s.EnsureSchema(ctx); !errors.Is(err, dbErr) {
		t.Fatal(err)
	}
	mock.ExpectExec("INSERT INTO app_settings").WillReturnError(dbErr)
	if err := s.Save(ctx, &config.SettingsPayload{}); !errors.Is(err, dbErr) {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT payload").WillReturnError(dbErr)
	if _, err := s.Load(ctx); !errors.Is(err, dbErr) {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT payload").WillReturnRows(sqlmock.NewRows([]string{"payload"}).AddRow("invalid json"))
	if _, err := s.Load(ctx); err == nil {
		t.Fatal("expected JSON error")
	}
}

func TestUserStoreEnsureSchema(t *testing.T) {
	s, mock := testUserStore(t)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS app_users").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS app_sessions").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS app_automation_tokens").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	dbErr := errors.New("schema failed")
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS app_users").WillReturnError(dbErr)
	if err := s.EnsureSchema(context.Background()); !errors.Is(err, dbErr) {
		t.Fatal(err)
	}
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS app_users").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS app_sessions").WillReturnError(dbErr)
	if err := s.EnsureSchema(context.Background()); !errors.Is(err, dbErr) {
		t.Fatal(err)
	}
}

func TestUserStoreDefaultAdminAndAuthentication(t *testing.T) {
	s, mock := testUserStore(t)
	ctx := context.Background()
	admin := userFixture(t, BootstrapAdminID, DefaultAdminUsername, DefaultAdminPassword, true, false)
	expectList(mock)
	mock.ExpectExec("INSERT INTO app_users").WithArgs(DefaultAdminUsername, passwordHashArg(DefaultAdminPassword), 1).WillReturnResult(sqlmock.NewResult(BootstrapAdminID, 1))
	expectGet(mock, admin.ID, admin)
	expectGet(mock, admin.ID, admin)
	mock.ExpectExec("UPDATE app_users").WithArgs(admin.Username, admin.PasswordHash, 1, 1, admin.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	changed := *admin
	changed.MustChangePassword = true
	expectGet(mock, admin.ID, &changed)
	if err := s.EnsureDefaultAdmin(ctx, ""); err != nil {
		t.Fatal(err)
	}
	expectList(mock, &changed)
	if err := s.EnsureDefaultAdmin(ctx, DefaultAdminPassword); err != nil {
		t.Fatal(err)
	}
	expectList(mock, &changed)
	users, err := s.ListUsers(ctx)
	if err != nil || len(users) != 1 || !users[0].IsAdmin || !users[0].IsBootstrapAdmin || !users[0].MustChangePassword {
		t.Fatalf("users: %+v, %v", users, err)
	}
	mock.ExpectQuery("FROM app_users WHERE username =").WithArgs(admin.Username).WillReturnRows(userRows(&changed))
	if got, err := s.Authenticate(ctx, admin.Username, DefaultAdminPassword); err != nil || got == nil {
		t.Fatalf("auth: %+v, %v", got, err)
	}
	mock.ExpectQuery("FROM app_users WHERE username =").WithArgs(admin.Username).WillReturnRows(userRows(&changed))
	if got, err := s.Authenticate(ctx, admin.Username, "wrong"); err != nil || got != nil {
		t.Fatalf("wrong password: %+v, %v", got, err)
	}
}

func TestUserStoreCreateUpdateDeleteAndSessions(t *testing.T) {
	s, mock := testUserStore(t)
	ctx := context.Background()
	user := userFixture(t, 2, "backup.user", "user-pass", false, false)
	admin := userFixture(t, 1, "adminuser", "admin-pass", true, false)
	mock.ExpectExec("INSERT INTO app_users").WithArgs("backup.user", passwordHashArg("user-pass"), 0).WillReturnResult(sqlmock.NewResult(2, 1))
	expectGet(mock, 2, user)
	got, err := s.CreateUser(ctx, "  Backup.User  ", "user-pass", false)
	if err != nil || got.Username != "backup.user" || got.IsAdmin {
		t.Fatalf("create: %+v, %v", got, err)
	}
	mock.ExpectExec("INSERT INTO app_users").WillReturnError(errors.New("unique constraint"))
	if _, err := s.CreateUser(ctx, "backup.user", "other-pass", false); err == nil {
		t.Fatal("expected duplicate error")
	}

	mock.ExpectExec("INSERT INTO app_sessions").WithArgs(sqlmock.AnyArg(), 2, expiryArg{}).WillReturnResult(sqlmock.NewResult(0, 1))
	token, err := s.CreateSession(ctx, 2)
	if err != nil || token == "" {
		t.Fatalf("session: %q, %v", token, err)
	}
	mock.ExpectExec("DELETE FROM app_sessions WHERE expires_at").WithArgs(sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("FROM app_sessions sess").WithArgs(token, sqlmock.AnyArg()).WillReturnRows(userRows(user))
	if got, err := s.UserForSession(ctx, token); err != nil || got == nil || got.ID != 2 {
		t.Fatalf("session user: %+v, %v", got, err)
	}
	if got, err := s.UserForSession(ctx, ""); err != nil || got != nil {
		t.Fatalf("empty session: %+v, %v", got, err)
	}

	next := userFixture(t, 2, "operator", "operator-pass", true, true)
	expectGet(mock, 2, user)
	mock.ExpectExec("UPDATE app_users").WithArgs("operator", passwordHashArg("operator-pass"), 1, 1, 2).WillReturnResult(sqlmock.NewResult(0, 1))
	expectGet(mock, 2, next)
	name, password, makeAdmin, mustChange := "operator", "operator-pass", true, true
	if got, err := s.UpdateUser(ctx, 2, &name, &password, &makeAdmin, &mustChange); err != nil || got.Username != name || !got.IsAdmin || !got.MustChangePassword {
		t.Fatalf("update: %+v, %v", got, err)
	}
	mock.ExpectQuery("FROM app_users WHERE username =").WithArgs(name).WillReturnRows(userRows(next))
	if got, err := s.Authenticate(ctx, name, password); err != nil || got == nil {
		t.Fatalf("updated auth: %+v, %v", got, err)
	}

	expectGet(mock, 2, next)
	expectList(mock, admin, next)
	mock.ExpectExec("UPDATE app_users").WithArgs(name, next.PasswordHash, 0, 1, 2).WillReturnResult(sqlmock.NewResult(0, 1))
	demoted := *next
	demoted.IsAdmin = false
	expectGet(mock, 2, &demoted)
	falseAdmin := false
	if _, err := s.UpdateUser(ctx, 2, nil, nil, &falseAdmin, nil); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec("DELETE FROM app_sessions WHERE token =").WithArgs(token).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.DeleteSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	// Expired sessions were just cleaned up, so this lookup does not write again.
	mock.ExpectQuery("FROM app_sessions sess").WithArgs(token, sqlmock.AnyArg()).WillReturnRows(userRows())
	if got, err := s.UserForSession(ctx, token); err != nil || got != nil {
		t.Fatalf("deleted session: %+v, %v", got, err)
	}
	mock.ExpectExec("DELETE FROM app_sessions WHERE user_id =").WithArgs(2).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := s.DeleteSessionsForUser(ctx, 2); err != nil {
		t.Fatal(err)
	}
	expectGet(mock, 2, &demoted)
	expectList(mock, admin, &demoted)
	mock.ExpectExec("DELETE FROM app_sessions WHERE user_id =").WithArgs(2).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM app_users WHERE id =").WithArgs(2).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.DeleteUser(ctx, 2); err != nil {
		t.Fatal(err)
	}
	expectGet(mock, 2)
	if got, err := s.GetUserByID(ctx, 2); err != nil || got != nil {
		t.Fatalf("deleted user: %+v, %v", got, err)
	}
}

func TestUserStoreValidationAndProtection(t *testing.T) {
	s, mock := testUserStore(t)
	ctx := context.Background()
	for _, input := range []struct{ name, password string }{
		{"ab", "valid-pass"}, {strings.Repeat("a", 65), "valid-pass"}, {"bad/name", "valid-pass"},
		{"valid", "1234"}, {"valid", "     "}, {"admin", "valid-pass"},
	} {
		if _, err := s.CreateUser(ctx, input.name, input.password, false); err == nil {
			t.Fatalf("accepted %q", input.name)
		}
	}
	admin := userFixture(t, 1, "admin", "valid-pass", true, false)
	expectGet(mock, 1, admin)
	if err := s.DeleteUser(ctx, 1); err == nil {
		t.Fatal("deleted bootstrap admin")
	}
	expectGet(mock, 1, admin)
	mock.ExpectExec("UPDATE app_users").WithArgs("admin", admin.PasswordHash, 1, 0, 1).WillReturnResult(sqlmock.NewResult(0, 1))
	expectGet(mock, 1, admin)
	falseAdmin := false
	if got, err := s.UpdateUser(ctx, 1, nil, nil, &falseAdmin, nil); err != nil || !got.IsAdmin {
		t.Fatalf("bootstrap demoted: %+v, %v", got, err)
	}
	expectGet(mock, 9999)
	if _, err := s.UpdateUser(ctx, 9999, nil, nil, nil, nil); err == nil {
		t.Fatal("updated missing user")
	}
	expectGet(mock, 9999)
	if err := s.DeleteUser(ctx, 9999); err == nil {
		t.Fatal("deleted missing user")
	}

	last := userFixture(t, 2, "operator", "valid-pass", true, false)
	expectGet(mock, 2, last)
	expectList(mock, last)
	if err := s.DeleteUser(ctx, 2); err == nil {
		t.Fatal("deleted last admin")
	}
	expectGet(mock, 2, last)
	expectList(mock, last)
	if _, err := s.UpdateUser(ctx, 2, nil, nil, &falseAdmin, nil); err == nil {
		t.Fatal("demoted last admin")
	}
}

func TestUserStoreChangePasswordAndExpiredSession(t *testing.T) {
	s, mock := testUserStore(t)
	ctx := context.Background()
	user := userFixture(t, 2, "operator", "old-password", true, true)
	expectGet(mock, 2, user)
	if _, err := s.ChangePassword(ctx, 2, "wrong-password", "new-password"); err == nil {
		t.Fatal("accepted wrong password")
	}
	expectGet(mock, 2, user)
	expectGet(mock, 2, user)
	mock.ExpectExec("UPDATE app_users").WithArgs("operator", passwordHashArg("new-password"), 1, 0, 2).WillReturnResult(sqlmock.NewResult(0, 1))
	updated := userFixture(t, 2, "operator", "new-password", true, false)
	expectGet(mock, 2, updated)
	if got, err := s.ChangePassword(ctx, 2, "old-password", "new-password"); err != nil || got.MustChangePassword {
		t.Fatalf("password change: %+v, %v", got, err)
	}
	mock.ExpectQuery("FROM app_users WHERE username =").WithArgs("operator").WillReturnRows(userRows(updated))
	if got, err := s.Authenticate(ctx, "operator", "new-password"); err != nil || got == nil {
		t.Fatalf("new password: %+v, %v", got, err)
	}
	mock.ExpectExec("DELETE FROM app_sessions WHERE expires_at").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("FROM app_sessions sess").WithArgs("expired-token", sqlmock.AnyArg()).WillReturnRows(userRows())
	if got, err := s.UserForSession(ctx, "expired-token"); err != nil || got != nil {
		t.Fatalf("expired: %+v, %v", got, err)
	}
}

func TestUserStoreLookupAndSessionErrors(t *testing.T) {
	s, mock := testUserStore(t)
	ctx := context.Background()
	dbErr := errors.New("read failed")
	mock.ExpectQuery("FROM app_users WHERE id =").WithArgs(2).WillReturnError(dbErr)
	if _, err := s.GetUserByID(ctx, 2); !errors.Is(err, dbErr) {
		t.Fatal(err)
	}
	mock.ExpectQuery("FROM app_users ORDER BY").WillReturnError(dbErr)
	if _, err := s.ListUsers(ctx); !errors.Is(err, dbErr) {
		t.Fatal(err)
	}
	mock.ExpectExec("INSERT INTO app_sessions").WillReturnError(dbErr)
	if _, err := s.CreateSession(ctx, 2); !errors.Is(err, dbErr) {
		t.Fatal(err)
	}
	if err := s.DeleteSession(ctx, ""); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec("DELETE FROM app_sessions WHERE token").WillReturnError(dbErr)
	if err := s.DeleteSession(ctx, "token"); !errors.Is(err, dbErr) {
		t.Fatal(err)
	}
	mock.ExpectExec("DELETE FROM app_sessions WHERE user_id").WillReturnError(dbErr)
	if err := s.DeleteSessionsForUser(ctx, 2); !errors.Is(err, dbErr) {
		t.Fatal(err)
	}
}

func TestPasswordHelpersRejectMalformedHashes(t *testing.T) {
	for _, encoded := range []string{"", "plain", "pbkdf2_sha1$1$00$00", "pbkdf2_sha256$x$00$00", "pbkdf2_sha256$1$zz$00", "pbkdf2_sha256$1$00$zz"} {
		if verifyPassword("anything", encoded) {
			t.Fatalf("verifyPassword accepted %q", encoded)
		}
	}
}
