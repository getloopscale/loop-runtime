// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package memory_webhook_worker

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func stubLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func stubDelivery(id, secret string, payload []byte) *Delivery {
	d := &Delivery{
		ID:           id,
		WebhookID:    "mwh-0000111122223333",
		EventType:    "memory.forgotten",
		Payload:      payload,
		AttemptCount: 0,
	}
	if secret != "" {
		d.Secret = sql.NullString{String: secret, Valid: true}
	}
	return d
}

// T5.5 — 200 response → Delivered=true.
func TestDispatch_2xxMarksDelivered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	d := stubDelivery("mwd-aaaa1111bbbb2222", "whsec_secret", []byte(`{"e":"x"}`))
	d.URL = srv.URL
	result := Dispatch(context.Background(), srv.Client(), stubLogger(), d)
	if !result.Delivered {
		t.Errorf("expected Delivered=true on 2xx, got %+v", result)
	}
	if result.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", result.StatusCode)
	}
}

// T5.5 — 500 response → Delivered=false + ErrorMessage populated.
func TestDispatch_5xxMarksFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server is on fire"))
	}))
	defer srv.Close()

	d := stubDelivery("mwd-aaaa1111bbbb2222", "whsec_x", []byte("body"))
	d.URL = srv.URL
	result := Dispatch(context.Background(), srv.Client(), stubLogger(), d)
	if result.Delivered {
		t.Errorf("expected Delivered=false on 5xx")
	}
	if result.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", result.StatusCode)
	}
	if result.ErrorMessage == "" {
		t.Error("ErrorMessage must be populated on non-2xx")
	}
}

// T5.5 — outbound request carries Idempotency-Key: delivery-{id} per D5.
func TestDispatch_SetsIdempotencyKeyHeader(t *testing.T) {
	var seenKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenKey = r.Header.Get("Idempotency-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := stubDelivery("mwd-uniqueid", "whsec_x", []byte("body"))
	d.URL = srv.URL
	_ = Dispatch(context.Background(), srv.Client(), stubLogger(), d)
	if seenKey != "delivery-mwd-uniqueid" {
		t.Errorf("Idempotency-Key = %q, want %q (D5 invariant)", seenKey, "delivery-mwd-uniqueid")
	}
}

// T5.5 — outbound request carries X-LoopZero-Signature in Stripe format when
// the webhook has a secret (D1).
func TestDispatch_SetsSignatureHeaderWhenSecretPresent(t *testing.T) {
	var seenSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenSig = r.Header.Get("X-LoopZero-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	secret := "whsec_verysecret"
	body := []byte(`{"e":"x"}`)
	d := stubDelivery("mwd-1", secret, body)
	d.URL = srv.URL
	_ = Dispatch(context.Background(), srv.Client(), stubLogger(), d)

	if seenSig == "" {
		t.Fatal("X-LoopZero-Signature header missing")
	}
	if !strings.HasPrefix(seenSig, "t=") {
		t.Errorf("signature does not start with t=: %q", seenSig)
	}
	// Parse `t={ts},v1={hex}` and verify HMAC matches.
	parts := strings.Split(seenSig, ",")
	if len(parts) != 2 {
		t.Fatalf("signature must be `t=...,v1=...`, got %q", seenSig)
	}
	tsStr := strings.TrimPrefix(parts[0], "t=")
	hexHMAC := strings.TrimPrefix(parts[1], "v1=")
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		t.Fatalf("timestamp parse: %v", err)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d.", ts)))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if hexHMAC != want {
		t.Errorf("HMAC verification failed: got %s, want %s", hexHMAC, want)
	}
}

// T5.5 — NULL secret (pre-delivery-plan webhook) → signature header
// OMITTED (D1 consequence).
func TestDispatch_OmitsSignatureWhenSecretNull(t *testing.T) {
	var seenSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenSig = r.Header.Get("X-LoopZero-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := stubDelivery("mwd-1", "", []byte("body")) // empty secret → NULL
	d.URL = srv.URL
	_ = Dispatch(context.Background(), srv.Client(), stubLogger(), d)

	if seenSig != "" {
		t.Errorf("expected NO signature header for NULL secret, got %q", seenSig)
	}
}

// T5.5 — outbound carries the event type so consumer can route quickly.
func TestDispatch_SetsEventTypeHeader(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-LoopZero-Event")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := stubDelivery("mwd-1", "whsec_x", []byte("x"))
	d.URL = srv.URL
	_ = Dispatch(context.Background(), srv.Client(), stubLogger(), d)
	if seen != "memory.forgotten" {
		t.Errorf("X-LoopZero-Event = %q, want memory.forgotten", seen)
	}
}

// T5.5 — Body capped at 4 KiB on the consumer's response (audit row size).
func TestDispatch_TruncatesLargeResponseBody(t *testing.T) {
	// Server returns 8 KiB of body to test truncation.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(make([]byte, 8*1024))
	}))
	defer srv.Close()

	d := stubDelivery("mwd-1", "", []byte("x"))
	d.URL = srv.URL
	result := Dispatch(context.Background(), srv.Client(), stubLogger(), d)

	if len(result.Body) > maxResponseBodyForAudit {
		t.Errorf("response body not truncated: %d bytes (cap %d)", len(result.Body), maxResponseBodyForAudit)
	}
}

// T5.5 — Same delivery_id across multiple Dispatch invocations carries the
// same Idempotency-Key header (D5: stable across retries).
func TestDispatch_SameIdempotencyKeyAcrossRetries(t *testing.T) {
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		w.WriteHeader(http.StatusInternalServerError) // forces retry semantics on caller side
	}))
	defer srv.Close()

	d := stubDelivery("mwd-retrying", "whsec_x", []byte("x"))
	d.URL = srv.URL
	for i := 0; i < 3; i++ {
		_ = Dispatch(context.Background(), srv.Client(), stubLogger(), d)
	}
	if len(keys) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(keys))
	}
	for i := 1; i < len(keys); i++ {
		if keys[i] != keys[0] {
			t.Errorf("idempotency-key changed across retries — D5 violation: %q vs %q", keys[i], keys[0])
		}
	}
}
