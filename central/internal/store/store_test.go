package store

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/3to1go/central/internal/config"
	"github.com/3to1go/central/internal/signing"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ---------------------------------------------------------------------------
// Mock pgx.Row
// ---------------------------------------------------------------------------

type mockRow struct {
	scanFn func(dest ...any) error
}

func (r *mockRow) Scan(dest ...any) error {
	if r.scanFn == nil {
		return pgx.ErrNoRows
	}
	return r.scanFn(dest...)
}

func noRow() pgx.Row         { return &mockRow{} }
func errRow(e error) pgx.Row { return &mockRow{scanFn: func(...any) error { return e }} }

// ---------------------------------------------------------------------------
// Mock pgx.Rows
// ---------------------------------------------------------------------------

type mockRows struct {
	scanFns []func(dest ...any) error
	idx     int
	err     error
}

func (r *mockRows) Next() bool {
	r.idx++
	return r.idx <= len(r.scanFns)
}
func (r *mockRows) Scan(dest ...any) error                       { return r.scanFns[r.idx-1](dest...) }
func (r *mockRows) Err() error                                   { return r.err }
func (r *mockRows) Close()                                       {}
func (r *mockRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *mockRows) Values() ([]any, error)                       { return nil, nil }
func (r *mockRows) RawValues() [][]byte                          { return nil }
func (r *mockRows) Conn() *pgx.Conn                              { return nil }

func emptyRows() pgx.Rows      { return &mockRows{} }
func errRows(e error) pgx.Rows { return &mockRows{err: e} }

// ---------------------------------------------------------------------------
// Mock dbPool
// ---------------------------------------------------------------------------

type mockPool struct {
	execFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
}

func (m *mockPool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if m.execFn != nil {
		return m.execFn(ctx, sql, args...)
	}
	return pgconn.CommandTag{}, nil
}

func (m *mockPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, sql, args...)
	}
	return emptyRows(), nil
}

func (m *mockPool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, sql, args...)
	}
	return noRow()
}

// queuePool returns a pool where each call pops the next function off a queue.
type queuePool struct {
	rows []pgx.Row
	rowQ []pgx.Rows
}

func (q *queuePool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (q *queuePool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if len(q.rowQ) == 0 {
		return emptyRows(), nil
	}
	r := q.rowQ[0]
	q.rowQ = q.rowQ[1:]
	return r, nil
}
func (q *queuePool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if len(q.rows) == 0 {
		return noRow()
	}
	r := q.rows[0]
	q.rows = q.rows[1:]
	return r
}

// ---------------------------------------------------------------------------
// Helper: scan functions for common types
// ---------------------------------------------------------------------------

func userScanFn(id int, username, hash string, isAdmin, mustChange bool) func(dest ...any) error {
	return func(dest ...any) error {
		*dest[0].(*int) = id
		*dest[1].(*string) = username
		*dest[2].(*string) = hash
		*dest[3].(*bool) = isAdmin
		*dest[4].(*bool) = mustChange
		*dest[5].(*time.Time) = time.Now()
		return nil
	}
}

// ---------------------------------------------------------------------------
// Pure function tests — no DB needed
// ---------------------------------------------------------------------------

func TestSplitNamespace_Valid(t *testing.T) {
	e, i, j, err := splitNamespace("edge1/inst1/job1")
	if err != nil || e != "edge1" || i != "inst1" || j != "job1" {
		t.Errorf("got %q %q %q %v", e, i, j, err)
	}
}

func TestSplitNamespace_Invalid(t *testing.T) {
	cases := []string{"", "a/b", "a//b", "/a/b", "a/b/"}
	for _, ns := range cases {
		_, _, _, err := splitNamespace(ns)
		if err == nil {
			t.Errorf("splitNamespace(%q) expected error", ns)
		}
	}
}

func TestNormalizeUsername(t *testing.T) {
	cases := []struct {
		in  string
		out string
		ok  bool
	}{
		{"Alice", "alice", true},
		{"  BOB  ", "bob", true},
		{"ab", "", false},                    // too short
		{strings.Repeat("a", 65), "", false}, // too long
		{"alice@example", "", false},         // invalid char
		{"alice_123", "alice_123", true},
		{"al", "", false},
	}
	for _, tc := range cases {
		got, err := normalizeUsername(tc.in)
		if tc.ok && (err != nil || got != tc.out) {
			t.Errorf("normalizeUsername(%q) = %q, %v; want %q, nil", tc.in, got, err, tc.out)
		}
		if !tc.ok && err == nil {
			t.Errorf("normalizeUsername(%q) expected error", tc.in)
		}
	}
}

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := hashPassword("correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	if !verifyPassword("correcthorsebatterystaple", hash) {
		t.Error("verifyPassword: correct password not verified")
	}
	if verifyPassword("wrongpassword", hash) {
		t.Error("verifyPassword: wrong password verified")
	}
}

