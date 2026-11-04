package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestVerifyCredentialRejectsTokensWithAudience(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.RegisteredClaims{
		Audience:  jwt.ClaimStrings{"3to1go-automation"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		ID:        "token-id",
	}).SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCredential(token, pub); err == nil {
		t.Fatal("automation token passed as a Station token")
	}
}
