// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — J-22 Secrets CRUD.
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J22
// LIVE pre-conditions: dev-public infra UP + JWT seeded + cloud-dev reachable
// + OpenBao transit + ESO wired (per cleanroom-up-live-test-mandate § 2.5).

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-22 Secrets CRUD", Label("live", "dogfood", "j22"), func() {
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
		It("GET /engine/api/v1/secrets returns 200", func() {
			status, _, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/secrets", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
		})
	})

	// A secret created through the API and read by the app's process is step 2 of the
	// app lifecycle journey (journey_app_lifecycle_live_test.go).
})
