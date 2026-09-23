// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"log/slog"
	"net/http"

	"github.com/getloopscale/loopzero/api/internal/backgroundloops"
	"github.com/getloopscale/loopzero/api/internal/cfkv"
	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/gitops"
	"github.com/getloopscale/loopzero/api/internal/services/build"
	"github.com/getloopscale/loopzero/api/internal/services/platform"
)

// WorkflowDeps holds workflow submitter, sync client, gitops pusher, and KV client.
type WorkflowDeps struct {
	// WorkflowSubmitter is circuit-breaker-wrapped.
	WorkflowSubmitter *platform.CircuitBreakerWorkflowSubmitter
	SyncClient        *platform.CircuitBreakerSyncClient
	GitOpsPusher      *gitops.GiteaGitOpsPusher
	KVClient          build.StaticRoutingClient
}

// setupWorkflows creates the Argo workflow submitter, the ArgoCD sync client
// and the GitOps pusher through backgroundloops (the same constructors the
// controller binary uses), plus the Cloudflare KV client static builds use.
func setupWorkflows(cfg config.Config, httpTransport *http.Transport, logger *slog.Logger) (*WorkflowDeps, error) {
	workflowSubmitter, err := backgroundloops.NewWorkflowSubmitter(cfg, logger)
	if err != nil {
		return nil, err
	}

	// Cloudflare KV client for static frontend routing; nil when CF
	// credentials are not configured (local dev). Assigned to the interface
	// only when non-nil: a typed nil *cfkv.Client in a StaticRoutingClient is
	// not nil and panics on first use.
	var kvClient build.StaticRoutingClient
	if c := cfkv.NewClient(cfg.CFAccountID, cfg.CFKVNamespaceID, cfg.CFAPIToken); c != nil {
		kvClient = c
		logger.Info("cloudflare KV client configured for static frontend routing")
	}

	syncClient, err := backgroundloops.NewSyncClient(cfg, httpTransport, logger)
	if err != nil {
		return nil, err
	}

	return &WorkflowDeps{
		WorkflowSubmitter: workflowSubmitter,
		SyncClient:        syncClient,
		GitOpsPusher:      backgroundloops.NewGitOpsPusher(cfg),
		KVClient:          kvClient,
	}, nil
}
