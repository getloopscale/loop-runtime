// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

import (
	"context"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/operators/internal/metrics"
)

// misplacedTenantScanInterval is how often the scan re-reads the cluster.
// The MisplacedTenantDetected alert waits 5 minutes, so one scan per window
// is enough to keep it current.
const misplacedTenantScanInterval = 5 * time.Minute

// MisplacedTenantScanner surfaces Tenant CRs outside the canonical namespace
// (ADR-0018 C2/C5). The tenant-operator's cache is scoped to loopzero-system, so
// such a Tenant is invisible to every reconciler; this scan reads the cluster
// uncached, at start and then periodically, and publishes the count per
// namespace in loopzero_misplaced_tenant_total — the series the
// MisplacedTenantDetected alert queries.
type MisplacedTenantScanner struct {
	// Reader must be uncached (mgr.GetAPIReader()): the cache cannot see
	// outside loopzero-system, which is the whole point.
	Reader   client.Reader
	Interval time.Duration
}

// NeedLeaderElection keeps the scan on the leader only.
func (s *MisplacedTenantScanner) NeedLeaderElection() bool { return true }

// Start runs the scan until ctx ends. A failed scan is logged and retried on
// the next tick; it never stops the manager.
func (s *MisplacedTenantScanner) Start(ctx context.Context) error {
	interval := s.Interval
	if interval <= 0 {
		interval = misplacedTenantScanInterval
	}
	logger := log.FromContext(ctx).WithName("misplaced-tenant-scan")
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := scanMisplacedTenants(ctx, s.Reader); err != nil {
			logger.Error(err, "misplaced Tenant scan failed; the previous reading stays until the next scan")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// scanMisplacedTenants lists every Tenant in the cluster and publishes how
// many sit outside constants.NamespacePlatformSystem, per namespace. A failed
// List leaves the previous reading in place: zeroing it would clear a firing
// alert because the scan could not see, not because anything was fixed.
func scanMisplacedTenants(ctx context.Context, reader client.Reader) error {
	var tenants loopzerov1.TenantList
	if err := reader.List(ctx, &tenants); err != nil {
		return fmt.Errorf("list Tenants cluster-wide: %w", err)
	}
	perNamespace := map[string]int{}
	for _, t := range tenants.Items {
		if t.Namespace != constants.NamespacePlatformSystem {
			perNamespace[t.Namespace]++
		}
	}
	metrics.MisplacedTenantTotal.Reset()
	for ns, n := range perNamespace {
		metrics.MisplacedTenantTotal.WithLabelValues(ns).Set(float64(n))
	}
	return nil
}
