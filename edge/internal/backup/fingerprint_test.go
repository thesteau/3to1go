package backup

import (
	"os"
	"path/filepath"
	"testing"
)

// Same-size edits are intentionally invisible to automatic change detection.
// Force Backup is the supported way to capture them without a path/size change.
func TestFingerprintIntentionallyIgnoresSameSizeContentEdits(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.txt")
	job := &JobDefinition{RootPath: root, JobName: "job", IncludeHidden: true}
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := BuildFileList(job, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("world"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := BuildFileList(job, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 || len(after) != 1 {
		t.Fatal("expected one discovered file")
	}
	if ComputeFingerprint(before) != ComputeFingerprint(after) {
		t.Fatal("path-and-size fingerprint changed for a same-size edit")
	}
}

func TestComputeFingerprint_Deterministic(t *testing.T) {
	files := []*DiscoveredFile{
		{ArchivePath: "a.txt", Size: 100},
		{ArchivePath: "b.txt", Size: 200},
	}
	if ComputeFingerprint(files) != ComputeFingerprint(files) {
		t.Error("fingerprint is not deterministic")
	}
}

func TestComputeFingerprint_OrderInvariant(t *testing.T) {
	a := []*DiscoveredFile{
		{ArchivePath: "a.txt", Size: 100},
		{ArchivePath: "b.txt", Size: 200},
	}
	b := []*DiscoveredFile{
		{ArchivePath: "b.txt", Size: 200},
		{ArchivePath: "a.txt", Size: 100},
	}
	if ComputeFingerprint(a) != ComputeFingerprint(b) {
		t.Error("fingerprint must be order-invariant")
	}
}

func TestComputeFingerprint_DifferentPaths(t *testing.T) {
	a := []*DiscoveredFile{{ArchivePath: "a.txt", Size: 100}}
	b := []*DiscoveredFile{{ArchivePath: "b.txt", Size: 100}}
	if ComputeFingerprint(a) == ComputeFingerprint(b) {
		t.Error("different file paths should produce different fingerprints")
	}
}

func TestComputeFingerprint_SamePathDifferentSize(t *testing.T) {
	a := []*DiscoveredFile{{ArchivePath: "a.txt", Size: 100}}
	b := []*DiscoveredFile{{ArchivePath: "a.txt", Size: 101}}
	if ComputeFingerprint(a) == ComputeFingerprint(b) {
		t.Error("same path with different size should produce different fingerprints")
	}
}

func TestComputeFingerprint_EmptyIsConsistent(t *testing.T) {
	fp1 := ComputeFingerprint(nil)
	fp2 := ComputeFingerprint([]*DiscoveredFile{})
	if fp1 == "" {
		t.Error("fingerprint of empty list should not be empty string")
	}
	if fp1 != fp2 {
		t.Error("fingerprint of nil and empty slice should be equal")
	}
}

func TestComputeFingerprint_ExtraFileChangesResult(t *testing.T) {
	a := []*DiscoveredFile{
		{ArchivePath: "a.txt", Size: 100},
	}
	b := []*DiscoveredFile{
		{ArchivePath: "a.txt", Size: 100},
		{ArchivePath: "b.txt", Size: 50},
	}
	if ComputeFingerprint(a) == ComputeFingerprint(b) {
		t.Error("adding a file should change the fingerprint")
	}
}

func TestComputeFingerprint_IsHex(t *testing.T) {
	fp := ComputeFingerprint([]*DiscoveredFile{{ArchivePath: "x", Size: 1}})
	if len(fp) != 64 {
		t.Errorf("fingerprint length = %d, want 64 (SHA-256 hex)", len(fp))
	}
}
