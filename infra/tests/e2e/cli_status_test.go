// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero status` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/status.ts (AUDIT-2, AUDIT-24, AUDIT-33).
//
// Behavior:
//   - Exit 0: success (with or without builds/releases)
//   - Exit 1: MISSING_CONFIG (no loopzero.yaml), INVALID_CONFIG, ApiError mapped
//   - Exit 3: API_UNREACHABLE, UNEXPECTED_ERROR
//   - Exit 4: AUTH_REQUIRED
//   - --json success: {environment, last_build?, active_release?,
//     sync_status?, url?, scaling?}
//   - --json error: {error, exit_code, code}
//
// CRITICAL CONTRACTS (regressions caught here):
//   - AUDIT-2: last_build uses `id` field, not `build_id`
//   - AUDIT-33: active_release uses `{id, status, git_commit, created_at}`,
//     NOT `{release_id, state, activated_at}`
//   - sync_status mapping: Synced→"Up to date", OutOfSync→"Update pending",
//     Progressing→"Deploying"
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero status", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 3 + AUDIT-2/33 contract ---

	It("Contract 1+3 + AUDIT-2/33: status returns exit 0 + valid JSON shape", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", resolveToken())
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZeroInDir(fixtureDir, "status", "--json")

		expectAllowedExitCode(exitCode, "status --json")
		expectNoInternalTerms(stdout+stderr, "status --json")

		Expect(exitCode).To(Equal(0), "stderr=%s", stderr)
		Expect(looksLikeJSONLine(stdout)).To(BeTrue())

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())

		// `environment` is always present.
		Expect(obj).To(HaveKey("environment"))

		// AUDIT-2 contract: if last_build present, must use `id` field.
		if lb, ok := obj["last_build"].(map[string]interface{}); ok {
			Expect(lb).To(HaveKey("id"),
				"AUDIT-2 contract violation: last_build must use 'id', not 'build_id'")
			Expect(lb).NotTo(HaveKey("build_id"),
				"AUDIT-2 contract violation: last_build still has 'build_id' field")
			Expect(lb).To(HaveKey("status"))
		}

		// AUDIT-33 contract: if active_release present (and not null), must
		// use `{id, status, git_commit, created_at}`, not `{release_id, ...}`.
		if ar, ok := obj["active_release"].(map[string]interface{}); ok {
			Expect(ar).To(HaveKey("id"),
				"AUDIT-33 contract violation: active_release must use 'id', not 'release_id'")
			Expect(ar).NotTo(HaveKey("release_id"))
			Expect(ar).To(HaveKey("status"))
			Expect(ar).NotTo(HaveKey("state"),
				"AUDIT-33 contract violation: active_release still has 'state'")
			Expect(ar).To(HaveKey("created_at"))
			Expect(ar).NotTo(HaveKey("activated_at"))
		}
	})

	// --- Contract 2 + 5: missing loopzero.yaml ---

	It("Contract 2+5a: missing loopzero.yaml yields exit 1 MISSING_CONFIG", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		tmpDir, err := os.MkdirTemp("", "loopzero-e2e-status-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		stdout, stderr, exitCode := runLoopZeroInDir(tmpDir, "status", "--json")

		expectAllowedExitCode(exitCode, "status (no yaml)")
		Expect(exitCode).To(Equal(1),
			"missing yaml should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("MISSING_CONFIG"))
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5b: no token yields exit 4 AUTH_REQUIRED", func() {
		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZeroInDir(fixtureDir, "status", "--json")

		expectAllowedExitCode(exitCode, "status (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		// Sync status mapping must NOT show internal `Synced`/`OutOfSync`
		// to user — those are ArgoCD-isms (Rule 3 territory). Mapped to
		// product language: "Up to date", "Update pending", etc.
		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		for _, args := range [][]string{
			{"status"},
			{"status", "--json"},
		} {
			stdout, stderr, _ := runLoopZeroInDir(fixtureDir, args...)
			expectNoInternalTerms(stdout, strings.Join(args, " ")+" stdout")
			expectNoInternalTerms(stderr, strings.Join(args, " ")+" stderr")
		}

		// Authenticated path — sync_status display must not leak.
		err := seed.Run(seed.Config{})
		if err == nil && apiHealthy() {
			os.Setenv("LOOPZERO_TOKEN", resolveToken())

			for _, args := range [][]string{
				{"status"},
				{"status", "--json"},
				{"status", "--env", "production"},
			} {
				stdout, stderr, _ := runLoopZeroInDir(fixtureDir, args...)
				expectNoInternalTerms(stdout, strings.Join(args, " ")+" stdout")
				expectNoInternalTerms(stderr, strings.Join(args, " ")+" stderr")
			}
		}
	})
})
