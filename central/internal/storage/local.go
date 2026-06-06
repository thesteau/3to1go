package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type StorageFile struct {
	Filename  string
	Path      string
	SizeBytes int64
	Mtime     float64
}

type LocalBackend struct {
	BackupRoot string
	probePath  string
}

func NewLocalBackend(backupRoot string) *LocalBackend {
	probeRoot := filepath.Join(os.TempDir(), "3to1go", "central", "healthchecks")
	sum := sha256.Sum256([]byte(backupRoot))
	probeName := hex.EncodeToString(sum[:])[:16]
	return &LocalBackend{
		BackupRoot: backupRoot,
		probePath:  filepath.Join(probeRoot, probeName+".healthcheck"),
	}
}

func (b *LocalBackend) Store(namespace, filename string, stagedPath string) (string, error) {
	targetDir := filepath.Join(b.BackupRoot, namespace)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	finalPath := filepath.Join(targetDir, filename)

	if err := os.Rename(stagedPath, finalPath); err != nil {
		if !isCrossDeviceError(err) {
			return "", fmt.Errorf("move staged file: %w", err)
		}
		if err := copyAcrossFilesystems(stagedPath, finalPath); err != nil {
			return "", fmt.Errorf("copy staged file: %w", err)
		}
	}
	return filename, nil
}

func (b *LocalBackend) List(namespace string) ([]StorageFile, error) {
	dir := filepath.Join(b.BackupRoot, namespace)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []StorageFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, StorageFile{
			Filename:  e.Name(),
			Path:      filepath.Join(dir, e.Name()),
			SizeBytes: info.Size(),
			Mtime:     float64(info.ModTime().UnixNano()) / 1e9,
		})
	}
	return files, nil
}

func (b *LocalBackend) Open(namespace, filename string) (io.ReadCloser, error) {
	path := filepath.Join(b.BackupRoot, namespace, filename)
	return os.Open(path)
}

func (b *LocalBackend) Delete(namespace, filename string) error {
	path := filepath.Join(b.BackupRoot, namespace, filename)
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (b *LocalBackend) Healthcheck() bool {
	info, err := os.Stat(b.BackupRoot)
	if err != nil || !info.IsDir() {
		return false
	}
	probeDir := filepath.Dir(b.probePath)
	if err := os.MkdirAll(probeDir, 0o755); err != nil {
		return false
	}
	f, err := os.OpenFile(b.probePath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	defer f.Close()
	_, err = f.WriteAt([]byte("ok\n"), 0)
	return err == nil
}

func copyAcrossFilesystems(src, dst string) error {
	tmpPath := dst + ".tmp"
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return err
	}
	out.Close()

	if err := os.Rename(tmpPath, dst); err != nil {
		os.Remove(tmpPath)
		return err
	}
	os.Remove(src)
	return nil
}

func isCrossDeviceError(err error) bool {
	linkErr, ok := errors.AsType[*os.LinkError](err)
	return ok && linkErr.Err == crossDeviceError
}

// DiskInfo returns total filesystem capacity, bytes used by the backup directory tree,
// and free bytes on the filesystem — all relative to BackupRoot.
func (b *LocalBackend) DiskInfo() (total, used, free int64) {
	total, _, free, err := DiskUsage(b.BackupRoot)
	if err != nil {
		total, free = 0, 0
	}
	used = DirSize(b.BackupRoot)
	return total, used, free
}

// DiskUsage returns (total, used, free) for the filesystem containing path.
func DiskUsage(path string) (total, used, free int64, err error) {
	return diskUsage(path)
}

// DirSize sums sizes of all regular files under path recursively.
func DirSize(path string) int64 {
	var total int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}
