package encryption

import (
	"bytes"
	"os"
	"testing"
)

func TestKeyFingerprint_Deterministic(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	if KeyFingerprint(key) != KeyFingerprint(key) {
		t.Error("fingerprint is not deterministic")
	}
}

func TestKeyFingerprint_DifferentKeys(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	key2[0] = 1
	if KeyFingerprint(key1) == KeyFingerprint(key2) {
		t.Error("different keys should produce different fingerprints")
	}
}

func TestKeyFingerprint_IsHex(t *testing.T) {
	key := make([]byte, 32)
	fp := KeyFingerprint(key)
	if len(fp) != 64 {
		t.Errorf("fingerprint length = %d, want 64", len(fp))
	}
}

func TestKeyAsBase64_NonEmpty(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i * 7)
	}
	if b64 := KeyAsBase64(key); b64 == "" {
		t.Error("expected non-empty base64 string")
	}
}

func TestKeyAsBase64_DifferentForDifferentKeys(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	key2[31] = 1
	if KeyAsBase64(key1) == KeyAsBase64(key2) {
		t.Error("different keys should produce different base64 strings")
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := testKey()
	dir := t.TempDir()
	src := dir + "/plain.txt"
	enc := dir + "/plain.txt.enc"
	dec := dir + "/plain.txt.dec"
	plaintext := []byte("hello, world! this is a test payload for streaming DARE encryption.")

	if err := os.WriteFile(src, plaintext, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EncryptFile(key, src, enc); err != nil {
		t.Fatalf("EncryptFile: %v", err)
	}
	if err := DecryptFile(key, enc, dec); err != nil {
		t.Fatalf("DecryptFile: %v", err)
	}
	result, err := os.ReadFile(dec)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result, plaintext) {
		t.Errorf("decrypted %q, want %q", result, plaintext)
	}
}

func TestEncryptDecrypt_LargeRoundTrip(t *testing.T) {
	key := testKey()
	dir := t.TempDir()
	src := dir + "/large.bin"
	enc := dir + "/large.enc"
	dec := dir + "/large.dec"

	plaintext := make([]byte, 6*1024*1024)
	for i := range plaintext {
		plaintext[i] = byte(i % 251)
	}
	if err := os.WriteFile(src, plaintext, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EncryptFile(key, src, enc); err != nil {
		t.Fatalf("EncryptFile: %v", err)
	}
	if err := DecryptFile(key, enc, dec); err != nil {
		t.Fatalf("DecryptFile: %v", err)
	}
	result, err := os.ReadFile(dec)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result, plaintext) {
		t.Fatal("decrypted large payload mismatch")
	}
}

func TestEncryptFile_NonDeterministic(t *testing.T) {
	key := testKey()
	dir := t.TempDir()
	src := dir + "/data.txt"
	enc1 := dir + "/data1.enc"
	enc2 := dir + "/data2.enc"

	if err := os.WriteFile(src, []byte("same content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EncryptFile(key, src, enc1); err != nil {
		t.Fatal(err)
	}
	if err := EncryptFile(key, src, enc2); err != nil {
		t.Fatal(err)
	}
	d1, _ := os.ReadFile(enc1)
	d2, _ := os.ReadFile(enc2)
	if bytes.Equal(d1, d2) {
		t.Error("two encryptions of the same plaintext should differ")
	}
}

func TestDecryptFile_PlaintextRejected(t *testing.T) {
	key := testKey()
	dir := t.TempDir()
	src := dir + "/raw.bin"
	dst := dir + "/raw.out"

	if err := os.WriteFile(src, []byte("plain bytes are not a valid DARE stream"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := DecryptFile(key, src, dst); err == nil {
		t.Fatal("expected plaintext input to be rejected")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("expected failed decrypt output to be removed, stat err=%v", err)
	}
}

func TestDecryptFile_WrongKey(t *testing.T) {
	key1 := testKey()
	key2 := make([]byte, 32)
	for i := range key2 {
		key2[i] = byte(i + 11)
	}
	dir := t.TempDir()
	src := dir + "/plain.txt"
	enc := dir + "/plain.enc"
	dec := dir + "/plain.dec"

	if err := os.WriteFile(src, []byte("secret data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EncryptFile(key1, src, enc); err != nil {
		t.Fatalf("EncryptFile: %v", err)
	}
	if err := DecryptFile(key2, enc, dec); err == nil {
		t.Error("expected error when decrypting with wrong key")
	}
}

func TestDecryptFile_TruncatedCiphertext(t *testing.T) {
	key := testKey()
	dir := t.TempDir()
	src := dir + "/plain.txt"
	enc := dir + "/plain.enc"
	truncated := dir + "/truncated.enc"

	if err := os.WriteFile(src, []byte("secret data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EncryptFile(key, src, enc); err != nil {
		t.Fatalf("EncryptFile: %v", err)
	}
	data, err := os.ReadFile(enc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(truncated, data[:len(data)-3], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := DecryptFile(key, truncated, dir+"/out"); err == nil {
		t.Error("expected error for truncated ciphertext")
	}
}

func TestLoadOrCreate_CreatesKey(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/encryption.key"
	key, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("expected 32-byte key, got %d", len(key))
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected key file to exist: %v", err)
	}
}

func TestLoadOrCreate_LoadsExistingKey(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/encryption.key"
	key1, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	key2, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(key1, key2) {
		t.Error("LoadOrCreate should return the same key on subsequent calls")
	}
}

func testKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}