func TestHashPassword_TooShort(t *testing.T) {
	_, err := hashPassword("abc")
	if err == nil {
		t.Error("expected error for short password")
	}
}

func TestHashPassword_SpacesOnly(t *testing.T) {
	_, err := hashPassword("     ")
	if err == nil {
		t.Error("expected error for spaces-only password")
	}
}

func TestVerifyPassword_InvalidFormat(t *testing.T) {
	if verifyPassword("password", "notahash") {
		t.Error("expected false for malformed hash")
	}
	if verifyPassword("password", "pbkdf2_sha256$bad$salt$digest") {
		t.Error("expected false for bad iterations")
	}
}

func TestParseInt(t *testing.T) {
	n, err := parseInt("260000")
	if err != nil || n != 260000 {
		t.Errorf("parseInt(%q) = %d, %v", "260000", n, err)
	}
	_, err = parseInt("abc")
	if err == nil {
		t.Error("expected error for non-integer")
	}
}

func TestPublicUser(t *testing.T) {
	u := &User{ID: BootstrapAdminID, Username: "admin", IsAdmin: true}
	pub := publicUser(u)
	if !pub.IsBootstrapAdmin {
		t.Error("IsBootstrapAdmin should be true for bootstrap admin")
	}
	u2 := &User{ID: 99, Username: "bob"}
	if publicUser(u2).IsBootstrapAdmin {
		t.Error("IsBootstrapAdmin should be false for non-bootstrap user")
	}
}

func TestBoolPtr(t *testing.T) {
	b := boolPtr(true)
	if b == nil || !*b {
		t.Error("boolPtr(true) should return non-nil pointer to true")
	}
}

func TestTokenHash(t *testing.T) {
	h1 := tokenHash("mytoken")
	h2 := tokenHash("mytoken")
	if h1 != h2 {
		t.Error("tokenHash should be deterministic")
	}
	if h1 == tokenHash("othertoken") {
		t.Error("different tokens should have different hashes")
	}
}

func TestUTCNow(t *testing.T) {
	s := UTCNow()
	_, err := time.ParseInLocation("2006-01-02T15:04:05Z", s, time.UTC)
	if err != nil {
		t.Errorf("UTCNow() = %q, invalid format: %v", s, err)
	}
}

func TestScanUser_Success(t *testing.T) {
	row := &mockRow{scanFn: userScanFn(42, "alice", "hash", true, false)}
	u, err := scanUser(row)
	if err != nil || u.ID != 42 || u.Username != "alice" || !u.IsAdmin {
		t.Errorf("scanUser unexpected result: %+v %v", u, err)
	}
}

func TestScanUser_Error(t *testing.T) {
	row := errRow(pgx.ErrNoRows)
	_, err := scanUser(row)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected ErrNoRows, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// SnapshotIndex tests
// ---------------------------------------------------------------------------

func newSnapIndex(pool dbPool) *SnapshotIndex { return &SnapshotIndex{pool: pool} }

func TestSnapshotIndex_EnsureSchema_Success(t *testing.T) {
	idx := newSnapIndex(&mockPool{})
	if err := idx.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
}

func TestSnapshotIndex_EnsureSchema_ExecError(t *testing.T) {
	calls := 0
	idx := newSnapIndex(&mockPool{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		calls++
		if calls == 3 {
			return pgconn.CommandTag{}, errors.New("exec error")
		}
		return pgconn.CommandTag{}, nil
	}})
	if err := idx.EnsureSchema(context.Background()); err == nil {
		t.Error("expected error from EnsureSchema when Exec fails")
	}
}

