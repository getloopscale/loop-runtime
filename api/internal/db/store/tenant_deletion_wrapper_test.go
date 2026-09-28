// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/ports"
)

// TestTenantDeletionStoreSqlc_SatisfiesTheDomainInterface is the assertion whose
// absence let this whole defect live.
//
// `TenantDeletionStoreSqlc` was created on 2026-05-29 by the sqlc pilot, which
// deleted the legacy store in the same commit and never wired the new one into
// `server.Config`. Nothing referenced it, so on 2026-06-16 a code-quality pass
// correctly identified it as dead and removed it — leaving its doc comments
// behind, so every later reader believed the implementation existed.
//
// A compile-time assertion that the type satisfies the interface would not have
// saved it on its own (dead code satisfying an interface is still dead), but it
// is the floor: it fails the moment the type stops existing, instead of the file
// quietly becoming a paragraph of prose about nothing.
func TestTenantDeletionStoreSqlc_SatisfiesTheDomainInterface(t *testing.T) {
	var _ ports.TenantDeletionStore = (*TenantDeletionStoreSqlc)(nil)
}

// TestDeletionRowToRecord pins the conversion the adapter exists to perform.
//
// The queries speak `pgtype.Timestamptz`, the domain speaks `time.Time` and
// `*time.Time`. The adapter is the only piece that knows both, and the nil case
// is where an adapter like this usually goes wrong: an INVALID timestamptz must
// become a nil pointer, not the zero time — a tenant with no grace deadline and
// a tenant whose deadline is the year zero are different facts.
func TestDeletionRowToRecord(t *testing.T) {
	grace := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	requested := time.Date(2026, 8, 16, 10, 30, 0, 0, time.UTC)

	t.Run("a present grace deadline becomes a pointer to the instant", func(t *testing.T) {
		rec := deletionRowToRecord("tn-1", requested, "user@x", "user_request", &grace, true, 3)
		if rec.GraceUntil == nil {
			t.Fatal("GraceUntil = nil for a present deadline")
		}
		if !rec.GraceUntil.Equal(grace) {
			t.Errorf("GraceUntil = %v, want %v", *rec.GraceUntil, grace)
		}
		if rec.TenantID != "tn-1" || rec.RequestedBy != "user@x" || rec.Reason != "user_request" {
			t.Errorf("scalar fields lost: %+v", rec)
		}
		if !rec.Force || rec.AttemptCount != 3 {
			t.Errorf("Force/AttemptCount lost: force=%v count=%d", rec.Force, rec.AttemptCount)
		}
	})

	t.Run("an absent deadline becomes nil, not the zero instant", func(t *testing.T) {
		rec := deletionRowToRecord("tn-2", requested, "admin", "admin_evict", nil, false, 0)
		if rec.GraceUntil != nil {
			t.Errorf("GraceUntil = %v for an absent deadline — no deadline and a deadline in the year zero "+
				"are different facts, and whoever reads the answer decides based on that difference",
				*rec.GraceUntil)
		}
	})
}
