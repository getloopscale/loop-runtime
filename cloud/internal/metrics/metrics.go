// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package metrics provides lightweight observability counters for the cloud service.
// §4.1 + §4.2: Auth and billing metrics exposed via /metrics endpoint.
//
// Uses sync/atomic counters (no external dependency). Prometheus scrapes via /metrics
// endpoint that emits text format. When the cloud service adds prometheus/client_golang
// as a dependency, these can be migrated to promauto.NewCounter.
package metrics

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// Auth metrics (§4.1)
var (
	TokenExchangeSuccess int64
	TokenExchangeError   int64
	DeviceFlowSuccess    int64
	DeviceFlowError      int64
	TenantProvSuccess    int64
	TenantProvError      int64
	TenantProvDurationMs int64 // cumulative for average calculation
	TenantProvCount      int64
	AuthRejectExpired    int64
	AuthRejectInvalid    int64
	AuthRejectNoTenant   int64
)

// Billing metrics (§4.2)
var (
	WebhookProcessed      int64
	WebhookFailed         int64
	CheckoutStarted       int64
	CheckoutError         int64
	UsageDriftCount       int64
	SubscriptionFallback  int64 // S1.4: missing subscription row → starter fallback
)

// IncTokenExchange increments token exchange counter.
func IncTokenExchange(success bool) {
	if success {
		atomic.AddInt64(&TokenExchangeSuccess, 1)
	} else {
		atomic.AddInt64(&TokenExchangeError, 1)
	}
}

// IncDeviceFlow increments device flow counter.
func IncDeviceFlow(success bool) {
	if success {
		atomic.AddInt64(&DeviceFlowSuccess, 1)
	} else {
		atomic.AddInt64(&DeviceFlowError, 1)
	}
}

// ObserveTenantProvisioning records a tenant provisioning operation.
func ObserveTenantProvisioning(success bool, duration time.Duration) {
	atomic.AddInt64(&TenantProvCount, 1)
	atomic.AddInt64(&TenantProvDurationMs, duration.Milliseconds())
	if success {
		atomic.AddInt64(&TenantProvSuccess, 1)
	} else {
		atomic.AddInt64(&TenantProvError, 1)
	}
}

// IncAuthRejection increments auth rejection counter by reason.
func IncAuthRejection(reason string) {
	switch reason {
	case "expired":
		atomic.AddInt64(&AuthRejectExpired, 1)
	case "invalid":
		atomic.AddInt64(&AuthRejectInvalid, 1)
	case "no_tenant":
		atomic.AddInt64(&AuthRejectNoTenant, 1)
	}
}

// IncWebhook increments webhook counter.
func IncWebhook(success bool) {
	if success {
		atomic.AddInt64(&WebhookProcessed, 1)
	} else {
		atomic.AddInt64(&WebhookFailed, 1)
	}
}

// IncCheckout increments checkout counter.
func IncCheckout(success bool) {
	if success {
		atomic.AddInt64(&CheckoutStarted, 1)
	} else {
		atomic.AddInt64(&CheckoutError, 1)
	}
}

// IncUsageDrift increments usage drift counter.
func IncUsageDrift() {
	atomic.AddInt64(&UsageDriftCount, 1)
}

// IncSubscriptionFallback increments when plan resolution falls back to starter (S1.4).
func IncSubscriptionFallback() {
	atomic.AddInt64(&SubscriptionFallback, 1)
}

