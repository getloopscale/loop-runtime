// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — J-19 Runtime Logs Streaming.
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J19
// LIVE pre-conditions: dev-public infra UP + JWT seeded + cloud-dev reachable.

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-19 Runtime Logs Streaming", Label("live", "dogfood", "j19"), func() {
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

	Context("Scenario A — runtime logs stream endpoint reachable", func() {
		It("authenticated builds query succeeds before subscribing to the log stream", func() {
			// Streaming assertion (SSE Flusher proxy + named 'log' event channel)
			// needs a streaming client, same as j17 Scenario B; this scenario
			// asserts the authenticated prereq only.
			status, _, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/projects/sample/builds", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
		})
	})

	Context("Scenario B — SSE 'log' event arrives", func() {
		It("GET /engine/api/v1/projects/sample/logs/stream yields a 'log' event", func() {
			Skip("SSE streaming spec — implementation in T3.2 follow-up commit (mirrors j17 Scenario B)")
		})
	})
})
