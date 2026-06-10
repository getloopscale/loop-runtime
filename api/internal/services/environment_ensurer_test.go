// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type stubLister struct {
	exists    atomic.Bool
	err       error
	callCount atomic.Int32
}

func (s *stubLister) HasEnvironment(_ context.Context, _, _ string) (bool, error) {
	s.callCount.Add(1)
	return s.exists.Load(), s.err
}

type stubAdder struct {
	err         error
	callCount   atomic.Int32
	onAddBecome bool // when true, marks lister as existing after AddEnvironment
	lister      *stubLister
}

func (s *stubAdder) AddEnvironment(_ context.Context, _, _ string) error {
	s.callCount.Add(1)
	if s.err != nil {
		return s.err
	}
	if s.onAddBecome && s.lister != nil {
		s.lister.exists.Store(true)
	}
	return nil
}

// TestEnsureEnvironment_NoopWhenExists asserts that an existing env returns
// nil immediately without calling AddEnvironment.
// Plan T2.2 RED #1.
func TestEnsureEnvironment_NoopWhenExists(t *testing.T) {
	lister := &stubLister{}
	lister.exists.Store(true)
	adder := &stubAdder{}
	e := &EnvironmentEnsurer{Lister: lister, Adder: adder}

	if err := e.EnsureEnvironment(context.Background(), "tn-1", "preview"); err != nil {
		t.Errorf("expected nil for existing env, got %v", err)
	}
	if adder.callCount.Load() != 0 {
		t.Errorf("expected zero AddEnvironment calls for existing env, got %d", adder.callCount.Load())
	}
}

// TestEnsureEnvironment_CreatesAndWaits asserts that a missing env is
// created via AddEnvironment, then polling confirms existence.
// Plan T2.2 RED #2.
func TestEnsureEnvironment_CreatesAndWaits(t *testing.T) {
	lister := &stubLister{}
	lister.exists.Store(false)
	adder := &stubAdder{onAddBecome: true, lister: lister}
	e := &EnvironmentEnsurer{
		Lister:       lister,
		Adder:        adder,
		WaitTimeout:  3 * time.Second,
		PollInterval: 100 * time.Millisecond,
	}

	if err := e.EnsureEnvironment(context.Background(), "tn-1", "preview"); err != nil {
		t.Errorf("expected nil after create+wait, got %v", err)
	}
	if adder.callCount.Load() != 1 {
		t.Errorf("expected exactly 1 AddEnvironment call, got %d", adder.callCount.Load())
	}
}

// TestEnsureEnvironment_TimeoutWhenOperatorSlow asserts that the ensurer
// times out gracefully if the operator never reconciles.
// Plan T2.2 RED #3.
func TestEnsureEnvironment_TimeoutWhenOperatorSlow(t *testing.T) {
	lister := &stubLister{} // exists stays false
	adder := &stubAdder{}   // does NOT flip lister
	e := &EnvironmentEnsurer{
		Lister:       lister,
		Adder:        adder,
		WaitTimeout:  300 * time.Millisecond,
		PollInterval: 50 * time.Millisecond,
	}

	err := e.EnsureEnvironment(context.Background(), "tn-1", "preview")
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "not yet ready") {
		t.Errorf("expected timeout message, got %q", err.Error())
	}
}

// TestEnsureEnvironment_AlreadyExistsErrorIsSuccess asserts that an
// "already exists" error from AddEnvironment (race) is treated as success.
// Plan T2.2 RED #4.
func TestEnsureEnvironment_AlreadyExistsErrorIsSuccess(t *testing.T) {
	lister := &stubLister{} // exists=false, so we'll call Add
	adder := &stubAdder{err: errors.New("environment already exists for tenant")}
	e := &EnvironmentEnsurer{
		Lister:       lister,
		Adder:        adder,
		WaitTimeout:  100 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
	}

	err := e.EnsureEnvironment(context.Background(), "tn-1", "preview")
	if err != nil {
		t.Errorf("expected nil for already-exists error, got %v", err)
	}
}

// TestEnsureEnvironment_NilStoreNoOp asserts safe behavior when ensurer is
// misconfigured (Lister or Adder nil).
// Plan T2.2 RED #5.
func TestEnsureEnvironment_NilStoreNoOp(t *testing.T) {
	e := &EnvironmentEnsurer{}
	if err := e.EnsureEnvironment(context.Background(), "tn-1", "preview"); err != nil {
		t.Errorf("expected nil for misconfigured ensurer (fail open), got %v", err)
	}
}

// TestEnsureEnvironment_NilReceiver asserts a nil pointer also no-ops safely.
func TestEnsureEnvironment_NilReceiver(t *testing.T) {
	var e *EnvironmentEnsurer
	if err := e.EnsureEnvironment(context.Background(), "tn-1", "preview"); err != nil {
		t.Errorf("expected nil for nil receiver, got %v", err)
	}
}

// TestEnsureEnvironment_AddFailReturnsLoopZeroError asserts that non-already-exists
// errors from AddEnvironment surface as LoopZeroError(ENV_PROVISION_FAILED).
// Plan T2.2 RED #6.
func TestEnsureEnvironment_AddFailReturnsLoopZeroError(t *testing.T) {
	lister := &stubLister{}
	adder := &stubAdder{err: errors.New("permission denied")}
	e := &EnvironmentEnsurer{Lister: lister, Adder: adder}

	err := e.EnsureEnvironment(context.Background(), "tn-1", "preview")
	if err == nil {
		t.Fatalf("expected error from AddEnvironment failure, got nil")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("expected underlying error in message, got %q", err.Error())
	}
}

// stubReadinessChecker implements EnvironmentReadinessChecker for tests.
// Toggles via .ready atomic.Bool; callCount tracks invocations.
type stubReadinessChecker struct {
	ready     atomic.Bool
	err       error
	callCount atomic.Int32
}

