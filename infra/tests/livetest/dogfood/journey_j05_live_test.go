// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — J-05 Device Success (OAuth device-flow, RFC 8628).
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J05
// LIVE pre-conditions: dev-public infra UP + JWT seeded + auth-dev reachable.

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-05 Device Success", Label("live", "dogfood", "j05"), func() {
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

	Context("Scenario A — device-flow prerequisites reachable", func() {
		It("auth stack up + seeded session authenticates (device flow prereq)", func() {
			status, _, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/projects", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
		})
	})

	Context("Scenario B — RFC 8628 device lifecycle (cross-device)", func() {
		It("device_code → user approves on second device → token issued", func() {
			// The cross-device RFC 8628 lifecycle needs two coordinated clients;
			// wiring the device-flow polling into Ginkgo is deferred to a T3.2
			// follow-up commit.
			Skip("device-flow lifecycle spec — implementation in T3.2 follow-up commit")
		})
	})
})
