// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — J-10 Environments (per-project model).
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J10
// LIVE pre-conditions: dev-public infra UP + JWT seeded + cloud-dev reachable.

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-10 Environments", Label("live", "dogfood", "j10"), func() {
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

	Context("Scenario A — REST list (per-project M2 model)", func() {
		It("GET /engine/api/v1/environments returns 200", func() {
			status, _, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/environments", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
		})
	})

	Context("Scenario B — no duplicate environments (J-10 regression #142)", func() {
		It("environments are not duplicated per project", func() {
			// The M2 refactor closed a backfill bug where production/preview
			// appeared 2x. Asserting de-dup requires parsing the array shape
			// per project — deferred to a T3.2 follow-up commit.
			Skip("dedup assertion — implementation in T3.2 follow-up commit")
		})
	})
})
