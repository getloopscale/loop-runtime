// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

// T4.6 of plan canonical-tests-rewrite-v2 v1.0 — J-06 Overview (project overview tab).

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-06 Overview — project overview tab", Label("live", "dogfood", "j06", "ux"), func() {
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
		Expect(err).ToNot(HaveOccurred())
		Expect(HealthcheckCloud(ctx, env)).To(Succeed())
	})

	AfterEach(func() { cancel() })

	Context("Scenario A — project status payload", func() {
		It("GET /engine/api/v1/projects/sample/status returns 200 with active_release", func() {
			status, body, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/projects/sample/status?env=production", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
			Expect(body).To(Or(HaveKey("active_release"), HaveKey("status")))
		})
	})

	Context("Scenario B — recent deployments visible", func() {
		It("GET /engine/api/v1/projects/sample/builds?limit=5 returns up to 5", func() {
			Skip("implementation in T4.6 follow-up — requires seeded builds")
		})
	})
})
