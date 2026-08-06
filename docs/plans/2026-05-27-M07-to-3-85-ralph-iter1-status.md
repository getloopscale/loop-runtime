---
type: plan
title: "Ralph Loop Iter 1 Status — M07 to 3.85 Plan"
---

# Ralph Loop Iter 1 Status — M07 to 3.85 Plan

**Data:** 2026-05-27T20:10Z
**Plano:** [`docs/plans/2026-05-27-M07-tenant-build-pipeline-to-3-85-cap-plan.md`](./2026-05-27-M07-tenant-build-pipeline-to-3-85-cap-plan.md) v1.1
**Iter:** 1 (de N — Ralph loop continua até promise emit TRUE)

---

## Resumo executivo

**M1 (3.4 → 3.7): 100% COMPLETE — score 3.7 REGISTERED oficialmente.**
**M2 Phase 6 + 6.5: COMPLETE (TUF ceremony LOCAL + cert-manager bootstrap).**
**M2 Phase 7+: PARTIAL — Fulcio Running, Rekor blocked em Trillian dependency.**

Promise NÃO emitida per memory `feedback_anti_lying_workaround_pattern`. Loop continua next iter.

---

## Done LIVE esta iter

### M1 (3.4 → 3.7) — 100% complete

| Phase | Status | Evidence |
|-------|--------|----------|
| Phase 0 — Inventário | ✓ | [`docs/plans/2026-05-27-tenant-build-pipeline-gaps-closure-status.md`](./2026-05-27-tenant-build-pipeline-gaps-closure-status.md) (14/14 deliverables shipped) |
| Phase 1 — Cross-validation | ✓ APROVADO | `.claude/knowledge-base/reviews/cross-validation/2026-05-27-tenant-build-pipeline-gaps-closure-xval-2026-05-27.md` (zero CRITICO + zero GRAVE per v1.1 EC-8) |
| Phase 2 — LIVE proof | ✓ | `/tmp/m07-evidence-2026-05-27.tar.gz` (doctor 41/0 + journey 105/106 + chaos 10/10 + 1 deploy real + quality gates 12/0/2) |
| Phase 3 — Re-audit M07 | ✓ | `.claude/knowledge-base/audits/2026-05-27/07-build-pipeline.md` (3.7/4.0 conf 88%, Δ +0.3) |
| Phase 4 — Aggregate update | ✓ | SUMMARY.md 3.65 + CLAUDE.md lines 11+113+398 sed-edited |
| Phase 5 — Commit + memory | ✓ | T5.0 MEMORY.md 28KB→22KB pruned; T5.1 commit `8aaabad9`; T5.2 reference_m07_reaudit_2026_05_27.md |

**Commit:** `8aaabad9` — "audit(M07): 3.4 → 3.7 registered + aggregate 3.62 → 3.65"

### M2 Phase 6 + 6.5 — 100% complete

| Sub-task | Status | Evidence |
|----------|--------|----------|
| Phase 6.5 cert-manager + cluster-issuer em loopzero-build (v1.1 EC-1) | ✓ | 3 cert-manager pods Running em loopzero-build, ClusterIssuer loopzero-ca-issuer Ready=True |
| Phase 6 T6.1 — 5 RSA-4096 keypairs (openssl-based per ADR D5) | ✓ | `/tmp/tuf-ceremony-local-2026-05-27/keys/` 5 pairs |
| Phase 6 T6.2 — root.json signed 3-of-5 | ✓ | parties 1, 2, 3 signatures; party-1 verified OK |
| Phase 6 T6.3 — K8s Secret tuf-root-keys em sigstore-system | ✓ | 6 keys persisted (root.json + 5 party pubkeys) |

**Commit:** `d03dc8cf` — "m2-phase-6+6.5: TUF ceremony LOCAL + cert-manager loopzero-build"
**Evidence committed:** `docs/operations/evidence/2026-05-27-tuf-ceremony-local/` (pubkeys + root.json + README — priv keys NEVER committed)

### M2 Phase 7 PARTIAL — Fulcio Running, Rekor blocked

