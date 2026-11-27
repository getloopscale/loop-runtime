// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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
		It("a submitted registration hands a verification mail to the provider", func() {
			admin := KratosAdminURL()
			if admin == "" {
				Skip(KratosAdminEnv + " is not set: the courier of Kratos cannot be read")
			}
			email := SinkAddress("signup", time.Now())
			password, err := NewPassword()
			Expect(err).ToNot(HaveOccurred())

			_, answer, err := SignUp(ctx, env, email, password)
			Expect(err).ToNot(HaveOccurred())
			Expect(answer.Status).To(Equal(200), "registration: %s", answer.Body)

			// Kratos keeps no readable copy of the code, so the journey ends where
			// the provider accepted the mail.
			Eventually(func() (bool, error) {
				return SentTo(ctx, admin, email, "verification_code_valid")
			}).WithTimeout(90*time.Second).WithPolling(3*time.Second).Should(BeTrue(),
				"no verification mail to %s reached the provider", email)
		})
	})

	Context("Scenario C — a new account gets a team on the starter plan", func() {
		It("the first token of a new account names a team whose plan is starter", func() {
			email := SinkAddress("team", time.Now())
			password, err := NewPassword()
			Expect(err).ToNot(HaveOccurred())

			browser, answer, err := SignUp(ctx, env, email, password)
			Expect(err).ToNot(HaveOccurred())
			Expect(answer.Status).To(Equal(200), "registration: %s", answer.Body)

			exchanged, err := browser.Do(ctx, "POST", env.CloudBaseURL+"/v1/auth/session/token", "application/json", map[string]any{}, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(exchanged.Status).To(Equal(200), "the session of the new account was not exchanged for a token")
			token, _ := exchanged.JSON()["access_token"].(string)
			Expect(token).ToNot(BeEmpty())

			status, billing, err := CurlJSON(ctx, env, "GET", env.CloudBaseURL+"/engine/api/v1/billing", token, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
			Expect(billing["plan"]).To(Equal("starter"))
			Expect(billing["team"]).To(HavePrefix("tn-"))
		})
	})
})
