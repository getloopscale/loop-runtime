// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package jwt provides JWT validation against Ory Hydra's JWKS endpoint.
// Shared between the cloud auth service and the engine API.
//
// Per ADR-0029, this package enforces RFC 8725bis hardening:
//   - Algorithm whitelist via goJWT.WithValidMethods (defense in depth
//     against algorithm confusion attacks; RFC 8725 §3.1).
//   - typ header validation — only "JWT", "at+jwt", or absent are
//     accepted (RFC 8725bis §2.6 cross-JWT confusion mitigation).
//   - JWKS fetch wrapped by circuitbreaker.Execute (when configured)
//     so that Hydra outages do not cascade into auth failures —
//     cached keys are served during the circuit-open window
//     (gate PKG-G16 eat-your-own-dogfood).
package jwt

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	goJWT "github.com/golang-jwt/jwt/v5"
	"github.com/loopzero/loopzero/pkg/circuitbreaker"
)

// AllowedAccessAlgs is the whitelist of signing algorithms acceptable
// for access tokens issued by Ory Hydra. Hydra produces RS256 by
// default; widening this list MUST go through ADR-0029 review.
var AllowedAccessAlgs = []string{"RS256"}

// AllowedSessionAlgs is the whitelist of signing algorithms acceptable
// for HMAC session tokens (dashboard → API).
var AllowedSessionAlgs = []string{"HS256"}

// allowedTypValues is the closed set of "typ" header values accepted
// by LoopZero validators per RFC 7519 / RFC 9068. Empty (header absent)
// is also accepted — RFC 7519 does not mandate the header.
var allowedTypValues = map[string]struct{}{
	"":       {}, // header absent is allowed
	"JWT":    {}, // RFC 7519 default
	"at+jwt": {}, // RFC 9068 OAuth access-token profile
}

// validateTypHeader rejects tokens whose "typ" header is set to a
// value outside allowedTypValues. Closes the cross-JWT confusion
// vector (RFC 8725bis §2.6).
func validateTypHeader(token *goJWT.Token) error {
	raw, ok := token.Header["typ"]
	if !ok {
		return nil
	}
	typ, ok := raw.(string)
	if !ok {
		return fmt.Errorf("typ header must be a string, got %T", raw)
	}
	if _, accepted := allowedTypValues[typ]; !accepted {
		return fmt.Errorf("unexpected typ header: %q", typ)
	}
	return nil
}

// Claims represents the JWT claims extracted from a Hydra-issued token.
// Subject is in RegisteredClaims.Subject — do NOT redeclare it here.
type Claims struct {
	goJWT.RegisteredClaims
	Email    string `json:"email,omitempty"`
	Name     string `json:"name,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
	Role     string `json:"role,omitempty"`
	Scope    string `json:"scope,omitempty"`
	// Hydra puts consent session claims in "ext" (access_token session).
	Ext struct {
		Email    string `json:"email,omitempty"`
		Name     string `json:"name,omitempty"`
		TenantID string `json:"tenant_id,omitempty"`
	} `json:"ext,omitempty"`
}

// SessionClaims are the claims in HMAC session tokens (dashboard → API).
// Shared between cloud service (issuer) and engine API (validator).
type SessionClaims struct {
	goJWT.RegisteredClaims
	Email    string `json:"email,omitempty"`
	Name     string `json:"name,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
}

// Validator validates JWT tokens against a JWKS endpoint.
type Validator struct {
	jwksURL    string
	issuer     string
	audience   string
	httpClient *http.Client

	mu      sync.RWMutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
	ttl     time.Duration

	// cb optionally wraps fetchJWKS calls under circuit breaker
	// protection. When the breaker is Open, fetchJWKS short-circuits
	// without contacting the JWKS endpoint; cached keys are served
	// instead via getKey (graceful degradation).
	cb *circuitbreaker.Breaker
}

