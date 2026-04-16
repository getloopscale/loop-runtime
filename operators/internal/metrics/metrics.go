// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package metrics provides custom Prometheus metrics for operator controllers.
// All metrics follow the loopzero_operator_ prefix convention for Grafana dashboards.
package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	// ReconcileDuration measures the duration of each reconcile loop in seconds.
	ReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "loopzero_operator_reconcile_duration_seconds",
			Help:    "Duration of reconcile loops by controller and result.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"controller", "result"},
	)

	// ReconcileErrors counts reconcile errors by controller and error type.
	ReconcileErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "loopzero_operator_reconcile_errors_total",
			Help: "Total number of reconcile errors by controller and error type.",
		},
		[]string{"controller", "error_type"},
	)

	// ReconcileTotal counts total reconcile invocations by controller and result.
	ReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "loopzero_operator_reconcile_total",
			Help: "Total number of reconcile invocations by controller and result (success/error/requeue).",
		},
		[]string{"controller", "result"},
	)

	// ProvisionerDuration measures the duration of provisioner operations in seconds.
	ProvisionerDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "loopzero_operator_provisioner_duration_seconds",
			Help:    "Duration of provisioner operations (gitea, argocd, harbor, minio).",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"provisioner", "operation"},
	)

	// CRDCount tracks the number of CRD instances by type (set during reconcile).
	CRDCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "loopzero_operator_crd_count",
			Help: "Current count of CRD instances by type.",
		},
		[]string{"crd_type"},
	)
)

func init() {
	prometheus.MustRegister(
		ReconcileDuration,
		ReconcileErrors,
		ReconcileTotal,
		ProvisionerDuration,
		CRDCount,
	)
}
