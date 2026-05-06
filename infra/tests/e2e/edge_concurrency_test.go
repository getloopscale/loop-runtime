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
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
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

	It("idempotency key replay returns cached response", func() {
		idempotencyKey := fmt.Sprintf("ginkgo-idem-%s", uniqueSuffix())

		// First deploy with idempotency key.
		firstResp := deployWithIdempotencyKey(workDir, "production", idempotencyKey)
		Expect(firstResp.StatusCode).To(SatisfyAny(Equal(200), Equal(201), Equal(202)))

		// Second deploy with SAME idempotency key.
		secondResp := deployWithIdempotencyKey(workDir, "production", idempotencyKey)
		Expect(secondResp.StatusCode).To(SatisfyAny(Equal(200), Equal(201), Equal(202)))

		// Both should return the same build_id (cached response).
		Expect(secondResp.BuildID).To(Equal(firstResp.BuildID),
			"idempotency replay should return same build_id")
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
		case <-time.After(5 * time.Minute):
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

// idempotencyResponse captures the result of an API deploy call.
type idempotencyResponse struct {
	StatusCode int
	BuildID    string
}

// deployWithIdempotencyKey calls the builds API with a specific Idempotency-Key header.
// The actual deploy endpoint is POST /api/v1/builds (not /api/v1/deploy).
func deployWithIdempotencyKey(dir, env, key string) idempotencyResponse {
	url := fmt.Sprintf("%s/api/v1/builds", apiURL())

	body := fmt.Sprintf(`{"project":"hello-server","environment":"%s"}`, env)
	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	Expect(err).NotTo(HaveOccurred())

	req.Header.Set("Authorization", "Bearer "+seed.AuthToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	buildID, _ := result["build_id"].(string)

	return idempotencyResponse{
		StatusCode: resp.StatusCode,
		BuildID:    buildID,
	}
}
