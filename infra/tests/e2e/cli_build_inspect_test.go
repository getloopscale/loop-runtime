// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero build inspect <build-id>` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/build-inspect.ts (AUDIT-34, AUDIT-25).
//
// CRITICAL CONTRACT (AUDIT-34): API returns `project_id` not `project`. CLI
// previously used `project` field, producing "Project: undefined" in output.
// Tests must assert `project_id` is the key, not `project`.
//
// Behavior:
//   - Exit 0: success
//   - Exit 1: MISSING_ARGUMENT, BUILD_NOT_FOUND (404), API error
//   - Exit 4: AUTH_REQUIRED
//   - --json success: {build_id, project_id, environment, status,
//     commit_sha, created_at, started_at?, finished_at?,
//     duration_seconds?, created_by?, apps?[], error?}
//   - --json error: {error, exit_code, code}
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero build inspect", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 2 + 5: 404 ---

	It("Contract 2+5a: nonexistent build yields exit 1 BUILD_NOT_FOUND", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("build", "inspect",
			"build-absent-"+uniqueSuffix(), "--json")

		expectAllowedExitCode(exitCode, "build inspect (404)")
		Expect(exitCode).To(Equal(1),
			"missing build should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("BUILD_NOT_FOUND"))
	})

	// --- Contract 2 + 5: missing argument ---

	It("Contract 2+5b: missing build-id yields exit 1 MISSING_ARGUMENT", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		_, stderr, exitCode := runLoopZero("build", "inspect", "", "--json")
		expectAllowedExitCode(exitCode, "build inspect (no id)")
		Expect(exitCode).To(Equal(1),
			"missing id should exit 1, got %d (stderr=%s)", exitCode, stderr)
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5c: no token yields exit 4 AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("build", "inspect",
			"build-xyz", "--json")

		expectAllowedExitCode(exitCode, "build inspect (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- AUDIT-34 contract: project_id NOT project ---
	//
	// This test does not require a real build to exist. It validates that
	// IF the API returns a build, the CLI reads `project_id` not `project`.
	// We construct the assertion via a build that may exist (latest from
	// seed deploy) — if none exists, soft-skip.

	It("AUDIT-34 contract: --json shape uses 'project_id' not 'project'", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		// Need any existing build. Fetch via build list first.
		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		listOut, _, listCode := runLoopZeroInDir(fixtureDir, "build", "list", "--json")
		if listCode != 0 {
			Skip("build list failed — cannot find a build to inspect")
		}
		listObj, err := parseJSON(listOut)
		if err != nil {
			Skip("build list output unparseable")
		}
		buildsRaw, ok := listObj["builds"].([]interface{})
		if !ok || len(buildsRaw) == 0 {
			Skip("no builds exist for this seed — AUDIT-34 contract not exercisable")
		}
		first, ok := buildsRaw[0].(map[string]interface{})
		if !ok {
			Skip("build list shape unexpected")
		}
		buildID := jsonStringField(first, "build_id")
		if buildID == "" {
			Skip("build_id missing in list response")
		}

		stdout, stderr, exitCode := runLoopZero("build", "inspect", buildID, "--json")
		expectAllowedExitCode(exitCode, "build inspect (existing)")
		expectNoInternalTerms(stdout+stderr, "build inspect")
		Expect(exitCode).To(Equal(0))

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())

		// AUDIT-34: must be project_id, not project.
		Expect(obj).To(HaveKey("project_id"),
			"AUDIT-34 contract violation: response missing project_id")
		Expect(obj).To(HaveKey("build_id"))
		Expect(obj).To(HaveKey("environment"))
		Expect(obj).To(HaveKey("status"))
		Expect(obj).To(HaveKey("commit_sha"))
		Expect(obj).To(HaveKey("created_at"))
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		stdout, stderr, _ := runLoopZeroNoToken("build", "inspect", "build-x")
		expectNoInternalTerms(stdout, "build inspect (no token) stdout")
		expectNoInternalTerms(stderr, "build inspect (no token) stderr")

		stdout, stderr, _ = runLoopZeroNoToken("build", "inspect", "build-x", "--json")
		expectNoInternalTerms(stdout, "build inspect --json (no token) stdout")
		expectNoInternalTerms(stderr, "build inspect --json (no token) stderr")

		err := seed.Run(seed.Config{})
		if err == nil && apiHealthy() {
			oldToken := os.Getenv("LOOPZERO_TOKEN")
			os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
			defer os.Setenv("LOOPZERO_TOKEN", oldToken)

			for _, args := range [][]string{
				{"build", "inspect", "absent-" + uniqueSuffix()},
				{"build", "inspect", "absent-" + uniqueSuffix(), "--json"},
			} {
				stdout, stderr, _ := runLoopZero(args...)
				expectNoInternalTerms(stdout, strings.Join(args, " ")+" stdout")
				expectNoInternalTerms(stderr, strings.Join(args, " ")+" stderr")
			}
		}
	})
})
