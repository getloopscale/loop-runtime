// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"context"
	"fmt"

	loopzerojwt "github.com/getloopscale/contracts/jwt"
)

// JWTOnlyValidator validates JWT tokens — RSA (Hydra) + HMAC session (dashboard).
// No GitHub PAT. Single source of truth: pkg/jwt.
type JWTOnlyValidator struct {
	rsaValidator  *loopzerojwt.Validator
	sessValidator *loopzerojwt.SessionTokenIssuer
}

// NewJWTOnlyValidator creates a validator that accepts RSA + HMAC JWTs.
func NewJWTOnlyValidator(rsaValidator *loopzerojwt.Validator, sessValidator *loopzerojwt.SessionTokenIssuer) *JWTOnlyValidator {
	return &JWTOnlyValidator{
		rsaValidator:  rsaValidator,
		sessValidator: sessValidator,
	}
}

// ValidateToken tries RSA JWT first, then HMAC session token.
func (v *JWTOnlyValidator) ValidateToken(ctx context.Context, token string) (*TokenValidationResult, error) {
	// Try RSA JWT (Hydra — CLI)
	if v.rsaValidator != nil {
		claims, err := v.rsaValidator.Validate(token)
		if err == nil {
			email := claims.Email
			if email == "" {
				email = claims.Ext.Email
			}
			return &TokenValidationResult{
				UserID: claims.Subject,
				Email:  email,
			}, nil
		}
	}

	// Try HMAC session token (dashboard)
	if v.sessValidator != nil {
		claims, err := v.sessValidator.ValidateSession(token)
		if err == nil {
			return &TokenValidationResult{
				UserID: claims.Subject,
				Email:  claims.Email,
			}, nil
		}
	}

	return nil, fmt.Errorf("%w: no valid JWT", ErrInvalidToken)
}
