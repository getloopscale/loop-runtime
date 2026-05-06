// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Journey 08: GitHub Auto-Deploy (Connect/Disconnect)
//
// Migrated from: infra/tests/dogfood/journeys/journey-08-github-autodeploy.sh
//
// Flow: connect repo → verify DB row → idempotent reconnect → disconnect → verify cleanup
//
// Limitation: Full webhook → auto-deploy flow requires public ingress.
// Locally we test the connect/disconnect lifecycle and audit trail.
package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Journey 08: GitHub Autodeploy", Ordered, Label("journey"), func() {
	var (
		project string
	)

	BeforeAll(func() {
		skipIfNotE2E()
		seedE2E()

		project = fmt.Sprintf("ginkgo-connect-%s", uniqueSuffix())
	})

	AfterAll(func() {
		// Cleanup: disconnect if still connected.
		runLoopZero("disconnect", "--project", project, "--json")
		ensureProjectDeleted(project)
	})

	It("connects a GitHub repository", func() {
		stdout, stderr, exitCode := runLoopZero("connect",
			"--repo", "getloopscale/loopzero-repotest",
			"--installation-id", "99999999",
			"--branch", "main",
			"--project", project,
			"--json")

		Expect(exitCode).To(Equal(0),
			"loopzero connect should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		result, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(result["ok"]).To(BeEquivalentTo(true),
			"connect response should have ok=true")

		assertNoForbiddenTermsInAll(map[string]string{
			"connect stdout": stdout,
			"connect stderr": stderr,
		})
	})

	It("idempotent reconnect succeeds without duplicate", func() {
		stdout, _, exitCode := runLoopZero("connect",
			"--repo", "getloopscale/loopzero-repotest",
			"--installation-id", "99999999",
			"--branch", "main",
			"--project", project,
			"--json")

		Expect(exitCode).To(Equal(0))

		result, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(result["ok"]).To(BeEquivalentTo(true),
			"idempotent reconnect should succeed")
	})

	It("disconnects repository", func() {
		stdout, stderr, exitCode := runLoopZero("disconnect",
			"--project", project,
			"--json")

		Expect(exitCode).To(Equal(0),
			"loopzero disconnect should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		result, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(result["ok"]).To(BeEquivalentTo(true))

		assertNoForbiddenTermsInAll(map[string]string{
			"disconnect stdout": stdout,
			"disconnect stderr": stderr,
		})
	})

	It("audit records connect and disconnect", func() {
		assertAuditEntry("repository.connected", project)
	})
})
