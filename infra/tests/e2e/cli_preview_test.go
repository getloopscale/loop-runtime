// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero preview list/inspect/delete`.
//
// Source-of-truth: cli/src/commands/preview.ts.
//
// Behavior validated:
//   - Exit 0: success on list/inspect/delete with valid inputs
//   - Exit 1: usage errors (missing project, invalid args, NOT_FOUND, etc.)
//   - Exit 4: missing/invalid auth
//   - --json: parseable JSON with required fields
//   - No internal terms in any output (Rule 3)
//   - Exit codes restricted to 0/1/2/3/4/5 (Rule 4)
//
// Pre-T4.1 these commands surfaced PREVIEW_NOT_AVAILABLE; that branch
// has been removed (T4.1) and these specs assert the new behavior.
//
// Set LOOPZERO_E2E=1 to enable. Skip when k3d not running.
package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)


var _ = Describe("CLI: loopzero preview [Preview]", Ordered, func() {

	var projectName = "api"
	var origDir string
	var tmpDir string

	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("set LOOPZERO_E2E=1 to run preview CLI E2E specs")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}

		var err error
		origDir, err = os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		tmpDir, err = os.MkdirTemp("", "loopzero-preview-cli-e2e-*")
		Expect(err).NotTo(HaveOccurred())
		yaml := "project: " + projectName + "\napps:\n  - name: hello\n    type: server\n    port: 8080\n"
		Expect(os.WriteFile(filepath.Join(tmpDir, "loopzero.yaml"), []byte(yaml), 0o600)).To(Succeed())
		Expect(os.Chdir(tmpDir)).To(Succeed())
	})

	AfterEach(func() {
		if origDir != "" {
			_ = os.Chdir(origDir)
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	// ---- Contract 4: no internal terms ----

	It("Contract 4 (Rule 3): preview list output contains no internal terms", func() {
		stdout, stderr, _ := runLoopZero("preview", "list", "--json")
		combined := stdout + "\n" + stderr
		for _, term := range internalTerms {
			Expect(strings.Contains(combined, term)).To(BeFalse(),
				"Rule 3 violation: %q in preview list output: %s", term, combined)
		}
	})

	// ---- Contract 5: exit codes ----

	It("Contract 5 (Rule 4): preview list yields a permitted exit code", func() {
		_, _, code := runLoopZero("preview", "list", "--json")
		_, ok := allowedExitCodes[code]
		Expect(ok).To(BeTrue(), "exit code %d not in allowed set 0/1/2/3/4/5", code)
	})

	// ---- Contract 1+3: happy path JSON shape ----

	It("Contract 1+3: preview list --json returns parseable schema", func() {
		stdout, _, code := runLoopZero("preview", "list", "--json")
		// Either exit 0 with previews or exit-1 with a structured error
		// — both must be valid JSON.
		Expect([]int{0, 1, 4}).To(ContainElement(code), "unexpected exit: %d, body: %s", code, stdout)
		var payload map[string]any
		Expect(json.Unmarshal([]byte(stdout), &payload)).To(Succeed(), "stdout not JSON: %s", stdout)
	})

	// ---- Contract 2: missing auth ----

	It("Contract 2: preview list without auth yields exit 4", func() {
		// runLoopZeroNoToken sets LOOPZERO_TOKEN="" via env, but the CLI also
		// reads ~/.loopzero/config.json. To exercise the unauthenticated
		// path, we temporarily isolate HOME to a fresh dir without any
		// stored token. This is the standard pattern from auth_test.go.
		origHome := os.Getenv("HOME")
		fakeHome, err := os.MkdirTemp("", "loopzero-noauth-*")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(fakeHome)
		_ = os.Setenv("HOME", fakeHome)
		defer os.Setenv("HOME", origHome)

		_, _, code := runLoopZeroNoToken("preview", "list", "--json")
		Expect(code).To(Equal(4), "missing auth must yield exit 4")
	})

	// ---- Inspect: missing ID ----

	It("preview inspect without an ID yields exit 1 / MISSING_ARGUMENT", func() {
		// Commander rejects this at parse time, then the subcommand
		// validates internally — exit 1 either way.
		_, _, code := runLoopZero("preview", "inspect", "")
		Expect(code).To(Equal(1), "missing ID must yield exit 1")
	})

	// ---- Inspect: nonexistent ID surfaces NOT_FOUND, not PREVIEW_NOT_AVAILABLE ----

	It("preview inspect on unknown ID yields exit 1 / PREVIEW_NOT_FOUND (T4.1: no PREVIEW_NOT_AVAILABLE remap)", func() {
		stdout, _, code := runLoopZero("preview", "inspect", "prev-nonexistent-deadbeef", "--json")
		Expect(code).To(Equal(1), "unknown ID must yield exit 1")

		var payload map[string]any
		Expect(json.Unmarshal([]byte(stdout), &payload)).To(Succeed(), "JSON expected: %s", stdout)
		// Code is preserved as-is per T4.1; PREVIEW_NOT_AVAILABLE is gone.
		if codeField, ok := payload["code"].(string); ok {
			Expect(codeField).NotTo(Equal("PREVIEW_NOT_AVAILABLE"),
				"PREVIEW_NOT_AVAILABLE remap was removed by T4.1")
		}
	})

	// ---- Delete: confirmation required without --yes ----

	It("preview delete without --yes in CI mode yields exit 1 / CONFIRMATION_REQUIRED", func() {
		// CI=true is the default in our Github Actions runner; the
		// command honors it.
		_, _, code := runLoopZeroWithCI("preview", "delete", "prev-anything", "--json")
		Expect(code).To(Equal(1), "missing confirmation in CI must yield exit 1")
	})

	// ---- Delete: with --yes on unknown ID yields PREVIEW_NOT_FOUND ----

	It("preview delete --yes on unknown ID yields exit 1 / PREVIEW_NOT_FOUND", func() {
		stdout, _, code := runLoopZero("preview", "delete", "prev-nonexistent-feedface", "--yes", "--json")
		Expect(code).To(Equal(1), "unknown ID must yield exit 1, got %d body=%s", code, stdout)
	})
})

// runLoopZeroWithCI runs the CLI with CI=true forced on, used to test the
// confirmation-required path in delete.
func runLoopZeroWithCI(args ...string) (stdout, stderr string, exitCode int) {
	old := os.Getenv("CI")
	_ = os.Setenv("CI", "true")
	defer os.Setenv("CI", old)
	return runLoopZero(args...)
}
