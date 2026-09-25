// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package backgroundloops builds the platform's background loops (build
// reconcile, reapers, release convergence, pruners, domain lifecycle) and the
// cluster clients they drive, ONCE, for both binaries that can run them: the
// API (when it holds the loops lease) and the controller.
//
// It exists because each binary used to wire the loop graph by hand, and the
// copies drifted: the controller's lacked the deploy-event outbox writers
// (deploy.failed / deploy.succeeded webhooks), the submitter's Harbor URL and
// per-environment Fulcio audience, the self-healing ArgoCD token, and the
// audit-log pruner, build GC, cell metrics and domain loops. Whichever binary
// ran the loops decided whether deploy webhooks fired.
package backgroundloops

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/getloopscale/loopzero/api/internal/argocd"
	"github.com/getloopscale/loopzero/api/internal/buildplane/argo"
	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/gitops"
	"github.com/getloopscale/loopzero/api/internal/services/platform"
)

// NewWorkflowSubmitter builds the circuit-breaker-wrapped Argo Workflows
// submitter for the build cluster, with every submitter setting the
// configuration carries (Harbor registry, per-environment Fulcio audience,
// cosign keyless/attestation).
func NewWorkflowSubmitter(cfg config.Config, logger *slog.Logger) (*platform.CircuitBreakerWorkflowSubmitter, error) {
	raw, err := argo.NewWorkflowSubmitter(argo.WorkflowSubmitterConfig{
		Kubeconfig:            cfg.BuildKubeconfig,
		Context:               cfg.BuildKubeContext,
		RegistryURL:           cfg.RegistryURL,
		RegistryInsecure:      cfg.RegistryInsecure,
		BaseDomain:            cfg.BaseDomain,
		HarborRegistryURL:     cfg.HarborRegistryURL,
		CosignKeyless:         cfg.CosignKeyless,
		CosignAttestEnabled:   cfg.CosignAttestEnabled,
		CosignFulcioURL:       cfg.CosignFulcioURL,
		CosignRekorURL:        cfg.CosignRekorURL,
		CosignInsecureSkipSCT: cfg.CosignInsecureSkipSCT,
		Env:                   cfg.Env, // sec-010: per-environment Fulcio audience
	})
	if err != nil {
		return nil, fmt.Errorf("workflow submitter: %w", err)
	}
	if cfg.CosignKeyless {
		logger.Info("cosign keyless mode ENABLED",
			"fulcio_url", cfg.CosignFulcioURL,
			"rekor_url", cfg.CosignRekorURL,
			"attest_enabled", cfg.CosignAttestEnabled)
	}
	// Fail fast against an Argo outage instead of retrying every deploy.
	return platform.NewCircuitBreakerWorkflowSubmitter(raw, circuitBreaker(cfg), logger), nil
}

// NewSyncClient builds the circuit-breaker-wrapped ArgoCD sync client. It
// authenticates with the self-healing KubeMintingTokenProvider when the mgmt
// cluster and the argocd-refresh-credentials Secret are reachable, and falls
// back to the static cfg.ArgocdToken otherwise. transport is shared pooling
// for the secure case; nil uses the default transport.
func NewSyncClient(cfg config.Config, transport *http.Transport, logger *slog.Logger) *platform.CircuitBreakerSyncClient {
	httpClient := &http.Client{Timeout: cfg.ArgocdClientTimeout}
	if cfg.ArgocdInsecure {
		// Local k3d only: cert-manager cannot issue for *.loopzero.local. Production
		// sets ArgocdInsecure=false (audit SEC-101).
		httpClient.Transport = &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // G402 expected: local k3d only (cfg.ArgocdInsecure gates)
		}
	} else if transport != nil {
		httpClient.Transport = transport
	}
	syncCfg := argocd.SyncClientConfig{
		BaseURL:    cfg.ArgocdURL,
		Insecure:   cfg.ArgocdInsecure,
		HTTPClient: httpClient,
	}
	if provider := newArgoCDTokenProvider(cfg, logger); provider != nil {
		syncCfg.TokenProvider = provider
	} else {
		syncCfg.Token = cfg.ArgocdToken
	}
	return platform.NewCircuitBreakerSyncClient(argocd.NewSyncClient(syncCfg), circuitBreaker(cfg), logger)
}

// NewGitOpsPusher builds the Gitea pusher manifests are committed through.
func NewGitOpsPusher(cfg config.Config) *gitops.GiteaGitOpsPusher {
	return gitops.NewGiteaGitOpsPusher(gitops.GiteaGitOpsPusherConfig{
		BaseURL: cfg.GiteaURL,
		Token:   cfg.GiteaToken,
		OrgName: cfg.GiteaOrg,
	})
}

func circuitBreaker(cfg config.Config) platform.CircuitBreakerConfig {
	return platform.CircuitBreakerConfig{
		OpenThreshold:  cfg.CBOpenThreshold,
		CloseThreshold: cfg.CBCloseThreshold,
		RetryInterval:  cfg.CBRetryInterval,
	}
}

// newArgoCDTokenProvider returns a started KubeMintingTokenProvider, or nil
// (static token) when the mgmt cluster or its credentials are unreachable.
// Both failures are logged: a stale static token keeps working until ArgoCD
// rejects it.
func newArgoCDTokenProvider(cfg config.Config, logger *slog.Logger) argocd.TokenProvider {
	restCfg, err := MgmtRestConfig(cfg)
	if err != nil {
		logger.Warn("argocd token provider: no mgmt cluster config, using static token", "error", err)
		return nil
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		logger.Warn("argocd token provider: cannot build mgmt kube client, using static token", "error", err)
		return nil
	}
	provider, err := argocd.NewKubeMintingTokenProvider(argocd.KubeMintingConfig{
		ArgoCDBaseURL: cfg.ArgocdURL,
		Insecure:      cfg.ArgocdInsecure,
		KubeClient:    clientset,
		Logger:        logger,
	})
	if err != nil {
		logger.Warn("argocd token provider: NewKubeMintingTokenProvider failed, using static token", "error", err)
		return nil
	}
	provider.Start(context.Background())
	logger.Info("argocd token provider: KubeMintingTokenProvider active (self-healing)", "argocd_url", cfg.ArgocdURL)
	return provider
}

// MgmtRestConfig resolves the REST config for the mgmt cluster: in-cluster
// first (so the pod's own ServiceAccount and its RBAC apply), then the
// explicit CORE kubeconfig/context for out-of-cluster runs.
func MgmtRestConfig(cfg config.Config) (*rest.Config, error) {
	if inCluster, err := rest.InClusterConfig(); err == nil {
		return inCluster, nil
	}
	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: cfg.MgmtKubeconfig}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: cfg.MgmtKubeContext}
	restCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("mgmt cluster config: %w", err)
	}
	return restCfg, nil
}
