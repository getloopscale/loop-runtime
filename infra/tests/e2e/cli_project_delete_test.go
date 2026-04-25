// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero project delete <name>` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/project-delete.ts (AUDIT-25).
// CRITICAL per CLAUDE.md Common Mistakes: gitops.Remove failure is FATAL.
// Project delete must fail loudly if orphaned manifests cannot be removed,
// otherwise Degraded ArgoCD blocks all deploys.
//
// Behavior:
//   - Exit 0: success OR cancellation (status: cancelled)
//   - Exit 1: MISSING_ARGUMENT, CONFIRMATION_REQUIRED in CI mode
//   - Exit 3: API_UNREACHABLE, DELETE_FAILED
//   - Exit 4: AUTH_REQUIRED
//   - --json success: {project, deleted_environments[], cancelled_builds, status: "deleted"}
//   - --json cancellation: {status: "cancelled", project}
//   - --json error: {error, exit_code, code}
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero project delete", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 3: round-trip create→delete ---

	It("Contract 1+3: --yes deletes project created in same test", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		projName := "e2e-pdel-" + strings.ToLower(uniqueSuffix())

		// Pre-arrange: create.
		_, _, createCode := runLoopZero("project", "create", projName, "--json")
		if createCode != 0 {
			Skip("project create failed — cannot test delete round-trip")
		}

		stdout, stderr, exitCode := runLoopZero("project", "delete", projName, "--yes", "--json")

		expectAllowedExitCode(exitCode, "project delete --yes --json")
		expectNoInternalTerms(stdout+stderr, "project delete --yes --json")

		Expect(exitCode).To(Equal(0), "stderr=%s", stderr)
		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "status")).To(Equal("deleted"))
		Expect(jsonStringField(obj, "project")).To(Equal(projName))

		// Schema: deleted_environments + cancelled_builds present.
		Expect(obj).To(HaveKey("deleted_environments"))
		Expect(obj).To(HaveKey("cancelled_builds"))
	})

	// --- Contract 2 + 5: CI mode requires --yes ---

	It("Contract 2+5a: CI=true without --yes yields exit 1 CONFIRMATION_REQUIRED", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		oldCI := os.Getenv("CI")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		os.Setenv("CI", "true")
		defer func() {
			os.Setenv("LOOPZERO_TOKEN", oldToken)
			os.Setenv("CI", oldCI)
		}()

		stdout, stderr, exitCode := runLoopZero("project", "delete", "some-proj", "--json")

		expectAllowedExitCode(exitCode, "project delete (CI no --yes)")
		Expect(exitCode).To(Equal(1),
			"CI without --yes should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("CONFIRMATION_REQUIRED"))
	})

	// --- Contract 2 + 5: missing argument ---

	It("Contract 2+5b: missing name yields exit 1 MISSING_ARGUMENT", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("project", "delete", "", "--yes", "--json")
		expectAllowedExitCode(exitCode, "project delete (no name)")
		Expect(exitCode).To(Equal(1),
			"missing name should exit 1, got %d (stderr=%s)", exitCode, stderr)

		// Code may be MISSING_ARGUMENT (our handler) or COMMAND_VALIDATION_ERROR.
		if obj, err := parseJSON(stdout); err == nil {
			code := jsonStringField(obj, "code")
			Expect(code == "MISSING_ARGUMENT" || code == "COMMAND_VALIDATION_ERROR").To(BeTrue())
		}
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5c: no token yields exit 4 AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("project", "delete",
			"anything", "--yes", "--json")
		expectAllowedExitCode(exitCode, "project delete (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		oldCI := os.Getenv("CI")
		os.Setenv("CI", "true")
		defer func() {
			os.Setenv("LOOPZERO_TOKEN", oldToken)
			os.Setenv("CI", oldCI)
		}()

		for _, args := range [][]string{
			{"project", "delete", "abc"},
			{"project", "delete", "abc", "--json"},
			{"project", "delete", "abc", "--yes", "--json"},
		} {
			stdout, stderr, _ := runLoopZero(args...)
			expectNoInternalTerms(stdout, strings.Join(args, " ")+" stdout")
			expectNoInternalTerms(stderr, strings.Join(args, " ")+" stderr")
		}

		stdout, stderr, _ := runLoopZeroNoToken("project", "delete", "x", "--yes")
		expectNoInternalTerms(stdout, "project delete (no token)")
		expectNoInternalTerms(stderr, "project delete (no token)")
	})
})
