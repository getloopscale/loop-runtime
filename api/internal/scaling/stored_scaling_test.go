// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scaling

import (
	"context"
	"errors"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/store"
)

type configsByApp map[string]*store.ScalingConfig

func (c configsByApp) GetByApp(_ context.Context, _, _, _, app string) (*store.ScalingConfig, error) {
	if row, ok := c[app]; ok {
		return row, nil
	}
	return nil, store.ErrScalingConfigNotFound
}

type failingConfigs struct{}

func (failingConfigs) GetByApp(context.Context, string, string, string, string) (*store.ScalingConfig, error) {
	return nil, errors.New("db down")
}

func TestStoredScaling_Apply_OverlaysOnlyTheAppsWithASavedConfig(t *testing.T) {
	stored := StoredScaling{Configs: configsByApp{"web": {MinReplicas: 4, MaxReplicas: 4}}}
	deployed := []db.BuildAppSpec{
		{Name: "web", Replicas: 1, Autoscaling: &db.BuildAppAutoscaling{MinReplicas: 1, MaxReplicas: 3}},
		{Name: "worker", Replicas: 2},
	}

	apps, err := stored.Apply(context.Background(), "tn-acme", "shop", "production", deployed)

	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if apps[0].Replicas != 4 || apps[0].Autoscaling != nil {
		t.Errorf("web = %+v, want the saved fixed 4 and no autoscaling", apps[0])
	}
	if apps[1].Replicas != 2 {
		t.Errorf("worker = %+v, want the deployed 2: it has no saved config", apps[1])
	}
	if deployed[0].Replicas != 1 || deployed[0].Autoscaling == nil {
		t.Errorf("the input was changed: %+v", deployed[0])
	}
}

func TestStoredScaling_ApplyToApp_ReportsWhetherAConfigIsSaved(t *testing.T) {
	stored := StoredScaling{Configs: configsByApp{"web": {MinReplicas: 2, MaxReplicas: 6}}}

	web, saved, err := stored.ApplyToApp(context.Background(), "tn-acme", "shop", "production", db.BuildAppSpec{Name: "web"})
	if err != nil || !saved {
		t.Fatalf("web: saved=%v err=%v, want a saved config", saved, err)
	}
	if web.Autoscaling == nil || web.Autoscaling.MinReplicas != 2 || web.Autoscaling.MaxReplicas != 6 {
		t.Errorf("web autoscaling = %+v, want 2..6", web.Autoscaling)
	}

	_, saved, err = stored.ApplyToApp(context.Background(), "tn-acme", "shop", "production", db.BuildAppSpec{Name: "worker"})
	if err != nil || saved {
		t.Errorf("worker: saved=%v err=%v, want no saved config and no error", saved, err)
	}
}

func TestStoredScaling_Apply_ReportsAReadFailure(t *testing.T) {
	_, err := StoredScaling{Configs: failingConfigs{}}.Apply(context.Background(), "tn-acme", "shop", "production",
		[]db.BuildAppSpec{{Name: "web"}})

	if err == nil {
		t.Fatal("a config that cannot be read must not be rendered as if none were saved")
	}
}

func TestStoredScaling_ApplyToApp_CarriesTheSavedTargets(t *testing.T) {
	cpu, memory := int32(70), int32(60)
	stored := StoredScaling{Configs: configsByApp{"web": {
		MinReplicas: 1, MaxReplicas: 3, TargetCPUPct: &cpu, TargetMemoryPct: &memory,
	}}}

	app, saved, err := stored.ApplyToApp(context.Background(), "tn-acme", "shop", "production",
		db.BuildAppSpec{Name: "web", Replicas: 1})

	if err != nil || !saved {
		t.Fatalf("ApplyToApp: saved=%v err=%v", saved, err)
	}
	if a := app.Autoscaling; a == nil || a.TargetCPU != 70 || a.TargetMemory != 60 {
		t.Errorf("autoscaling = %+v, want cpu 70 and memory 60", a)
	}
}
