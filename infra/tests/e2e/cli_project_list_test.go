// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero project list` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/project-list.ts (AUDIT-24 history).
//
// Behavior:
//   - Exit 0: success (including empty list)
//   - Exit 1: API error, API unreachable
//   - Exit 4: not authenticated, 401
//   - --json success: {projects: [{id, name, team, created_at}], total}
//   - --json error: {error, exit_code, code}
//   - Does NOT require loopzero.yaml.
package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero project list", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 3 ---

	It("Contract 1+3: authenticated project list returns exit 0 + valid JSON shape", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred(), "seed failed")

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", resolveToken())
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("project", "list", "--json")

		expectAllowedExitCode(exitCode, "project list --json")
		expectNoInternalTerms(stdout+stderr, "project list --json")

		Expect(exitCode).To(Equal(0), "stderr=%s", stderr)
		Expect(looksLikeJSONLine(stdout)).To(BeTrue(),
			"--json should produce JSON, got: %q", stdout)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())

		// Schema: projects array + total number.
		projectsRaw, exists := obj["projects"]
		Expect(exists).To(BeTrue(), "projects field missing: %v", obj)
		projects, ok := projectsRaw.([]interface{})
		Expect(ok).To(BeTrue(), "projects should be array")

		totalRaw, exists := obj["total"]
		Expect(exists).To(BeTrue(), "total field missing")
		total, ok := totalRaw.(float64)
		Expect(ok).To(BeTrue())
		Expect(int(total)).To(Equal(len(projects)))

		// Each project has id, name, team, created_at.
		for i, raw := range projects {
			p, ok := raw.(map[string]interface{})
			Expect(ok).To(BeTrue(), "projects[%d] should be object", i)
			Expect(p).To(HaveKey("id"))
			Expect(p).To(HaveKey("name"))
			Expect(p).To(HaveKey("team"))
			Expect(p).To(HaveKey("created_at"))
		}
	})

	// --- Contract 2 + 5 ---

	It("Contract 2+5: no token yields exit 4 with AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("project", "list", "--json")

		expectAllowedExitCode(exitCode, "project list --json (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 4 ---

	It("Contract 4: human + JSON paths leak no internal terms", func() {
		stdout, stderr, _ := runLoopZeroNoToken("project", "list")
		expectNoInternalTerms(stdout, "project list (no token) stdout")
		expectNoInternalTerms(stderr, "project list (no token) stderr")

		stdout, stderr, _ = runLoopZeroNoToken("project", "list", "--json")
		expectNoInternalTerms(stdout, "project list --json (no token) stdout")
		expectNoInternalTerms(stderr, "project list --json (no token) stderr")

		err := seed.Run(seed.Config{})
		if err == nil && apiHealthy() {
			oldToken := os.Getenv("LOOPZERO_TOKEN")
			os.Setenv("LOOPZERO_TOKEN", resolveToken())
			defer os.Setenv("LOOPZERO_TOKEN", oldToken)

			stdout, stderr, _ := runLoopZero("project", "list")
			expectNoInternalTerms(stdout, "project list (authenticated) stdout")
			expectNoInternalTerms(stderr, "project list (authenticated) stderr")
		}
	})
})
