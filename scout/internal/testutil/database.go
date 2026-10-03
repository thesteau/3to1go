// Package testutil provides helpers for unit tests. It must not be imported by runtime code.
package testutil

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// MockDB returns a database handle backed by a mock driver, with no SQLite files
// or server connection. Every expected operation must be consumed by the test.
func MockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("database expectations: %v", err)
		}
		mock.ExpectClose()
		if err := db.Close(); err != nil {
			t.Errorf("close mock database: %v", err)
		}
	})
	return db, mock
}
