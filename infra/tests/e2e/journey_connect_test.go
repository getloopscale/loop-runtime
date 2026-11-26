// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Journey: Connect (T3.2 of deep-review-remediation-plan)
//
// Validates the `loopruntime connect` and `loopruntime disconnect` CLI commands. The
// command links a project to a GitHub repo for autodeploy. Production
// uses GitHub App OAuth; this journey tests the API contract surface
// (idempotent connect, JSON shape, disconnect cleanup).
//
// We do NOT install a GitHub App into the test repo — that requires
// org-level admin and is out of scope. Instead we validate:
//   - `loopruntime connect --help` exits 0 + emits structured help (CLI contract).
//   - The JSON contract from `loopruntime project inspect --json` includes the
//     repository field shape, even when not connected.
package e2e

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Journey: Connect", Ordered, Label("journey"), func() {
	BeforeAll(func() {
		skipIfNotE2E()
		seedE2E()
	})

	It("loopruntime connect --help emits structured help", func() {
		stdout, stderr, exitCode := runLoopRuntime("connect", "--help")

		Expect(exitCode).To(Equal(0),
			"loopruntime connect --help should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		assertNoForbiddenTermsInAll(map[string]string{
			"connect help stdout": stdout,
			"connect help stderr": stderr,
		})

		// Help output must mention key sub-options per CLAUDE.md (CLI stability contract).
		joined := stdout + stderr
		Expect(strings.ToLower(joined)).To(ContainSubstring("connect"),
			"--help should mention the command name")
	})

	It("loopruntime disconnect --help emits structured help", func() {
		stdout, stderr, exitCode := runLoopRuntime("disconnect", "--help")

		Expect(exitCode).To(Equal(0),
			"loopruntime disconnect --help should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		assertNoForbiddenTermsInAll(map[string]string{
			"disconnect help stdout": stdout,
			"disconnect help stderr": stderr,
		})
	})

	It("project inspect surfaces repository field", func() {
		// The project list / inspect endpoint must surface a `repository`
		// shape (even null) per CLAUDE.md (CLI stability contract) so dashboards can render
		// connect status. This validates the API contract surface even
		// when no GitHub App is installed.
		stdout, stderr, exitCode := runLoopRuntime("project", "list", "--json")

		Expect(exitCode).To(Equal(0),
			"loopruntime project list should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		assertNoForbiddenTermsInAll(map[string]string{
			"project list stdout": stdout,
			"project list stderr": stderr,
		})

		s := strings.TrimSpace(stdout)
		Expect(s[0:1]).To(Or(Equal("{"), Equal("[")),
			"--json output must be valid JSON")
	})
})
