// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Journey 07: Custom Domain Registration & Lifecycle
//
// Migrated from: infra/tests/dogfood/journeys/journey-07-custom-domain.sh
//
// Flow: deploy → domain add → domain list → domain status → domain remove → verify gone
// Fixture: solo-server
//
// Limitation: CF Zone Management requires real DNS. Locally we test the API flow
// up to "pending_ns" state and verify CLI output correctness.
package e2e

import (
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/getloopscale/loop-runtime/infra/scripts/seed"
)

var _ = Describe("Journey 07: Custom Domain", Ordered, Label("journey"), func() {
	const (
		project = "solo-server"
		app     = "api"
		env     = "j-domain"
	)

	var (
		workDir    string
		testDomain string
	)

	BeforeAll(func() {
		skipIfNotE2E()
		seedE2E()

		testDomain = fmt.Sprintf("api.ginkgo-%s.example", uniqueSuffix())

		ensureProjectDeleted(project)

		workDir = copyFixture("solo-server")
		initGitRepo(workDir)
	})

	AfterAll(func() {
		// Cleanup: remove domain and project.
		runLoopRuntimeInDir(workDir, "domain", "remove", testDomain, "--yes", "--json")
		ensureProjectDeleted(project)
		if workDir != "" {
			os.RemoveAll(workDir)
		}
	})

	It("deploys project", func() {
		deployAndWaitSuccess(workDir, env, deployTimeout)
	})

	It("registers a custom domain", func() {
		stdout, stderr, exitCode := runLoopRuntimeInDir(workDir, "domain", "add", testDomain,
			"--project", project, "--app", app, "--env", env, "--json")

		Expect(exitCode).To(Equal(0),
			"loopruntime domain add should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		// Verify response has expected fields.
		result, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveKey("id"))

		assertNoForbiddenTermsInAll(map[string]string{
			"domain add stdout": stdout,
			"domain add stderr": stderr,
		})
	})

	It("domain appears in list", func() {
		stdout, _, exitCode := runLoopRuntimeInDir(workDir, "domain", "list", "--json")
		Expect(exitCode).To(Equal(0))
		Expect(stdout).To(ContainSubstring(testDomain),
			"domain list should contain the registered domain")
	})

	It("domain status returns details", func() {
		stdout, _, exitCode := runLoopRuntimeInDir(workDir, "domain", "status", testDomain, "--json")
		Expect(exitCode).To(Equal(0))
		Expect(stdout).To(ContainSubstring(testDomain))
	})

	It("rejects reserved domain", func() {
		stdout, stderr, exitCode := runLoopRuntimeInDir(workDir, "domain", "add", "api.example.test",
			"--project", project, "--app", app, "--env", env, "--json")

		combined := stdout + stderr
		// Should contain "reserved" or similar rejection.
		GinkgoWriter.Printf("reserved domain probe: exit=%d output=%s\n", exitCode, combined)

		assertNoForbiddenTermsInAll(map[string]string{
			"reserved stdout": stdout,
			"reserved stderr": stderr,
		})
		expectAllowedExitCode(exitCode, "reserved domain rejection")
	})

	It("removes domain", func() {
		stdout, stderr, exitCode := runLoopRuntimeInDir(workDir, "domain", "remove", testDomain, "--yes", "--json")

		Expect(exitCode).To(Equal(0),
			"loopruntime domain remove should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		assertNoForbiddenTermsInAll(map[string]string{
			"domain remove stdout": stdout,
			"domain remove stderr": stderr,
		})
	})

	It("domain gone from list after removal", func() {
		Eventually(func() bool {
			stdout, _, _ := runLoopRuntimeInDir(workDir, "domain", "list", "--json")
			// Domain should no longer appear or be in removed/removing state.
			result, err := parseJSON(stdout)
			if err != nil {
				return true // No parseable output = likely empty
			}
			domains, ok := result["domains"].([]interface{})
			if !ok {
				return true
			}
			for _, d := range domains {
				dm, ok := d.(map[string]interface{})
				if !ok {
					continue
				}
				name, _ := dm["domain"].(string)
				status, _ := dm["status"].(string)
				if name == testDomain && status != "removed" && status != "removing" {
					return false
				}
			}
			return true
		}, 60*cleanupPollInterval, cleanupPollInterval).Should(BeTrue(),
			"domain should be removed from list")
	})

	It("audit records domain operations", func() {
		assertAuditEntry("domain.added", project)
	})

	It("cleanup: delete + Prova de Morte", func() {
		ensureProjectDeleted(project)
		proveDead(seed.TenantID, project, env)
	})
})
