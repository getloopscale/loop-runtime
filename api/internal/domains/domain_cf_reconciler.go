// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package domains is the custom-domain feature (CF zone/DNS/TLS): provisioning, health checking
// and Cloudflare reconciliation. Its handlers live in domainshttp and its Cloudflare zone client
// in domains/cfzone.
package domains

import (
	"context"
	"log/slog"
	"time"
)

// CFZoneReconcilerStore reads CF-managed domains from the database.
type CFZoneReconcilerStore interface {
	FindActiveCFDomains(ctx context.Context) ([]CFDomainInfo, error)
	UpdateCFZoneStatus(ctx context.Context, domain, cfZoneStatus string) error
}

// CFDomainInfo is the minimal info needed for CF zone reconciliation.
type CFDomainInfo struct {
	Domain       string
	CFZoneID     string
	CFZoneStatus string
}

// CFZoneStatusReader checks the real CF zone status.
type CFZoneStatusReader interface {
	GetZoneStatus(ctx context.Context, zoneID string) (string, error)
}

// DomainCFReconciler periodically verifies that CF zone status matches database state.
// Detects drift: zone deleted externally, zone suspended, etc.
// MEET-2026-04-09-044 P1.
type DomainCFReconciler struct {
	store    CFZoneReconcilerStore
	cfClient CFZoneStatusReader
	logger   *slog.Logger
	interval time.Duration
}

// NewDomainCFReconciler creates a new CF zone reconciler.
func NewDomainCFReconciler(store CFZoneReconcilerStore, cfClient CFZoneStatusReader, logger *slog.Logger, interval time.Duration) *DomainCFReconciler {
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	return &DomainCFReconciler{store: store, cfClient: cfClient, logger: logger, interval: interval}
}

// Start runs the reconciler loop.
func (r *DomainCFReconciler) Start(ctx context.Context) {
	r.logger.Info("domain CF reconciler started", "interval", r.interval)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("domain CF reconciler stopped")
			return
		case <-ticker.C:
			r.reconcile(ctx)
		}
	}
}

func (r *DomainCFReconciler) reconcile(ctx context.Context) {
	domains, err := r.store.FindActiveCFDomains(ctx)
	if err != nil {
		r.logger.Error("domain CF reconciler: query failed", "error", err)
		return
	}

	for _, d := range domains {
		if ctx.Err() != nil {
			return
		}
		realStatus, err := r.cfClient.GetZoneStatus(ctx, d.CFZoneID)
		if err != nil {
			r.logger.Warn("domain CF reconciler: zone check failed", "domain", d.Domain, "error", err)
			continue
		}
		if realStatus != d.CFZoneStatus {
			r.logger.Warn("domain CF reconciler: drift detected",
				"domain", d.Domain, "db_status", d.CFZoneStatus, "cf_status", realStatus)
			if err := r.store.UpdateCFZoneStatus(ctx, d.Domain, realStatus); err != nil {
				r.logger.Error("domain CF reconciler: update failed", "domain", d.Domain, "error", err)
			}
		}
	}
}
