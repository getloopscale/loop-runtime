// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T4.7 of plan canonical-tests-rewrite-v2 v1.0 — J-18 Status + Health pages.

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-18 Status + Health — system status pages", Label("live", "dogfood", "j18", "ux"), func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		env    Env
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
		env = DefaultEnv()
	})

	AfterEach(func() { cancel() })

	Context("Scenario A — deep health probe", func() {
		It("returns 200 with components map", func() {
			Expect(HealthcheckCloud(ctx, env)).To(Succeed())
		})
	})

	Context("Scenario B — engine /api/v1/health returns 200", func() {
		It("engine basic health endpoint reachable", func() {
			// loopzero-cloud authenticates every /engine path, the health one included,
			// so the probe carries the seeded session; without it the answer is 401.
			jwt, err := LoadJWT(env)
			Expect(err).ToNot(HaveOccurred(), "JWT seed required")
			status, _, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/health", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
		})
	})
})
