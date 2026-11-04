package signing

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// MonitoringAudience keeps monitoring tokens apart from Station tokens, which
// are signed with the same issuer key.
const MonitoringAudience = "3to1go-station-monitoring"

const (
	MonitoringRouteMetrics = "metrics"
	MonitoringRouteReport  = "report"
)

// MonitoringPayload is the signed monitoring token body.
type MonitoringPayload struct {
	JTI       string
	Routes    []string
	IssuedAt  int64
	ExpiresAt int64
}

type monitoringClaims struct {
	jwt.RegisteredClaims
	Routes []string `json:"routes"`
}

// MintMonitoringToken creates a signed EdDSA JWT limited to the given routes.
func MintMonitoringToken(priv ed25519.PrivateKey, ttlDays int, routes []string) (string, *MonitoringPayload, error) {
	jti, err := uuid.NewRandom()
	if err != nil {
		return "", nil, fmt.Errorf("generate monitoring token id: %w", err)
	}
	now := time.Now().Unix()
	expires := now + int64(ttlDays)*86400
	claims := monitoringClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings{MonitoringAudience},
			IssuedAt:  jwt.NewNumericDate(time.Unix(now, 0)),
			ExpiresAt: jwt.NewNumericDate(time.Unix(expires, 0)),
			ID:        jti.String(),
		},
		Routes: routes,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["typ"] = "JWT"
	signed, err := token.SignedString(priv)
	if err != nil {
		return "", nil, fmt.Errorf("sign monitoring token: %w", err)
	}
	return signed, &MonitoringPayload{JTI: jti.String(), Routes: routes, IssuedAt: now, ExpiresAt: expires}, nil
}

// VerifyMonitoringToken checks the signature, audience, expiry, and token ID.
// A Station token has no audience, so it fails here.
func VerifyMonitoringToken(tokenString string, pub ed25519.PublicKey) (*MonitoringPayload, error) {
	claims := &monitoringClaims{}
	_, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		func(*jwt.Token) (any, error) { return pub, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithAudience(MonitoringAudience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(claims.ID) == "" {
		return nil, errors.New("missing jti in monitoring token")
	}
	payload := &MonitoringPayload{JTI: claims.ID, Routes: claims.Routes, ExpiresAt: claims.ExpiresAt.Unix()}
	if claims.IssuedAt != nil {
		payload.IssuedAt = claims.IssuedAt.Unix()
	}
	return payload, nil
}
