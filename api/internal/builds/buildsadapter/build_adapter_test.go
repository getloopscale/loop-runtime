// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package buildsadapter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/builds/buildshttp"
	"github.com/getloopscale/loop-runtime/api/internal/db"
)

func TestAppSpecsFromInput_AnUntypedAppIsAServerOn8080(t *testing.T) {
	apps := appSpecsFromInput([]buildshttp.AppInput{{Name: "web", Runtime: "node"}})
	if apps[0].Type != "server" || apps[0].Port != 8080 {
		t.Errorf("untyped app became type=%q port=%d, want server/8080", apps[0].Type, apps[0].Port)
	}
}

func TestAppSpecsFromInput_ADeclaredPortIsKept(t *testing.T) {
	apps := appSpecsFromInput([]buildshttp.AppInput{{Name: "api", Type: "server", Port: 3000}})
	if apps[0].Port != 3000 {
		t.Errorf("port = %d, want 3000", apps[0].Port)
	}
}

func TestAppSpecsFromInput_AWorkerGetsNoDefaultPort(t *testing.T) {
	apps := appSpecsFromInput([]buildshttp.AppInput{{Name: "bg", Type: "worker"}})
	if apps[0].Port != 0 {
		t.Errorf("worker port = %d, want 0", apps[0].Port)
	}
}

func TestAppSpecsFromInput_CarriesBuildInstructionsAndAutoscaling(t *testing.T) {
	apps := appSpecsFromInput([]buildshttp.AppInput{{
		Name: "api", Path: "apps/api", Runtime: "python", Build: "dockerfile", Start: "python app.py",
		Autoscaling: &buildshttp.AppAutoscalingInput{Min: 1, Max: 4, TargetCPU: 60},
	}})
	a := apps[0]
	if a.Path != "apps/api" || a.Runtime != "python" || a.Build != "dockerfile" || a.Start != "python app.py" {
		t.Errorf("build instructions lost: %+v", a)
	}
	if a.Autoscaling == nil || a.Autoscaling.MinReplicas != 1 || a.Autoscaling.MaxReplicas != 4 || a.Autoscaling.TargetCPU != 60 {
		t.Errorf("autoscaling = %+v, want {1 4 60}", a.Autoscaling)
	}
}

func TestGetBuildStatus_ReportsTheBuildDuration(t *testing.T) {
	dur := 300
	a := &BuildOrchestratorAdapter{BuildStore: &fakeBuildStore{
		build: &db.Build{ID: "bld-dur-001", Status: "SUCCEEDED", CreatedAt: time.Now(), DurationSeconds: &dur},
	}}
	resp, err := a.GetBuildStatus(context.Background(), "bld-dur-001")
	if err != nil {
		t.Fatalf("GetBuildStatus: %v", err)
	}
	if resp.DurationSeconds == nil || *resp.DurationSeconds != 300 {
		t.Errorf("duration = %v, want 300", resp.DurationSeconds)
	}
}

type fakeBuildStoreWithGetError struct {
	fakeBuildStore
	err error
}

func (f *fakeBuildStoreWithGetError) GetByID(context.Context, string) (*db.Build, error) {
	return nil, f.err
}

func TestGetBuildStatus_PropagatesTheBuildLookupError(t *testing.T) {
	lookupErr := errors.New("db timeout")
	a := &BuildOrchestratorAdapter{BuildStore: &fakeBuildStoreWithGetError{err: lookupErr}}
	if _, err := a.GetBuildStatus(context.Background(), "bld-err"); !errors.Is(err, lookupErr) {
		t.Fatalf("want the lookup error, got %v", err)
	}
}

func TestBuildListStoreAdapter_NoBuildsIsAnEmptyPage(t *testing.T) {
	a := &BuildListStoreAdapter{Store: &fakeBuildListStore{}}
	results, total, err := a.ListByProjectAndEnv(context.Background(), "tn", "proj", "prod", 10, 0)
	if err != nil || total != 0 || len(results) != 0 {
		t.Fatalf("got (%v, %d, %v), want an empty page", results, total, err)
	}
}
