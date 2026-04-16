// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIncTokenExchange(t *testing.T) {
	// Should not panic
	IncTokenExchange(true)
	IncTokenExchange(false)
}

func TestIncDeviceFlow(t *testing.T) {
	IncDeviceFlow(true)
	IncDeviceFlow(false)
}

func TestObserveTenantProvisioning(t *testing.T) {
	ObserveTenantProvisioning(true, 150*time.Millisecond)
	ObserveTenantProvisioning(false, 3*time.Second)
}

func TestIncAuthRejection(t *testing.T) {
	IncAuthRejection("expired")
	IncAuthRejection("invalid")
	IncAuthRejection("no_tenant")
	IncAuthRejection("unknown_reason") // should not panic
}

func TestIncWebhook(t *testing.T) {
	IncWebhook(true)
	IncWebhook(false)
}

func TestIncCheckout(t *testing.T) {
	IncCheckout(true)
	IncCheckout(false)
}

func TestIncUsageDrift(t *testing.T) {
	IncUsageDrift()
}

func TestIncSubscriptionFallback(t *testing.T) {
	IncSubscriptionFallback()
}

func TestObserveHTTPRequest(t *testing.T) {
	ObserveHTTPRequest("GET", "/health", "200", 5*time.Millisecond)
	ObserveHTTPRequest("POST", "/v1/auth/device/authorize", "200", 120*time.Millisecond)
}

func TestHandler_ReturnsPrometheusFormat(t *testing.T) {
	// Fire some metrics so there is output
	IncTokenExchange(true)
	IncWebhook(true)

	h := Handler()
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()

	// Prometheus exposition format contains HELP and TYPE lines
	if !strings.Contains(body, "loopzero_auth_token_exchanges_total") {
		t.Error("missing loopzero_auth_token_exchanges_total in /metrics output")
	}
	if !strings.Contains(body, "loopzero_webhook_events_total") {
		t.Error("missing loopzero_webhook_events_total in /metrics output")
	}
	if !strings.Contains(body, "loopzero_cloud_http_request_duration_seconds") {
		t.Error("missing loopzero_cloud_http_request_duration_seconds in /metrics output")
	}
	if !strings.Contains(body, "loopzero_tenant_provisioning_duration_seconds") {
		t.Error("missing loopzero_tenant_provisioning_duration_seconds in /metrics output")
	}
}
