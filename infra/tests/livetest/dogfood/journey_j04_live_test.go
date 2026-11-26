// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — J-04 Recovery (password reset).
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J04
// LIVE pre-conditions: dev-public infra UP + JWT seeded + auth-dev reachable + Resend wired.

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-04 Recovery", Label("live", "dogfood", "j04"), func() {
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

	Context("Scenario A — recovery prerequisites reachable", func() {
		It("auth stack up + seeded session authenticates (recovery flow prereq)", func() {
			status, _, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/projects", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
		})
	})

	Context("Scenario B — recovery mail for a known address, silence for an unknown one", func() {
		It("recovery for a known address sends a mail; an unknown address gets the same answer and no mail", func() {
			admin := KratosAdminURL()
			if admin == "" {
				Skip(KratosAdminEnv + " is not set: the courier of Kratos cannot be read")
			}
			now := time.Now()
			known, unknown := SinkAddress("recovery", now), SinkAddress("nobody", now)
			password, err := NewPassword()
			Expect(err).ToNot(HaveOccurred())
			_, registered, err := SignUp(ctx, env, known, password)
			Expect(err).ToNot(HaveOccurred())
			Expect(registered.Status).To(Equal(200), "registration: %s", registered.Body)

			// What the person asking sees: the state of the flow and its messages.
			recovery := func(address string) (string, []any) {
				browser, err := NewBrowser(env, "")
				Expect(err).ToNot(HaveOccurred())
				started, err := browser.Do(ctx, "GET", env.AuthBaseURL+"/self-service/recovery/browser", "application/json", nil, false)
				Expect(err).ToNot(HaveOccurred())
				Expect(started.Status).To(Equal(200), "recovery flow: %s", started.Body)
				flow := started.JSON()
				answer, err := browser.Do(ctx, "POST", FlowAction(flow), "application/json", map[string]any{
					"method":     "code",
					"email":      address,
					"csrf_token": FlowField(flow, "csrf_token"),
				}, false)
				Expect(err).ToNot(HaveOccurred())
				Expect(answer.Status).To(Equal(200), "recovery for %s: %s", address, answer.Body)
				submitted := answer.JSON()
				state, _ := submitted["state"].(string)
				ui, _ := submitted["ui"].(map[string]any)
				var ids []any
				messages, _ := ui["messages"].([]any)
				for _, raw := range messages {
					message, _ := raw.(map[string]any)
					ids = append(ids, message["id"])
				}
				return state, ids
			}

			knownState, knownMessages := recovery(known)
			unknownState, unknownMessages := recovery(unknown)
			Expect(knownState).To(Equal("sent_email"))
			Expect(unknownState).To(Equal(knownState), "the answer tells a known address from an unknown one")
			Expect(unknownMessages).To(Equal(knownMessages), "the answer tells a known address from an unknown one")

			Eventually(func() (bool, error) {
				return SentTo(ctx, admin, known, "recovery_code_valid")
			}).WithTimeout(90*time.Second).WithPolling(3*time.Second).Should(BeTrue(),
				"no recovery mail to %s reached the provider", known)
			// The mail to the known address went out, so one to the unknown address
			// would have been queued by now.
			queued, err := CourierMessages(ctx, admin, unknown)
			Expect(err).ToNot(HaveOccurred())
			Expect(queued).To(BeEmpty(), "Kratos queued mail for an address that has no account")
		})
	})
})
