// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — J-21 Readiness Score.
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J21
// LIVE pre-conditions: dev-public infra UP + JWT seeded + cloud-dev reachable.

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-21 Readiness Score", Label("live", "dogfood", "j21"), func() {
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

	Context("Scenario A — status feeds the readiness signals", func() {
		It("GET /engine/api/v1/projects/sample/status returns 200 (source of the 8 signals)", func() {
			status, _, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/projects/sample/status", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
		})
	})

	Context("Scenario B — tristate score (Unknown excluded from numerator AND denominator)", func() {
		It("readiness score composes 8 signals with Unknown excluded", func() {
			// The weighted 0-100 score is dashboard-computed from /status +
			// /metrics + /signals; asserting the tristate math needs the signal
			// payloads. Deferred to a T3.2 follow-up commit.
			Skip("readiness score composition spec — implementation in T3.2 follow-up commit")
		})
	})
})
