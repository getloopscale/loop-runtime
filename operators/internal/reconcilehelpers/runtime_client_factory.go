// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Cell-aware runtime client factory for the EnvironmentReconciler.
//
// cell-1-activation-plan iter 4 BUG-FIX:
// The pre-existing EnvironmentReconciler held a single `RuntimeClient` field
// configured at startup pointing to one runtime cluster. When a tenant was
// pinned to cell-1 (via `loopzero admin tenants pin --cell cell-1`), the
// reconciler still created Namespace/RBAC/NetworkPolicy/Quota on cell-0 —
// violating ADR C10 (tenant data isolation) and recreating orphan namespaces
// on the source cell after re-pin.
//
// This factory closes the gap:
//   1. Reads the parent Tenant CR to determine spec.cellID.
//   2. For cellID="" → returns the legacy RuntimeClient (preserves the
//      single-cluster path where no cell was ever resolved).
//   3. For EVERY named cellID (cell-0, cell-1, ...) → looks up the matching
//      ClusterCredential CR by spec.cellID, reads its kubeconfig Secret,
//      builds a controller-runtime client.Client from it, and caches the
//      result for reuse. Symmetric to api/internal/cellmgr.ClientManager.
//
// O atalho do item 2 já cobriu um nome de cell, e isso era um defeito: enquanto a
// primeira cell se chamou `cell-default`, o nome era ao mesmo tempo uma cell do
// catálogo e a sentinela de "sem cell". Com a unificação 1:1 (ADR 2026-08-09) ela
// passou a ser `cell-0`, uma cell real com tenants — e manter o atalho faria TODO
// lookup dela devolver o cliente legado em vez de resolver o ClusterCredential dela,
// que é exatamente o isolamento que esta factory existe para garantir. A string
// vazia é a única sentinela a partir daqui, pelo mesmo motivo que `NULL` passou a
// ser a única no banco (migração 000061, fase 4).
//
// Thread-safe via sync.RWMutex with double-check pattern. Cache eviction
// is left to operator restart (token rotation triggers staleness; heal
// script H14+H15 keep ArgoCD + ClusterCredential Secrets in sync, but the
// in-memory cache here is built from the Secret at first lookup; subsequent
// rotations require a process restart or invalidate-on-error policy.
// Future work: integrate ClusterCredential reconcile event watcher to
// invalidate cache on Secret update).

package reconcilehelpers

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
)

// RuntimeClientFactory abstracts cell-aware runtime client lookup.
// Implementations resolve a tenant's cellID to the right cluster client.
type RuntimeClientFactory interface {
	// For returns a controller-runtime client connected to the runtime
	// cluster identified by cellID. Só cellID "" devolve o cliente
	// legado; toda cell nomeada resolve o ClusterCredential dela.
	For(ctx context.Context, cellID string) (client.Client, error)
}

// CellAwareRuntimeClientFactory implements RuntimeClientFactory by reading
// ClusterCredential CRs from the mgmt cluster and building runtime clients
// from their kubeconfig Secrets.
type CellAwareRuntimeClientFactory struct {
	// MgmtClient reads ClusterCredential CRs + their kubeconfig Secrets.
	MgmtClient client.Client
	// DefaultClient is returned only for cellID="" (legacy path).
	DefaultClient client.Client
	// MgmtNamespace is where ClusterCredential CRs live (default: loopzero-system).
	MgmtNamespace string
	// SchemeProvider is the controller-runtime scheme used to build clients.
	// Must include corev1 + loopzerov1 (for ClusterCredential GET).
	SchemeProvider func() *client.Options

	mu      sync.RWMutex
	clients map[string]client.Client
}

// NewCellAwareRuntimeClientFactory builds a factory wired to the mgmt
// cluster client + a default runtime client (legacy single-cell path).
// MgmtNamespace defaults to "loopzero-system" if empty.
func NewCellAwareRuntimeClientFactory(mgmtClient, defaultClient client.Client, mgmtNamespace string) *CellAwareRuntimeClientFactory {
	if mgmtNamespace == "" {
		mgmtNamespace = "loopzero-system"
	}
	return &CellAwareRuntimeClientFactory{
		MgmtClient:    mgmtClient,
		DefaultClient: defaultClient,
		MgmtNamespace: mgmtNamespace,
		clients:       make(map[string]client.Client),
	}
}

