// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RLSPosture is the measured privilege posture of a database connection with
// respect to Row Level Security. Every field is read from the live catalog —
// nothing here is inferred from configuration.
type RLSPosture struct {
	Role       string // current_user
	BypassRLS  bool   // pg_roles.rolbypassrls
	IsOwner    bool   // owns the probe table
	ForceRLS   bool   // probe table has FORCE ROW LEVEL SECURITY
	ProbeTable string
}

// CanBypass reports whether this connection escapes the tenant-isolation
// policies. Two independent escapes exist and either one is sufficient:
//
//   - BYPASSRLS short-circuits policy evaluation entirely.
//   - Ownership. An owner is exempt from its own policies unless FORCE is set,
//     AND migration 000062 grants the owner a *_owner_maintenance policy so
//     migrations and admin scripts keep working. Either way it reads across
//     tenants.
//
// Ownership is treated as disqualifying regardless of FORCE, because FORCE is
// not a boundary against an owner: the owner holds DDL and can lift it with
// `ALTER TABLE ... NO FORCE`. A request path must simply not be the owner.
//
// This is the distinction that made the 2026-08-14 exposure survive review:
// the policies were present and looked correct, but the request pool never
// reached them.
func (p RLSPosture) CanBypass() bool {
	return p.BypassRLS || p.IsOwner
}

func (p RLSPosture) String() string {
	return fmt.Sprintf("role=%q bypassrls=%v owner_of_%s=%v force_rls=%v",
		p.Role, p.BypassRLS, p.ProbeTable, p.IsOwner, p.ForceRLS)
}

// MeasureRLSPosture reads the connection's actual isolation posture from the
// catalog, using probeTable (a tenant-scoped table) as the reference.
func MeasureRLSPosture(ctx context.Context, pool *pgxpool.Pool, probeTable string) (RLSPosture, error) {
	p := RLSPosture{ProbeTable: probeTable}

	if err := pool.QueryRow(ctx,
		`SELECT current_user, COALESCE(rolbypassrls, false)
		   FROM pg_roles WHERE rolname = current_user`,
	).Scan(&p.Role, &p.BypassRLS); err != nil {
		return p, fmt.Errorf("db: reading role attributes: %w", err)
	}

	if err := pool.QueryRow(ctx,
		`SELECT pg_get_userbyid(relowner) = current_user, relforcerowsecurity
		   FROM pg_class WHERE relname = $1 AND relkind = 'r'`,
		probeTable,
	).Scan(&p.IsOwner, &p.ForceRLS); err != nil {
		return p, fmt.Errorf("db: reading table posture for %q: %w", probeTable, err)
	}

	return p, nil
}

// AssertRequestPoolIsolated refuses to let the process serve requests over a
// connection that can read across tenants.
//
// WHY THIS EXISTS AS A BOOT CHECK rather than a code review rule: on
// 2026-08-14 the request pool was measured connecting as the table owner with
// rolbypassrls=true, so all twelve isolation policies were inert on every HTTP
// request. Nothing failed, nothing logged, and the integration test that
// should have caught it asserted isolation over that same un-isolatable
// connection. A configuration mistake of this shape is invisible at runtime —
// the only moment it can be caught cheaply is at startup, before the first
// request is served.
//
// Deliberately fails closed: a process that cannot prove isolation does not
// start. Refusing to boot is loud and recoverable in seconds; serving another
// tenant's rows is neither.
func AssertRequestPoolIsolated(ctx context.Context, pool *pgxpool.Pool) error {
	const probeTable = "builds"

	posture, err := MeasureRLSPosture(ctx, pool, probeTable)
	if err != nil {
		return fmt.Errorf("db: cannot verify tenant isolation on the request pool: %w", err)
	}

	if !posture.CanBypass() {
		return nil
	}

	reason := fmt.Sprintf("the role owns %s, and an owner both escapes FORCE by holding DDL "+
		"and carries the %s_owner_maintenance policy for migrations", probeTable, probeTable)
	fix := "point DATABASE_URL at the non-owner request role (loopzero_api)"
	if posture.BypassRLS {
		reason = "the role has BYPASSRLS, so isolation policies are never evaluated"
		fix = "point DATABASE_URL at a non-owner role without BYPASSRLS (loopzero_api), " +
			"or run `ALTER ROLE <role> NOBYPASSRLS`"
	}

	return fmt.Errorf(
		"db: refusing to serve requests — the request pool can read across tenants: %s.\n"+
			"  cause: %s\n"+
			"  fix:   %s\n"+
			"Background loops that legitimately span tenants use loopzero_worker, which reaches "+
			"them through the named *_worker_cross_tenant policies (migration 000062).",
		posture, reason, fix)
}
