package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"slices"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func monitoringKeys(t *testing.T) (ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv, pub
}

func signMonitoringClaims(t *testing.T, priv ed25519.PrivateKey, claims monitoringClaims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestMonitoringTokenRoundTrip(t *testing.T) {
	priv, pub := monitoringKeys(t)
	token, minted, err := MintMonitoringToken(priv, 30, []string{MonitoringRouteReport})
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyMonitoringToken(token, pub)
	if err != nil {
		t.Fatal(err)
	}
	if got.JTI != minted.JTI || !slices.Equal(got.Routes, []string{MonitoringRouteReport}) || got.ExpiresAt != got.IssuedAt+30*86400 {
		t.Fatalf("payload: %+v", got)
	}
}

func TestMonitoringAndStationTokensDoNotCross(t *testing.T) {
	priv, pub := monitoringKeys(t)
	monitoring, _, err := MintMonitoringToken(priv, 30, []string{MonitoringRouteMetrics})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCredential(monitoring, pub); err == nil {
		t.Fatal("monitoring token passed as a Station token")
	}
	station, err := MintCredential(priv, 30)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyMonitoringToken(station, pub); err == nil {
		t.Fatal("Station token passed as a monitoring token")
	}
	if _, err := VerifyCredential(station, pub); err != nil {
		t.Fatalf("Station token stopped working: %v", err)
	}
}

func TestVerifyMonitoringTokenRejects(t *testing.T) {
	priv, pub := monitoringKeys(t)
	otherPriv, _ := monitoringKeys(t)
	now := time.Now()
	valid := jwt.RegisteredClaims{
		Audience:  jwt.ClaimStrings{MonitoringAudience},
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		ID:        "token-id",
	}
	with := func(change func(*jwt.RegisteredClaims)) monitoringClaims {
		claims := valid
		change(&claims)
		return monitoringClaims{RegisteredClaims: claims, Routes: []string{MonitoringRouteMetrics}}
	}
	validToken, _, err := MintMonitoringToken(priv, 1, []string{MonitoringRouteMetrics})
	if err != nil {
		t.Fatal(err)
	}
	for name, token := range map[string]string{
		"expired":        signMonitoringClaims(t, priv, with(func(c *jwt.RegisteredClaims) { c.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Hour)) })),
		"no expiry":      signMonitoringClaims(t, priv, with(func(c *jwt.RegisteredClaims) { c.ExpiresAt = nil })),
		"wrong audience": signMonitoringClaims(t, priv, with(func(c *jwt.RegisteredClaims) { c.Audience = jwt.ClaimStrings{"other"} })),
		"no token ID":    signMonitoringClaims(t, priv, with(func(c *jwt.RegisteredClaims) { c.ID = "" })),
		"wrong key":      signMonitoringClaims(t, otherPriv, with(func(*jwt.RegisteredClaims) {})),
		"tampered":       validToken + "x",
		"not a token":    "not-a-token",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := VerifyMonitoringToken(token, pub); err == nil {
				t.Fatal("token accepted")
			}
		})
	}
}
