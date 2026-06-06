package backup

import (
	"archive/tar"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func TestArchiveTimestampAndNameHelpers(t *testing.T) {
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("offset", -8*60*60))
	if got := TimestampForFilename(ts); got != "2024-01-02T11-04-05Z" {
		t.Fatalf("TimestampForFilename = %q", got)
	}
	if got := TimestampForAPI(ts); got != "2024-01-02T11:04:05Z" {
		t.Fatalf("TimestampForAPI = %q", got)
	}
	if got := BuildArchiveName("job", ts, "abcdef1234567890"); got != "job__2024-01-02T11-04-05Z__abcdef12.tar.zst" {
		t.Fatalf("BuildArchiveName = %q", got)
	}
	if got := BuildArchiveName("job", ts, "abc"); got != "job__2024-01-02T11-04-05Z__abc.tar.zst" {
		t.Fatalf("BuildArchiveName short fp = %q", got)
	}
}

func TestCreateListAndExtractArchive(t *testing.T) {
	root := t.TempDir()
	srcA := filepath.Join(root, "src", "a.txt")
	srcB := filepath.Join(root, "src", "nested", "b.txt")
	if err := os.MkdirAll(filepath.Dir(srcB), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(srcA, []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(srcB, []byte("bravo"), 0o644); err != nil {
		t.Fatalf("write b: %v", err)
	}
	files := []*DiscoveredFile{
		{SourcePath: srcB, ArchivePath: "nested/b.txt", Size: 5, MtimeNs: time.Unix(20, 0).UnixNano()},
		{SourcePath: srcA, ArchivePath: "a.txt", Size: 5, MtimeNs: time.Unix(10, 0).UnixNano()},
	}
	archivePath := filepath.Join(root, "spool", "archive.tar.zst")
	if err := CreateArchive(archivePath, files); err != nil {
		t.Fatalf("CreateArchive: %v", err)
	}

	target := filepath.Join(root, "target")
	if err := os.MkdirAll(filepath.Join(target, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "a.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write old: %v", err)
	}
	preview, err := ListArchiveEntries(archivePath, target)
	if err != nil {
		t.Fatalf("ListArchiveEntries: %v", err)
	}
	if preview["total_files"] != 2 || preview["replace_count"] != 1 || preview["add_count"] != 1 {
		t.Fatalf("preview = %+v", preview)
	}

	count, err := ExtractArchive(archivePath, target)
	if err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
	if count != 2 {
		t.Fatalf("ExtractArchive count = %d", count)
	}
	if got, err := os.ReadFile(filepath.Join(target, "a.txt")); err != nil || string(got) != "alpha" {
		t.Fatalf("restored a = %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(target, "nested", "b.txt")); err != nil || string(got) != "bravo" {
		t.Fatalf("restored b = %q, %v", got, err)
	}
}

func TestCreateArchiveMissingSourceFails(t *testing.T) {
	err := CreateArchive(filepath.Join(t.TempDir(), "archive.tar.zst"), []*DiscoveredFile{
		{SourcePath: filepath.Join(t.TempDir(), "missing"), ArchivePath: "missing.txt"},
	})
	if err == nil {
		t.Fatal("expected missing source error")
	}
}

func TestArchiveRejectsUnsafeOrUnsupportedEntries(t *testing.T) {
	root := t.TempDir()
	unsafeArchive := filepath.Join(root, "unsafe.tar.zst")
	if err := writeTestArchive(unsafeArchive, []*tar.Header{{Name: "../escape.txt", Size: 1, Typeflag: tar.TypeReg}}, [][]byte{[]byte("x")}); err != nil {
		t.Fatalf("write unsafe archive: %v", err)
	}
	if _, err := ListArchiveEntries(unsafeArchive, root); err == nil {
		t.Fatal("expected unsafe preview error")
	}
	if _, err := ExtractArchive(unsafeArchive, root); err == nil {
		t.Fatal("expected unsafe extract error")
	}

	dirOnlyArchive := filepath.Join(root, "dir.tar.zst")
	if err := writeTestArchive(dirOnlyArchive, []*tar.Header{{Name: "dir", Typeflag: tar.TypeDir}}, nil); err != nil {
		t.Fatalf("write dir archive: %v", err)
	}
	if preview, err := ListArchiveEntries(dirOnlyArchive, root); err != nil || preview["total_files"] != 0 {
		t.Fatalf("dir preview = %+v, %v", preview, err)
	}
	if count, err := ExtractArchive(dirOnlyArchive, root); err != nil || count != 0 {
		t.Fatalf("dir extract = %d, %v", count, err)
	}

	linkArchive := filepath.Join(root, "link.tar.zst")
	if err := writeTestArchive(linkArchive, []*tar.Header{{Name: "link", Typeflag: tar.TypeSymlink}}, nil); err != nil {
		t.Fatalf("write link archive: %v", err)
	}
	if _, err := ListArchiveEntries(linkArchive, root); err == nil {
		t.Fatal("expected unsupported preview error")
	}
	if _, err := ExtractArchive(linkArchive, root); err == nil {
		t.Fatal("expected unsupported extract error")
	}
}

func TestArchiveDestination(t *testing.T) {
	root := t.TempDir()
	got, err := archiveDestination(root, "nested/file.txt")
	if err != nil {
		t.Fatalf("archiveDestination: %v", err)
	}
	if got != filepath.Join(root, "nested", "file.txt") {
		t.Fatalf("destination = %q", got)
	}
	for _, name := range []string{"../escape.txt", "..\\escape.txt"} {
		if _, err := archiveDestination(root, name); err == nil {
			t.Fatalf("expected invalid entry for %q", name)
		}
	}
}

func writeTestArchive(path string, headers []*tar.Header, bodies [][]byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc, err := zstd.NewWriter(f)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(enc)
	for i, hdr := range headers {
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if i < len(bodies) {
			if _, err := tw.Write(bodies[i]); err != nil {
				return err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return enc.Close()
}
