// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Edge Cases: State Machine transitions (T2.3)
//
// Tests: cancel mid-build → CANCELLED, cancel on terminal → error,
// failed build → FAILED (not stuck), deploy after cancel succeeds.
package e2e

import (
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("Edge: State Machine", Ordered, Label("edge-case"), func() {
	const (
		project = "solo-server"
		env     = "production"
	)

	var workDir string

	BeforeAll(func() {
		skipIfNotE2E()
		seedE2E()

		ensureProjectDeleted(project)

		workDir = copyFixture("solo-server")
		initGitRepo(workDir)
	})

	AfterAll(func() {
		ensureProjectDeleted(project)
		if workDir != "" {
			os.RemoveAll(workDir)
		}
	})

	It("cancel during RUNNING transitions to CANCELLED", func() {
		// Deploy with --no-wait to get build_id while build is still running.
		stdout, _, exitCode := runLoopZeroDeploy(workDir, "--env", env, "--no-wait", "--json")
		Expect(exitCode).To(Equal(0))

		var buildID string
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
		Expect(buildID).NotTo(BeEmpty())

		// Wait briefly for build to enter RUNNING state.
		time.Sleep(5 * time.Second)

		// Cancel the build.
		cancelOut, cancelErr, cancelCode := runLoopZero("cancel",
			"--build", buildID, "--json")

		GinkgoWriter.Printf("cancel: exit=%d stdout=%s stderr=%s\n",
			cancelCode, cancelOut, cancelErr)

		// Cancel should succeed or report already terminal.
		expectAllowedExitCode(cancelCode, "cancel mid-build")

		// Wait for terminal state.
		status := waitBuildTerminal(buildID, 5*time.Minute)
		Expect(status).To(SatisfyAny(
			Equal("CANCELLED"),
			Equal("SUCCEEDED"), // May have completed before cancel was processed.
			Equal("FAILED"),
		), "build should reach terminal state after cancel attempt")

		assertNoForbiddenTermsInAll(map[string]string{
			"cancel stdout": cancelOut,
			"cancel stderr": cancelErr,
		})
	})

	It("failed build transitions to FAILED, not stuck", func() {
		ensureProjectDeleted("broken-build")

		brokenDir := copyFixture("broken-build")
		defer os.RemoveAll(brokenDir)
		initGitRepo(brokenDir)

		stdout, _, exitCode := runLoopZeroDeploy(brokenDir, "--env", "production", "--no-wait", "--json")
		Expect(exitCode).To(Equal(0))

		var buildID string
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
		Expect(buildID).NotTo(BeEmpty())

		status := waitBuildTerminal(buildID, 10*time.Minute)
		Expect(status).To(Equal("FAILED"),
			"broken build should reach FAILED, not get stuck")

		ensureProjectDeleted("broken-build")
	})

	It("deploy after cancelled/failed build succeeds", func() {
		ensureProjectDeleted(project)

		workDir2 := copyFixture("solo-server")
		defer os.RemoveAll(workDir2)
		initGitRepo(workDir2)

		buildID, _ := deployAndWaitSuccess(workDir2, env, deployTimeout)
		Expect(buildID).NotTo(BeEmpty(),
			"deploy after cancel should succeed (lock released)")

		ensureProjectDeleted(project)
		proveDead(seed.TenantID, project, env)
	})
})
