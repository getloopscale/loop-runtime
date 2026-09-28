// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
)

// rollbackTargetFrom projects a persisted release row onto the delivery
// contract's own type.
//
// Counterpart of activationBuildFrom in the build context (B-079): the release
// context knows db.Release, deploycontracts does not. Nil in, nil out — the
// orchestrator has a nil-delivery fallback path and must reach it rather than
// panic on the way.
func rollbackTargetFrom(r *db.Release) *deploycontracts.RollbackTarget {
	if r == nil {
		return nil
	}
	return &deploycontracts.RollbackTarget{
		ID:        r.ID,
		GitCommit: r.GitCommit,
	}
}
