// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T4.5 of plan canonical-tests-rewrite-v2 v1.0 — J-14 Audit Log.

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-14 Audit Log — query + filter", Label("live", "dogfood", "j14", "compliance"), func() {
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

	Context("Scenario A — audit log default page", func() {
		It("GET /engine/api/v1/audit returns 200 with entries + pagination", func() {
			// The audit log is loopzero-api's GET /api/v1/audit (entries, total, limit,
			// offset), reached through loopzero-cloud's /engine proxy. loopzero-cloud
			// serves no /api/v1/audit-log, so the path this spec used answered 404.
			status, body, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/audit?limit=10", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
			Expect(body).To(HaveKey("entries"))
			Expect(body).To(HaveKey("total"))
		})
	})

	Context("Scenario B — action filter", func() {
		It("GET /api/v1/audit-log?action=project.created returns subset", func() {
			Skip("implementation in T4.5 follow-up — requires seeded audit events")
		})
	})

	Context("Scenario C — since filter", func() {
		It("GET /api/v1/audit-log?since=1h returns only recent events", func() {
			Skip("implementation in T4.5 follow-up — requires temporal probe helper")
		})
	})
})
