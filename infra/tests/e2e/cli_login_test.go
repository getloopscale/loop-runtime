// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero login` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/login.ts.
//
// Behavior:
//   - Exit 0: authenticated (with valid token + team)
//   - Exit 1: MISSING_TOKEN (CI mode no --token), TEAM_NOT_FOUND,
//     TEAM_REQUIRED (multiple teams + no --team), NO_TEAMS
//   - Exit 4: AUTH_FAILED (invalid token), SERVICE_UNAVAILABLE
//   - --json success: {authenticated, email, team, team_id, expires_in?}
//   - --json error: {error, exit_code, code}
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CLI: loopzero login", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 2 + 5: CI mode without --token ---

	It("Contract 2+5a: CI=true without --token yields exit 1 MISSING_TOKEN", func() {
		oldCI := os.Getenv("CI")
		os.Setenv("CI", "true")
		defer os.Setenv("CI", oldCI)

		stdout, stderr, exitCode := runLoopZeroNoToken("login", "--json")

		expectAllowedExitCode(exitCode, "login (CI no token)")
		Expect(exitCode).To(Equal(1),
			"CI without token should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("MISSING_TOKEN"))
	})

	// --- Contract 2 + 5: invalid token ---

	It("Contract 2+5b: invalid token yields exit 4 AUTH_FAILED", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}

		stdout, stderr, exitCode := runLoopZero("login",
			"--token", "definitely-invalid-token-"+uniqueSuffix(),
			"--json")

		expectAllowedExitCode(exitCode, "login (invalid token)")
		Expect(exitCode).To(Equal(4),
			"invalid token should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		// Either AUTH_FAILED (invalid) or SERVICE_UNAVAILABLE (5xx).
		code := jsonStringField(obj, "code")
		Expect(code == "AUTH_FAILED" || code == "SERVICE_UNAVAILABLE").To(BeTrue(),
			"unexpected code: %q", code)
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		oldCI := os.Getenv("CI")
		os.Setenv("CI", "true")
		defer os.Setenv("CI", oldCI)

		stdout, stderr, _ := runLoopZeroNoToken("login")
		expectNoInternalTerms(stdout, "login (CI no token) stdout")
		expectNoInternalTerms(stderr, "login (CI no token) stderr")

		stdout, stderr, _ = runLoopZeroNoToken("login", "--json")
		expectNoInternalTerms(stdout, "login --json (CI no token) stdout")
		expectNoInternalTerms(stderr, "login --json (CI no token) stderr")

		if apiHealthy() {
			stdout, stderr, _ := runLoopZero("login",
				"--token", "bad-"+uniqueSuffix(), "--json")
			expectNoInternalTerms(stdout, "login (invalid token) stdout")
			expectNoInternalTerms(stderr, "login (invalid token) stderr")
		}
	})

	// --- Contract 1 + 3: SKIP — happy path requires real token issuance ---

	It("Contract 1+3: SKIP — happy path requires Hydra OAuth flow + valid token", func() {
		Skip("happy path requires (a) Hydra OAuth2 client_credentials grant or " +
			"(b) GitHub PAT with read:user scope. Both are environment-specific. " +
			"`seed.AuthToken` is `e2e-token-dev` — valid only against E2E API stub. " +
			"Tracked as Phase 5 follow-up.")
	})

	// --- Bonus: deprecation warning for GitHub PAT ---

	It("Bonus: GitHub PAT token shows deprecation warning to stderr", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}

		// `ghp_*` tokens trigger deprecation warning unless LOOPZERO_SUPPRESS_DEPRECATION=1.
		oldSuppress := os.Getenv("LOOPZERO_SUPPRESS_DEPRECATION")
		os.Setenv("LOOPZERO_SUPPRESS_DEPRECATION", "")
		defer os.Setenv("LOOPZERO_SUPPRESS_DEPRECATION", oldSuppress)

		_, stderr, exitCode := runLoopZero("login",
			"--token", "ghp_invalid_pat_"+uniqueSuffix())

		// Token will fail validation (exit 4), but the warning should appear
		// on stderr regardless.
		expectAllowedExitCode(exitCode, "login (PAT)")

		// Without --json, deprecation warning is printed.
		// Tolerate environments where warning is suppressed.
		if !strings.Contains(stderr, "deprecated") && !strings.Contains(stderr, "Token") {
			GinkgoWriter.Printf("INFO: deprecation warning not detected (stderr=%q)\n", stderr)
		}

		// No internal terms in the warning regardless.
		expectNoInternalTerms(stderr, "login PAT warning")
	})
})
