// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"context"
	"fmt"

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

// DualValidator tries JWT validation first, then falls back to GitHub PAT validation.
// During the migration period, both token types are accepted.
type DualValidator struct {
	jwtValidator *JWTValidator
	patValidator TokenValidator // Existing GitHub PAT validator
}

// NewDualValidator creates a validator that accepts both JWT and GitHub PAT tokens.
// If jwtValidator is nil, only PAT validation is used (backward compatible).
func NewDualValidator(jwtValidator *JWTValidator, patValidator TokenValidator) *DualValidator {
	return &DualValidator{
		jwtValidator: jwtValidator,
		patValidator: patValidator,
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

	// Unknown format — try JWT first, then PAT
	result, err := v.jwtValidator.ValidateToken(ctx, token)
	if err == nil {
		return result, nil
	}
	return v.patValidator.ValidateToken(ctx, token)
}
