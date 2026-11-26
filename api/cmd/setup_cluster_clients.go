// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"context"
	"log/slog"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/getloopscale/loop-runtime/api/internal/builds/buildsadapter"
	"github.com/getloopscale/loop-runtime/api/internal/builds/stepwatch"
	"github.com/getloopscale/loop-runtime/api/internal/config"
	"github.com/getloopscale/loop-runtime/api/internal/k8s"
	"github.com/getloopscale/loop-runtime/api/internal/sse"
)

// Cluster clients the API server wires outside the background loop graph:
// the build-cluster Argo step watcher and the mgmt-cluster dynamic client.

// buildMgmtDynamicClient builds a dynamic client for the mgmt cluster through
// k8s.RestConfig (in-cluster first, so the loop-runtime-api ServiceAccount's RBAC on
// runtime.getloopscale.com/tenants applies; then the CORE kubeconfig/context). CellPinner uses
// it to patch tenant.spec.cellID after the DB update; the readiness checker
// and the Argo app label reader use it too.
//
// Returns nil on failure (logged): the pinner then runs in DB-only mode and
// logs a manual kubectl patch hint when a CR patch is skipped.
func buildMgmtDynamicClient(cfg config.Config, logger *slog.Logger) dynamic.Interface {
	restCfg, err := k8s.RestConfig(cfg.MgmtKubeconfig, cfg.MgmtKubeContext)
	if err != nil {
		logger.Warn("mgmt dynamic client: no cluster config, CR patches disabled", "error", err)
		return nil
	}
	dyn, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		logger.Warn("mgmt dynamic client: dynamic.NewForConfig failed, CR patches disabled", "error", err)
		return nil
	}
	return dyn
}

// startArgoStepWatcher polls Argo Workflows on the build cluster every 2s, extracts Pod node
// phase transitions, and emits build.step.{started,succeeded,failed}
// events to the SSE journal (sse_events table). Without this goroutine
// the dashboard's BuildTimelineLive shows zero step events even though
// the workflow is running — the LIVE bug caught 2026-06-06 J-17 dogfood
// (image loop-runtime-api:j17-iter1 missed this wiring on first roll-out).
//
// Failure modes are non-fatal: the watcher logs + skips a tick. EC-1
// surfaces 401/403 to the dedicated counter so Grafana alerts fire
// before silent token expiry breaks visibility.
//
// Lifecycle: started before the HTTP server, cancelled at shutdown.
func startArgoStepWatcher(ctx context.Context, cfg config.Config, eventStore sse.EventStore, logger *slog.Logger) {
	if eventStore == nil {
		logger.Warn("argo step watcher: event store nil, skipping")
		return
	}

	// Build a dynamic client connected to the build cluster, mirroring the
	// pattern in argo.NewWorkflowSubmitter. clientcmd handles in-cluster
	// SA token + ExplicitPath kubeconfig for local dev.
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: cfg.BuildKubeconfig}
	overrides := &clientcmd.ConfigOverrides{}
	if cfg.BuildKubeContext != "" {
		overrides.CurrentContext = cfg.BuildKubeContext
	}
	restCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		logger.Warn("argo step watcher: build cluster config failed, watcher disabled",
			"error", err)
		return
	}

	// Issue #242 fix 3 — pick up a rotated build-cluster credential without a
	// pod restart.
	//
	// The token resolved above is a VALUE captured at process start. When the
	// operator rotates `loop-runtime-cluster-kubeconfigs.build.kubeconfig`, the mounted
	// file changes and this client does not: every poll keeps authenticating
	// with a credential that was already replaced, and the only recovery is
	// deleting the pod. The watcher already noticed (`ReportWatchError` → the
	// 401/403 log + ArgoWatcherTokenExpiredTotal); it had no way to act on it.
	//
	// Wired ONLY when the kubeconfig is a real file. In-cluster (empty path)
	// client-go already refreshes the projected SA token natively — wrapping
	// there would replace a working mechanism with a weaker one.
	if cfg.BuildKubeconfig != "" {
		restCfg.WrapTransport = k8s.NewReloadingTransportWrapper(cfg.BuildKubeconfig, cfg.BuildKubeContext)
		// The wrapper owns the Authorization header. Left set, client-go also
		// installs its own bearer round-tripper carrying the STARTUP token, and
		// whichever writes the header last wins — reintroducing exactly the
		// staleness this removes.
		restCfg.BearerToken = ""
		restCfg.BearerTokenFile = ""
		logger.Info("argo step watcher: build-cluster token reloads on auth failure",
			"kubeconfig", cfg.BuildKubeconfig)
	}
	dyn, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		logger.Warn("argo step watcher: dynamic.NewForConfig failed, watcher disabled",
			"error", err)
		return
	}

	// Sprint 8 plan qa-sprint8-cleanup-2026-06-25 T2.1 (closes loop-console#37):
	// scope the workflow watcher to the `argo` namespace specifically. The
	// pre-Sprint-8 empty namespace `""` was interpreted by kubernetes as
	// cluster-scope LIST, which the Sprint 7 RBAC fix (namespace-scoped Role
	// for argo/loop-runtime-workflow-submitter) does NOT grant. The watcher then 403'd
	// every poll → couldn't observe workflow state → reconcile loop blocked
	// → release stuck in SYNC_FAILED → `active_release: null` (#37 root cause).
	// All LoopRuntime Workflow CRs are created in the `argo` namespace by the build
	// orchestrator (workflow_submitter.go); the pre-Sprint-8 comment about
	// per-tenant namespaces was incorrect — workflows have ALWAYS lived in
	// argo, only the build PODS run per-tenant. KISS + least privilege.
	source := buildsadapter.NewK8sWorkflowNodeSource(dyn, "argo", 2*time.Second, logger)
	// terminalIDs is nil — first-tick startup may emit duplicate step events
	// for already-running workflows; the watcher's internal lastPhase dedup
	// catches duplicates from tick 2 onwards. A future enhancement can wire
	// a BuildStore-backed TerminalBuildIDStore for cleaner cold start.
	watcher := stepwatch.NewArgoStepWatcher(source, eventStore, nil, logger)
	source.WithWatchErrorHandler(watcher.ReportWatchError)

	go func() {
		if err := watcher.Start(ctx); err != nil && ctx.Err() == nil {
			logger.Warn("argo step watcher: stopped unexpectedly", "error", err)
		}
	}()
}
