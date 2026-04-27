// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero env list` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/env-list.ts.
//
// Behavior:
//   - Exit 0: success (including empty list)
//   - Exit 1: API error
//   - Exit 4: auth error
//   - --json success: {environments: [{name, status}], total: number}
//   - Does NOT require loopzero.yaml
package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero env list", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 3: happy path + --json schema ---

	It("Contract 1+3: authenticated env list returns exit 0 + valid JSON shape", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred(), "seed failed")

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("env", "list", "--json")

		expectAllowedExitCode(exitCode, "env list --json (authenticated)")
		expectNoInternalTerms(stdout+stderr, "env list --json")

		Expect(exitCode).To(Equal(0), "stderr=%s", stderr)
		Expect(looksLikeJSONLine(stdout)).To(BeTrue(),
			"--json should produce JSON, got: %q", stdout)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())

		// Schema: environments array + total number.
		envsRaw, exists := obj["environments"]
		Expect(exists).To(BeTrue(), "environments field missing: %v", obj)
		envs, ok := envsRaw.([]interface{})
		Expect(ok).To(BeTrue(), "environments should be array")

		totalRaw, exists := obj["total"]
		Expect(exists).To(BeTrue(), "total field missing: %v", obj)
		total, ok := totalRaw.(float64)
		Expect(ok).To(BeTrue(), "total should be number")
		Expect(int(total)).To(Equal(len(envs)),
			"total should equal len(environments)")

		// Each env entry has name + status (env-list.ts schema).
		for i, raw := range envs {
			env, ok := raw.(map[string]interface{})
			Expect(ok).To(BeTrue(), "environments[%d] should be object", i)
			Expect(env).To(HaveKey("name"))
			Expect(env).To(HaveKey("status"))
		}
	})

	// --- Contract 2 + 5: failure path + exit codes ---

	It("Contract 2+5: no token yields exit 4 with AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("env", "list", "--json")

		expectAllowedExitCode(exitCode, "env list --json (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated env list should exit 4, got %d (stderr=%s)",
			exitCode, stderr)

		// Error JSON shape from env-list.ts: {error, exit_code, code}
		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred(), "stdout=%s stderr=%s", stdout, stderr)
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 4: internal terms (Rule 3) ---

	It("Contract 4: human + JSON paths leak no internal terms", func() {
		// No-token path.
		stdout, stderr, _ := runLoopZeroNoToken("env", "list")
		expectNoInternalTerms(stdout, "env list (no token) stdout")
		expectNoInternalTerms(stderr, "env list (no token) stderr")

		stdout, stderr, _ = runLoopZeroNoToken("env", "list", "--json")
		expectNoInternalTerms(stdout, "env list --json (no token) stdout")
		expectNoInternalTerms(stderr, "env list --json (no token) stderr")

		// Authenticated path.
		err := seed.Run(seed.Config{})
		if err == nil && apiHealthy() {
			oldToken := os.Getenv("LOOPZERO_TOKEN")
			os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
			defer os.Setenv("LOOPZERO_TOKEN", oldToken)

			stdout, stderr, _ := runLoopZero("env", "list")
			expectNoInternalTerms(stdout, "env list (authenticated) stdout")
			expectNoInternalTerms(stderr, "env list (authenticated) stderr")

			stdout, stderr, _ = runLoopZero("env", "list", "--json")
			expectNoInternalTerms(stdout, "env list --json (authenticated) stdout")
			expectNoInternalTerms(stderr, "env list --json (authenticated) stderr")
		}
	})
})