func TestFindDuplicate_NotFound(t *testing.T) {
	idx := newSnapIndex(&mockPool{})
	result, err := idx.FindDuplicate(context.Background(), "edge/inst/job", "sha256")
	if err != nil || result != nil {
		t.Errorf("expected nil, nil; got %v, %v", result, err)
	}
}

func TestFindDuplicate_Found(t *testing.T) {
	idx := newSnapIndex(&mockPool{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*string) = "job__2024-01-01T00-00-00Z__abcdef12.tar.zst"
			*dest[1].(*string) = "sha256"
			*dest[2].(*string) = "fp"
			*dest[3].(*string) = "2024-01-01T00:00:00Z"
			*dest[4].(*int64) = 100
			*dest[5].(*float64) = 1234.0
			return nil
		}}
	}})
	e, err := idx.FindDuplicate(context.Background(), "edge/inst/job", "sha256")
	if err != nil || e == nil || e.StoredAs == "" {
		t.Errorf("expected entry, got %v, %v", e, err)
	}
}

func TestFindDuplicate_InvalidNamespace(t *testing.T) {
	idx := newSnapIndex(&mockPool{})
	_, err := idx.FindDuplicate(context.Background(), "bad-namespace", "sha")
	if err == nil {
		t.Error("expected error for invalid namespace")
	}
}

func TestFindDuplicate_DBError(t *testing.T) {
	idx := newSnapIndex(&mockPool{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
		return errRow(errors.New("connection reset"))
	}})
	_, err := idx.FindDuplicate(context.Background(), "edge/inst/job", "sha256")
	if err == nil {
		t.Error("expected DB error to propagate")
	}
}

func TestUpsertSnapshot_Success(t *testing.T) {
	idx := newSnapIndex(&mockPool{})
	err := idx.UpsertSnapshot(context.Background(), "edge/inst/job", SnapshotEntry{StoredAs: "file.tar.zst"})
	if err != nil {
		t.Fatalf("UpsertSnapshot: %v", err)
	}
}

func TestUpsertSnapshot_InvalidNamespace(t *testing.T) {
	idx := newSnapIndex(&mockPool{})
	if err := idx.UpsertSnapshot(context.Background(), "bad", SnapshotEntry{}); err == nil {
		t.Error("expected error for invalid namespace")
	}
}

func TestUpsertSnapshot_ExecError(t *testing.T) {
	idx := newSnapIndex(&mockPool{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("exec error")
	}})
	if err := idx.UpsertSnapshot(context.Background(), "e/i/j", SnapshotEntry{}); err == nil {
		t.Error("expected exec error to propagate")
	}
}

func TestReconcileNamespace_WithFiles(t *testing.T) {
	idx := newSnapIndex(&mockPool{})
	err := idx.ReconcileNamespace(context.Background(), "edge/inst/job", []StorageFile{
		{Filename: "a.tar.zst"}, {Filename: "b.tar.zst"},
	})
	if err != nil {
		t.Fatalf("ReconcileNamespace: %v", err)
	}
}

func TestReconcileNamespace_EmptyFiles(t *testing.T) {
	idx := newSnapIndex(&mockPool{})
	if err := idx.ReconcileNamespace(context.Background(), "edge/inst/job", nil); err != nil {
		t.Fatalf("ReconcileNamespace empty: %v", err)
	}
}

func TestListNamespaces_Empty(t *testing.T) {
	idx := newSnapIndex(&mockPool{})
	ns, err := idx.ListNamespaces(context.Background())
	if err != nil || len(ns) != 0 {
		t.Errorf("expected empty list, got %v, %v", ns, err)
	}
}

func TestListNamespaces_WithData(t *testing.T) {
	rows := &mockRows{scanFns: []func(dest ...any) error{
		func(dest ...any) error {
			*dest[0].(*string) = "edge1"
			*dest[1].(*string) = "inst1"
			*dest[2].(*string) = "backup"
			*dest[3].(*string) = "file.tar.zst"
			*dest[4].(*int64) = 100
			*dest[5].(*float64) = 1234.5
			return nil
		},
	}}
	idx := newSnapIndex(&mockPool{queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
		return rows, nil
	}})
	ns, err := idx.ListNamespaces(context.Background())
	if err != nil || len(ns) != 1 || len(ns[0].Jobs) != 1 {
		t.Errorf("unexpected result: %v, %v", ns, err)
	}
}

