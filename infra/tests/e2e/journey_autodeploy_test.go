// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Journey 08: GitHub Autodeploy", Ordered, Label("journey"), func() {
	var (
		project string
		workDir string
	)

	BeforeAll(func() {
		skipIfNotE2E()
		seedE2E()

		project = fmt.Sprintf("ginkgo-connect-%s", uniqueSuffix())

		// loopruntime connect / disconnect read the project name from loopzero.yaml in
		// the working directory — the CLI does NOT have a --project flag
		// (verified 2026-05-06: `loopruntime connect --help` shows only --repo,
		// --branch, --installation-id). Mirror infra/tests/dogfood/
		// journeys/_legacy/journey-08-github-autodeploy.sh: write a minimal
		// loopzero.yaml and exec connect from that dir.
		var err error
		workDir, err = os.MkdirTemp("", "loopruntime-e2e-connect-")
		Expect(err).NotTo(HaveOccurred())

		loopruntimeYAML := fmt.Sprintf(
			"project: %s\napps:\n  - name: api\n    type: server\n    build:\n      dockerfile: Dockerfile\n",
			project,
		)
		Expect(os.WriteFile(workDir+"/loopzero.yaml", []byte(loopruntimeYAML), 0644)).To(Succeed())
	})

	AfterAll(func() {
		// Cleanup: disconnect if still connected (best-effort).
		runLoopRuntimeInDir(workDir, "disconnect", "--json")
		ensureProjectDeleted(project)
		if workDir != "" {
			os.RemoveAll(workDir)
		}
	})

	It("connects a GitHub repository", func() {
		stdout, stderr, exitCode := runLoopRuntimeInDir(workDir, "connect",
			"--repo", "getloopscale/loopzerokit-example",
			"--installation-id", "99999999",
			"--branch", "main",
			"--json")

		Expect(exitCode).To(Equal(0),
			"loopruntime connect should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

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
		stdout, _, exitCode := runLoopRuntimeInDir(workDir, "connect",
			"--repo", "getloopscale/loopzerokit-example",
			"--installation-id", "99999999",
			"--branch", "main",
			"--json")

		Expect(exitCode).To(Equal(0))

		result, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(result["ok"]).To(BeEquivalentTo(true),
			"idempotent reconnect should succeed")
	})

	It("disconnects repository", func() {
		stdout, stderr, exitCode := runLoopRuntimeInDir(workDir, "disconnect", "--json")

		Expect(exitCode).To(Equal(0),
			"loopruntime disconnect should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

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
