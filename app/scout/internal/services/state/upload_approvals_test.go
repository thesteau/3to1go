package state

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"testing"

	"github.com/3to1go/scout/internal/testutil"
	"github.com/DATA-DOG/go-sqlmock"
)

func approvalValues(a UploadApproval) []driver.Value {
	return []driver.Value{a.ID, a.JobPath, a.JobName, a.ScoutID, a.StationURL, a.ApprovedAt}
}

func approvalRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "job_path", "job_name", "scout_id", "station_url", "approved_at"})
}

func TestUploadApprovalStoreLifecycle(t *testing.T) {
	db, mock := testutil.MockDB(t)
	s := NewUploadApprovalStore(db)
	ctx := context.Background()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS upload_size_approvals").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT .* WHERE id =").WithArgs("missing").WillReturnRows(approvalRows())
	if got, err := s.Get(ctx, "missing"); err != nil || got != nil {
		t.Fatalf("missing approval: %+v, %v", got, err)
	}
	a := UploadApproval{ID: "id", JobPath: "/scan/docs", JobName: "docs", ScoutID: "scout", StationURL: "https://station.example", ApprovedAt: "2026-10-13T00:00:00Z"}
	mock.ExpectExec("INSERT INTO upload_size_approvals").WithArgs(approvalValues(a)...).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT .* WHERE id =").WithArgs(a.ID).WillReturnRows(approvalRows().AddRow(approvalValues(a)...))
	if got, err := s.Get(ctx, a.ID); err != nil || !reflect.DeepEqual(got, &a) {
		t.Fatalf("approval: %+v, %v", got, err)
	}
	mock.ExpectQuery("SELECT .* ORDER BY").WillReturnRows(approvalRows().AddRow(approvalValues(a)...)).RowsWillBeClosed()
	if got, err := s.List(ctx); err != nil || !reflect.DeepEqual(got, []UploadApproval{a}) {
		t.Fatalf("list: %+v, %v", got, err)
	}
	mock.ExpectExec("DELETE FROM upload_size_approvals").WithArgs(a.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.Delete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT .* ORDER BY").WillReturnRows(approvalRows()).RowsWillBeClosed()
	if got, err := s.List(ctx); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty list: %+v, %v", got, err)
	}
}

func TestUploadApprovalStoreReportsDatabaseErrors(t *testing.T) {
	db, mock := testutil.MockDB(t)
	s := NewUploadApprovalStore(db)
	ctx := context.Background()
	dbErr := errors.New("database unavailable")
	mock.ExpectExec("CREATE TABLE").WillReturnError(dbErr)
	if err := s.EnsureSchema(ctx); !errors.Is(err, dbErr) {
		t.Fatalf("schema error: %v", err)
	}
	mock.ExpectQuery("SELECT .* WHERE id =").WithArgs("id").WillReturnError(dbErr)
	if _, err := s.Get(ctx, "id"); !errors.Is(err, dbErr) {
		t.Fatalf("get error: %v", err)
	}
	mock.ExpectExec("INSERT INTO").WillReturnError(dbErr)
	if err := s.Save(ctx, UploadApproval{}); !errors.Is(err, dbErr) {
		t.Fatalf("save error: %v", err)
	}
	mock.ExpectQuery("SELECT .* ORDER BY").WillReturnError(dbErr)
	if _, err := s.List(ctx); !errors.Is(err, dbErr) {
		t.Fatalf("list error: %v", err)
	}
	mock.ExpectExec("DELETE FROM").WithArgs("id").WillReturnError(dbErr)
	if err := s.Delete(ctx, "id"); !errors.Is(err, dbErr) {
		t.Fatalf("delete error: %v", err)
	}
}
