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
	apiv1 "github.com/getloopscale/loopzero/operators/api/v1"
)

// SecretCellClientProvider resolves a cell's read-scoped kubernetes.Interface by
// reading its ClusterCredential CR + kubeconfig Secret from the mgmt cluster. It
// implements CellClientProvider (the seam PodListerFactory depends on). Built
// clients are cached per cell (the kubeconfig is stable for the cell's lifetime;
// a rotated credential is picked up on the next controller restart — acceptable
// for observe-only metering).
type SecretCellClientProvider struct {
	mgmtClient   client.Client
	mgmtNS       string
	mu           sync.RWMutex
	clientsByCel map[string]kubernetes.Interface
}

// NewSecretCellClientProvider wires the provider to the core-cluster client (the
// controller's controller-runtime client, which reads ClusterCredential CRs +
// Secrets). mgmtNamespace defaults to loopzero-system (ADR-0018 — the canonical CR ns).
func NewSecretCellClientProvider(mgmtClient client.Client, mgmtNamespace string) *SecretCellClientProvider {
	if mgmtNamespace == "" {
		mgmtNamespace = k8s.ControlPlaneNamespace
	}
	return &SecretCellClientProvider{
		mgmtClient:   mgmtClient,
		mgmtNS:       mgmtNamespace,
		clientsByCel: make(map[string]kubernetes.Interface),
	}
}

// ClientFor returns the cell's kubernetes.Interface. It lists ClusterCredential
// CRs and matches by spec.cellID (MEET-053 B9 — NOT metadata.name), reads the
// referenced kubeconfig Secret's data.kubeconfig, and builds a lazy client. Every
// failure is a typed error — never a silent nil client that would meter zero and
// advance the cursor.
func (p *SecretCellClientProvider) ClientFor(ctx context.Context, cellID string) (kubernetes.Interface, error) {
	p.mu.RLock()
	if c, ok := p.clientsByCel[cellID]; ok {
		p.mu.RUnlock()
		return c, nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.clientsByCel[cellID]; ok { // double-check under the write lock
		return c, nil
	}

	var ccList apiv1.ClusterCredentialList
	if err := p.mgmtClient.List(ctx, &ccList, client.InNamespace(p.mgmtNS)); err != nil {
		return nil, fmt.Errorf("metering: list ClusterCredentials for cell %q: %w", cellID, err)
	}
	var match *apiv1.ClusterCredential
	for i := range ccList.Items {
		if ccList.Items[i].Spec.CellID == cellID {
			match = &ccList.Items[i]
			break
		}
	}
	if match == nil {
		return nil, fmt.Errorf("metering: no ClusterCredential with spec.cellID=%q in namespace %s", cellID, p.mgmtNS)
	}

	secretNS := match.Spec.SecretRef.Namespace
	if secretNS == "" {
		secretNS = p.mgmtNS
	}
	var secret corev1.Secret
	if err := p.mgmtClient.Get(ctx, client.ObjectKey{Name: match.Spec.SecretRef.Name, Namespace: secretNS}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("metering: kubeconfig Secret %s/%s not found for cell %q", secretNS, match.Spec.SecretRef.Name, cellID)
		}
		return nil, fmt.Errorf("metering: get kubeconfig Secret for cell %q: %w", cellID, err)
	}
	kubeconfig, ok := secret.Data["kubeconfig"]
	if !ok || len(kubeconfig) == 0 {
		return nil, fmt.Errorf("metering: Secret %s/%s has no data.kubeconfig for cell %q", secretNS, match.Spec.SecretRef.Name, cellID)
	}

	restCfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("metering: parse kubeconfig for cell %q: %w", cellID, err)
	}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("metering: build kubernetes client for cell %q: %w", cellID, err)
	}

	p.clientsByCel[cellID] = cs
	return cs, nil
}
