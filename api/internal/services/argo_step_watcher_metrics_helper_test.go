// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"github.com/prometheus/client_golang/prometheus/testutil"

	cloudmetrics "github.com/loopzero/loopzero/api/internal/metrics"
)

// readArgoWatcherCounter exposes the counter value for tests via the
// canonical promtest helper.
func readArgoWatcherCounter() float64 {
	return testutil.ToFloat64(cloudmetrics.ArgoWatcherTokenExpiredTotal)
}
