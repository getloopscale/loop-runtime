// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

// domain_types.go — public struct for the custom_domains lifecycle
// store that survives the T2.1 (Batch A #9) deletion of the legacy
// 3-file split (domain_store.go + domain_store_background.go +
// domain_store_cfzone.go). The 2 prod call sites use the struct-
// literal `&db.PostgresDomainStore{DB: dbAdapter}` — preserved here.
//
// SEC-005 sentinel: the legacy isParameterizedCondition regex
// validator that protected against fmt.Sprintf-style raw value
// injection in ListByTenant is DROPPED — sqlc-emitted SQL never has
// raw value interpolation by construction. The dynamic-filter
// pattern uses sqlc.narg() nullable args instead. Equivalent SEC-005
// guarantee, but enforced by code generation rather than runtime
// regex.
//
// The isParameterizedCondition helper + paramConditionRegex remain
// available below for any caller that still composes runtime SQL
// (e.g., custom report queries). They're not used by the wrapper.

import (
	"regexp"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// PostgresDomainStore implements ports.DomainStore (the request path) and
// the services/domain background ports using PostgreSQL via sqlc.
//
// T2.1 Batch A #9 (plan 2026-05-29-sqlc-migration v1.3): DB field
// type preserved (db.DBTX); 22+ methods (domain_wrapper.go)
// dispatch through SqlcBridge to sqlc-generated queries.
type PostgresDomainStore struct {
	DB db.DBTX
}

var _ ports.DomainStore = (*PostgresDomainStore)(nil)

// isParameterizedCondition validates that a SQL WHERE condition
// follows the `<column> = $N` pattern (where N is one or more
// digits). Preserved from legacy as a defense-in-depth helper for
// callers that still compose runtime SQL strings outside the sqlc
// wrapper.
//
// Accepts (sample):
//
//	"tenant_id = $1"
//	"app_name = $42"
//	"environment_id = $7"
//
// Rejects:
//
//	"tenant_id = 'literal'"        (raw value)
//	"tenant_id = 'foo' OR 1=1"     (injection attempt)
//	"tenant_id = $1 OR 1=1"        (extra clauses)
//
// Tightly anchored: ^[a-z_][a-z0-9_]* = \$[0-9]+$
func isParameterizedCondition(c string) bool {
	return paramConditionRegex.MatchString(c)
}

var paramConditionRegex = regexp.MustCompile(`^[a-z_][a-z0-9_]* = \$[0-9]+$`)
