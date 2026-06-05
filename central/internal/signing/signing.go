package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
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

// MintCredential creates a signed EdDSA JWT credential.
func MintCredential(priv ed25519.PrivateKey, ttlDays int) (string, error) {
	now := time.Now().Unix()
	jti, err := newUUID()
	if err != nil {
		return "", err
	}

	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(time.Unix(now, 0)),
		ExpiresAt: jwt.NewNumericDate(time.Unix(now+int64(ttlDays)*86400, 0)),
		ID:        jti,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["typ"] = "JWT"
	signed, err := token.SignedString(priv)
	if err != nil {
		return "", fmt.Errorf("sign credential: %w", err)
	}
	return signed, nil
}

// DecodeCredentialPayload decodes the payload without verifying the signature.
func DecodeCredentialPayload(token string) (*CredentialPayload, error) {
	claims := &jwt.RegisteredClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(token, claims); err != nil {
		return nil, fmt.Errorf("malformed payload")
	}
	return payloadFromClaims(claims), nil
}

// VerifyCredential verifies the token signature, expiry, and jti presence.
func VerifyCredential(tokenString string, pub ed25519.PublicKey) (*CredentialPayload, error) {
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		func(token *jwt.Token) (interface{}, error) {
			if token.Method.Alg() != jwt.SigningMethodEdDSA.Alg() {
				return nil, fmt.Errorf("unexpected signing method %q", token.Method.Alg())
			}
			return pub, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, errors.New("credential expired")
		}
		if errors.Is(err, jwt.ErrTokenSignatureInvalid) {
			return nil, errors.New("invalid signature")
		}
		return nil, err
	}
	if token == nil || !token.Valid {
		return nil, errors.New("invalid token")
	}

	payload := payloadFromClaims(claims)
	if strings.TrimSpace(payload.JTI) == "" {
		return nil, errors.New("credential missing jti")
	}

	return payload, nil
}

func payloadFromClaims(claims *jwt.RegisteredClaims) *CredentialPayload {
	payload := &CredentialPayload{JTI: claims.ID}
	if claims.IssuedAt != nil {
		payload.IssuedAt = claims.IssuedAt.Unix()
	}
	if claims.ExpiresAt != nil {
		payload.ExpiresAt = claims.ExpiresAt.Unix()
	}
	return payload
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
