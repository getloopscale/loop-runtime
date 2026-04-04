// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package jwt provides JWT validation against Ory Hydra's JWKS endpoint.
// Shared between the cloud auth service and the engine API.
package jwt

import (
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	goJWT "github.com/golang-jwt/jwt/v5"
)

// Claims represents the JWT claims extracted from a Hydra-issued token.
type Claims struct {
	Subject  string `json:"sub"`
	Email    string `json:"email,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
	Role     string `json:"role,omitempty"`
	Scope    string `json:"scope,omitempty"`
	goJWT.RegisteredClaims
}

// Validator validates JWT tokens against a JWKS endpoint.
type Validator struct {
	jwksURL    string
	issuer     string
	httpClient *http.Client

	mu      sync.RWMutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
	ttl     time.Duration
}

// NewValidator creates a JWT validator that fetches JWKS from the given URL.
func NewValidator(jwksURL, issuer string) *Validator {
	return &Validator{
		jwksURL: jwksURL,
		issuer:  issuer,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		keys: make(map[string]*rsa.PublicKey),
		ttl:  5 * time.Minute,
	}
}

// Validate parses and validates a JWT token, returning the claims.
func (v *Validator) Validate(tokenString string) (*Claims, error) {
	token, err := goJWT.ParseWithClaims(tokenString, &Claims{}, func(token *goJWT.Token) (interface{}, error) {
		if _, ok := token.Method.(*goJWT.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("missing kid in token header")
		}

		key, err := v.getKey(kid)
		if err != nil {
			if err := v.fetchJWKS(); err != nil {
				return nil, fmt.Errorf("failed to refresh JWKS: %w", err)
			}
			key, err = v.getKey(kid)
			if err != nil {
				return nil, err
			}
		}
		return key, nil
	}, goJWT.WithIssuer(v.issuer), goJWT.WithExpirationRequired())

	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}

// IsJWTToken returns true if the token looks like a JWT (has 3 dot-separated segments).
func IsJWTToken(token string) bool {
	parts := strings.Split(token, ".")
	return len(parts) == 3 && len(parts[0]) > 10
}

// IsGitHubPAT returns true if the token looks like a GitHub PAT.
func IsGitHubPAT(token string) bool {
	return strings.HasPrefix(token, "ghp_") || strings.HasPrefix(token, "gho_") || strings.HasPrefix(token, "github_pat_")
}

func (v *Validator) getKey(kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	if time.Since(v.fetched) > v.ttl || len(v.keys) == 0 {
		v.mu.RUnlock()
		if err := v.fetchJWKS(); err != nil {
			return nil, err
		}
		v.mu.RLock()
	}
	key, ok := v.keys[kid]
	v.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("key %s not found in JWKS", kid)
	}
	return key, nil
}

type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
	Use string `json:"use"`
}

func (v *Validator) fetchJWKS() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if time.Since(v.fetched) < v.ttl && len(v.keys) > 0 {
		return nil
	}

	resp, err := v.httpClient.Get(v.jwksURL)
	if err != nil {
		return fmt.Errorf("JWKS fetch failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS fetch returned status %d", resp.StatusCode)
	}

	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("JWKS decode failed: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey)
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Use != "sig" {
			continue
		}
		pubKey, err := parseRSAPublicKey(k.N, k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = pubKey
	}

	v.keys = keys
	v.fetched = time.Now()
	return nil
}

func parseRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := goJWT.NewParser().DecodeSegment(nStr)
	if err != nil {
		return nil, err
	}
	eBytes, err := goJWT.NewParser().DecodeSegment(eStr)
	if err != nil {
		return nil, err
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(new(big.Int).SetBytes(eBytes).Int64()),
	}, nil
}
