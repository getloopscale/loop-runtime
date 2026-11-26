// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — J-11 Domains.
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J11
// LIVE pre-conditions: dev-public infra UP + JWT seeded + cloud-dev reachable.

package dogfood

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-11 Domains", Label("live", "dogfood", "j11"), func() {
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

	Context("Scenario A — REST list", func() {
		It("GET /engine/api/v1/domains returns 200", func() {
			status, _, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/domains", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
		})
	})

	Context("Scenario B — verify reports what DNS says", func() {
		It("a domain with no TXT record stays pending and the answer names the record to create", func() {
			_, err := NewEngine(env, jwt).EnsureProject(ctx, ProbeProject)
			Expect(err).ToNot(HaveOccurred())

			// A fresh name under the test zone has no TXT record, so the lookup is
			// real and its answer is "not found". The engine refuses reserved names
			// such as example.com.
			domain := fmt.Sprintf("journey-%d.%s", time.Now().UnixNano(), envOr("LOOPRUNTIME_DOGFOOD_TEST_DOMAIN", "theokit.dev"))
			engine := NewEngine(env, jwt)
			call := func(ctx context.Context, method, path string, in any) map[string]any {
				out := map[string]any{}
				Expect(engine.json(ctx, method, path, idempotencyKey(), in, &out)).To(Succeed())
				return out
			}

			created := call(ctx, "POST", "/api/v1/domains", map[string]string{
				"domain": domain, "project": ProbeProject, "app": "app", "environment": "production",
			})
			DeferCleanup(func() {
				cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				call(cleanup, "DELETE", "/api/v1/domains/"+domain, nil)
			})
			Expect(created["status"]).To(Equal("pending_verification"))
			verification, _ := created["verification"].(map[string]any)
			record := "_loopruntime-verify." + domain
			Expect(verification["txt_name"]).To(Equal(record))
			Expect(verification["txt_value"]).To(HavePrefix("loopruntime-verify="))
			Expect(created["cname_target"]).ToNot(BeEmpty())

			verified := call(ctx, "POST", "/api/v1/domains/"+domain+"/verify", nil)
			Expect(verified["status"]).To(Equal("pending_verification"))
			Expect(verified["verification_error"]).To(ContainSubstring(record))

			read := call(ctx, "GET", "/api/v1/domains/"+domain, nil)
			Expect(read["status"]).To(Equal("pending_verification"), "a failed verification moved the domain")
		})
	})
})
