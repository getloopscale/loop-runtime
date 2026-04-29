// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"crypto/tls"
	"log/slog"
	"net/http"
	"time"

	"github.com/loopzero/loopzero/api/internal/argo"
	"github.com/loopzero/loopzero/api/internal/argocd"
	"github.com/loopzero/loopzero/api/internal/config"
	"github.com/loopzero/loopzero/api/internal/gitops"
	"github.com/loopzero/loopzero/api/internal/services"
	"github.com/loopzero/loopzero/api/internal/cfkv"
)

// WorkflowDeps holds workflow submitter, sync client, gitops pusher, and KV client.
type WorkflowDeps struct {
	// WorkflowSubmitter is circuit-breaker-wrapped.
	WorkflowSubmitter *services.CircuitBreakerWorkflowSubmitter
	SyncClient        *services.CircuitBreakerSyncClient
	GitOpsPusher      *gitops.GiteaGitOpsPusher
	KVClient          services.StaticRoutingClient
}

// setupWorkflows creates the Argo workflow submitter (with circuit breaker),
// ArgoCD sync client (with circuit breaker), GitOps pusher, and CF KV client.
func setupWorkflows(cfg config.Config, httpTransport *http.Transport, logger *slog.Logger) (*WorkflowDeps, error) {
	// Workflow submitter — Argo Workflows in build cluster
	// C-01 fix: CallbackURL/CallbackSecret removed from workflow config.
	// The workflow no longer calls the API. Build completion is detected
	// by the BuildReconcileLoop polling Argo Workflow CRDs.
	rawWorkflowSubmitter, err := argo.NewWorkflowSubmitter(argo.WorkflowSubmitterConfig{
		Kubeconfig:        cfg.BuildKubeconfig,
		Context:           cfg.BuildKubeContext,
		RegistryURL:       cfg.RegistryURL,
		RegistryInsecure:  cfg.RegistryInsecure,
		BaseDomain:        cfg.BaseDomain,
		HarborRegistryURL: cfg.HarborRegistryURL,
	})
	if err != nil {
		return nil, err
	}

	// REVIEW-FIX: Wire CircuitBreakerWorkflowSubmitter to protect against
	// Argo Workflow API outages. Without this, 10 concurrent deploys against
	// a down Argo controller waste ~70s in retries instead of failing fast.
	workflowSubmitter := services.NewCircuitBreakerWorkflowSubmitter(
		rawWorkflowSubmitter,
		services.CircuitBreakerConfig{
			OpenThreshold:  cfg.CBOpenThreshold,
			CloseThreshold: cfg.CBCloseThreshold,
			RetryInterval:  cfg.CBRetryInterval,
		},
		logger,
	)

	// ArgoCD HTTP client — shares transport pooling but uses ArgoCD-specific timeout.
	// When Insecure is true, we need a separate transport for TLS skip.
	argocdHTTPClient := &http.Client{Timeout: cfg.ArgocdClientTimeout}
	if cfg.ArgocdInsecure {
		argocdHTTPClient.Transport = &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		}
	} else {
		argocdHTTPClient.Transport = httpTransport
	}

	// ArgoCD sync client with in-memory circuit breaker (M-02 fix).
	// ADR-0008 Phase 1+: Replace single syncClient with SyncClientFactory per Cell.
	// For now (single Cell), the circuit-breaker-wrapped client is used directly.
	// When multi-cell activates, create SyncClientFactory(cellRouter, builderFn)
	// and wrap each Cell's client with its own circuit breaker.
	// State is per-replica, not shared via PostgreSQL. This eliminates
	// write amplification and the circular dependency where the circuit
	// breaker protecting ArgoCD depended on PostgreSQL being healthy.
	rawSyncClient := argocd.NewSyncClient(argocd.SyncClientConfig{
		BaseURL:    cfg.ArgocdURL,
		Token:      cfg.ArgocdToken,
		Insecure:   cfg.ArgocdInsecure,
		HTTPClient: argocdHTTPClient,
	})
	syncClient := services.NewCircuitBreakerSyncClient(rawSyncClient, services.CircuitBreakerConfig{
		OpenThreshold:  cfg.CBOpenThreshold,
		CloseThreshold: cfg.CBCloseThreshold,
		RetryInterval:  cfg.CBRetryInterval,
	}, logger)

	// GitOps pusher — commits manifests to Gitea after build succeeds
	gitopsPusher := gitops.NewGiteaGitOpsPusher(gitops.GiteaGitOpsPusherConfig{
		BaseURL: cfg.GiteaURL,
		Token:   cfg.GiteaToken,
		OrgName: cfg.GiteaOrg, // B77: was hardcoded "loopzero"
	})

	// Cloudflare KV client for static frontend routing (Sprint 10).
	// nil when CF credentials not configured (local dev).
	// IMPORTANT: Assign to the interface type directly to avoid the Go nil interface trap.
	// A typed nil *cfkv.Client assigned to a StaticRoutingClient interface is NOT nil,
	// which causes panics when methods are called on it.
	var kvClient services.StaticRoutingClient
	if c := cfkv.NewClient(cfg.CFAccountID, cfg.CFKVNamespaceID, cfg.CFAPIToken); c != nil {
		kvClient = c
		slog.Info("cloudflare KV client configured for static frontend routing")
	}

	return &WorkflowDeps{
		WorkflowSubmitter: workflowSubmitter,
		SyncClient:        syncClient,
		GitOpsPusher:      gitopsPusher,
		KVClient:          kvClient,
	}, nil
}
