// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — J-09 Project Connect (repo integration).
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J09
// LIVE pre-conditions: dev-public infra UP + JWT seeded + cloud-dev reachable.

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-09 Project Connect", Label("live", "dogfood", "j09"), func() {
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

	Context("Scenario A — connected project is reachable", func() {
		It("GET /engine/api/v1/projects/sample returns 200 (canonical fixture repo connected)", func() {
			status, _, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/projects/sample", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
		})
	})

	Context("Scenario B — connect flow (GitHub App installation)", func() {
		It("POST /engine/api/v1/projects/{name}/connect binds a repo", func() {
			// The connect flow needs a GitHub App installation + webhook; it
			// mutates state and is covered LIVE by the canonical fixture repo.
			// Deferred to a T3.2 follow-up commit.
			Skip("connect mutation spec — implementation in T3.2 follow-up commit")
		})
	})
})
