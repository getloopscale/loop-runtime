// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/loopzero/loopzero/cloud/internal/billing"
)

func TestVerifySignature_Valid(t *testing.T) {
	secret := "whsec_test_secret"
	payload := []byte(`{"id":"evt_123","type":"invoice.paid"}`)
	timestamp := fmt.Sprintf("%d", time.Now().Unix())

	sig := computeSignature(t, timestamp, payload, secret)
	header := fmt.Sprintf("t=%s,v1=%s", timestamp, sig)

	if err := billing.VerifySignature(payload, header, secret); err != nil {
		t.Errorf("valid signature rejected: %v", err)
	}
}

func TestVerifySignature_Invalid(t *testing.T) {
	secret := "whsec_test_secret"
	payload := []byte(`{"id":"evt_123"}`)
	timestamp := fmt.Sprintf("%d", time.Now().Unix())

	header := fmt.Sprintf("t=%s,v1=invalidsignature", timestamp)

	if err := billing.VerifySignature(payload, header, secret); err == nil {
		t.Error("invalid signature should be rejected")
	}
}

func TestVerifySignature_MissingHeader(t *testing.T) {
	if err := billing.VerifySignature([]byte("{}"), "", "secret"); err == nil {
		t.Error("empty signature header should be rejected")
	}
}

func TestVerifySignature_ExpiredTimestamp(t *testing.T) {
	secret := "whsec_test_secret"
	payload := []byte(`{"id":"evt_123"}`)
	oldTimestamp := fmt.Sprintf("%d", time.Now().Add(-10*time.Minute).Unix())

	sig := computeSignature(t, oldTimestamp, payload, secret)
	header := fmt.Sprintf("t=%s,v1=%s", oldTimestamp, sig)

	if err := billing.VerifySignature(payload, header, secret); err == nil {
		t.Error("expired timestamp should be rejected")
	}
}

func TestVerifySignature_299sAccepted(t *testing.T) {
	// Stripe recommends 300s tolerance — 299s should be accepted
	secret := "whsec_test_secret"
	payload := []byte(`{"id":"evt_299"}`)
	ts := fmt.Sprintf("%d", time.Now().Add(-299*time.Second).Unix())

	sig := computeSignature(t, ts, payload, secret)
	header := fmt.Sprintf("t=%s,v1=%s", ts, sig)

	if err := billing.VerifySignature(payload, header, secret); err != nil {
		t.Errorf("299s-old timestamp should be accepted: %v", err)
	}
}

func TestVerifySignature_301sRejected(t *testing.T) {
	// Beyond 300s tolerance — should be rejected
	secret := "whsec_test_secret"
	payload := []byte(`{"id":"evt_301"}`)
	ts := fmt.Sprintf("%d", time.Now().Add(-301*time.Second).Unix())

	sig := computeSignature(t, ts, payload, secret)
	header := fmt.Sprintf("t=%s,v1=%s", ts, sig)

	if err := billing.VerifySignature(payload, header, secret); err == nil {
		t.Error("301s-old timestamp should be rejected")
	}
}

func TestWebhookHandlerNilDB(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := billing.NewWebhookHandler(nil, "secret", logger)
	event := billing.StripeEvent{
		ID:   "evt_test",
		Type: "customer.subscription.updated",
		Data: json.RawMessage(`{"object":{"id":"sub_123","customer":"cus_123","status":"active","current_period_start":1712000000,"current_period_end":1714678400}}`),
	}
	// Should not panic with nil DB
	if err := handler.ProcessEvent(event); err != nil {
		t.Errorf("ProcessEvent with nil DB should not error: %v", err)
	}
}

func TestPlanDisplayName(t *testing.T) {
	tests := []struct {
		plan string
		want string
	}{
		{"starter", "Starter"},
		{"pro", "Pro"},
		{"team", "Team"},
		{"enterprise", "Enterprise"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		got := billing.PlanDisplayName(tt.plan)
		if got != tt.want {
			t.Errorf("PlanDisplayName(%q) = %q, want %q", tt.plan, got, tt.want)
		}
	}
}

func TestPlanConfig_PriceIDForPlan(t *testing.T) {
	cfg := billing.PlanConfig{
		StarterPriceID: "price_starter",
		ProPriceID:     "price_pro",
		TeamPriceID:    "price_team",
	}

	if got := cfg.PriceIDForPlan("starter"); got != "price_starter" {
		t.Errorf("got %q, want price_starter", got)
	}
	if got := cfg.PriceIDForPlan("pro"); got != "price_pro" {
		t.Errorf("got %q, want price_pro", got)
	}
	if got := cfg.PriceIDForPlan("enterprise"); got != "" {
		t.Errorf("enterprise should return empty, got %q", got)
	}
}

func TestStripeClient_IdempotencyKey(t *testing.T) {
	var receivedKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("Idempotency-Key")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"cus_test","email":"test@test.com"}`))
	}))
	defer srv.Close()

	client := billing.NewStripeClientWithURL("sk_test", srv.URL)
	_, err := client.CreateCustomer("test@test.com", "tn-test", "Test User")
	if err != nil {
		t.Fatalf("CreateCustomer failed: %v", err)
	}
	if receivedKey == "" {
		t.Error("Idempotency-Key header not sent")
	}
	if !strings.Contains(receivedKey, "tn-test") {
		t.Errorf("Idempotency-Key should contain tenant ID, got %q", receivedKey)
	}
}

func TestBillingProvisionerNilDB(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	p := billing.NewProvisioner(nil, nil, logger)

	// Should not panic with nil DB — no-op
	err := p.ProvisionStarter(context.Background(), "user-123", "test@test.com", "tn-test")
	if err != nil {
		t.Errorf("ProvisionStarter with nil DB should not error: %v", err)
	}
}

func computeSignature(t *testing.T, timestamp string, payload []byte, secret string) string {
	t.Helper()
	signedPayload := fmt.Sprintf("%s.%s", timestamp, string(payload))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedPayload))
	return hex.EncodeToString(mac.Sum(nil))
}
