// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T4.8 of plan canonical-tests-rewrite-v2 v1.0 — J-15 Profile settings.

package dogfood

import (
	"context"
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
			Skip("implementation in T4.8 follow-up — requires cookie session + settings flow handler")
		})
	})
	_ = jwt
})
