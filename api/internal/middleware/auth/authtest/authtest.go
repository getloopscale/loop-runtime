// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package authtest builds request contexts carrying an authenticated user,
// for handler tests in other packages. Test-only by convention, like
// net/http/httptest.
package authtest

import (
	"context"

	"github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
)

// Context returns a context holding the UserContext the auth middleware
// would have set for userID in tenantID with role.
func Context(userID, tenantID, role string) context.Context {
	return context.WithValue(context.Background(), auth.UserContextKeyPublic(), &auth.UserContext{
		UserID:   userID,
		Email:    userID + "@test.com",
		TenantID: tenantID,
		Role:     role,
	})
}
