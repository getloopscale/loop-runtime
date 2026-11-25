// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statusadapter

import (
	"context"
	"errors"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/status/statushttp"
)

type fakeBuildApps struct {
	apps []db.BuildAppSpec
	err  error
}

func (f fakeBuildApps) GetBuildApps(context.Context, string) ([]db.BuildAppSpec, error) {
	return f.apps, f.err
}

// savedScale stands for scaling.StoredScaling: it replaces the scaling of the
// apps it has a config for.
type savedScale map[string]db.BuildAppSpec

func (s savedScale) ApplyToApp(_ context.Context, _, _, _ string, app db.BuildAppSpec) (db.BuildAppSpec, bool, error) {
	saved, ok := s[app.Name]
	if !ok {
		return app, false, nil
	}
	app.Replicas, app.Autoscaling = saved.Replicas, saved.Autoscaling
	return app, true, nil
}

func TestStatusScaleAdapter_ReportsTheSavedConfigAndElseWhatTheBuildRendered(t *testing.T) {
	adapter := StatusScaleAdapter{
		Builds: fakeBuildApps{apps: []db.BuildAppSpec{
			{Name: "web", Replicas: 1},
			{Name: "api", Autoscaling: &db.BuildAppAutoscaling{MinReplicas: 2, MaxReplicas: 5}},
			{Name: "worker"},
			{Name: "cron", Replicas: 1},
		}},
		Stored: savedScale{
			"web":  {Replicas: 4},
			"cron": {Autoscaling: &db.BuildAppAutoscaling{MinReplicas: 0, MaxReplicas: 3}},
		},
	}

	got, err := adapter.DesiredScale(context.Background(), "tn-acme", "shop", "production", "bld-1")

	if err != nil {
		t.Fatalf("DesiredScale: %v", err)
	}
	want := []statushttp.StatusAppScale{
		{App: "web", Replicas: 4, Min: 4, Max: 4, Saved: true},
		{App: "api", Replicas: 2, Min: 2, Max: 5},
		// No replicas declared: the Rollout is rendered with the default.
		{App: "worker", Replicas: 2},
		// Saved scale to zero: the workload is rendered with no replica.
		{App: "cron", Replicas: 0, Min: 0, Max: 3, Saved: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d apps, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("app %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestStatusScaleAdapter_ReportsAReadFailure(t *testing.T) {
	adapter := StatusScaleAdapter{Builds: fakeBuildApps{err: errors.New("db down")}, Stored: savedScale{}}

	if _, err := adapter.DesiredScale(context.Background(), "tn-acme", "shop", "production", "bld-1"); err == nil {
		t.Fatal("a build whose apps cannot be read has no desired scale to report")
	}
}
