// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
)

// KVRoutingSource provides the expected set of KV routing keys from the database.
// Each key is a subdomain (e.g., "tn-loopzero-project-web-production").
type KVRoutingSource interface {
	// ExpectedStaticKeys returns the set of KV keys that should exist based on
	// active static builds in the database.
	ExpectedStaticKeys(ctx context.Context) (map[string]bool, error)
}

// KVKeyLister lists the keys present in the edge routing KV namespace;
// *cfkv.Client implements it.
type KVKeyLister interface {
	ListKeys(ctx context.Context) ([]string, error)
}

// KVReconciler periodically compares database state against Cloudflare KV state.
// V1: detection only — logs drift and emits metrics. Does NOT auto-correct.
type KVReconciler struct {
	kvClient KVKeyLister
	source   KVRoutingSource
	logger   *slog.Logger
	interval time.Duration
}

// NewKVReconciler creates a reconciler that checks for KV drift every interval.
func NewKVReconciler(kvClient KVKeyLister, source KVRoutingSource, logger *slog.Logger, interval time.Duration) *KVReconciler {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &KVReconciler{
		kvClient: kvClient,
		source:   source,
		logger:   logger,
		interval: interval,
	}
}

// Start runs the reconciler until the context is cancelled. Leader-only.
func (r *KVReconciler) Start(ctx context.Context) {
	r.logger.Info("kv reconciler started", "interval", r.interval)

	// Run immediately on start
	r.reconcile(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("kv reconciler stopped")
			return
		case <-ticker.C:
			r.reconcile(ctx)
		}
	}
}

func (r *KVReconciler) reconcile(ctx context.Context) {
	if r.kvClient == nil {
		return // not configured (local dev)
	}

	expected, err := r.source.ExpectedStaticKeys(ctx)
	if err != nil {
		r.logger.Error("kv reconciler: failed to get expected keys from DB", "error", err)
		return
	}

	actual, err := r.kvClient.ListKeys(ctx)
	if err != nil {
		r.logger.Error("kv reconciler: failed to list KV keys", "error", err)
		return
	}

	// Build the set of actual keys, leaving out the edge's cell routing, which shares
	// this store and is not static delivery's (#448).
	actualSet := make(map[string]bool, len(actual))
	for _, key := range actual {
		if isEdgeRoutingKey(key) {
			continue
		}
		actualSet[key] = true
	}

	// Detect missing: expected in DB but not in KV
	var missing []string
	for key := range expected {
		if !actualSet[key] {
			missing = append(missing, key)
		}
	}

	// Detect extra: present in KV but not in DB
	var extra []string
	for key := range actualSet {
		if !expected[key] {
			extra = append(extra, key)
		}
	}

	driftFound := len(missing) > 0 || len(extra) > 0

	if driftFound {
		metrics.CFKVDriftDetected.Set(1)
		r.logger.Warn("kv reconciler: drift detected",
			"missing_count", len(missing), "extra_count", len(extra),
			"missing", missing, "extra", extra)
	} else {
		metrics.CFKVDriftDetected.Set(0)
		r.logger.Debug("kv reconciler: no drift detected",
			"expected_count", len(expected), "actual_count", len(actualSet))
	}
}

// edgeRoutingKeyPrefixes name the keys the edge's cell routing owns: route:<host> is
// written by the engine's edge publisher and ingress:<cell> by cell provisioning. cell:
// is the retired per-tenant form, ignored so a store still holding one is not drift.
var edgeRoutingKeyPrefixes = []string{"route:", "ingress:", "cell:"}

func isEdgeRoutingKey(key string) bool {
	for _, p := range edgeRoutingKeyPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}
