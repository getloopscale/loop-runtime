// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — J-01 Login polish (OIDC GitHub + Google).
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J01
// LIVE pre-conditions: dev-public infra UP + JWT seeded + auth-dev reachable.

package dogfood

import (
	"context"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-01 Login polish (OIDC)", Label("live", "dogfood", "j01"), func() {
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
		Expect(err).ToNot(HaveOccurred(), "JWT seed required (proves a prior login succeeded)")
		Expect(HealthcheckCloud(ctx, env)).To(Succeed(), "cloud-dev /health/deep prereq")
	})

	AfterEach(func() { cancel() })

	Context("Scenario A — authenticated session works end-to-end", func() {
		It("a seeded JWT authenticates against an engine route (login round-trip proof)", func() {
			status, _, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/projects", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200), "authenticated request must succeed with a valid session")
		})
	})

	Context("Scenario B — OIDC provider redirect (GitHub/Google)", func() {
		It("POST self-service/methods/oidc returns 303 to the provider", func() {
			providers := map[string]struct{ host, path string }{
				"github": {"github.com", "/login/oauth/authorize"},
				"google": {"accounts.google.com", "/o/oauth2/v2/auth"},
			}
			for provider, want := range providers {
				browser, err := NewBrowser(env, "")
				Expect(err).ToNot(HaveOccurred())
				started, err := browser.Do(ctx, "GET", env.AuthBaseURL+"/self-service/login/browser", "application/json", nil, false)
				Expect(err).ToNot(HaveOccurred())
				Expect(started.Status).To(Equal(200), "login flow for %s", provider)
				flow := started.JSON()
				Expect(FlowAction(flow)).ToNot(BeEmpty())

				form := url.Values{"provider": {provider}, "csrf_token": {FlowField(flow, "csrf_token")}}
				answer, err := browser.Do(ctx, "POST", FlowAction(flow), "text/html", form, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(answer.Status).To(Equal(303), "choosing %s answered %d: %s", provider, answer.Status, answer.Body)

				target, err := url.Parse(answer.Location)
				Expect(err).ToNot(HaveOccurred())
				Expect(target.Host).To(Equal(want.host), "%s redirects to %s", provider, target.Host)
				Expect(target.Path).To(Equal(want.path))
				query := target.Query()
				Expect(query.Get("client_id")).ToNot(BeEmpty(), "%s has no client id configured", provider)
				Expect(query.Get("state")).ToNot(BeEmpty())
				Expect(query.Get("redirect_uri")).To(Equal(env.AuthBaseURL + "/self-service/methods/oidc/callback/" + provider))
			}
		})
	})
})
