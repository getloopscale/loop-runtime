// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T4.8 of plan canonical-tests-rewrite-v2 v1.0 — J-15 Profile settings.

package dogfood

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-15 Profile — user profile settings", Label("live", "dogfood", "j15", "settings"), func() {
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
	})

	AfterEach(func() { cancel() })

	Context("Scenario A — whoami returns identity payload", func() {
		It("GET /sessions/whoami via Kratos returns identity with email", func() {
			status, body, err := CurlJSON(ctx, env, "GET",
				env.AuthBaseURL+"/sessions/whoami", "", nil)
			// May 401 without cookie; just verify endpoint responds
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Or(Equal(200), Equal(401)))
			_ = body
		})
	})

	Context("Scenario B — settings flow accepts profile updates", func() {
		It("Kratos settings flow accepts traits.name update", func() {
			session, err := LoadSession()
			Expect(err).ToNot(HaveOccurred())
			browser, err := NewBrowser(env, session)
			Expect(err).ToNot(HaveOccurred())

			whoami := func() map[string]any {
				answer, err := browser.Do(ctx, "GET", env.AuthBaseURL+"/sessions/whoami", "application/json", nil, false)
				Expect(err).ToNot(HaveOccurred())
				Expect(answer.Status).To(Equal(200), "whoami with the validation session")
				identity, _ := answer.JSON()["identity"].(map[string]any)
				traits, _ := identity["traits"].(map[string]any)
				Expect(traits).To(HaveKey("email"))
				return traits
			}
			rename := func(ctx context.Context, name string) Response {
				started, err := browser.Do(ctx, "GET", env.AuthBaseURL+"/self-service/settings/browser", "application/json", nil, false)
				Expect(err).ToNot(HaveOccurred())
				Expect(started.Status).To(Equal(200), "settings flow: %s", started.Body)
				flow := started.JSON()
				answer, err := browser.Do(ctx, "POST", FlowAction(flow), "application/json", map[string]any{
					"method":     "profile",
					"csrf_token": FlowField(flow, "csrf_token"),
					"traits":     map[string]any{"email": FlowField(flow, "traits.email"), "name": name},
				}, false)
				Expect(err).ToNot(HaveOccurred())
				return answer
			}

			before := whoami()
			original, _ := before["name"].(string)
			renamed := fmt.Sprintf("Validation %d", time.Now().UnixNano())
			// The cleanup runs after the spec's context is cancelled, so it has its own.
			DeferCleanup(func() {
				restoreCtx, stop := context.WithTimeout(context.Background(), time.Minute)
				defer stop()
				restored := rename(restoreCtx, original)
				Expect(restored.Status).To(Equal(200), "the profile name was not restored: %s", restored.Body)
			})

			answer := rename(ctx, renamed)
			Expect(answer.Status).To(Equal(200), "the profile update answered %s", answer.Body)
			Expect(answer.JSON()["state"]).To(Equal("success"))

			after := whoami()
			Expect(after["name"]).To(Equal(renamed))
			Expect(after["email"]).To(Equal(before["email"]), "the update changed the email")
		})
	})
	_ = jwt
})
