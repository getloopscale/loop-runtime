// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero domain status <domain>` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/domain.ts (DomainStatusCommand, AUDIT-25).
// Validated manually in F1.6 (e2e-validation-final-2026-04-23.md).
//
// Behavior:
//   - Exit 0: success
//   - Exit 1: missing name (MISSING_ARGUMENT), 404 (NOT_FOUND)
//   - Exit 3: API error, API_UNREACHABLE, UNEXPECTED_ERROR
//   - Exit 4: not authenticated, 401
//   - --json success: {id, domain, project, app, environment, status,
//     tls?, verification?, cname_target?, nameservers?, cf_managed?,
//     created_at, verified_at?}
//   - --json error: {error, exit_code, code}
package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero domain status", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 2 + 5: 404 ---
	//
	// We start with the 404 path because (a) it doesn't require a registered
	// domain (avoiding seed dependency) and (b) it exercises the error-shape
	// contract that exists for every domain command.

	It("Contract 2+5a: nonexistent domain yields exit 1 NOT_FOUND", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", resolveToken())
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("domain", "status",
			"definitely-not-registered-"+uniqueSuffix()+".example", "--json")

		expectAllowedExitCode(exitCode, "domain status (404)")
		Expect(exitCode).To(Equal(1),
			"missing domain should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("NOT_FOUND"))
	})

	// --- Contract 2 + 5: missing argument ---

	It("Contract 2+5b: missing domain argument yields exit 1 MISSING_ARGUMENT", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		// Empty arg.
		stdout, stderr, exitCode := runLoopZero("domain", "status", "", "--json")
		expectAllowedExitCode(exitCode, "domain status (empty arg)")
		Expect(exitCode).To(Equal(1),
			"empty domain should exit 1, got %d (stderr=%s)", exitCode, stderr)

		// Code is MISSING_ARGUMENT for our handler, but Commander may handle
		// missing positional first depending on parse order — accept either.
		if obj, err := parseJSON(stdout); err == nil {
			code := jsonStringField(obj, "code")
			Expect(code == "MISSING_ARGUMENT" || code == "COMMAND_VALIDATION_ERROR").To(BeTrue(),
				"unexpected code %q", code)
		}
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5c: no token yields exit 4 AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("domain", "status",
			"example.com", "--json")

		expectAllowedExitCode(exitCode, "domain status (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		stdout, stderr, _ := runLoopZeroNoToken("domain", "status", "example.com")
		expectNoInternalTerms(stdout, "domain status (no token) stdout")
		expectNoInternalTerms(stderr, "domain status (no token) stderr")

		stdout, stderr, _ = runLoopZeroNoToken("domain", "status", "example.com", "--json")
		expectNoInternalTerms(stdout, "domain status --json (no token) stdout")
		expectNoInternalTerms(stderr, "domain status --json (no token) stderr")

		// 404 path on real API.
		err := seed.Run(seed.Config{})
		if err == nil && apiHealthy() {
			oldToken := os.Getenv("LOOPZERO_TOKEN")
			os.Setenv("LOOPZERO_TOKEN", resolveToken())
			defer os.Setenv("LOOPZERO_TOKEN", oldToken)

			stdout, stderr, _ := runLoopZero("domain", "status",
				"absent-"+uniqueSuffix()+".example", "--json")
			expectNoInternalTerms(stdout, "domain status (404) stdout")
			expectNoInternalTerms(stderr, "domain status (404) stderr")
		}
	})
})
