// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero domain remove <domain>` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/domain.ts (DomainRemoveCommand, AUDIT-25).
//
// Behavior:
//   - Exit 0: success
//   - Exit 1: MISSING_ARGUMENT, **CONFIRMATION_REQUIRED** (must --yes),
//     NOT_FOUND (404)
//   - Exit 3: API error, API_UNREACHABLE, UNEXPECTED_ERROR
//   - Exit 4: AUTH_REQUIRED
//   - --json success: {ok, domain, status}
//   - --json error: {error, exit_code, code}
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero domain remove", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 2 + 5: confirmation required ---

	It("Contract 2+5a: no --yes yields exit 1 CONFIRMATION_REQUIRED", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("domain", "remove",
			"example.com", "--json")

		expectAllowedExitCode(exitCode, "domain remove (no --yes)")
		Expect(exitCode).To(Equal(1),
			"missing --yes should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("CONFIRMATION_REQUIRED"))
	})

	// --- Contract 2 + 5: 404 ---

	It("Contract 2+5b: nonexistent domain yields exit 1 NOT_FOUND", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", resolveToken())
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("domain", "remove",
			"absent-"+uniqueSuffix()+".example", "--yes", "--json")

		expectAllowedExitCode(exitCode, "domain remove (404)")
		Expect(exitCode).To(Equal(1),
			"missing domain should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("NOT_FOUND"))
	})

	// --- Contract 2 + 5: missing argument ---

	It("Contract 2+5c: empty domain yields exit 1 MISSING_ARGUMENT", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		_, stderr, exitCode := runLoopZero("domain", "remove", "", "--yes", "--json")
		expectAllowedExitCode(exitCode, "domain remove (empty)")
		Expect(exitCode).To(Equal(1),
			"empty domain should exit 1, got %d (stderr=%s)", exitCode, stderr)
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5d: no token yields exit 4 AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("domain", "remove",
			"example.com", "--yes", "--json")

		expectAllowedExitCode(exitCode, "domain remove (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		for _, args := range [][]string{
			{"domain", "remove", "example.com"},
			{"domain", "remove", "example.com", "--json"},
			{"domain", "remove", "example.com", "--yes"},
			{"domain", "remove", "example.com", "--yes", "--json"},
		} {
			stdout, stderr, _ := runLoopZero(args...)
			expectNoInternalTerms(stdout, strings.Join(args, " ")+" stdout")
			expectNoInternalTerms(stderr, strings.Join(args, " ")+" stderr")
		}

		stdout, stderr, _ := runLoopZeroNoToken("domain", "remove", "x.example", "--yes")
		expectNoInternalTerms(stdout, "domain remove (no token)")
		expectNoInternalTerms(stderr, "domain remove (no token)")
	})

	// --- Happy path SKIP ---

	It("Contract 1+3: SKIP — happy path requires registered domain", func() {
		Skip("happy path requires a domain registered to the seeded tenant. " +
			"Domain registration depends on CF Zone Management which is " +
			"environment-specific and exceeds Phase 5 scope. Tracked.")
	})
})
