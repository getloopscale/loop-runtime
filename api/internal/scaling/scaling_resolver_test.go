// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Tests for ScalingResolver — see docs/plans/loop-runtime-scale-end-to-end-plan.md T1.1.
// RED tests written FIRST per TDD; implementation in scaling_resolver.go.

package scaling

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// fakeScalingStore is a stub implementing store.ScalingStore for tests.
// Each test sets the desired GetByApp behavior via the get callback.
type fakeScalingStore struct {
	get func(ctx context.Context, tenantID, projectID, env, app string) (*store.ScalingConfig, error)
}

func (f *fakeScalingStore) GetByApp(ctx context.Context, tenantID, projectID, env, app string) (*store.ScalingConfig, error) {
	if f.get == nil {
		return nil, store.ErrScalingConfigNotFound
	}
	return f.get(ctx, tenantID, projectID, env, app)
}

func (f *fakeScalingStore) Upsert(ctx context.Context, tx db.DBTX, c *store.ScalingConfig) (*store.ScalingConfig, bool, error) {
	return c, false, nil
}

func (f *fakeScalingStore) ListByEnv(ctx context.Context, tenantID, projectID, env string) ([]store.ScalingConfig, error) {
	return nil, nil
}

func (f *fakeScalingStore) Delete(ctx context.Context, tx db.DBTX, tenantID, projectID, env, app string) error {
	return nil
}

func (f *fakeScalingStore) DeleteByProject(ctx context.Context, tx db.DBTX, tenantID, projectID string) error {
	return nil
}

func (f *fakeScalingStore) CountByTenant(ctx context.Context, tenantID string) (int, error) {
	return 0, nil
}

// helper: build a minimal AppSpec with given name + replicas
func makeAppSpec(name string, replicas int) deploycontracts.AppSpec {
	return deploycontracts.AppSpec{
		Name:     name,
		Type:     "server",
		Port:     3000,
		Replicas: replicas,
	}
}

func i32ptr(v int32) *int32 { return &v }

func TestResolve_DBRowOverridesYAML_FixedReplicas(t *testing.T) {
	store := &fakeScalingStore{
		get: func(_ context.Context, _, _, _, app string) (*store.ScalingConfig, error) {
			if app != "api" {
				return nil, store.ErrScalingConfigNotFound
			}
			return &store.ScalingConfig{
				MinReplicas: 3,
				MaxReplicas: 3,
				ScaleToZero: false,
			}, nil
		},
	}
	r := mustResolver(t, store)

	in := []deploycontracts.AppSpec{makeAppSpec("api", 1)}
	out, err := r.Resolve(context.Background(), "tn-x", "proj-x", "production", in)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 app, got %d", len(out))
	}
	if out[0].Replicas != 3 {
		t.Errorf("expected replicas=3 (DB override), got %d", out[0].Replicas)
	}
	if out[0].Autoscaling != nil {
		t.Errorf("expected Autoscaling=nil for fixed replicas, got %+v", out[0].Autoscaling)
	}
}

func TestResolve_DBRowOverridesYAML_HPA(t *testing.T) {
	store := &fakeScalingStore{
		get: func(_ context.Context, _, _, _, _ string) (*store.ScalingConfig, error) {
			return &store.ScalingConfig{
				MinReplicas:  2,
				MaxReplicas:  5,
				TargetCPUPct: i32ptr(80),
				ScaleToZero:  false,
			}, nil
		},
	}
	r := mustResolver(t, store)

	in := []deploycontracts.AppSpec{makeAppSpec("api", 1)}
	out, err := r.Resolve(context.Background(), "tn-x", "proj-x", "production", in)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out[0].Autoscaling == nil {
		t.Fatalf("expected Autoscaling != nil for HPA, got nil")
	}
	if out[0].Replicas != 0 {
		t.Errorf("expected Replicas=0 next to a range (the yaml had 1), got %d", out[0].Replicas)
	}
	if out[0].Autoscaling.MinReplicas != 2 {
		t.Errorf("expected Min=2, got %d", out[0].Autoscaling.MinReplicas)
	}
	if out[0].Autoscaling.MaxReplicas != 5 {
		t.Errorf("expected Max=5, got %d", out[0].Autoscaling.MaxReplicas)
	}
	if out[0].Autoscaling.TargetCPU != 80 {
		t.Errorf("expected TargetCPU=80, got %d", out[0].Autoscaling.TargetCPU)
	}
}

