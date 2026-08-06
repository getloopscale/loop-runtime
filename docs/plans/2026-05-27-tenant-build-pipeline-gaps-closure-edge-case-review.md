---
title: "Edge Case Review — tenant-build-pipeline-gaps-closure-plan"
generated: { by: process:edge-case-plan-skill-invocation, at: 2026-05-27 }
name: tenant-build-pipeline-gaps-closure-edge-case-review
description: Edge case review do plano 2026-05-27-tenant-build-pipeline-gaps-closure-plan.md via /edge-case-plan. Identifica 9 MUST FIX + 3 SHOULD TEST + 3 DOCUMENT que devem ser incorporados antes de implementação começar.
type: review
created_at: 2026-05-27
updated_at: 2026-05-27
updated_by: /edge-case-plan skill invocation
---

# Edge Case Review — tenant-build-pipeline-gaps-closure-plan

**Data:** 2026-05-27
**Plano:** [`docs/plans/2026-05-27-tenant-build-pipeline-gaps-closure-plan.md`](./2026-05-27-tenant-build-pipeline-gaps-closure-plan.md) v1.0
**Tasks analisadas:** 9 (T1.1-T1.5 + T2.1-T2.3 + T3.1 + T4.1)
**Edge cases encontrados:** 15 (**MUST FIX: 9, SHOULD TEST: 3, DOCUMENT: 3**)
**Veredicto:** **PLANO PRECISA DE AJUSTE** — 9 MUST FIX deve ser absorvido antes de Phase 1 começar.

Nota: o plano já tinha edge case review inline com 3 MUST FIX + 2 SHOULD TEST + 2 DOCUMENT (EC-1 a EC-7). Esta revisão externa adiciona items NOVOS não-cobertos pelo review inline.

## MUST FIX (9)

### EC-NEW-1: Fulcio Prometheus metric name incorreto

- **Task afetada:** T1.2
- **Família:** Format / Integration
- **Cenário:** Plan declara alert sobre `fulcio_cert_issuance_duration`. Fulcio binary emite `fulcio_cert_issuance_duration_seconds` (Prometheus naming convention: histogram base unit suffix). Sem `_seconds`, alert nunca dispara — nenhum match no PromQL.
- **Impacto:** Alert silenciosamente nunca fires. Operator nunca recebe sinal de Fulcio cert issuance slow.
- **Fix sugerido:** Trocar todas referências a `fulcio_cert_issuance_duration` por `fulcio_cert_issuance_duration_seconds` (4 ocorrências no plano: 2 em descrição + 2 em PromQL exemplo).

### EC-NEW-2: `tuf_root_expiry_timestamp` métrica sem emissor definido

- **Task afetada:** T1.2 (Alert 4)
- **Família:** Wiring
- **Cenário:** Plan diz "Requires LoopZero-emitted metric reading TUF ceremony date from outputs.tufCeremonyDate Pulumi output" mas NÃO define quem emite. Sigstore-scaffolding chart não exporta essa métrica nativamente. Sem emissor, alert é um stub que nunca tem dados.
- **Impacto:** "Implemented but not wired" pattern — alert renders mas nunca dispara. TUF root pode expirar sem warning.
- **Fix sugerido:** Adicionar T1.2.b sub-task: emit `tuf_root_expiry_timestamp` via CronJob que lê `pulumi stack output --json | jq .tufCeremonyDate` + posta para Pushgateway daily. ~2h eng adicional.

### EC-NEW-3: Chaos drill T1.3 cannot run until Sigstore ATIVADO

