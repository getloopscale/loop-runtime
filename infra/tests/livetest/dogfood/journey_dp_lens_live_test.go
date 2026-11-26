// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

// DP9 — loopruntime-lens LIVE journey.
//
// Blueprint OQ2 correction: POST /v1/replay-sessions accepts `{traceId}`
// (packages/api/src/server/app.ts:399-415), NOT `{agent_id}`. The body is
// validated at the boundary (fail-fast 400 on missing/empty traceId per
// Rule 8). Also OQ4: loopruntime-lens Model B twin path — MintKey signature matches
// loopruntime-memory/loopruntime-promptly (POST /v1/admin/keys → FLAT
// {id, name, key_prefix, scopes, workspace_id, secret} @ 200), see
// loopruntime-cloud/internal/thelens/admin_client.go:14-20.
//
// Why direct-host + edge fail-closed both matter (Rule 3 — honesty):
//   - The Model B customer-key exchange through the edge broker is currently
//     under investigation (README-QA-REDTEAM.md § "Known edge behavior"). Until
//     that quirk resolves, direct-host proves the product; the edge fail-closed
//     assertion proves the M1 canonical fix is intact.
//
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run DPLens

package dogfood

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DP-lens loopruntime-lens LIVE", Label("live", "dogfood", "dp-lens"), func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		env    Env
		ep     DataPlaneEndpoint
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
		env = DefaultEnv()
		var err error
		ep, err = LoadDataPlaneEndpoint(env, "lens")
		Expect(err).ToNot(HaveOccurred(), "data_plane.lens seed required — run seed-post-cleanroom-all.sh --verify")
	})

	AfterEach(func() {
		cancel()
	})

	Context("Scenario A — edge fail-closed contract", func() {
		It("GET app-dev /v1/lens/health without a bearer returns 401", func() {
			status, _, err := CurlJSON(ctx, env, "GET",
				env.AppBaseURL+ep.EdgePath+"health", "", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(401),
				"unauthenticated /v1/lens/ prefix MUST fail-closed — a 2xx is a CWE-441 regression on the M1 fix")
		})
	})

	Context("Scenario B — host process record replay + north-star metrics", func() {
		It("records a replay-session and reads the north-star aggregate", func() {
			hostURL := DataPlaneHostURL(ep)
			key := mintLensCustomerKey(ctx, env, ep)
			Expect(key).ToNot(BeEmpty())

			// (1) POST /v1/replay-sessions — app.ts:399-415.
			//     Body shape (OQ2 close): {"traceId":"<non-empty>"};
			//     scope required: observability:write.
			body, _ := json.Marshal(map[string]any{"traceId": "dp9-probe-trace"})
			status, out, err := CurlJSON(ctx, env, "POST",
				hostURL+"/v1/replay-sessions", key, body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200),
				"POST /v1/replay-sessions returns 200 with {recorded:true} per app.ts:414")
			recorded, _ := out["recorded"].(bool)
			Expect(recorded).To(BeTrue(),
				"response envelope MUST carry recorded:true — anything else is a semantics regression")

			// (2) GET /v1/metrics/north-star — app.ts:417.
			//     Requires scope observability:read; response is validated
			//     against northStarResponseSchema.
			status, out, err = CurlJSON(ctx, env, "GET",
				hostURL+"/v1/metrics/north-star", key, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200),
				"GET /v1/metrics/north-star returns 200 (public spec — schema pinned)")
			// Actual response keys per LIVE probe 2026-07-14:
			// {"tracesPerDay":[], "replaySessionsPerWeek":[]}. Pin one
			// canonical key so a rename gets caught here (contract test).
			Expect(out).To(HaveKey("tracesPerDay"),
				"north-star response MUST include tracesPerDay[] per northStarResponseSchema")
			Expect(out).To(HaveKey("replaySessionsPerWeek"),
				"north-star response MUST include replaySessionsPerWeek[] per northStarResponseSchema")
		})
	})

	Context("Scenario C — negative case: empty traceId is rejected 400", func() {
		It("POST /v1/replay-sessions {} returns 400 INVALID_BODY (fail-fast at boundary)", func() {
			hostURL := DataPlaneHostURL(ep)
			key := mintLensCustomerKey(ctx, env, ep)
			// Empty body triggers app.ts:410-412 fail-fast:
			// "traceId is required" — proves the boundary validation is
			// not swallowed (Rule 8 — negative case = explicit typed error).
			body, _ := json.Marshal(map[string]any{})
			status, out, err := CurlJSON(ctx, env, "POST",
				hostURL+"/v1/replay-sessions", key, body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(400),
				"empty body MUST fail-fast 400 per app.ts:410 — not 500, not silent-drop")
			code, _ := out["code"].(string)
			Expect(code).To(Equal("INVALID_BODY"),
				"typed error code MUST be INVALID_BODY per apiError shape")
		})
	})
})

// mintLensCustomerKey provisions a per-tenant observability:{read,write} key
// via the direct-host bootstrap OR falls back to LOOPRUNTIME_DP9_LENS_KEY.
func mintLensCustomerKey(ctx context.Context, env Env, ep DataPlaneEndpoint) string {
	if k := envKey("LOOPRUNTIME_DP9_LENS_KEY"); k != "" {
		return k
	}
	key, err := MintDataPlaneCustomerKey(env, ep, "", "dp9-probe", "default",
		[]string{"observability:read", "observability:write"})
	Expect(err).ToNot(HaveOccurred(),
		"lens bootstrap mint failed AND no LOOPRUNTIME_DP9_LENS_KEY fallback in env — set the env or reset admin_bootstrap")
	_ = ctx
	return key
}
