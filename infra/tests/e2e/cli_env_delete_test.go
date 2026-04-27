// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero env delete <name>` covering the 5 CLI contracts plus
// the PRODUCTION_PROTECTED safety invariant.
//
// Source-of-truth: cli/src/commands/env-delete.ts (AUDIT-11).
//
// Behavior:
//   - Exit 0: success
//   - Exit 1: MISSING_ARGUMENT, **PRODUCTION_PROTECTED** (production never
//     deletable), CONFIRMATION_REQUIRED, CONFIRMATION_MISMATCH, ENV_NOT_FOUND
//   - Exit 3: DELETE_FAILED
//   - Exit 4: AUTH_REQUIRED
//   - Two confirmation modes: --yes (UX consistency) OR --confirm <name>
//   - --json success: {name, status: "deleted"}
//   - --json error: {error, exit_code, code}
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero env delete", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- CRITICAL: production guard ---

	It("CRITICAL: 'production' env can NEVER be deleted, even with --yes --confirm production", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy-token-validation-path")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		// Try every form: bare, --yes, --confirm production, all combined.
		variants := [][]string{
			{"env", "delete", "production"},
			{"env", "delete", "production", "--yes"},
			{"env", "delete", "production", "--confirm", "production"},
			{"env", "delete", "production", "--yes", "--confirm", "production"},
		}

		for _, args := range variants {
			argsJSON := append([]string{}, args...)
			argsJSON = append(argsJSON, "--json")

			stdout, stderr, exitCode := runLoopZero(argsJSON...)

			expectAllowedExitCode(exitCode, "env delete production "+strings.Join(args, " "))
			Expect(exitCode).To(Equal(1),
				"production must NEVER be deletable, got exit %d for args %v (stderr=%s)",
				exitCode, args, stderr)

			obj, err := parseJSON(stdout)
			Expect(err).NotTo(HaveOccurred())
			Expect(jsonStringField(obj, "code")).To(Equal("PRODUCTION_PROTECTED"),
				"production guard must yield PRODUCTION_PROTECTED, got: %v", obj)
		}
	})

	// --- Contract 1 + 3: happy path round-trip create→delete ---

	It("Contract 1+3: --yes deletes env created in same test", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		envName := "e2e-del-" + strings.ToLower(uniqueSuffix())

		// Pre-arrange: create.
		_, _, createCode := runLoopZero("env", "create", envName, "--json")
		if createCode != 0 {
			Skip("env create failed — cannot test delete round-trip")
		}

		// Act: delete with --yes.
		stdout, stderr, exitCode := runLoopZero("env", "delete", envName, "--yes", "--json")

		expectAllowedExitCode(exitCode, "env delete --yes --json")
		expectNoInternalTerms(stdout+stderr, "env delete --yes --json")

		Expect(exitCode).To(Equal(0), "stderr=%s", stderr)
		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "status")).To(Equal("deleted"))
		Expect(jsonStringField(obj, "name")).To(Equal(envName))
	})

	// --- Contract 2 + 5: confirmation required ---

	It("Contract 2+5a: no --yes/--confirm yields exit 1 CONFIRMATION_REQUIRED", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("env", "delete", "some-env", "--json")

		expectAllowedExitCode(exitCode, "env delete (no confirmation)")
		Expect(exitCode).To(Equal(1),
			"missing confirmation should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("CONFIRMATION_REQUIRED"))
	})

	// --- Contract 2 + 5: confirmation mismatch ---

	It("Contract 2+5b: --confirm with wrong name yields exit 1 CONFIRMATION_MISMATCH", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("env", "delete", "abc",
			"--confirm", "xyz", "--json")

		expectAllowedExitCode(exitCode, "env delete (mismatch)")
		Expect(exitCode).To(Equal(1),
			"mismatch should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("CONFIRMATION_MISMATCH"))
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5c: no token yields exit 4 AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("env", "delete",
			"anything", "--yes", "--json")

		expectAllowedExitCode(exitCode, "env delete (no token)")
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
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		for _, args := range [][]string{
			{"env", "delete", "production", "--yes"},
			{"env", "delete", "production", "--yes", "--json"},
			{"env", "delete", "abc", "--confirm", "xyz"},
			{"env", "delete", "abc", "--confirm", "xyz", "--json"},
			{"env", "delete", "abc"},
			{"env", "delete", "abc", "--json"},
		} {
			stdout, stderr, _ := runLoopZero(args...)
			expectNoInternalTerms(stdout, strings.Join(args, " ")+" stdout")
			expectNoInternalTerms(stderr, strings.Join(args, " ")+" stderr")
		}
	})
})
