// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"context"
	"fmt"

	goJWT "github.com/golang-jwt/jwt/v5"
	loopzerojwt "github.com/loopzero/loopzero/pkg/jwt"
)

// JWTValidator validates JWT tokens against Ory Hydra's JWKS endpoint.
// Implements the TokenValidator interface for the engine API.
type JWTValidator struct {
	validator *loopzerojwt.Validator
}

// NewJWTValidator creates a JWT token validator.
func NewJWTValidator(jwksURL, issuer string) *JWTValidator {
	return &JWTValidator{
		validator: loopzerojwt.NewValidator(jwksURL, issuer),
	}
}

// ValidateToken validates a JWT and returns a TokenValidationResult compatible
// with the existing auth middleware.
func (v *JWTValidator) ValidateToken(_ context.Context, token string) (*TokenValidationResult, error) {
	claims, err := v.validator.Validate(token)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	return &TokenValidationResult{
		UserID:   claims.Subject,
		Email:    claims.Email,
		GitHubID: 0, // JWT tokens don't carry GitHub ID
	}, nil
}

// DualValidator tries JWT validation first, then HMAC session token, then GitHub PAT.
// Accepts all three token types for unified auth across CLI and dashboard.
type DualValidator struct {
	jwtValidator  *JWTValidator
	hmacValidator *HMACTokenValidator
	patValidator  TokenValidator // Existing GitHub PAT validator
}

// NewDualValidator creates a validator that accepts JWT, HMAC session, and GitHub PAT tokens.
// If jwtValidator or hmacValidator is nil, that validation path is skipped.
func NewDualValidator(jwtValidator *JWTValidator, hmacValidator *HMACTokenValidator, patValidator TokenValidator) *DualValidator {
	return &DualValidator{
		jwtValidator:  jwtValidator,
		hmacValidator: hmacValidator,
		patValidator:  patValidator,
	}
}

// ValidateToken routes to the appropriate validator based on token format.
// - Tokens starting with "ghp_", "gho_", or "github_pat_" → GitHub PAT validator
// - Tokens with 3 dot-separated segments → JWT validator
// - Everything else → try JWT first, then PAT
func (v *DualValidator) ValidateToken(ctx context.Context, token string) (*TokenValidationResult, error) {
	// If no JWT validator configured, fall back to PAT only
	if v.jwtValidator == nil {
		return v.patValidator.ValidateToken(ctx, token)
	}

	// Route by token format
	if loopzerojwt.IsGitHubPAT(token) {
		return v.patValidator.ValidateToken(ctx, token)
	}

	if loopzerojwt.IsJWTToken(token) {
		return v.jwtValidator.ValidateToken(ctx, token)
	}

	// Unknown format — try JWT first, then HMAC, then PAT
	result, err := v.jwtValidator.ValidateToken(ctx, token)
	if err == nil {
		return result, nil
	}
	if v.hmacValidator != nil {
		result, err := v.hmacValidator.ValidateToken(ctx, token)
		if err == nil {
			return result, nil
		}
	}
	return v.patValidator.ValidateToken(ctx, token)
}

// HMACTokenValidator validates HMAC-SHA256 session tokens issued by the cloud service.
// Used for dashboard → engine API authentication.
type HMACTokenValidator struct {
	secret   []byte
	issuer   string
	audience string
}

// NewHMACTokenValidator creates a validator for cloud service session JWTs.
func NewHMACTokenValidator(secret, issuer string) *HMACTokenValidator {
	return &HMACTokenValidator{
		secret:   []byte(secret),
		issuer:   issuer,
		audience: "loopzero-cloud",
	}
}

// SessionClaims for HMAC tokens (matches cloud service SessionClaims).
type SessionClaims struct {
	goJWT.RegisteredClaims
	Email    string `json:"email,omitempty"`
	Name     string `json:"name,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
}

// ValidateToken validates an HMAC session token.
func (v *HMACTokenValidator) ValidateToken(_ context.Context, token string) (*TokenValidationResult, error) {
	parsed, err := goJWT.ParseWithClaims(token, &SessionClaims{}, func(t *goJWT.Token) (interface{}, error) {
		if _, ok := t.Method.(*goJWT.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return v.secret, nil
	},
		goJWT.WithIssuer(v.issuer),
		goJWT.WithAudience(v.audience),
		goJWT.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	claims, ok := parsed.Claims.(*SessionClaims)
	if !ok || !parsed.Valid {
		return nil, fmt.Errorf("%w: invalid claims", ErrInvalidToken)
	}

	return &TokenValidationResult{
		UserID: claims.RegisteredClaims.Subject,
		Email:  claims.Email,
	}, nil
}
