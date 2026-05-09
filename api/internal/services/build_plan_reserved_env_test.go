// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// build_plan_reserved_env_test.go — T2.5 of deep-review-remediation-plan.
// Closes find_sec_004 (MEDIUM): user-submitted env keys with reserved
// prefixes (LOOPZERO_*, KUBERNETES_*, AWS_*, DO_*, GOOGLE_*) MUST be rejected
// at the build-plan layer. loopzero-dockerfile-lint blocks them inside
// Dockerfiles; this layer blocks them in loopzero.yaml `env` blocks.

package services

import "testing"

func TestIsReservedEnvKey_LoopZeroPrefix(t *testing.T) {
	if !isReservedEnvKey("LOOPZERO_ADMIN") {
		t.Error("LOOPZERO_* must be reserved")
	}
}

func TestIsReservedEnvKey_KubernetesPrefix(t *testing.T) {
	if !isReservedEnvKey("KUBERNETES_SERVICE_HOST") {
		t.Error("KUBERNETES_* must be reserved (would let user spoof in-cluster API endpoint)")
	}
}

func TestIsReservedEnvKey_AWSPrefix(t *testing.T) {
	if !isReservedEnvKey("AWS_ACCESS_KEY_ID") {
		t.Error("AWS_* must be reserved (would let user spoof cloud creds in build context)")
	}
}

func TestIsReservedEnvKey_DOPrefix(t *testing.T) {
	if !isReservedEnvKey("DO_TOKEN") {
		t.Error("DO_* must be reserved")
	}
}

func TestIsReservedEnvKey_GooglePrefix(t *testing.T) {
	if !isReservedEnvKey("GOOGLE_APPLICATION_CREDENTIALS") {
		t.Error("GOOGLE_* must be reserved")
	}
}

func TestIsReservedEnvKey_UserAllowed(t *testing.T) {
	for _, k := range []string{"NODE_ENV", "PORT", "DATABASE_URL", "MY_APP_SECRET"} {
		if isReservedEnvKey(k) {
			t.Errorf("user key %q must NOT be reserved", k)
		}
	}
}

// TestIsReservedEnvKey_CaseSensitive — reserved check is case-sensitive.
// Lowercase variants of reserved prefixes are NOT reserved (the platform's
// injected env always uses uppercase per K8s/cloud SDK conventions).
func TestIsReservedEnvKey_CaseSensitive(t *testing.T) {
	if isReservedEnvKey("loopzero_lower") {
		t.Error("lowercase loopzero_ should NOT be reserved (case-sensitive prefix match)")
	}
}

// TestIsReservedEnvKey_ShortKey — keys shorter than the prefix are not
// reserved (avoid panic on len comparison).
func TestIsReservedEnvKey_ShortKey(t *testing.T) {
	for _, k := range []string{"X", "AW", "DO", "GO"} {
		if isReservedEnvKey(k) {
			t.Errorf("short key %q must NOT be reserved", k)
		}
	}
}
