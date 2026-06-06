package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
)

// Magic header written before IV + ciphertext. Must match the Python RCENC1\x00\x00.
var magic = []byte("RCENC1\x00\x00")

const ivSize = 12

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

// EncryptFile reads src, encrypts with AES-256-GCM, and writes magic+IV+ciphertext to dst.
func EncryptFile(key []byte, src, dst string) error {
	plaintext, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read plaintext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	iv := make([]byte, ivSize)
	if _, err := rand.Read(iv); err != nil {
		return err
	}
	ciphertext := gcm.Seal(nil, iv, plaintext, nil)

	out := make([]byte, 0, len(magic)+ivSize+len(ciphertext))
	out = append(out, magic...)
	out = append(out, iv...)
	out = append(out, ciphertext...)
	return os.WriteFile(dst, out, 0o644)
}

// DecryptFile reads an encrypted file written by EncryptFile and writes the plaintext to dst.
// If the file does not begin with magic, it is copied as-is (unencrypted passthrough).
func DecryptFile(key []byte, src, dst string) error {
	payload, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read ciphertext: %w", err)
	}

	if len(payload) < len(magic) || string(payload[:len(magic)]) != string(magic) {
		return os.WriteFile(dst, payload, 0o644)
	}

	minLen := len(magic) + ivSize + 16 // 16 = GCM auth tag minimum
	if len(payload) < minLen {
		return fmt.Errorf("invalid encrypted archive format")
	}

	iv := payload[len(magic) : len(magic)+ivSize]
	ciphertext := payload[len(magic)+ivSize:]

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	plaintext, err := gcm.Open(nil, iv, ciphertext, nil)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}
	return os.WriteFile(dst, plaintext, 0o644)
}
