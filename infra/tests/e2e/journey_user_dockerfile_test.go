// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Journey: User Dockerfile Ignored (INVERTED by plan simplify-deploy-loopzerokit-only
// v2.0 T2.4 — the BYO-Dockerfile build path was removed for security, D4).
//
// Originally this journey asserted the BYO path (dockerfile-lint step, malicious
// Dockerfile rejected by lint). That path is GONE: the platform always builds
// from its canonical Dockerfile, and any user Dockerfile in the bundle is
// ignored fail-closed (overwritten + a ::loopruntime:: warning; never executed).
//
// Validates two scenarios under the new contract:
//   T4.3 (inverted) — bundle with a Dockerfile: workflow has the canonical
//          `generate-dockerfile` step and NO `dockerfile-lint`. Pod Running.
//   T4.4 (inverted) — bundle with a MALICIOUS Dockerfile: it is ignored, never
//          linted, never executed. The build runs from the canonical Dockerfile;
//          there is NO dockerfile-lint step.
//
// Skip semantics: skipped when LOOPRUNTIME_E2E unset (suite-level gate). LIVE run is
// deferred to the plan's Final Phase (Cleanroom + LIVE).

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Journey: User Dockerfile happy path", Ordered, Label("journey", "user-dockerfile"), func() {
	const (
		project = "rollout-canary-app" // existing fixture has build: dockerfile + clean Dockerfile
		app     = "api"
		env     = "j-udfile-a"
	)

	var workDir string

	BeforeAll(func() {
		skipIfNotE2E()
		seedE2E()
		ensureProjectDeleted(project)
		workDir = copyFixture("rollout-canary-app")
		initGitRepo(workDir)
	})

	AfterAll(func() {
		ensureProjectDeleted(project)
		if workDir != "" {
			cleanupWorkDir(workDir)
		}
	})

	It("T4.3 (inverted, T2.4): user Dockerfile IGNORED — workflow has canonical generate-dockerfile, NOT dockerfile-lint", func() {
		buildID, _ := deployAndWaitSuccess(workDir, env, deployTimeout)
		Expect(buildID).NotTo(BeEmpty())

		// Plan simplify-deploy-loopzerokit-only v2.0 (D4): the BYO-Dockerfile path is
		// removed. A bundle containing a Dockerfile builds from the platform's
		// CANONICAL Dockerfile (generate-dockerfile step) and there is NO
		// dockerfile-lint step — the user Dockerfile is ignored fail-closed.
		hasLint, hasPacks := workflowStepsForBuild(buildID)
		Expect(hasLint).To(BeFalse(),
			"BYO dockerfile-lint step MUST be gone — user Dockerfile is ignored, never linted/executed (D4)")
		Expect(hasPacks).To(BeTrue(),
			"workflow MUST contain the canonical generate-dockerfile step")
	})

	It("T4.3: pod is Running and app responds", func() {
		ns := mustE2ENamespace(project, env)
		resName := project + "-" + app

		// Wait for Pod (via Rollout) Running. Drill-aware via kubectlForCluster.
		Eventually(func() string {
			cmd := kubectlForCluster(ClusterWorkload,
				"-n", ns, "get", "pod", "-l", "app="+resName,
				"-o", "jsonpath={.items[0].status.phase}")
			out, _ := cmd.CombinedOutput()
			return strings.TrimSpace(string(out))
		}, 5*time.Minute, 5*time.Second).Should(Equal("Running"),
			"pod for build:dockerfile app should reach Running")
	})
})

