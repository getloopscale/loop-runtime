// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"context"
	"fmt"

	goJWT "github.com/golang-jwt/jwt/v5"
	loopzerojwt "github.com/loopzero/loopzero/pkg/jwt"
)

// JWTValidator validates RSA JWT tokens against Ory Hydra's JWKS endpoint.
// Used for CLI authentication (Device Code Flow → Hydra-issued JWT).
type JWTValidator struct {
	validator *loopzerojwt.Validator
}

// NewJWTValidator creates a JWT token validator.
func NewJWTValidator(jwksURL, issuer string) *JWTValidator {
	return &JWTValidator{
		validator: loopzerojwt.NewValidator(jwksURL, issuer),
	}
}

// ValidateToken validates a Hydra RSA JWT.
func (v *JWTValidator) ValidateToken(_ context.Context, token string) (*TokenValidationResult, error) {
	claims, err := v.validator.Validate(token)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	return &TokenValidationResult{
		UserID: claims.Subject,
		Email:  claims.Email,
	}, nil
}

// HMACTokenValidator validates HMAC-SHA256 session tokens issued by the cloud service.
// Used for dashboard authentication (Kratos session → BFF token exchange → HMAC JWT).
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

// JWTOnlyValidator validates JWT tokens only (no GitHub PAT).
// Tries Hydra RSA JWT first, then HMAC session token.
type JWTOnlyValidator struct {
	rsaValidator  *JWTValidator
	hmacValidator *HMACTokenValidator
}

// NewJWTOnlyValidator creates a validator that accepts RSA + HMAC JWTs.
func NewJWTOnlyValidator(rsaValidator *JWTValidator, hmacValidator *HMACTokenValidator) *JWTOnlyValidator {
	return &JWTOnlyValidator{
		rsaValidator:  rsaValidator,
		hmacValidator: hmacValidator,
	}
}

// ValidateToken tries RSA JWT first, then HMAC session token.
func (v *JWTOnlyValidator) ValidateToken(ctx context.Context, token string) (*TokenValidationResult, error) {
	// Try RSA JWT (Hydra — CLI)
	if v.rsaValidator != nil {
		result, err := v.rsaValidator.ValidateToken(ctx, token)
		if err == nil {
			return result, nil
		}
	}

	// Try HMAC session token (dashboard)
	if v.hmacValidator != nil {
		result, err := v.hmacValidator.ValidateToken(ctx, token)
		if err == nil {
			return result, nil
		}
	}

	return nil, ErrInvalidToken
}
