package signing

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadOrCreateIssuerKeypair_CreateNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "issuer.key")

	priv, pub, err := LoadOrCreateIssuerKeypair(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(priv) != ed25519.PrivateKeySize {
		t.Errorf("private key size %d, want %d", len(priv), ed25519.PrivateKeySize)
	}
	if len(pub) != ed25519.PublicKeySize {
		t.Errorf("public key size %d, want %d", len(pub), ed25519.PublicKeySize)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("key file not created: %v", err)
	}
	if len(raw) != ed25519.SeedSize {
		t.Errorf("seed file length %d, want %d", len(raw), ed25519.SeedSize)
	}
}

func TestLoadOrCreateIssuerKeypair_LoadExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "issuer.key")

	priv1, pub1, err := LoadOrCreateIssuerKeypair(path)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	priv2, pub2, err := LoadOrCreateIssuerKeypair(path)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if !priv1.Equal(priv2) {
		t.Error("private keys differ on reload")
	}
	if !pub1.Equal(pub2) {
		t.Error("public keys differ on reload")
	}
}

func TestLoadOrCreateIssuerKeypair_PathIsDirectory(t *testing.T) {
	dir := t.TempDir()
	_, _, err := LoadOrCreateIssuerKeypair(dir)
	if err == nil {
		t.Error("expected error when path is a directory")
	}
}

func TestLoadOrCreateIssuerKeypair_WrongSeedLength(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.key")
	os.WriteFile(path, []byte("tooshort"), 0o600)

	_, _, err := LoadOrCreateIssuerKeypair(path)
	if err == nil {
		t.Error("expected error for wrong-length seed file")
	}
}

func TestMintCredential_Structure(t *testing.T) {
	dir := t.TempDir()
	priv, _, _ := LoadOrCreateIssuerKeypair(filepath.Join(dir, "key"))

	token, err := MintCredential(priv, 1)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts, want 3", len(parts))
	}
	for i, p := range parts {
		if p == "" {
			t.Errorf("part %d is empty", i)
		}
	}
	// Header must decode to JSON with alg=EdDSA
	rawHdr, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("header not base64url: %v", err)
	}
	var hdr map[string]string
	if err := json.Unmarshal(rawHdr, &hdr); err != nil {
		t.Fatalf("header not JSON: %v", err)
	}
	if hdr["alg"] != "EdDSA" {
		t.Errorf("alg = %q, want EdDSA", hdr["alg"])
	}
}

func TestVerifyCredential_ValidRoundTrip(t *testing.T) {
	dir := t.TempDir()
	priv, pub, _ := LoadOrCreateIssuerKeypair(filepath.Join(dir, "key"))

	token, _ := MintCredential(priv, 30)
	payload, err := VerifyCredential(token, pub)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if payload.JTI == "" {
		t.Error("JTI should be non-empty")
	}
	if payload.ExpiresAt <= time.Now().Unix() {
		t.Error("ExpiresAt should be in the future")
	}
}

func TestVerifyCredential_WrongKey(t *testing.T) {
	dir := t.TempDir()
	priv, _, _ := LoadOrCreateIssuerKeypair(filepath.Join(dir, "k1"))
	_, pub2, _ := LoadOrCreateIssuerKeypair(filepath.Join(dir, "k2"))

	token, _ := MintCredential(priv, 30)
	_, err := VerifyCredential(token, pub2)
	if err == nil || !strings.Contains(err.Error(), "invalid signature") {
		t.Errorf("expected invalid signature, got %v", err)
	}
}

