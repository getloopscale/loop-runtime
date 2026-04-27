// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero connect` and `loopzero disconnect` covering the 5 CLI
// contracts.
//
// Source-of-truth: cli/src/commands/connect.ts (AUDIT-25).
//
// connect behavior:
//   - Auto-detects repo from git remote
//   - Exit 0: connected (or already connected — depends on backend semantics)
//   - Exit 1: MISSING_CONFIG (no loopzero.yaml), MISSING_REPO (cannot detect)
//   - Exit 3: REPO_ALREADY_LINKED (409), API_UNREACHABLE, UNEXPECTED_ERROR
//   - Exit 4: AUTH_REQUIRED
//
// disconnect behavior:
//   - Exit 0: success
//   - Exit 1: MISSING_CONFIG, NOT_CONNECTED (404)
//   - Exit 3: API_UNREACHABLE, UNEXPECTED_ERROR
//   - Exit 4: AUTH_REQUIRED
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero connect", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 2 + 5: no loopzero.yaml ---

	It("Contract 2+5a: missing loopzero.yaml yields exit 1 MISSING_CONFIG", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		tmpDir, err := os.MkdirTemp("", "loopzero-e2e-connect-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		stdout, stderr, exitCode := runLoopZeroInDir(tmpDir,
			"connect", "--repo", "owner/repo", "--json")

		expectAllowedExitCode(exitCode, "connect (no yaml)")
		Expect(exitCode).To(Equal(1),
			"missing yaml should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("MISSING_CONFIG"))
	})

	// --- Contract 2 + 5: no repo detected, no --repo flag ---

	It("Contract 2+5b: cannot detect repo + no --repo yields exit 1 MISSING_REPO", func() {
		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		// Ensure no .git directory exists in the fixture dir so git remote
		// detection fails. (copyFixture should not include one but be safe.)
		os.RemoveAll(fixtureDir + "/.git")

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZeroInDir(fixtureDir, "connect", "--json")

		expectAllowedExitCode(exitCode, "connect (no repo)")
		Expect(exitCode).To(Equal(1),
			"missing repo should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("MISSING_REPO"))
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5c: no token yields exit 4 AUTH_REQUIRED", func() {
		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZeroInDir(fixtureDir,
			"connect", "--repo", "owner/repo", "--json")

		expectAllowedExitCode(exitCode, "connect (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)
		os.RemoveAll(fixtureDir + "/.git")

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		for _, args := range [][]string{
			{"connect"},
			{"connect", "--json"},
			{"connect", "--repo", "owner/repo"},
			{"connect", "--repo", "owner/repo", "--json"},
		} {
			stdout, stderr, _ := runLoopZeroInDir(fixtureDir, args...)
			expectNoInternalTerms(stdout, strings.Join(args, " ")+" stdout")
			expectNoInternalTerms(stderr, strings.Join(args, " ")+" stderr")
		}
	})

	// --- Happy path SKIP ---

	It("Contract 1+3: SKIP — happy path requires GitHub App installation", func() {
		Skip("happy path requires (a) GitHub App installed on a real repo + " +
			"(b) installation_id known + (c) cleanup. Tests against " +
			"getloopscale/loopzero-repotest are documented in production, but the seed " +
			"environment doesn't replicate that. Tracked.")
	})
})

var _ = Describe("CLI: loopzero disconnect", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 2 + 5: no loopzero.yaml ---

	It("Contract 2+5a: missing loopzero.yaml yields exit 1 MISSING_CONFIG", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		tmpDir, err := os.MkdirTemp("", "loopzero-e2e-disconnect-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		stdout, stderr, exitCode := runLoopZeroInDir(tmpDir, "disconnect", "--json")

		expectAllowedExitCode(exitCode, "disconnect (no yaml)")
		Expect(exitCode).To(Equal(1),
			"missing yaml should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("MISSING_CONFIG"))
	})

	// --- Contract 2 + 5: not connected (404) ---

	It("Contract 2+5b: project not connected yields exit 1 NOT_CONNECTED", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZeroInDir(fixtureDir, "disconnect", "--json")

		expectAllowedExitCode(exitCode, "disconnect (not connected)")
		// If the seeded project happens to be connected, exit will be 0.
		// We accept both: 0 (was connected, now disconnected) or 1 (NOT_CONNECTED).
		if exitCode == 0 {
			obj, _ := parseJSON(stdout)
			Expect(jsonStringField(obj, "project")).NotTo(BeEmpty())
			Skip("seeded project was connected (exit=0) — NOT_CONNECTED path skipped")
		}
		Expect(exitCode).To(Equal(1),
			"not connected should exit 1, got %d (stderr=%s)", exitCode, stderr)
		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("NOT_CONNECTED"))
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5c: no token yields exit 4 AUTH_REQUIRED", func() {
		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZeroInDir(fixtureDir, "disconnect", "--json")

		expectAllowedExitCode(exitCode, "disconnect (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		tmpDir, _ := os.MkdirTemp("", "loopzero-e2e-disconnect-leak-")
		defer os.RemoveAll(tmpDir)

		stdout, stderr, _ := runLoopZeroInDir(tmpDir, "disconnect")
		expectNoInternalTerms(stdout, "disconnect (no yaml) stdout")
		expectNoInternalTerms(stderr, "disconnect (no yaml) stderr")

		stdout, stderr, _ = runLoopZeroInDir(tmpDir, "disconnect", "--json")
		expectNoInternalTerms(stdout, "disconnect --json (no yaml) stdout")
		expectNoInternalTerms(stderr, "disconnect --json (no yaml) stderr")
	})
})
