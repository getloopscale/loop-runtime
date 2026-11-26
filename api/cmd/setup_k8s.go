// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"fmt"
	"log/slog"

	"github.com/getloopscale/loop-runtime/api/internal/builds/buildsadapter"
	"github.com/getloopscale/loop-runtime/pkg/domain"

	"github.com/getloopscale/loop-runtime/api/internal/builds"

	"github.com/getloopscale/loop-runtime/api/internal/config"
	"github.com/getloopscale/loop-runtime/api/internal/k8s/secretsync"
	"github.com/getloopscale/loop-runtime/api/internal/k8s/tenancy"
	"github.com/getloopscale/loop-runtime/api/internal/k8s/workload"
	"github.com/getloopscale/loop-runtime/api/internal/secrets/secretshttp"
)

// K8sDeps holds Kubernetes client managers used by the API server.
type K8sDeps struct {
	TenantCRMgr   *tenancy.TenantCRManager
	PreviewCRMgr  *workload.PreviewCRManager
	AppCRMgr      builds.ApplicationCREnsurer
	SecretK8sSync secretshttp.SecretK8sSync
}

// setupK8s creates the Kubernetes client managers for Tenant CRs and
// Application CRs. Secret K8s sync is wired later by setupSecretK8sSync,
// because it needs the tenant placement store. Returns error if tenant CR
// manager creation fails.
func setupK8s(cfg config.Config, logger *slog.Logger) (*K8sDeps, error) {
	// Tenant CR manager — real Kubernetes client for Tenant CRs.
	tenantCRMgr, err := tenancy.NewTenantCRManager(cfg.MgmtKubeconfig, cfg.MgmtKubeContext, cfg.MaxConcurrentBuildsPerTenant)
	if err != nil {
		return nil, fmt.Errorf("tenant CR manager: %w", err)
	}

	// Application CR manager — creates LoopRuntimeApplication CRs on mgmt cluster.
	appCRMgr := &buildsadapter.ApplicationCRAdapter{Mgr: workload.NewApplicationCRManager(tenantCRMgr.Client, "")}

	// Preview CR manager — creates/queries PreviewEnvironment CRs on
	// mgmt cluster. Shares the same typed client + namespace as
	// TenantCRManager (both target loopruntime-system in the mgmt cluster).
	previewCRMgr := workload.NewPreviewCRManager(tenantCRMgr.Client)

	return &K8sDeps{
		TenantCRMgr:  tenantCRMgr,
		PreviewCRMgr: previewCRMgr,
		AppCRMgr:     appCRMgr,
	}, nil
}

// setupSecretK8sSync builds the Secret + ExternalSecret writer (ESO). Its
// runtime client is the cell-0 kubeconfig (CELL_0_KUBECONFIG), so it serves
// domain.DefaultCellID only and refuses tenants placed elsewhere instead
// of writing their ExternalSecret into a cluster that does not host them.
// F-01: returns the interface as nil (not a typed nil) when unavailable.
func setupSecretK8sSync(cfg config.Config, tenantCells secretsync.TenantCellLookup, logger *slog.Logger) secretshttp.SecretK8sSync {
	mgr, err := secretsync.NewSecretK8sManager(
		cfg.MgmtKubeconfig, cfg.MgmtKubeContext,
		cfg.RuntimeKubeconfig, cfg.RuntimeKubeContext,
		domain.DefaultCellID, tenantCells,
	)
	if err != nil {
		logger.Warn("secret K8s manager not available — ESO sync disabled", "error", err)
		return nil
	}
	return mgr
}
