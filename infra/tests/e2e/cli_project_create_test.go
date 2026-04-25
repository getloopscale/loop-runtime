// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero project create <name>` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/project-create.ts (AUDIT-25).
//
// Behavior:
//   - Exit 0: success OR 409 idempotent (already_exists)
//   - Exit 1: missing name (MISSING_ARGUMENT), invalid name (INVALID_NAME),
//     API error, API_UNREACHABLE
//   - Exit 4: not authenticated, 401
//   - RFC 1123: ^[a-z][a-z0-9-]*$, max 63, no trailing hyphen
//   - --json success: {id, name, team, created_at, status: "created" | "already_exists"}
//   - --json error: {error, exit_code, code}
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero project create", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 3 ---

	It("Contract 1+3: creates new project then idempotently re-creates", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		projName := "e2e-proj-" + strings.ToLower(uniqueSuffix())

		stdout, stderr, exitCode := runLoopZero("project", "create", projName, "--json")
		expectAllowedExitCode(exitCode, "project create (first)")
		expectNoInternalTerms(stdout+stderr, "project create (first)")
		Expect(exitCode).To(Equal(0), "stderr=%s", stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "status")).To(Equal("created"))
		Expect(jsonStringField(obj, "name")).To(Equal(projName))
		Expect(obj).To(HaveKey("id"))
		Expect(obj).To(HaveKey("team"))
		Expect(obj).To(HaveKey("created_at"))

		// Second create — idempotent.
		stdout2, stderr2, exitCode2 := runLoopZero("project", "create", projName, "--json")
		Expect(exitCode2).To(Equal(0), "stderr=%s", stderr2)
		obj2, err := parseJSON(stdout2)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj2, "status")).To(Equal("already_exists"))

		// Cleanup: best-effort.
		_, _, _ = runLoopZero("project", "delete", projName, "--yes", "--json")
	})

	// --- Contract 2 + 5: invalid names per RFC 1123 ---

	It("Contract 2+5a: invalid names (uppercase, leading hyphen, trailing hyphen, too long) yield INVALID_NAME", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		// Each variant must yield exit 1 + INVALID_NAME.
		invalidNames := []string{
			"BadName",                              // uppercase
			"-leading",                             // leading hyphen
			"trailing-",                            // trailing hyphen
			strings.Repeat("a", 64),                // > 63 chars
			"contains_underscore",                  // RFC 1123 forbids _
		}

		for _, n := range invalidNames {
			stdout, stderr, exitCode := runLoopZero("project", "create", n, "--json")
			expectAllowedExitCode(exitCode, "project create "+n)
			Expect(exitCode).To(Equal(1),
				"%q should yield exit 1, got %d (stderr=%s)", n, exitCode, stderr)
			obj, err := parseJSON(stdout)
			Expect(err).NotTo(HaveOccurred())
			Expect(jsonStringField(obj, "code")).To(Equal("INVALID_NAME"),
				"%q should yield INVALID_NAME, got: %v", n, obj)
		}
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5b: no token yields exit 4 AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("project", "create", "anything", "--json")
		expectAllowedExitCode(exitCode, "project create (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 4 ---

	It("Contract 4: validation/error paths leak no internal terms", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		for _, args := range [][]string{
			{"project", "create", "BAD"},
			{"project", "create", "BAD", "--json"},
			{"project", "create", "trailing-"},
		} {
			stdout, stderr, _ := runLoopZero(args...)
			expectNoInternalTerms(stdout, strings.Join(args, " ")+" stdout")
			expectNoInternalTerms(stderr, strings.Join(args, " ")+" stderr")
		}

		stdout, stderr, _ := runLoopZeroNoToken("project", "create", "x")
		expectNoInternalTerms(stdout, "project create (no token)")
		expectNoInternalTerms(stderr, "project create (no token)")
	})
})
