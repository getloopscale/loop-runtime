// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Unit tests for CellAwareRuntimeClientFactory + StaticRuntimeClientFactory.
// cell-1-activation-plan iter 4 BUG-FIX. These tests use fake clients (no envtest).

package cellrouting

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
)

func newSchemeForTest(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 to scheme: %v", err)
	}
	if err := loopzerov1.AddToScheme(scheme); err != nil {
		t.Fatalf("add loopzerov1 to scheme: %v", err)
	}
	return scheme
}

// TestCellAwareFactory_DefaultClientOnlyForEmptyCellID locks the boundary
// between "no cell" and "a named cell".
//
// This test once accepted TWO values on the legacy path: the empty string and
// the name of the first cell, then `cell-default`. That worked because the name
// was a sentinel and a cell at once. With the 1:1 unification (ADR 2026-08-09)
// it became `cell-0`, a real cell with tenants, and accepting its name here
// would return the legacy client instead of its cluster — breaking the per-cell
// isolation this factory exists to guarantee. The empty string is the only
// sentinel.
func TestCellAwareFactory_DefaultClientOnlyForEmptyCellID(t *testing.T) {
	scheme := newSchemeForTest(t)
	mgmt := fake.NewClientBuilder().WithScheme(scheme).Build()
	def := fake.NewClientBuilder().WithScheme(scheme).Build()

	f := NewCellAwareRuntimeClientFactory(mgmt, def, "")

	c, err := f.For(context.Background(), "")
	if err != nil {
		t.Fatalf(`For(""): unexpected error: %v`, err)
	}
	if c != def {
		t.Error(`For(""): expected default client, got different`)
	}

	// A named cell NEVER takes the shortcut, even the first one.
	if _, err := f.For(context.Background(), "cell-0"); err == nil {
		t.Error(`For("cell-0"): returned the legacy client; a named cell must resolve ` +
			`its own ClusterCredential, or every cell-0 tenant would be reconciled on the ` +
			`wrong cluster`)
	}
}

func TestCellAwareFactory_ErrorWhenNoCCFound(t *testing.T) {
	scheme := newSchemeForTest(t)
	mgmt := fake.NewClientBuilder().WithScheme(scheme).Build() // empty
	def := fake.NewClientBuilder().WithScheme(scheme).Build()
	f := NewCellAwareRuntimeClientFactory(mgmt, def, "loopzero-system")

	_, err := f.For(context.Background(), "cell-1")
	if err == nil {
		t.Fatal("expected error for missing ClusterCredential, got nil")
	}
}

func TestCellAwareFactory_ErrorWhenSecretMissing(t *testing.T) {
	scheme := newSchemeForTest(t)
	cc := &loopzerov1.ClusterCredential{
		ObjectMeta: metav1.ObjectMeta{Name: "cell-1", Namespace: "loopzero-system"},
		Spec: loopzerov1.ClusterCredentialSpec{
			ClusterName: "loopzero-cell-1",
			APIServer:   "https://172.18.0.10:6443",
			CellID:      "cell-1",
			SecretRef: loopzerov1.SecretReference{
				Name:      "cell-1-kubeconfig",
				Namespace: "loopzero-system",
			},
		},
	}
	mgmt := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cc).Build()
	def := fake.NewClientBuilder().WithScheme(scheme).Build()
	f := NewCellAwareRuntimeClientFactory(mgmt, def, "loopzero-system")

	_, err := f.For(context.Background(), "cell-1")
	if err == nil {
		t.Fatal("expected error for missing kubeconfig Secret, got nil")
	}
}

func TestCellAwareFactory_ErrorWhenSecretLacksKubeconfig(t *testing.T) {
	scheme := newSchemeForTest(t)
	cc := &loopzerov1.ClusterCredential{
		ObjectMeta: metav1.ObjectMeta{Name: "cell-1", Namespace: "loopzero-system"},
		Spec: loopzerov1.ClusterCredentialSpec{
			ClusterName: "loopzero-cell-1",
			APIServer:   "https://172.18.0.10:6443",
			CellID:      "cell-1",
			SecretRef: loopzerov1.SecretReference{
				Name:      "cell-1-kubeconfig",
				Namespace: "loopzero-system",
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "cell-1-kubeconfig", Namespace: "loopzero-system"},
		Data:       map[string][]byte{"other": []byte("not-kubeconfig")},
	}
	mgmt := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cc, secret).Build()
	def := fake.NewClientBuilder().WithScheme(scheme).Build()
	f := NewCellAwareRuntimeClientFactory(mgmt, def, "loopzero-system")

	_, err := f.For(context.Background(), "cell-1")
	if err == nil {
		t.Fatal("expected error when Secret has no data.kubeconfig, got nil")
	}
}

