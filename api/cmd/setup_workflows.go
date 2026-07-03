// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"time"

	"github.com/getloopscale/loopzero/api/internal/services/build"

	"github.com/getloopscale/loopzero/api/internal/services/platform"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/getloopscale/loopzero/api/internal/argo"
	"github.com/getloopscale/loopzero/api/internal/argocd"
	"github.com/getloopscale/loopzero/api/internal/cfkv"
	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/gitops"
)

// WorkflowDeps holds workflow submitter, sync client, gitops pusher, and KV client.
type WorkflowDeps struct {
	// WorkflowSubmitter is circuit-breaker-wrapped.
	WorkflowSubmitter *platform.CircuitBreakerWorkflowSubmitter
	SyncClient        *platform.CircuitBreakerSyncClient
	GitOpsPusher      *gitops.GiteaGitOpsPusher
	KVClient          build.StaticRoutingClient
}

// setupWorkflows creates the Argo workflow submitter (with circuit breaker),
// ArgoCD sync client (with circuit breaker), GitOps pusher, and CF KV client.
func setupWorkflows(cfg config.Config, httpTransport *http.Transport, logger *slog.Logger) (*WorkflowDeps, error) {
	// Workflow submitter — Argo Workflows in build cluster
	// C-01 fix: CallbackURL/CallbackSecret removed from workflow config.
	// The workflow no longer calls the API. Build completion is detected
	// by the BuildReconcileLoop polling Argo Workflow CRDs.
	rawWorkflowSubmitter, err := argo.NewWorkflowSubmitter(argo.WorkflowSubmitterConfig{
		Kubeconfig:            cfg.BuildKubeconfig,
		Context:               cfg.BuildKubeContext,
		RegistryURL:           cfg.RegistryURL,
		RegistryInsecure:      cfg.RegistryInsecure,
		BaseDomain:            cfg.BaseDomain,
		HarborRegistryURL:     cfg.HarborRegistryURL,
		CosignKeyless:         cfg.CosignKeyless,       // v1.1 EC-3 wire env var
		CosignAttestEnabled:   cfg.CosignAttestEnabled, // v1.1 EC-3 wire env var
		CosignFulcioURL:       cfg.CosignFulcioURL,
		CosignRekorURL:        cfg.CosignRekorURL,
		CosignInsecureSkipSCT: cfg.CosignInsecureSkipSCT, // T1.2 CR-1 fix
		Env:                   cfg.Env,                   // T1.1 of plan 2026-06-01-p1-security-hardening (sec-010 per-tenant Fulcio audience)
	})
	if cfg.CosignKeyless {
		logger.Info("cosign keyless mode ENABLED",
			"fulcio_url", cfg.CosignFulcioURL,
			"rekor_url", cfg.CosignRekorURL,
			"attest_enabled", cfg.CosignAttestEnabled)
	}
	if err != nil {
		return nil, err
	}

	// REVIEW-FIX: Wire CircuitBreakerWorkflowSubmitter to protect against
	// Argo Workflow API outages. Without this, 10 concurrent deploys against
	// a down Argo controller waste ~70s in retries instead of failing fast.
	workflowSubmitter := platform.NewCircuitBreakerWorkflowSubmitter(
		rawWorkflowSubmitter,
		platform.CircuitBreakerConfig{
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
		// G402 expected: `cfg.ArgocdInsecure` is a config flag enabled
		// ONLY for local k3d (per CLAUDE.md: cert-manager letsencrypt-prod
		// cannot issue for `*.loopzero.local` since there is no public DNS
		// for the ACME challenge). Production sets this flag false and
		// uses CF-managed TLS via the runtime cluster. Audit SEC-101.
		argocdHTTPClient.Transport = &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // G402 expected: local k3d only (cfg.ArgocdInsecure gates)
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
	// CHG-2026-04-30-DOGFOOD-LOCAL-HEAL NEW-15: prefer KubeMintingTokenProvider
	// when a mgmt kubeconfig + argocd-refresh-credentials Secret are reachable.
	// Falls back to the static cfg.ArgocdToken when those preconditions aren't
	// met (eg. unit tests, smoke environments). The provider self-heals on
	// expiry / 401 — eliminates the manual `task k3d:heal` H11 step.
	tokenProvider := buildArgoCDTokenProvider(cfg, logger)
	syncCfg := argocd.SyncClientConfig{
		BaseURL:    cfg.ArgocdURL,
		Insecure:   cfg.ArgocdInsecure,
		HTTPClient: argocdHTTPClient,
	}
	if tokenProvider != nil {
		syncCfg.TokenProvider = tokenProvider
	} else {
		syncCfg.Token = cfg.ArgocdToken
	}
	rawSyncClient := argocd.NewSyncClient(syncCfg)
	syncClient := platform.NewCircuitBreakerSyncClient(rawSyncClient, platform.CircuitBreakerConfig{
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
	var kvClient build.StaticRoutingClient
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

// buildArgoCDTokenProvider returns a self-healing KubeMintingTokenProvider
// when the runtime has access to the mgmt cluster (in-cluster or via
// MGMT_KUBECONFIG) AND the argocd-refresh-credentials Secret is present.
// Otherwise returns nil so the caller falls back to the static token.
//
// Failure cases logged at WARN — they are non-fatal: a stale static token
// will still work until ArgoCD rejects it, at which point it will surface
// as DEPLOY_SERVICE_AUTH_FAILED for the operator.
func buildArgoCDTokenProvider(cfg config.Config, logger *slog.Logger) argocd.TokenProvider {
	clientset, err := mgmtKubeClient(cfg)
	if err != nil {
		logger.Warn("argocd token provider: cannot build mgmt kube client, using static token",
			"error", err)
		return nil
	}
	provider, err := argocd.NewKubeMintingTokenProvider(argocd.KubeMintingConfig{
		ArgoCDBaseURL: cfg.ArgocdURL,
		Insecure:      cfg.ArgocdInsecure,
		KubeClient:    clientset,
		Logger:        logger,
	})
	if err != nil {
		logger.Warn("argocd token provider: NewKubeMintingTokenProvider failed, using static token",
			"error", err)
		return nil
	}
	provider.Start(context.Background())
	logger.Info("argocd token provider: KubeMintingTokenProvider active (self-healing)",
		"argocd_url", cfg.ArgocdURL)
	return provider
}

// mgmtKubeClient constructs a typed kubernetes.Interface for the mgmt
// cluster. Prefers in-cluster config (so the pod's own ServiceAccount
// token + RBAC apply — required for loopzero-api's Secret read/write rules
// granted via clusterrolebinding/loopzero-api). Falls back to MGMT_KUBECONFIG
// / MGMT_KUBE_CONTEXT for out-of-cluster execution (CLI tests, etc).
//
// Why this order matters (NEW-15 follow-up): in local k3d the
// loopzero-cluster-kubeconfigs Secret references kube-system:default SA which
// does NOT have the namespaced Secret read RBAC granted to loopzero-api SA.
// Using in-cluster credentials avoids the RBAC mismatch entirely.
func mgmtKubeClient(cfg config.Config) (kubernetes.Interface, error) {
	// Try in-cluster first.
	if inCfg, err := rest.InClusterConfig(); err == nil {
		return kubernetes.NewForConfig(inCfg)
	}
	// Fall back to explicit kubeconfig.
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: cfg.MgmtKubeconfig}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: cfg.MgmtKubeContext}
	restCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(restCfg)
}
