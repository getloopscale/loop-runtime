// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Journey 06: Rollback
//
// Migrated from: infra/tests/dogfood/journeys/journey-06-rollback.sh
//
// Flow: deploy v1 → verify → deploy v2 → verify → rollback → verify v1 again
// Fixture: versioned-app (VERSION.txt manipulated between deploys)
package e2e

import (
	"os"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/getloopscale/loop-runtime/infra/scripts/seed"
)

var _ = Describe("Journey 06: Rollback", Ordered, Label("journey"), func() {
	const (
		project = "versioned-app"
		app     = "api"
		env     = "j-rollback"
	)

	var (
		workDir string
		host    string
		buildV1 string
		buildV2 string
	)

	BeforeAll(func() {
		skipIfNotE2E()
		seedE2E()

		host = seed.TenantID + "-" + project + "-" + app + "-" + env + "." + e2eBaseDomain()

		ensureProjectDeleted(project)

		workDir = copyFixture("versioned-app")

		// Write v1.
		err := os.WriteFile(workDir+"/src/VERSION.txt", []byte("v1"), 0644)
		Expect(err).NotTo(HaveOccurred())

		initGitRepo(workDir)
	})

	AfterAll(func() {
		ensureProjectDeleted(project)
		if workDir != "" {
			os.RemoveAll(workDir)
		}
	})

	It("deploys v1", func() {
		buildV1, _ = deployAndWaitSuccess(workDir, env, deployTimeout)
		Expect(buildV1).NotTo(BeEmpty())
	})

	It("v1 responds with correct version", func() {
		waitAppHealthyViaKubectl(project, app, env, "/health", httpProbeTimeout)

		Eventually(func() string {
			_, body, _ := httpGetWithHost(host, "/version")
			return body
		}, releaseActiveTimeout, httpPollInterval).Should(
			ContainSubstring("v1"),
			"/version should return v1")
	})

	It("v1 release is ACTIVE", func() {
		waitReleaseActive(project, env, buildV1, releaseActiveTimeout)
	})

	It("deploys v2", func() {
		// Update VERSION.txt to v2.
		err := os.WriteFile(workDir+"/src/VERSION.txt", []byte("v2"), 0644)
		Expect(err).NotTo(HaveOccurred())

		// Commit the change.
		for _, args := range [][]string{
			{"git", "add", "-A"},
			{"git", "commit", "-m", "bump to v2"},
		} {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = workDir
			output, err := cmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(),
				"git %v failed: %s", args, string(output))
		}

		buildV2, _ = deployAndWaitSuccess(workDir, env, deployTimeout)
		Expect(buildV2).NotTo(BeEmpty())
		Expect(buildV2).NotTo(Equal(buildV1))
	})

	It("v2 responds with correct version", func() {
		Eventually(func() string {
			_, body, _ := httpGetWithHost(host, "/version")
			return body
		}, releaseActiveTimeout, httpPollInterval).Should(
			ContainSubstring("v2"),
			"/version should return v2")
	})

	It("v2 release is ACTIVE", func() {
		waitReleaseActive(project, env, buildV2, releaseActiveTimeout)
	})

	It("rollback to previous release", func() {
		stdout, stderr, exitCode := runLoopZeroInDir(workDir, "rollback",
			"--env", env, "--yes", "--json")

		Expect(exitCode).To(Equal(0),
			"loopzero rollback should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		Expect(stdout).To(SatisfyAny(
			ContainSubstring("rolled_back_release"),
			ContainSubstring("active_release"),
			ContainSubstring("reactivated_release"),
		), "rollback response should reference the reactivated release")

		assertNoForbiddenTermsInAll(map[string]string{
			"rollback stdout": stdout,
			"rollback stderr": stderr,
		})
	})

	It("app reverts to v1 after rollback", func() {
		Eventually(func() string {
			_, body, _ := httpGetWithHost(host, "/version")
			return body
		}, releaseActiveTimeout, httpPollInterval).Should(
			ContainSubstring("v1"),
			"/version should return v1 after rollback")
	})

	It("audit records rollback", func() {
		assertAuditEntry("rollback", project)
	})

	It("cleanup: delete + Prova de Morte", func() {
		ensureProjectDeleted(project)
		proveDead(seed.TenantID, project, env)
	})
})