// For implements RuntimeClientFactory.
func (f *CellAwareRuntimeClientFactory) For(ctx context.Context, cellID string) (client.Client, error) {
	if cellID == "" {
		if f.DefaultClient == nil {
			return nil, fmt.Errorf("runtime-client-factory: default client not configured for cellID=%q", cellID)
		}
		return f.DefaultClient, nil
	}

	// Fast path: cached client.
	f.mu.RLock()
	if c, ok := f.clients[cellID]; ok {
		f.mu.RUnlock()
		return c, nil
	}
	f.mu.RUnlock()

	// Slow path: write lock with double-check.
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.clients[cellID]; ok {
		return c, nil
	}

	// Find ClusterCredential CR with spec.cellID == requested cellID.
	// We list CRs (small set; CR per cell) and filter by spec — equivalent
	// to the API's K8sCellStore.LoadCells pattern (MEET-053 B9: lookup by
	// spec.cellID, NOT metadata.name).
	var ccList loopzerov1.ClusterCredentialList
	if err := f.MgmtClient.List(ctx, &ccList, client.InNamespace(f.MgmtNamespace)); err != nil {
		return nil, fmt.Errorf("runtime-client-factory: list ClusterCredentials: %w", err)
	}
	var match *loopzerov1.ClusterCredential
	for i := range ccList.Items {
		if ccList.Items[i].Spec.CellID == cellID {
			match = &ccList.Items[i]
			break
		}
	}
	if match == nil {
		return nil, fmt.Errorf("runtime-client-factory: no ClusterCredential found with spec.cellID=%q in namespace %s", cellID, f.MgmtNamespace)
	}

	// Read kubeconfig Secret referenced by the CC.
	var secret corev1.Secret
	secretRefNS := match.Spec.SecretRef.Namespace
	if secretRefNS == "" {
		secretRefNS = f.MgmtNamespace
	}
	if err := f.MgmtClient.Get(ctx, client.ObjectKey{
		Name:      match.Spec.SecretRef.Name,
		Namespace: secretRefNS,
	}, &secret); err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("runtime-client-factory: kubeconfig Secret %s/%s not found for cell %q",
				secretRefNS, match.Spec.SecretRef.Name, cellID)
		}
		return nil, fmt.Errorf("runtime-client-factory: get kubeconfig Secret for cell %q: %w", cellID, err)
	}
	kubeconfigBytes, ok := secret.Data["kubeconfig"]
	if !ok || len(kubeconfigBytes) == 0 {
		return nil, fmt.Errorf("runtime-client-factory: Secret %s/%s has no data.kubeconfig",
			secretRefNS, match.Spec.SecretRef.Name)
	}

	// Build rest.Config from kubeconfig bytes + controller-runtime Client.
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return nil, fmt.Errorf("runtime-client-factory: parse kubeconfig for cell %q: %w", cellID, err)
	}
	scheme := f.MgmtClient.Scheme()
	c, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("runtime-client-factory: build client for cell %q: %w", cellID, err)
	}

	f.clients[cellID] = c
	return c, nil
}

// staticRuntimeClientFactory is a trivial RuntimeClientFactory that always
// returns the same client regardless of cellID. Used in suite_test.go and
// any other code path that hasn't been migrated to multi-cell yet —
// preserves backward compatibility during incremental rollout.
type staticRuntimeClientFactory struct {
	c client.Client
}

// NewStaticRuntimeClientFactory wraps a single client as a factory. For
// tests + legacy single-cell deployments that don't have a mgmt-side
// ClusterCredential registry yet.
func NewStaticRuntimeClientFactory(c client.Client) RuntimeClientFactory {
	return &staticRuntimeClientFactory{c: c}
}

// For returns the static client regardless of cellID.
func (s *staticRuntimeClientFactory) For(_ context.Context, _ string) (client.Client, error) {
	if s.c == nil {
		return nil, fmt.Errorf("static-runtime-client-factory: no client configured")
	}
	return s.c, nil
}
