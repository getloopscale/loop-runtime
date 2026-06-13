// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package metrics provides Prometheus-native observability for the cloud service.
// §4.1 + §4.2: Auth and billing metrics exposed via /metrics endpoint.
//
// Migrated from manual sync/atomic counters to prometheus/client_golang.
// The public API (Inc*, Observe* functions) is preserved for all call sites.
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// Auth metrics (§4.1)
	tokenExchanges = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_auth_token_exchanges_total",
		Help: "Token exchanges by result.",
	}, []string{"result"})

	deviceFlows = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_auth_device_flow_total",
		Help: "Device flow completions.",
	}, []string{"result"})

	// Plan 2026-06-05-dashboard-journey-j05-device-success T3.1 D4:
	// Browser-side success signal — incremented when /v1/telemetry/events
	// receives event=device_success_viewed from the dashboard beacon.
	// Powers SLO "CLI auth completion rate" (ratio against successful
	// device-flow token grants in `loopzero_auth_device_flow_total{result=success}`).
	deviceFlowBrowserSuccess = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "loopzero_device_flow_browser_success_total",
		Help: "Device-flow consent confirmations rendered in the dashboard.",
	})

	tenantProvisioning = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_tenant_provisioning_total",
		Help: "Tenant provisioning operations.",
	}, []string{"result"})

	tenantProvisioningDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "loopzero_tenant_provisioning_duration_seconds",
		Help:    "Tenant provisioning duration in seconds.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
	})

	authRejections = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_auth_rejections_total",
		Help: "Auth rejections by reason.",
	}, []string{"reason"})

	// Billing metrics (§4.2)
	webhookEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_webhook_events_total",
		Help: "Webhook events by status.",
	}, []string{"status"})

	checkoutOps = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_billing_checkout_total",
		Help: "Checkout operations.",
	}, []string{"result"})

	usageDrift = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "loopzero_usage_drift_detected_total",
		Help: "Usage drift detection events.",
	})

	subscriptionFallback = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "loopzero_subscription_fallback_total",
		Help: "Subscription missing -- defaulted to starter.",
	})

	// Plan J-09 T1.1 (2026-06-05): GitHub App install + update callback counter.
	// Labelled by setup_action (install | update) so SLOs can distinguish
	// fresh provisioning from re-permission flows.
	githubAppInstallations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_github_app_installations_total",
		Help: "GitHub App install callback completions by setup_action.",
	}, []string{"setup_action"})

	// HTTP metrics (added with OTel instrumentation)
	httpRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopzero_cloud_http_request_duration_seconds",
		Help:    "HTTP request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path", "status_code"})
)

func init() {
	prometheus.MustRegister(
		tokenExchanges,
		deviceFlows,
		deviceFlowBrowserSuccess,
		tenantProvisioning,
		tenantProvisioningDuration,
		authRejections,
		webhookEvents,
		checkoutOps,
		usageDrift,
		subscriptionFallback,
		githubAppInstallations,
		httpRequestDuration,
	)
}

// IncTokenExchange increments token exchange counter.
func IncTokenExchange(success bool) {
	if success {
		tokenExchanges.WithLabelValues("success").Inc()
	} else {
		tokenExchanges.WithLabelValues("error").Inc()
	}
}

// IncDeviceFlow increments device flow counter.
func IncDeviceFlow(success bool) {
	if success {
		deviceFlows.WithLabelValues("success").Inc()
	} else {
		deviceFlows.WithLabelValues("error").Inc()
	}
}

// IncDeviceFlowBrowserSuccess increments when the dashboard renders the
// /device-success confirmation page (Plan J-05 T3.1 D4). Caller MUST gate
// this on the parsed event being `device_success_viewed` (EC-3 ordering
// invariant: never increment on rejected payloads).
func IncDeviceFlowBrowserSuccess() {
	deviceFlowBrowserSuccess.Inc()
}

// ObserveTenantProvisioning records a tenant provisioning operation.
func ObserveTenantProvisioning(success bool, duration time.Duration) {
	tenantProvisioningDuration.Observe(duration.Seconds())
	if success {
		tenantProvisioning.WithLabelValues("success").Inc()
	} else {
		tenantProvisioning.WithLabelValues("error").Inc()
	}
}

// IncAuthRejection increments auth rejection counter by reason.
func IncAuthRejection(reason string) {
	authRejections.WithLabelValues(reason).Inc()
}

// IncWebhook increments webhook counter.
func IncWebhook(success bool) {
	if success {
		webhookEvents.WithLabelValues("processed").Inc()
	} else {
		webhookEvents.WithLabelValues("failed").Inc()
	}
}

// IncCheckout increments checkout counter.
func IncCheckout(success bool) {
	if success {
		checkoutOps.WithLabelValues("started").Inc()
	} else {
		checkoutOps.WithLabelValues("error").Inc()
	}
}

// IncUsageDrift increments usage drift counter.
func IncUsageDrift() {
	usageDrift.Inc()
}

// IncSubscriptionFallback increments when plan resolution falls back to starter (S1.4).
func IncSubscriptionFallback() {
	subscriptionFallback.Inc()
}

// IncGitHubAppInstallation increments the GitHub App callback counter
// (Plan J-09 T1.1). setupAction MUST be one of "install" or "update";
// the callback handler normalises the empty string to "install" before
// invoking this (matches GitHub's documented default for first-time
// installs). Unknown values are still recorded so dashboards surface
// unexpected GitHub behaviour without losing the event.
func IncGitHubAppInstallation(setupAction string) {
	if setupAction == "" {
		setupAction = "install"
	}
	githubAppInstallations.WithLabelValues(setupAction).Inc()
}

// ObserveHTTPRequest records an HTTP request duration.
func ObserveHTTPRequest(method, path, statusCode string, duration time.Duration) {
	httpRequestDuration.WithLabelValues(method, path, statusCode).Observe(duration.Seconds())
}

// Handler returns an HTTP handler that emits metrics in Prometheus exposition format.
func Handler() http.HandlerFunc {
	h := promhttp.Handler()
	return func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
	}
}
