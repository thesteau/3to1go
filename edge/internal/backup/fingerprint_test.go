package backup

import (
	"testing"
)

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
