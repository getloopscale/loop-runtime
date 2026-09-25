// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/getloopscale/loopzero/api/internal/db/dbtx"
)

// tenantScoper sets the Row Level Security variable `app.tenant_id` on every
// connection the request pool hands out, from the tenant the caller's context
// carries (dbtx.WithTenantScope).
//
// # Why the pool does it and not each store
//
// Migration 000062 made every tenant table fail-closed: a statement sees only
// the rows of the tenant named by `app.tenant_id`, and the request role
// (loopzero_api) holds no other policy. Until this existed nothing on the request
// path set the variable outside the environment lock, so an authenticated
// request read nothing from any tenant table and every write to one was
// refused — the membership lookup found no team, and reads returned empty
// results and default values with no error. Scoping each store would repeat
// one rule in every one of them; scoping the connection makes it impossible to
// forget.
//
// # Why a session setting is safe here
//
// The value is re-established on every acquisition, before the caller's first
// statement: a connection last used for tenant A and handed to tenant B is
// switched to B, and handed to an unscoped caller it is cleared. A transaction
// that sets the variable locally (the environment lock) overrides it for its
// own duration and PostgreSQL restores it at COMMIT or ROLLBACK.
//
// An unscoped acquisition sets the variable to the empty string, on a fresh
// connection as on a reused one, so the answer never depends on what the
// connection did before. The empty string is the partition of rows that belong
// to no tenant — the pre-authentication audit records, written with an
// empty tenant_id — and no tenant's rows. A fresh session would otherwise read
// NULL and a cleared one the empty string, and those two admit different rows.
type tenantScoper struct {
	mu sync.Mutex
	// scopeOf is what each live connection's `app.tenant_id` holds, so an
	// acquisition for the same tenant costs no round trip.
	scopeOf map[*pgx.Conn]string
}

func newTenantScoper() *tenantScoper {
	return &tenantScoper{scopeOf: map[*pgx.Conn]string{}}
}

// prepare is the pool's PrepareConn hook. A failure to set the scope destroys
// the connection and fails the caller's statement: running it with a stale
// scope would show it another tenant's rows.
func (s *tenantScoper) prepare(ctx context.Context, conn *pgx.Conn) (bool, error) {
	want, _ := dbtx.TenantScope(ctx)

	s.mu.Lock()
	have, set := s.scopeOf[conn]
	s.mu.Unlock()
	if set && have == want {
		return true, nil
	}

	if _, err := conn.Exec(ctx, "SELECT set_config('app.tenant_id', $1, false)", want); err != nil {
		s.forget(conn)
		return false, fmt.Errorf("db: scope the connection to tenant %q: %w", want, err)
	}
	s.mu.Lock()
	s.scopeOf[conn] = want
	s.mu.Unlock()
	return true, nil
}

// forget drops a closed connection from the record. It is the pool's
// BeforeClose hook.
func (s *tenantScoper) forget(conn *pgx.Conn) {
	s.mu.Lock()
	delete(s.scopeOf, conn)
	s.mu.Unlock()
}
