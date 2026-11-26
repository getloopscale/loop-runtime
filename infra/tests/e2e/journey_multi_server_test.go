// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Journey 02: Multi-Server Monorepo
//
// Migrated from: infra/tests/dogfood/journeys/journey-02-multi-server.sh
//
// Flow: validate (2 apps) → deploy → 2 builds → API HTTP 200 → worker pod Running → cleanup
// Fixture: multi-server (api: server + worker: worker)
package e2e

import (
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/getloopscale/loop-runtime/infra/scripts/seed"
)

var _ = Describe("Journey 02: Multi-Server", Ordered, Label("journey"), func() {
	const (
		project = "multi-server"
		env     = "j-multi"
	)

	var (
		workDir string
		buildID string
		apiHost string
		ns      string
	)

	BeforeAll(func() {
		skipIfNotE2E()
		seedE2E()

		apiHost = seed.TenantID + "-" + project + "-api-" + env + "." + e2eBaseDomain()
		ns = mustE2ENamespace(project, env)

		ensureProjectDeleted(project)

		workDir = copyFixture("multi-server")
		initGitRepo(workDir)
	})

	AfterAll(func() {
		if workDir != "" {
			os.RemoveAll(workDir)
		}
	})

	It("validates project with 2 apps", func() {
		stdout, stderr, exitCode := runLoopRuntimeInDir(workDir, "validate", "--json")
		Expect(exitCode).To(Equal(0),
			"loopruntime validate should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		assertNoForbiddenTermsInAll(map[string]string{
			"validate stdout": stdout,
			"validate stderr": stderr,
		})
	})

	It("deploys and build succeeds", func() {
		buildID, _ = deployAndWaitSuccess(workDir, env, deployTimeout)
		Expect(buildID).NotTo(BeEmpty())
	})

	It("workload names are project-scoped", func() {
		// Workload primitive is Deployment (legacy) OR Rollout
		// (argo-rollouts-progressive-delivery-plan T1.3, default-on per
		// LOOPRUNTIME_PROGRESSIVE_DELIVERY=true). Query both kinds via comma-list
		// so this spec is forward-compatible regardless of which renderer
		// path generated the manifests.
		//
		// 2026-05-09 cleanroom dogfood: this assertion intermittently
		// failed because `loopruntime deploy` SSE emits `deploy.completed` on
		// Application Healthy, which fires marginally before the Rollout
		// resource is created (Application sync creates Rollout async).
		// Eventually-poll for up to 90s — workload primitive should
		// converge well within that window in cleanroom-cold-start.
		Eventually(func() []string {
			cmd := kubectlForCluster(ClusterWorkload,
				"-n", ns, "get", "deployments.apps,rollouts.argoproj.io",
				"-l", "runtime.getloopscale.com/project="+project,
				"--no-headers", "-o", "custom-columns=NAME:.metadata.name")
			output, err := cmd.CombinedOutput()
			if err != nil {
				return nil
			}
			return strings.Fields(strings.TrimSpace(string(output)))
		}, 90*time.Second, 3*time.Second).Should(
			And(
				ContainElement("multi-server-api"),
				ContainElement("multi-server-worker"),
			),
			"workload primitives should converge after Application sync",
		)
	})

	It("API responds HTTP 200", func() {
		waitHTTP200WithHost(apiHost, "/health", httpProbeTimeout)

		_, body, err := httpGetWithHost(apiHost, "/")
		Expect(err).NotTo(HaveOccurred())
		Expect(body).To(ContainSubstring("api"))
		Expect(body).To(ContainSubstring(project))
	})

	It("worker has no IngressRoute", func() {
		// Positive control: an absence query with --ignore-not-found against a
		// namespace that does not exist returns empty and exit 0, so without
		// this the assertion below passed over nothing.
		nsOut, nsErr := kubectlForCluster(ClusterWorkload,
			"get", "namespace", ns, "-o", "name").CombinedOutput()
		Expect(nsErr).NotTo(HaveOccurred(),
			"workload namespace %s must exist before its absence of an IngressRoute means anything: %s",
			ns, strings.TrimSpace(string(nsOut)))

		cmd := kubectlForCluster(ClusterWorkload,
			"-n", ns, "get", "ingressroute.traefik.io",
			"-l", "runtime.getloopscale.com/project="+project+",runtime.getloopscale.com/app-name=worker",
			"--no-headers", "--ignore-not-found", "-o", "name")
		output, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(string(output))).To(BeEmpty(),
			"worker should not have an IngressRoute")
	})

	It("worker pod is Running", func() {
		cmd := kubectlForCluster(ClusterWorkload,
			"-n", ns, "get", "pod",
			"-l", "runtime.getloopscale.com/app-name=worker",
			"--field-selector=status.phase=Running",
			"--no-headers", "-o", "name")
		output, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(string(output))).NotTo(BeEmpty(),
			"expected at least one Running worker pod")
	})

	It("audit log records deploy", func() {
		assertAuditEntry("build.submitted", project)
	})

	It("project delete succeeds and cleanup complete", func() {
		stdout, stderr, exitCode := runLoopRuntimeInDir(workDir, "project", "delete", project, "--yes", "--json")
		Expect(exitCode).To(Equal(0),
			"project delete should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		proveDead(seed.TenantID, project, env)
	})
})
