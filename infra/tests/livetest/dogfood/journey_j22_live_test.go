// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — J-22 Secrets CRUD.
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run J22
// LIVE pre-conditions: dev-public infra UP + JWT seeded + cloud-dev reachable
// + OpenBao transit + ESO wired (per cleanroom-up-live-test-mandate § 2.5).

package dogfood

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("J-22 Secrets CRUD", Label("live", "dogfood", "j22"), func() {
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
		It("GET /engine/api/v1/secrets returns 200", func() {
			status, _, err := CurlJSON(ctx, env, "GET",
				env.CloudBaseURL+"/engine/api/v1/secrets", jwt, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200))
		})
	})

	Context("Scenario B — create → ESO sync → K8s Secret materialized", func() {
		It("create secret round-trips through OpenBao transit + ESO to a K8s Secret", func() {
			// The full chain (Dashboard → BFF → Engine → PG vault:v1: → ESO →
			// broker → transit decrypt → K8s Secret) is the LIVE zero-leak proof;
			// it mutates cluster state + needs KubectlGetJSON on the tenant ns.
			// Deferred to a T3.2 follow-up commit (see dev-public-secrets-zero-leak_test.sh).
			Skip("secret create/sync spec — implementation in T3.2 follow-up commit")
		})
	})
})
