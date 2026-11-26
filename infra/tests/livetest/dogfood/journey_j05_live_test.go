// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — J-05 Device Success (OAuth device-flow, RFC 8628).
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J05
// LIVE pre-conditions: dev-public infra UP + JWT seeded + auth-dev reachable.

package dogfood

import (
	"context"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-05 Device Success", Label("live", "dogfood", "j05"), func() {
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

	Context("Scenario A — device-flow prerequisites reachable", func() {
		It("auth stack up + seeded session authenticates (device flow prereq)", func() {
			status, _, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/projects", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
		})
	})

	Context("Scenario B — RFC 8628 device lifecycle (cross-device)", func() {
		It("device_code → user approves on second device → token issued", func() {
			session, err := LoadSession()
			Expect(err).ToNot(HaveOccurred())

			// The device (the CLI) asks for a code. It has no session.
			device, err := NewBrowser(env, "")
			Expect(err).ToNot(HaveOccurred())
			started, err := device.Do(ctx, "POST", env.CloudBaseURL+"/v1/auth/device/authorize", "application/json", map[string]any{}, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(started.Status).To(Equal(200))
			grant := started.JSON()
			deviceCode, _ := grant["device_code"].(string)
			userCode, _ := grant["user_code"].(string)
			verify, _ := grant["verification_uri_complete"].(string)
			Expect(deviceCode).ToNot(BeEmpty())
			Expect(userCode).ToNot(BeEmpty())
			Expect(verify).To(HavePrefix(env.AuthBaseURL + "/"))

			pending, err := device.Do(ctx, "POST", env.CloudBaseURL+"/v1/auth/device/token", "application/json",
				map[string]any{"device_code": deviceCode}, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(pending.JSON()["status"]).To(Equal("pending"), "a code nobody approved answered %s", pending.Body)

			// The person opens the link on a second device, signed in, and approves.
			person, err := NewBrowser(env, session)
			Expect(err).ToNot(HaveOccurred())
			next, accepted, landed := verify, false, ""
			for hop := 0; hop < 15 && next != ""; hop++ {
				answer, err := person.Do(ctx, "GET", next, "text/html", nil, false)
				Expect(err).ToNot(HaveOccurred())
				landed, next = next, answer.Location
				if next == "" {
					Expect(answer.Status).To(Equal(200), "the approval stopped at %s with %d", landed, answer.Status)
					break
				}
				target, err := url.Parse(next)
				Expect(err).ToNot(HaveOccurred())
				if challenge := target.Query().Get("device_challenge"); challenge != "" && !accepted {
					// The dashboard's /device-verify page posts the code the person confirms.
					accept, err := person.Do(ctx, "POST", env.CloudBaseURL+"/v1/auth/device/accept", "application/json",
						map[string]any{"device_challenge": challenge, "user_code": userCode}, false)
					Expect(err).ToNot(HaveOccurred())
					Expect(accept.Status).To(Equal(200), "accept answered %s", accept.Body)
					next, _ = accept.JSON()["redirect_to"].(string)
					Expect(next).ToNot(BeEmpty())
					accepted = true
				}
			}
			Expect(accepted).To(BeTrue(), "the verification link never asked for the code")
			end, err := url.Parse(landed)
			Expect(err).ToNot(HaveOccurred())
			Expect(end.Path).To(Equal("/device-success"), "the approval ended at %s%s", end.Host, end.Path)

			// The device polls and gets its token, which the API accepts.
			var token string
			Eventually(func(g Gomega) {
				polled, err := device.Do(ctx, "POST", env.CloudBaseURL+"/v1/auth/device/token", "application/json",
					map[string]any{"device_code": deviceCode}, false)
				g.Expect(err).ToNot(HaveOccurred())
				token, _ = polled.JSON()["access_token"].(string)
				g.Expect(token).ToNot(BeEmpty(), "the device still has no token")
			}, time.Minute, 5*time.Second).Should(Succeed())

			status, _, err := CurlJSON(ctx, env, "GET", env.CloudBaseURL+"/engine/api/v1/projects", token, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200), "the API refused the token of the device flow")
		})
	})
})
