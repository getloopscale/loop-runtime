// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero domain add <domain>` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/domain.ts (DomainAddCommand, AUDIT-25).
// Validated manually in F1.6 (e2e-validation-final-2026-04-23.md).
//
// Behavior:
//   - Exit 0: success (domain registered, prints CF nameservers OR DNS records)
//   - Exit 1: MISSING_ARGUMENT (no domain, no --project, no --app, no --env)
//   - Exit 3: DOMAIN_ALREADY_EXISTS (409), API error, API_UNREACHABLE
//   - Exit 4: AUTH_REQUIRED
//   - --json success: full DomainCreateResponse {id, domain, project, app,
//     environment, status, verification?, cname_target?, nameservers?,
//     dns_records_imported?, action_required?}
//   - --json error: {error, exit_code, code}
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CLI: loopzero domain add", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 2 + 5: required flags missing ---

	It("Contract 2+5a: missing --project yields exit 1 MISSING_ARGUMENT", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("domain", "add", "example.com",
			"--app", "api", "--env", "production", "--json")

		expectAllowedExitCode(exitCode, "domain add (no --project)")
		Expect(exitCode).To(Equal(1),
			"missing --project should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("MISSING_ARGUMENT"))
	})

	It("Contract 2+5b: missing --app yields exit 1 MISSING_ARGUMENT", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, _, exitCode := runLoopZero("domain", "add", "example.com",
			"--project", "myproj", "--env", "production", "--json")

		expectAllowedExitCode(exitCode, "domain add (no --app)")
		Expect(exitCode).To(Equal(1))
		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("MISSING_ARGUMENT"))
	})

	It("Contract 2+5c: missing --env yields exit 1 MISSING_ARGUMENT", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, _, exitCode := runLoopZero("domain", "add", "example.com",
			"--project", "myproj", "--app", "api", "--json")

		expectAllowedExitCode(exitCode, "domain add (no --env)")
		Expect(exitCode).To(Equal(1))
		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("MISSING_ARGUMENT"))
	})

	// --- Contract 2 + 5: empty domain ---

	It("Contract 2+5d: empty domain yields exit 1 MISSING_ARGUMENT", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		_, stderr, exitCode := runLoopZero("domain", "add", "",
			"--project", "p", "--app", "a", "--env", "e", "--json")

		expectAllowedExitCode(exitCode, "domain add (empty domain)")
		Expect(exitCode).To(Equal(1),
			"empty domain should exit 1, got %d (stderr=%s)", exitCode, stderr)
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5e: no token yields exit 4 AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("domain", "add", "example.com",
			"--project", "p", "--app", "a", "--env", "e", "--json")

		expectAllowedExitCode(exitCode, "domain add (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		for _, args := range [][]string{
			{"domain", "add", "example.com"},
			{"domain", "add", "example.com", "--json"},
			{"domain", "add", "example.com", "--project", "p"},
			{"domain", "add", "example.com", "--project", "p", "--app", "a"},
			{"domain", "add", "example.com", "--project", "p", "--app", "a", "--env", "e", "--json"},
		} {
			stdout, stderr, _ := runLoopZero(args...)
			expectNoInternalTerms(stdout, strings.Join(args, " ")+" stdout")
			expectNoInternalTerms(stderr, strings.Join(args, " ")+" stderr")
		}

		// no-token path
		stdout, stderr, _ := runLoopZeroNoToken("domain", "add", "example.com",
			"--project", "p", "--app", "a", "--env", "e")
		expectNoInternalTerms(stdout, "domain add (no token) stdout")
		expectNoInternalTerms(stderr, "domain add (no token) stderr")
	})

	// --- Happy path SKIP ---

	It("Contract 1+3: SKIP — happy path requires CF Zone Management credentials", func() {
		Skip("happy path requires CF API credentials configured + valid project/app/env. " +
			"E2E in droplet (F1.6) skipped CF integration. CF zone creation costs money " +
			"and produces real DNS records — outside Phase 5 scope.")
	})
})
