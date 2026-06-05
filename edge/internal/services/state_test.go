package services

import (
	"testing"
)

// ---------------------------------------------------------------------------
// StateStore
// ---------------------------------------------------------------------------

func TestStateStore_GetMissingKey_ReturnsZeroValue(t *testing.T) {
	s, err := NewStateStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}
	got := s.Get("/no/such/path")
	if got.LastStatus != "" {
		t.Errorf("expected zero-value JobState, got %+v", got)
	}
}

func TestStateStore_SetAndGet(t *testing.T) {
	s, err := NewStateStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}
	st := JobState{LastStatus: "success", JobName: "photos"}
	if err := s.Set("/data/photos", st); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got := s.Get("/data/photos")
	if got.LastStatus != "success" {
		t.Errorf("LastStatus = %q, want success", got.LastStatus)
	}
	if got.JobName != "photos" {
		t.Errorf("JobName = %q, want photos", got.JobName)
	}
}

func TestStateStore_DeleteRemovesKey(t *testing.T) {
	s, err := NewStateStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}
	s.Set("/data/photos", JobState{LastStatus: "success"})
	if err := s.Delete("/data/photos"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got := s.Get("/data/photos")
	if got.LastStatus != "" {
		t.Errorf("expected zero-value after delete, got %+v", got)
	}
}

func TestStateStore_DeleteNonExistentKey_NoError(t *testing.T) {
	s, err := NewStateStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}
	if err := s.Delete("/no/such/path"); err != nil {
		t.Errorf("Delete missing key should not error, got %v", err)
	}
}

func TestStateStore_ReferencedPendingArchives(t *testing.T) {
	s, err := NewStateStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}
	s.Set("/data/photos", JobState{PendingArchive: "/spool/photos.tar.zst"})
	s.Set("/data/docs", JobState{PendingArchive: "/spool/docs.tar.zst"})
	s.Set("/data/empty", JobState{})

	refs := s.ReferencedPendingArchives()
	if !refs["/spool/photos.tar.zst"] {
		t.Error("expected /spool/photos.tar.zst in refs")
	}
	if !refs["/spool/docs.tar.zst"] {
		t.Error("expected /spool/docs.tar.zst in refs")
	}
	if refs["/no/archive"] {
		t.Error("expected /no/archive NOT in refs")
	}
}

func TestStateStore_Snapshot_ReturnsCopy(t *testing.T) {
	s, err := NewStateStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}
	s.Set("/data/photos", JobState{LastStatus: "success"})
	snap := s.Snapshot()
	if len(snap) != 1 {
		t.Errorf("snapshot len = %d, want 1", len(snap))
	}
	// Mutating the snapshot should not affect the store.
	delete(snap, "/data/photos")
	got := s.Get("/data/photos")
	if got.LastStatus != "success" {
		t.Error("snapshot mutation affected the store")
	}
}

func TestStateStore_ClearManualInterventions(t *testing.T) {
	s, err := NewStateStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}
	s.Set("/data/a", JobState{ManualInterventionRequired: true, LastStatus: "manual_intervention_required"})
	s.Set("/data/b", JobState{ManualInterventionRequired: true, LastStatus: "manual_intervention_required"})
	s.Set("/data/c", JobState{ManualInterventionRequired: false, LastStatus: "success"})

	count, err := s.ClearManualInterventions()
	if err != nil {
		t.Fatalf("ClearManualInterventions: %v", err)
	}
	if count != 2 {
		t.Errorf("cleared = %d, want 2", count)
	}
	for _, key := range []string{"/data/a", "/data/b"} {
		st := s.Get(key)
		if st.ManualInterventionRequired {
			t.Errorf("%s: ManualInterventionRequired should be false after clear", key)
		}
		if st.LastStatus != "manual_retry_requested" {
			t.Errorf("%s: LastStatus = %q, want manual_retry_requested", key, st.LastStatus)
		}
	}
}

func TestStateStore_ClearManualIntervention_Single(t *testing.T) {
	s, err := NewStateStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}
	s.Set("/data/a", JobState{ManualInterventionRequired: true})

	cleared, err := s.ClearManualIntervention("/data/a")
	if err != nil {
		t.Fatalf("ClearManualIntervention: %v", err)
	}
	if !cleared {
		t.Error("expected cleared=true")
	}
	st := s.Get("/data/a")
	if st.ManualInterventionRequired {
		t.Error("ManualInterventionRequired should be false")
	}
}

func TestStateStore_ClearManualIntervention_NotPending(t *testing.T) {
	s, err := NewStateStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}
	s.Set("/data/a", JobState{ManualInterventionRequired: false})
	cleared, err := s.ClearManualIntervention("/data/a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cleared {
		t.Error("expected cleared=false for non-pending job")
	}
}

func TestStateStore_PersistenceAcrossReload(t *testing.T) {
	dir := t.TempDir()
	s1, err := NewStateStore(dir)
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}
	s1.Set("/data/photos", JobState{LastStatus: "success", JobName: "photos"})

	// Reload from same directory.
	s2, err := NewStateStore(dir)
	if err != nil {
		t.Fatalf("NewStateStore reload: %v", err)
	}
	got := s2.Get("/data/photos")
	if got.LastStatus != "success" {
		t.Errorf("after reload: LastStatus = %q, want success", got.LastStatus)
	}
	if got.JobName != "photos" {
		t.Errorf("after reload: JobName = %q, want photos", got.JobName)
	}
}
