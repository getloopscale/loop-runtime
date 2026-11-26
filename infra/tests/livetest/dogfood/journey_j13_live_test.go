// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

// T4.3 of plan canonical-tests-rewrite-v2 v1.0 — J-13 Billing (Stripe upgrade flows).
// Per ADR D6 order: third in backlog (revenue path).

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-13 Billing — Stripe upgrade + plan tier flows", Label("live", "dogfood", "j13", "billing"), func() {
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

	Context("Scenario A — checkout session creation", func() {
		It("POST /v1/billing/checkout returns Stripe cs_test_* URL", func() {
			status, body, err := CurlJSON(ctx, env, "POST",
				env.CloudBaseURL+"/v1/billing/checkout", jwt, []byte(`{"plan":"pro"}`))
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
			Expect(body).To(HaveKey("url"))
			Expect(body["url"]).To(ContainSubstring("cs_test_"))
		})
	})

	Context("Scenario B — webhook updates tenant plan tier", func() {
		It("stripe trigger checkout.session.completed updates PG subscriptions", func() {
			Skip("implementation in T4.3 follow-up — requires stripe-cli orchestration + PG probe")
		})
	})

	Context("Scenario C — billing portal returns Stripe management URL", func() {
		It("POST /v1/billing/portal returns billing.stripe.com URL", func() {
			status, body, err := CurlJSON(ctx, env, "POST",
				env.CloudBaseURL+"/v1/billing/portal", jwt, []byte(`{}`))
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
			Expect(body).To(HaveKey("url"))
			Expect(body["url"]).To(ContainSubstring("billing.stripe.com"))
		})
	})
})
