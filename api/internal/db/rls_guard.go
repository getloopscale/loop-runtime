// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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

	reason := fmt.Sprintf("the role owns %s, and an owner escapes FORCE by holding the DDL "+
		"that can lift it (`ALTER TABLE ... NO FORCE`)", probeTable)
	fix := "point DATABASE_URL at the non-owner request role (loop_runtime_api)"
	if posture.BypassRLS {
		reason = "the role has BYPASSRLS, so isolation policies are never evaluated"
		fix = "point DATABASE_URL at a non-owner role without BYPASSRLS (loop_runtime_api), " +
			"or run `ALTER ROLE <role> NOBYPASSRLS`"
	}

	return fmt.Errorf(
		"db: refusing to serve requests — the request pool can read across tenants: %s.\n"+
			"  cause: %s\n"+
			"  fix:   %s\n"+
			"Background loops that legitimately span tenants use loop_runtime_worker, which reaches "+
			"them through the named *_worker_cross_tenant policies (migration 000062)",
		posture, reason, fix)
}

// BackgroundPosture is the measured cross-tenant reach of a connection used by
// background loops. Read from the live catalog; nothing is inferred from config.
type BackgroundPosture struct {
	Role       string // current_user
	BypassRLS  bool   // pg_roles.rolbypassrls
	IsOwner    bool   // owns the probe table
	NamedReach bool   // a policy on the probe table names THIS role and admits everything
	Policy     string // the policy that grants it, for the log line
	ProbeTable string
}

func (p BackgroundPosture) String() string {
	return fmt.Sprintf("role=%q bypassrls=%v owner_of_%s=%v named_cross_tenant_policy=%q",
		p.Role, p.BypassRLS, p.ProbeTable, p.IsOwner, p.Policy)
}

// MeasureBackgroundReach reads whether this connection can legitimately see
// every tenant's rows in probeTable, and by what means.
//
// The distinction the two booleans draw is the whole point. Reaching every
// tenant through a policy that NAMES the role is auditable (`pg_policies` lists
// it), revocable one table at a time, and carries no DDL. Reaching them by
// owning the table, or by BYPASSRLS, is the same reach with none of those
// properties — and it is what migrations 000063/000064 removed.
func MeasureBackgroundReach(ctx context.Context, pool *pgxpool.Pool, probeTable string) (BackgroundPosture, error) {
	p := BackgroundPosture{ProbeTable: probeTable}

	base, err := MeasureRLSPosture(ctx, pool, probeTable)
	if err != nil {
		return p, err
	}
	p.Role, p.BypassRLS, p.IsOwner = base.Role, base.BypassRLS, base.IsOwner

	// `qual IS NULL` covers a policy whose USING is absent (permissive on read
	// for the named role); `qual = 'true'` covers the explicit form migration
	// 000062 writes. Both admit every row, which is what "cross-tenant" means.
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(
		          (SELECT policyname FROM pg_policies
		            WHERE schemaname = 'public'
		              AND tablename = $1
		              AND current_user = ANY(roles)
		              AND (qual IS NULL OR qual = 'true')
		            ORDER BY policyname
		            LIMIT 1), '')`,
		probeTable,
	).Scan(&p.Policy); err != nil {
		return p, fmt.Errorf("db: reading cross-tenant policies for %q: %w", probeTable, err)
	}
	p.NamedReach = p.Policy != ""

	return p, nil
}

// AssertBackgroundPoolCrossTenant refuses to start background loops over a
// connection that cannot legitimately span tenants.
//
// WHY THIS IS A BOOT CHECK AND NOT A COMMENT. Background work — retention
// pruning, build reconciliation, idempotency cleanup — is cross-tenant by
// definition. Since migration 000062 the tenant tables are fail-closed, and the
// request role holds no cross-tenant policy: over that pool a sweep does not
// fail, it finds nothing. Measured on 2026-08-14 against a real database
// holding one audit_log row: the owner without context read 0, loop_runtime_api read 0,
// loop_runtime_worker read 1.
//
// "Deleted 0 rows" and "reconciled 0 builds" are indistinguishable from a quiet
// night. There is no runtime symptom, no error, and no metric that separates
// them — which is precisely the shape of defect the isolation work exists to
// remove, reappearing on the other side of the boundary.
//
// The check refuses BOTH failure modes, and they are different:
//
//   - no reach at all (loop_runtime_api) → the loops would sweep nothing;
//   - reach through an UNNAMED privilege (owner, BYPASSRLS) → the loops would
//     work, while holding a broad standing privilege that no auditor can
//     enumerate and nobody can revoke per table.
//
// Fixing it is one environment variable. Discovering months later that
// retention never ran is not.
func AssertBackgroundPoolCrossTenant(ctx context.Context, pool *pgxpool.Pool) error {
	const probeTable = "audit_log"

	posture, err := MeasureBackgroundReach(ctx, pool, probeTable)
	if err != nil {
		return fmt.Errorf("db: cannot verify cross-tenant reach on the background pool: %w", err)
	}

	if posture.BypassRLS || posture.IsOwner {
		return fmt.Errorf(
			"db: refusing to start background loops — the pool reaches every tenant through an "+
				"UNNAMED privilege: %s.\n"+
				"  cause: %s\n"+
				"  fix:   point LOOP_RUNTIME_WORKER_DATABASE_URL at loop_runtime_worker, whose reach is the named "+
				"*_worker_cross_tenant policies (migration 000062) — enumerable in pg_policies and "+
				"revocable one table at a time",
			posture,
			map[bool]string{true: "the role has BYPASSRLS, so no policy is ever evaluated",
				false: "the role owns the tenant tables and holds the DDL that can lift FORCE"}[posture.BypassRLS],
		)
	}

	if !posture.NamedReach {
		return fmt.Errorf(
			"db: refusing to start background loops — the pool CANNOT cross tenants: %s.\n"+
				"  cause: no policy on %s names this role, and the tables are fail-closed since "+
				"migration 000062, so every sweep would read and delete NOTHING — silently\n"+
				"  fix:   set LOOP_RUNTIME_WORKER_DATABASE_URL to the loop_runtime_worker role (migration 000023 "+
				"created it; 000062 gave it the named *_worker_cross_tenant policies)",
			posture, probeTable,
		)
	}

	return nil
}
