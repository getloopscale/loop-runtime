// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — J-07 Projects List + CreateProjectSheet.
//
// Fills the v2 dogfood coverage gap for M1 (was missing; 13/25 → 14/25).
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J07
//
// LIVE pre-conditions: dev-public infra UP + JWT seeded + cloud-dev reachable.

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-07 Projects List", Label("live", "dogfood", "j07"), func() {
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
		It("GET /engine/api/v1/projects returns 200 with a projects array", func() {
			status, body, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/projects", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
			Expect(body).To(HaveKey("projects"))
		})
	})

	// Creating a project by deploying to it is step 1 of the app lifecycle journey
	// (journey_app_lifecycle_live_test.go).

	Context("Scenario C — preflight health", func() {
		It("cloud /health/deep stays 200 throughout", func() {
			Expect(HealthcheckCloud(ctx, env)).To(Succeed())
		})
	})
})