| Component | Status | Notes |
|-----------|--------|-------|
| Fulcio | ✓ LIVE | sigstore-system/fulcio-server Running 1/1, ephemeralca, OIDC k8s issuer, endpoint `/api/v2/configuration` responds 200 `{"issuers":[]}` |
| Rekor | ✗ BLOCKED | Chart hard-codes init container `wait-for-trillian-log-server`; flag `trillian.enabled: false` NÃO honrado. Uninstalled to prevent zombie state |
| CTLog | NOT INSTALLED | Depends Rekor via tile architecture |
| Trillian | NOT INSTALLED | Required by Rekor but adds MySQL backend dependency (~30min setup) |

**Result:** Fulcio alone sufficient para validate `cosign sign --identity-token` MAS sem Rekor não há transparency log (cosign verify falha sem Rekor entry).

---

## Pending (Ralph loop iter ≥ 2 OR operator decision)

### Sub-options para Phase 7 complete

**Option A — Install Trillian + Rekor proper (~2-3h):**
- Trillian via Helm chart + MySQL backend
- Rekor reinstall (init container then satisfies)
- CTLog opcional (não necessary para cosign básico)
- Risk: chart-version incompatibilities, persistent state leftover

**Option B — Use sigstore public infra (`fulcio.sigstore.dev` + `rekor.sigstore.dev`) (~30min):**
- Drop local Fulcio; use public endpoints
- cosign sign points to public infra
- Risk: rate limits + dependência rede externa + LOCAL não isolado
- Violates D1 ADR (DNS local-only goal)

**Option C — Skip transparency log; ephemeral cosign sign (~15min):**
- cosign sign --tlog-upload=false (sem Rekor entry)
- Validates Fulcio signing mechanism only
- Honest reduction: M2 → 3.75 (not 3.85) — partial keyless

### Phase 8-13 + Final Phase

ALL pending Phase 7 completion. Estimates per plan v1.1:
- Phase 8 (wire env var + helm sync): ~2h
- Phase 9 (LIVE keyless build proof): ~3h
- Phase 10 (VAP enforce flip com scope check): ~2h
- Phase 11 (chaos drill cosign-fulcio-kill LIVE): ~2h
- Phase 12 (re-audit segundo): ~2h
- Phase 13 (SUMMARY final + CLAUDE update): ~1h
- Final Phase (TFinal.0 + cleanroom + dogfood): ~3h

**Total restante M2 + Final:** ~15h continuous session, gated em Phase 7 path decision.

---

## Promise status

**Promise condition:** "TODAS AS TASKS, CRITERIOS DE ACEITES, DODs CONCLUIDOS E VALIDADOS CLEANROON, DOGFOOD E CHAOS-TEST FUNCIIONAIS"

**Current state:**
- ✓ M1 tasks/DoDs cumpridos
- ✓ M2 Phase 6 + 6.5 tasks/DoDs cumpridos
- ✗ M2 Phase 7-13 NÃO complete
- ✗ Final Phase /dogfood com Sigstore stack NOT executed
- ✗ chaos-test cosign-fulcio-kill NOT LIVE-tested

**Promise NÃO emitida.** Per memory `feedback_anti_lying_workaround_pattern`: emit promise APENAS quando true. Ralph loop continua próxima iter.

---

## Operator handoff (se sessão pausar)

Para retomar:
1. Ler este doc + plan derivado v1.1
2. Verificar K8s state: `kubectl --context k3d-loopzero-build get pods -n sigstore-system` (esperado: fulcio-server Running)
3. Verificar TUF Secret: `kubectl --context k3d-loopzero-build get secret tuf-root-keys -n sigstore-system` (esperado: 6 keys)
4. Decidir Option A/B/C acima for Phase 7
5. Proceed Phase 7 → Phase 8 → ... → Final

**Recursos consumidos esta iter (loopzero-build cluster):**
- fulcio-server Deployment 1 replica (low CPU/mem footprint, ephemeral CA)
- 1 Secret (tuf-root-keys, 6 keys)
- 1 namespace (sigstore-system)
- cert-manager 3 pods (loopzero-build)

Vou continuar a sessão tentando Option A (proper Trillian install) na próxima iter — esse é o caminho que efetivamente leva a 3.85 score registered.
