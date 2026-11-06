package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AutomationAudience keeps automation tokens apart from any other token signed
// with the same key, such as Station's tokens for Scouts.
const AutomationAudience = "3to1go-automation"

type automationClaims struct {
	jwt.RegisteredClaims
	Scopes []string `json:"scopes"`
}

// mintAutomationToken returns an EdDSA-signed JWT.
func mintAutomationToken(key ed25519.PrivateKey, userID int, id string, scopes []string, issued, expires time.Time) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, automationClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings{AutomationAudience},
			Subject:   strconv.Itoa(userID),
			ID:        id,
			IssuedAt:  jwt.NewNumericDate(issued),
			ExpiresAt: jwt.NewNumericDate(expires),
		},
		Scopes: scopes,
	})
	signed, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("sign auth token: %w", err)
	}
	return signed, nil
}

// verifyAutomationToken accepts only a token this key signed with EdDSA. Limiting
// the method rejects unsigned tokens and tokens signed with the public key as a secret.
func verifyAutomationToken(token string, pub ed25519.PublicKey) error {
	claims := &automationClaims{}
	_, err := jwt.ParseWithClaims(
		token,
		claims,
		func(*jwt.Token) (any, error) { return pub, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithAudience(AutomationAudience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return err
	}
	if strings.TrimSpace(claims.ID) == "" {
		return errors.New("auth token has no ID")
	}
	return nil
}

// LoadOrCreateSigningKey reads a 32-byte Ed25519 seed from path, or creates one.
func LoadOrCreateSigningKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		if len(raw) != ed25519.SeedSize {
			return nil, fmt.Errorf("signing key file %q has unexpected length %d (expected %d bytes)", path, len(raw), ed25519.SeedSize)
		}
		return ed25519.NewKeyFromSeed(raw), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading signing key: %w", err)
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("generating signing key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating signing key directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			// Another request created it first.
			return LoadOrCreateSigningKey(path)
		}
		return nil, fmt.Errorf("creating signing key: %w", err)
	}
	if _, err := file.Write(seed); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("writing signing key: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("writing signing key: %w", err)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}
