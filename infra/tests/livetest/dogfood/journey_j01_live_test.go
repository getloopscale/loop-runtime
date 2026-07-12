// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — J-01 Login polish (OIDC GitHub + Google).
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J01
// LIVE pre-conditions: dev-public infra UP + JWT seeded + auth-dev reachable.

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-01 Login polish (OIDC)", Label("live", "dogfood", "j01"), func() {
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
		Expect(err).ToNot(HaveOccurred(), "JWT seed required (proves a prior login succeeded)")
		Expect(HealthcheckCloud(ctx, env)).To(Succeed(), "cloud-dev /health/deep prereq")
	})

	AfterEach(func() { cancel() })

	Context("Scenario A — authenticated session works end-to-end", func() {
		It("a seeded JWT authenticates against an engine route (login round-trip proof)", func() {
			status, _, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/projects", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200), "authenticated request must succeed with a valid session")
		})
	})

	Context("Scenario B — OIDC provider redirect (GitHub/Google)", func() {
		It("POST self-service/methods/oidc returns 303 to the provider", func() {
			// The Kratos OIDC browser-flow redirect is covered by
			// dev-public-oidc-redirect_test.sh; wiring a Kratos flow client into
			// the Ginkgo harness is deferred to a T3.2 follow-up commit.
			Skip("OIDC redirect spec — implementation in T3.2 follow-up commit")
		})
	})
})
