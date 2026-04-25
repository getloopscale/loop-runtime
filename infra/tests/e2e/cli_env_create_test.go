// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero env create <name>` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/env-create.ts (AUDIT-25).
//
// Behavior:
//   - Exit 0: success OR 409 idempotent (already_exists)
//   - Exit 1: missing/invalid name (INVALID_NAME), reserved (RESERVED_NAME),
//     API error, API_UNREACHABLE
//   - Exit 4: not authenticated, 401
//   - Name regex: ^[a-z][a-z0-9-]*$, max 63 chars
//   - Reserved: default, system, internal
//   - --json success: {name, status: "created" | "already_exists"}
//   - --json error: {error, exit_code, code}
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero env create", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 3: happy path + idempotency (409) ---

	It("Contract 1+3: creates new env then idempotently re-creates same name", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		envName := "e2e-env-" + strings.ToLower(uniqueSuffix())

		// First create — should be `status: created`.
		stdout, stderr, exitCode := runLoopZero("env", "create", envName, "--json")

		expectAllowedExitCode(exitCode, "env create (first)")
		expectNoInternalTerms(stdout+stderr, "env create (first)")
		Expect(exitCode).To(Equal(0), "stderr=%s", stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "status")).To(Equal("created"),
			"first create should yield status=created, got: %v", obj)

		// Second create — should be idempotent: status: already_exists.
		stdout2, stderr2, exitCode2 := runLoopZero("env", "create", envName, "--json")
		expectAllowedExitCode(exitCode2, "env create (second)")
		Expect(exitCode2).To(Equal(0),
			"second create should also exit 0 (idempotent), got %d (stderr=%s)",
			exitCode2, stderr2)

		obj2, err := parseJSON(stdout2)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj2, "status")).To(Equal("already_exists"),
			"second create should yield status=already_exists, got: %v", obj2)

		// Cleanup: best-effort delete.
		_, _, _ = runLoopZero("env", "delete", envName, "--yes", "--json")
	})

	// --- Contract 2 + 5: invalid name ---

	It("Contract 2+5a: invalid name yields exit 1 INVALID_NAME", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy-for-validation")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		// UPPERCASE violates ^[a-z]
		stdout, stderr, exitCode := runLoopZero("env", "create", "BAD_NAME", "--json")
		expectAllowedExitCode(exitCode, "env create (uppercase)")
		Expect(exitCode).To(Equal(1),
			"uppercase name should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("INVALID_NAME"))

		// Leading hyphen also invalid.
		stdout, _, exitCode = runLoopZero("env", "create", "-bad-leading", "--json")
		Expect(exitCode).To(Equal(1))
		obj, _ = parseJSON(stdout)
		Expect(jsonStringField(obj, "code")).To(Equal("INVALID_NAME"))
	})

	// --- Contract 2 + 5: reserved name ---

	It("Contract 2+5b: reserved names yield exit 1 RESERVED_NAME", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		for _, reserved := range []string{"default", "system", "internal"} {
			stdout, stderr, exitCode := runLoopZero("env", "create", reserved, "--json")
			Expect(exitCode).To(Equal(1),
				"reserved name %q should exit 1 (stderr=%s)", reserved, stderr)
			obj, err := parseJSON(stdout)
			Expect(err).NotTo(HaveOccurred())
			Expect(jsonStringField(obj, "code")).To(Equal("RESERVED_NAME"),
				"reserved %q should yield RESERVED_NAME", reserved)
		}
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5c: no token yields exit 4 AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("env", "create", "anything", "--json")
		expectAllowedExitCode(exitCode, "env create (no token)")
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
			{"env", "create", "BAD_NAME"},
			{"env", "create", "BAD_NAME", "--json"},
			{"env", "create", "system"},
			{"env", "create", "system", "--json"},
		} {
			stdout, stderr, _ := runLoopZero(args...)
			expectNoInternalTerms(stdout, strings.Join(args, " ")+" stdout")
			expectNoInternalTerms(stderr, strings.Join(args, " ")+" stderr")
		}

		stdout, stderr, _ := runLoopZeroNoToken("env", "create", "x")
		expectNoInternalTerms(stdout, "env create (no token)")
		expectNoInternalTerms(stderr, "env create (no token)")
	})
})
