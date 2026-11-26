// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package vault

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// newCaptureLogger builds a JSON slog.Logger that writes into the
// returned *bytes.Buffer, wrapped through RedactionHandler. Tests scan
// the buffer for the literal [REDACTED:vault] marker AND for absence of
// the original sensitive bytes.
func newCaptureLogger() (*slog.Logger, *bytes.Buffer, *sync.Mutex) {
	var buf bytes.Buffer
	var mu sync.Mutex
	// Use a mutex-wrapped writer so concurrent goroutines don't trip the
	// race detector on the buffer.
	w := &mutexWriter{w: &buf, mu: &mu}
	inner := slog.NewJSONHandler(w, nil)
	return slog.New(NewRedactionHandler(inner)), &buf, &mu
}

type mutexWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (m *mutexWriter) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.w.Write(p)
}

// readBuf grabs the buffer content while holding the lock.
func readBuf(buf *bytes.Buffer, mu *sync.Mutex) string {
	mu.Lock()
	defer mu.Unlock()
	return buf.String()
}

// TestRedaction_VaultPrefixCiphertext — canonical case: any vault:v1:
// payload MUST be redacted in attrs.
func TestRedaction_VaultPrefixCiphertext(t *testing.T) {
	logger, buf, mu := newCaptureLogger()
	logger.Info("processed secret", "ciphertext", "vault:v1:abc123XYZsensitive")
	out := readBuf(buf, mu)

	if !strings.Contains(out, redactedLabel) {
		t.Errorf("output should include %q; got %s", redactedLabel, out)
	}
	if strings.Contains(out, "abc123XYZsensitive") {
		t.Errorf("CRITICAL: raw vault payload leaked: %s", out)
	}
}

// TestRedaction_AppRoleToken — Vault tokens (hvs.* + hvb.*) are
// redacted.
func TestRedaction_AppRoleToken(t *testing.T) {
	logger, buf, mu := newCaptureLogger()
	logger.Info("login response", "token", "hvs.SECRETLY_LONG_TOKEN_BYTES_DO_NOT_LEAK")
	out := readBuf(buf, mu)
	if strings.Contains(out, "SECRETLY_LONG_TOKEN_BYTES_DO_NOT_LEAK") {
		t.Errorf("AppRole token leaked: %s", out)
	}
	if !strings.Contains(out, redactedLabel) {
		t.Errorf("expected redaction marker; got %s", out)
	}
}

// TestRedaction_LongBase64Token — generic > 32-char base64 strings are
// redacted (defense against AppRole secret_id or AES key leakage).
func TestRedaction_LongBase64Token(t *testing.T) {
	logger, buf, mu := newCaptureLogger()
	// 40 base64 chars — likely a JWT-shape or AES-key-shape bytes.
	longToken := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	logger.Info("dump", "blob", longToken)
	out := readBuf(buf, mu)
	if strings.Contains(out, longToken) {
		t.Errorf("long base64 leaked: %s", out)
	}
}

// TestRedaction_TenantIDNotRedacted — operational logs need tenant IDs
// and short names. "tn-X" is short + does not match any sensitive
// pattern; MUST survive intact for forensics.
func TestRedaction_TenantIDNotRedacted(t *testing.T) {
	logger, buf, mu := newCaptureLogger()
	logger.Info("audit", "tenant", "tn-test-123", "secret_name", "DATABASE_URL")
	out := readBuf(buf, mu)
	if !strings.Contains(out, "tn-test-123") {
		t.Errorf("tenant ID was redacted (should NOT be); got %s", out)
	}
	if !strings.Contains(out, "DATABASE_URL") {
		t.Errorf("secret name was redacted (should NOT be); got %s", out)
	}
}

// TestRedaction_MessageBody — the log message itself is redacted, not
// only attrs. Defense against `slog.Error("vault:v1:abc decryption failed")`.
func TestRedaction_MessageBody(t *testing.T) {
	logger, buf, mu := newCaptureLogger()
	logger.Error("failed to decrypt vault:v1:abcSENSITIVE")
	out := readBuf(buf, mu)
	if strings.Contains(out, "abcSENSITIVE") {
		t.Errorf("message body leaked sensitive payload: %s", out)
	}
}

