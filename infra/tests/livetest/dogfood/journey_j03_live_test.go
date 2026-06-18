// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T4.2 of plan canonical-tests-rewrite-v2 v1.0 — J-03 Verification (email code via Resend).

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-03 Verification — email code via Resend", Label("live", "dogfood", "j03", "onboarding"), func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		env    Env
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
		env = DefaultEnv()
		Expect(HealthcheckCloud(ctx, env)).To(Succeed())
	})

	AfterEach(func() { cancel() })

	Context("Scenario A — verification flow accepts email + initiates code", func() {
		It("POST /self-service/verification/api with email queues Kratos courier message", func() {
			Skip("implementation in T4.2 follow-up — requires Kratos flow handler + PG courier_messages probe")
		})
	})

	Context("Scenario B — submitted code verifies identity", func() {
		It("POST verification flow with valid code transitions to verified state", func() {
			Skip("implementation in T4.2 follow-up — requires verification code retrieval from PG courier_messages + flow submission")
		})
	})

	Context("Scenario C — courier successfully delivers to Resend", func() {
		It("courier_messages status transitions 1 (queued) -> 2 (sent), not 4 (abandoned)", func() {
			Skip("implementation in T4.2 follow-up — requires PG probe helper")
		})
	})
})
