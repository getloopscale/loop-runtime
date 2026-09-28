// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metering

// secret_cell_client.go — the SecretRef-based cell client provider (plan
// metering-compute-loop T3.2, slice S5 enabler). The metering worker is the first
// api-side consumer that must list pods on tenant cells; there was no production
// mechanism to resolve a cell's kubernetes.Interface from its ClusterCredential.
// The former per-cell ClientManager under api/internal/ was path-based
// (StaticRouter), incompatible with the DynamicRouter's SecretRef model, and was
// removed by B-023 — so this provider mirrors the operators-side
// CellAwareRuntimeClientFactory (list ClusterCredential by spec.cellID → read its
// kubeconfig Secret → build a client) but returns a client-go kubernetes.Interface
// (the shape PodLister needs) instead of a controller-runtime client.Client.

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/getloopscale/loopzero/api/internal/k8s"
	"github.com/getloopscale/loopzero/api/internal/placement"
)

// CellResolver resolves a connected cell to its connection details. In
// production it is placement.DynamicRouter, which lists ClusterCredentials by
// spec.cellID, keeps only status.connected ones and refreshes periodically —
// the one place cell discovery is implemented.
type CellResolver interface {
	Resolve(ctx context.Context, cellID string) (*placement.CellConfig, error)
}

// SecretCellClientProvider builds a read-scoped client for a cell from the
// kubeconfig Secret the cell's ClusterCredential names, and caches it until
// Invalidate is called (the pod lister does so on 401/403, so a rotated
// credential is picked up on the next pass).
type SecretCellClientProvider struct {
	cells   CellResolver
	secrets client.Reader
	mu      sync.RWMutex
	clients map[string]kubernetes.Interface
}

// NewSecretCellClientProvider wires the provider to the cell resolver and to a
// reader of the core cluster, where the kubeconfig Secrets live.
func NewSecretCellClientProvider(cells CellResolver, secrets client.Reader) *SecretCellClientProvider {
	return &SecretCellClientProvider{
		cells:   cells,
		secrets: secrets,
		clients: make(map[string]kubernetes.Interface),
	}
}

// Invalidate drops the cached client for cellID; the next ClientFor rebuilds
// it from the current Secret.
func (p *SecretCellClientProvider) Invalidate(cellID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.clients, cellID)
}

func (p *SecretCellClientProvider) ClientFor(ctx context.Context, cellID string) (kubernetes.Interface, error) {
	p.mu.RLock()
	if c, ok := p.clients[cellID]; ok {
		p.mu.RUnlock()
		return c, nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.clients[cellID]; ok { // double-check under the write lock
		return c, nil
	}

	cell, err := p.cells.Resolve(ctx, cellID)
	if err != nil {
		return nil, fmt.Errorf("metering: resolve cell %q: %w", cellID, err)
	}
	secretNS := cell.SecretRefNamespace
	if secretNS == "" {
		secretNS = k8s.ControlPlaneNamespace
	}
	var secret corev1.Secret
	if err := p.secrets.Get(ctx, client.ObjectKey{Name: cell.SecretRefName, Namespace: secretNS}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("metering: kubeconfig Secret %s/%s not found for cell %q", secretNS, cell.SecretRefName, cellID)
		}
		return nil, fmt.Errorf("metering: get kubeconfig Secret for cell %q: %w", cellID, err)
	}
	kubeconfig, ok := secret.Data["kubeconfig"]
	if !ok || len(kubeconfig) == 0 {
		return nil, fmt.Errorf("metering: Secret %s/%s has no kubeconfig key for cell %q", secretNS, cell.SecretRefName, cellID)
	}

	restCfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("metering: parse kubeconfig for cell %q: %w", cellID, err)
	}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("metering: build kubernetes client for cell %q: %w", cellID, err)
	}

	p.clients[cellID] = cs
	return cs, nil
}
