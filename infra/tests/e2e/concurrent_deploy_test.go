// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package e2e tests concurrent deploy rejection behavior end-to-end.
//
// These tests prove that the environment lock correctly serializes deploys:
//   - Two simultaneous deploys to the SAME environment: exactly one succeeds (exit 0),
//     the other fails with exit code 3 and DA-18 message.
//   - Two simultaneous deploys to DIFFERENT environments: both succeed (exit 0).
//
// Requires: LOOPZERO_E2E=1, running infrastructure (API, k3d clusters), and seeded E2E data.
package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

// forbiddenTerms references the canonical list from constants_test.go
// with additional concurrency-specific terms.
var forbiddenTerms = append(forbiddenInternalTerms, "lock", "advisory")

// deployResult captures the output of a single loopzero deploy invocation.
type deployResult struct {
	stdout   string
	stderr   string
	exitCode int
}

// runDeployAsync launches a deploy in a goroutine, waiting on the barrier
// channel before starting. This ensures both deploys start simultaneously.
func runDeployAsync(dir string, env string, barrier <-chan struct{}, wg *sync.WaitGroup, resultCh chan<- deployResult) {
	defer GinkgoRecover()
	defer wg.Done()
	<-barrier
	stdout, stderr, exitCode := runLoopZeroDeploy(dir, "--env", env, "--json")
	resultCh <- deployResult{
		stdout:   stdout,
		stderr:   stderr,
		exitCode: exitCode,
	}
}

// assertNoForbiddenTerms checks that none of the forbidden internal terms
// appear in the given output string.
func assertNoForbiddenTerms(output string, label string) {
	lower := strings.ToLower(output)
	for _, term := range forbiddenTerms {
		Expect(lower).NotTo(
			ContainSubstring(strings.ToLower(term)),
			fmt.Sprintf("%s should not contain forbidden term %q", label, term),
		)
	}
}

// resultForExitCode returns the deployResult that matches the given exit code.
// Used for error reporting when an unexpected exit code is encountered.
func resultForExitCode(r1, r2 deployResult, code int) deployResult {
	if r1.exitCode == code {
		return r1
	}
	return r2
}

