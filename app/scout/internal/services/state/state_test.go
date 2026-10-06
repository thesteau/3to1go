package state

import (
	"context"
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/3to1go/scout/internal/testutil"
	"github.com/DATA-DOG/go-sqlmock"
)

func newTestStore(t *testing.T) (*StateStore, sqlmock.Sqlmock) {
	t.Helper()
	db, mock := testutil.MockDB(t)
	return NewStateStore(db), mock
}

func stateValues(key string, js JobState) []driver.Value {
	var pending, chunk, size driver.Value
	if js.PendingArchiveSize != nil {
		pending = *js.PendingArchiveSize
	}
	if js.CurrentChunkSizeBytes != nil {
		chunk = *js.CurrentChunkSizeBytes
	}
	if js.LastBackupSizeBytes != nil {
		size = *js.LastBackupSizeBytes
	}
	return []driver.Value{
		key, js.JobName, js.LastSuccessfulFingerprint, js.LastSuccessfulUpload,
		js.PendingArchive, pending, js.PendingArchiveSHA256, js.PendingFingerprint,
		js.PendingTimestamp, js.UploadID, js.UploadOffset, js.UploadAttemptCount,
		chunk, js.NextRetryAt, js.LastErrorDetail, js.LastErrorCategory,
		js.LastUploadStartedAt, js.LastUploadUpdatedAt, js.ActivePhase, js.ActivePhasePercent,
		boolToInt(js.ManualInterventionRequired), js.LastStatus, js.LastStoredAs,
		js.LastPruned, boolToInt(js.LastDuplicate), size,
	}
}

func stateRows() *sqlmock.Rows {
	return sqlmock.NewRows(strings.FieldsFunc(selectCols, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }))
}

func TestStateStore_EnsureSchema(t *testing.T) {
	s, mock := newTestStore(t)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS job_states").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	dbErr := errors.New("schema failed")
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS job_states").WillReturnError(dbErr)
	if err := s.EnsureSchema(context.Background()); !errors.Is(err, dbErr) {
		t.Fatalf("EnsureSchema: %v", err)
	}
}

func TestStateStore_GetMissingKey_ReturnsZeroValue(t *testing.T) {
	s, mock := newTestStore(t)
	mock.ExpectQuery("SELECT .* FROM job_states WHERE key =").WithArgs("missing").WillReturnRows(stateRows())
	if got := s.Get("missing"); !reflect.DeepEqual(got, JobState{}) {
		t.Fatalf("Get: %+v", got)
	}
}

func TestStateStore_SetAndGet(t *testing.T) {
	s, mock := newTestStore(t)
	value := int64(123)
	st := JobState{
		JobName: "photos", LastStatus: "success", LastSuccessfulFingerprint: "fingerprint",
		LastSuccessfulUpload: "uploaded", PendingArchive: "/spool/photos.tar.zst",
		PendingArchiveSize: &value, PendingArchiveSHA256: "sha256", PendingFingerprint: "pending",
		PendingTimestamp: "timestamp", UploadID: "upload", UploadOffset: 12, UploadAttemptCount: 3,
		CurrentChunkSizeBytes: &value, NextRetryAt: "retry", LastErrorDetail: "detail",
		LastErrorCategory: "network", LastUploadStartedAt: "started", LastUploadUpdatedAt: "updated",
		ActivePhase: "upload", ActivePhasePercent: 50, ManualInterventionRequired: true,
		LastStoredAs: "stored", LastPruned: 2, LastDuplicate: true, LastBackupSizeBytes: &value,
	}
	mock.ExpectExec("INSERT INTO job_states").WithArgs(stateValues("photos", st)...).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.Set("photos", st); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT .* FROM job_states WHERE key =").WithArgs("photos").WillReturnRows(stateRows().AddRow(stateValues("photos", st)...))
	if got := s.Get("photos"); !reflect.DeepEqual(got, st) {
		t.Fatalf("Get: %+v, want %+v", got, st)
	}
}

func TestStateStore_GetNullSizes(t *testing.T) {
	s, mock := newTestStore(t)
	st := JobState{JobName: "photos"}
	mock.ExpectQuery("SELECT .* FROM job_states WHERE key =").WithArgs("photos").WillReturnRows(stateRows().AddRow(stateValues("photos", st)...))
	got := s.Get("photos")
	if got.PendingArchiveSize != nil || got.CurrentChunkSizeBytes != nil || got.LastBackupSizeBytes != nil {
		t.Fatalf("Get null sizes: %+v", got)
	}
}

