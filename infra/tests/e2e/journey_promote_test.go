// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Journey 05: Promote staging → production
//
// Migrated from: infra/tests/dogfood/journeys/journey-05-promote.sh
//
// Flow: deploy v1 to staging → verify staging HTTP → promote to production → verify production HTTP
// Fixture: versioned-app (with VERSION.txt)
package e2e

import (
	"os"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("Journey 05: Promote", Ordered, Label("journey"), func() {
	const (
		project = "versioned-app"
		app     = "api"
	)

	var (
		workDir     string
		buildID     string
		stagingHost string
		prodHost    string
	)

	BeforeAll(func() {
		skipIfNotE2E()
		seedE2E()

		stagingHost = seed.TenantID + "-" + project + "-" + app + "-staging.loopzero.local"
		prodHost = seed.TenantID + "-" + project + "-" + app + "-production.loopzero.local"

		ensureProjectDeleted(project)

		workDir = copyFixture("versioned-app")

		// Write v1 to VERSION.txt.
		err := os.WriteFile(workDir+"/src/VERSION.txt", []byte("v1"), 0644)
		Expect(err).NotTo(HaveOccurred())

		initGitRepo(workDir)

		// Create staging environment (idempotent).
		runLoopZeroInDir(workDir, "env", "create", "staging", "--json")
	})

	AfterAll(func() {
		ensureProjectDeleted(project)
		if workDir != "" {
			os.RemoveAll(workDir)
		}
	})

	It("deploys v1 to staging", func() {
		buildID, _ = deployAndWaitSuccess(workDir, "staging", deployTimeout)
		Expect(buildID).NotTo(BeEmpty())
	})

	It("staging responds HTTP 200 with v1", func() {
		waitHTTP200WithHost(stagingHost, "/health", httpProbeTimeout)

		Eventually(func() string {
			_, body, _ := httpGetWithHost(stagingHost, "/version")
			return body
		}, releaseActiveTimeout, httpPollInterval).Should(
			ContainSubstring("v1"),
			"staging /version should return v1")
	})

	It("staging release becomes ACTIVE", func() {
		waitReleaseActive(project, "staging", buildID, releaseActiveTimeout)
	})

	It("promotes staging to production", func() {
		stdout, stderr, exitCode := runLoopZeroInDir(workDir, "promote",
			"--from", "staging", "--to", "production", "--yes", "--json")

		Expect(exitCode).To(Equal(0),
			"loopzero promote should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		// Verify promote response has expected fields.
		Expect(stdout).To(SatisfyAny(
			ContainSubstring("active_release"),
			ContainSubstring("reactivated_release"),
		), "promote response should reference the activated release")

		assertNoForbiddenTermsInAll(map[string]string{
			"promote stdout": stdout,
			"promote stderr": stderr,
		})
	})

	It("production responds HTTP 200 with v1", func() {
		waitHTTP200WithHost(prodHost, "/health", httpProbeTimeout)

		Eventually(func() string {
			_, body, _ := httpGetWithHost(prodHost, "/version")
			return body
		}, releaseActiveTimeout, httpPollInterval).Should(
			ContainSubstring("v1"),
			"production /version should return v1 after promote")
	})

	It("audit records promote", func() {
		assertAuditEntry("release.promoted", project)
	})

	It("cleanup: delete + Prova de Morte", func() {
		ensureProjectDeleted(project)

		// Wait for both environments to clean up.
		proveDead(seed.TenantID, project, "production")

		// Staging cleanup may take longer — use Eventually.
		Eventually(func() bool {
			cmd := kubectlWorkload("get", "namespace",
				"loopzero-"+seed.TenantID+"-staging",
				"--no-headers", "-o", "name")
			return cmd.Run() != nil // true when namespace is gone
		}, cleanupTimeout, cleanupPollInterval).Should(BeTrue(),
			"staging namespace should be deleted")
	})
})

// kubectlWorkload creates a kubectl command targeting the workload cluster.
func kubectlWorkload(args ...string) *exec.Cmd {
	allArgs := append([]string{"--context", workloadKubeContext}, args...)
	return exec.Command("kubectl", allArgs...)
}
