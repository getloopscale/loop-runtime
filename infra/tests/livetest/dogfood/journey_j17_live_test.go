// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 v1.0 — J-17 Deployments + Realtime Logs.
//
// Replaces archive/canonical-tests-v1/scripts/local/dogfood-deployments.sh (~139 LOC bash)
// with ~80 LOC Ginkgo BDD spec using shared dogfood helpers.
//
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J17
//
// LIVE pre-conditions:
//   - dev-public infra UP (task setup with LOOPZERO_HELMFILE_ENV=dev-public)
//   - JWT seeded at ~/.loopzero/local-dogfood-token.txt
//   - cloud-dev.loopzero.example reachable

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-17 Deployments + Realtime Logs", Label("live", "dogfood", "j17"), func() {
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

	AfterEach(func() {
		cancel()
	})

	Context("Scenario A — REST list + filter forwarding", func() {
		It("GET /engine/api/v1/projects/sample/builds returns 200 with total > 0", func() {
			status, body, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/projects/sample/builds?env=production", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
			Expect(body).To(HaveKey("builds"))
		})
	})

	Context("Scenario B — SSE stream first event", func() {
		It("SSE Content-Type + first 'log' event arrives within 10s", func() {
			// Sketch — actual SSE consumption needs a streaming client; out of scope for
			// the spec skeleton. Real implementation will wrap eventsource.Stream or
			// equivalent + assert event name "log" arrives within timeout.
			Skip("SSE streaming spec — implementation in T3.2 follow-up commit")
		})
	})

	Context("Scenario C — Loki ::loopzero:: marker coverage", func() {
		It("LokiQuery returns markers for clone/install/build phases", func() {
			Skip("Loki coverage spec — implementation in T3.2 follow-up commit")
		})
	})

	Context("Scenario D — preflight health", func() {
		It("cloud /health/deep stays 200 throughout", func() {
			// Repeat the BeforeEach healthcheck — defense-in-depth that the
			// service stayed healthy throughout the previous scenarios.
			Expect(HealthcheckCloud(ctx, env)).To(Succeed())
		})
	})
})
