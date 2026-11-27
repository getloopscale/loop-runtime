// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package environments

import (
	"context"
	"errors"
	"testing"
)

// TestEnvManagerAdapter_HasEnvironment_ExistsWhenDetailNonNil asserts the
// TenantCRManager contract: (non-nil detail, nil err) → exists.
func TestEnvManagerAdapter_HasEnvironment_ExistsWhenDetailNonNil(t *testing.T) {
	lister, _ := NewEnvManagerAdapter(
		func(_ context.Context, _, _, _ string) error { return nil }, // M2 v1.2: project arg added
		func(_ context.Context, _, _, _ string) (any, error) {
			return struct{ Name string }{"prod"}, nil
		},
	)
	exists, err := lister.HasEnvironment(context.Background(), "tn-1", "", "prod")
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
		func(_ context.Context, _, _, _ string) error { return nil }, // M2 v1.2: project arg added
		func(_ context.Context, _, _, _ string) (any, error) {
			return nil, nil // env not present per TenantCRManager
		},
	)
	exists, err := lister.HasEnvironment(context.Background(), "tn-1", "", "preview")
	if err != nil {
		t.Fatalf("expected nil err for absent, got %v", err)
	}
	if exists {
		t.Errorf("expected exists=false for (nil, nil) detail, got true")
	}
}

// The lookup reports absence as (nil, nil). An error is never absence, even
// when its message says "not found" — a missing Tenant CR is a real failure,
// and reading it as "the environment is absent" hid it behind a create call.
func TestEnvManagerAdapter_HasEnvironment_PropagatesNotFoundMessageAsError(t *testing.T) {
	lister, _ := NewEnvManagerAdapter(
		func(_ context.Context, _, _, _ string) error { return nil },
		func(_ context.Context, _, _, _ string) (any, error) {
			return nil, errors.New(`k8s: get tenant CR "tn-1": tenants.loop-runtime.dev "tn-1" not found`)
		},
	)

	exists, err := lister.HasEnvironment(context.Background(), "tn-1", "", "preview")

	if err == nil {
		t.Fatal("expected the lookup error to propagate, got nil")
	}
	if exists {
		t.Errorf("expected exists=false on error, got true")
	}
}

// TestEnvManagerAdapter_HasEnvironment_PropagatesOtherErrors asserts that
// transient/auth errors (not "not found") propagate as Lister errors.
func TestEnvManagerAdapter_HasEnvironment_PropagatesOtherErrors(t *testing.T) {
	lister, _ := NewEnvManagerAdapter(
		func(_ context.Context, _, _, _ string) error { return nil }, // M2 v1.2: project arg added
		func(_ context.Context, _, _, _ string) (any, error) {
			return nil, errors.New("forbidden")
		},
	)
	exists, err := lister.HasEnvironment(context.Background(), "tn-1", "", "preview")
	if err == nil {
		t.Errorf("expected error for non-not-found, got nil")
	}
	if exists {
		t.Errorf("expected exists=false on error, got true")
	}
}

// TestEnvManagerAdapter_AddEnvironment_Delegates asserts AddEnvironment is
// straight delegation. M2 v1.2: project arg propagates verbatim.
func TestEnvManagerAdapter_AddEnvironment_Delegates(t *testing.T) {
	called := false
	_, adder := NewEnvManagerAdapter(
		func(_ context.Context, tenantID, project, envName string) error {
			called = true
			if tenantID != "tn-1" || project != "web" || envName != "preview" {
				t.Errorf("unexpected args: tenant=%q project=%q env=%q", tenantID, project, envName)
			}
			return nil
		},
		func(_ context.Context, _, _, _ string) (any, error) { return nil, nil },
	)
	if err := adder.AddEnvironment(context.Background(), "tn-1", "web", "preview"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !called {
		t.Errorf("Add was not called")
	}
}
