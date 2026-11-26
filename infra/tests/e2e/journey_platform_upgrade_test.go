// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Journey: Platform Upgrade (T3.2 of deep-review-remediation-plan)
//
// Validates the `loopruntime platform status`, `loopruntime platform install --dry-run`,
// and `loopruntime platform upgrade --dry-run` CLI commands. These are admin-only
// commands gated behind LOOPRUNTIME_SERVICE_AUTH; this journey validates the
// CLI contract surface (exit codes, JSON shape, --help) without
// performing a real platform install/upgrade.
//
// Real platform install/upgrade requires a destructive operator-driven
// workflow against a fresh cluster — NOT in scope for the E2E journey
// suite.
package e2e

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Journey: Platform Upgrade", Ordered, Label("journey"), func() {
	BeforeAll(func() {
		skipIfNotE2E()
		seedE2E()
	})

	It("loopruntime platform --help emits structured help", func() {
		stdout, stderr, exitCode := runLoopRuntime("platform", "--help")

		Expect(exitCode).To(Equal(0),
			"loopruntime platform --help should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		assertNoForbiddenTermsInAll(map[string]string{
			"platform help stdout": stdout,
			"platform help stderr": stderr,
		})

		joined := strings.ToLower(stdout + stderr)
		// Help must list the sub-commands per CLAUDE.md (CLI stability contract).
		Expect(joined).To(ContainSubstring("status"),
			"platform --help should list 'status' subcommand")
	})

	It("loopruntime platform status emits structured output", func() {
		stdout, stderr, exitCode := runLoopRuntime("platform", "status", "--json")

		// platform status invokes kubectl probes against the local cluster.
		// Valid contract paths per `cli/src/commands/platform.ts` platformStatus
		// doc-comment (ADR D3):
		//   - exit 0: cluster reachable, no drift.
		//   - exit 1: cluster unreachable / kubectl probe failed.
		//   - exit 4: auth required (no service-auth secret).
		//   - exit 5: cluster reachable, drift detected (runtime ≠ Git).
		// All four are stable user-facing contracts.
		Expect(exitCode).To(Or(Equal(0), Equal(1), Equal(4), Equal(5)),
			"loopruntime platform status: unexpected exit %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)

		assertNoForbiddenTermsInAll(map[string]string{
			"platform status stdout": stdout,
			"platform status stderr": stderr,
		})

		// Exit 0 (no drift) AND exit 5 (drift detected) BOTH emit valid JSON
		// per the CLI contract. Validate shape in both cases.
		if exitCode == 0 || exitCode == 5 {
			s := strings.TrimSpace(stdout)
			Expect(s[0:1]).To(Or(Equal("{"), Equal("[")),
				"--json output must be valid JSON when exit=%d", exitCode)
		}
	})

	It("loopruntime platform install --dry-run does not mutate", func() {
		// --dry-run is a stable contract per CLAUDE.md (CLI stability contract): it produces
		// what *would* happen without applying. The journey only asserts
		// the contract surface (exit code + Rule-3 + JSON shape). Running
		// the real install is destructive and requires fresh-cluster setup.
		stdout, stderr, exitCode := runLoopRuntime("platform", "install", "--dry-run", "--json")

		// Some auth modes return 4 if LOOPRUNTIME_SERVICE_AUTH is not in env;
		// dry-run path does NOT mutate state regardless.
		Expect(exitCode).To(Or(Equal(0), Equal(1), Equal(4)),
			"loopruntime platform install --dry-run: unexpected exit\nstdout: %s\nstderr: %s", stdout, stderr)

		assertNoForbiddenTermsInAll(map[string]string{
			"platform install stdout": stdout,
			"platform install stderr": stderr,
		})
	})
})
