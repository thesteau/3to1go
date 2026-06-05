package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/3to1go/edge/internal/config"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "edge.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func testUserStore(t *testing.T) (*UserStore, context.Context) {
	t.Helper()
	ctx := context.Background()
	s := NewUserStore(testDB(t))
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return s, ctx
}

func TestOpenCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "edge.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestSettingsStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := NewSettingsStore(testDB(t))
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	got, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load empty: %v", err)
	}
	if got != nil {
		t.Fatalf("Load empty = %+v, want nil", got)
	}

	payload := &config.SettingsPayload{
		EdgeID:            "edge-1",
		ScanRoot:          "/data",
		CentralURL:        "https://central.example",
		CronSchedule:      "*/30 * * * *",
		UploadChunkSizeMB: 16,
		NtfyTopic:         "backups",
	}
	if err := store.Save(ctx, payload); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err = store.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.EdgeID != payload.EdgeID || got.UploadChunkSizeMB != payload.UploadChunkSizeMB {
		t.Fatalf("Load = %+v", got)
	}

	payload.EdgeID = "edge-2"
	if err := store.Save(ctx, payload); err != nil {
		t.Fatalf("Save replace: %v", err)
	}
	got, err = store.Load(ctx)
	if err != nil {
		t.Fatalf("Load replace: %v", err)
	}
	if got.EdgeID != "edge-2" {
		t.Fatalf("EdgeID = %q", got.EdgeID)
	}
}

func TestUserStoreDefaultAdminAndAuthentication(t *testing.T) {
	store, ctx := testUserStore(t)
	if err := store.EnsureDefaultAdmin(ctx); err != nil {
		t.Fatalf("EnsureDefaultAdmin: %v", err)
	}
	if err := store.EnsureDefaultAdmin(ctx); err != nil {
		t.Fatalf("EnsureDefaultAdmin idempotent: %v", err)
	}
	users, err := store.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 || users[0].Username != DefaultAdminUsername || !users[0].IsAdmin {
		t.Fatalf("users = %+v", users)
	}
	if !users[0].IsBootstrapAdmin || !users[0].MustChangePassword {
		t.Fatalf("bootstrap flags not set: %+v", users[0])
	}

	auth, err := store.Authenticate(ctx, DefaultAdminUsername, DefaultAdminPassword)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if auth == nil || auth.Username != DefaultAdminUsername {
		t.Fatalf("Authenticate = %+v", auth)
	}
	if auth, err = store.Authenticate(ctx, DefaultAdminUsername, "wrong"); err != nil || auth != nil {
		t.Fatalf("Authenticate wrong = %+v, %v", auth, err)
	}
}

