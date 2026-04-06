// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SessionTokenIssuer issues short-lived JWT-like tokens for dashboard sessions.
// These tokens are exchanged from Kratos session cookies via /v1/auth/session/token.
// All cloud service endpoints validate these tokens using the same secret.
//
// Format: standard JWT (header.payload.signature) signed with HMAC-SHA256.
// This avoids dependency on Hydra for dashboard → API authentication.
type SessionTokenIssuer struct {
	secret   []byte
	issuer   string
	lifetime time.Duration
}

// SessionClaims are the claims embedded in a session token.
type SessionClaims struct {
	Sub      string `json:"sub"`
	Email    string `json:"email"`
	Name     string `json:"name,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
	Iss      string `json:"iss"`
	Iat      int64  `json:"iat"`
	Exp      int64  `json:"exp"`
}

// NewSessionTokenIssuer creates an issuer with the given secret and lifetime.
func NewSessionTokenIssuer(secret, issuer string, lifetime time.Duration) *SessionTokenIssuer {
	return &SessionTokenIssuer{
		secret:   []byte(secret),
		issuer:   issuer,
		lifetime: lifetime,
	}
}

// Issue creates a signed JWT for a Kratos-authenticated user.
func (s *SessionTokenIssuer) Issue(sub, email, name, tenantID string) (string, int64, error) {
	now := time.Now()
	exp := now.Add(s.lifetime)

	claims := SessionClaims{
		Sub:      sub,
		Email:    email,
		Name:     name,
		TenantID: tenantID,
		Iss:      s.issuer,
		Iat:      now.Unix(),
		Exp:      exp.Unix(),
	}

	header := base64url([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", 0, fmt.Errorf("marshal claims: %w", err)
	}
	payloadB64 := base64url(payload)

	sigInput := header + "." + payloadB64
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(sigInput))
	sig := base64url(mac.Sum(nil))

	token := sigInput + "." + sig
	return token, int64(s.lifetime.Seconds()), nil
}

// Validate parses and validates a session token, returning the claims.
func (s *SessionTokenIssuer) Validate(tokenString string) (*SessionClaims, error) {
	parts := strings.SplitN(tokenString, ".", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}

	// Verify signature
	sigInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(sigInput))
	expected := base64url(mac.Sum(nil))

	if !hmac.Equal([]byte(parts[2]), []byte(expected)) {
		return nil, fmt.Errorf("invalid signature")
	}

	// Decode payload
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}

	var claims SessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal claims: %w", err)
	}

	// Check expiration
	if time.Now().Unix() > claims.Exp {
		return nil, fmt.Errorf("token expired")
	}

	// Check issuer
	if claims.Iss != s.issuer {
		return nil, fmt.Errorf("invalid issuer")
	}

	return &claims, nil
}

func base64url(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}
