// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Journey 01: Solo Deploy + Proof of Life + Proof of Death
//
// Migrated from: infra/tests/dogfood/journeys/journey-01-solo-deploy.sh
//
// Flow: validate → deploy → build SUCCEEDED → HTTP 200 → audit → delete → prove_dead
// Fixture: solo-server (single app: api, type: server)
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/getloopscale/loopzero/infra/scripts/seed"
)

var _ = Describe("Journey 01: Solo Deploy", Ordered, Label("journey", "smoke"), func() {
	const (
		project = "solo-server"
		app     = "api"
		env     = "j-solo"
	)

	var (
		workDir string
		buildID string
		host    string
	)

	BeforeAll(func() {
		skipIfNotE2E()
		seedE2E()

		host = seed.TenantID + "-" + project + "-" + app + "-" + env + "." + e2eBaseDomain()

		// Idempotent cleanup from previous runs.
		ensureProjectDeleted(project)

		// Copy fixture and init git repo.
		workDir = copyFixture("solo-server")
		initGitRepo(workDir)
	})

	AfterAll(func() {
		// Cleanup working directory.
		if workDir != "" {
			os.RemoveAll(workDir)
		}
	})

	It("validates project structure", func() {
		stdout, stderr, exitCode := runLoopZeroInDir(workDir, "validate", "--json")

		Expect(exitCode).To(Equal(0),
			"loopzero validate should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		assertNoForbiddenTermsInAll(map[string]string{
			"validate stdout": stdout,
			"validate stderr": stderr,
		})

		Expect(strings.ToLower(stdout)).To(ContainSubstring("true"),
			"validate output should indicate valid project")
	})

	It("deploys and build succeeds", func() {
		var result map[string]interface{}
		buildID, result = deployAndWaitSuccess(workDir, env, deployTimeout)

		Expect(result).To(HaveKey("build_id"))

		assertNoForbiddenTermsInAll(map[string]string{
			"deploy stdout": buildID, // buildID is extracted from stdout
		})
	})

	It("app responds HTTP 200 (Prova de Vida)", func() {
		waitAppHealthyViaKubectl(project, app, env, "/health", httpProbeTimeout)

		// Verify response contains project name.
		_, body, err := httpGetWithHost(host, "/")
		Expect(err).NotTo(HaveOccurred())
		Expect(body).To(ContainSubstring(project),
			"app response should contain project name %q", project)
	})

	It("audit log records deploy", func() {
		// CHG-2026-05-06-AUDIT-RESOURCE-ID: middleware now snoops the JSON
		// body for project_id, so build.submitted entries carry the
		// project as resource_id (was empty before).
		assertAuditEntry("build.submitted", project)
	})

	It("project delete succeeds", func() {
		stdout, stderr, exitCode := runLoopZeroInDir(workDir, "project", "delete", project, "--yes", "--json")

		Expect(exitCode).To(Equal(0),
			"loopzero project delete should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		assertNoForbiddenTermsInAll(map[string]string{
			"delete stdout": stdout,
			"delete stderr": stderr,
		})
	})

	It("Prova de Morte: cleanup complete", func() {
		proveDead(seed.TenantID, project, env)
	})
})
