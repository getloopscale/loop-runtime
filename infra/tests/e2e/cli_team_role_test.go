// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero team role <username> <role>` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/team-role.ts (AUDIT-25).
//
// Behavior:
//   - Exit 0: role changed
//   - Exit 1: invalid role (INVALID_ROLE), missing args (MISSING_ARGUMENT),
//     OWNER_PROTECTED (409), MEMBER_NOT_FOUND (404)
//   - Exit 3: forbidden (FORBIDDEN), API unreachable (API_UNREACHABLE),
//     unexpected (UNEXPECTED_ERROR)
//   - Exit 4: not authenticated, 401
//   - Valid roles: "member", "admin" (no ownership transfer)
//   - --json success: {ok: true, username, role}
//   - --json error: {error, exit_code, code?}
//
// SAFETY: this test does NOT mutate real teams in production-style flows.
// All mutation tests target seeded e2e tenant + verify either:
//   (a) error paths (no real change), or
//   (b) round-trip change-then-revert.
package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CLI: loopzero team role", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 2 + 5: invalid role ---

	It("Contract 2+5a: invalid role yields exit 1 INVALID_ROLE", func() {
		// This test does not need API/k3d — auth check is short-circuited by
		// providing no token, but we want the role validation to fire so we
		// pass an invalid role with --json. However, the auth check happens
		// FIRST (line 41 of team-role.ts), so we'd hit AUTH_REQUIRED before
		// INVALID_ROLE. We emulate authentication via a dummy token; the API
		// call will be reached only if validation passes.
		//
		// Strategy: provide a token (any non-empty string), invalid role.
		// Validation rejects before API contact -> exit 1 INVALID_ROLE.
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy-for-validation-path")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("team", "role",
			"someone", "owner-not-allowed", "--json")

		expectAllowedExitCode(exitCode, "team role (invalid role)")
		Expect(exitCode).To(Equal(1),
			"invalid role should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("INVALID_ROLE"))
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5b: no token yields exit 4 AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("team", "role",
			"someone", "admin", "--json")

		expectAllowedExitCode(exitCode, "team role (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 2 + 5: missing args ---

	It("Contract 2+5c: missing username yields exit 1 (Commander or MISSING_ARGUMENT)", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("team", "role")

		expectAllowedExitCode(exitCode, "team role (no args)")
		Expect(exitCode).To(Equal(1),
			"missing args should exit 1, got %d (stderr=%s)", exitCode, stderr)

		// Either Commander error OR our MISSING_ARGUMENT — both valid exit 1.
		// stderr will contain Commander error text in non-JSON mode.
		Expect(stdout + stderr).NotTo(BeEmpty())
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		// No-token path.
		stdout, stderr, _ := runLoopZeroNoToken("team", "role", "user", "admin")
		expectNoInternalTerms(stdout, "team role (no token) stdout")
		expectNoInternalTerms(stderr, "team role (no token) stderr")

		stdout, stderr, _ = runLoopZeroNoToken("team", "role", "user", "admin", "--json")
		expectNoInternalTerms(stdout, "team role --json (no token) stdout")
		expectNoInternalTerms(stderr, "team role --json (no token) stderr")

		// Invalid-role path.
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, _ = runLoopZero("team", "role", "user", "garbage")
		expectNoInternalTerms(stdout, "team role (invalid) stdout")
		expectNoInternalTerms(stderr, "team role (invalid) stderr")

		stdout, stderr, _ = runLoopZero("team", "role", "user", "garbage", "--json")
		expectNoInternalTerms(stdout, "team role --json (invalid) stdout")
		expectNoInternalTerms(stderr, "team role --json (invalid) stderr")
	})

	// --- Contract 1 + 3: happy path (mutation, requires real environment) ---
	//
	// We deliberately keep happy path simple: only validate JSON shape if
	// changing role succeeds. We do NOT assume a target user exists; we add
	// then remove + change role on a synthetic seeded user.

	It("Contract 1+3: SKIP if no admin context — happy path requires owner role and target member", func() {
		// This is documented as a known gap in Phase 5: writing a working
		// happy-path mutation test requires:
		//   1. a seeded team owner token
		//   2. a seeded non-owner member to mutate
		//   3. cleanup that restores the original role
		// The seed package currently provides only one user (Owner). Until
		// the seed includes a second member, this test stays as a SKIP
		// placeholder. Marking explicitly per matrix transparency.
		Skip("seed package needs a second non-owner member to exercise role change happy path. " +
			"Tracked in PR Guardrail Phase 5 follow-up. Without this, the test would " +
			"either mutate the only user (irreversible) or be a no-op against missing target.")
	})
})