func TestVerifyCredential_TamperedPayload(t *testing.T) {
	dir := t.TempDir()
	priv, pub, _ := LoadOrCreateIssuerKeypair(filepath.Join(dir, "key"))
	token, _ := MintCredential(priv, 30)

	parts := strings.Split(token, ".")
	// Replace payload with a different base64url encoding
	tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"iat":1,"exp":9999999999,"jti":"x"}`)) + "." + parts[2]
	_, err := VerifyCredential(tampered, pub)
	if err == nil || !strings.Contains(err.Error(), "invalid signature") {
		t.Errorf("expected invalid signature, got %v", err)
	}
}

func TestVerifyCredential_Expired(t *testing.T) {
	dir := t.TempDir()
	priv, pub, _ := LoadOrCreateIssuerKeypair(filepath.Join(dir, "key"))

	// Craft an expired token manually
	now := time.Now().Unix()
	payloadJSON, _ := json.Marshal(CredentialPayload{IssuedAt: now - 100, ExpiresAt: now - 1, JTI: "test-jti"})
	hdrJSON, _ := json.Marshal(map[string]string{"alg": "EdDSA", "typ": "RCT"})
	hdr := b64url(hdrJSON)
	pay := b64url(payloadJSON)
	sig := ed25519.Sign(priv, []byte(hdr+"."+pay))
	token := hdr + "." + pay + "." + b64url(sig)

	_, err := VerifyCredential(token, pub)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("expected expired error, got %v", err)
	}
}

func TestVerifyCredential_MissingJTI(t *testing.T) {
	dir := t.TempDir()
	priv, pub, _ := LoadOrCreateIssuerKeypair(filepath.Join(dir, "key"))

	payloadJSON, _ := json.Marshal(CredentialPayload{IssuedAt: time.Now().Unix(), ExpiresAt: time.Now().Unix() + 86400, JTI: "   "})
	hdrJSON, _ := json.Marshal(map[string]string{"alg": "EdDSA", "typ": "RCT"})
	hdr := b64url(hdrJSON)
	pay := b64url(payloadJSON)
	sig := ed25519.Sign(priv, []byte(hdr+"."+pay))
	token := hdr + "." + pay + "." + b64url(sig)

	_, err := VerifyCredential(token, pub)
	if err == nil || !strings.Contains(err.Error(), "jti") {
		t.Errorf("expected missing jti error, got %v", err)
	}
}

func TestVerifyCredential_MalformedTokens(t *testing.T) {
	dir := t.TempDir()
	_, pub, _ := LoadOrCreateIssuerKeypair(filepath.Join(dir, "key"))

	cases := []string{
		"",
		"nodots",
		"one.dot",
		"too.many.dots.here",
		"a..c",
		".b.c",
		"a.b.",
	}
	for _, tc := range cases {
		_, err := VerifyCredential(tc, pub)
		if err == nil {
			t.Errorf("expected error for token %q", tc)
		}
	}
}

func TestVerifyCredential_InvalidSignatureEncoding(t *testing.T) {
	dir := t.TempDir()
	_, pub, _ := LoadOrCreateIssuerKeypair(filepath.Join(dir, "key"))

	token := "aGVhZA.cGF5bG9hZA.!!!notbase64!!!"
	_, err := VerifyCredential(token, pub)
	if err == nil {
		t.Error("expected error for invalid signature encoding")
	}
}

func TestDecodeCredentialPayload_Valid(t *testing.T) {
	dir := t.TempDir()
	priv, _, _ := LoadOrCreateIssuerKeypair(filepath.Join(dir, "key"))
	token, _ := MintCredential(priv, 7)

	payload, err := DecodeCredentialPayload(token)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.JTI == "" {
		t.Error("JTI empty")
	}
}

func TestDecodeCredentialPayload_BadPayloadBase64(t *testing.T) {
	token := "aGVhZA.!!!.c2ln"
	_, err := DecodeCredentialPayload(token)
	if err == nil {
		t.Error("expected error for bad payload base64")
	}
}

func TestDecodeCredentialPayload_BadPayloadJSON(t *testing.T) {
	token := "aGVhZA." + b64url([]byte("not-json")) + ".c2ln"
	_, err := DecodeCredentialPayload(token)
	if err == nil {
		t.Error("expected error for bad payload JSON")
	}
}

func TestSplitToken_ExtraDotsInSig(t *testing.T) {
	_, err := splitToken("a.b.c.d")
	if err == nil {
		t.Error("expected error for token with extra dots in sig")
	}
}