// NewValidator creates a JWT validator that fetches JWKS from the given URL.
// The audience parameter enforces strict audience validation on all tokens.
// MEET-2026-04-07-039: B4 fix — audience is mandatory per service.
func NewValidator(jwksURL, issuer, audience string) *Validator {
	return &Validator{
		jwksURL:  jwksURL,
		issuer:   issuer,
		audience: audience,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		keys: make(map[string]*rsa.PublicKey),
		ttl:  5 * time.Minute,
	}
}

// WithCircuitBreaker attaches a shared circuit breaker that wraps
// JWKS fetches. When the breaker opens (after repeated upstream
// failures), the validator continues serving cached keys until the
// breaker recovers — preventing Hydra outages from cascading into
// blanket auth failures.
//
// Per pkg-sota-faang-grade-plan T1.4 + ADR-0029 + gate PKG-G16.
//
// Returns the Validator so callers can chain construction:
//
//	v := jwt.NewValidator(jwksURL, issuer, audience).WithCircuitBreaker(cb)
func (v *Validator) WithCircuitBreaker(cb *circuitbreaker.Breaker) *Validator {
	v.cb = cb
	return v
}

// errJWKSCircuitOpen is returned by fetchJWKS when the circuit breaker
// short-circuited the fetch. Callers (getKey) interpret this as
// "use whatever cache you have".
var errJWKSCircuitOpen = errors.New("jwks fetch skipped: circuit breaker open")

// Validate parses and validates a JWT token, returning the claims.
//
// Validation steps (in order):
//  1. Parser-level whitelist via WithValidMethods(AllowedAccessAlgs)
//     rejects alg=none and alg!=RS256 BEFORE keyfunc runs.
//  2. typ header validation rejects cross-JWT confusion attacks.
//  3. Signing method type-assertion (defense in depth — should be
//     redundant with step 1 but kept as belt-and-suspenders).
//  4. JWKS lookup by kid + signature verification.
//  5. Issuer / audience / exp claim checks.
func (v *Validator) Validate(tokenString string) (*Claims, error) {
	token, err := goJWT.ParseWithClaims(tokenString, &Claims{}, func(token *goJWT.Token) (interface{}, error) {
		if err := validateTypHeader(token); err != nil {
			return nil, err
		}
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
	},
		goJWT.WithValidMethods(AllowedAccessAlgs),
		goJWT.WithIssuer(v.issuer),
		goJWT.WithAudience(v.audience),
		goJWT.WithExpirationRequired(),
	)

	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}

// SessionTokenIssuer issues and validates HMAC-SHA256 session JWTs.
// §2.2: Supports kid header and dual-key rotation (current + previous secret).
type SessionTokenIssuer struct {
	secret         []byte
	previousSecret []byte // nil if no rotation in progress
	kid            string // first 8 chars of SHA256(secret)
	issuer         string
	audience       string
	lifetime       time.Duration
}

// computeKID generates a key ID from the first 8 chars of the secret's SHA256 hash.
func computeKID(secret []byte) string {
	h := sha256.Sum256(secret)
	return fmt.Sprintf("%x", h[:4]) // 8 hex chars
}

// NewSessionTokenIssuer creates an issuer/validator for HMAC session tokens.
// previousSecret can be empty string if no rotation is in progress.
func NewSessionTokenIssuer(secret, issuer, audience string, lifetime time.Duration) *SessionTokenIssuer {
	s := &SessionTokenIssuer{
		secret:   []byte(secret),
		kid:      computeKID([]byte(secret)),
		issuer:   issuer,
		audience: audience,
		lifetime: lifetime,
	}
	return s
}

// WithPreviousSecret adds a previous secret for zero-downtime rotation.
// Tokens signed with the previous secret are accepted during the rotation window.
func (s *SessionTokenIssuer) WithPreviousSecret(previousSecret string) *SessionTokenIssuer {
	if previousSecret != "" {
		s.previousSecret = []byte(previousSecret)
	}
	return s
}

