package encryption

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type failingKeyFile struct {
	*os.File
	phase   string
	failure error
}

func (f *failingKeyFile) Write(data []byte) (int, error) {
	if f.phase == "write" || f.phase == "short write" {
		n, err := f.File.Write(data[:7])
		if err != nil {
			return n, err
		}
		if f.phase == "write" {
			return n, f.failure
		}
		return n, nil
	}
	return f.File.Write(data)
}

func (f *failingKeyFile) Sync() error {
	if f.phase == "sync" {
		return f.failure
	}
	return f.File.Sync()
}

func (f *failingKeyFile) Close() error {
	err := f.File.Close()
	if f.phase == "close" {
		return errors.Join(err, f.failure)
	}
	return err
}

func TestLoadOrCreateCleansUpFailedCreationAndCanRetry(t *testing.T) {
	for _, phase := range []string{"write", "short write", "sync", "close"} {
		t.Run(phase, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "encryption.key")
			failure := errors.New("injected key creation failure")
			want := failure
			if phase == "short write" {
				want = io.ErrShortWrite
			}
			key, err := loadOrCreate(path, func(path string) (keyFile, error) {
				f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
				if err != nil {
					return nil, err
				}
				return &failingKeyFile{File: f, phase: phase, failure: failure}, nil
			})
			if key != nil || !errors.Is(err, want) {
				t.Fatalf("key = %x, error = %v, want %v", key, err, want)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("failed key left behind: %v", err)
			}
			key, err = LoadOrCreate(path)
			if err != nil || len(key) != 32 {
				t.Fatalf("retry failed: %v", err)
			}
			again, err := LoadOrCreate(path)
			if err != nil || !bytes.Equal(key, again) {
				t.Fatalf("retry key was not persisted: %v", err)
			}
		})
	}
}

func TestLoadOrCreateRejectsMalformedKeyWithoutReplacingIt(t *testing.T) {
	for _, size := range []int{0, 16, 31, 33} {
		path := filepath.Join(t.TempDir(), "encryption.key")
		original := bytes.Repeat([]byte{42}, size)
		if err := os.WriteFile(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadOrCreate(path); err == nil {
			t.Fatalf("accepted %d-byte key", size)
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, original) {
			t.Fatalf("existing key changed: %v", err)
		}
	}
}

func TestLoadOrCreateReturnsReadFailure(t *testing.T) {
	path := t.TempDir()
	if _, err := LoadOrCreate(path); err == nil {
		t.Fatal("accepted a directory as a key")
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatal("key path was changed")
	}
}
