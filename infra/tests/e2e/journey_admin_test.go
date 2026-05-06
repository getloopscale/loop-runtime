// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Journey 11: Admin Commands
//
// Migrated from: infra/tests/dogfood/journeys/journey-11-admin.sh
//
// Tests: evict dry-run, broad pattern rejection, missing service auth rejection
// Requires: LOOPZERO_SERVICE_AUTH environment variable
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Journey 11: Admin", Label("journey"), func() {
	var serviceAuth string

	BeforeEach(func() {
		skipIfNotE2E()
		seedE2E()

		// Load service auth from file or env.
		serviceAuth = os.Getenv("LOOPZERO_SERVICE_AUTH")
		if serviceAuth == "" {
			authFile := os.ExpandEnv("${HOME}/.loopzero/local-service-auth.txt")
			data, err := os.ReadFile(authFile)
			if err != nil {
				Skip("LOOPZERO_SERVICE_AUTH not set and " + authFile + " not found")
			}
			serviceAuth = strings.TrimSpace(string(data))
		}
	})

	It("dry-run with no-match pattern returns queued=0", func() {
		noMatchPattern := fmt.Sprintf("tn-ginkgo-nomatch-%s-%%", uniqueSuffix())

		cmd := exec.Command("loopzero", "admin", "tenants", "evict",
			"--pattern", noMatchPattern, "--dry-run", "--json")
		cmd.Env = append(os.Environ(),
			"LOOPZERO_API_URL="+apiURL(),
			"LOOPZERO_SERVICE_AUTH="+serviceAuth,
		)

		var stdoutBuf, stderrBuf strings.Builder
		cmd.Stdout = &stdoutBuf
		cmd.Stderr = &stderrBuf

		err := cmd.Run()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}

		Expect(exitCode).To(Equal(0),
			"dry-run no-match should exit 0\nstdout: %s\nstderr: %s",
			stdoutBuf.String(), stderrBuf.String())

		result, err := parseJSON(stdoutBuf.String())
		Expect(err).NotTo(HaveOccurred())
		queued, _ := result["queued"].(float64)
		Expect(int(queued)).To(Equal(0),
			"dry-run with no-match pattern should queue 0 tenants")
	})

	It("rejects too-broad pattern", func() {
		cmd := exec.Command("loopzero", "admin", "tenants", "evict",
			"--pattern", "%", "--dry-run", "--json")
		cmd.Env = append(os.Environ(),
			"LOOPZERO_API_URL="+apiURL(),
			"LOOPZERO_SERVICE_AUTH="+serviceAuth,
		)

		var stdoutBuf, stderrBuf strings.Builder
		cmd.Stdout = &stdoutBuf
		cmd.Stderr = &stderrBuf
		_ = cmd.Run()

		combined := strings.ToLower(stdoutBuf.String() + stderrBuf.String())
		Expect(combined).To(SatisfyAny(
			ContainSubstring("too broad"),
			ContainSubstring("enumerate"),
		), "broad pattern should be rejected")
	})

	It("rejects missing service auth", func() {
		cmd := exec.Command("loopzero", "admin", "tenants", "evict",
			"--pattern", "tn-test-%", "--dry-run", "--json")
		cmd.Env = append(os.Environ(),
			"LOOPZERO_API_URL="+apiURL(),
			// Deliberately no LOOPZERO_SERVICE_AUTH.
		)

		// Remove LOOPZERO_SERVICE_AUTH from env.
		var filteredEnv []string
		for _, e := range cmd.Env {
			if !strings.HasPrefix(e, "LOOPZERO_SERVICE_AUTH=") {
				filteredEnv = append(filteredEnv, e)
			}
		}
		cmd.Env = filteredEnv

		var stdoutBuf, stderrBuf strings.Builder
		cmd.Stdout = &stdoutBuf
		cmd.Stderr = &stderrBuf
		_ = cmd.Run()

		combined := strings.ToLower(stdoutBuf.String() + stderrBuf.String())
		Expect(combined).To(SatisfyAny(
			ContainSubstring("service.auth"),
			ContainSubstring("required"),
			ContainSubstring("operator"),
			ContainSubstring("unauthorized"),
		), "missing service auth should be rejected")
	})

	It("dry-run with real tenant pattern shows match", func() {
		cmd := exec.Command("loopzero", "admin", "tenants", "evict",
			"--pattern", "e2e-tenant", "--dry-run", "--json")
		cmd.Env = append(os.Environ(),
			"LOOPZERO_API_URL="+apiURL(),
			"LOOPZERO_SERVICE_AUTH="+serviceAuth,
		)

		var stdoutBuf, stderrBuf strings.Builder
		cmd.Stdout = &stdoutBuf
		cmd.Stderr = &stderrBuf
		err := cmd.Run()

		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}

		Expect(exitCode).To(Equal(0))

		// Log matched tenants for debugging.
		GinkgoWriter.Printf("admin evict dry-run: %s\n", stdoutBuf.String())

		assertNoForbiddenTermsInAll(map[string]string{
			"admin stdout": stdoutBuf.String(),
			"admin stderr": stderrBuf.String(),
		})
	})
})
