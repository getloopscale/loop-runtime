// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T4.1 of plan canonical-tests-rewrite-v2 v1.0 — J-02 Register (sign-up new tenant).
// Per ADR D6 order: first in backlog (onboarding completeness).

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-02 Register — sign-up new tenant", Label("live", "dogfood", "j02", "onboarding"), func() {
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

	Context("Scenario A — sign-up flow initiates Kratos registration", func() {
		It("POST /self-service/registration/api returns 200 with flow id", func() {
			status, body, err := CurlJSON(ctx, env, "GET",
				env.AuthBaseURL+"/self-service/registration/api", "", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
			Expect(body).To(HaveKey("id"))
		})
	})

	Context("Scenario B — submission triggers email verification", func() {
		It("POST registration flow with email triggers Resend queueing", func() {
			Skip("implementation in T4.1 follow-up — requires Resend courier mock + flow submission helper")
		})
	})

	Context("Scenario C — new tenant created in PG with starter plan", func() {
		It("tenants row + tenant_members row appears post-verification", func() {
			Skip("implementation in T4.1 follow-up — requires PG probe helper + flow completion")
		})
	})
})
