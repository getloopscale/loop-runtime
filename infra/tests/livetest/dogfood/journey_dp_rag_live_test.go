// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

// DP9 — loop-runtime-rag LIVE journey (rag-edge-live anchor).
//
// This is the anchor scenario per loop-runtime/.claude/rules/dogfood-golden-rule.md
// § 1 (slug rag-edge-live): a real loop-runtime-rag capability, exercised end-to-end
// with a real per-tenant key + real OpenAI provider (text-embedding-3-small
// + real LLM), against the REST surface. The MCP-surface half of the anchor
// is validated by the sibling gateway integration tests
// (loop-console/internal/mcp/*_test.go); this file owns the REST half.
//
// Why direct-host + edge fail-closed both matter (Rule 3 — honesty):
//   - The Model B customer-key exchange through the edge broker is currently
//     under investigation (README-QA-REDTEAM.md § "Known edge behavior"). Until
//     that quirk resolves, direct-host proves the product; the edge fail-closed
//     assertion proves the M1 canonical fix is intact.
//   - When the edge chain closes, this same test file gets a Scenario C that
//     exercises the same flow through env.AppBaseURL (no re-scaffolding).
//
// Build with: go test -tags=live ./infra/tests/livetest/dogfood/... -run DPRag

package dogfood

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DP-rag loop-runtime-rag LIVE (rag-edge-live anchor)",
	Label("live", "dogfood", "dp-rag", "anchor"), func() {
		var (
			ctx    context.Context
			cancel context.CancelFunc
			env    Env
			ep     DataPlaneEndpoint
			key    string
		)

		BeforeEach(func() {
			ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
			env = DefaultEnv()
			var err error
			ep, err = LoadDataPlaneEndpoint(env, "rag")
			Expect(err).ToNot(HaveOccurred(), "data_plane.rag seed required — run seed-post-cleanroom-all.sh --verify")
			// rag's admin_bootstrap is single-use per dev/rag-up.sh — the
			// persisted platform_admin_key already carries rag:{read,write,
			// delete,admin} scopes (rag-up.sh:61), so use it as the customer
			// bearer on host paths.
			key = ep.PlatformAdminKey
			Expect(key).ToNot(BeEmpty())
		})

		AfterEach(func() {
			cancel()
		})

		Context("Scenario A — edge fail-closed contract", func() {
			It("GET app-dev /v1/rag/v1/health without a bearer returns 401", func() {
				status, _, err := CurlJSON(ctx, env, "GET",
					env.AppBaseURL+ep.EdgePath+"v1/health", "", nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(401),
					"unauthenticated /v1/rag/ prefix MUST fail-closed — a 2xx is a CWE-441 regression on the M1 fix")
			})
		})

		Context("Scenario B — host process create/ingest/poll/ask (real OpenAI)", func() {
			It("creates a collection, ingests raw text, polls to ready, asks for a cited answer", func() {
				hostURL := DataPlaneHostURL(ep)

				// (1) POST /v1/collections — CreateCollectionSchema
				//     (packages/api/src/server/handlers/collections.ts:154).
				collName := fmt.Sprintf("dp9-anchor-%d", time.Now().UnixNano())
				body, _ := json.Marshal(map[string]any{"name": collName})
				status, out, err := CurlJSON(ctx, env, "POST", hostURL+"/v1/collections", key, body)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(201), "POST /v1/collections returns 201 with the created Collection")
				collID, _ := out["id"].(string)
				Expect(collID).To(HavePrefix("col_"),
					"collection id must be an IdSchema('col') per contract/collection/collection.ts:19")

				// AfterEach-like cleanup: best-effort delete on completion.
				defer func() {
					cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cleanCancel()
					_, _, _ = CurlJSON(cleanCtx, env, "DELETE", hostURL+"/v1/collections/"+collID, key, nil)
				}()

				// (2) POST /v1/documents/raw — inline content ingest
				//     (handlers/documents.ts:409). The paragraph is
				//     purposely specific so the answer step in (4) can
				//     assert grounded citation semantics rather than an
				//     LLM hallucination that happens to look right.
				paragraph := "The DP9 canonical smoke — 2026-07-14 marker — verifies that loop-runtime-rag " +
					"ingestion, embedding, retrieval, and answer synthesis operate end-to-end " +
					"with a real OpenAI provider and a per-tenant workspace key."
				docBody, _ := json.Marshal(map[string]any{
					"collection_id": collID,
					"name":          "dp9-anchor-doc.txt",
					"content":       paragraph,
				})
				status, out, err = CurlJSON(ctx, env, "POST",
					hostURL+"/v1/documents/raw", key, docBody)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(BeElementOf([]int{200, 201, 202}),
					"POST /v1/documents/raw returns 2xx (200 cached / 201 created / 202 accepted)")
				docID, _ := out["id"].(string)
				Expect(docID).To(HavePrefix("doc_"),
					"document id must be IdSchema('doc') per contract/document/document.ts:8")

				// (3) Poll GET /v1/documents/{id} until status="ready"
				//     (documents.ts:583) — DocumentStatusSchema is
				//     {pending, processing, ready, failed, deleting}
				//     per contract/document/document-status.ts.
				Eventually(func() string {
					_, doc, e := CurlJSON(ctx, env, "GET",
						hostURL+"/v1/documents/"+docID, key, nil)
					if e != nil {
						return "err:" + e.Error()
					}
					s, _ := doc["status"].(string)
					return s
				}, 2*time.Minute, 3*time.Second).Should(Equal("ready"),
					"document MUST reach status=ready within 2min (worker embeds via real OpenAI)")

				// (4) POST /v1/answers — REAL RAG synthesis (answers.ts:169).
				//     Blueprint OQ1 correction: canonical path is /v1/answers,
				//     NOT /v1/ask. Request schema: AnswerRequestSchema
				//     (answers.ts:66-96) requires {question, collection_id};
				//     response is AnswerSchema (contract/answer/answer.ts:34)
				//     with {answer, citations[], retrieved_chunks?}.
				askBody, _ := json.Marshal(map[string]any{
					"question":      "What does the DP9 canonical smoke marker verify?",
					"collection_id": collID,
					"top_k":         5,
				})
				status, out, err = CurlJSON(ctx, env, "POST",
					hostURL+"/v1/answers", key, askBody)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(200), "POST /v1/answers returns 200 with the synthesized Answer")

				answer, _ := out["answer"].(string)
				Expect(strings.TrimSpace(answer)).ToNot(BeEmpty(),
					"answer field MUST be non-empty when status=completed (Rule 8 — no null-as-error)")

				// Citation-backed answer invariant (the whole reason the
				// anchor exists — a floating hallucination is dogfood
				// theatre per dogfood-golden-rule.md § 7).
				citations, _ := out["citations"].([]any)
				Expect(citations).ToNot(BeEmpty(),
					"citations[] MUST be populated — an uncited answer is not proof of retrieval")
				var foundDoc bool
				for _, c := range citations {
					cit, _ := c.(map[string]any)
					if did, _ := cit["document_id"].(string); did == docID {
						foundDoc = true
						break
					}
				}
				Expect(foundDoc).To(BeTrue(),
					"at least one citation MUST reference the ingested document_id — anchor invariant")
			})
		})
	})