- **Task afetada:** T1.3
- **Família:** State / Prerequisite
- **Cenário:** Plan ships manifest `cosign-fulcio-kill.yaml` que targets pods Fulcio com labelSelector. Mas Sigstore NÃO está ativado ainda (operator HARD STOP T2.2 activation). Manifest existe no repo mas chaos-test pode falhar silenciosamente (no pods match selector) OU operator confunde e tenta rodar antes de Sigstore live.
- **Impacto:** Drill produz "0 pods affected" → false confidence "chaos passed". Sem signal real.
- **Fix sugerido:** Adicionar `loopzero.io/prerequisite: "sigstore-active"` annotation + pre-flight check em chaos-test SKILL: skip manifests com prerequisite annotation se prerequisite não satisfeito (e.g., `kubectl get pods -n sigstore-system -l app=fulcio` returns 0).

### EC-NEW-4: OIDC issuer URL intra-cluster quebra multi-cell signing

- **Task afetada:** T1.4
- **Família:** Multi-Cell / Cross-Cluster
- **Cenário:** Plan default `https://kubernetes.default.svc.cluster.local` é URL intra-cluster (resolve apenas no cluster que emite). LoopZero é **multi-cluster**: build cluster ASSINA imagens, workload clusters (cell-default, cell-1, cell-2) VERIFICAM. Workload cluster precisa resolver issuer URL do build cluster. URL intra-cluster do build NÃO é alcançável de outros clusters.
- **Impacto:** ClusterImagePolicy keyless verification falha em workload clusters → ALL pods rejected at admission → cluster-wide outage post-Sigstore activation.
- **Fix sugerido:** Usar **cluster-external OIDC URL** (DOKS provides public OIDC discovery URL via `kubectl get --raw /.well-known/openid-configuration`). Cada cluster tem URL distinta — plan deve documentar que operator extracts BUILD cluster's external OIDC URL + pin it (NOT workload cluster's).

### EC-NEW-5: cosign-attest DAG dependency só em syft (falta cosign-sign)

- **Task afetada:** T2.1 + T2.2
- **Família:** State / DAG ordering
- **Cenário:** Plan T2.1 Deep Dives diz "Insert attestation step between syft + push". TDD test `test_workflow_builder_inserts_attest_step_after_syft` confirma dep em syft. MAS `cosign attest` precisa que cosign-sign step JÁ tenha rodado (cosign attest associates attestation to existing signature OR creates new one — para SLSA L3 link, sig deve preceder).
- **Impacto:** Se cosign-attest run sem cosign-sign complete → 2 Rekor entries unlinked → SLSA L3 verification FAILS (no chain between sig + attestation).
- **Fix sugerido:** T2.1 TDD adicionar `test_attest_step_depends_on_BOTH_sign_AND_syft()` + workflow_builder.go: `attestStep.Dependencies = []string{signStepName, syftStepName}` (NOT só syft).

### EC-NEW-6: cosign-attest step sem activeDeadlineSeconds

- **Task afetada:** T2.1
- **Família:** Timing / Resource
- **Cenário:** Plan adiciona attestStep mas não especifica timeout. Rekor append latency é unbounded (depende de transparência log throughput + cross-witness). Build pode hang indefinidamente em attestation se Rekor degradado.
- **Impacto:** Build workflow nunca completa → tenant deploy stuck → manual intervention required.
- **Fix sugerido:** T2.1 add `activeDeadlineSeconds: 180` (3min) to attest container template, matching existing cosign-sign timeout pattern.

### EC-NEW-7: T3.1 Pushgateway vs direct Prometheus client indecidido

- **Task afetada:** T3.1
- **Família:** Architecture decision missing
- **Cenário:** Plan menciona "increments counter via Pushgateway or directly via cgroup-isolated Prometheus client" — OR ambíguo, não decide. Pushgateway tem known issue: stale metrics persist forever (job não auto-deletes). Direct client requires scrape target registration (more complex).
- **Impacto:** Operator escolhe ad-hoc na implementação → metric incoherence (stale failures contadas eternamente).
- **Fix sugerido:** Decide em ADR: use **Pushgateway WITH TTL** (Pushgateway `--web.enable-admin-api` + push with `Push-Lifetime: 24h` header). Push from exit handler then auto-expire. Trade-off: 24h window for alerting OK.