func TestListNamespaces_QueryError(t *testing.T) {
	idx := newSnapIndex(&mockPool{queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
		return nil, errors.New("query error")
	}})
	_, err := idx.ListNamespaces(context.Background())
	if err == nil {
		t.Error("expected query error to propagate")
	}
}

func TestGetEdgeRegistration_NotFound(t *testing.T) {
	idx := newSnapIndex(&mockPool{})
	r, err := idx.GetEdgeRegistration(context.Background(), "edge1", "inst1")
	if err != nil || r != nil {
		t.Errorf("expected nil,nil; got %v,%v", r, err)
	}
}

func TestGetEdgeRegistration_Found(t *testing.T) {
	idx := newSnapIndex(&mockPool{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*string) = "edge1"
			*dest[1].(*string) = "inst1"
			*dest[2].(**string) = nil // nil is fine for optional fields
			*dest[3].(**string) = nil
			*dest[4].(*string) = "2024-01-01T00:00:00Z"
			*dest[5].(*string) = "2024-01-01T01:00:00Z"
			*dest[6].(**string) = nil
			return nil
		}}
	}})
	reg, err := idx.GetEdgeRegistration(context.Background(), "edge1", "inst1")
	if err != nil || reg == nil || reg.EdgeID != "edge1" {
		t.Errorf("unexpected: %v, %v", reg, err)
	}
}

func TestGetEdgeRegistration_DBError(t *testing.T) {
	idx := newSnapIndex(&mockPool{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
		return errRow(errors.New("db error"))
	}})
	_, err := idx.GetEdgeRegistration(context.Background(), "edge1", "inst1")
	if err == nil {
		t.Error("expected error to propagate")
	}
}

func TestUpsertEdgeRegistration(t *testing.T) {
	idx := newSnapIndex(&mockPool{})
	err := idx.UpsertEdgeRegistration(context.Background(), &EdgeRegistration{
		EdgeID: "e", EdgeInstanceID: "i", FirstSeenAt: "now", LastSeenAt: "now",
	})
	if err != nil {
		t.Fatalf("UpsertEdgeRegistration: %v", err)
	}
}

func TestDeleteEdgeRegistration(t *testing.T) {
	idx := newSnapIndex(&mockPool{})
	if err := idx.DeleteEdgeRegistration(context.Background(), "edge1", "inst1"); err != nil {
		t.Fatalf("DeleteEdgeRegistration: %v", err)
	}
}

func TestListEdgeRegistrations_NoFilter(t *testing.T) {
	idx := newSnapIndex(&mockPool{})
	regs, err := idx.ListEdgeRegistrations(context.Background(), nil)
	if err != nil || len(regs) != 0 {
		t.Errorf("expected empty, got %v, %v", regs, err)
	}
}

func TestListEdgeRegistrations_WithFilter(t *testing.T) {
	filter := "edge1"
	idx := newSnapIndex(&mockPool{})
	_, err := idx.ListEdgeRegistrations(context.Background(), &filter)
	if err != nil {
		t.Fatalf("ListEdgeRegistrations with filter: %v", err)
	}
}

func TestListEdgeRegistrations_QueryError(t *testing.T) {
	idx := newSnapIndex(&mockPool{queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
		return nil, errors.New("query error")
	}})
	_, err := idx.ListEdgeRegistrations(context.Background(), nil)
	if err == nil {
		t.Error("expected query error")
	}
}

func TestHasNamespaceEntries(t *testing.T) {
	idx := newSnapIndex(&mockPool{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*int) = 2
			return nil
		}}
	}})
	has, err := idx.HasNamespaceEntries(context.Background(), "edge1", "inst1")
	if err != nil || !has {
		t.Errorf("HasNamespaceEntries: %v, %v", has, err)
	}
}

