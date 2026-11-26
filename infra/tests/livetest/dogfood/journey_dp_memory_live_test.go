// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

// DP9 — loopruntime-memory LIVE journey.
//
// Anchor spec (`loopruntime/.claude/rules/dogfood-golden-rule.md § 1`) requires
// exercising the RAG capability end-to-end through the unified edge. The
// memory + lens siblings ship the same discipline for their surfaces: prove
// (a) the edge fail-closed contract (401 for un-keyed prefix hits — the M1
// canonical fix regression protector) AND (b) that the direct-host product
// actually functions end-to-end with a per-tenant customer key minted from
// the bootstrap chain.
//
// Why direct-host + edge fail-closed both matter (Rule 3 — honesty):
//   - The Model B customer-key exchange through the edge broker is currently
//     under investigation (see loopruntime-workspace/README-QA-REDTEAM.md
//     § "Known edge behavior" — a platform-admin key returns 404 on customer
//     paths). Until that quirk resolves, direct-host proves the product; the
//     edge fail-closed assertion proves the M1 canonical fix is intact.
//
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run DPMemory

package dogfood

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DP-memory loopruntime-memory LIVE", Label("live", "dogfood", "dp-memory"), func() {
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
		ep, err = LoadDataPlaneEndpoint(env, "memory")
		Expect(err).ToNot(HaveOccurred(), "data_plane.memory seed required — run seed-post-cleanroom-all.sh --verify")
	})

	AfterEach(func() {
		cancel()
	})

	Context("Scenario A — edge fail-closed contract", func() {
		It("GET app-dev /v1/memory/v1/health without a bearer returns 401", func() {
			// Regression protector for the M1 canonical fix (loopruntime-cloud edge
			// fail-closed 401×3 restored 2026-07-14 04:17 UTC per
			// dev-credentials.json § data_plane.memory.edge_status).
			status, _, err := CurlJSON(ctx, env, "GET", env.AppBaseURL+ep.EdgePath+"v1/health", "", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(401), "unauthenticated prefix hit MUST fail-closed — a 2xx here is a CWE-441 fail-open regression")
		})
	})

	Context("Scenario B — host process end-to-end remember/list/forget", func() {
		It("remembers, lists, and forgets a memory scoped to agent=dp9-probe", func() {
			hostURL := DataPlaneHostURL(ep)
			key := mintMemoryCustomerKey(ctx, env, ep)
			Expect(key).ToNot(BeEmpty())

			// (1) POST /v1/remember — scope is inline single-tier per RememberBody
			//     (loopruntime-memory/packages/api/src/server/schemas.ts:48-63); "agent"
			//     is the writable tier we can round-trip on the list surface.
			//     Text is nanosecond-stamped because loopruntime-memory dedupes by
			//     content hash: identical text across re-runs returns
			//     {"items":[]} (silent dedup, LIVE-caught 2026-07-14).
			uniq := fmt.Sprintf("%d", time.Now().UnixNano())
			rememberBody, _ := json.Marshal(map[string]any{
				"agentId": "dp9-probe",
				"text":    "DP9 canonical smoke " + uniq + " — memory LIVE (agent scope)",
			})
			status, body, err := CurlJSON(ctx, env, "POST", hostURL+"/v1/remember", key, rememberBody)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(201), "POST /v1/remember must return 201 (openapi-registration.ts:70)")
			items, ok := body["items"].([]any)
			Expect(ok).To(BeTrue(), "response envelope MUST include items[]")
			Expect(items).ToNot(BeEmpty())
			first, _ := items[0].(map[string]any)
			memID, _ := first["id"].(string)
			Expect(memID).ToNot(BeEmpty(), "remembered memory MUST carry an id")

			// (2) GET /v1/scopes/agent/dp9-probe/memories — list surface
			//     mounted at openapi-registration.ts:495-513.
			status, body, err = CurlJSON(ctx, env, "GET", hostURL+"/v1/scopes/agent/dp9-probe/memories", key, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(200), "GET /v1/scopes/agent/{id}/memories MUST return 200")
			listItems, _ := body["items"].([]any)
			var foundID bool
			for _, it := range listItems {
				m, _ := it.(map[string]any)
				if id, _ := m["id"].(string); id == memID {
					foundID = true
					break
				}
			}
			Expect(foundID).To(BeTrue(), "listed memories MUST contain the memId just remembered — round-trip proof")

			// (3) DELETE /v1/scopes/agent/dp9-probe/memories/{memId} —
			//     scoped forget (openapi-registration.ts:242-256).
			status, _, err = CurlJSON(ctx, env, "DELETE",
				fmt.Sprintf("%s/v1/scopes/agent/dp9-probe/memories/%s", hostURL, memID), key, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Or(Equal(204), Equal(200)),
				"scoped DELETE MUST return 204 (or 200) — 404 without hard=true would be an unwanted absence-leak")
		})
	})
})

// mintMemoryCustomerKey provisions a per-tenant memory:{read,write} key via
// the direct-host bootstrap token OR falls back to the pre-minted key stashed
// in env LOOPRUNTIME_DP9_MEMORY_KEY. Fail-fast per Rule 8.
func mintMemoryCustomerKey(ctx context.Context, env Env, ep DataPlaneEndpoint) string {
	// Prefer an operator-provided cached key (rotation-friendly + avoids
	// bootstrap single-use exhaustion mid-run).
	if k := envKey("LOOPRUNTIME_DP9_MEMORY_KEY"); k != "" {
		return k
	}
	// memory:delete is required by the scoped DELETE surface
	// (openapi-registration.ts:242-256) — read+write alone returns 403 (RBAC).
	key, err := MintDataPlaneCustomerKey(env, ep, "", "dp9-probe", "default",
		[]string{"memory:read", "memory:write", "memory:delete"})
	Expect(err).ToNot(HaveOccurred(),
		"memory bootstrap mint failed AND no LOOPRUNTIME_DP9_MEMORY_KEY fallback in env — set the env or reset admin_bootstrap")
	_ = ctx
	return key
}
