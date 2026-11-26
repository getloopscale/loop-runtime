// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// T2.6 of deep-review-remediation-plan (closes find_sec_005 / SEC-005 MEDIUM).
// Unit tests for the runtime SQL parameterization guard.

package store

import "testing"

func TestIsParameterizedCondition_Accepts(t *testing.T) {
	cases := []string{
		"tenant_id = $1",
		"app_name = $42",
		"environment_id = $7",
		"domain = $9",
		"a = $1",
	}
	for _, c := range cases {
		if !isParameterizedCondition(c) {
			t.Errorf("expected accepted, got rejected: %q", c)
		}
	}
}

func TestIsParameterizedCondition_RejectsLiteral(t *testing.T) {
	cases := []string{
		"tenant_id = 'literal'",
		"tenant_id = 5",
		"tenant_id = NULL",
	}
	for _, c := range cases {
		if isParameterizedCondition(c) {
			t.Errorf("expected rejected (literal value): %q", c)
		}
	}
}

func TestIsParameterizedCondition_RejectsInjection(t *testing.T) {
	cases := []string{
		"tenant_id = $1 OR 1=1",
		"tenant_id = 'foo' OR 1=1",
		"tenant_id = $1; DROP TABLE tenants",
		"1=1",
		"$1 = $2",       // both placeholders, not column-parameterized
		"tenant_id $1",  // missing =
		"tenant_id =$1", // missing space
	}
	for _, c := range cases {
		if isParameterizedCondition(c) {
			t.Errorf("expected rejected (injection or malformed): %q", c)
		}
	}
}

func TestIsParameterizedCondition_RejectsUppercase(t *testing.T) {
	// PostgreSQL allows uppercase column names but the lint enforces lowercase
	// to match the existing schema convention. Mixed case = reject (catches
	// inadvertent fmt.Sprintf with user input via UPPER() or similar).
	cases := []string{
		"TenantID = $1",
		"TENANT_ID = $1",
	}
	for _, c := range cases {
		if isParameterizedCondition(c) {
			t.Errorf("expected rejected (uppercase): %q", c)
		}
	}
}
