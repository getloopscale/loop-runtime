// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero team add <username>` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/team-add.ts (AUDIT-25).
//
// Behavior:
//   - Exit 0: success OR 409 idempotent (already_member)
//   - Exit 1: MISSING_ARGUMENT, INVALID_ROLE, USER_NOT_FOUND (404)
//   - Exit 3: FORBIDDEN, API_UNREACHABLE, UNEXPECTED_ERROR
//   - Exit 4: AUTH_REQUIRED
//   - Default role: 'member'. Valid: ['member', 'admin'] — NOT owner.
//   - --json success: {ok: true, username, role, already_member?}
//   - --json error: {error, exit_code, code}
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CLI: loopzero team add", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 2 + 5: invalid role ---

	It("Contract 2+5a: --role owner yields exit 1 INVALID_ROLE", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy-validation")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		// 'owner' is NOT in VALID_ROLES — only member and admin.
		stdout, stderr, exitCode := runLoopZero("team", "add", "someone",
			"--role", "owner", "--json")

		expectAllowedExitCode(exitCode, "team add --role owner")
		Expect(exitCode).To(Equal(1),
			"owner role should exit 1 (cannot add as owner), got %d (stderr=%s)",
			exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("INVALID_ROLE"))
	})

	It("Contract 2+5b: --role with garbage value yields exit 1 INVALID_ROLE", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("team", "add", "someone",
			"--role", "superuser", "--json")

		Expect(exitCode).To(Equal(1),
			"garbage role should exit 1, got %d (stderr=%s)", exitCode, stderr)
		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("INVALID_ROLE"))
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5c: no token yields exit 4 AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("team", "add", "anyone", "--json")

		expectAllowedExitCode(exitCode, "team add (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 2 + 5: missing username ---

	It("Contract 2+5d: missing username yields exit 1", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		_, stderr, exitCode := runLoopZero("team", "add", "--json")

		expectAllowedExitCode(exitCode, "team add (no username)")
		Expect(exitCode).To(Equal(1),
			"missing username should exit 1, got %d (stderr=%s)", exitCode, stderr)
	})

	// --- Contract 4 ---

	It("Contract 4: validation/error paths leak no internal terms", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		for _, args := range [][]string{
			{"team", "add", "user", "--role", "owner"},
			{"team", "add", "user", "--role", "owner", "--json"},
			{"team", "add", "user", "--role", "garbage"},
			{"team", "add", "user", "--role", "garbage", "--json"},
		} {
			stdout, stderr, _ := runLoopZero(args...)
			expectNoInternalTerms(stdout, strings.Join(args, " ")+" stdout")
			expectNoInternalTerms(stderr, strings.Join(args, " ")+" stderr")
		}

		stdout, stderr, _ := runLoopZeroNoToken("team", "add", "x")
		expectNoInternalTerms(stdout, "team add (no token)")
		expectNoInternalTerms(stderr, "team add (no token)")
	})

	// --- Happy path SKIP ---

	It("Contract 1+3: SKIP — adding a real user requires GitHub OAuth flow + cleanup", func() {
		Skip("happy path requires (a) target user with valid GitHub login + (b) cleanup. " +
			"Tracked in PR Guardrail Phase 5 follow-up. Same gap as team-role.")
	})
})