### EC-NEW-8: T3.1 Loki ingest endpoint unreachable hangs exit handler

- **Task afetada:** T3.1
- **Família:** I/O / Resilience
- **Cenário:** Exit handler container POSTs to Loki ingest URL. Se Loki down OR network partition → curl/script hangs até connection timeout default (poderia ser ~120s). Workflow exit handler bloqueado → Workflow "Cancelling" indefinitely.
- **Impacto:** Workflow CR stuck em Cancelling state → API doesn't release build slot → next build rejected (409 concurrent).
- **Fix sugerido:** Exit handler script: `curl --max-time 10 --connect-timeout 5 -X POST ...`. Failure non-fatal (we're already in best-effort path).

### EC-NEW-9: T4.1 ADR D3 head vs tail sampling decisão contraditória

- **Task afetada:** T4.1
- **Família:** Architecture decision conflict
- **Cenário:** Plan ADR D3 diz "head-based 10% sampling for build workflows. 100% sampling on errors (tail-based)". MAS head-based decision happens BEFORE error occurs — once decided to drop trace, error context lost. Tail-based requires OTel collector buffering ALL spans + post-completion decision. Plan can't have BOTH head 10% + tail 100% on errors — physically incompatible.
- **Impacto:** Operator implements head 10% (per plan default), errors at 10% rate sampling → 90% of errors lost trace. Or implements tail-based, OTel collector cost explodes at scale.
- **Fix sugerido:** Pick ONE: head-based 10% with `LOOPZERO_BUILD_FORCE_TRACE=true` escape hatch for forensics (recommended — simpler, cheaper). Document: "Errors at 10% sample rate; operator re-runs build with FORCE_TRACE if forensic needed."

## SHOULD TEST (3)

### EC-NEW-10: T1.1 PV resize requires PVC patch (potential downtime)

- **Task afetada:** T1.1
- **Teste sugerido:** `test_pv_resize_runbook_present` — confirma runbook section "expanding Rekor PV beyond initial 140GB" exists em sigstore-storage-model.md com procedure (PVC patch + StatefulSet rollout restart).

### EC-NEW-11: T1.4 SA name special character escape em subjectRegExp

- **Task afetada:** T1.4
- **Teste sugerido:** `test_subject_regexp_escapes_special_chars` — assert `loopzero-build-sa` regex starts with `^` ends with `$` + colon escapes via `\\:`. Future SA renames (e.g., `loopzero-build-sa-v2`) won't accidentally match.

### EC-NEW-12: T2.1 cosign attest idempotency mesmo image rebuild

- **Task afetada:** T2.1
- **Teste sugerido:** `test_attestation_step_idempotent_for_same_digest` — `cosign attest` ran twice for same image@sha256 produces same Rekor entry (or 2 entries, but verification still passes). Already idempotent per cosign docs; test documents the expectation.

## DOCUMENT (3)

### EC-NEW-13: T1.3 chaos drill prerequisite Sigstore activation

- **Risco aceito:** Chaos manifest committed before Sigstore live = waiting for operator to flip. Operator runbook tells when to run.

### EC-NEW-14: T2.1 build retries duplicate Rekor entries

- **Risco aceito:** Same image rebuilt → second attestation Rekor entry created. cosign verification accepts either entry. Storage cost: +1KB per duplicate. Acceptable for retry rate ~5% builds.

### EC-NEW-15: T3.1 transient Harbor failures produce spurious WARN log

- **Risco aceito:** Workflow retry succeeds but first-attempt's exit handler already emitted WARN log. Operator dashboard shows N WARN per build → operator filters by `workflow_phase=Succeeded` for true sustained failures only. Documented in T3.1 runbook query template.

## Padrões Sistemicos Detectados

| Padrão | Encontrado? | Onde |
|--------|-------------|------|
| Implemented but not wired | **SIM** | T1.2 (tuf_root_expiry métrica sem emissor — EC-NEW-2) + T2.3 (verify helper ack'd) |
| Correct code in wrong place | Não | — |
| Project name vs ID | Não | — |
| ArgoCD notifiers not services | Não | — |
| Single ArgoCD App per tenant | Não | — |
| Multi-cell awareness (cluster-aware OIDC) | **SIM** | T1.4 OIDC issuer URL intra-cluster (EC-NEW-4) |
| ArgoCD repo-server cache | Não | — |
| ESO refresh timing | Não | — |
| Decisão pendente vs decidida | **SIM** | T3.1 (Pushgateway vs Prom client — EC-NEW-7) + T4.1 (head vs tail sampling — EC-NEW-9) |

## Resumo

| Task | Edges (novos) | MUST FIX | SHOULD TEST | DOCUMENT |
|------|---------------|----------|-------------|----------|
| T1.1 | 1 | 0 | 1 | 0 |
| T1.2 | 2 | 2 | 0 | 0 |
| T1.3 | 1 | 1 | 0 | 1 |
| T1.4 | 2 | 1 | 1 | 0 |
| T1.5 | 0 | 0 | 0 | 0 |
| T2.1 | 4 | 2 | 1 | 1 |
| T2.2 | 0 | 0 | 0 | 0 |
| T2.3 | 0 | 0 | 0 | 0 |
| T3.1 | 3 | 2 | 0 | 1 |
| T4.1 | 1 | 1 | 0 | 0 |
| **TOTAL** | **15** | **9** | **3** | **3** |

## Veredicto

**PLANO PRECISA DE AJUSTE.**

9 MUST FIX são bloqueadores reais — não-fixar produzia bugs em produção:
- EC-NEW-1: alert silenciosamente nunca dispara (typo metric name)
- EC-NEW-2: TUF expiry sem emissor = sem warning até build bloqueia
- EC-NEW-4: multi-cluster verification quebra = admission outage post-Sigstore
- EC-NEW-5: SLSA L3 chain broken = compliance audit fail
- EC-NEW-6: build hang indefinitely se Rekor degraded
- EC-NEW-9: head/tail sampling conflict = 90% error traces perdidos OU OTel cost explosion

3 SHOULD TEST + 3 DOCUMENT são guardrails contra regressão future.

## Ação recomendada

Antes de implementar Phase 1-4, **incorporar as 9 MUST FIX no plano v1.1**:

1. **T1.2 spec updates** (~30min): metric name typo + tuf_root_expiry emitter cronjob sub-task
2. **T1.3 spec update** (~10min): prerequisite annotation + chaos-test pre-flight check
3. **T1.4 spec update** (~10min): OIDC URL deve ser cluster-external (multi-cell aware)
4. **T2.1 TDD additions** (~10min): test_attest_depends_on_sign_AND_syft + activeDeadlineSeconds
5. **T3.1 ADR addendum** (~10min): Pushgateway-with-TTL decision + Loki timeout
6. **T4.1 ADR D3 rewrite** (~10min): pick head-based + escape hatch

**Total esforço: ~1h30 de plan revision.** Após v1.1, implementação Phase 1-4 segue em paralelo conforme plano.

## Referências

- Plano analisado: [`docs/plans/2026-05-27-tenant-build-pipeline-gaps-closure-plan.md`](./2026-05-27-tenant-build-pipeline-gaps-closure-plan.md)
- AS-IS arch: [`docs/architecture/build-pipeline/tenant-build-pipeline-as-is.md`](../architecture/build-pipeline/tenant-build-pipeline-as-is.md)
- M07 audit: `.claude/knowledge-base/audits/2026-05-26/07-build-pipeline.md`
- Sigstore metric naming: https://github.com/sigstore/fulcio/blob/main/pkg/server/metrics.go
- Pushgateway TTL: https://github.com/prometheus/pushgateway/blob/master/README.md#push-acks
- OTel sampling: https://opentelemetry.io/docs/concepts/sampling/
