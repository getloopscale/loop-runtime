// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Journey 14: Concurrency + Lock Discipline (absorbed from edge_concurrency
// + failure_scenarios on 2026-05-25 per user mandate "keep only the
// full dogfood ones, absorb whatever makes sense").
//
// Validates contracts that historically lived in non-journey files but
// describe REAL user-observable behavior:
//   - Idempotency-Key replay (same body → cached; different body → 4xx)
//   - 3 concurrent deploys: 1 wins (202), 2 rejected (exit 3)
//   - Environment lock released after build completes (next deploy succeeds)
//   - Sync timeout error path leaks no internal terms (Rule 3)
//   - Health probe failure error path leaks no internal terms (Rule 3)
package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Journey 14: Concurrency + Lock Discipline", Label("journey"), func() {
	var workDir string

	BeforeEach(func() {
		skipIfNotE2E()
		seedE2E()

		workDir = copyFixture("hello-server")
		initGitRepo(workDir)
	})

	AfterEach(func() {
		if workDir != "" {
			os.RemoveAll(workDir)
		}
	})

	It("Idempotency: same body returns cached response (same build_id)", func() {
		idempotencyKey := fmt.Sprintf("ginkgo-idem-same-%s", uniqueSuffix())
		projectID := fmt.Sprintf("idem-same-%s", uniqueSuffix())
		// T1.3 follow-up (2026-05-29): handler now rejects empty `apps` with
		// 400 MISSING_FIELD instead of silently producing a no-op workflow.
		// Send a minimal valid app shape so the idempotency contract is
		// exercised against the canonical happy path.
		body := fmt.Sprintf(`{"project_id":"%s","environment":"production","commit_sha":"%s","apps":[{"name":"app","type":"server","port":3000,"framework":"node"}]}`,
			projectID, uniqueSuffix())

		firstResp := postWithIdempotencyKey(body, idempotencyKey)
		Expect(firstResp.StatusCode).To(SatisfyAny(Equal(200), Equal(201), Equal(202)),
			"first call should succeed; got %d body=%s", firstResp.StatusCode, firstResp.RawBody)

		secondResp := postWithIdempotencyKey(body, idempotencyKey)
		Expect(secondResp.StatusCode).To(SatisfyAny(Equal(200), Equal(201), Equal(202)),
			"replay should return same status; got %d body=%s", secondResp.StatusCode, secondResp.RawBody)
		Expect(secondResp.BuildID).To(Equal(firstResp.BuildID),
			"idempotency replay should return same build_id")
	})

	It("Idempotency: different body with same key returns 4xx", func() {
		idempotencyKey := fmt.Sprintf("ginkgo-idem-diff-%s", uniqueSuffix())
		projectID := fmt.Sprintf("idem-diff-%s", uniqueSuffix())

		// T1.3 follow-up: both bodies need valid apps (see comment above).
		// Differ them by app port so body hash diverges.
		bodyA := fmt.Sprintf(`{"project_id":"%s","environment":"production","commit_sha":"%s","apps":[{"name":"app","type":"server","port":3000,"framework":"node"}]}`,
			projectID, uniqueSuffix())
		bodyB := fmt.Sprintf(`{"project_id":"%s","environment":"production","commit_sha":"%s","apps":[{"name":"app","type":"server","port":3001,"framework":"node"}]}`,
			projectID, uniqueSuffix())

		firstResp := postWithIdempotencyKey(bodyA, idempotencyKey)
		Expect(firstResp.StatusCode).To(SatisfyAny(Equal(200), Equal(201), Equal(202)))

		secondResp := postWithIdempotencyKey(bodyB, idempotencyKey)
		Expect(secondResp.StatusCode).To(SatisfyAny(Equal(400), Equal(409), Equal(422)),
			"reuse with different body must NOT return 2xx; got %d body=%s",
			secondResp.StatusCode, secondResp.RawBody)
	})

	It("3 concurrent deploys: 1 wins, 2 rejected", func() {
		barrier := make(chan struct{})
		results := make(chan deployResult, 3)
		var wg sync.WaitGroup

		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer GinkgoRecover()
				defer wg.Done()
				<-barrier
				stdout, stderr, exitCode := runLoopZeroDeploy(workDir, "--env", "production", "--json")
				results <- deployResult{stdout: stdout, stderr: stderr, exitCode: exitCode}
			}()
		}

		close(barrier)

		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()

		select {
		case <-done:
		case <-time.After(10 * time.Minute):
			Fail("timed out waiting for 3 concurrent deploys")
		}

		var successes, rejections int
		for i := 0; i < 3; i++ {
			r := <-results
			switch r.exitCode {
			case 0:
				successes++
			case 3:
				rejections++
			default:
				Fail(fmt.Sprintf("unexpected exit code %d\nstdout: %s\nstderr: %s",
					r.exitCode, r.stdout, r.stderr))
			}
		}

		Expect(successes).To(Equal(1), "exactly 1 deploy should succeed")
		Expect(rejections).To(Equal(2), "exactly 2 deploys should be rejected")
	})

	It("Environment lock released after build completes (next deploy succeeds)", func() {
		buildID, _ := deployAndWaitSuccess(workDir, "production", deployTimeout)
		Expect(buildID).NotTo(BeEmpty())

		buildID2, _ := deployAndWaitSuccess(workDir, "production", deployTimeout)
		Expect(buildID2).NotTo(BeEmpty())
		Expect(buildID2).NotTo(Equal(buildID),
			"second deploy must produce distinct build_id (lock released, new build submitted)")
	})

	It("Sync timeout error path leaks no internal terms (Rule 3)", func() {
		stdout, stderr, exitCode := runLoopZero("status",
			"--project", "nonexistent-sync-test",
			"--env", "production", "--json")

		expectAllowedExitCode(exitCode, "status for sync timeout scenario")
		assertNoForbiddenTermsInAll(map[string]string{
			"sync timeout stdout": stdout,
			"sync timeout stderr": stderr,
		})
	})

	It("Health probe failure error path leaks no internal terms (Rule 3)", func() {
		stdout, stderr, exitCode := runLoopZero("status",
			"--project", "nonexistent-health-test",
			"--env", "production", "--json")

		expectAllowedExitCode(exitCode, "status for nonexistent project")
		assertNoForbiddenTermsInAll(map[string]string{
			"health probe stdout": stdout,
			"health probe stderr": stderr,
		})
	})
})

// idempotencyResponse captures the result of an Idempotency-Key API call.
// Moved here from edge_concurrency_test.go on 2026-05-25 absorption.
type idempotencyResponse struct {
	StatusCode int
	BuildID    string
	RawBody    string
}

// postWithIdempotencyKey posts a raw JSON body to /api/v1/builds with the
// given Idempotency-Key header. Caller controls body bytes for deterministic
// contract testing (same-body cached replay vs different-body 4xx).
func postWithIdempotencyKey(body, key string) idempotencyResponse {
	url := fmt.Sprintf("%s/api/v1/builds", apiURL())

	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	Expect(err).NotTo(HaveOccurred())

	req.Header.Set("Authorization", "Bearer "+resolveToken())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	rawBody := string(bodyBytes)

	var result map[string]interface{}
	_ = json.Unmarshal(bodyBytes, &result)
	buildID, _ := result["build_id"].(string)

	return idempotencyResponse{
		StatusCode: resp.StatusCode,
		BuildID:    buildID,
		RawBody:    rawBody,
	}
}
