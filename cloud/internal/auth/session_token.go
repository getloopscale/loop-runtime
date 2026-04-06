// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SessionTokenIssuer issues short-lived JWTs for dashboard sessions.
// Uses golang-jwt with HMAC-SHA256 (same library as Hydra JWT validation).
// Dashboard exchanges Kratos session → JWT via /v1/auth/session/token.
type SessionTokenIssuer struct {
	secret   []byte
	issuer   string
	audience string
	lifetime time.Duration
}

// SessionClaims are the claims embedded in a session token.
type SessionClaims struct {
	jwt.RegisteredClaims
	Email    string `json:"email,omitempty"`
	Name     string `json:"name,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
}

// NewSessionTokenIssuer creates an issuer with the given secret and lifetime.
func NewSessionTokenIssuer(secret, issuer string, lifetime time.Duration) *SessionTokenIssuer {
	return &SessionTokenIssuer{
		secret:   []byte(secret),
		issuer:   issuer,
		audience: "loopzero-cloud",
		lifetime: lifetime,
	}
}

// Issue creates a signed JWT for a Kratos-authenticated user.
func (s *SessionTokenIssuer) Issue(sub, email, name, tenantID string) (string, int64, error) {
	now := time.Now()

	claims := SessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   sub,
			Issuer:    s.issuer,
			Audience:  jwt.ClaimStrings{s.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.lifetime)),
		},
		Email:    email,
		Name:     name,
		TenantID: tenantID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", 0, fmt.Errorf("sign session token: %w", err)
	}

	return signed, int64(s.lifetime.Seconds()), nil
}

// Validate parses and validates a session token, returning the claims.
func (s *SessionTokenIssuer) Validate(tokenString string) (*SessionClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &SessionClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.secret, nil
	},
		jwt.WithIssuer(s.issuer),
		jwt.WithAudience(s.audience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("invalid session token: %w", err)
	}

	claims, ok := token.Claims.(*SessionClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid session token claims")
	}

	return claims, nil
}