func TestResolve_DBRowOverridesYAML_KEDA(t *testing.T) {
	store := &fakeScalingStore{
		get: func(_ context.Context, _, _, _, _ string) (*store.ScalingConfig, error) {
			return &store.ScalingConfig{
				MinReplicas: 0,
				MaxReplicas: 5,
				ScaleToZero: true,
			}, nil
		},
	}
	r := mustResolver(t, store)

	in := []deploycontracts.AppSpec{makeAppSpec("api", 1)}
	out, err := r.Resolve(context.Background(), "tn-x", "proj-x", "production", in)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out[0].Replicas != 0 {
		t.Errorf("expected Replicas=0 for scale-to-zero, got %d", out[0].Replicas)
	}
	if out[0].Autoscaling == nil {
		t.Fatalf("expected Autoscaling != nil for KEDA path, got nil")
	}
	if out[0].Autoscaling.MinReplicas != 0 {
		t.Errorf("expected Autoscaling.Min=0, got %d", out[0].Autoscaling.MinReplicas)
	}
	if out[0].Autoscaling.MaxReplicas != 5 {
		t.Errorf("expected Autoscaling.Max=5, got %d", out[0].Autoscaling.MaxReplicas)
	}
}

func TestResolve_NoDBRow_PreservesYAML(t *testing.T) {
	store := &fakeScalingStore{
		get: func(_ context.Context, _, _, _, _ string) (*store.ScalingConfig, error) {
			return nil, store.ErrScalingConfigNotFound
		},
	}
	r := mustResolver(t, store)

	in := []deploycontracts.AppSpec{makeAppSpec("api", 2)}
	out, err := r.Resolve(context.Background(), "tn-x", "proj-x", "production", in)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out[0].Replicas != 2 {
		t.Errorf("expected Replicas=2 (yaml preserved), got %d", out[0].Replicas)
	}
}

func TestResolve_MultipleApps_OnlyOverrideMatching(t *testing.T) {
	store := &fakeScalingStore{
		get: func(_ context.Context, _, _, _, app string) (*store.ScalingConfig, error) {
			if app == "api" {
				return &store.ScalingConfig{MinReplicas: 4, MaxReplicas: 4}, nil
			}
			return nil, store.ErrScalingConfigNotFound
		},
	}
	r := mustResolver(t, store)

	in := []deploycontracts.AppSpec{makeAppSpec("api", 1), makeAppSpec("worker", 2)}
	out, err := r.Resolve(context.Background(), "tn-x", "proj-x", "production", in)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(out))
	}
	if out[0].Replicas != 4 {
		t.Errorf("expected api.Replicas=4 (DB override), got %d", out[0].Replicas)
	}
	if out[1].Replicas != 2 {
		t.Errorf("expected worker.Replicas=2 (yaml preserved), got %d", out[1].Replicas)
	}
}

func TestResolve_DBError_PropagatesErr(t *testing.T) {
	wantErr := errors.New("connection refused")
	store := &fakeScalingStore{
		get: func(_ context.Context, _, _, _, _ string) (*store.ScalingConfig, error) {
			return nil, wantErr
		},
	}
	r := mustResolver(t, store)

	in := []deploycontracts.AppSpec{makeAppSpec("api", 1)}
	_, err := r.Resolve(context.Background(), "tn-x", "proj-x", "production", in)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected error to contain 'connection refused', got: %v", err)
	}
}

// A resolver without a store would silently deploy the yaml's scaling and
// ignore every persisted scaling config; it must not be constructible.
func TestNewDBScalingResolver_RefusesANilStore(t *testing.T) {
	if _, err := NewDBScalingResolver(nil); err == nil {
		t.Fatal("expected an error for a nil store")
	}
}

func TestResolve_EmptyApps_ReturnsEmpty(t *testing.T) {
	r := mustResolver(t, &fakeScalingStore{})
	out, err := r.Resolve(context.Background(), "tn-x", "proj-x", "production", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty slice, got %d apps", len(out))
	}
}

func mustResolver(t *testing.T, store ScalingConfigReader) ScalingResolver {
	t.Helper()
	r, err := NewDBScalingResolver(store)
	if err != nil {
		t.Fatalf("NewDBScalingResolver: %v", err)
	}
	return r
}

func TestResolve_DBRowCarriesTheMemoryTarget(t *testing.T) {
	store := &fakeScalingStore{
		get: func(_ context.Context, _, _, _, _ string) (*store.ScalingConfig, error) {
			return &store.ScalingConfig{
				MinReplicas:     1,
				MaxReplicas:     3,
				TargetCPUPct:    i32ptr(80),
				TargetMemoryPct: i32ptr(65),
			}, nil
		},
	}
	r := mustResolver(t, store)

	out, err := r.Resolve(context.Background(), "tn-x", "proj-x", "production",
		[]deploycontracts.AppSpec{makeAppSpec("api", 1)})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out[0].Autoscaling == nil || out[0].Autoscaling.TargetMemory != 65 {
		t.Errorf("autoscaling = %+v, want TargetMemory 65", out[0].Autoscaling)
	}
}
