// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"net/http"

	"github.com/getloopscale/loopzero/api/internal/middleware"
	"github.com/getloopscale/loopzero/api/internal/routes/routestest"
)

// The bodies moved to routes/routestest so that a family split into its own
// subpackage keeps access to them — a helper in a _test.go is compiled only into
// its own package's test binary, and the `tenants` split proved it by failing on
// `undefined: withAuth`.
//
// These four remain as thin delegations rather than being rewritten across the
// thirty test files that call them: the rename is mechanical but it is not this
// slice's subject, and duplicating the bodies would be the DRY violation the move
// exists to avoid. Each family that moves swaps its own calls, the way tenants did.

func withAuth(r *http.Request, uc *middleware.UserContext) *http.Request {
	return routestest.WithAuth(r, uc)
}

func withChiParam(r *http.Request, key, value string) *http.Request {
	return routestest.WithChiParam(r, key, value)
}

func defaultUserContext() *middleware.UserContext { return routestest.DefaultUserContext() }

func memberUserContext() *middleware.UserContext { return routestest.MemberUserContext() }
