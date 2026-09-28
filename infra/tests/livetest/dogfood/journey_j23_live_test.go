// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — J-23 Previews per-PR.
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J23
// LIVE pre-conditions: dev-public infra UP + JWT seeded + cloud-dev reachable.

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-23 Previews per-PR", Label("live", "dogfood", "j23"), func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		env    Env
		jwt    string
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
		env = DefaultEnv()
		var err error
		jwt, err = LoadJWT(env)
		Expect(err).ToNot(HaveOccurred(), "JWT seed required")
		Expect(HealthcheckCloud(ctx, env)).To(Succeed(), "cloud-dev /health/deep prereq")
	})

	AfterEach(func() { cancel() })

	Context("Scenario A — REST list", func() {
		It("GET /engine/api/v1/projects/sample/previews returns 200", func() {
			status, _, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/projects/sample/previews", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
		})
	})

	Context("Scenario B — per-PR webhook → build → wildcard TLS chain", func() {
		It("a PR webhook creates a preview environment with wildcard TLS", func() {
			// The canonical preview chain (webhook → build → CR → operator → ns
			// → SA → ArgoCD → wildcard TLS) mutates cluster state. Deferred to a
			// T3.2 follow-up commit.
			Skip("preview creation chain spec — implementation in T3.2 follow-up commit")
		})
	})
})