var _ = Describe("Concurrent Deploy Rejection", Ordered, func() {
	var workDir string

	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}

		if !clusterRunning() {
			Skip("kubectl cluster-info failed - clusters not running")
		}

		if !apiHealthy() {
			Skip("API deep health check failed - API not ready")
		}

		// Seed E2E data (idempotent).
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred(), "E2E seed failed")

		// Each test gets its own copy of the fixture to avoid contamination.
		workDir = copyFixture("hello-server")
		_ = initGitRepo(workDir)
	})

	AfterEach(func() {
		if workDir != "" {
			os.RemoveAll(workDir)
			workDir = ""
		}

		if os.Getenv("LOOPZERO_E2E") != "" {
			verifyNoActiveBuilds(seed.TenantID)
		}
	})

	It("should reject concurrent deploys for the same environment", func() {
		// Arrange: prepare synchronization primitives.
		// A closed channel acts as a barrier so both goroutines start
		// their deploy at approximately the same time.
		barrier := make(chan struct{})
		resultCh := make(chan deployResult, 2)
		var wg sync.WaitGroup
		wg.Add(2)

		// Act: launch two concurrent deploys to the SAME environment.
		go runDeployAsync(workDir, "production", barrier, &wg, resultCh)
		go runDeployAsync(workDir, "production", barrier, &wg, resultCh)

		// Release both goroutines simultaneously.
		close(barrier)

		// Wait for both to complete (with timeout to avoid hanging).
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()
		select {
		case <-done:
			// Both completed.
		case <-time.After(5 * time.Minute):
			Fail("Timed out waiting for concurrent deploys to complete")
		}

		// Collect results.
		r1 := <-resultCh
		r2 := <-resultCh

		// Assert: exactly one exits with 0 (success), the other with 3 (deploy failure).
		exitCodes := []int{r1.exitCode, r2.exitCode}

		hasSuccess := false
		hasDeployFailure := false
		for _, code := range exitCodes {
			switch code {
			case 0:
				Expect(hasSuccess).To(BeFalse(),
					"Both deploys succeeded - lock did not serialize. Exit codes: %v", exitCodes)
				hasSuccess = true
			case 3:
				Expect(hasDeployFailure).To(BeFalse(),
					"Both deploys failed - neither succeeded. Exit codes: %v", exitCodes)
				hasDeployFailure = true
			default:
				r := resultForExitCode(r1, r2, code)
				Fail(fmt.Sprintf(
					"Unexpected exit code %d. Expected 0 or 3.\nStdout: %s\nStderr: %s",
					code, r.stdout, r.stderr,
				))
			}
		}

		Expect(hasSuccess).To(BeTrue(), "Expected exactly one deploy to succeed (exit 0)")
		Expect(hasDeployFailure).To(BeTrue(), "Expected exactly one deploy to fail (exit 3)")

		// Assert: the failing deploy's output contains the DA-18 message.
		failedResult := r1
		if r2.exitCode == 3 {
			failedResult = r2
		}
		combinedOutput := failedResult.stdout + failedResult.stderr
		Expect(combinedOutput).To(
			ContainSubstring("A deploy is already in progress for this environment"),
			"Rejected deploy must show DA-18 message. Got stdout: %s\nstderr: %s",
			failedResult.stdout, failedResult.stderr,
		)

		// Assert: no forbidden internal terms in ANY output from either deploy.
		assertNoForbiddenTerms(r1.stdout, "deploy-1 stdout")
		assertNoForbiddenTerms(r1.stderr, "deploy-1 stderr")
		assertNoForbiddenTerms(r2.stdout, "deploy-2 stdout")
		assertNoForbiddenTerms(r2.stderr, "deploy-2 stderr")

		// Assert: the successful deploy's --json output is valid JSON.
		successResult := r1
		if r2.exitCode == 0 {
			successResult = r2
		}
		if strings.TrimSpace(successResult.stdout) != "" {
			var jsonOutput map[string]interface{}
			err := json.Unmarshal([]byte(successResult.stdout), &jsonOutput)
			Expect(err).NotTo(HaveOccurred(),
				"Successful deploy --json output must be valid JSON: %s", successResult.stdout)
		}
	})

	It("should allow concurrent deploys to different environments", func() {
		// Arrange: prepare synchronization primitives.
		barrier := make(chan struct{})
		resultCh := make(chan deployResult, 2)
		var wg sync.WaitGroup
		wg.Add(2)

		// Act: launch two concurrent deploys to DIFFERENT environments.
		go runDeployAsync(workDir, "production", barrier, &wg, resultCh)
		go runDeployAsync(workDir, "staging", barrier, &wg, resultCh)

		// Release both goroutines simultaneously.
		close(barrier)

		// Wait for both to complete (with timeout to avoid hanging).
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()
		select {
		case <-done:
			// Both completed.
		case <-time.After(5 * time.Minute):
			Fail("Timed out waiting for concurrent deploys to complete")
		}

		// Collect results.
		r1 := <-resultCh
		r2 := <-resultCh

		// Assert: both deploys succeed (exit 0).
		Expect(r1.exitCode).To(Equal(0),
			"First deploy should succeed.\nStdout: %s\nStderr: %s", r1.stdout, r1.stderr)
		Expect(r2.exitCode).To(Equal(0),
			"Second deploy should succeed.\nStdout: %s\nStderr: %s", r2.stdout, r2.stderr)

		// Assert: no forbidden internal terms in any output.
		assertNoForbiddenTerms(r1.stdout, "deploy-production stdout")
		assertNoForbiddenTerms(r1.stderr, "deploy-production stderr")
		assertNoForbiddenTerms(r2.stdout, "deploy-staging stdout")
		assertNoForbiddenTerms(r2.stderr, "deploy-staging stderr")

		// Assert: both outputs are valid JSON (--json flag was set).
		for i, r := range []deployResult{r1, r2} {
			if strings.TrimSpace(r.stdout) != "" {
				var jsonOutput map[string]interface{}
				err := json.Unmarshal([]byte(r.stdout), &jsonOutput)
				Expect(err).NotTo(HaveOccurred(),
					"Deploy %d --json output must be valid JSON: %s", i+1, r.stdout)
			}
		}
	})
})
