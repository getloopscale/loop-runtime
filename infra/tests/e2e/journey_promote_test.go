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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/getloopscale/loop-runtime/infra/scripts/seed"
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

		stagingHost = seed.TenantID + "-" + project + "-" + app + "-staging." + e2eBaseDomain()
		prodHost = seed.TenantID + "-" + project + "-" + app + "-production." + e2eBaseDomain()

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
		// V9 L14 fix: promote can race with envEnsurer (creating production
		// env idempotently) or with the staging release still being persisted.
		// Wrap in Eventually with DEPLOY_IN_PROGRESS-aware retry to handle
		// the env-lock race. promoteTimeout (5min) is the budget for the
		// race to clear.
		var (
			stdout, stderr string
			exitCode       int
		)
		Eventually(func() int {
			stdout, stderr, exitCode = runLoopZeroInDir(workDir, "promote",
				"--from", "staging", "--to", "production", "--yes", "--json")
			return exitCode
		}, promoteTimeout, 5*time.Second).Should(Equal(0),
			"loopzero promote should eventually exit 0\nlast stdout: %s\nlast stderr: %s", stdout, stderr)

		// Verify promote response has expected fields per
		// api/internal/routes/promote.go:64 PromotionResponse.NewRelease.
		// (rollback uses "reactivated_release" and status uses
		// "active_release" — distinct contracts, do not conflate.)
		Expect(stdout).To(ContainSubstring("new_release"),
			"promote response should reference new_release")

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

		// Project delete removes labeled resources from EVERY environment
		// it lived in (production + staging here). Tenant-level namespaces
		// (loopzero-{tenant}-{env}) are shared across projects and survive a
		// project delete — only the Tenant CR teardown removes them. So
		// Prova de Morte asserts "no resources for this project" per env,
		// NOT "namespace gone".
		proveDead(seed.TenantID, project, "production")
		proveDead(seed.TenantID, project, "staging")
	})
})
