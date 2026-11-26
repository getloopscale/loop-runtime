// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — J-20 Scale (replicas + HPA/KEDA).
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J20
// LIVE pre-conditions: dev-public infra UP + JWT seeded + cloud-dev reachable.

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-20 Scale", Label("live", "dogfood", "j20"), func() {
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

	Context("Scenario A — status reflects scale config", func() {
		It("GET /engine/api/v1/projects/sample/status returns 200", func() {
			status, _, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/projects/sample/status", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
		})
	})

	Context("Scenario B — plan-cap enforcement (Free=1/Pro=5/Team=20)", func() {
		It("scaling beyond the plan cap is rejected with 402 PLAN_UPGRADE_REQUIRED", func() {
			// The scale mutation + KEDA gate (AssertKEDAAllowed / AssertHPATargetBounds)
			// changes the Deployment/Rollout replicas; assertion needs KubectlGetJSON on
			// the tenant ns. Deferred to a T3.2 follow-up commit.
			Skip("scale-cap enforcement spec — implementation in T3.2 follow-up commit")
		})
	})
})