func TestListNamespaceEntries_QueryError(t *testing.T) {
	idx := newSnapIndex(&mockPool{queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
		return nil, errors.New("query error")
	}})
	_, err := idx.ListNamespaceEntries(context.Background(), "e/i/j")
	if err == nil {
		t.Error("expected error")
	}
}

func TestListNamespaceEntries_InvalidNamespace(t *testing.T) {
	idx := newSnapIndex(&mockPool{})
	_, err := idx.ListNamespaceEntries(context.Background(), "bad")
	if err == nil {
		t.Error("expected error for invalid namespace")
	}
}

// ---------------------------------------------------------------------------
// UserStore tests
// ---------------------------------------------------------------------------

func newUserStore(pool dbPool) *UserStore { return &UserStore{pool: pool} }

func TestUserStore_EnsureSchema_Success(t *testing.T) {
	s := newUserStore(&mockPool{})
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
}

func TestUserStore_EnsureSchema_ExecError(t *testing.T) {
	s := newUserStore(&mockPool{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("exec error")
	}})
	if err := s.EnsureSchema(context.Background()); err == nil {
		t.Error("expected exec error")
	}
}

func TestUserStore_ListUsers_Empty(t *testing.T) {
	s := newUserStore(&mockPool{})
	users, err := s.ListUsers(context.Background())
	if err != nil || len(users) != 0 {
		t.Errorf("expected empty, got %v, %v", users, err)
	}
}

func TestUserStore_ListUsers_WithData(t *testing.T) {
	rows := &mockRows{scanFns: []func(dest ...any) error{
		func(dest ...any) error {
			*dest[0].(*int) = 1
			*dest[1].(*string) = "alice"
			*dest[2].(*string) = "hash"
			*dest[3].(*bool) = true
			*dest[4].(*bool) = false
			*dest[5].(*time.Time) = time.Now()
			return nil
		},
	}}
	s := newUserStore(&mockPool{queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
		return rows, nil
	}})
	users, err := s.ListUsers(context.Background())
	if err != nil || len(users) != 1 || users[0].Username != "alice" {
		t.Errorf("unexpected: %v, %v", users, err)
	}
}

func TestUserStore_GetUserByID_NotFound(t *testing.T) {
	s := newUserStore(&mockPool{})
	u, err := s.GetUserByID(context.Background(), 99)
	if err != nil || u != nil {
		t.Errorf("expected nil,nil; got %v, %v", u, err)
	}
}

func TestUserStore_GetUserByID_Found(t *testing.T) {
	s := newUserStore(&mockPool{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: userScanFn(1, "alice", "hash", true, false)}
	}})
	u, err := s.GetUserByID(context.Background(), 1)
	if err != nil || u == nil || u.Username != "alice" {
		t.Errorf("unexpected: %v, %v", u, err)
	}
}

func TestUserStore_GetUserByUsername_NotFound(t *testing.T) {
	s := newUserStore(&mockPool{})
	u, err := s.GetUserByUsername(context.Background(), "unknown")
	if err != nil || u != nil {
		t.Errorf("expected nil,nil; got %v, %v", u, err)
	}
}

func TestUserStore_CreateSession_Success(t *testing.T) {
	s := newUserStore(&mockPool{})
	token, err := s.CreateSession(context.Background(), 1)
	if err != nil || token == "" {
		t.Errorf("CreateSession: %q, %v", token, err)
	}
}

func TestUserStore_DeleteSession_EmptyToken(t *testing.T) {
	s := newUserStore(&mockPool{})
	if err := s.DeleteSession(context.Background(), ""); err != nil {
		t.Errorf("DeleteSession empty: %v", err)
	}
}

