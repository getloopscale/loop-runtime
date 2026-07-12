// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — J-04 Recovery (password reset).
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J04
// LIVE pre-conditions: dev-public infra UP + JWT seeded + auth-dev reachable + Resend wired.

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-04 Recovery", Label("live", "dogfood", "j04"), func() {
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

	Context("Scenario A — recovery prerequisites reachable", func() {
		It("auth stack up + seeded session authenticates (recovery flow prereq)", func() {
			status, _, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/projects", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
		})
	})

	Context("Scenario B — recovery email via Resend + anti-enumeration mute", func() {
		It("recovery for a known address enqueues a Resend email; unknown address is muted", func() {
			// The recovery flow + Resend delivery + anti-enumeration behavior is
			// covered by dev-public-recovery-rate-limit_test.sh +
			// dev-public-auth-email-delivery_test.sh. Wiring a Kratos recovery
			// client into Ginkgo is deferred to a T3.2 follow-up commit.
			Skip("recovery flow spec — implementation in T3.2 follow-up commit")
		})
	})
})