// Handler returns an HTTP handler that emits metrics in Prometheus text format.
func Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		// Auth metrics
		fmt.Fprintf(w, "# HELP loopzero_auth_token_exchanges_total Token exchanges by result.\n")
		fmt.Fprintf(w, "# TYPE loopzero_auth_token_exchanges_total counter\n")
		fmt.Fprintf(w, "loopzero_auth_token_exchanges_total{result=\"success\"} %d\n", atomic.LoadInt64(&TokenExchangeSuccess))
		fmt.Fprintf(w, "loopzero_auth_token_exchanges_total{result=\"error\"} %d\n", atomic.LoadInt64(&TokenExchangeError))

		fmt.Fprintf(w, "# HELP loopzero_auth_device_flow_total Device flow completions.\n")
		fmt.Fprintf(w, "# TYPE loopzero_auth_device_flow_total counter\n")
		fmt.Fprintf(w, "loopzero_auth_device_flow_total{result=\"success\"} %d\n", atomic.LoadInt64(&DeviceFlowSuccess))
		fmt.Fprintf(w, "loopzero_auth_device_flow_total{result=\"error\"} %d\n", atomic.LoadInt64(&DeviceFlowError))

		fmt.Fprintf(w, "# HELP loopzero_tenant_provisioning_total Tenant provisioning operations.\n")
		fmt.Fprintf(w, "# TYPE loopzero_tenant_provisioning_total counter\n")
		fmt.Fprintf(w, "loopzero_tenant_provisioning_total{result=\"success\"} %d\n", atomic.LoadInt64(&TenantProvSuccess))
		fmt.Fprintf(w, "loopzero_tenant_provisioning_total{result=\"error\"} %d\n", atomic.LoadInt64(&TenantProvError))

		count := atomic.LoadInt64(&TenantProvCount)
		durationMs := atomic.LoadInt64(&TenantProvDurationMs)
		avgMs := int64(0)
		if count > 0 {
			avgMs = durationMs / count
		}
		fmt.Fprintf(w, "# HELP loopzero_tenant_provisioning_duration_ms_avg Average provisioning duration in ms.\n")
		fmt.Fprintf(w, "# TYPE loopzero_tenant_provisioning_duration_ms_avg gauge\n")
		fmt.Fprintf(w, "loopzero_tenant_provisioning_duration_ms_avg %d\n", avgMs)

		fmt.Fprintf(w, "# HELP loopzero_auth_rejections_total Auth rejections by reason.\n")
		fmt.Fprintf(w, "# TYPE loopzero_auth_rejections_total counter\n")
		fmt.Fprintf(w, "loopzero_auth_rejections_total{reason=\"expired\"} %d\n", atomic.LoadInt64(&AuthRejectExpired))
		fmt.Fprintf(w, "loopzero_auth_rejections_total{reason=\"invalid\"} %d\n", atomic.LoadInt64(&AuthRejectInvalid))
		fmt.Fprintf(w, "loopzero_auth_rejections_total{reason=\"no_tenant\"} %d\n", atomic.LoadInt64(&AuthRejectNoTenant))

		// Billing metrics
		fmt.Fprintf(w, "# HELP loopzero_webhook_events_total Webhook events by status.\n")
		fmt.Fprintf(w, "# TYPE loopzero_webhook_events_total counter\n")
		fmt.Fprintf(w, "loopzero_webhook_events_total{status=\"processed\"} %d\n", atomic.LoadInt64(&WebhookProcessed))
		fmt.Fprintf(w, "loopzero_webhook_events_total{status=\"failed\"} %d\n", atomic.LoadInt64(&WebhookFailed))

		fmt.Fprintf(w, "# HELP loopzero_billing_checkout_total Checkout operations.\n")
		fmt.Fprintf(w, "# TYPE loopzero_billing_checkout_total counter\n")
		fmt.Fprintf(w, "loopzero_billing_checkout_total{result=\"started\"} %d\n", atomic.LoadInt64(&CheckoutStarted))
		fmt.Fprintf(w, "loopzero_billing_checkout_total{result=\"error\"} %d\n", atomic.LoadInt64(&CheckoutError))

		fmt.Fprintf(w, "# HELP loopzero_usage_drift_detected_total Usage drift detection events.\n")
		fmt.Fprintf(w, "# TYPE loopzero_usage_drift_detected_total counter\n")
		fmt.Fprintf(w, "loopzero_usage_drift_detected_total %d\n", atomic.LoadInt64(&UsageDriftCount))

		fmt.Fprintf(w, "# HELP loopzero_subscription_fallback_total Subscription missing — defaulted to starter.\n")
		fmt.Fprintf(w, "# TYPE loopzero_subscription_fallback_total counter\n")
		fmt.Fprintf(w, "loopzero_subscription_fallback_total %d\n", atomic.LoadInt64(&SubscriptionFallback))
	}
}
