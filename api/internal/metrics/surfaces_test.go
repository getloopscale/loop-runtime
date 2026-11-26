// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loop-runtime/api/internal/surfaces"
)

func TestPublishSurfaces_ZeroForDisabledOneForEnabled(t *testing.T) {
	PublishSurfaces([]surfaces.Surface{
		{Name: "github_webhook", Enabled: false, Reason: "GITHUB_WEBHOOK_SECRET missing"},
		{Name: "billing", Enabled: true},
	})

	if got := testutil.ToFloat64(SurfaceEnabled.WithLabelValues("github_webhook")); got != 0 {
		t.Errorf("disabled surface = %v, want 0", got)
	}
	if got := testutil.ToFloat64(SurfaceEnabled.WithLabelValues("billing")); got != 1 {
		t.Errorf("enabled surface = %v, want 1", got)
	}
}

func TestPublishSurfaces_EnabledEmitsASeriesInsteadOfAbsence(t *testing.T) {
	// An absent series and a zero are indistinguishable to an alert. An ENABLED
	// surface must emit 1 explicitly, or the dashboard cannot tell "it is up"
	// from "this version does not even know this surface" — the ambiguity B-104
	// addresses.
	PublishSurfaces([]surfaces.Surface{{Name: "only_enabled", Enabled: true}})

	n := testutil.CollectAndCount(SurfaceEnabled)
	if n == 0 {
		t.Fatal("no series collected for an enabled surface")
	}
}

func TestSurfaceEnabled_ReasonIsNotALabel(t *testing.T) {
	// The reason is free text. Free text in a label is unbounded cardinality,
	// which is how a collector falls over. It belongs in the startup summary.
	desc := SurfaceEnabled.WithLabelValues("any").Desc().String()
	for _, forbidden := range []string{"reason", "detail"} {
		if strings.Contains(strings.ToLower(desc), forbidden) {
			t.Errorf("the metric descriptor carries the forbidden label %q: %s", forbidden, desc)
		}
	}
	if !strings.Contains(desc, "surface") {
		t.Errorf("the descriptor does not declare the `surface` label: %s", desc)
	}
}

func TestSurfaceEnabled_CanonicalName(t *testing.T) {
	// The name is an operations contract: dashboards and alerts point at it.
	if !strings.Contains(SurfaceEnabled.WithLabelValues("x").Desc().String(), "loopruntime_api_surface_enabled") {
		t.Error("the metric is not named `loopruntime_api_surface_enabled`")
	}
}

func TestPublishSurfaces_EmptyListDoesNotPanic(t *testing.T) {
	// A binary registering no surface at all is unlikely, but publishing nothing
	// must be a silent no-op, not a crash at startup.
	PublishSurfaces(nil)
	PublishSurfaces([]surfaces.Surface{})
}

var _ prometheus.Collector = SurfaceEnabled