func (s *stubReadinessChecker) IsReady(_ context.Context, _, _ string) (bool, error) {
	s.callCount.Add(1)
	return s.ready.Load(), s.err
}

// TestEnsureEnvironment_BlockedUntilReadinessActive (Sprint A trio-fix
// 2026-06-01) asserts that with a non-nil ReadinessChecker, EnsureEnvironment
// continues polling AFTER HasEnvironment returns true UNTIL IsReady also
// returns true. This is the canonical regression for the
// SYNC_FAILED-because-AppProject-not-reconciled bug detected during the
// trio dogfood: shallow check (env in tenant.spec.environments) passes
// immediately but the operator hasn't updated AppProject destinations yet,
// causing downstream ArgoCD App stuck InvalidSpecError.
func TestEnsureEnvironment_BlockedUntilReadinessActive(t *testing.T) {
	lister := &stubLister{}
	lister.exists.Store(false)
	adder := &stubAdder{onAddBecome: true, lister: lister}
	checker := &stubReadinessChecker{} // ready stays false
	e := &EnvironmentEnsurer{
		Lister:           lister,
		Adder:            adder,
		ReadinessChecker: checker,
		WaitTimeout:      400 * time.Millisecond,
		PollInterval:     50 * time.Millisecond,
	}

	err := e.EnsureEnvironment(context.Background(), "tn-1", "preview")
	if err == nil {
		t.Fatalf("expected timeout because readiness never flips, got nil")
	}
	if !strings.Contains(err.Error(), "not yet ready") {
		t.Errorf("expected ENV_PROVISION_TIMEOUT-style error, got %q", err.Error())
	}
	// HasEnvironment should have been called multiple times (poll loop).
	if lister.callCount.Load() < 2 {
		t.Errorf("expected HasEnvironment polled >=2x, got %d", lister.callCount.Load())
	}
	// IsReady should have been called multiple times too (gating the return).
	if checker.callCount.Load() < 2 {
		t.Errorf("expected IsReady polled >=2x (poll loop), got %d", checker.callCount.Load())
	}
}

// TestEnsureEnvironment_ReturnsWhenBothReady (Sprint A trio-fix) asserts the
// HAPPY path: HasEnvironment + IsReady both return true → ensurer returns nil
// quickly without timeout.
func TestEnsureEnvironment_ReturnsWhenBothReady(t *testing.T) {
	lister := &stubLister{}
	lister.exists.Store(false)
	adder := &stubAdder{onAddBecome: true, lister: lister}
	checker := &stubReadinessChecker{}
	checker.ready.Store(true) // ready immediately
	e := &EnvironmentEnsurer{
		Lister:           lister,
		Adder:            adder,
		ReadinessChecker: checker,
		WaitTimeout:      2 * time.Second,
		PollInterval:     20 * time.Millisecond,
	}

	start := time.Now()
	err := e.EnsureEnvironment(context.Background(), "tn-1", "preview")
	if err != nil {
		t.Fatalf("expected nil when both checks pass, got %v", err)
	}
	// Should complete fast — well under WaitTimeout.
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("expected fast return (<500ms) when ready, took %v", elapsed)
	}
	if adder.callCount.Load() != 1 {
		t.Errorf("expected exactly 1 AddEnvironment call, got %d", adder.callCount.Load())
	}
}

// TestEnsureEnvironment_LegacyBehaviorWhenCheckerNil (Sprint A trio-fix)
// asserts that nil ReadinessChecker preserves backward-compatible behavior:
// HasEnvironment=true is sufficient to return success. This guards test
// fixtures and any boot path that intentionally omits the deep check.
func TestEnsureEnvironment_LegacyBehaviorWhenCheckerNil(t *testing.T) {
	lister := &stubLister{}
	lister.exists.Store(true) // already exists
	adder := &stubAdder{}
	e := &EnvironmentEnsurer{
		Lister: lister,
		Adder:  adder,
		// ReadinessChecker intentionally nil
	}

	if err := e.EnsureEnvironment(context.Background(), "tn-1", "preview"); err != nil {
		t.Errorf("expected nil for existing env with no readiness check, got %v", err)
	}
}

// TestEnsureEnvironment_ReadinessErrorTreatedAsTransient (Sprint A trio-fix)
// asserts that IsReady errors do NOT immediately fail EnsureEnvironment —
// the poll loop continues, treating the error as transient (matches existing
// HasEnvironment error semantics). This guards against flaky K8s API
// responses (e.g., rolling control-plane upgrade) causing spurious
// ENV_PROVISION_TIMEOUT.
func TestEnsureEnvironment_ReadinessErrorTreatedAsTransient(t *testing.T) {
	lister := &stubLister{}
	lister.exists.Store(false)
	adder := &stubAdder{onAddBecome: true, lister: lister}
	checker := &stubReadinessChecker{err: errors.New("API server timeout")}
	e := &EnvironmentEnsurer{
		Lister:           lister,
		Adder:            adder,
		ReadinessChecker: checker,
		WaitTimeout:      250 * time.Millisecond,
		PollInterval:     50 * time.Millisecond,
	}

	err := e.EnsureEnvironment(context.Background(), "tn-1", "preview")
	// Expected: timeout (readiness check NEVER returned ready=true), but
	// the loop should have retried multiple times (treated err as transient).
	if err == nil {
		t.Fatalf("expected timeout (readiness perma-erroring), got nil")
	}
	if !strings.Contains(err.Error(), "not yet ready") {
		t.Errorf("expected timeout error, got %q", err.Error())
	}
	if checker.callCount.Load() < 2 {
		t.Errorf("expected IsReady polled multiple times (transient retry), got %d",
			checker.callCount.Load())
	}
}
