// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"fmt"
	"github.com/getloopscale/loopzero/api/internal/adapters"
	"log/slog"

	"github.com/getloopscale/loopzero/api/internal/services/build"

	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/k8s/secretsync"
	"github.com/getloopscale/loopzero/api/internal/k8s/tenancy"
	"github.com/getloopscale/loopzero/api/internal/k8s/workload"
	"github.com/getloopscale/loopzero/api/internal/routes/secrets"
)

// K8sDeps holds Kubernetes client managers used by the API server.
type K8sDeps struct {
	TenantCRMgr   *tenancy.TenantCRManager
	PreviewCRMgr  *workload.PreviewCRManager
	AppCRMgr      build.ApplicationCREnsurer
	SecretK8sSync secrets.SecretK8sSync
}

// setupK8s creates the Kubernetes client managers for Tenant CRs, Application CRs,
// and Secret K8s sync (ESO). Returns error if tenant CR manager creation fails.
func setupK8s(cfg config.Config, logger *slog.Logger) (*K8sDeps, error) {
	// Tenant CR manager — real Kubernetes client for Tenant CRs.
	tenantCRMgr, err := tenancy.NewTenantCRManager(cfg.MgmtKubeconfig, cfg.MgmtKubeContext)
	if err != nil {
		return nil, fmt.Errorf("tenant CR manager: %w", err)
	}

	// Application CR manager — creates LoopZeroApplication CRs on mgmt cluster.
	appCRMgr := &adapters.ApplicationCRAdapter{Mgr: workload.NewApplicationCRManager(tenantCRMgr.Client, "")}

	// Secret K8s manager — creates K8s Secrets on mgmt + ExternalSecret CRs on workload.
	// F-01 fix: Use interface variable to avoid Go nil interface trap.
	var secretK8sSync secrets.SecretK8sSync
	if mgr, err := secretsync.NewSecretK8sManager(
		cfg.MgmtKubeconfig, cfg.MgmtKubeContext,
		cfg.RuntimeKubeconfig, cfg.RuntimeKubeContext,
	); err != nil {
		logger.Warn("secret K8s manager not available — ESO sync disabled", "error", err)
	} else {
		secretK8sSync = mgr
	}

	// Preview CR manager — creates/queries PreviewEnvironment CRs on
	// mgmt cluster. Shares the same typed client + namespace as
	// TenantCRManager (both target loopzero-system in the mgmt cluster).
	previewCRMgr := workload.NewPreviewCRManager(tenantCRMgr.Client)

	return &K8sDeps{
		TenantCRMgr:   tenantCRMgr,
		PreviewCRMgr:  previewCRMgr,
		AppCRMgr:      appCRMgr,
		SecretK8sSync: secretK8sSync,
	}, nil
}
