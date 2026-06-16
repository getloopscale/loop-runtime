// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"time"

	"github.com/loopzero/loopzero/pkg/plan"
)

// CurrentPeriod returns the start and end of the current calendar month (UTC).
// M1.2: moved to pkg/plan (value vocabulary); re-exported here transitionally
// (plan m1-decouple-in-place D4).
var CurrentPeriod = plan.CurrentPeriod

// formatPeriodImpl is the helper invoked by formatPeriod() in cost_cap.go.
// Kept in its own file so the cost_cap.go file body stays focused on the
// enforcement logic without time-format scaffolding.
func formatPeriodImpl(unixSec int64) string {
	return time.Unix(unixSec, 0).UTC().Format("2006-01")
}
