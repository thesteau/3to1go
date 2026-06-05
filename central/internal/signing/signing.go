package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CredentialPayload is the signed credential body.
type CredentialPayload struct {
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	JTI       string `json:"jti"`
}

func b64url(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func b64urlDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random uuid bytes: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// LoadOrCreateIssuerKeypair reads a 32-byte Ed25519 seed from path, or generates one.
func LoadOrCreateIssuerKeypair(path string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return nil, nil, fmt.Errorf("issuer key path %q is a directory; mount './secrets:/run/secrets' or create the key file first", path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("reading issuer key: %w", err)
		}
		if len(raw) != ed25519.SeedSize {
			return nil, nil, fmt.Errorf("issuer key file %q has unexpected length %d (expected 32 bytes)", path, len(raw))
		}
		priv := ed25519.NewKeyFromSeed(raw)
		return priv, priv.Public().(ed25519.PublicKey), nil
	}

	if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("stat issuer key: %w", err)
	}

	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, nil, fmt.Errorf("generating issuer key seed: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, fmt.Errorf("creating issuer key directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return LoadOrCreateIssuerKeypair(path)
		}
		return nil, nil, fmt.Errorf("creating issuer key file: %w", err)
	}
	if _, err := f.Write(seed); err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("writing issuer key: %w", err)
	}
	f.Close()

	priv := ed25519.NewKeyFromSeed(seed)
	return priv, priv.Public().(ed25519.PublicKey), nil
}

// MintCredential creates a signed JWT token compatible with the Python implementation.
func MintCredential(priv ed25519.PrivateKey, ttlDays int) (string, error) {
	now := time.Now().Unix()
	jti, err := newUUID()
	if err != nil {
		return "", err
	}

	headerJSON, err := json.Marshal(struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
	}{
		Algorithm: "EdDSA",
		Type:      "RCT",
	})
	if err != nil {
		return "", fmt.Errorf("marshal credential header: %w", err)
	}
	payloadJSON, err := json.Marshal(CredentialPayload{
		IssuedAt:  now,
		ExpiresAt: now + int64(ttlDays)*86400,
		JTI:       jti,
	})
	if err != nil {
		return "", fmt.Errorf("marshal credential payload: %w", err)
	}

	header := b64url(headerJSON)
	payload := b64url(payloadJSON)
	signingInput := []byte(header + "." + payload)
	sig := ed25519.Sign(priv, signingInput)

	return header + "." + payload + "." + b64url(sig), nil
}

// DecodeCredentialPayload decodes the payload without verifying the signature.
func DecodeCredentialPayload(token string) (*CredentialPayload, error) {
	parts, err := splitToken(token)
	if err != nil {
		return nil, err
	}
	rawPayload, err := b64urlDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("malformed payload")
	}
	var payload CredentialPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return nil, fmt.Errorf("malformed payload")
	}
	return &payload, nil
}

// VerifyCredential verifies the token signature, expiry, and jti presence.
func VerifyCredential(token string, pub ed25519.PublicKey) (*CredentialPayload, error) {
	parts, err := splitToken(token)
	if err != nil {
		return nil, err
	}

	sig, err := b64urlDecode(parts[2])
	if err != nil {
		return nil, errors.New("invalid signature encoding")
	}

	signingInput := []byte(parts[0] + "." + parts[1])
	if !ed25519.Verify(pub, signingInput, sig) {
		return nil, errors.New("invalid signature")
	}

	payload, err := DecodeCredentialPayload(token)
	if err != nil {
		return nil, err
	}

	if payload.ExpiresAt < time.Now().Unix() {
		return nil, errors.New("credential expired")
	}

	if strings.TrimSpace(payload.JTI) == "" {
		return nil, errors.New("credential missing jti")
	}

	return payload, nil
}

func splitToken(token string) ([3]string, error) {
	var parts [3]string
	head, rest, ok := strings.Cut(token, ".")
	if !ok {
		return parts, errors.New("malformed token")
	}
	payload, sig, ok := strings.Cut(rest, ".")
	if !ok || strings.Contains(sig, ".") {
		return parts, errors.New("malformed token")
	}
	if head == "" || payload == "" || sig == "" {
		return parts, errors.New("malformed token")
	}
	parts[0] = head
	parts[1] = payload
	parts[2] = sig
	return parts, nil
}
