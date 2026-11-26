// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package k8s

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

// NewScheme builds a runtime.Scheme with the loopzero.io/v1 CRD types registered
// (Tenant, LoopZeroApplication, PreviewEnvironment, …) via the operators/api
// contract module (M6/M8). The typed client uses this to (de)serialize CRs as
// Go structs instead of unstructured maps — the compiler now checks every field.
func NewScheme() (*runtime.Scheme, error) {
	s := runtime.NewScheme()
	if err := apiv1.AddToScheme(s); err != nil {
		return nil, fmt.Errorf("k8s: register loopzero.io scheme: %w", err)
	}
	return s, nil
}

// RestConfig resolves a *rest.Config: in-cluster first (API running inside the
// cluster), then kubeconfig with the given context (local development). Shared
// by every typed CR manager (DRY — was duplicated per manager pre-M8).
func RestConfig(kubeconfig, kubeContext string) (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	return ExplicitRestConfig(kubeconfig, kubeContext)
}

// ExplicitRestConfig resolves a *rest.Config from the given kubeconfig file and
// context, WITHOUT preferring the in-cluster config. Use it for a cluster the
// API does not run in (build, runtime cells): RestConfig would silently return
// the cluster the pod runs in. An empty kubeconfig with no other configuration
// falls back to in-cluster, which is clientcmd's own behaviour for an empty
// config.
func ExplicitRestConfig(kubeconfig, kubeContext string) (*rest.Config, error) {
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
	overrides := &clientcmd.ConfigOverrides{}
	if kubeContext != "" {
		overrides.CurrentContext = kubeContext
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("k8s: resolve rest config: %w", err)
	}
	return cfg, nil
}

// NewTypedClient builds a controller-runtime client.Client wired to the loopzero.io
// scheme. This is the typed replacement for the dynamic client the loopzero.io CR
// managers used pre-M8 — it makes api the second Go consumer of operators/api.
func NewTypedClient(cfg *rest.Config) (client.Client, error) {
	scheme, err := NewScheme()
	if err != nil {
		return nil, err
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("k8s: typed client: %w", err)
	}
	return c, nil
}
