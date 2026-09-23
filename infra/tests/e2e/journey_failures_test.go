// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Journey 10: Failure Modes
//
// Migrated from: infra/tests/dogfood/journeys/journey-10-failures.sh
//
// Tests: invalid token → exit 4, build failure → FAILED, Rule 3 enforcement
// Fixture: broken-build (intentionally failing Dockerfile)
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/getloopscale/loopzero/infra/scripts/seed"
)

var _ = Describe("Journey 10: Failure Modes", Label("journey"), func() {
	Context("F1: Invalid Token", func() {
		It("returns exit code 4 for invalid token", func() {
			skipIfNotE2E()

			stdout, stderr, exitCode := execLoopZero(
				loopzeroInvocation{env: []string{"LOOPZERO_TOKEN=garbage-not-a-real-jwt"}},
				"whoami", "--json")

			Expect(exitCode).To(Equal(4),
				"invalid token should produce exit code 4\nstdout: %s\nstderr: %s", stdout, stderr)

			assertNoForbiddenTermsInAll(map[string]string{
				"invalid token stdout": stdout,
				"invalid token stderr": stderr,
			})
		})
	})

	Context("F3: Build Failure (broken Dockerfile)", Ordered, func() {
		var (
			workDir string
			buildID string
		)

		BeforeAll(func() {
			skipIfNotE2E()
			seedE2E()

			ensureProjectDeleted("broken-build")

			workDir = copyFixture("broken-build")
			initGitRepo(workDir)
		})

		AfterAll(func() {
			ensureProjectDeleted("broken-build")
			if workDir != "" {
				os.RemoveAll(workDir)
			}
		})

		It("submits deploy and build fails", func() {
			stdout, stderr, exitCode := runLoopZeroDeploy(workDir, "--env", "production", "--no-wait", "--json")

			// Deploy submission should succeed (exit 0) since --no-wait.
			Expect(exitCode).To(Equal(0),
				"deploy --no-wait should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

			// Extract build_id from JSON output.
			lines := strings.Split(strings.TrimSpace(stdout), "\n")
			for i := len(lines) - 1; i >= 0; i-- {
				result, err := parseJSON(lines[i])
				if err == nil {
					if id, ok := result["build_id"].(string); ok {
						buildID = id
						break
					}
				}
			}
			Expect(buildID).NotTo(BeEmpty(), "should extract build_id from deploy output")
		})

		It("build reaches FAILED terminal state", func() {
			status := waitBuildTerminal(buildID, 10*deployTimeout/25) // ~10 min for broken build
			Expect(status).To(Equal("FAILED"),
				"broken-build should reach FAILED state, got %s", status)
		})

		It("status output has no internal terms", func() {
			stdout, stderr, exitCode := runLoopZeroInDir(workDir, "status", "--env", "production")

			expectAllowedExitCode(exitCode, "status after failed build")
			assertNoForbiddenTermsInAll(map[string]string{
				"status stdout": stdout,
				"status stderr": stderr,
			})
		})

		It("cleanup: Prova de Morte", func() {
			ensureProjectDeleted("broken-build")
			proveDead(seed.TenantID, "broken-build", "production")
		})
	})
})
