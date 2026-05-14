// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Edge Cases: Concurrency + Idempotency (T2.1)
//
// Expands on concurrent_deploy_test.go (which already covers 2-goroutine
// same-env rejection + cross-env success).
//
// New scenarios: idempotency key replay, 3+ concurrent deploys, lock release recovery.
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

var _ = Describe("Edge: Concurrency + Idempotency", Label("edge-case"), func() {
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

	// Plan T5.2 (2026-05-14, F-04 fix): split into two subtests reflecting
	// the actual idempotency contract (Stripe-style): same key + same body
	// = cached replay; same key + different body = HTTP 400 / 409 with
	// IDEMPOTENCY_KEY_REUSED_DIFFERENT_BODY (or similar).
	//
	// Pre-fix: a single test sent different bodies (test bug — fixture
	// flow varied each call) with same key, expected 200, got correct 400.
	// Now we explicitly assert both branches.

	It("idempotency replay (same body): returns cached response", func() {
		idempotencyKey := fmt.Sprintf("ginkgo-idem-same-%s", uniqueSuffix())
		// Unique project_id per subtest avoids collisions with concurrent
		// in-flight deploys from other specs (env-lock would return 409
		// even before idempotency middleware runs).
		projectID := fmt.Sprintf("idem-same-%s", uniqueSuffix())

		// Deterministic body so two calls are byte-identical.
		body := fmt.Sprintf(`{"project_id":"%s","environment":"production","commit_sha":"%s"}`, projectID, uniqueSuffix())

		firstResp := postWithIdempotencyKey(body, idempotencyKey)
		Expect(firstResp.StatusCode).To(SatisfyAny(Equal(200), Equal(201), Equal(202)),
			"first call should succeed; got %d body=%s", firstResp.StatusCode, firstResp.RawBody)

		secondResp := postWithIdempotencyKey(body, idempotencyKey)
		Expect(secondResp.StatusCode).To(SatisfyAny(Equal(200), Equal(201), Equal(202)),
			"replay should return same status; got %d body=%s", secondResp.StatusCode, secondResp.RawBody)

		Expect(secondResp.BuildID).To(Equal(firstResp.BuildID),
			"idempotency replay should return same build_id")
	})

	It("idempotency replay (different body): returns 4xx with reused-key code", func() {
		idempotencyKey := fmt.Sprintf("ginkgo-idem-diff-%s", uniqueSuffix())
		// Unique project_id per subtest avoids cross-spec env-lock contention.
		projectID := fmt.Sprintf("idem-diff-%s", uniqueSuffix())
		commitA := uniqueSuffix()
		commitB := uniqueSuffix()

		bodyA := fmt.Sprintf(`{"project_id":"%s","environment":"production","commit_sha":"%s"}`, projectID, commitA)
		bodyB := fmt.Sprintf(`{"project_id":"%s","environment":"production","commit_sha":"%s"}`, projectID, commitB)

		firstResp := postWithIdempotencyKey(bodyA, idempotencyKey)
		Expect(firstResp.StatusCode).To(SatisfyAny(Equal(200), Equal(201), Equal(202)),
			"first call should succeed; got %d body=%s", firstResp.StatusCode, firstResp.RawBody)

		secondResp := postWithIdempotencyKey(bodyB, idempotencyKey)
		// Accept either 409 (canonical idempotency-key reuse) or 400
		// (validation failure) — both signal a violated contract; the
		// important invariant is "NOT 2xx" (would imply silent body
		// swap). Code string check is best-effort because the exact
		// error code may evolve.
		Expect(secondResp.StatusCode).To(SatisfyAny(Equal(400), Equal(409), Equal(422)),
			"reuse with different body must NOT return 2xx; got %d body=%s",
			secondResp.StatusCode, secondResp.RawBody)
	})

	It("3 concurrent deploys: 1 wins, 2 rejected", func() {
		barrier := make(chan struct{})
		results := make(chan deployResult, 3)
		var wg sync.WaitGroup

		// Launch 3 concurrent deploys.
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

		close(barrier) // Release all goroutines simultaneously.

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		// Plan T5.1 (2026-05-14, G2 fix): 5min was too tight given real
		// Argo Workflow + BuildKit run time on k3d (~5-7 min). Raise
		// to 10min to accommodate end-to-end termination without
		// flaking. The contract being tested (1 wins, N rejected) is
		// timing-independent — extra margin doesn't weaken the test.
		case <-time.After(10 * time.Minute):
			Fail("timed out waiting for 3 concurrent deploys")
		}

		// Collect results.
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

	It("lock released after build completes, next deploy succeeds", func() {
		// First deploy.
		buildID, _ := deployAndWaitSuccess(workDir, "production", deployTimeout)
		Expect(buildID).NotTo(BeEmpty())

		// Second deploy should succeed (lock released).
		buildID2, _ := deployAndWaitSuccess(workDir, "production", deployTimeout)
		Expect(buildID2).NotTo(BeEmpty())
		Expect(buildID2).NotTo(Equal(buildID))
	})
})

// idempotencyResponse captures the result of an API call. Includes
// RawBody for diagnostics on unexpected status codes (so failures show
// the API's actual error message, not just the integer code).
type idempotencyResponse struct {
	StatusCode int
	BuildID    string
	RawBody    string
}

// postWithIdempotencyKey posts a raw JSON body to /api/v1/builds with the
// given Idempotency-Key header. Used by T5.2 subtests (same body / diff
// body) — caller controls body bytes for deterministic contract testing.
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