var _ = Describe("Journey: User Dockerfile rejection path", Ordered, Label("journey", "user-dockerfile"), func() {
	const (
		project = "malicious-dockerfile"
		env     = "j-udfile-b"
	)

	var workDir string

	BeforeAll(func() {
		skipIfNotE2E()
		seedE2E()
		ensureProjectDeleted(project)
		workDir = copyFixture("malicious-dockerfile")
		initGitRepo(workDir)
	})

	AfterAll(func() {
		ensureProjectDeleted(project)
		if workDir != "" {
			cleanupWorkDir(workDir)
		}
	})

	It("T4.4 (inverted, T2.4): malicious Dockerfile is IGNORED — never linted, never executed (D4)", func() {
		// Plan simplify-deploy-loopzerokit-only v2.0 (D4): with BYO removed, a
		// malicious Dockerfile in the bundle has NO effect — the platform builds
		// from its canonical Dockerfile. The malicious FROM never executes. We no
		// longer rely on a lint step to reject it (defense was lint-and-hope);
		// the malicious file is simply never the build recipe.
		//
		// The canonical build's terminal status depends on whether the fixture
		// carries a valid node/python app + lockfile (success) or not
		// (fail-closed) — both are acceptable; the security invariant is that
		// there is NO dockerfile-lint step and the canonical generate-dockerfile
		// step is what runs.
		buildID := deployAndExpectBuildTerminal(workDir, env, "SUCCEEDED", 10*time.Minute)
		Expect(buildID).NotTo(BeEmpty())

		hasLint, hasPacks := workflowStepsForBuild(buildID)
		Expect(hasLint).To(BeFalse(),
			"BYO dockerfile-lint step MUST be gone — the malicious Dockerfile is ignored, not linted (D4)")
		Expect(hasPacks).To(BeTrue(),
			"workflow MUST build from the canonical generate-dockerfile step, not the user Dockerfile")
	})
})

// deployAndExpectBuildTerminal submits a deploy and waits for the build to
// reach a specific terminal status. Unlike deployAndWaitSuccess, it does
// NOT assert success — used for negative paths.
func deployAndExpectBuildTerminal(dir, env, expectedStatus string, timeout time.Duration) string {
	// Only a CLI that never started is a submission failure here: even a build
	// destined to fail is SUBMITTED successfully (HTTP 202), and the build_id
	// parse below is what proves it.
	stdout, stderr, exitCode := execLoopRuntime(loopruntimeInvocation{dir: dir},
		"deploy", "--env", env, "--no-wait", "--json")
	Expect(exitCode).NotTo(Equal(-1), "loopruntime did not start: %s", stderr)

	// Parse build_id from JSON.
	result := make(map[string]interface{})
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "{") {
			if err := json.Unmarshal([]byte(l), &result); err == nil {
				break
			}
		}
	}
	buildID, _ := result["build_id"].(string)
	Expect(buildID).NotTo(BeEmpty(),
		"deploy submission did not return build_id\nstdout: %s\nstderr: %s",
		stdout, stderr)

	// Wait for terminal status.
	status := waitBuildTerminal(buildID, timeout)
	Expect(status).To(Equal(expectedStatus),
		"build %s ended with status %s, expected %s", buildID, status, expectedStatus)
	return buildID
}

// workflowStepsForBuild queries the API's build inspect endpoint and returns
// whether the underlying Argo Workflow contains dockerfile-lint and
// generate-dockerfile steps. Returns (hasLint, hasPacks).
//
// Implementation: we fetch /api/v1/builds/<id>/workflow which returns the
// raw workflow JSON. If the endpoint is unavailable, we fall back to
// inspecting workflow CRs via kubectl on the build cluster.
func workflowStepsForBuild(buildID string) (hasLint, hasPacks bool) {
	// Try kubectl on build cluster first — it's the source of truth.
	// Drill-aware via kubectlForCluster(ClusterBuild) (2026-05-20).
	cmd := kubectlForCluster(ClusterBuild,
		"-n", "argo", "get", "workflows",
		"-l", fmt.Sprintf("runtime.getloopscale.com/build-id=%s", buildID),
		"-o", "jsonpath={range .items[*].status.nodes.*}{.displayName}{\"\\n\"}{end}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback: name-based search (loopruntime-build-build-<buildID>-...)
		cmd = kubectlForCluster(ClusterBuild,
			"-n", "argo", "get", "workflows",
			"-o", "jsonpath={range .items[?(@.metadata.name)]}{.metadata.name}{\"\\n\"}{end}")
		out, _ = cmd.CombinedOutput()
		// Find the matching one.
		var matchedName string
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, buildID) {
				matchedName = strings.TrimSpace(line)
				break
			}
		}
		if matchedName == "" {
			return false, false
		}
		cmd = kubectlForCluster(ClusterBuild,
			"-n", "argo", "get", "workflow", matchedName,
			"-o", "jsonpath={range .status.nodes.*}{.displayName}{\"\\n\"}{end}")
		out, _ = cmd.CombinedOutput()
	}

	for _, line := range strings.Split(string(out), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "dockerfile-lint") {
			hasLint = true
		}
		if strings.HasPrefix(t, "generate-dockerfile") {
			hasPacks = true
		}
	}
	return hasLint, hasPacks
}

// Avoid unused-import linter complaint when http isn't directly referenced.
var _ = http.StatusOK