// TestRedaction_GroupAttrs — slog.Group values recurse into the redactor
// so nested sensitive strings are still scrubbed.
func TestRedaction_GroupAttrs(t *testing.T) {
	logger, buf, mu := newCaptureLogger()
	logger.Info("nested",
		slog.Group("ctx",
			slog.String("ciphertext", "vault:v1:nestedPAYLOAD"),
			slog.String("tenant", "tn-A"),
		),
	)
	out := readBuf(buf, mu)
	if strings.Contains(out, "nestedPAYLOAD") {
		t.Errorf("nested ciphertext leaked: %s", out)
	}
	if !strings.Contains(out, "tn-A") {
		t.Errorf("tenant inside group was over-redacted: %s", out)
	}
}

// TestRedaction_WithAttrsPreApplied — when logger.With(...) is used,
// the attrs are redacted once at chain time and the chained logger emits
// them already-redacted.
func TestRedaction_WithAttrsPreApplied(t *testing.T) {
	logger, buf, mu := newCaptureLogger()
	scoped := logger.With("ciphertext", "vault:v1:preApplied")
	scoped.Info("event")
	out := readBuf(buf, mu)
	if strings.Contains(out, "preApplied") {
		t.Errorf("With() attrs not redacted: %s", out)
	}
}

// TestRedaction_NonStringAttrsUnchanged — int/bool/duration attrs are
// metadata, not values. They pass through verbatim.
func TestRedaction_NonStringAttrsUnchanged(t *testing.T) {
	logger, buf, mu := newCaptureLogger()
	logger.Info("metrics", "count", 42, "ok", true)
	out := readBuf(buf, mu)
	if !strings.Contains(out, "42") {
		t.Errorf("int attr lost: %s", out)
	}
	if !strings.Contains(out, "true") {
		t.Errorf("bool attr lost: %s", out)
	}
}

// TestRedaction_ConcurrentRace — EC-04 + T3.2 invariant: 100 concurrent
// goroutines log vault-shaped ciphertext; the race detector MUST be
// clean and 100% of emitted records contain [REDACTED:vault] (i.e. zero
// race-induced bypasses).
func TestRedaction_ConcurrentRace(t *testing.T) {
	logger, buf, mu := newCaptureLogger()

	const N = 100
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ct := "vault:v1:raceCiphertextNumberXYZ"
			logger.Info("concurrent", "ct", ct, "i", idx)
		}(i)
	}
	wg.Wait()

	out := readBuf(buf, mu)
	if strings.Contains(out, "raceCiphertextNumberXYZ") {
		t.Errorf("CRITICAL: race-induced leak; raw vault payload appears in output")
	}
	// Count the redacted markers — should be at least N (one per call).
	got := strings.Count(out, redactedLabel)
	if got < N {
		t.Errorf("expected ≥%d [REDACTED:vault] markers; got %d", N, got)
	}
}

// TestRedaction_NilInnerReturnsNil — defensive: NewRedactionHandler(nil)
// returns nil so callers can fall back at wire time. It used to cite
// adapters.NewVaultTransitAdapter as the mirror of this contract; B-058 deleted
// that adapter and the citation survived it. The live mirror is
// setupTransitClient (api/cmd/setup_transit.go), which returns (nil, nil) when
// the resolved crypto mode is not "transit" so the caller keeps the legacy path.
func TestRedaction_NilInnerReturnsNil(t *testing.T) {
	if NewRedactionHandler(nil) != nil {
		t.Errorf("NewRedactionHandler(nil) should return nil")
	}
}

// TestRedaction_EnabledDelegates — the redaction wrapper does not change
// level-filtering semantics.
func TestRedaction_EnabledDelegates(t *testing.T) {
	inner := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := NewRedactionHandler(inner)
	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Errorf("Debug enabled when inner is Warn-min — delegation broken")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Errorf("Error not enabled when inner is Warn-min — delegation broken")
	}
}
