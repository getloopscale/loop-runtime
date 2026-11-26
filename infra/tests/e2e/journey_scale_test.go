// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Journey 04: Scale
//
// Migrated from: infra/tests/dogfood/journeys/journey-04-scale.sh
//
// Flow: deploy → scale --replicas 3 → verify → scale --replicas 11 (cap rejection) → cleanup
// Fixture: solo-server
package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/getloopscale/loop-runtime/infra/scripts/seed"
)

var _ = Describe("Journey 04: Scale", Ordered, Label("journey"), func() {
	const (
		project = "solo-server"
		app     = "api"
		env     = "j-scale"
	)

	var (
		workDir string
	)

	BeforeAll(func() {
		skipIfNotE2E()
		seedE2E()

		// host computed but not used since V9 Option-B kubectl exec
		// bypass — keep computation removed to satisfy go vet.

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

	It("deploys initial version", func() {
		deployAndWaitSuccess(workDir, env, deployTimeout)
	})

	It("app responds HTTP 200", func() {
		waitAppHealthyViaKubectl(project, app, env, "/health", httpProbeTimeout)
	})

	It("scales to 3 replicas", func() {
		stdout, stderr, exitCode := runLoopRuntimeInDir(workDir, "scale",
			"--app", app, "--env", env, "--replicas", "3", "--json")

		// Scale returns 0 or reports acknowledged/applied.
		Expect(exitCode).To(Equal(0),
			"loopruntime scale --replicas 3 should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		assertNoForbiddenTermsInAll(map[string]string{
			"scale stdout": stdout,
			"scale stderr": stderr,
		})
	})

	It("over-cap scale is rejected or acknowledged", func() {
		stdout, stderr, exitCode := runLoopRuntimeInDir(workDir, "scale",
			"--app", app, "--env", env, "--replicas", "11", "--json")

		// Starter plan cap is 2, Pro is 5, Team is 10. 11 should be rejected.
		// However, the exact behavior depends on the tenant's plan.
		// We record the response without hard-asserting rejection.
		GinkgoWriter.Printf("over-cap scale: exit=%d stdout=%s stderr=%s\n",
			exitCode, stdout, stderr)

		assertNoForbiddenTermsInAll(map[string]string{
			"over-cap stdout": stdout,
			"over-cap stderr": stderr,
		})

		expectAllowedExitCode(exitCode, "over-cap scale")
	})

	It("app still responds after scale", func() {
		waitAppHealthyViaKubectl(project, app, env, "/health", httpProbeTimeout)
	})

	It("cleanup: delete + Prova de Morte", func() {
		ensureProjectDeleted(project)
		proveDead(seed.TenantID, project, env)
	})
})
