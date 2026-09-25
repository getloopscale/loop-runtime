// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package audit is the audit-log feature: the audit-log handlers live in
// audithttp, the nightly retention pruner is AuditLogPruner, and this file
// holds the canonical closed enum of allowed audit log
// `action` strings — the boundary-side filter per ADR-Q4 of
// qa-dogfood-cleanup-2026-06-21-plan.md.
//
// The 2026-06-21 QA dogfooding run flagged a raw SQL fragment
// (`INSERT tenant_members`) leaking into the audit log alongside
// canonical domain events (`project.created`, `repository.connected`).
// Per ADR-Q4 the audit boundary accepts ONLY domain events; raw SQL
// ops are rejected (or remapped) at emit time. The dashboard ships a
// defense-in-depth display fallback (#126 — `displayAction()`); this
// package is the canonical engine-side enforcement.
//
// Two-phase rollout per EC-3:
//
//	Phase A — feature flag AUDIT_ENFORCE_DOMAIN_ENUM=false (default):
//	          ValidateAction() LOGS WARN on unknown actions but returns
//	          nil (accept). Operators observe the WARN counter for at
//	          least one release cycle before flipping the flag.
//
//	Phase B — AUDIT_ENFORCE_DOMAIN_ENUM=true: ValidateAction() returns
//	          ErrUnknownDomainEvent for any action outside the enum.
//	          Emitters must remap (e.g., `INSERT` on tenant_members →
//	          `tenant.member.added`) before calling WriteAuditLog.
//
// The enum extension protocol is intentionally loose: adding a new
// allowed action is just appending to AllowedDomainEvents. New events
// land via normal PR review; the closed-enum is the floor that blocks
// LLM-emitted SQL leaks, not a tight policy gate on legitimate events.
package audit

import (
	"errors"
	"fmt"
	"os"
)

// EnforceDomainEnumEnvVar is the canonical environment-variable name
// for the Phase A → Phase B rollout flag. Operators set
// AUDIT_ENFORCE_DOMAIN_ENUM=true to flip to reject-mode.
const EnforceDomainEnumEnvVar = "AUDIT_ENFORCE_DOMAIN_ENUM"

// ErrUnknownDomainEvent is returned by ValidateAction in Phase B
// (AUDIT_ENFORCE_DOMAIN_ENUM=true) when the caller passes an action
// string outside the canonical enum.
var ErrUnknownDomainEvent = errors.New("audit: action is not a canonical domain event")

// AllowedDomainEvents is the closed set of canonical domain event
// names per ADR-Q4. New events land via PR review — add the literal
// here AND update the dashboard `displayAction()` remap table.
//
// The naming convention is `subject.verb` in past tense (matches the
// existing audit log shape: `project.created`, `secret.set`).
var AllowedDomainEvents = map[string]struct{}{
	"project.created":         {},
	"project.deleted":         {},
	"project.updated":         {},
	"repository.connected":    {},
	"repository.disconnected": {},
	"tenant.member.added":     {},
	"tenant.member.removed":   {},
	"secret.set":              {},
	"secret.removed":          {},
	"build.started":           {},
	"build.completed":         {},
	"build.failed":            {},
	"release.activated":       {},
	"release.rolled_back":     {},
	"environment.created":     {},
	"environment.deleted":     {},
	"domain.added":            {},
	"domain.verified":         {},
	"domain.removed":          {},
	"api_key.created":         {},
	"api_key.revoked":         {},
}

// IsAllowedDomainEvent reports whether the given action is a member
// of the canonical enum. Stateless; cheap (map lookup).
func IsAllowedDomainEvent(action string) bool {
	_, ok := AllowedDomainEvents[action]
	return ok
}

// ValidateAction is the boundary-side filter called from WriteAuditLog
// (or any audit emitter) before persisting. Returns nil to ACCEPT, an
// error to REJECT.
//
// Phase A (default): always nil — the caller is expected to log a WARN
// via the returned warn-only signal. Use ValidateActionVerbose to also
// surface the unknown-action signal in Phase A.
//
// Phase B (AUDIT_ENFORCE_DOMAIN_ENUM=true): returns
// ErrUnknownDomainEvent when action is outside AllowedDomainEvents.
func ValidateAction(action string) error {
	if IsAllowedDomainEvent(action) {
		return nil
	}
	if EnforceMode() {
		return fmt.Errorf("%w: %q", ErrUnknownDomainEvent, action)
	}
	return nil
}

// EnforceMode reports whether the engine is currently configured to
// REJECT unknown actions (Phase B). Reads the env var on each call so
// tests can flip the mode without restarting the process.
func EnforceMode() bool {
	v := os.Getenv(EnforceDomainEnumEnvVar)
	return v == "true" || v == "1" || v == "yes"
}

// UnknownActionWarning is the canonical structured-log shape for
// Phase A WARN messages. Emitters should use this exact format so a
// downstream log query can count occurrences pre-Phase-B.
func UnknownActionWarning(action, resourceType string) string {
	return fmt.Sprintf("audit: WARN unknown_domain_event action=%q resource_type=%q (Phase A WARN-only; will REJECT after %s flips to true)", action, resourceType, EnforceDomainEnumEnvVar)
}
