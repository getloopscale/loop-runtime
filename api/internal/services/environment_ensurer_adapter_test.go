// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"errors"
	"testing"
)

// TestEnvManagerAdapter_HasEnvironment_ExistsWhenGetReturnsNil asserts
// HasEnvironment maps Get(nil) → (true, nil).
func TestEnvManagerAdapter_HasEnvironment_ExistsWhenGetReturnsNil(t *testing.T) {
	lister, _ := NewEnvManagerAdapter(
		func(_ context.Context, _, _ string) error { return nil },
		func(_ context.Context, _, _ string) error { return nil },
	)
	exists, err := lister.HasEnvironment(context.Background(), "tn-1", "prod")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !exists {
		t.Errorf("expected exists=true on Get nil-error, got false")
	}
}

// TestEnvManagerAdapter_HasEnvironment_AbsentOnNotFound asserts that Get
// returning a "not found" error is mapped to (false, nil) — NOT an error.
func TestEnvManagerAdapter_HasEnvironment_AbsentOnNotFound(t *testing.T) {
	lister, _ := NewEnvManagerAdapter(
		func(_ context.Context, _, _ string) error { return nil },
		func(_ context.Context, _, _ string) error { return errors.New("environment not found") },
	)
	exists, err := lister.HasEnvironment(context.Background(), "tn-1", "preview")
	if err != nil {
		t.Fatalf("expected nil err for not-found, got %v", err)
	}
	if exists {
		t.Errorf("expected exists=false for not-found, got true")
	}
}

// TestEnvManagerAdapter_HasEnvironment_PropagatesOtherErrors asserts that
// transient/auth errors (not "not found") propagate as Lister errors so the
// ensurer can act defensively (warn + proceed to create, idempotent).
func TestEnvManagerAdapter_HasEnvironment_PropagatesOtherErrors(t *testing.T) {
	lister, _ := NewEnvManagerAdapter(
		func(_ context.Context, _, _ string) error { return nil },
		func(_ context.Context, _, _ string) error { return errors.New("forbidden") },
	)
	exists, err := lister.HasEnvironment(context.Background(), "tn-1", "preview")
	if err == nil {
		t.Errorf("expected error for non-not-found, got nil")
	}
	if exists {
		t.Errorf("expected exists=false on error, got true")
	}
}

// TestEnvManagerAdapter_AddEnvironment_Delegates asserts AddEnvironment is
// straight delegation.
func TestEnvManagerAdapter_AddEnvironment_Delegates(t *testing.T) {
	called := false
	_, adder := NewEnvManagerAdapter(
		func(_ context.Context, tenantID, envName string) error {
			called = true
			if tenantID != "tn-1" || envName != "preview" {
				t.Errorf("unexpected args: tenant=%q env=%q", tenantID, envName)
			}
			return nil
		},
		func(_ context.Context, _, _ string) error { return nil },
	)
	if err := adder.AddEnvironment(context.Background(), "tn-1", "preview"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !called {
		t.Errorf("Add was not called")
	}
}
