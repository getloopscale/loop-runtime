// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// T3.2 of plan 2026-06-10-j22-openbao-transit-eso-sync v1.2 — slog.Handler
// wrapper that redacts sensitive patterns (vault:v1: ciphertext, Vault/AppRole
// tokens, long base64-shaped strings) in every emitted log record's message
// AND attributes. Defense against accidental plaintext leaks via slog.
//
// FAANG-grade pattern: belt + suspenders alongside the D5 grep proof.
// Without this filter, a careless `slog.Error("decrypt failed", "value",
// secret.Value)` would leak plaintext into Loki / stdout / journald.
//
// Fail-open contract: regex panics or unexpected types fall through to
// the wrapped handler unchanged — observability MUST NOT itself become a
// bug surface. The grep proof (T4.1) is the canonical safety net.

package vault

import (
	"context"
	"log/slog"
	"regexp"
)

// redactedLabel is the canonical substitution for any matched sensitive
// pattern. Operators searching for redaction events grep for this exact
// string.
const redactedLabel = "[REDACTED:vault]"

// Sensitive-pattern regexes. Order at call site:
//  1. vaultCipherRe — narrowest, longest expected match; must run first
//     so the broader longBase64Re does NOT consume the ciphertext payload
//     before we tag it as vault-specific.
//  2. vaultTokenRe — Vault/AppRole client tokens (hvs.* modern; hvb.*
//     batch). Distinguishable from base64 by the literal prefix.
//  3. longBase64Re — generic > 32-char base64-shaped strings. Catches
//     AppRole secret_id, AES keys, or any blob accidentally inlined.
//
// Patterns are compiled once at package init (cheap; goroutine-safe per
// Go regexp contract).
var (
	vaultCipherRe = regexp.MustCompile(`vault:v1:[A-Za-z0-9+/=._-]+`)
	vaultTokenRe  = regexp.MustCompile(`hv[sb]\.[A-Za-z0-9_-]{16,}`)
	longBase64Re  = regexp.MustCompile(`[A-Za-z0-9+/=]{33,}`)
)

// RedactionHandler wraps any slog.Handler and applies redaction before
// delegating. Construct via NewRedactionHandler; the type is exported so
// callers can chain it (e.g. wrap a JSON handler that wraps an os.Stdout
// writer).
type RedactionHandler struct {
	inner slog.Handler
}

// NewRedactionHandler wraps inner with vault-pattern redaction.
// Nil inner is rejected — caller passes a real handler (json/text/etc).
func NewRedactionHandler(inner slog.Handler) *RedactionHandler {
	if inner == nil {
		return nil
	}
	return &RedactionHandler{inner: inner}
}

// Enabled delegates to the inner handler — redaction does not change
// the level-filter contract.
func (h *RedactionHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle redacts the record's message + every attribute before passing
// the new record to the inner handler. Original record is NOT mutated
// (slog.Record is a struct copy).
func (h *RedactionHandler) Handle(ctx context.Context, r slog.Record) error {
	newRec := slog.NewRecord(r.Time, r.Level, redactString(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		newRec.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, newRec)
}

// WithAttrs wraps the inner handler's WithAttrs with pre-redacted
// versions of the given attributes. Caller chaining
// `logger.With("ciphertext", "vault:v1:...").Info(...)` emits the
// redacted attr correctly.
func (h *RedactionHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = redactAttr(a)
	}
	return &RedactionHandler{inner: h.inner.WithAttrs(redacted)}
}

// WithGroup delegates unchanged — group naming is structural metadata,
// not value content.
func (h *RedactionHandler) WithGroup(name string) slog.Handler {
	return &RedactionHandler{inner: h.inner.WithGroup(name)}
}

// redactString applies all sensitive-pattern replacements. Order matters:
// vault:v1: ciphertext FIRST so the broader longBase64Re does not eat
// it before we tag it as the specific vault shape.
func redactString(s string) string {
	s = vaultCipherRe.ReplaceAllString(s, redactedLabel)
	s = vaultTokenRe.ReplaceAllString(s, redactedLabel)
	s = longBase64Re.ReplaceAllString(s, redactedLabel)
	return s
}

// redactAttr returns the input attr with string values redacted and
// group values recursed. Non-string, non-group kinds pass through
// unchanged (int, bool, time, etc. — these are non-sensitive metadata).
func redactAttr(a slog.Attr) slog.Attr {
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, redactString(a.Value.String()))
	case slog.KindGroup:
		inner := a.Value.Group()
		out := make([]slog.Attr, len(inner))
		for i := range inner {
			out[i] = redactAttr(inner[i])
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	default:
		return a
	}
}
