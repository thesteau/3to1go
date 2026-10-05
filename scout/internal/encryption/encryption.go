package encryption

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/3to1go/scout/internal/cancelio"
	"github.com/minio/sio"
)

// LoadOrCreate reads a 32-byte key from path, creating one if absent.
func LoadOrCreate(path string) ([]byte, error) {
	return loadOrCreate(path, func(path string) (keyFile, error) {
		return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	})
}

type keyFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

func loadOrCreate(path string, create func(string) (keyFile, error)) (key []byte, err error) {
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != 32 {
			return nil, fmt.Errorf("invalid Scout key: expected 32 bytes, got %d", len(data))
		}
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read Scout key: %w", err)
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate Scout key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := create(path)
	if err != nil {
		return nil, err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, f.Close())
		}
		// Only remove a file created by this call, and close it first so
		// cleanup also works on Windows. Existing invalid keys are untouched.
		if err != nil {
			if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
				err = errors.Join(err, fmt.Errorf("remove incomplete Scout key: %w", removeErr))
			}
		}
	}()
	if n, err := f.Write(key); err != nil {
		return nil, err
	} else if n != len(key) {
		return nil, io.ErrShortWrite
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	err = f.Close()
	closed = true
	if err != nil {
		return nil, err
	}
	return key, nil
}

// KeyAsBase64 returns the URL-safe base64 encoding of key.
func KeyAsBase64(key []byte) string {
	return base64.URLEncoding.EncodeToString(key)
}

// KeyFromBase64 parses a 32-byte key in the URL-safe form KeyAsBase64 shows,
// also accepting standard base64 and omitted padding.
func KeyFromBase64(s string) ([]byte, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.RawStdEncoding} {
		if key, err := enc.DecodeString(s); err == nil && len(key) == 32 {
			return key, nil
		}
	}
	return nil, errors.New("invalid Scout key: must be a base64 encoded 32-byte key")
}

// KeyFingerprint returns the SHA-256 hex digest of key.
func KeyFingerprint(key []byte) string {
	sum := sha256.Sum256(key)
	return fmt.Sprintf("%x", sum)
}

// EncryptFile streams src through minio/sio DARE v2 and writes the encrypted file to dst.
func EncryptFile(key []byte, src, dst string) error {
	return EncryptFileContext(context.Background(), key, src, dst)
}

func EncryptFileContext(ctx context.Context, key []byte, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open plaintext: %w", err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open ciphertext: %w", err)
	}
	closeOut := true
	defer func() {
		if closeOut {
			_ = out.Close()
		}
	}()

	if _, err := sio.Encrypt(cancelio.Writer{Context: ctx, Writer: out}, cancelio.Reader{Context: ctx, Reader: in}, sioConfig(key)); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return fmt.Errorf("encrypt: %w", err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		closeOut = false
		_ = os.Remove(dst)
		return err
	}
	closeOut = false
	return nil
}

// DecryptFile decrypts a minio/sio DARE v2 file written by EncryptFile.
func DecryptFile(key []byte, src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open ciphertext: %w", err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open plaintext: %w", err)
	}
	closeOut := true
	defer func() {
		if closeOut {
			_ = out.Close()
		}
	}()

	if _, err := sio.Decrypt(out, in, sioConfig(key)); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return fmt.Errorf("decrypt: %w", err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		closeOut = false
		_ = os.Remove(dst)
		return err
	}
	closeOut = false
	return nil
}

func sioConfig(key []byte) sio.Config {
	return sio.Config{
		MinVersion:   sio.Version20,
		MaxVersion:   sio.Version20,
		CipherSuites: []byte{sio.AES_GCM},
		Key:          key,
	}
}