func TestCellAwareFactory_LookupByCellIDNotCRName(t *testing.T) {
	// Per MEET-053 B9: lookup by spec.cellID, NOT metadata.name.
	// The CR name might be "workload" with cellID="cell-0", or "cell-1"
	// with cellID="cell-1"; either should be found.
	scheme := newSchemeForTest(t)
	cc := &loopzerov1.ClusterCredential{
		ObjectMeta: metav1.ObjectMeta{Name: "workload-not-matching-cellid", Namespace: "loopzero-system"},
		Spec: loopzerov1.ClusterCredentialSpec{
			ClusterName: "loopzero-cell-0",
			APIServer:   "https://172.18.0.7:6443",
			CellID:      "cell-0",
			SecretRef: loopzerov1.SecretReference{
				Name:      "workload-not-matching-cellid-kubeconfig",
				Namespace: "loopzero-system",
			},
		},
	}
	mgmt := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cc).Build()
	def := fake.NewClientBuilder().WithScheme(scheme).Build()
	f := NewCellAwareRuntimeClientFactory(mgmt, def, "loopzero-system")

	// Lookup by spec.cellID, NOT by metadata.name.
	_, err := f.For(context.Background(), "cell-0")
	// It should find the CR but fail on Secret missing — which proves it
	// matched by cellID not name.
	if err == nil {
		t.Fatal("expected Secret-missing error (proving CR matched by cellID), got nil")
	}
	// Drift guard: should NOT report "no ClusterCredential found" because
	// the CR exists, just under a different name.
	want := "no ClusterCredential found"
	if got := err.Error(); contains(got, want) {
		t.Errorf("factory looked up by metadata.name (drift!): %s", got)
	}
}

func TestStaticFactory_ReturnsSameClient(t *testing.T) {
	scheme := newSchemeForTest(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	f := NewStaticRuntimeClientFactory(c)

	for _, cellID := range []string{"", "cell-0", "cell-0", "cell-1"} {
		got, err := f.For(context.Background(), cellID)
		if err != nil {
			t.Fatalf("For(%q) unexpected error: %v", cellID, err)
		}
		if got != c {
			t.Errorf("For(%q): expected static client, got different", cellID)
		}
	}
}

func TestStaticFactory_NilErrors(t *testing.T) {
	f := NewStaticRuntimeClientFactory(nil)
	_, err := f.For(context.Background(), "cell-1")
	if err == nil {
		t.Fatal("expected error for nil static client, got nil")
	}
}

// _ is a sentinel ensuring CellAwareRuntimeClientFactory implements
// RuntimeClientFactory. Compile-time guard.
var _ RuntimeClientFactory = (*CellAwareRuntimeClientFactory)(nil)
var _ client.Client = (client.Client)(nil)

func contains(haystack, needle string) bool {
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// A deployment that has no runtime cluster access (app-operator without
// CELL_0_KUBECONFIG) is a configuration, not a nil: every lookup returns a
// typed error the caller can recognise, instead of each controller carrying
// its own nil branch with its own degraded-mode semantics.
func TestUnavailableFactory_ReturnsTheTypedErrorForEveryCell(t *testing.T) {
	f := NewUnavailableRuntimeClientFactory("CELL_0_KUBECONFIG not set")

	for _, cellID := range []string{"", "cell-0", "cell-1"} {
		c, err := f.For(context.Background(), cellID)
		if !errors.Is(err, ErrRuntimeClusterUnavailable) {
			t.Fatalf("For(%q) error = %v, want ErrRuntimeClusterUnavailable", cellID, err)
		}
		if c != nil {
			t.Fatalf("For(%q) returned a client alongside the error", cellID)
		}
	}
}
