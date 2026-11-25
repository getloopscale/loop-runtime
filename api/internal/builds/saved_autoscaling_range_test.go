// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
	"github.com/getloopscale/loopzero/api/internal/scaling"
)

type savedRange struct{ min, max int32 }

func (s savedRange) GetByApp(context.Context, string, string, string, string) (*store.ScalingConfig, error) {
	return &store.ScalingConfig{MinReplicas: s.min, MaxReplicas: s.max}, nil
}

// A build of an app whose saved scale is an autoscaling range has to pass the
// scaling gate: the range is the platform's own answer, not an ambiguous request.
func TestEnforceScaling_AcceptsASavedAutoscalingRange(t *testing.T) {
	resolver, err := scaling.NewDBScalingResolver(savedRange{min: 1, max: 3})
	if err != nil {
		t.Fatalf("NewDBScalingResolver: %v", err)
	}
	// The source build was deployed with one fixed replica, as a redeploy copies it.
	apps, err := resolver.Resolve(context.Background(), "tn-x", "demo", "production",
		[]deploycontracts.AppSpec{{Name: "app", Replicas: 1}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := enforceScaling(context.Background(), apps, nil, "tn-x", false); err != nil {
		t.Fatalf("enforceScaling refused a saved range of 1 to 3: %v", err)
	}
	if apps[0].Autoscaling == nil || apps[0].Autoscaling.MinReplicas != 1 || apps[0].Autoscaling.MaxReplicas != 3 {
		t.Fatalf("resolved autoscaling = %+v, want the range 1 to 3", apps[0].Autoscaling)
	}
}
