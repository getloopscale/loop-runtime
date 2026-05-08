// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero domain verify <domain>` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/domain.ts (DomainVerifyCommand, AUDIT-25).
//
// Behavior:
//   - Exit 0: success (verified, provisioning, active, OR pending — pending is
//     "not an error, just not ready yet")
//   - Exit 1: MISSING_ARGUMENT (empty domain)
//   - Exit 3: API error (400/5xx via handleApiError → exit 3),
//     API_UNREACHABLE, UNEXPECTED_ERROR
//   - Exit 4: AUTH_REQUIRED
//   - --json success: {domain, status, verified_at?, verification_error?,
//     hint?, message?}
//   - --json error: {error, exit_code, code}
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero domain verify", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 2 + 5: missing argument ---

	It("Contract 2+5a: empty domain yields exit 1 MISSING_ARGUMENT", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		_, stderr, exitCode := runLoopZero("domain", "verify", "", "--json")
		expectAllowedExitCode(exitCode, "domain verify (empty)")
		Expect(exitCode).To(Equal(1),
			"empty domain should exit 1, got %d (stderr=%s)", exitCode, stderr)
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5b: no token yields exit 4 AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("domain", "verify",
			"example.com", "--json")

		expectAllowedExitCode(exitCode, "domain verify (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 2 + 5: nonexistent domain (handleApiError → exit 3) ---

	It("Contract 2+5c: nonexistent domain yields exit 3", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", resolveToken())
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("domain", "verify",
			"absent-"+uniqueSuffix()+".example", "--json")

		expectAllowedExitCode(exitCode, "domain verify (404)")
		expectNoInternalTerms(stdout+stderr, "domain verify (404)")

		// API may return 404 or 400 — domain.ts maps both via handleApiError
		// to exit 3. Some setups may surface as exit 3 with code from server.
		Expect(exitCode).To(Equal(3),
			"nonexistent domain should exit 3, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(obj).To(HaveKey("error"))
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		stdout, stderr, _ := runLoopZeroNoToken("domain", "verify", "example.com")
		expectNoInternalTerms(stdout, "domain verify (no token) stdout")
		expectNoInternalTerms(stderr, "domain verify (no token) stderr")

		stdout, stderr, _ = runLoopZeroNoToken("domain", "verify", "example.com", "--json")
		expectNoInternalTerms(stdout, "domain verify --json (no token) stdout")
		expectNoInternalTerms(stderr, "domain verify --json (no token) stderr")

		err := seed.Run(seed.Config{})
		if err == nil && apiHealthy() {
			oldToken := os.Getenv("LOOPZERO_TOKEN")
			os.Setenv("LOOPZERO_TOKEN", resolveToken())
			defer os.Setenv("LOOPZERO_TOKEN", oldToken)

			for _, args := range [][]string{
				{"domain", "verify", "absent-" + uniqueSuffix() + ".example"},
				{"domain", "verify", "absent-" + uniqueSuffix() + ".example", "--json"},
			} {
				stdout, stderr, _ := runLoopZero(args...)
				expectNoInternalTerms(stdout, strings.Join(args, " ")+" stdout")
				expectNoInternalTerms(stderr, strings.Join(args, " ")+" stderr")
			}
		}
	})

	// --- Happy path SKIP ---

	It("Contract 1+3: SKIP — happy path requires registered domain with DNS ready", func() {
		Skip("happy path requires (a) registered domain + (b) DNS configured + " +
			"(c) CF/cert-manager state. Outside Phase 5 scope per ADR-PG-005.")
	})
})
