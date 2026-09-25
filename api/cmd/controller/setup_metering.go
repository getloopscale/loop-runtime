// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

// setup_metering.go — the composition root for the compute-metering producer
// (plan metering-compute-loop T3.2 S5b). Wires the tested collaborators
// (metering.NewWorkerLoop + SecretCellClientProvider + the dedicated worker pool)
// into a leader-gated background loop.
//
// OBSERVE-ONLY, NON-FATAL: metering is an observe-only producer (no enforcement,
// no 402). Any wiring failure here logs + returns — metering is skipped, and the
// controller's core reconcile loops keep running. A metering wiring problem must
// NEVER os.Exit the controller (unlike setupCellRouter, which is fatal-by-design
// for the API's routing).

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/k8s"
	"github.com/getloopscale/loopzero/api/internal/leader"
	"github.com/getloopscale/loopzero/api/internal/metering"
	apiv1 "github.com/getloopscale/loopzero/operators/api/v1"
)

const (
	// meteringInterval drives the loop ticker. Alerts key off 3×interval freshness.
	meteringInterval = 60 * time.Second
	// meteringMaxDelta clamps a stale-cursor catch-up (ADR-8 / SC-7). MUST be
	// shorter than one billing period — a value ≥ one period could span two
	// boundaries the split writer does not handle.
	meteringMaxDelta = time.Hour
	// meteringWorkerPoolConns bounds the dedicated worker pool (ADR-10 / EC-11).
	meteringWorkerPoolConns = 4
	// meteringMaxConcurrency bounds the per-tenant fan-out — kept ≤ the pool's
	// MaxConns so the pool is never starved.
	meteringMaxConcurrency = meteringWorkerPoolConns
)

// startMeteringLoop wires + starts the compute-metering loop on the elected
// leader, registered on bgWg. Observe-only + non-fatal (see file header).
func startMeteringLoop(shutdownCtx context.Context, bgWg *sync.WaitGroup, cfg config.Config, lease leader.Lease, logger *slog.Logger) {
	// Dedicated bounded worker pool (ADR-10) — physically separate from the API
	// pool, connected as loopzero_worker so the SET LOCAL app.tenant_id='' RLS bypass
	// in the money-writer is load-bearing, not incidental.
	poolCfg, err := pgxpool.ParseConfig(cfg.EffectiveWorkerDatabaseURL())
	if err != nil {
		logger.Error("metering: parse worker DSN failed — metering disabled (observe-only, non-fatal)", "error", err)
		return
	}
	poolCfg.MaxConns = meteringWorkerPoolConns
	pool, err := pgxpool.NewWithConfig(shutdownCtx, poolCfg)
	if err != nil {
		logger.Error("metering: worker pool init failed — metering disabled (observe-only, non-fatal)", "error", err)
		return
	}

	// Mgmt-cluster client: reads Tenant CRs (enumeration) + ClusterCredential CRs
	// and their kubeconfig Secrets (per-cell pod listing). The scheme carries BOTH
	// the loopzero.io CRD types AND corev1 (for the kubeconfig Secret GET) — the k8s
	// package's NewScheme is loopzero.io-only, so this client is built here.
	mgmtClient, err := meteringMgmtClient(cfg)
	if err != nil {
		pool.Close()
		logger.Error("metering: mgmt client init failed — metering disabled (observe-only, non-fatal)", "error", err)
		return
	}

	cellProvider := metering.NewSecretCellClientProvider(mgmtClient, tenantCRNamespace)
	loop := metering.NewWorkerLoop(pool, mgmtClient, cellProvider, metering.LoopConfig{
		Interval:       meteringInterval,
		MaxDelta:       meteringMaxDelta,
		MaxConcurrency: meteringMaxConcurrency,
		Lease:          lease,
	})

	bgWg.Add(1)
	go func() {
		defer bgWg.Done()
		defer pool.Close()
		logger.Info("metering: compute-metering loop started (observe-only)",
			"interval", meteringInterval.String(),
			"cell", lease.CellID,
			"generation", lease.GenerationID,
		)
		loop.Run(shutdownCtx)
	}()
}

// tenantCRNamespace is the canonical namespace for Tenant + ClusterCredential CRs
// (ADR-0018 — mirrors k8s.tenantNamespace, which is unexported).
const tenantCRNamespace = "loopzero-system"

// meteringMgmtClient builds a controller-runtime client for the mgmt cluster whose
// scheme includes the loopzero.io CRD types (Tenant, ClusterCredential) AND corev1
// (the kubeconfig Secret). In-cluster config first, then the kubeconfig + context
// (k8s.RestConfig).
func meteringMgmtClient(cfg config.Config) (ctrlclient.Client, error) {
	restCfg, err := k8s.RestConfig(cfg.MgmtKubeconfig, cfg.MgmtKubeContext)
	if err != nil {
		return nil, err
	}
	scheme := runtime.NewScheme()
	if err := apiv1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("metering: register loopzero.io scheme: %w", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("metering: register corev1 scheme: %w", err)
	}
	c, err := ctrlclient.New(restCfg, ctrlclient.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("metering: build mgmt client: %w", err)
	}
	return c, nil
}