func TestUserStoreCreateUpdateDeleteAndSessions(t *testing.T) {
	store, ctx := testUserStore(t)
	admin, err := store.CreateUser(ctx, "AdminUser", "admin-pass", true)
	if err != nil {
		t.Fatalf("CreateUser admin: %v", err)
	}
	user, err := store.CreateUser(ctx, "  Backup.User  ", "user-pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.Username != "backup.user" || user.IsAdmin {
		t.Fatalf("normalized user = %+v", user)
	}
	if _, err := store.CreateUser(ctx, "backup.user", "other-pass", false); err == nil {
		t.Fatal("expected duplicate username error")
	}

	token, err := store.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessionUser, err := store.UserForSession(ctx, token)
	if err != nil {
		t.Fatalf("UserForSession: %v", err)
	}
	if sessionUser == nil || sessionUser.ID != user.ID {
		t.Fatalf("UserForSession = %+v", sessionUser)
	}
	if nilUser, err := store.UserForSession(ctx, ""); err != nil || nilUser != nil {
		t.Fatalf("UserForSession empty = %+v, %v", nilUser, err)
	}

	newName := "operator"
	newPassword := "operator-pass"
	makeAdmin := true
	mustChange := true
	updated, err := store.UpdateUser(ctx, user.ID, &newName, &newPassword, &makeAdmin, &mustChange)
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if updated.Username != newName || !updated.IsAdmin || !updated.MustChangePassword {
		t.Fatalf("updated = %+v", updated)
	}
	if _, err := store.Authenticate(ctx, newName, newPassword); err != nil {
		t.Fatalf("Authenticate updated: %v", err)
	}

	falseAdmin := false
	if _, err := store.UpdateUser(ctx, user.ID, nil, nil, &falseAdmin, nil); err != nil {
		t.Fatalf("demote second admin: %v", err)
	}
	if err := store.DeleteSession(ctx, token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if got, err := store.UserForSession(ctx, token); err != nil || got != nil {
		t.Fatalf("deleted session = %+v, %v", got, err)
	}
	if err := store.DeleteSessionsForUser(ctx, user.ID); err != nil {
		t.Fatalf("DeleteSessionsForUser: %v", err)
	}
	if err := store.DeleteUser(ctx, user.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if got, err := store.GetUserByID(ctx, user.ID); err != nil || got != nil {
		t.Fatalf("deleted user = %+v, %v", got, err)
	}
	if err := store.DeleteUser(ctx, admin.ID); err == nil {
		t.Fatal("expected last-admin delete to fail")
	}
}

func TestUserStoreValidationAndProtection(t *testing.T) {
	store, ctx := testUserStore(t)
	if _, err := store.CreateUser(ctx, "ab", "valid-pass", false); err == nil {
		t.Fatal("expected short username error")
	}
	if _, err := store.CreateUser(ctx, strings.Repeat("a", 65), "valid-pass", false); err == nil {
		t.Fatal("expected long username error")
	}
	if _, err := store.CreateUser(ctx, "bad/name", "valid-pass", false); err == nil {
		t.Fatal("expected invalid username error")
	}
	if _, err := store.CreateUser(ctx, "valid", "1234", false); err == nil {
		t.Fatal("expected short password error")
	}
	if _, err := store.CreateUser(ctx, "valid", "     ", false); err == nil {
		t.Fatal("expected blank password error")
	}

	if err := store.EnsureDefaultAdmin(ctx); err != nil {
		t.Fatalf("EnsureDefaultAdmin: %v", err)
	}
	if err := store.DeleteUser(ctx, BootstrapAdminID); err == nil {
		t.Fatal("expected bootstrap delete to fail")
	}
	falseAdmin := false
	if _, err := store.UpdateUser(ctx, BootstrapAdminID, nil, nil, &falseAdmin, nil); err != nil {
		t.Fatalf("bootstrap remains admin update: %v", err)
	}
	got, err := store.GetUserByID(ctx, BootstrapAdminID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if !got.IsAdmin {
		t.Fatalf("bootstrap admin was demoted: %+v", got)
	}
	if _, err := store.UpdateUser(ctx, 9999, nil, nil, nil, nil); err == nil {
		t.Fatal("expected missing update error")
	}
	if err := store.DeleteUser(ctx, 9999); err == nil {
		t.Fatal("expected missing delete error")
	}
}

func TestUserStoreChangePasswordAndExpiredSession(t *testing.T) {
	store, ctx := testUserStore(t)
	user, err := store.CreateUser(ctx, "operator", "old-password", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.ChangePassword(ctx, user.ID, "wrong-password", "new-password"); err == nil {
		t.Fatal("expected current password error")
	}
	updated, err := store.ChangePassword(ctx, user.ID, "old-password", "new-password")
	if err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if updated.MustChangePassword {
		t.Fatalf("MustChangePassword = true after explicit change")
	}
	auth, err := store.Authenticate(ctx, user.Username, "new-password")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if auth == nil {
		t.Fatal("expected authenticated user")
	}

	expired := "expired-token"
	_, err = store.db.ExecContext(ctx,
		`INSERT INTO app_sessions (token, user_id, expires_at) VALUES (?, ?, ?)`,
		expired, user.ID, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert expired session: %v", err)
	}
	got, err := store.UserForSession(ctx, expired)
	if err != nil {
		t.Fatalf("UserForSession expired: %v", err)
	}
	if got != nil {
		t.Fatalf("expired session returned %+v", got)
	}
}

func TestPasswordHelpersRejectMalformedHashes(t *testing.T) {
	for _, encoded := range []string{
		"",
		"plain",
		"pbkdf2_sha1$1$00$00",
		"pbkdf2_sha256$x$00$00",
		"pbkdf2_sha256$1$zz$00",
		"pbkdf2_sha256$1$00$zz",
	} {
		if verifyPassword("anything", encoded) {
			t.Fatalf("verifyPassword accepted %q", encoded)
		}
	}
}
