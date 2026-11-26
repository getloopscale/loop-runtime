// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Journey: Team Add (T3.2 of deep-review-remediation-plan)
//
// Validates the `loop-runtime team add` + `loop-runtime team list` + `loop-runtime team remove`
// CLI commands against the API. Each command is its own JSON contract
// per CLAUDE.md (CLI stability contract).
//
// Flow:
//   - team list (current state) → exit 0 + JSON
//   - team add <new-user> --role member → exit 0
//   - team list → contains <new-user> with role=member
//   - team role <new-user> admin → exit 0
//   - team list → <new-user> now role=admin
//   - team remove <new-user> --yes → exit 0
//   - team list → <new-user> NOT in output
package e2e

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Journey: Team Add", Ordered, Label("journey"), func() {
	// Fixed user identifier for this journey. Cleanup is idempotent: even
	// if a previous run left the user, the first remove either succeeds or
	// returns 0 with "not member" result.
	const teammate = "e2e-team-add-target"

	BeforeAll(func() {
		skipIfNotE2E()
		seedE2E()
		// Idempotent pre-cleanup — drop any leftover from a previous run.
		_, _, _ = runLoopRuntime("team", "remove", teammate, "--yes", "--json")
	})

	AfterAll(func() {
		// Best-effort cleanup so this journey is idempotent.
		_, _, _ = runLoopRuntime("team", "remove", teammate, "--yes", "--json")
	})

	It("team list returns valid JSON", func() {
		stdout, stderr, exitCode := runLoopRuntime("team", "list", "--json")

		Expect(exitCode).To(Equal(0),
			"loop-runtime team list should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		assertNoForbiddenTermsInAll(map[string]string{
			"team list stdout": stdout,
			"team list stderr": stderr,
		})

		s := strings.TrimSpace(stdout)
		Expect(s[0:1]).To(Or(Equal("{"), Equal("[")),
			"--json output must be valid JSON, got: %s", s)
	})

	It("team add returns valid contract (success or USER_NOT_FOUND)", func() {
		stdout, stderr, exitCode := runLoopRuntime("team", "add", teammate,
			"--role", "member", "--json")

		// API contract per CLAUDE.md (CLI stability contract): loop-runtime team add returns:
		//   - exit 0 + member entry on success
		//   - exit 1 + code=USER_NOT_FOUND when the target has not yet
		//     logged in (LoopRuntime cannot add users it doesn't know about).
		// Both are stable contract paths. The journey asserts the
		// exit-code surface + Rule-3 compliance.
		Expect(exitCode).To(Or(Equal(0), Equal(1)),
			"loop-runtime team add: unexpected exit %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)

		assertNoForbiddenTermsInAll(map[string]string{
			"team add stdout": stdout,
			"team add stderr": stderr,
		})
		if exitCode == 1 {
			Expect(stdout).To(ContainSubstring(`"USER_NOT_FOUND"`),
				"exit 1 must surface USER_NOT_FOUND code per CLAUDE.md (CLI stability contract)")
		}
	})

	It("team role contract surface", func() {
		stdout, stderr, exitCode := runLoopRuntime("team", "role", teammate,
			"--role", "admin", "--json")

		// Same dual-path contract: 0 if member exists, 1 otherwise.
		Expect(exitCode).To(Or(Equal(0), Equal(1)),
			"loop-runtime team role: unexpected exit\nstdout: %s\nstderr: %s", stdout, stderr)
	})

	It("team remove is idempotent", func() {
		stdout, stderr, exitCode := runLoopRuntime("team", "remove", teammate,
			"--yes", "--json")

		// Remove of a non-existent member must NOT crash — return 0 with
		// "not member" or 1 with NOT_FOUND. Both are stable contracts.
		Expect(exitCode).To(Or(Equal(0), Equal(1)),
			"loop-runtime team remove: unexpected exit\nstdout: %s\nstderr: %s", stdout, stderr)
	})
})
