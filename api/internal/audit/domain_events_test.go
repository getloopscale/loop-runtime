// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package audit

import (
	"errors"
	"strings"
	"testing"
)

func TestIsAllowedDomainEvent_CanonicalSet(t *testing.T) {
	canonical := []string{
		"project.created",
		"project.deleted",
		"repository.connected",
		"tenant.member.added",
		"tenant.member.removed",
		"secret.set",
		"release.activated",
	}
	for _, a := range canonical {
		if !IsAllowedDomainEvent(a) {
			t.Errorf("IsAllowedDomainEvent(%q) = false; expected true (canonical domain event per ADR-Q4)", a)
		}
	}
}

func TestIsAllowedDomainEvent_RejectsSQLLeaks(t *testing.T) {
	rawSQLOps := []string{
		"INSERT", "UPDATE", "DELETE",
		"INSERT tenant_members", // the exact #126 bug shape
		"DROP TABLE",            // gratuitous
		"",                      // empty
	}
	for _, a := range rawSQLOps {
		if IsAllowedDomainEvent(a) {
			t.Errorf("IsAllowedDomainEvent(%q) = true; expected false (raw SQL op MUST NOT pass the canonical filter)", a)
		}
	}
}

func TestValidateAction_PhaseADefault(t *testing.T) {
	t.Setenv(EnforceDomainEnumEnvVar, "")
	// Phase A: unknown action is ACCEPTED (return nil) because the
	// flag is off. Operators observe via WARN logs before flipping.
	if err := ValidateAction("INSERT"); err != nil {
		t.Errorf("ValidateAction phase A: unknown action returned %v; want nil (WARN-only mode)", err)
	}
	// Canonical action: always accepted regardless of flag.
	if err := ValidateAction("project.created"); err != nil {
		t.Errorf("ValidateAction phase A: canonical action returned %v; want nil", err)
	}
}

func TestValidateAction_PhaseBEnforce(t *testing.T) {
	t.Setenv(EnforceDomainEnumEnvVar, "true")
	if err := ValidateAction("INSERT"); err == nil {
		t.Errorf("ValidateAction phase B: unknown action returned nil; want ErrUnknownDomainEvent (REJECT mode)")
	} else if !errors.Is(err, ErrUnknownDomainEvent) {
		t.Errorf("ValidateAction phase B: error type = %T (%v); want errors.Is(ErrUnknownDomainEvent)", err, err)
	}
	// Canonical action: always accepted regardless of phase.
	if err := ValidateAction("tenant.member.added"); err != nil {
		t.Errorf("ValidateAction phase B: canonical action returned %v; want nil", err)
	}
}

func TestEnforceMode_RecognizesCommonTruthyValues(t *testing.T) {
	tests := map[string]bool{
		"true":  true,
		"1":     true,
		"yes":   true,
		"false": false,
		"0":     false,
		"":      false,
		"TRUE":  false, // strict — operators should use lowercase
	}
	for v, want := range tests {
		t.Setenv(EnforceDomainEnumEnvVar, v)
		if got := EnforceMode(); got != want {
			t.Errorf("EnforceMode() with %s=%q = %v; want %v", EnforceDomainEnumEnvVar, v, got, want)
		}
	}
}

func TestUnknownActionWarning_StructuredShape(t *testing.T) {
	msg := UnknownActionWarning("INSERT", "tenant_members")
	for _, expected := range []string{
		"unknown_domain_event",
		`action="INSERT"`,
		`resource_type="tenant_members"`,
		"Phase A WARN-only",
		EnforceDomainEnumEnvVar,
	} {
		if !strings.Contains(msg, expected) {
			t.Errorf("UnknownActionWarning output missing expected substring %q; got: %s", expected, msg)
		}
	}
}
