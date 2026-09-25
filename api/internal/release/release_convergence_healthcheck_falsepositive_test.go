// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// TestReleaseConvergenceLoop_HealthCheck_ConvergingNotFlagged locks the fix for
// loopzero-cloud#242 follow-up (the promote-to-production "sem sucesso" false alarm).  // english-only: verbatim dashboard label
//
// Root cause: healthCheckActiveReleases discarded the `synced` return of
// GetAppStatus and flagged FalseSuccessTotal whenever `!healthy`. But during
// the ArgoCD reconcile-rewrite window the app's status is transiently empty /
// Unknown — GetAppStatus then returns synced=false, healthy=false (no workload
// resources visible yet), which is a STILL-CONVERGING state, not a
// post-activation health regression. The pre-fix code fired the false-success
// WARN + metric for it, making a live, healthy promotion look failed.
//
// Fix A: only evaluate post-activation health when the ArgoCD status is
// DETERMINATE (synced==true). A non-Synced app is converging or drifting —
// neither is a "healthy workload degraded" verdict.
//
// Genuine unhealth (synced==true, healthy==false → a matched workload is
// Degraded) MUST still flag.
func TestReleaseConvergenceLoop_HealthCheck_ConvergingNotFlagged(t *testing.T) {
	cases := []struct {
		name    string
		synced  bool
		healthy bool
		wantInc float64
		tenant  string
		env     string
	}{
		{"converging: sync empty/Unknown → skip (the #242 false alarm)", false, false, 0, "tn-converging", "production"},
		{"synced but workload Degraded → still flag", true, false, 1, "tn-degraded", "production"},
		{"synced and healthy → not flagged", true, true, 0, "tn-ok", "production"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rs := newFakeConvergenceReleaseStore()
			rs.active = []db.Release{
				{
					ID: "rel-x", BuildID: "build-1", TenantID: tc.tenant,
					ProjectID: "proj-1", Environment: tc.env,
					Status: string(statemachine.ReleaseActive),
					Apps:   json.RawMessage(`[{"name":"api","rendering":"container"}]`),
				},
			}
			sc := &fakeConvergenceSyncClient{synced: tc.synced, healthy: tc.healthy}
			loop := NewReleaseConvergenceLoop(
				rs, newFakeConvergenceEnvTargetStore(), sc, &fakeConvergenceLocker{},
				&fakeConvergenceEventStore{},
				"dev.loopzero.example", "", slog.Default(),
				ReleaseConvergenceLoopConfig{Interval: time.Second, HealthInterval: time.Second},
			)

			before := testutil.ToFloat64(metrics.FalseSuccessTotal.WithLabelValues(tc.tenant, tc.env))
			loop.healthCheckActiveReleases(context.Background())
			got := testutil.ToFloat64(metrics.FalseSuccessTotal.WithLabelValues(tc.tenant, tc.env)) - before

			if got != tc.wantInc {
				t.Errorf("FalseSuccessTotal delta = %v, want %v (synced=%v healthy=%v)", got, tc.wantInc, tc.synced, tc.healthy)
			}

			// Health check is observability-only regardless of verdict.
			if len(rs.lastStatus) > 0 {
				t.Error("health check must NOT change release state")
			}
		})
	}
}
