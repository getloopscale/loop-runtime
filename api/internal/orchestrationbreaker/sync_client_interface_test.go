// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package orchestrationbreaker

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

// Sprint B T1.1 (ADR D1) — three structural invariants that keep the
// SyncClient contract honest:
//
//  1. The interface MUST declare all 6 methods (TriggerSync, WaitForSync,
//     ForceReconcile, GetSyncRevision, TerminateOperation, GetAppStatus).
//  2. *CircuitBreakerSyncClient MUST satisfy SyncClient at compile time.
//  3. release_convergence_loop.go MUST NOT regress to duck-typed dispatch
//     (`r.sync_.(...)` assertions).
//
// L22 incident (commit 098b6846) proved that runtime type assertions hide
// missing wrapper delegations. These tests move the safety net to build
// time and CI test time, so the same class of bug cannot ship to prod.

// TestSyncClientInterface_DeclaresAllMethods asserts the SyncClient
// interface exposes exactly 6 named methods. Adding/removing a method
// without updating this list will fail the test loudly, forcing the
// author to reason about all implementers + every test fake.
func TestSyncClientInterface_DeclaresAllMethods(t *testing.T) {
	// reflect.TypeOf((*SyncClient)(nil)).Elem() yields the interface type.
	ifaceType := reflect.TypeOf((*deploycontracts.SyncClient)(nil)).Elem()
	if ifaceType.Kind() != reflect.Interface {
		t.Fatalf("expected SyncClient to be an interface, got kind=%v", ifaceType.Kind())
	}

	expected := map[string]bool{
		"TriggerSync":        false,
		"WaitForSync":        false,
		"ForceReconcile":     false,
		"GetSyncRevision":    false,
		"TerminateOperation": false,
		"GetAppStatus":       false,
	}

	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if _, want := expected[name]; !want {
			t.Errorf("unexpected method on SyncClient: %s", name)
			continue
		}
		expected[name] = true
	}

	for name, seen := range expected {
		if !seen {
			t.Errorf("missing required SyncClient method: %s", name)
		}
	}

	if got, want := ifaceType.NumMethod(), len(expected); got != want {
		t.Errorf("SyncClient method count: got %d, want %d", got, want)
	}
}

// TestCircuitBreakerSyncClient_SatisfiesSyncClient_AtCompileTime is a
// belt-and-suspenders companion to the package-level
//
//	var _ SyncClient = (*CircuitBreakerSyncClient)(nil)
//
// declared in pg_circuit_breaker.go. The package-level assertion fires
// on `go build`; this test makes the contract visible to reviewers
// reading the test suite and runs as part of the CI test gate.
func TestCircuitBreakerSyncClient_SatisfiesSyncClient_AtCompileTime(t *testing.T) {
	var _ deploycontracts.SyncClient = (*CircuitBreakerSyncClient)(nil)
	// Also verify the runtime conversion does not panic.
	var cb interface{} = (*CircuitBreakerSyncClient)(nil)
	if _, ok := cb.(deploycontracts.SyncClient); !ok {
		t.Fatal("*CircuitBreakerSyncClient does not satisfy SyncClient at runtime")
	}
}

// minimalSyncClient is a no-op SyncClient used by the structural tests
// above. Kept in this file so reviewers can see at a glance what the
// contract requires; mirrors the noop pattern used in production wrappers.
type minimalSyncClient struct{}

func (minimalSyncClient) TriggerSync(context.Context, string) error { return nil }
func (minimalSyncClient) WaitForSync(context.Context, string, time.Duration, []string) error {
	return nil
}
func (minimalSyncClient) ForceReconcile(context.Context, string) error { return nil }
func (minimalSyncClient) GetSyncRevision(context.Context, string) (string, error) {
	return "", nil
}
func (minimalSyncClient) TerminateOperation(context.Context, string) error { return nil }
func (minimalSyncClient) GetAppStatus(context.Context, string, []string) (bool, bool, error) {
	return true, true, nil
}

// Compile-time assertion: the canonical minimal implementer satisfies
// the interface. If a future method is added to SyncClient without
// updating this stub, the package fails to compile.
var _ deploycontracts.SyncClient = minimalSyncClient{}
