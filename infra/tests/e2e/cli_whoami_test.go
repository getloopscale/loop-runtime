// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero whoami` covering the 5 CLI contracts:
//
//  1. Happy path: authenticated user gets exit 0 + JSON with authenticated=true
//  2. Failure path: no token yields exit 4
//  3. --json schema: shape matches whoami contract (authenticated, email, team)
//  4. Internal terms: no Rule 3 leak
//  5. Exit codes: only 0 and 4 (never 2/3/5)
//
// Source-of-truth: cli/src/commands/whoami.ts and AUDIT-10 / AUDIT-22 history.
package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero whoami", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 3: happy path + --json schema ---

	It("Contract 1+3: returns exit 0 and authenticated=true with valid token", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred(), "seed failed")

		// Inject token from seed for this test only.
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("whoami", "--json")

		expectAllowedExitCode(exitCode, "whoami --json (authenticated)")
		expectNoInternalTerms(stdout+stderr, "whoami --json")

		Expect(exitCode).To(Equal(0), "stderr=%s", stderr)
		Expect(looksLikeJSONLine(stdout)).To(BeTrue(),
			"--json should produce a single JSON line, got: %q", stdout)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred(), "JSON parse failed: %s", stdout)

		// Schema contract: authenticated must be present and true.
		auth, ok := jsonBoolField(obj, "authenticated")
		Expect(ok).To(BeTrue(), "authenticated field missing or wrong type")
		Expect(auth).To(BeTrue(), "expected authenticated=true")

		// At least one of email/username must be present (service accounts
		// have no email; AUDIT-10).
		identity := jsonStringField(obj, "email")
		if identity == "" {
			identity = jsonStringField(obj, "username")
		}
		Expect(identity).NotTo(BeEmpty(),
			"either email or username should be present: %v", obj)
	})

	// --- Contract 2 + 5: failure path + exit codes ---

	It("Contract 2+5: returns exit 4 when no token is present", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("whoami", "--json")

		// Must use only allowed exit codes; for whoami specifically: 0 or 4.
		expectAllowedExitCode(exitCode, "whoami --json (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated whoami should exit 4, got %d (stderr=%s)",
			exitCode, stderr)

		// JSON output should still parse.
		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred(), "JSON parse failed: stdout=%s stderr=%s", stdout, stderr)

		auth, ok := jsonBoolField(obj, "authenticated")
		Expect(ok).To(BeTrue(), "authenticated field missing")
		Expect(auth).To(BeFalse(), "expected authenticated=false")
	})

	// --- Contract 4: internal terms (Rule 3) ---

	It("Contract 4: stdout/stderr contain no internal terms in any path", func() {
		// Run all observable paths and check each:
		// a) human mode authenticated
		// b) human mode unauthenticated
		// c) JSON mode unauthenticated (covered above too — re-checked here)
		err := seed.Run(seed.Config{})
		if err == nil && apiHealthy() {
			oldToken := os.Getenv("LOOPZERO_TOKEN")
			os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
			stdout, stderr, _ := runLoopZero("whoami")
			expectNoInternalTerms(stdout, "whoami human (authenticated) stdout")
			expectNoInternalTerms(stderr, "whoami human (authenticated) stderr")
			os.Setenv("LOOPZERO_TOKEN", oldToken)
		}

		stdout, stderr, _ := runLoopZeroNoToken("whoami")
		expectNoInternalTerms(stdout, "whoami human (no token) stdout")
		expectNoInternalTerms(stderr, "whoami human (no token) stderr")

		stdout, stderr, _ = runLoopZeroNoToken("whoami", "--json")
		expectNoInternalTerms(stdout, "whoami --json (no token) stdout")
		expectNoInternalTerms(stderr, "whoami --json (no token) stderr")
	})
})
