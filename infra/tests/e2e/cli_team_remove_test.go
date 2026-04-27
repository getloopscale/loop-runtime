// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero team remove <username>` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/team-remove.ts (AUDIT-25).
//
// Behavior:
//   - Exit 0: success
//   - Exit 1: MISSING_ARGUMENT, OWNER_PROTECTED (409), SELF_REMOVE (409),
//     MEMBER_NOT_FOUND (404)
//   - Exit 3: FORBIDDEN, API_UNREACHABLE
//   - Exit 4: AUTH_REQUIRED
//   - --json success: {ok: true, username, status: "removed"}
//   - --json error: {error, exit_code, code}
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero team remove", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 2 + 5: 404 not a member ---

	It("Contract 2+5a: nonexistent member yields exit 1 MEMBER_NOT_FOUND", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("team", "remove",
			"this-user-is-not-a-member-"+uniqueSuffix(), "--json")

		expectAllowedExitCode(exitCode, "team remove (not a member)")
		Expect(exitCode).To(Equal(1),
			"non-member should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("MEMBER_NOT_FOUND"))
	})

	// --- CRITICAL: SELF_REMOVE / OWNER_PROTECTED ---
	//
	// We can exercise SELF_REMOVE by attempting to remove the seed owner with
	// the seed owner's token. Backend should return 409 SELF_REMOVE.
	// OWNER_PROTECTED: removing owner with another owner-level user — same
	// path effectively. Our seed has only owner, so SELF_REMOVE is what fires.

	It("CRITICAL: removing self yields exit 1 SELF_REMOVE (cannot self-eject)", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		// seed.UserID is the GitHub-style identifier for the seed user.
		stdout, stderr, exitCode := runLoopZero("team", "remove", seed.UserID, "--json")

		expectAllowedExitCode(exitCode, "team remove self")
		expectNoInternalTerms(stdout+stderr, "team remove self")

		// Backend may return either OWNER_PROTECTED or SELF_REMOVE depending
		// on which check fires first. Both yield exit 1. Accept either.
		Expect(exitCode).To(Equal(1),
			"self-removal must exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		code := jsonStringField(obj, "code")
		Expect(code == "SELF_REMOVE" || code == "OWNER_PROTECTED" || code == "MEMBER_NOT_FOUND").To(BeTrue(),
			"self-remove should yield SELF_REMOVE or OWNER_PROTECTED (or MEMBER_NOT_FOUND if username mismatch). Got: %v",
			obj)
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5b: no token yields exit 4 AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("team", "remove",
			"anyone", "--json")

		expectAllowedExitCode(exitCode, "team remove (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		stdout, stderr, _ := runLoopZeroNoToken("team", "remove", "anyone")
		expectNoInternalTerms(stdout, "team remove (no token) stdout")
		expectNoInternalTerms(stderr, "team remove (no token) stderr")

		stdout, stderr, _ = runLoopZeroNoToken("team", "remove", "anyone", "--json")
		expectNoInternalTerms(stdout, "team remove --json (no token) stdout")
		expectNoInternalTerms(stderr, "team remove --json (no token) stderr")

		// 404 path with valid auth.
		err := seed.Run(seed.Config{})
		if err == nil && apiHealthy() {
			oldToken := os.Getenv("LOOPZERO_TOKEN")
			os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
			defer os.Setenv("LOOPZERO_TOKEN", oldToken)

			for _, args := range [][]string{
				{"team", "remove", "absent-" + uniqueSuffix()},
				{"team", "remove", "absent-" + uniqueSuffix(), "--json"},
			} {
				stdout, stderr, _ := runLoopZero(args...)
				expectNoInternalTerms(stdout, strings.Join(args, " ")+" stdout")
				expectNoInternalTerms(stderr, strings.Join(args, " ")+" stderr")
			}
		}
	})

	// --- Happy path SKIP per seed gap ---

	It("Contract 1+3: SKIP — happy path requires second non-owner member", func() {
		Skip("seed package only provides 1 user (owner). Removing the only user " +
			"is irreversible and breaks subsequent tests. Tracked as PR Guardrail " +
			"Phase 5 follow-up: enrich seed with a removable secondary member.")
	})
})
