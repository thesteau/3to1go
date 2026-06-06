package encryption

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/minio/sio"
)

// LoadOrCreate reads a 32-byte key from path, creating one if absent.
func LoadOrCreate(path string) ([]byte, error) {
	if data, err := os.ReadFile(path); err == nil && len(data) == 32 {
		return data, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate encryption key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// KeyAsBase64 returns the URL-safe base64 encoding of key.
func KeyAsBase64(key []byte) string {
	return base64.URLEncoding.EncodeToString(key)
}

// KeyFingerprint returns the SHA-256 hex digest of key.
func KeyFingerprint(key []byte) string {
	sum := sha256.Sum256(key)
	return fmt.Sprintf("%x", sum)
}

// EncryptFile streams src through minio/sio DARE v2 and writes the encrypted file to dst.
func EncryptFile(key []byte, src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open plaintext: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open ciphertext: %w", err)
	}
	closeOut := true
	defer func() {
		if closeOut {
			out.Close()
		}
	}()

	if _, err := sio.Encrypt(out, in, sioConfig(key)); err != nil {
		out.Close()
		os.Remove(dst)
		return fmt.Errorf("encrypt: %w", err)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		closeOut = false
		os.Remove(dst)
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
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open plaintext: %w", err)
	}
	closeOut := true
	defer func() {
		if closeOut {
			out.Close()
		}
	}()

	if _, err := sio.Decrypt(out, in, sioConfig(key)); err != nil {
		out.Close()
		os.Remove(dst)
		return fmt.Errorf("decrypt: %w", err)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		closeOut = false
		os.Remove(dst)
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