func TestUserStore_DeleteSession_Success(t *testing.T) {
	s := newUserStore(&mockPool{})
	if err := s.DeleteSession(context.Background(), "sometoken"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
}

func TestUserStore_DeleteSessionsForUser(t *testing.T) {
	s := newUserStore(&mockPool{})
	if err := s.DeleteSessionsForUser(context.Background(), 1); err != nil {
		t.Fatalf("DeleteSessionsForUser: %v", err)
	}
}

func TestUserStore_UserForSession_EmptyToken(t *testing.T) {
	s := newUserStore(&mockPool{})
	u, err := s.UserForSession(context.Background(), "")
	if err != nil || u != nil {
		t.Errorf("expected nil,nil; got %v, %v", u, err)
	}
}

func TestUserStore_UserForSession_NotFound(t *testing.T) {
	s := newUserStore(&mockPool{})
	u, err := s.UserForSession(context.Background(), "badtoken")
	if err != nil || u != nil {
		t.Errorf("expected nil,nil; got %v, %v", u, err)
	}
}

func TestUserStore_CreateUser_Success(t *testing.T) {
	s := newUserStore(&mockPool{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*int) = 10
			*dest[1].(*string) = "newuser"
			*dest[2].(*string) = "hash"
			*dest[3].(*bool) = false
			*dest[4].(*bool) = false
			*dest[5].(*time.Time) = time.Now()
			return nil
		}}
	}})
	u, err := s.CreateUser(context.Background(), "newuser", "password123", false)
	if err != nil || u == nil {
		t.Errorf("CreateUser: %v, %v", u, err)
	}
}

func TestUserStore_CreateUser_InvalidUsername(t *testing.T) {
	s := newUserStore(&mockPool{})
	_, err := s.CreateUser(context.Background(), "ab", "password123", false) // too short
	if err == nil {
		t.Error("expected error for short username")
	}
}

func TestUserStore_CreateUser_ShortPassword(t *testing.T) {
	s := newUserStore(&mockPool{})
	_, err := s.CreateUser(context.Background(), "valid-user", "abc", false)
	if err == nil {
		t.Error("expected error for short password")
	}
}

func TestUserStore_CreateUser_DuplicateUsername(t *testing.T) {
	s := newUserStore(&mockPool{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
		return errRow(errors.New("unique violation"))
	}})
	_, err := s.CreateUser(context.Background(), "alice", "password123", false)
	if err == nil {
		t.Error("expected error for duplicate username")
	}
}

func TestUserStore_UpdateUser_NotFound(t *testing.T) {
	s := newUserStore(&mockPool{}) // GetUserByID returns nil
	_, err := s.UpdateUser(context.Background(), 99, nil, nil, nil, nil)
	if err == nil {
		t.Error("expected error when user not found")
	}
}

func TestUserStore_DeleteUser_NotFound(t *testing.T) {
	s := newUserStore(&mockPool{}) // GetUserByID returns nil
	err := s.DeleteUser(context.Background(), 999)
	if err == nil {
		t.Error("expected error when user not found")
	}
}

func TestUserStore_DeleteUser_BootstrapAdmin(t *testing.T) {
	s := newUserStore(&mockPool{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: userScanFn(BootstrapAdminID, "admin", "hash", true, false)}
	}})
	if err := s.DeleteUser(context.Background(), BootstrapAdminID); err == nil {
		t.Error("expected error when deleting bootstrap admin")
	}
}

func TestUserStore_ChangePassword_UserNotFound(t *testing.T) {
	s := newUserStore(&mockPool{})
	_, err := s.ChangePassword(context.Background(), 99, "current", "newpass")
	if err == nil {
		t.Error("expected error when user not found")
	}
}

func TestUserStore_Authenticate_UserNotFound(t *testing.T) {
	s := newUserStore(&mockPool{})
	u, err := s.Authenticate(context.Background(), "nobody", "password")
	if err != nil || u != nil {
		t.Errorf("expected nil,nil for unknown user; got %v, %v", u, err)
	}
}

