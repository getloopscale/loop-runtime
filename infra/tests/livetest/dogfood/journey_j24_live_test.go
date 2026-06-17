// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T4.10 of plan canonical-tests-rewrite-v2 v1.0 — J-24 Configuration UI.

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-24 Configuration — UI for engine config", Label("live", "dogfood", "j24", "settings"), func() {
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

	Context("Scenario A — runtime env vars listed", func() {
		It("GET /engine/api/v1/projects/teste/environments/production/runtime-envs returns array", func() {
			Skip("implementation in T4.10 follow-up — endpoint already covered by J-22 secrets")
		})
	})

	Context("Scenario B — scale config retrievable", func() {
		It("GET /engine/api/v1/projects/teste/scale returns replicas + HPA target", func() {
			Skip("implementation in T4.10 follow-up — endpoint already covered by J-20 scale")
		})
	})
	_ = jwt
})
