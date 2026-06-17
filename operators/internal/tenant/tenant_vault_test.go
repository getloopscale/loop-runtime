// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

// tenant_vault_test.go — T3.3 of openbao-adoption v1.3.
// EC-15 amendment requires explicit coverage for VaultQuotaExceeded path.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/conditions"
	"github.com/getloopscale/loopzero/operators/internal/vault"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mockVault implements vault.TenantTransitClient for tests.
type mockVault struct {
	ensureErr    error
	deleteErr    error
	ensureCalls  int
	lastTenantID string
}

func (m *mockVault) EnsureTenantKey(_ context.Context, tenantID string) error {
	m.ensureCalls++
	m.lastTenantID = tenantID
	return m.ensureErr
}

func (m *mockVault) DeleteTenantKey(_ context.Context, tenantID string) error {
	return m.deleteErr
}

func newTenantFixture(name string) *loopzerov1.Tenant {
	return &loopzerov1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: name, Generation: 1},
	}
}

func findCondition(conds []metav1.Condition, t string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == t {
			return &conds[i]
		}
	}
	return nil
}

func TestReconcileTransitKey_NoOpWhenVaultClientNil(t *testing.T) {
	r := &TenantReconciler{VaultClient: nil}
	tenant := newTenantFixture("tenant-a")

	err := r.reconcileTransitKey(context.Background(), tenant)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	// Must NOT set TransitKeyAvailable condition (preserves Unknown state
	// so an opt-in later doesn't conflict with a permanent False).
	if cond := findCondition(tenant.Status.Conditions, conditions.TypeTransitKeyAvailable); cond != nil {
		t.Errorf("expected no condition set, got %v", cond)
	}
}

func TestReconcileTransitKey_SuccessSetsTrueCondition(t *testing.T) {
	mv := &mockVault{}
	r := &TenantReconciler{VaultClient: mv}
	tenant := newTenantFixture("tenant-success")

	err := r.reconcileTransitKey(context.Background(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	if mv.ensureCalls != 1 {
		t.Errorf("expected 1 EnsureTenantKey call, got %d", mv.ensureCalls)
	}
	if mv.lastTenantID != "tenant-success" {
		t.Errorf("expected tenant-success, got %s", mv.lastTenantID)
	}
	cond := findCondition(tenant.Status.Conditions, conditions.TypeTransitKeyAvailable)
	if cond == nil {
		t.Fatal("expected TransitKeyAvailable condition set, got none")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("expected True, got %v", cond.Status)
	}
	if cond.Reason != conditions.ReasonTransitKeyProvisioned {
		t.Errorf("expected ReasonTransitKeyProvisioned, got %v", cond.Reason)
	}
}

// EC-15 MUST FIX SHOULD TEST: quota error sets specific reason for alarm wiring.
func TestReconcileTransitKey_QuotaErrorSetsCondition(t *testing.T) {
	mv := &mockVault{
		ensureErr: fmt.Errorf("wrapped: %w", vault.ErrVaultQuotaExceeded),
	}
	r := &TenantReconciler{VaultClient: mv}
	tenant := newTenantFixture("tenant-quota")

	err := r.reconcileTransitKey(context.Background(), tenant)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, vault.ErrVaultQuotaExceeded) {
		t.Errorf("expected ErrVaultQuotaExceeded, got %v", err)
	}
	cond := findCondition(tenant.Status.Conditions, conditions.TypeTransitKeyAvailable)
	if cond == nil {
		t.Fatal("expected condition set, got none")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected False, got %v", cond.Status)
	}
	if cond.Reason != conditions.ReasonVaultQuotaExceeded {
		t.Errorf("expected ReasonVaultQuotaExceeded (EC-15 alarm reason), got %v", cond.Reason)
	}
}

func TestReconcileTransitKey_UnreachableErrorSetsDistinctReason(t *testing.T) {
	mv := &mockVault{ensureErr: errors.New("connection refused")}
	r := &TenantReconciler{VaultClient: mv}
	tenant := newTenantFixture("tenant-unreachable")

	err := r.reconcileTransitKey(context.Background(), tenant)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	cond := findCondition(tenant.Status.Conditions, conditions.TypeTransitKeyAvailable)
	if cond == nil {
		t.Fatal("expected condition set, got none")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected False, got %v", cond.Status)
	}
	if cond.Reason != conditions.ReasonVaultUnreachable {
		t.Errorf("expected ReasonVaultUnreachable, got %v", cond.Reason)
	}
}

func TestRequiredProvisioningConditions_IncludesTransitKeyWhenVaultClientSet(t *testing.T) {
	r := &TenantReconciler{VaultClient: &mockVault{}}
	required := r.requiredProvisioningConditions()

	found := false
	for _, c := range required {
		if c == conditions.TypeTransitKeyAvailable {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected TypeTransitKeyAvailable in required conditions, got %v", required)
	}
}

func TestRequiredProvisioningConditions_OmitsTransitKeyWhenVaultClientNil(t *testing.T) {
	r := &TenantReconciler{VaultClient: nil}
	required := r.requiredProvisioningConditions()

	for _, c := range required {
		if c == conditions.TypeTransitKeyAvailable {
			t.Errorf("expected TypeTransitKeyAvailable NOT in required conditions when vault disabled, got %v", required)
		}
	}
}
