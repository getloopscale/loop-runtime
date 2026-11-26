// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package auth

import (
	"context"
	"errors"
	"fmt"
	"github.com/getloopscale/loop-runtime/api/internal/middleware/clientip"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// captureWriter implements AuthAuditWriter and records calls.
type captureWriter struct {
	mu       sync.Mutex
	entries  []capturedEntry
	failNext bool
}

type capturedEntry struct {
	tenantID, action, actor, resourceType, resourceID, correlationID string
	metadata                                                         map[string]any
}

func (c *captureWriter) write(ctx context.Context, tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failNext {
		c.failNext = false
		return errors.New("simulated DB error")
	}
	c.entries = append(c.entries, capturedEntry{
		tenantID: tenantID, action: action, actor: actor,
		resourceType: resourceType, resourceID: resourceID,
		correlationID: correlationID, metadata: metadata,
	})
	return nil
}

func (c *captureWriter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func makeReq(ip string) *http.Request {
	r := httptest.NewRequest("POST", "/api/v1/deploys", nil)
	r.RemoteAddr = ip + ":54321"
	return r
}

func TestAuthFailureAuditor_EmitsTokenInvalidEntry(t *testing.T) {
	cap := &captureWriter{}
	auditor := NewAuthFailureAuditor(cap.write, discardLogger(), clientip.Resolver{})

	auditor.Emit(context.Background(), makeReq("203.0.113.1"), AuthFailureTokenInvalid, "bad signature", nil)

	if cap.count() != 1 {
		t.Fatalf("expected 1 audit entry, got %d", cap.count())
	}
	entry := cap.entries[0]
	if entry.action != string(AuthFailureTokenInvalid) {
		t.Errorf("expected action=%s, got %s", AuthFailureTokenInvalid, entry.action)
	}
	if entry.resourceType != "session" {
		t.Errorf("expected resource_type=session, got %s", entry.resourceType)
	}
}

func TestAuthFailureAuditor_FailOpenOnDBError(t *testing.T) {
	cap := &captureWriter{failNext: true}
	auditor := NewAuthFailureAuditor(cap.write, discardLogger(), clientip.Resolver{})

	// Should NOT panic, should NOT block, even if DB fails (EC-NEW-12)
	auditor.Emit(context.Background(), makeReq("203.0.113.1"), AuthFailureTokenInvalid, "test", nil)

	// Reaching here = test passes; auditor didn't panic on DB error
}

func TestAuthFailureAuditor_RateLimitsPerIP(t *testing.T) {
	cap := &captureWriter{}
	auditor := NewAuthFailureAuditor(cap.write, discardLogger(), clientip.Resolver{})

	req := makeReq("203.0.113.1")
	for i := 0; i < 150; i++ {
		auditor.Emit(context.Background(), req, AuthFailureTokenInvalid, "test", nil)
	}

	count := cap.count()
	if count > 100 {
		t.Errorf("expected ≤100 entries (rate-limited), got %d", count)
	}
	if count < 80 {
		t.Errorf("expected ≥80 entries before rate limit, got %d", count)
	}
}

func TestAuthFailureAuditor_DoesNotRateLimitAcrossIPs(t *testing.T) {
	cap := &captureWriter{}
	auditor := NewAuthFailureAuditor(cap.write, discardLogger(), clientip.Resolver{})

	for ipSuffix := 1; ipSuffix <= 5; ipSuffix++ {
		req := makeReq("203.0.113." + string(rune('0'+ipSuffix)))
		for i := 0; i < 50; i++ {
			auditor.Emit(context.Background(), req, AuthFailureTokenInvalid, "test", nil)
		}
	}

	if cap.count() < 200 {
		t.Errorf("expected ≥200 entries (no cross-IP rate limit), got %d", cap.count())
	}
}

func TestAuthFailureAuditor_NilSafe(t *testing.T) {
	var a *AuthFailureAuditor // nil
	a.Emit(context.Background(), makeReq("1.1.1.1"), AuthFailureTokenInvalid, "test", nil)
}

func TestAuthFailureAuditor_EnrichesMetadata(t *testing.T) {
	cap := &captureWriter{}
	auditor := NewAuthFailureAuditor(cap.write, discardLogger(), clientip.Resolver{})

	req := makeReq("203.0.113.42")
	req.Header.Set("Correlation-ID", "req-12345")
	req.Header.Set("User-Agent", "loopruntime-cli/0.13.0")

	auditor.Emit(context.Background(), req, AuthFailureServiceMismatch, "shared secret mismatch", map[string]any{"extra_key": "extra_value"})

	if cap.count() != 1 {
		t.Fatalf("expected 1 entry, got %d", cap.count())
	}
	m := cap.entries[0].metadata
	if m["ip"] != "203.0.113.42" {
		t.Errorf("expected ip in metadata, got %v", m["ip"])
	}
	if m["correlation_id"] != "req-12345" {
		t.Errorf("expected correlation_id in metadata, got %v", m["correlation_id"])
	}
	if m["user_agent"] != "loopruntime-cli/0.13.0" {
		t.Errorf("expected user_agent in metadata, got %v", m["user_agent"])
	}
	if m["extra_key"] != "extra_value" {
		t.Errorf("expected merged extra metadata, got %v", m["extra_key"])
	}
	if m["reason"] != "shared secret mismatch" {
		t.Errorf("expected reason in metadata, got %v", m["reason"])
	}
}

// A client that rotates X-Forwarded-For must not get a fresh audit bucket per
// value: the header is only believed from a trusted proxy, so an untrusted
// peer is throttled on its own address whatever it claims to be.
func TestAuthFailureAuditor_ThrottlesAnUntrustedPeerThatRotatesXForwardedFor(t *testing.T) {
	cap := &captureWriter{}
	auditor := NewAuthFailureAuditor(cap.write, discardLogger(), clientip.Resolver{})

	for i := 0; i < 150; i++ {
		req := httptest.NewRequest("POST", "/api/v1/deploys", nil)
		req.RemoteAddr = "198.51.100.7:4321"
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.0.%d.%d", i/250, i%250))
		auditor.Emit(context.Background(), req, AuthFailureTokenInvalid, "test", nil)
	}

	if got := cap.count(); got > 100 {
		t.Errorf("wrote %d audit rows for one peer; a spoofed X-Forwarded-For bypassed the 100/min throttle", got)
	}
}
