// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package webhook

import (
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
)

// promCounterValue reads the current value of the
// loop_runtime_github_webhook_deliveries_total counter for a given label tuple.
// Uses prometheus/testutil.ToFloat64 -- the canonical hermetic-test
// affordance.
func promCounterValue(eventType, status string) float64 {
	return testutil.ToFloat64(metrics.GitHubWebhookDeliveriesTotal.WithLabelValues(eventType, status))
}
