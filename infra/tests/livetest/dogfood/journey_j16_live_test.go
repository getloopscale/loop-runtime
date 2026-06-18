// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T4.9 of plan canonical-tests-rewrite-v2 v1.0 — J-16 General settings.

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-16 Settings — general tenant settings", Label("live", "dogfood", "j16", "settings"), func() {
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

	Context("Scenario A — tenant settings retrievable", func() {
		It("GET /api/v1/tenant/settings returns 200 with current tier + region", func() {
			Skip("implementation in T4.9 follow-up — endpoint contract TBD")
		})
	})

	Context("Scenario B — notification preferences update", func() {
		It("PATCH /api/v1/tenant/settings/notifications persists preferences", func() {
			Skip("implementation in T4.9 follow-up — endpoint contract TBD")
		})
	})
	_ = jwt
})
