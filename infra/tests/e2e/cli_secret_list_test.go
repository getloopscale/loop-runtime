// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero secret list` covering the 5 CLI contracts PLUS the
// critical zero-leak invariant (Rule 5 + F1.7 from e2e-validation 2026-04-23).
//
// Source-of-truth: cli/src/commands/secret.ts (SecretListCommand, AUDIT-25).
//
// Behavior:
//   - Exit 0: success (including empty list)
//   - Exit 1: missing loopzero.yaml (MISSING_CONFIG), API ApiError
//   - Exit 3: network error (API_UNREACHABLE), unexpected error
//   - Exit 4: not authenticated, 401 (via ApiError mapping)
//   - --json success: {secrets: [{name, environment, app}]} — NO values, NO total
//   - --json error: {error, exit_code, code}
//   - REQUIRES loopzero.yaml.
//
// CRITICAL INVARIANT (F1.7 / Rule 5):
//   Secret VALUES must never appear in stdout, stderr, or --json output.
//   This is enforced via fuzzy match against any value the test sets.
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero secret list", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 3 ---

	It("Contract 1+3: authenticated secret list returns exit 0 + valid JSON shape", func() {
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

		stdout, stderr, exitCode := runLoopZeroInDir(fixtureDir,
			"secret", "list", "--env", "production", "--json")

		expectAllowedExitCode(exitCode, "secret list --json")
		expectNoInternalTerms(stdout+stderr, "secret list --json")

		Expect(exitCode).To(Equal(0), "stderr=%s", stderr)
		Expect(looksLikeJSONLine(stdout)).To(BeTrue())

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())

		// Schema: {secrets: [{name, environment, app}]} — NO `total` per
		// secret.ts line 168. Asserting the response shape exactly.
		secretsRaw, exists := obj["secrets"]
		Expect(exists).To(BeTrue(), "secrets field missing: %v", obj)
		secrets, ok := secretsRaw.([]interface{})
		Expect(ok).To(BeTrue())

		// Each entry must have only name, environment, app — never `value`.
		for i, raw := range secrets {
			s, ok := raw.(map[string]interface{})
			Expect(ok).To(BeTrue(), "secrets[%d] should be object", i)
			Expect(s).To(HaveKey("name"))
			Expect(s).To(HaveKey("environment"))
			Expect(s).To(HaveKey("app"))
			// CRITICAL: value field must NOT exist.
			Expect(s).NotTo(HaveKey("value"),
				"Rule violation: secret value present in JSON: %v", s)
			Expect(s).NotTo(HaveKey("secret_value"),
				"Rule violation: secret_value present in JSON: %v", s)
		}
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5a: no token yields exit 4 AUTH_REQUIRED", func() {
		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZeroInDir(fixtureDir, "secret", "list", "--json")

		expectAllowedExitCode(exitCode, "secret list (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 2 + 5: missing loopzero.yaml ---

	It("Contract 2+5b: missing loopzero.yaml yields exit 1 MISSING_CONFIG", func() {
		err := seed.Run(seed.Config{})
		if err != nil || !apiHealthy() {
			Skip("seed/api not available")
		}
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", resolveToken())
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		tmpDir, err := os.MkdirTemp("", "loopzero-e2e-secretlist-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		stdout, stderr, exitCode := runLoopZeroInDir(tmpDir, "secret", "list", "--json")

		expectAllowedExitCode(exitCode, "secret list (no yaml)")
		Expect(exitCode).To(Equal(1),
			"missing yaml should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("MISSING_CONFIG"))
	})

	// --- CRITICAL: zero value leak (Rule + F1.7) ---

	It("CRITICAL: secret values never appear in stdout/stderr/--json output", func() {
		// This test sets a unique secret then lists it, asserting the value
		// never appears in any output channel. The value is constructed to
		// be highly unique so any leak is unambiguous.
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

		secretName := "E2E_LEAK_CHECK_" + strings.ToUpper(uniqueSuffix())
		secretValue := "leak-canary-" + uniqueSuffix() + "-DO-NOT-LEAK"

		// Set the secret. We assert exit 0 but tolerate other outcomes (e.g.
		// no project for this seed). The leak check still applies to whatever
		// output is produced.
		setOut, setErr, setCode := runLoopZeroInDir(fixtureDir,
			"secret", "set", secretName+"="+secretValue,
			"--env", "production", "--json")

		// Even on the SET path, the value should NOT echo in output.
		Expect(setOut+setErr).NotTo(ContainSubstring(secretValue),
			"Rule violation: secret value leaked on `secret set` (exit=%d)\n--- stdout ---\n%s\n--- stderr ---\n%s",
			setCode, setOut, setErr)

		// List secrets — value must not be present.
		listOut, listErr, listCode := runLoopZeroInDir(fixtureDir,
			"secret", "list", "--env", "production", "--json")

		expectAllowedExitCode(listCode, "secret list (post-set)")
		expectNoInternalTerms(listOut+listErr, "secret list (post-set)")

		Expect(listOut+listErr).NotTo(ContainSubstring(secretValue),
			"Rule violation: secret value leaked on `secret list` (exit=%d)\n--- stdout ---\n%s\n--- stderr ---\n%s",
			listCode, listOut, listErr)

		// Same on human (non-JSON) mode.
		humanOut, humanErr, _ := runLoopZeroInDir(fixtureDir,
			"secret", "list", "--env", "production")
		Expect(humanOut+humanErr).NotTo(ContainSubstring(secretValue),
			"Rule violation: secret value leaked on `secret list` human mode")

		// Cleanup: best-effort delete.
		_, _, _ = runLoopZeroInDir(fixtureDir,
			"secret", "delete", secretName, "--env", "production", "--yes", "--json")
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		tmpDir, _ := os.MkdirTemp("", "loopzero-e2e-secret-leak-")
		defer os.RemoveAll(tmpDir)

		stdout, stderr, _ := runLoopZeroInDir(tmpDir, "secret", "list")
		expectNoInternalTerms(stdout, "secret list (no yaml/no token) stdout")
		expectNoInternalTerms(stderr, "secret list (no yaml/no token) stderr")

		stdout, stderr, _ = runLoopZeroInDir(tmpDir, "secret", "list", "--json")
		expectNoInternalTerms(stdout, "secret list --json (no yaml) stdout")
		expectNoInternalTerms(stderr, "secret list --json (no yaml) stderr")
	})
})
