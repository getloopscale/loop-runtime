// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package server

import (
	"net/http"

	"github.com/getloopscale/loop-runtime/api/internal/db/dbtx"
	mwauth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
)

// scopeDatabaseToCaller confines the request's database work to the tenant the
// authentication middleware resolved for the caller.
//
// The request pool runs as loop_runtime_api, and every tenant table is fail-closed
// under Row Level Security (migration 000062): a connection sees only the rows
// of the tenant `app.tenant_id` names. The pool sets that variable from the
// context this middleware produces (db.PoolConfig.TenantScoped), so every store
// a handler reaches is scoped without carrying the rule itself.
//
// It must run after the authentication middleware: the tenant it reads is one
// the caller was authenticated AND authorized for, which is what makes it safe
// to grant. A request with no resolved tenant passes through unscoped and sees
// no tenant's rows.
func scopeDatabaseToCaller(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uc := mwauth.GetUserContext(r.Context())
		if uc == nil || uc.TenantID == "" {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(dbtx.WithTenantScope(r.Context(), uc.TenantID)))
	})
}
