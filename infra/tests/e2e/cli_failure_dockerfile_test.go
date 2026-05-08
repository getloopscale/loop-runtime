// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Implements the F3.2 failure scenario from e2e-validation-final-2026-04-23.md
// — previously a `PIt` placeholder in failure_scenarios_test.go (Phase 5
// PENDING_E2E task).
//
// Scenario: deploy with intentionally invalid Dockerfile must exit 2
// (BUILD_FAILED) per Rule 4 + spec §0.3.
//
// CRITICAL: this is one of the 5 exit codes (0/1/2/3/4/5) — refactor that
// makes build failures yield exit 1 or exit 3 would silently break CI
// detection in user pipelines.
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("Failure scenario F3.2: invalid Dockerfile yields exit 2", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	It("F3.2: loopzero deploy with broken Dockerfile exits 2 BUILD_FAILED", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		if !clusterRunning() {
			Skip("k3d clusters not running")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		// Set up a fixture-derived directory with a Dockerfile that fails
		// at parse time. This is detectable BY BuildKit before any image
		// layer build completes — fastest path to BUILD_FAILED.
		workDir := copyFixture("hello-server")
		defer os.RemoveAll(workDir)

		// Overwrite Dockerfile with invalid syntax. The hello-server fixture
		// uses framework-based detection if no Dockerfile is present, so we
		// force one with deliberately broken syntax.
		brokenDockerfile := `FROM nonexistent-base-image:thiscannotresolve-` + uniqueSuffix() + `
THIS-IS-NOT-A-VALID-INSTRUCTION blah
RUN exit 1
`
		dockerPath := filepath.Join(workDir, "Dockerfile")
		err = os.WriteFile(dockerPath, []byte(brokenDockerfile), 0o644)
		Expect(err).NotTo(HaveOccurred())

		// Initialize git so the deploy bundle can be created (deploy needs
		// a commit to upload).
		gitCommands := [][]string{
			{"git", "init"},
			{"git", "config", "user.email", "f32@test.local"},
			{"git", "config", "user.name", "F3.2 Test"},
			{"git", "add", "-A"},
			{"git", "commit", "-m", "broken dockerfile"},
		}
		for _, args := range gitCommands {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = workDir
			err := cmd.Run()
			Expect(err).NotTo(HaveOccurred(),
				"git command %v failed in %s", args, workDir)
		}

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", resolveToken())
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		// Act: run loopzero deploy --json. Expect exit 2.
		stdout, stderr, exitCode := runLoopZeroInDir(workDir, "deploy", "--json")

		// Rule 4: must be one of 0/1/2/3/4/5.
		expectAllowedExitCode(exitCode, "deploy with broken Dockerfile")

		// CRITICAL: must be exit 2, not 1 or 3.
		Expect(exitCode).To(Equal(2),
			"broken Dockerfile must yield exit 2 BUILD_FAILED per Rule 4 + spec §0.3, got %d\nstdout=%s\nstderr=%s",
			exitCode, stdout, stderr)

		// Rule 3: no internal terms in any output.
		expectNoInternalTerms(stdout+stderr, "deploy F3.2 BUILD_FAILED")

		// Contract 3: --json error shape.
		obj, err := parseJSON(stdout)
		// Tolerate output that is not strictly JSON only — the deploy renderer
		// may print human-friendly progress prior to the JSON line. parseJSON
		// already scans for a JSON line. If still nothing parseable, the
		// failure mode is significant enough that we want the error visible.
		if err == nil {
			// Code may be BUILD_FAILED, IMAGE_PULL_FAILED, DOCKERFILE_PARSE_ERROR
			// or similar — any 'BUILD_*' label is acceptable. Spec §0.3 leaves
			// the specific code free; the contract is the exit code.
			errStr := jsonStringField(obj, "error") +
				" " + jsonStringField(obj, "code")
			Expect(strings.ToUpper(errStr)).To(MatchRegexp("BUILD|DOCKERFILE|IMAGE"),
				"error description should mention build/dockerfile/image: %v", obj)
		} else {
			GinkgoWriter.Printf("INFO: --json output not parseable (deploy renderer mixed mode). " +
				"stdout=%s\n", stdout)
		}
	})
})
