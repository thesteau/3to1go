package encryption

import (
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
	// SHA-256 hex = 64 characters
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
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	dir := t.TempDir()
	src := dir + "/plain.txt"
	enc := dir + "/plain.txt.enc"
	dec := dir + "/plain.txt.dec"
	plaintext := []byte("hello, world! this is a test payload for AES-256-GCM.")

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
	if string(result) != string(plaintext) {
		t.Errorf("decrypted %q, want %q", result, plaintext)
	}
}

func TestEncryptFile_HasMagicHeader(t *testing.T) {
	key := make([]byte, 32)
	dir := t.TempDir()
	src := dir + "/data.txt"
	enc := dir + "/data.enc"

	if err := os.WriteFile(src, []byte("payload data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EncryptFile(key, src, enc); err != nil {
		t.Fatalf("EncryptFile: %v", err)
	}
	data, err := os.ReadFile(enc)
	if err != nil {
		t.Fatal(err)
	}
	wantMagic := "RCENC1\x00\x00"
	if len(data) < len(wantMagic) || string(data[:len(wantMagic)]) != wantMagic {
		t.Errorf("encrypted file missing magic header")
	}
}

func TestEncryptFile_NonDeterministic(t *testing.T) {
	// Two encryptions of the same plaintext should produce different ciphertext (random IV).
	key := make([]byte, 32)
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
	same := len(d1) == len(d2)
	if same {
		for i := range d1 {
			if d1[i] != d2[i] {
				same = false
				break
			}
		}
	}
	if same {
		t.Error("two encryptions of the same plaintext should differ (random IV)")
	}
}

func TestDecryptFile_Passthrough_NoMagic(t *testing.T) {
	key := make([]byte, 32)
	dir := t.TempDir()
	src := dir + "/raw.bin"
	dst := dir + "/raw.out"
	content := []byte("no magic header here — treat as plaintext passthrough")

	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := DecryptFile(key, src, dst); err != nil {
		t.Fatalf("DecryptFile passthrough: %v", err)
	}
	result, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != string(content) {
		t.Errorf("passthrough: got %q, want %q", result, content)
	}
}

func TestDecryptFile_WrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	for i := range key2 {
		key2[i] = byte(i + 1)
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
	key := make([]byte, 32)
	dir := t.TempDir()
	// Write a file that starts with the magic header but is too short
	truncated := dir + "/truncated.enc"
	payload := append([]byte("RCENC1\x00\x00"), []byte("tooshort")...)
	if err := os.WriteFile(truncated, payload, 0o644); err != nil {
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
	for i := range key1 {
		if key1[i] != key2[i] {
			t.Error("LoadOrCreate should return the same key on subsequent calls")
			break
		}
	}
}
