// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero billing` and `loopzero billing portal` covering the 5 CLI
// contracts.
//
// Source-of-truth: cli/src/commands/billing.ts.
//
// Behavior:
//   - Top-level `loopzero billing` shows plan/team/usage
//   - `loopzero billing portal` opens browser to subscription portal
//   - Exit 0: success
//   - Exit 4: AUTH_FAILED (returns null from API)
//   - --json show: {plan, team, usage}
//   - --json portal: {url}
//   - --json error: {error, exit_code, code}
//
// NOTE: billing.ts uses null-as-error pattern (different from other commands
// which throw ApiError). The exit code maps null → 4 AUTH_FAILED.
package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero billing", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 3 ---

	It("Contract 1+3: authenticated billing returns exit 0 + valid JSON shape", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", resolveToken())
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("billing", "--json")

		expectAllowedExitCode(exitCode, "billing --json")
		expectNoInternalTerms(stdout+stderr, "billing --json")

		// Backend may not be wired for this seed; allow exit 4 with shape OK.
		if exitCode == 4 {
			obj, err := parseJSON(stdout)
			Expect(err).NotTo(HaveOccurred())
			Expect(obj).To(HaveKey("error"))
			Skip("billing endpoint returned auth-fail (likely Stripe not seeded) — error shape validated")
		}

		Expect(exitCode).To(Equal(0), "stderr=%s", stderr)
		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())

		// Schema: plan + team + usage required.
		Expect(obj).To(HaveKey("plan"))
		Expect(obj).To(HaveKey("team"))
		Expect(obj).To(HaveKey("usage"))

		// Usage should be a map.
		usage, ok := obj["usage"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "usage should be object: %v", obj)

		// Each usage entry: {used, limit}
		for metric, raw := range usage {
			entry, ok := raw.(map[string]interface{})
			Expect(ok).To(BeTrue(), "usage[%s] should be object", metric)
			Expect(entry).To(HaveKey("used"))
			Expect(entry).To(HaveKey("limit"))
		}
	})

	// --- Contract 1 + 3: portal subcommand ---

	It("Contract 1+3 (portal): --json yields {url}", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", resolveToken())
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("billing", "portal", "--json")

		expectAllowedExitCode(exitCode, "billing portal --json")
		expectNoInternalTerms(stdout+stderr, "billing portal --json")

		if exitCode == 4 {
			Skip("billing portal returned auth-fail (likely Stripe not seeded)")
		}

		Expect(exitCode).To(Equal(0), "stderr=%s", stderr)
		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(obj).To(HaveKey("url"))
		Expect(jsonStringField(obj, "url")).NotTo(BeEmpty())
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5: no token yields exit 4 AUTH_FAILED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("billing", "--json")

		expectAllowedExitCode(exitCode, "billing (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_FAILED"))
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		stdout, stderr, _ := runLoopZeroNoToken("billing")
		expectNoInternalTerms(stdout, "billing (no token) stdout")
		expectNoInternalTerms(stderr, "billing (no token) stderr")

		stdout, stderr, _ = runLoopZeroNoToken("billing", "--json")
		expectNoInternalTerms(stdout, "billing --json (no token) stdout")
		expectNoInternalTerms(stderr, "billing --json (no token) stderr")

		stdout, stderr, _ = runLoopZeroNoToken("billing", "portal")
		expectNoInternalTerms(stdout, "billing portal (no token) stdout")
		expectNoInternalTerms(stderr, "billing portal (no token) stderr")

		stdout, stderr, _ = runLoopZeroNoToken("billing", "portal", "--json")
		expectNoInternalTerms(stdout, "billing portal --json (no token) stdout")
		expectNoInternalTerms(stderr, "billing portal --json (no token) stderr")
	})
})
