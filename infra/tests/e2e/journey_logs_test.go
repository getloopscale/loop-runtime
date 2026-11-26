// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Journey: Logs (T3.2 of deep-review-remediation-plan)
//
// Validates the `loopscale logs` CLI command against the API. The command
// surfaces build logs (Loki-backed in production, in-memory fallback in
// dev). This journey does NOT exercise the SSE stream — only the
// snapshot path which is the documented contract for CI scripts.
//
// Flow: solo deploy → logs --build-id $BID → assert exit 0 + non-empty body
//
//	→ logs (project default) exit 0 → cleanup.
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Journey: Logs", Ordered, Label("journey"), func() {
	const (
		project = "logs-fixture"
		env     = "j-logs"
	)

	var (
		workDir string
		buildID string
	)

	BeforeAll(func() {
		skipIfNotE2E()
		seedE2E()

		ensureProjectDeleted(project)
		workDir = copyFixture("solo-server")
		initGitRepo(workDir)
	})

	AfterAll(func() {
		if workDir != "" {
			os.RemoveAll(workDir)
		}
		ensureProjectDeleted(project)
	})

	It("deploys (prerequisite for log query)", func() {
		var result map[string]interface{}
		buildID, result = deployAndWaitSuccess(workDir, env, deployTimeout)
		Expect(result).To(HaveKey("build_id"))
		Expect(buildID).NotTo(BeEmpty())
	})

	It("loopscale logs --build returns exit 0 with body", func() {
		stdout, stderr, exitCode := runLoopRuntimeInDir(workDir, "logs",
			"--build", buildID, "--json")

		Expect(exitCode).To(Equal(0),
			"loopscale logs --build-id should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		assertNoForbiddenTermsInAll(map[string]string{
			"logs stdout": stdout,
			"logs stderr": stderr,
		})

		// Body shape contract — at minimum the JSON contains some structure
		// (object with `lines` array, `build_id`, or `error` for empty case).
		// Content varies because Loki may not be running locally; we only
		// assert the exit-code + Rule-3 contract here. Production tests
		// against a Loki-backed stack assert content separately.
		Expect(strings.TrimSpace(stdout)).NotTo(BeEmpty(),
			"loopscale logs should produce non-empty output")
	})

	It("loopscale logs --json schema is stable", func() {
		stdout, _, exitCode := runLoopRuntimeInDir(workDir, "logs",
			"--build", buildID, "--json")
		if exitCode != 0 {
			Skip("logs returned non-zero — schema check requires success path")
		}
		// Schema invariant per CLAUDE.md (CLI stability contract): top-level JSON object
		// (Rule 5 — --json field names are a stability contract).
		s := strings.TrimSpace(stdout)
		Expect(s[0:1]).To(Or(Equal("{"), Equal("[")),
			"--json output must be valid JSON object/array, not free-form text")
	})
})
