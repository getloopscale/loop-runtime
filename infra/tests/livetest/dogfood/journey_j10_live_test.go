// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — J-10 Environments (per-project model).
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J10
// LIVE pre-conditions: dev-public infra UP + JWT seeded + cloud-dev reachable.

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-10 Environments", Label("live", "dogfood", "j10"), func() {
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

	Context("Scenario A — REST list (per-project M2 model)", func() {
		It("GET /engine/api/v1/environments returns 200", func() {
			status, _, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/environments", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
		})
	})

	Context("Scenario B — no duplicate environments (J-10 regression #142)", func() {
		It("environments are not duplicated per project", func() {
			// The validation tenant needs a project for the list to say anything.
			target, err := NewEngine(env, jwt).EnsureProject(ctx, ProbeProject)
			Expect(err).ToNot(HaveOccurred())

			status, body, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/environments", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
			entries, ok := body["environments"].([]any)
			Expect(ok).To(BeTrue(), "the answer has no environments array: %v", body)

			seen := map[string]int{}
			ofTarget := []string{}
			for _, raw := range entries {
				entry, _ := raw.(map[string]any)
				project, _ := entry["project"].(string)
				name, _ := entry["name"].(string)
				Expect(name).ToNot(BeEmpty(), "an environment has no name: %v", entry)
				seen[project+"/"+name]++
				if project == target.Name {
					ofTarget = append(ofTarget, name)
				}
			}
			for key, count := range seen {
				Expect(count).To(Equal(1), "environment %s is listed %d times", key, count)
			}
			// Every project is born with the two permanent environments.
			Expect(ofTarget).To(ContainElements("production", "preview"),
				"project %s lists environments %v", target.Name, ofTarget)
		})
	})
})
