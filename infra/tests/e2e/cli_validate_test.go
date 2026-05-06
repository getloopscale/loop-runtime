// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero validate` covering the 5 CLI contracts.
//
// validate is local-only (no API/k3d required). It reads loopzero.yaml from the
// current directory and reports errors/warnings.
//
// Source-of-truth: cli/src/commands/validate.ts.
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// runLoopZeroInDir executes `loopzero <args...>` with the given working directory.
// Used for commands like `validate` that operate on cwd.
//
// LOOPZERO_API_TIMEOUT is bumped to 120s (vs the CLI default of 30s in
// cli/src/config/constants.ts:53). Project delete cascades through cancel
// builds + gitops remove + ArgoCD sync and exceeds 30s on LIVE k3d.
func runLoopZeroInDir(dir string, args ...string) (stdout, stderr string, exitCode int) {
	cmd := exec.Command("loopzero", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"LOOPZERO_API_URL="+apiURL(),
		"LOOPZERO_API_TIMEOUT=120000",
	)
	if token := os.Getenv("LOOPZERO_TOKEN"); token != "" {
		cmd.Env = append(cmd.Env, "LOOPZERO_TOKEN="+token)
	}

	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

var _ = Describe("CLI: loopzero validate", Ordered, func() {
	BeforeEach(func() {
		// validate does not need LOOPZERO_E2E or k3d; only the CLI binary.
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 3: happy path + --json schema ---

	It("Contract 1+3: valid loopzero.yaml yields exit 0 + valid:true JSON", func() {
		// Use the hello-server fixture which has a known-good loopzero.yaml.
		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		stdout, stderr, exitCode := runLoopZeroInDir(fixtureDir, "validate", "--json")

		expectAllowedExitCode(exitCode, "validate --json (valid)")
		expectNoInternalTerms(stdout+stderr, "validate --json (valid)")

		Expect(exitCode).To(Equal(0), "stderr=%s", stderr)
		Expect(looksLikeJSONLine(stdout)).To(BeTrue(),
			"--json should produce JSON, got: %q", stdout)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())

		valid, ok := jsonBoolField(obj, "valid")
		Expect(ok).To(BeTrue(), "valid field missing")
		Expect(valid).To(BeTrue(), "expected valid=true for hello-server fixture; got: %v", obj)

		// apps must be present and >= 1.
		appsRaw, exists := obj["apps"]
		Expect(exists).To(BeTrue(), "apps field missing")
		appsCount, ok := appsRaw.(float64)
		Expect(ok).To(BeTrue(), "apps should be a number")
		Expect(appsCount).To(BeNumerically(">=", 1))

		// errors must be present (possibly empty) array.
		errorsRaw, exists := obj["errors"]
		Expect(exists).To(BeTrue(), "errors field missing")
		errors, ok := errorsRaw.([]interface{})
		Expect(ok).To(BeTrue(), "errors should be array")
		Expect(errors).To(BeEmpty(), "valid yaml should have no errors")
	})

	// --- Contract 2 + 5: failure path + exit codes ---

	It("Contract 2+5: missing loopzero.yaml yields exit 1 with structured error", func() {
		// Empty temp dir => no loopzero.yaml => exit 1.
		tmpDir, err := os.MkdirTemp("", "loopzero-e2e-validate-empty-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		stdout, stderr, exitCode := runLoopZeroInDir(tmpDir, "validate", "--json")

		expectAllowedExitCode(exitCode, "validate --json (no loopzero.yaml)")
		Expect(exitCode).To(Equal(1),
			"missing loopzero.yaml should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred(), "JSON parse failed: %s", stdout)

		valid, ok := jsonBoolField(obj, "valid")
		Expect(ok).To(BeTrue())
		Expect(valid).To(BeFalse(), "expected valid=false")
	})

	It("Contract 2+5: malformed loopzero.yaml yields exit 1", func() {
		// Write an unparseable YAML file.
		tmpDir, err := os.MkdirTemp("", "loopzero-e2e-validate-bad-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		badYaml := "this is: not: valid: yaml::: [\n"
		err = os.WriteFile(filepath.Join(tmpDir, "loopzero.yaml"), []byte(badYaml), 0o644)
		Expect(err).NotTo(HaveOccurred())

		stdout, stderr, exitCode := runLoopZeroInDir(tmpDir, "validate", "--json")

		expectAllowedExitCode(exitCode, "validate --json (malformed)")
		Expect(exitCode).To(Equal(1),
			"malformed yaml should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		valid, _ := jsonBoolField(obj, "valid")
		Expect(valid).To(BeFalse())
	})

	// --- Contract 4: internal terms (Rule 3) ---

	It("Contract 4: human + JSON output paths leak no internal terms", func() {
		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		stdout, stderr, _ := runLoopZeroInDir(fixtureDir, "validate")
		expectNoInternalTerms(stdout, "validate human stdout")
		expectNoInternalTerms(stderr, "validate human stderr")

		stdout, stderr, _ = runLoopZeroInDir(fixtureDir, "validate", "--json")
		expectNoInternalTerms(stdout, "validate --json stdout")
		expectNoInternalTerms(stderr, "validate --json stderr")

		// Also exercise error path:
		tmpDir, _ := os.MkdirTemp("", "loopzero-e2e-validate-leak-")
		defer os.RemoveAll(tmpDir)
		stdout, stderr, _ = runLoopZeroInDir(tmpDir, "validate")
		expectNoInternalTerms(stdout, "validate error human stdout")
		expectNoInternalTerms(stderr, "validate error human stderr")
	})
})
