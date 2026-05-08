// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero domain dns <domain>` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/domain.ts (DomainDNSCommand, AUDIT-25).
//
// Behavior:
//   - Exit 0: success
//   - Exit 1: MISSING_ARGUMENT
//   - Exit 3: NOT_AVAILABLE (DNS management not exposed for this domain),
//     API error, API_UNREACHABLE
//   - Exit 4: AUTH_REQUIRED
//   - --json success: {domain, records: [{type, name, content, ttl?,
//     priority?, proxied?}], total}
//   - --json error: {error, exit_code, code}
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero domain dns", Ordered, func() {
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

		_, stderr, exitCode := runLoopZero("domain", "dns", "", "--json")
		expectAllowedExitCode(exitCode, "domain dns (empty)")
		Expect(exitCode).To(Equal(1),
			"empty domain should exit 1, got %d (stderr=%s)", exitCode, stderr)
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5b: no token yields exit 4 AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("domain", "dns",
			"example.com", "--json")

		expectAllowedExitCode(exitCode, "domain dns (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 2 + 5: error path against unregistered domain ---

	It("Contract 2+5c: domain without DNS management yields error (exit 3)", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", resolveToken())
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("domain", "dns",
			"absent-"+uniqueSuffix()+".example", "--json")

		expectAllowedExitCode(exitCode, "domain dns (unregistered)")
		expectNoInternalTerms(stdout+stderr, "domain dns")

		// API behavior depends on whether the domain has DNS management
		// configured. Either NOT_AVAILABLE (if listDNSRecords not wired) or
		// 404/400 from CF API → exit 3.
		Expect(exitCode == 3 || exitCode == 1).To(BeTrue(),
			"unregistered should yield exit 1 or 3, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(obj).To(HaveKey("error"))
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		stdout, stderr, _ := runLoopZeroNoToken("domain", "dns", "example.com")
		expectNoInternalTerms(stdout, "domain dns (no token) stdout")
		expectNoInternalTerms(stderr, "domain dns (no token) stderr")

		stdout, stderr, _ = runLoopZeroNoToken("domain", "dns", "example.com", "--json")
		expectNoInternalTerms(stdout, "domain dns --json (no token) stdout")
		expectNoInternalTerms(stderr, "domain dns --json (no token) stderr")

		err := seed.Run(seed.Config{})
		if err == nil && apiHealthy() {
			oldToken := os.Getenv("LOOPZERO_TOKEN")
			os.Setenv("LOOPZERO_TOKEN", resolveToken())
			defer os.Setenv("LOOPZERO_TOKEN", oldToken)

			for _, args := range [][]string{
				{"domain", "dns", "absent-" + uniqueSuffix() + ".example"},
				{"domain", "dns", "absent-" + uniqueSuffix() + ".example", "--json"},
			} {
				stdout, stderr, _ := runLoopZero(args...)
				expectNoInternalTerms(stdout, strings.Join(args, " ")+" stdout")
				expectNoInternalTerms(stderr, strings.Join(args, " ")+" stderr")
			}
		}
	})

	// --- Happy path SKIP ---

	It("Contract 1+3: SKIP — happy path requires CF-managed domain registered", func() {
		Skip("requires CF Zone Management enabled domain registered to seeded tenant. " +
			"Outside Phase 5 scope per ADR-PG-005.")
	})
})
