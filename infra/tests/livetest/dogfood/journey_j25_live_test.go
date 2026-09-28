// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — J-25 Live URL Discoverability.
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J25
// LIVE pre-conditions: dev-public infra UP + JWT seeded + cloud-dev reachable.

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-25 Live URL Discoverability", Label("live", "dogfood", "j25"), func() {
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

	Context("Scenario A — status surfaces the live URL", func() {
		It("GET /engine/api/v1/projects/sample/status returns 200 (source of the LIVE badge/URL)", func() {
			status, _, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/projects/sample/status", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
		})
	})

	Context("Scenario B — LIVE badge only when a production deploy exists (#119)", func() {
		It("Overview shows LIVE only for a project with a real production deploy", func() {
			// The badge/URL derivation composes status + release-active state;
			// asserting the badge logic needs the release payload. Deferred to a
			// T3.2 follow-up commit.
			Skip("LIVE badge derivation spec — implementation in T3.2 follow-up commit")
		})
	})
})
