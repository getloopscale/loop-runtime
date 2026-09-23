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

		stdout, stderr, exitCode := runLoopZeroAdmin(serviceAuth, "admin", "tenants", "evict",
			"--pattern", noMatchPattern, "--dry-run", "--json")

		Expect(exitCode).To(Equal(0),
			"dry-run no-match should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		result, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		queued, _ := result["queued"].(float64)
		Expect(int(queued)).To(Equal(0),
			"dry-run with no-match pattern should queue 0 tenants")
	})

	It("rejects too-broad pattern", func() {
		stdout, stderr, _ := runLoopZeroAdmin(serviceAuth, "admin", "tenants", "evict",
			"--pattern", "%", "--dry-run", "--json")

		combined := strings.ToLower(stdout + stderr)
		Expect(combined).To(SatisfyAny(
			ContainSubstring("too broad"),
			ContainSubstring("enumerate"),
		), "broad pattern should be rejected")
	})

	It("rejects missing service auth", func() {
		// Deliberately no LOOPZERO_SERVICE_AUTH: removed from the inherited env too.
		stdout, stderr, _ := execLoopZero(loopzeroInvocation{unset: []string{"LOOPZERO_SERVICE_AUTH"}},
			"admin", "tenants", "evict", "--pattern", "tn-test-%", "--dry-run", "--json")

		combined := strings.ToLower(stdout + stderr)
		Expect(combined).To(SatisfyAny(
			ContainSubstring("service.auth"),
			ContainSubstring("required"),
			ContainSubstring("operator"),
			ContainSubstring("unauthorized"),
		), "missing service auth should be rejected")
	})

	It("dry-run with real tenant pattern shows match", func() {
		stdout, stderr, exitCode := runLoopZeroAdmin(serviceAuth, "admin", "tenants", "evict",
			"--pattern", "e2e-tenant", "--dry-run", "--json")

		Expect(exitCode).To(Equal(0), "stdout: %s\nstderr: %s", stdout, stderr)

		// Log matched tenants for debugging.
		GinkgoWriter.Printf("admin evict dry-run: %s\n", stdout)

		assertNoForbiddenTermsInAll(map[string]string{
			"admin stdout": stdout,
			"admin stderr": stderr,
		})
	})
})

// runLoopZeroAdmin runs an admin command authenticated by the operator's
// X-Service-Auth secret.
func runLoopZeroAdmin(serviceAuth string, args ...string) (stdout, stderr string, exitCode int) {
	return execLoopZero(loopzeroInvocation{env: []string{"LOOPZERO_SERVICE_AUTH=" + serviceAuth}}, args...)
}
