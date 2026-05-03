// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// fix-all-platform-issues-plan T2.2 — Application reaper unit tests.
// Pure-Go fake client; no envtest required.

package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
)

type runtimeScheme = runtime.Scheme

func newRuntimeScheme() *runtimeScheme { return runtime.NewScheme() }

// fakeManifestChecker implements ManifestChecker in-memory.
type fakeManifestChecker struct {
	exists map[string]bool
	err    error
}

func (f *fakeManifestChecker) ManifestExists(_ context.Context, tenantRef, project, environment string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	key := tenantRef + "|" + project + "|" + environment
	return f.exists[key], nil
}

func appAge(age time.Duration) *loopzerov1.LoopZeroApplication {
	return &loopzerov1.LoopZeroApplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "tn-x-myapp-production",
			Namespace:         "loopzero-system",
			CreationTimestamp: metav1.Time{Time: time.Now().Add(-age)},
		},
		Spec: loopzerov1.LoopZeroApplicationSpec{
			TenantRef:   "tn-x",
			Project:     "myapp",
			Environment: "production",
		},
		Status: loopzerov1.LoopZeroApplicationStatus{
			Phase: loopzerov1.ApplicationPhaseProgressing,
		},
	}
}

func TestShouldReap_TooYoung_NotEligible(t *testing.T) {
	app := appAge(30 * time.Minute)
	checker := &fakeManifestChecker{exists: map[string]bool{}}
	ok, reason := ShouldReap(context.Background(), app, checker, 1*time.Hour, time.Now())
	if ok {
		t.Fatalf("expected not eligible (too young), got reaped — reason=%q", reason)
	}
}

func TestShouldReap_OldAndManifestAbsent_Eligible(t *testing.T) {
	app := appAge(2 * time.Hour)
	checker := &fakeManifestChecker{exists: map[string]bool{}} // empty → manifest absent
	ok, reason := ShouldReap(context.Background(), app, checker, 1*time.Hour, time.Now())
	if !ok {
		t.Fatalf("expected eligible (orphan), got skipped — reason=%q", reason)
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestShouldReap_OldButManifestPresent_NotEligible(t *testing.T) {
	app := appAge(2 * time.Hour)
	checker := &fakeManifestChecker{exists: map[string]bool{"tn-x|myapp|production": true}}
	ok, reason := ShouldReap(context.Background(), app, checker, 1*time.Hour, time.Now())
	if ok {
		t.Fatalf("expected not eligible (manifest present), got reaped — reason=%q", reason)
	}
}

func TestShouldReap_AlreadyCleaned_NotEligible(t *testing.T) {
	app := appAge(2 * time.Hour)
	app.Status.Phase = loopzerov1.ApplicationPhaseCleaned
	checker := &fakeManifestChecker{exists: map[string]bool{}}
	ok, reason := ShouldReap(context.Background(), app, checker, 1*time.Hour, time.Now())
	if ok {
		t.Fatalf("expected idempotent skip when already Cleaned — reason=%q", reason)
	}
}

func TestShouldReap_NonProgressing_NotEligible(t *testing.T) {
	for _, phase := range []loopzerov1.ApplicationPhase{
		loopzerov1.ApplicationPhaseServing,
		loopzerov1.ApplicationPhaseReachable,
		loopzerov1.ApplicationPhaseDegraded,
		loopzerov1.ApplicationPhaseDown,
	} {
		app := appAge(2 * time.Hour)
		app.Status.Phase = phase
		checker := &fakeManifestChecker{exists: map[string]bool{}}
		ok, _ := ShouldReap(context.Background(), app, checker, 1*time.Hour, time.Now())
		if ok {
			t.Errorf("phase %q should not be reaped", phase)
		}
	}
}

func TestShouldReap_NoChecker_NotEligible(t *testing.T) {
	app := appAge(2 * time.Hour)
	ok, reason := ShouldReap(context.Background(), app, nil, 1*time.Hour, time.Now())
	if ok {
		t.Fatalf("expected not eligible without checker (fail closed) — reason=%q", reason)
	}
}

func TestShouldReap_TransientCheckerError_NotEligible(t *testing.T) {
	app := appAge(2 * time.Hour)
	checker := &fakeManifestChecker{err: errors.New("git: connection refused")}
	ok, reason := ShouldReap(context.Background(), app, checker, 1*time.Hour, time.Now())
	if ok {
		t.Fatalf("expected not eligible on transient error — reason=%q", reason)
	}
}

func TestMarkAsCleaned_TransitionsPhaseAndRemovesFinalizers(t *testing.T) {
	scheme := newSchemeForTest(t)
	app := appAge(2 * time.Hour)
	app.Finalizers = []string{"loopzero.io/application-cleanup"}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(app).
		WithStatusSubresource(&loopzerov1.LoopZeroApplication{}).
		Build()

	if err := MarkAsCleaned(context.Background(), c, app, "test reason"); err != nil {
		t.Fatalf("MarkAsCleaned: %v", err)
	}
	if app.Status.Phase != loopzerov1.ApplicationPhaseCleaned {
		t.Errorf("expected phase=Cleaned, got %q", app.Status.Phase)
	}
	// Conditions should include the Cleaned condition.
	found := false
	for _, c := range app.Status.Conditions {
		if c.Type == "Cleaned" && c.Status == metav1.ConditionTrue {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected Cleaned condition with Status=True")
	}
}

func TestMarkAsCleaned_Idempotent(t *testing.T) {
	scheme := newSchemeForTest(t)
	app := appAge(2 * time.Hour)
	app.Status.Phase = loopzerov1.ApplicationPhaseCleaned
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(app).
		WithStatusSubresource(&loopzerov1.LoopZeroApplication{}).
		Build()

	if err := MarkAsCleaned(context.Background(), c, app, "no-op"); err != nil {
		t.Fatalf("MarkAsCleaned re-call: %v", err)
	}
	if app.Status.Phase != loopzerov1.ApplicationPhaseCleaned {
		t.Errorf("idempotent call must keep phase=Cleaned, got %q", app.Status.Phase)
	}
}

func TestReaperGracePeriod_DefaultAndOverride(t *testing.T) {
	t.Setenv("LOOPZERO_REAPER_GRACE_PERIOD_SECONDS", "")
	if g := ReaperGracePeriod(); g != DefaultReaperGracePeriod {
		t.Errorf("default mismatch: %v != %v", g, DefaultReaperGracePeriod)
	}
	t.Setenv("LOOPZERO_REAPER_GRACE_PERIOD_SECONDS", "120")
	if g := ReaperGracePeriod(); g != 120*time.Second {
		t.Errorf("override mismatch: %v != 120s", g)
	}
	t.Setenv("LOOPZERO_REAPER_GRACE_PERIOD_SECONDS", "garbage")
	if g := ReaperGracePeriod(); g != DefaultReaperGracePeriod {
		t.Errorf("malformed override should fall back to default: %v", g)
	}
}

// newSchemeForTest builds a minimal scheme for fake-client tests in this package.
func newSchemeForTest(t *testing.T) *runtimeScheme {
	t.Helper()
	scheme := newRuntimeScheme()
	if err := loopzerov1.AddToScheme(scheme); err != nil {
		t.Fatalf("add loopzerov1 to scheme: %v", err)
	}
	return scheme
}
