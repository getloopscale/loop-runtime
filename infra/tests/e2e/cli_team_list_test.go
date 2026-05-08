// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero team list` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/team-list.ts (AUDIT-24).
//
// Behavior:
//   - Exit 0: success (including empty list)
//   - Exit 1: API error, API unreachable
//   - Exit 4: not authenticated, 401
//   - --json success: {members: [{username, email, role, joined_at}], total}
//   - --json error: {error, exit_code, code}
//   - Does NOT require loopzero.yaml.
package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero team list", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 3 ---

	It("Contract 1+3: authenticated team list returns exit 0 + valid JSON shape", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred(), "seed failed")

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", resolveToken())
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("team", "list", "--json")

		expectAllowedExitCode(exitCode, "team list --json")
		expectNoInternalTerms(stdout+stderr, "team list --json")

		Expect(exitCode).To(Equal(0), "stderr=%s", stderr)
		Expect(looksLikeJSONLine(stdout)).To(BeTrue())

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())

		// Schema: members + total.
		membersRaw, exists := obj["members"]
		Expect(exists).To(BeTrue(), "members field missing: %v", obj)
		members, ok := membersRaw.([]interface{})
		Expect(ok).To(BeTrue())

		totalRaw, exists := obj["total"]
		Expect(exists).To(BeTrue())
		total, ok := totalRaw.(float64)
		Expect(ok).To(BeTrue())
		Expect(int(total)).To(Equal(len(members)))

		// Each member: username, email, role, joined_at
		for i, raw := range members {
			m, ok := raw.(map[string]interface{})
			Expect(ok).To(BeTrue(), "members[%d] should be object", i)
			Expect(m).To(HaveKey("username"))
			Expect(m).To(HaveKey("email"))
			Expect(m).To(HaveKey("role"))
			Expect(m).To(HaveKey("joined_at"))
		}

		// Seeded team should include the seed user.
		// (Soft check — empty team is permitted by contract; we just log.)
		if len(members) == 0 {
			GinkgoWriter.Printf("WARN: team list empty — seed may not have inserted membership\n")
		}
	})

	// --- Contract 2 + 5 ---

	It("Contract 2+5: no token yields exit 4 with AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("team", "list", "--json")

		expectAllowedExitCode(exitCode, "team list --json (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated team list should exit 4, got %d (stderr=%s)",
			exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 4 ---

	It("Contract 4: human + JSON paths leak no internal terms", func() {
		stdout, stderr, _ := runLoopZeroNoToken("team", "list")
		expectNoInternalTerms(stdout, "team list (no token) stdout")
		expectNoInternalTerms(stderr, "team list (no token) stderr")

		stdout, stderr, _ = runLoopZeroNoToken("team", "list", "--json")
		expectNoInternalTerms(stdout, "team list --json (no token) stdout")
		expectNoInternalTerms(stderr, "team list --json (no token) stderr")

		err := seed.Run(seed.Config{})
		if err == nil && apiHealthy() {
			oldToken := os.Getenv("LOOPZERO_TOKEN")
			os.Setenv("LOOPZERO_TOKEN", resolveToken())
			defer os.Setenv("LOOPZERO_TOKEN", oldToken)

			stdout, stderr, _ := runLoopZero("team", "list")
			expectNoInternalTerms(stdout, "team list (authenticated) stdout")
			expectNoInternalTerms(stderr, "team list (authenticated) stderr")
		}
	})
})
