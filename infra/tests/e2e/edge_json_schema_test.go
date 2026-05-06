// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Edge Cases: JSON Schema + CI Mode (T2.4)
//
// Validates --json schema stability (Rule 5) and CI mode behavior.
package e2e

import (
	"os"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Edge: JSON Schema Validation", Label("edge-case"), func() {
	BeforeEach(func() {
		skipIfNotE2E()
		seedE2E()
	})

	// Commands that must produce valid JSON with --json flag.
	jsonCommands := []struct {
		name     string
		args     []string
		required []string // Required top-level fields.
	}{
		{
			name:     "whoami",
			args:     []string{"whoami", "--json"},
			required: []string{"authenticated"},
		},
		{
			name:     "project list",
			args:     []string{"project", "list", "--json"},
			required: []string{}, // At minimum, must be valid JSON.
		},
		{
			name:     "env list",
			args:     []string{"env", "list", "--json"},
			required: []string{},
		},
		{
			name:     "secret list",
			args:     []string{"secret", "list", "--env", "production", "--json"},
			required: []string{},
		},
		{
			name:     "domain list",
			args:     []string{"domain", "list", "--json"},
			required: []string{},
		},
		{
			name:     "build list",
			args:     []string{"build", "list", "--json"},
			required: []string{},
		},
	}

	for _, tc := range jsonCommands {
		tc := tc // capture range variable
		It("--json produces valid JSON for "+tc.name, func() {
			stdout, stderr, exitCode := runLoopZero(tc.args...)

			expectAllowedExitCode(exitCode, tc.name+" --json")

			// Must produce parseable JSON.
			if strings.TrimSpace(stdout) != "" {
				result, err := parseJSON(stdout)
				Expect(err).NotTo(HaveOccurred(),
					"%s --json output is not valid JSON\nstdout: %s\nstderr: %s",
					tc.name, stdout, stderr)

				// Check required fields.
				for _, field := range tc.required {
					Expect(result).To(HaveKey(field),
						"%s --json output missing required field %q", tc.name, field)
				}
			}

			// Rule 3: no internal terms in any output.
			assertNoForbiddenTermsInAll(map[string]string{
				tc.name + " stdout": stdout,
				tc.name + " stderr": stderr,
			})
		})
	}
})

var _ = Describe("Edge: CI Mode", Label("edge-case"), func() {
	BeforeEach(func() {
		skipIfNotE2E()
	})

	It("CI=true produces no ANSI escape codes", func() {
		cmd := exec.Command("loopzero", "whoami", "--json")
		cmd.Env = append(os.Environ(),
			"LOOPZERO_API_URL="+apiURL(),
			"CI=true",
		)
		if token := os.Getenv("LOOPZERO_TOKEN"); token != "" {
			cmd.Env = append(cmd.Env, "LOOPZERO_TOKEN="+token)
		}

		var stdoutBuf, stderrBuf strings.Builder
		cmd.Stdout = &stdoutBuf
		cmd.Stderr = &stderrBuf
		_ = cmd.Run()

		combined := stdoutBuf.String() + stderrBuf.String()

		// ANSI escape codes start with \x1b[ or \033[.
		Expect(combined).NotTo(ContainSubstring("\x1b["),
			"CI mode should not contain ANSI escape codes")

		// No spinner characters (ora uses ⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏).
		spinnerChars := "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
		for _, r := range spinnerChars {
			Expect(combined).NotTo(ContainSubstring(string(r)),
				"CI mode should not contain spinner character %q", string(r))
		}
	})
})