func TestStateStore_Delete(t *testing.T) {
	for _, affected := range []int64{0, 1} {
		s, mock := newTestStore(t)
		mock.ExpectExec("DELETE FROM job_states WHERE key =").WithArgs("photos").WillReturnResult(sqlmock.NewResult(0, affected))
		if err := s.Delete("photos"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStateStore_ReferencedPendingArchives(t *testing.T) {
	s, mock := newTestStore(t)
	mock.ExpectQuery("SELECT pending_archive FROM job_states").WillReturnRows(sqlmock.NewRows([]string{"pending_archive"}).AddRow("/spool/photos.tar.zst").AddRow("/spool/docs.tar.zst").AddRow("")).RowsWillBeClosed()
	want := map[string]bool{"/spool/photos.tar.zst": true, "/spool/docs.tar.zst": true}
	if got := s.ReferencedPendingArchives(); !reflect.DeepEqual(got, want) {
		t.Fatalf("refs: %v", got)
	}
}

func TestStateStore_Snapshot_ReturnsCopy(t *testing.T) {
	s, mock := newTestStore(t)
	st := JobState{LastStatus: "success"}
	mock.ExpectQuery("SELECT .* FROM job_states$").WillReturnRows(stateRows().AddRow(stateValues("photos", st)...)).RowsWillBeClosed()
	snap := s.Snapshot()
	if !reflect.DeepEqual(snap, map[string]JobState{"photos": st}) {
		t.Fatalf("Snapshot: %+v", snap)
	}
	delete(snap, "photos")
	mock.ExpectQuery("SELECT .* FROM job_states WHERE key =").WithArgs("photos").WillReturnRows(stateRows().AddRow(stateValues("photos", st)...))
	if got := s.Get("photos"); got.LastStatus != "success" {
		t.Fatalf("Get after snapshot mutation: %+v", got)
	}
}

func TestStateStore_ClearManualInterventions(t *testing.T) {
	s, mock := newTestStore(t)
	mock.ExpectExec("UPDATE job_states .*WHERE manual_intervention_required = 1").WillReturnResult(sqlmock.NewResult(0, 2))
	if count, err := s.ClearManualInterventions(); err != nil || count != 2 {
		t.Fatalf("clear: %d, %v", count, err)
	}
}

func TestStateStore_ClearManualIntervention(t *testing.T) {
	for _, affected := range []int64{0, 1} {
		s, mock := newTestStore(t)
		mock.ExpectExec("UPDATE job_states .*WHERE key =").WithArgs("photos").WillReturnResult(sqlmock.NewResult(0, affected))
		if cleared, err := s.ClearManualIntervention("photos"); err != nil || cleared != (affected == 1) {
			t.Fatalf("clear: %v, %v", cleared, err)
		}
	}
}

func TestStateStore_ReportsReadErrors(t *testing.T) {
	for _, operation := range []string{"get", "snapshot", "archives"} {
		t.Run(operation, func(t *testing.T) {
			s, mock := newTestStore(t)
			dbErr := errors.New("read failed")
			var reported error
			s.SetErrorHandler(func(err error) { reported = err })
			mock.ExpectQuery("SELECT").WillReturnError(dbErr)
			switch operation {
			case "get":
				s.Get("photos")
			case "snapshot":
				s.Snapshot()
			case "archives":
				s.ReferencedPendingArchives()
			}
			if !errors.Is(reported, dbErr) {
				t.Fatalf("reported: %v", reported)
			}
		})
	}
}

func TestStateStore_WriteErrors(t *testing.T) {
	for _, operation := range []string{"set", "delete", "clear_all", "clear_one"} {
		t.Run(operation, func(t *testing.T) {
			s, mock := newTestStore(t)
			dbErr := errors.New("write failed")
			mock.ExpectExec("INSERT|DELETE|UPDATE").WillReturnError(dbErr)
			var err error
			switch operation {
			case "set":
				err = s.Set("photos", JobState{})
			case "delete":
				err = s.Delete("photos")
			case "clear_all":
				_, err = s.ClearManualInterventions()
			case "clear_one":
				_, err = s.ClearManualIntervention("photos")
			}
			if !errors.Is(err, dbErr) {
				t.Fatalf("error: %v", err)
			}
		})
	}
}

func TestStateStore_MigrateFromFile(t *testing.T) {
	s, mock := newTestStore(t)
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"photos":{"job_name":"photos","last_status":"success"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st := JobState{JobName: "photos", LastStatus: "success"}
	mock.ExpectExec("INSERT INTO job_states").WithArgs(stateValues("photos", st)...).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.MigrateFromFile(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".migrated"); err != nil {
		t.Fatal(err)
	}
	if err := s.MigrateFromFile(path); err != nil {
		t.Fatal(err)
	}
}

func TestStateStore_MigrateFromFileRetainsSourceOnWriteError(t *testing.T) {
	s, mock := newTestStore(t)
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"photos":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dbErr := errors.New("write failed")
	mock.ExpectExec("INSERT INTO job_states").WillReturnError(dbErr)
	if err := s.MigrateFromFile(path); !errors.Is(err, dbErr) {
		t.Fatalf("migration: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
