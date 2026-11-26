// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Cloudflare KV metrics — emitted by cfkv.Client on every Put/Delete.
var (
	CFKVOperationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loop_runtime_cfkv_operations_total",
		Help: "Total Cloudflare KV operations by type and result.",
	}, []string{"op", "result"}) // op: "put"/"delete", result: "success"/"failure"

	CFKVOperationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loop_runtime_cfkv_operation_duration_seconds",
		Help:    "Duration of Cloudflare KV operations.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5},
	}, []string{"op"})

	CFKVDriftDetected = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loop_runtime_cfkv_drift_detected",
		Help: "Set to 1 when KV state diverges from database state. 0 when reconciled.",
	})
)

// Custom domain metrics — MEET-2026-04-05-033.
// Emitted by DomainHandler, DomainProvisioner, and DomainHealthChecker.
var (
	CustomDomainsTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loop_runtime_custom_domains_total",
		Help: "Number of custom domains by status.",
	}, []string{"tenant", "status"})

	DomainVerificationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loop_runtime_domain_verification_duration_seconds",
		Help:    "Duration of DNS TXT verification checks.",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
	}, []string{"result"})

	DomainProvisioningDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loop_runtime_domain_provisioning_duration_seconds",
		Help:    "Duration from domain verification to active state.",
		Buckets: []float64{10, 30, 60, 120, 300, 600},
	}, []string{"result"})

	// Cardinality cap (audit gap M08 #19): the `tenant` label was removed
	// to bound series count at N (domains) instead of N×M (domains×tenants).
	// Tenant attribution remains forensically recoverable by joining on
	// `loop_runtime_custom_domains_total{tenant=...}` or querying audit_log by
	// domain. Operators that previously consumed `tenant` in alert
	// descriptions are migrated below to reference the audit trail.
	DomainTLSExpiryDays = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loop_runtime_domain_tls_expiry_days",
		Help: "Days until TLS certificate expires for a custom domain.",
	}, []string{"domain"})

	DomainVerificationFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loop_runtime_domain_verification_failures_total",
		Help: "Total DNS verification failures.",
	}, []string{"tenant"})

	DomainCNAMECheckFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loop_runtime_domain_cname_check_failures_total",
		Help: "Total CNAME re-verification failures for active domains.",
	}, []string{"tenant"})

	DomainHealthCheckerErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loop_runtime_domain_health_checker_errors_total",
		Help: "Total errors in the domain health checker by check type.",
	}, []string{"check"})

	// v2: CF Zone Management metrics
	DomainCFZoneErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loop_runtime_domain_cf_zone_errors_total",
		Help: "Total errors from Cloudflare Zone API.",
	}, []string{"operation"})

	DomainNSPropagationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loop_runtime_domain_ns_propagation_duration_seconds",
		Help:    "Duration from domain creation to NS propagation (pending_ns → ns_active).",
		Buckets: []float64{60, 300, 900, 1800, 3600, 7200, 14400, 43200, 86400},
	}, []string{"result"})
)
