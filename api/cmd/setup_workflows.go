// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"log/slog"
	"net/http"

	"github.com/getloopscale/loop-runtime/api/internal/backgroundloops"
	"github.com/getloopscale/loop-runtime/api/internal/config"
	"github.com/getloopscale/loop-runtime/api/internal/gitops"
	"github.com/getloopscale/loop-runtime/api/internal/orchestrationbreaker"
)

// WorkflowDeps holds the workflow submitter, the sync client and the gitops pusher.
type WorkflowDeps struct {
	// WorkflowSubmitter is circuit-breaker-wrapped.
	WorkflowSubmitter *orchestrationbreaker.CircuitBreakerWorkflowSubmitter
	SyncClient        *orchestrationbreaker.CircuitBreakerSyncClient
	GitOpsPusher      *gitops.GiteaGitOpsPusher
}

// setupWorkflows creates the Argo workflow submitter, the ArgoCD sync client
// and the GitOps pusher through backgroundloops (the same constructors the
// controller binary uses).
func setupWorkflows(cfg config.Config, httpTransport *http.Transport, logger *slog.Logger) (*WorkflowDeps, error) {
	workflowSubmitter, err := backgroundloops.NewWorkflowSubmitter(cfg, logger)
	if err != nil {
		return nil, err
	}

	syncClient, err := backgroundloops.NewSyncClient(cfg, httpTransport, logger)
	if err != nil {
		return nil, err
	}

	return &WorkflowDeps{
		WorkflowSubmitter: workflowSubmitter,
		SyncClient:        syncClient,
		GitOpsPusher:      backgroundloops.NewGitOpsPusher(cfg),
	}, nil
}
