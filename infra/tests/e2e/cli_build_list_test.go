// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero build list` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/build-list.ts (AUDIT-5, AUDIT-24).
//
// Behavior:
//   - Exit 0: success (including empty list)
//   - Exit 1: missing loopzero.yaml (MISSING_CONFIG), invalid loopzero.yaml
//     (INVALID_CONFIG), API error, API unreachable
//   - Exit 4: not authenticated, 401
//   - --json success: {builds: [{build_id, project, environment, status,
//     commit_sha, created_at, duration_seconds?}], total}
//   - --json error: {error, exit_code, code}
//   - REQUIRES loopzero.yaml in cwd (project name comes from it).
package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero build list", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 3 ---

	It("Contract 1+3: authenticated build list with loopzero.yaml returns exit 0 + valid JSON", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		// build list needs loopzero.yaml in cwd. Use hello-server fixture.
		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", resolveToken())
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZeroInDir(fixtureDir, "build", "list", "--json")

		expectAllowedExitCode(exitCode, "build list --json")
		expectNoInternalTerms(stdout+stderr, "build list --json")

		Expect(exitCode).To(Equal(0), "stderr=%s", stderr)
		Expect(looksLikeJSONLine(stdout)).To(BeTrue())

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())

		// Schema: builds + total.
		buildsRaw, exists := obj["builds"]
		Expect(exists).To(BeTrue(), "builds field missing: %v", obj)
		builds, ok := buildsRaw.([]interface{})
		Expect(ok).To(BeTrue())

		Expect(obj).To(HaveKey("total"))

		// If there are builds, validate fields per AUDIT-5 contract.
		for i, raw := range builds {
			b, ok := raw.(map[string]interface{})
			Expect(ok).To(BeTrue(), "builds[%d] should be object", i)
			Expect(b).To(HaveKey("build_id"))
			Expect(b).To(HaveKey("project"))
			Expect(b).To(HaveKey("environment"))
			Expect(b).To(HaveKey("status"))
			Expect(b).To(HaveKey("commit_sha"))
			Expect(b).To(HaveKey("created_at"))
		}
	})

	// --- Contract 2 + 5: no loopzero.yaml ---

	It("Contract 2+5a: missing loopzero.yaml yields exit 1 MISSING_CONFIG", func() {
		// Use temp dir without loopzero.yaml — but auth must be valid first.
		err := seed.Run(seed.Config{})
		if err != nil || !apiHealthy() {
			Skip("seed/api not available")
		}

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", resolveToken())
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		tmpDir, err := os.MkdirTemp("", "loopzero-e2e-buildlist-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		stdout, stderr, exitCode := runLoopZeroInDir(tmpDir, "build", "list", "--json")

		expectAllowedExitCode(exitCode, "build list --json (no yaml)")
		Expect(exitCode).To(Equal(1),
			"missing loopzero.yaml should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("MISSING_CONFIG"))
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5b: no token yields exit 4 AUTH_REQUIRED (before yaml check)", func() {
		// Even without loopzero.yaml present, auth check happens first.
		tmpDir, err := os.MkdirTemp("", "loopzero-e2e-buildlist-noauth-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		// Manually run with empty LOOPZERO_TOKEN in tmpDir.
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZeroInDir(tmpDir, "build", "list", "--json")

		expectAllowedExitCode(exitCode, "build list (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 4 ---

	It("Contract 4: error and success paths leak no internal terms", func() {
		// No-token path.
		tmpDir, _ := os.MkdirTemp("", "loopzero-e2e-buildlist-leak-")
		defer os.RemoveAll(tmpDir)

		stdout, stderr, _ := runLoopZeroInDir(tmpDir, "build", "list")
		expectNoInternalTerms(stdout, "build list (no yaml/no token) stdout")
		expectNoInternalTerms(stderr, "build list (no yaml/no token) stderr")

		stdout, stderr, _ = runLoopZeroInDir(tmpDir, "build", "list", "--json")
		expectNoInternalTerms(stdout, "build list --json (no yaml) stdout")
		expectNoInternalTerms(stderr, "build list --json (no yaml) stderr")

		// Authenticated + loopzero.yaml present path.
		err := seed.Run(seed.Config{})
		if err == nil && apiHealthy() {
			fixtureDir := copyFixture("hello-server")
			defer os.RemoveAll(fixtureDir)

			oldToken := os.Getenv("LOOPZERO_TOKEN")
			os.Setenv("LOOPZERO_TOKEN", resolveToken())
			defer os.Setenv("LOOPZERO_TOKEN", oldToken)

			stdout, stderr, _ := runLoopZeroInDir(fixtureDir, "build", "list")
			expectNoInternalTerms(stdout, "build list (authenticated) stdout")
			expectNoInternalTerms(stderr, "build list (authenticated) stderr")
		}
	})
})
