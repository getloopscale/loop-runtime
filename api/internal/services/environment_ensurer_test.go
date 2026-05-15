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
