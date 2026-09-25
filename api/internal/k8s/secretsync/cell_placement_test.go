// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secretsync

import (
	"context"
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

// fixedCell is a TenantCellLookup that places every tenant on one cell.
type fixedCell string

func (c fixedCell) GetTenantCellID(context.Context, string) (string, error) { return string(c), nil }

type failingCellLookup struct{}

func (failingCellLookup) GetTenantCellID(context.Context, string) (string, error) {
	return "", errors.New("cell store unreachable")
}

// The manager holds a client for ONE runtime cluster. A tenant placed on
// another cell has its namespace elsewhere, so writing its ExternalSecret here
// would create an object nothing reads: the manager must refuse, and must do
// so before touching either cluster.
func newPlacedManager(lookup TenantCellLookup) (*SecretK8sManager, *fake.Clientset, *dynamicfake.FakeDynamicClient) {
	mgmt := fake.NewClientset()
	dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	return &SecretK8sManager{
		mgmtClient: mgmt, runtimeDynamic: dyn, mgmtNamespace: "loopzero-system",
		tenantCells: lookup, runtimeCellID: "cell-0",
	}, mgmt, dyn
}

func TestCreateSecretRefusesATenantPlacedOnAnotherCell(t *testing.T) {
	// Arrange
	mgr, mgmt, dyn := newPlacedManager(fixedCell("cell-1"))

	// Act
	err := mgr.CreateSecret(context.Background(), "tn-1", "shop", "production", "api", "KEY", "v")

	// Assert
	if !errors.Is(err, ErrTenantCellNotServed) {
		t.Fatalf("err = %v, want ErrTenantCellNotServed", err)
	}
	if n := len(mgmt.Actions()) + len(dyn.Actions()); n != 0 {
		t.Errorf("refused write still reached a cluster (%d actions)", n)
	}
}

func TestDeleteSecretRefusesATenantPlacedOnAnotherCell(t *testing.T) {
	mgr, mgmt, dyn := newPlacedManager(fixedCell("cell-1"))

	err := mgr.DeleteSecret(context.Background(), "tn-1", "shop", "production", "api", "KEY")

	if !errors.Is(err, ErrTenantCellNotServed) {
		t.Fatalf("err = %v, want ErrTenantCellNotServed", err)
	}
	if n := len(mgmt.Actions()) + len(dyn.Actions()); n != 0 {
		t.Errorf("refused delete still reached a cluster (%d actions)", n)
	}
}

func TestCreateSecretFailsWhenTheTenantCellCannotBeRead(t *testing.T) {
	mgr, mgmt, _ := newPlacedManager(failingCellLookup{})

	err := mgr.CreateSecret(context.Background(), "tn-1", "shop", "production", "api", "KEY", "v")

	if err == nil {
		t.Fatal("CreateSecret wrote a secret without knowing which cell the tenant is on")
	}
	if len(mgmt.Actions()) != 0 {
		t.Error("secret written to the management cluster before placement was known")
	}
}

func TestCreateSecretWritesForATenantOnTheServedCell(t *testing.T) {
	mgr, _, dyn := newPlacedManager(fixedCell("cell-0"))

	err := mgr.CreateSecret(context.Background(), "tn-1", "shop", "production", "api", "KEY", "v")

	if err != nil {
		t.Fatalf("CreateSecret: %v", err)
	}
	if len(dyn.Actions()) == 0 {
		t.Error("no ExternalSecret written for a tenant on the served cell")
	}
}
