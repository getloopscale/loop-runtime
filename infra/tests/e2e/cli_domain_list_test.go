// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero domain list` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/domain.ts (DomainListCommand).
// Validated manually in F1.6 (e2e-validation-final-2026-04-23.md).
//
// Behavior:
//   - Exit 0: success (including empty list)
//   - Exit 3: API error, API_UNREACHABLE, UNEXPECTED_ERROR (NOT 1, per AUDIT-25)
//   - Exit 4: not authenticated, 401
//   - --json success: {domains: [DomainStatus...], total, limit, offset}
//   - --json error: {error, exit_code, code}
//   - Optional filters: --project, --app, --env
package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero domain list", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 3 ---

	It("Contract 1+3: authenticated domain list returns exit 0 + valid JSON shape", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("domain", "list", "--json")

		expectAllowedExitCode(exitCode, "domain list --json")
		expectNoInternalTerms(stdout+stderr, "domain list --json")

		Expect(exitCode).To(Equal(0), "stderr=%s", stderr)
		Expect(looksLikeJSONLine(stdout)).To(BeTrue())

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())

		// Schema: domains array + total + limit + offset.
		domainsRaw, exists := obj["domains"]
		Expect(exists).To(BeTrue(), "domains field missing: %v", obj)
		domains, ok := domainsRaw.([]interface{})
		Expect(ok).To(BeTrue(), "domains should be array")

		Expect(obj).To(HaveKey("total"))
		// `limit` and `offset` are pagination fields per DomainListResponse.
		// They may be 0 for empty results — assert presence not value.
		Expect(obj).To(HaveKey("limit"))
		Expect(obj).To(HaveKey("offset"))

		// Each domain entry: id, domain, project, app, environment, status.
		for i, raw := range domains {
			d, ok := raw.(map[string]interface{})
			Expect(ok).To(BeTrue(), "domains[%d] should be object", i)
			Expect(d).To(HaveKey("id"))
			Expect(d).To(HaveKey("domain"))
			Expect(d).To(HaveKey("project"))
			Expect(d).To(HaveKey("app"))
			Expect(d).To(HaveKey("environment"))
			Expect(d).To(HaveKey("status"))
		}
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5: no token yields exit 4 AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("domain", "list", "--json")

		expectAllowedExitCode(exitCode, "domain list (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 4 ---

	It("Contract 4: human + JSON paths leak no internal terms", func() {
		stdout, stderr, _ := runLoopZeroNoToken("domain", "list")
		expectNoInternalTerms(stdout, "domain list (no token) stdout")
		expectNoInternalTerms(stderr, "domain list (no token) stderr")

		stdout, stderr, _ = runLoopZeroNoToken("domain", "list", "--json")
		expectNoInternalTerms(stdout, "domain list --json (no token) stdout")
		expectNoInternalTerms(stderr, "domain list --json (no token) stderr")

		err := seed.Run(seed.Config{})
		if err == nil && apiHealthy() {
			oldToken := os.Getenv("LOOPZERO_TOKEN")
			os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
			defer os.Setenv("LOOPZERO_TOKEN", oldToken)

			stdout, stderr, _ := runLoopZero("domain", "list")
			expectNoInternalTerms(stdout, "domain list (authenticated) stdout")
			expectNoInternalTerms(stderr, "domain list (authenticated) stderr")
		}
	})
})