// Issue creates a signed HMAC JWT for a dashboard user.
// §2.2: Includes kid header for key identification during rotation.
func (s *SessionTokenIssuer) Issue(sub, email, name, tenantID string) (string, int64, error) {
	now := time.Now()
	claims := SessionClaims{
		RegisteredClaims: goJWT.RegisteredClaims{
			Subject:   sub,
			Issuer:    s.issuer,
			Audience:  goJWT.ClaimStrings{s.audience},
			IssuedAt:  goJWT.NewNumericDate(now),
			NotBefore: goJWT.NewNumericDate(now),
			ExpiresAt: goJWT.NewNumericDate(now.Add(s.lifetime)),
		},
		Email:    email,
		Name:     name,
		TenantID: tenantID,
	}

	token := goJWT.NewWithClaims(goJWT.SigningMethodHS256, claims)
	token.Header["kid"] = s.kid // §2.2: key identification
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", 0, fmt.Errorf("sign session token: %w", err)
	}
	return signed, int64(s.lifetime.Seconds()), nil
}

// ValidateSession parses and validates an HMAC session token.
// §2.2: Tries current secret first, then previous (for rotation window).
func (s *SessionTokenIssuer) ValidateSession(tokenString string) (*SessionClaims, error) {
	// Try current secret
	claims, err := s.validateWithSecret(tokenString, s.secret)
	if err == nil {
		return claims, nil
	}

	// Try previous secret if available (rotation window)
	if s.previousSecret != nil {
		claims, prevErr := s.validateWithSecret(tokenString, s.previousSecret)
		if prevErr == nil {
			return claims, nil
		}
	}

	return nil, fmt.Errorf("invalid session token: %w", err)
}

func (s *SessionTokenIssuer) validateWithSecret(tokenString string, secret []byte) (*SessionClaims, error) {
	token, err := goJWT.ParseWithClaims(tokenString, &SessionClaims{}, func(token *goJWT.Token) (interface{}, error) {
		if err := validateTypHeader(token); err != nil {
			return nil, err
		}
		if _, ok := token.Method.(*goJWT.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
	},
		goJWT.WithValidMethods(AllowedSessionAlgs),
		goJWT.WithIssuer(s.issuer),
		goJWT.WithAudience(s.audience),
		goJWT.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*SessionClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid session token claims")
	}
	return claims, nil
}

func (v *Validator) getKey(kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	stale := time.Since(v.fetched) > v.ttl || len(v.keys) == 0
	v.mu.RUnlock()
	if stale {
		if err := v.fetchJWKS(); err != nil {
			// If the CB is open, fall through to cached lookup —
			// graceful degradation. Any other fetch failure with
			// an empty cache propagates up as the original error.
			if !errors.Is(err, errJWKSCircuitOpen) {
				v.mu.RLock()
				cacheEmpty := len(v.keys) == 0
				v.mu.RUnlock()
				if cacheEmpty {
					return nil, err
				}
			}
		}
	}
	v.mu.RLock()
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
	// Fast path: cache still warm, nothing to do.
	v.mu.RLock()
	if time.Since(v.fetched) < v.ttl && len(v.keys) > 0 {
		v.mu.RUnlock()
		return nil
	}
	v.mu.RUnlock()

	// Wrap actual HTTP fetch in circuit breaker when configured.
	// gate PKG-G16: pkg/jwt eats its own dogfood (uses pkg/circuitbreaker).
	doFetch := func(ctx context.Context) (struct{}, error) {
		return struct{}{}, v.fetchJWKSLocked(ctx)
	}
	if v.cb != nil {
		_, err := circuitbreaker.Execute(context.Background(), v.cb, doFetch)
		if err != nil {
			// Translate ErrCircuitOpen into our sentinel so getKey
			// can decide to serve stale cache.
			if errors.Is(err, circuitbreaker.ErrCircuitOpen) {
				return errJWKSCircuitOpen
			}
			return err
		}
		return nil
	}
	_, err := doFetch(context.Background())
	return err
}

// fetchJWKSLocked performs the actual HTTP GET + parse + cache update
// under v.mu. Caller may run via Execute (CB) or directly.
func (v *Validator) fetchJWKSLocked(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if time.Since(v.fetched) < v.ttl && len(v.keys) > 0 {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return fmt.Errorf("JWKS request build failed: %w", err)
	}
	resp, err := v.httpClient.Do(req)
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
