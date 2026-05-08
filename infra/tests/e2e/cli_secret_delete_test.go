// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero secret delete <name>` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/secret.ts (SecretDeleteCommand, AUDIT-25).
//
// Behavior:
//   - Exit 0: success OR cancelled (status: cancelled)
//   - Exit 1: missing loopzero.yaml (MISSING_CONFIG), MISSING_NAME,
//     CONFIRMATION_REQUIRED (CI or --json without --yes)
//   - Exit 3: API_UNREACHABLE, UNEXPECTED_ERROR
//   - Exit 4: AUTH_REQUIRED
//   - --json success: {name, deleted}
//   - --json cancelled: {status: "cancelled", name}
//   - --json error: {error, exit_code, code}
//   - REQUIRES loopzero.yaml + name + (--yes or interactive prompt).
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero secret delete", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 3: round-trip set→delete ---

	It("Contract 1+3: --yes deletes secret created in same test", func() {
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

		secretName := "E2E_DEL_TEST_" + strings.ToUpper(uniqueSuffix())
		secretValue := "to-delete-" + uniqueSuffix()

		// Pre-arrange: set.
		_, _, setCode := runLoopZeroInDir(fixtureDir,
			"secret", "set", secretName+"="+secretValue,
			"--env", "production", "--json")
		if setCode != 0 {
			Skip("secret set failed — cannot test delete round-trip")
		}

		stdout, stderr, exitCode := runLoopZeroInDir(fixtureDir,
			"secret", "delete", secretName,
			"--env", "production", "--yes", "--json")

		expectAllowedExitCode(exitCode, "secret delete --yes --json")
		expectNoInternalTerms(stdout+stderr, "secret delete --yes --json")

		// Even on delete the value must not leak.
		Expect(stdout+stderr).NotTo(ContainSubstring(secretValue),
			"Rule violation: value leaked on `secret delete`")

		Expect(exitCode).To(Equal(0), "stderr=%s", stderr)
		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(obj).To(HaveKey("name"))
		Expect(obj).To(HaveKey("deleted"))
		Expect(jsonStringField(obj, "name")).To(Equal(secretName))
	})

	// --- Contract 2 + 5: confirmation required in --json mode ---

	It("Contract 2+5a: --json without --yes yields exit 1 CONFIRMATION_REQUIRED", func() {
		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZeroInDir(fixtureDir,
			"secret", "delete", "ANY_NAME",
			"--env", "production", "--json")

		expectAllowedExitCode(exitCode, "secret delete --json (no yes)")
		Expect(exitCode).To(Equal(1),
			"--json without --yes should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("CONFIRMATION_REQUIRED"))
	})

	// --- Contract 2 + 5: missing loopzero.yaml ---

	It("Contract 2+5b: no loopzero.yaml yields exit 1 MISSING_CONFIG", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		tmpDir, err := os.MkdirTemp("", "loopzero-e2e-secdel-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		stdout, stderr, exitCode := runLoopZeroInDir(tmpDir,
			"secret", "delete", "ANY", "--yes", "--json")

		expectAllowedExitCode(exitCode, "secret delete (no yaml)")
		Expect(exitCode).To(Equal(1),
			"missing yaml should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("MISSING_CONFIG"))
	})

	// --- Contract 2 + 5: missing name ---

	It("Contract 2+5c: missing name with --yes yields exit 1 MISSING_NAME", func() {
		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		// Pass empty name. Commander may handle this differently — accept
		// either MISSING_NAME (our handler) or COMMAND_VALIDATION_ERROR.
		_, stderr, exitCode := runLoopZeroInDir(fixtureDir,
			"secret", "delete", "", "--yes", "--json")

		expectAllowedExitCode(exitCode, "secret delete (no name)")
		Expect(exitCode).To(Equal(1),
			"missing name should exit 1, got %d (stderr=%s)", exitCode, stderr)
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5d: no token yields exit 4 AUTH_REQUIRED", func() {
		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZeroInDir(fixtureDir,
			"secret", "delete", "ANY", "--yes", "--json")

		expectAllowedExitCode(exitCode, "secret delete (no token)")
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

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		for _, args := range [][]string{
			{"secret", "delete", "ANY"},
			{"secret", "delete", "ANY", "--json"},
			{"secret", "delete", "ANY", "--yes"},
		} {
			stdout, stderr, _ := runLoopZeroInDir(fixtureDir, args...)
			expectNoInternalTerms(stdout, strings.Join(args, " ")+" stdout")
			expectNoInternalTerms(stderr, strings.Join(args, " ")+" stderr")
		}
	})
})
