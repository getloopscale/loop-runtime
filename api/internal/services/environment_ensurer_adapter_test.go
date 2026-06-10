// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"errors"
	"testing"
)

// TestEnvManagerAdapter_HasEnvironment_ExistsWhenDetailNonNil asserts the
// TenantCRManager contract: (non-nil detail, nil err) → exists.
func TestEnvManagerAdapter_HasEnvironment_ExistsWhenDetailNonNil(t *testing.T) {
	lister, _ := NewEnvManagerAdapter(
		func(_ context.Context, _, _ string) error { return nil },
		func(_ context.Context, _, _ string) (any, error) {
			return struct{ Name string }{"prod"}, nil
		},
	)
	exists, err := lister.HasEnvironment(context.Background(), "tn-1", "prod")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !exists {
		t.Errorf("expected exists=true on non-nil detail, got false")
	}
}

// TestEnvManagerAdapter_HasEnvironment_AbsentOnNilNilSemantic asserts the
// TenantCRManager contract: (nil detail, nil err) → does NOT exist.
func TestEnvManagerAdapter_HasEnvironment_AbsentOnNilNilSemantic(t *testing.T) {
	lister, _ := NewEnvManagerAdapter(
		func(_ context.Context, _, _ string) error { return nil },
		func(_ context.Context, _, _ string) (any, error) {
			return nil, nil // env not present per TenantCRManager
		},
	)
	exists, err := lister.HasEnvironment(context.Background(), "tn-1", "preview")
	if err != nil {
		t.Fatalf("expected nil err for absent, got %v", err)
	}
	if exists {
		t.Errorf("expected exists=false for (nil, nil) detail, got true")
	}
}

// TestEnvManagerAdapter_HasEnvironment_AbsentOnNotFoundError asserts the
// defensive legacy semantic: error containing "not found" → absent (not error).
func TestEnvManagerAdapter_HasEnvironment_AbsentOnNotFoundError(t *testing.T) {
	lister, _ := NewEnvManagerAdapter(
		func(_ context.Context, _, _ string) error { return nil },
		func(_ context.Context, _, _ string) (any, error) {
			return nil, errors.New("environment not found")
		},
	)
	exists, err := lister.HasEnvironment(context.Background(), "tn-1", "preview")
	if err != nil {
		t.Fatalf("expected nil err for not-found message, got %v", err)
	}
	if exists {
		t.Errorf("expected exists=false for not-found error, got true")
	}
}

// TestEnvManagerAdapter_HasEnvironment_PropagatesOtherErrors asserts that
// transient/auth errors (not "not found") propagate as Lister errors.
func TestEnvManagerAdapter_HasEnvironment_PropagatesOtherErrors(t *testing.T) {
	lister, _ := NewEnvManagerAdapter(
		func(_ context.Context, _, _ string) error { return nil },
		func(_ context.Context, _, _ string) (any, error) {
			return nil, errors.New("forbidden")
		},
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
		func(_ context.Context, _, _ string) (any, error) { return nil, nil },
	)
	if err := adder.AddEnvironment(context.Background(), "tn-1", "preview"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !called {
		t.Errorf("Add was not called")
	}
}
