// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T2.7 6th test condition per MEET-2026-05-27 + ADR-2026-05-27-T2.7-RAK.
// Hermetic unit tests for validateStripeKeyForEnv boot-time assertion.

package main

import (
	"strings"
	"testing"
)

func TestValidateStripeKeyForEnv_ProdRequiresLiveKey(t *testing.T) {
	cases := []struct {
		name       string
		env        string
		stripeKey  string
		wantErr    bool
		wantSubstr string
	}{
		{
			name:      "prod + sk_live_ key → OK",
			env:       "production",
			stripeKey: "REDACTED_SECRET_047",
			wantErr:   false,
		},
		{
			name:      "prod + Production case variant + sk_live_ → OK",
			env:       "Production",
			stripeKey: "sk_live_xxxxx",
			wantErr:   false,
		},
		{
			name:       "prod + sk_test_ → REJECT",
			env:        "production",
			stripeKey:  "sk_test_dangerous_test_mode_in_production",
			wantErr:    true,
			wantSubstr: "BOOT REFUSED",
		},
		{
			name:       "prod + sk_test_ → mentions ADR for traceability",
			env:        "production",
			stripeKey:  "sk_test_dangerous_test_mode_in_production",
			wantErr:    true,
			wantSubstr: "ADR-2026-05-27-T2.7-RAK",
		},
		{
			name:       "prod + completely wrong prefix → REJECT",
			env:        "production",
			stripeKey:  "pk_live_publishable_key_by_mistake",
			wantErr:    true,
			wantSubstr: "BOOT REFUSED",
		},
		{
			name:      "development + sk_test_ → OK (no enforcement)",
			env:       "development",
			stripeKey: "sk_test_dev_key",
			wantErr:   false,
		},
		{
			name:      "development + sk_live_ → OK (allowed but suspicious)",
			env:       "development",
			stripeKey: "sk_live_dev_with_live_key",
			wantErr:   false,
		},
		{
			name:      "staging + sk_test_ → OK (only production enforced)",
			env:       "staging",
			stripeKey: "sk_test_staging",
			wantErr:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStripeKeyForEnv(tc.env, tc.stripeKey)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error but got nil for env=%q key prefix=%q",
					tc.env, tc.stripeKey[:8])
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error but got: %v", err)
			}
			if tc.wantSubstr != "" && err != nil && !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error %q does not contain expected substring %q",
					err.Error(), tc.wantSubstr)
			}
		})
	}
}

// Regression guard: error message must NEVER contain the full key (only
// up to 8-char prefix). Operator logs MUST NOT leak credentials.
func TestValidateStripeKeyForEnv_NeverLeaksFullKey(t *testing.T) {
	fullKey := "sk_test_ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890_super_secret_DO_NOT_LEAK"
	err := validateStripeKeyForEnv("production", fullKey)
	if err == nil {
		t.Fatal("expected error for sk_test in production")
	}
	msg := err.Error()
	// Secret portion (after 8-char prefix) must NEVER appear in error.
	secretPortion := fullKey[8:]
	if strings.Contains(msg, secretPortion) {
		t.Errorf("error message LEAKED secret portion: %q (in message %q)",
			secretPortion, msg)
	}
	// Specific known-leaky substrings:
	for _, leaky := range []string{"DO_NOT_LEAK", "super_secret", "1234567890"} {
		if strings.Contains(msg, leaky) {
			t.Errorf("error message contains forbidden substring %q", leaky)
		}
	}
}

// Regression guard: Rule 3 (no internal terms in user-facing strings).
// The error message is operator-facing (panic to stderr at boot), so
// technical terms like "Stripe" + "RAK" + "sk_live" are ACCEPTABLE
// (operators need to know what to fix). But this test documents the
// expectation so future changes don't accidentally leak runtime internals.
func TestValidateStripeKeyForEnv_OperatorActionableMessage(t *testing.T) {
	err := validateStripeKeyForEnv("production", "sk_test_xxx")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	// Must include actionable next-step (Rule 7 from .claude/rules/cli-output.md).
	for _, required := range []string{
		"sk_live_",   // tell operator the correct prefix
		"ADR-",       // traceability to decision
		"ESO Secret", // tell operator where to rotate
	} {
		if !strings.Contains(msg, required) {
			t.Errorf("error message missing actionable element %q: %s", required, msg)
		}
	}
}

func TestIsProductionEnv_CaseInsensitive(t *testing.T) {
	cases := map[string]bool{
		"production":  true,
		"PRODUCTION":  true,
		"Production":  true,
		"prod":        false, // strict: only "production" matches (mirrors config.IsProduction)
		"staging":     false,
		"development": false,
		"":            false,
	}
	for env, want := range cases {
		got := isProductionEnv(env)
		if got != want {
			t.Errorf("isProductionEnv(%q) = %v, want %v", env, got, want)
		}
	}
}