func TestUserStore_EnsureDefaultAdmin_AlreadyExists(t *testing.T) {
	rows := &mockRows{scanFns: []func(dest ...any) error{
		func(dest ...any) error {
			*dest[0].(*int) = 1
			*dest[1].(*string) = "admin"
			*dest[2].(*string) = "hash"
			*dest[3].(*bool) = true
			*dest[4].(*bool) = false
			*dest[5].(*time.Time) = time.Now()
			return nil
		},
	}}
	s := newUserStore(&mockPool{queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
		return rows, nil
	}})
	if err := s.EnsureDefaultAdmin(context.Background(), DefaultAdminPassword); err != nil {
		t.Fatalf("EnsureDefaultAdmin: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CredentialStore tests
// ---------------------------------------------------------------------------

func newCredStore(pool dbPool) *CredentialStore { return &CredentialStore{pool: pool} }

func TestCredentialStore_EnsureSchema(t *testing.T) {
	s := newCredStore(&mockPool{})
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
}

func TestCredentialStore_Revoke_EmptyHash(t *testing.T) {
	s := newCredStore(&mockPool{})
	n, err := s.Revoke(context.Background(), "")
	if err != nil || n != 0 {
		t.Errorf("Revoke empty: %v, %v", n, err)
	}
}

func TestCredentialStore_Revoke_Success(t *testing.T) {
	s := newCredStore(&mockPool{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("DELETE 1"), nil
	}})
	n, err := s.Revoke(context.Background(), "somehash")
	if err != nil || n != 1 {
		t.Errorf("Revoke: %v, %v", n, err)
	}
}

func TestCredentialStore_CleanupExpired(t *testing.T) {
	s := newCredStore(&mockPool{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("DELETE 5"), nil
	}})
	n, err := s.CleanupExpired(context.Background())
	if err != nil || n != 5 {
		t.Errorf("CleanupExpired: %v, %v", n, err)
	}
}

func TestCredentialStore_Mint_Success(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	s := newCredStore(&mockPool{})
	token, err := s.Mint(context.Background(), priv, 30)
	if err != nil || token == "" {
		t.Errorf("Mint: %q, %v", token, err)
	}
}

func TestCredentialStore_Verify_Revoked(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	keyDir := t.TempDir()
	keyPath := keyDir + "/issuer.key"
	storedPub, _, err := signing.LoadOrCreateIssuerKeypair(keyPath)
	// Use a fresh key to ensure no collision with the test key
	_ = storedPub

	// Create a valid token with our key
	token, _ := signing.MintCredential(priv, 1)

	// Pool returns no rows (revoked / not in DB)
	s := newCredStore(&mockPool{})
	_, err = s.Verify(context.Background(), token, pub)
	if err == nil {
		t.Error("expected error for revoked/not-found credential")
	}
}

// ---------------------------------------------------------------------------
// SettingsStore tests
// ---------------------------------------------------------------------------

func newSettingsStore(pool dbPool) *SettingsStore { return &SettingsStore{pool: pool} }

func TestSettingsStore_EnsureSchema(t *testing.T) {
	s := newSettingsStore(&mockPool{})
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
}

func TestSettingsStore_Load_NotFound(t *testing.T) {
	s := newSettingsStore(&mockPool{})
	p, err := s.Load(context.Background())
	if err != nil || p != nil {
		t.Errorf("expected nil,nil; got %v, %v", p, err)
	}
}

func TestSettingsStore_Load_Found(t *testing.T) {
	payload := config.SettingsPayload{RetentionKeepLast: 7}
	raw, _ := json.Marshal(payload)
	s := newSettingsStore(&mockPool{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*[]byte) = raw
			return nil
		}}
	}})
	p, err := s.Load(context.Background())
	if err != nil || p == nil || p.RetentionKeepLast != 7 {
		t.Errorf("Load: %v, %v", p, err)
	}
}

func TestSettingsStore_Load_DBError(t *testing.T) {
	s := newSettingsStore(&mockPool{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
		return errRow(errors.New("db error"))
	}})
	_, err := s.Load(context.Background())
	if err == nil {
		t.Error("expected db error to propagate")
	}
}

func TestSettingsStore_Load_InvalidJSON(t *testing.T) {
	s := newSettingsStore(&mockPool{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*[]byte) = []byte("{invalid json")
			return nil
		}}
	}})
	_, err := s.Load(context.Background())
	if err == nil {
		t.Error("expected JSON parse error")
	}
}

func TestSettingsStore_Save_Success(t *testing.T) {
	s := newSettingsStore(&mockPool{})
	p := config.SettingsPayload{RetentionKeepLast: 7}
	if err := s.Save(context.Background(), &p); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func TestSettingsStore_Save_ExecError(t *testing.T) {
	s := newSettingsStore(&mockPool{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("exec error")
	}})
	if err := s.Save(context.Background(), &config.SettingsPayload{}); err == nil {
		t.Error("expected exec error")
	}
}
