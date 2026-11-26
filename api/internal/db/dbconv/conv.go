// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package dbconv holds the shared, stateless pgtype conversion helpers used by
// the db-store adapters.
//
// M9 Step 5 (5a-prep): these were unexported package-level funcs in the flat
// internal/db package (release_wrapper.go, idempotency_types.go). Relocating
// them to a leaf package (imported by both the ports package and the future
// db/store/ adapter subpackage) is the prerequisite for moving the entity
// adapters out of the flat db package without duplicating the helpers or
// exporting a status-layer file's private funcs (per MEET-2026-07-03-001,
// distributed-systems condition on shared-helper relocation). Pure functions;
// behavior-preserving.
package dbconv

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// TimeToTimestamptz wraps a time.Time as a valid pgtype.Timestamptz.
func TimeToTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// StringEmptyToNilPtr returns nil for the empty string, else a pointer to s.
func StringEmptyToNilPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// TimePtrToTimestamptz converts a *time.Time to a pgtype.Timestamptz. nil →
// Valid=false.
func TimePtrToTimestamptz(p *time.Time) pgtype.Timestamptz {
	if p == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *p, Valid: true}
}
