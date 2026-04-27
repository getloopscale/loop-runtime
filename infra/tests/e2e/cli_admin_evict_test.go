// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero admin tenants evict` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/admin-evict.ts.
//
// Behavior:
//   - Exit 0: success or dry-run listed matches
//   - Exit 1: missing flags (no --pattern and no --ids), broad pattern
//     ('%' or 'tn-%'), aborted (user typed != 'yes')
//   - Exit 4: missing LOOPZERO_SERVICE_AUTH (auth, not loopzero-token!)
//   - Exit 5: partial failure (job not accepted)
//   - --json: serialized AdminEvictResponse
//
// CRITICAL: this command uses LOOPZERO_SERVICE_AUTH (operator-only), NOT
// LOOPZERO_TOKEN. Auth model is different from regular commands.
package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CLI: loopzero admin tenants evict", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 2 + 5: missing LOOPZERO_SERVICE_AUTH ---

	It("Contract 2+5a: missing LOOPZERO_SERVICE_AUTH yields exit 4", func() {
		oldAuth := os.Getenv("LOOPZERO_SERVICE_AUTH")
		os.Setenv("LOOPZERO_SERVICE_AUTH", "")
		defer os.Setenv("LOOPZERO_SERVICE_AUTH", oldAuth)

		_, stderr, exitCode := runLoopZero("admin", "tenants", "evict",
			"--pattern", "tn-test-%", "--dry-run", "--json")

		expectAllowedExitCode(exitCode, "admin evict (no auth)")
		Expect(exitCode).To(Equal(4),
			"missing LOOPZERO_SERVICE_AUTH should exit 4, got %d (stderr=%s)",
			exitCode, stderr)
		// Note: this command uses writeError directly, not JSON shape, when
		// auth is missing. Validate exit code only here.
		expectNoInternalTerms(stderr, "admin evict (no auth)")
	})

	// --- Contract 2 + 5: missing flags ---

	It("Contract 2+5b: no --pattern and no --ids yields exit 1", func() {
		oldAuth := os.Getenv("LOOPZERO_SERVICE_AUTH")
		os.Setenv("LOOPZERO_SERVICE_AUTH", "dummy-service-auth")
		defer os.Setenv("LOOPZERO_SERVICE_AUTH", oldAuth)

		_, stderr, exitCode := runLoopZero("admin", "tenants", "evict",
			"--dry-run", "--json")

		expectAllowedExitCode(exitCode, "admin evict (no flags)")
		Expect(exitCode).To(Equal(1),
			"no --pattern/--ids should exit 1, got %d (stderr=%s)",
			exitCode, stderr)
		expectNoInternalTerms(stderr, "admin evict (no flags)")
	})

	// --- CRITICAL: broad pattern guard ---

	It("CRITICAL: pattern '%' or 'tn-%' is rejected (too broad — safety)", func() {
		oldAuth := os.Getenv("LOOPZERO_SERVICE_AUTH")
		os.Setenv("LOOPZERO_SERVICE_AUTH", "dummy-service-auth")
		defer os.Setenv("LOOPZERO_SERVICE_AUTH", oldAuth)

		for _, broad := range []string{"%", "tn-%"} {
			_, stderr, exitCode := runLoopZero("admin", "tenants", "evict",
				"--pattern", broad, "--dry-run", "--json")

			expectAllowedExitCode(exitCode, "admin evict (broad pattern)")
			Expect(exitCode).To(Equal(1),
				"broad pattern %q should exit 1, got %d (stderr=%s)",
				broad, exitCode, stderr)
			expectNoInternalTerms(stderr, "admin evict broad")
		}
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		oldAuth := os.Getenv("LOOPZERO_SERVICE_AUTH")
		defer os.Setenv("LOOPZERO_SERVICE_AUTH", oldAuth)

		// no auth
		os.Setenv("LOOPZERO_SERVICE_AUTH", "")
		stdout, stderr, _ := runLoopZero("admin", "tenants", "evict",
			"--pattern", "tn-x-%", "--dry-run")
		expectNoInternalTerms(stdout, "admin evict (no auth) stdout")
		expectNoInternalTerms(stderr, "admin evict (no auth) stderr")

		// missing flags
		os.Setenv("LOOPZERO_SERVICE_AUTH", "dummy")
		stdout, stderr, _ = runLoopZero("admin", "tenants", "evict", "--dry-run")
		expectNoInternalTerms(stdout, "admin evict (no flags) stdout")
		expectNoInternalTerms(stderr, "admin evict (no flags) stderr")

		// broad pattern
		stdout, stderr, _ = runLoopZero("admin", "tenants", "evict",
			"--pattern", "%", "--dry-run")
		expectNoInternalTerms(stdout, "admin evict broad stdout")
		expectNoInternalTerms(stderr, "admin evict broad stderr")
	})

	// --- Happy path SKIP ---

	It("Contract 1+3: SKIP — happy path requires real LOOPZERO_SERVICE_AUTH + tenants to evict", func() {
		Skip("happy path requires real operator auth + at least one tenant " +
			"matching the pattern. Mutation against shared seed is destructive — " +
			"would break other tests. Tracked as Phase 5 follow-up.")
	})
})
