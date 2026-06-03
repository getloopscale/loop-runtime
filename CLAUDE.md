# CLAUDE.md

Contract between Claude and the LoopZero codebase. Read before touching any file.

## What This Project Is

LoopZero is a **CLI-first managed cloud (Kubernetes-backed)** for monorepo deployments. Vercel-quality DX for backends — `loopzero login`, `loopzero init`, `loopzero deploy`, and in under 5 minutes every service is live with TLS, preview environments on every PR, and rollback with one command. No Kubernetes. No infrastructure YAML.

The authoritative spec is the code itself + `CHANGELOG.md` + relevant ADRs. If this file and the codebase conflict, the codebase wins.

**Status:** Pre-release. Latest snapshot: `.claude/knowledge-base/audits/2026-05-28-keyless-GREEN/07-build-pipeline.md` (M07 fourth re-audit, M07 3.80→**3.85** +0.05 pós LIVE keyless end-to-end GREEN: sign-image + attest-image Phase=Succeeded, Rekor tlog index 1+2, DB rel-da90a317114862f2 image_signature persisted). Anteriores: M07 3.4→3.7→3.75→3.80 (M1+M2 deste plan derivado). Aggregate baseline: `.claude/knowledge-base/audits/2026-05-26/SUMMARY.md` (12 módulos 3.62/4.0). Active uplift plans: `docs/plans/completed/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md` v1.1 (target portfolio 3.95+, ceiling 3.983 per ADR D3 cap) + `docs/plans/2026-05-28-M07-fulcio-cacert-keyless-LIVE-plan.md` v1.5 (M07 3.85 registered LIVE; 3.90+ gated em operator prod DNS for Sigstore + TUF root pinning). Total Δ session: 3.4 → 3.85 (+0.45). H1-H7 hardening themes complete. Sprint 9 (Polish + Launch) pending gates: (a) operator-run prod-identical drill GREEN; (b) 1 prod customer day-trial with no incidents. **M07 production cutover BLOCKED 2026-05-28** per MEET-2026-05-28-001 (4 absolute vetoes + 11 CRITICAL findings + 24 mandatory actions) — sponsor accepted veredict same day. Engineering side 3.85 LIVE-validated for LOCAL; prod-readiness gated on TUF ceremony + Pulumi sigstore.ts up + drill iter ≥56 + customer trial + 15 engineering actions. **MEET-2026-05-28-001 engineering closure COMPLETED 2026-05-29** per plan `docs/plans/2026-05-28-meet-001-engineering-closure-plan.md` v1.1 — 18 tasks (T0.1 → T4.6) + 27 commits durável, all CR-1..CR-11 fechados, plus T1.3 follow-up regression caught + fixed mid-Final-Phase (`b3168cce` + `b3933d7b`: empty-apps build now returns 400 MISSING_FIELD instead of 500). Final Phase Dogfood QA on cleanroom-fresh infra fully validated `.claude/knowledge-base/audits/2026-05-29-dogfood-cleanroom-fresh-evidence.md` (103 RUNNABLE specs PASS / 0 functional FAIL — 102 main run + 1 focused TIMEDOUT re-run / 29 precondition-skipped) + `.claude/knowledge-base/audits/2026-05-29-chaos-test-full-cleanroom-fresh-evidence.md` (chaos-test --full 10/10 PASS in 52min on cleanroom-fresh 4-cluster k3d, SLO held entire chaos window, C03 random-tenant-pod-kill executed — was SKIP on warm). Engineering signal canonical; operator gates (TUF ceremony + prod Sigstore stack + customer trial + drill prod-identical re-run) remain HARD STOP for prod cutover.
**OPENBAO-ADOPTION ENGINEERING COMPLETE 2026-06-03/04 (plan v1.5)** per Ralph loop session iter 28-35 (14 commits `3f97366d..a0ac21ea` pushed to origin/develop). Migrou platform secrets posture de inline/env-var/AES para OpenBao Vault como cryptographic root: T2 kv-v2 (3 critical secrets), T3 Transit (per-tenant DEK + crypto-shred + dispatcher), T4 ESO broker, T5 6 K8s auth mounts (k8s-mgmt/build/cell-{default,1,2} + legacy) + per-binary roles + projected SA token (audience=vault.loopzero.io expiration=3600s) + ClusterRole legacyClusterWideSecrets flag, T6 database engine (dynamic PG creds TTL 1h/24h) + loopzero-api role + CachingDatabaseClient SDK, T7 kubernetes engine (3 cluster roles) + CachingK8sEngineClient SDK + scoped-kubeconfig.sh Vault-first refactor (legacy fallback for cold-bootstrap), T8 4 P1 alerts (VaultAuthFailureRateHigh/PolicyDenialRateHigh/SealedOrDown/AuditLogStalled invariant=audit-log-must-flow) + Promtail vault-system scrape + Loki retention_stream 90d + DO Spaces archive cron + Grafana dashboard (7 panels uid=openbao-audit), T9 @pulumi/vault dep + VaultSecretReader component + migrate-pulumi-secrets-to-vault.sh + prod-pulumi-bootstrap.md runbook, T10 Day 30 verify + Day 60 AES delete CronJobs (safety defaults dryRun=true) + calendar-gated noaesremaining quality gate (WARN until 2026-08-01). **LIVE evidence iter 32 against k3d-loopzero-vault**: 7/7 Go LIVE tests PASS (auth mounts + secrets engines + Transit round-trip + policy denial 403 + database role TTL + K8s engine roles + cross-cluster isolation). **Test aggregate**: 51 Go unit + 7 Go LIVE + 13 TS contract + 12 hermetic shell tests PASS (138 assertions agregadas). **5 new quality gates wired**: cluster-role-no-secrets-star (T5.15), no-legacy-sa-token-secret (T7.6), no-pulumi-inline-secrets (T9.6 — operator-gated until migration), no-aes-remaining (T10.2 calendar-gated), no-env-vault-secrets (T2.7). Engineering closure: [[MEET-ASYNC-AMENDMENT-2026-06-03-002-openbao-adoption-engineering-closure]] + LIVE evidence audit `.claude/knowledge-base/audits/2026-06-03-openbao-adoption-LIVE-evidence.md`. **Operator-time gates pending** (9 items, see runbook below).

### 🔧 Next session: resume openbao-adoption operator handoff

**Estado atual (2026-06-04):** Engineering 100% completo, todos commits pushed, working tree limpo. Operator-time gates documentados + runbook + scripts prontos para execução LIVE quando recursos disponíveis.

**Pendências reais (9 operator-gated):**

1. **T5.1-T5.4 LIVE per-mount auth/<X>/config writes** — Execute `docs/operations/openbao-multi-cluster-auth-runbook.md` para cada 5 clusters (mgmt + build + cell-default + cell-1 + cell-2). Precisa: credenciais LIVE de cada cluster (kubectl access). Tempo: 30min operator. **Phase A 2026-06-03 (commit `1ac9980f`):** engineering-side LIVE-revalidated cleanroom-fresh — 7/7 `pkg/vault` LIVE tests PASS after `task k3d:vault:create` + `task k3d:vault:bootstrap` (fixed: now includes openbao-cert step) + manual `bao operator init` + helmfile sync openbao-init. Bootstrap recipe + LIVE evidence in audit `.claude/knowledge-base/audits/2026-06-03-openbao-adoption-LIVE-evidence.md` Phase A. Operator-time PROD setup against real cluster kubeconfigs remains.
2. **T6.2 LIVE database/config/postgres-loopzero-api** — Provisionar PG admin role no DOKS managed PG + `bao write database/config/postgres-loopzero-api ...`. Precisa: DOKS PG admin creds. Tempo: 15min operator.
3. **T6.6 LIVE PG audit log validation** (`v-k8s-loopzero-api-*` user visible). Depende de #2. Tempo: 5min após #2.
4. **T7.4 LIVE delete legacy SA-token Secrets** — Operator action pós-T7.3 refactor verify. Tempo: 10min.
5. **T7.5 LIVE cell access via Vault SA token**. Depende de T7.1 config done. Tempo: 15min. **Phase B 2026-06-03 (commit `79c1d64a`):** engineering-side LIVE-closed against k3d-loopzero-mgmt — 3 invariants validated end-to-end: (i) real mint via `bao write kubernetes/creds/loopzero-runtime-mgmt kubernetes_namespace=loopzero-system` returns JWT confirmed by K8s SelfSubjectReview as `system:serviceaccount:loopzero-system:loopzero-runtime` TTL 1h; (ii) Vault namespace allowlist enforcement BEFORE K8s (loopzero-runtime-build → loopzero-system returns HTTP 400 with explicit error); (iii) defense-in-depth — minted SA has zero RBAC by default (HTTP 403 on kube-system pods). Audit doc Phase B section. **Codification of Phase B manual setup as `infra/scripts/openbao/wire-k8s-engine-local.sh` was BLOCKED by governance-gate** (scope: openbao-adoption infra/scripts/ not in approved sqlc-migration amendments) — operator can extend via `/change` to enable durable reproduction. Operator-time PROD needs separate engine mounts per cluster (`kubernetes-mgmt/`, `kubernetes-build/`, `kubernetes-cell-*/`).
6. **T8.6 LIVE alert fire** com Prometheus rodando — kube-prometheus-stack precisa estar instalado em loopzero-mgmt. Tempo: 30min (install + trigger).
7. **T9.2-T9.5 LIVE production secret migration** — **9 inline secrets restantes** (4 prod + 5 dev em `Pulumi.{dev,prod}.yaml`). Execute `infra/scripts/openbao/migrate-pulumi-secrets-to-vault.sh --execute --stack <dev|prod>` per runbook `docs/operations/prod-pulumi-bootstrap.md` (8 passos). Precisa: `PULUMI_CONFIG_PASSPHRASE`. Tempo: 1h operator.
8. **T10.x calendar fires** — Day 30 (2026-07-01) habilita `vault.day30VerifyCron.enabled=true` em loopzero-api overlay; Day 60 (2026-08-01) habilita `vault.day60AesDeleteCron.enabled=true` com `dryRun=false + shredAesKey=true` após sponsor sign-off. Calendar-gated.
9. **Final Phase Dogfood QA** — `/dogfood full` vault-enabled. **Trio LIVE GREEN canonical achieved 2026-06-03 (commits `044b55de` + `a90f59de`):** cleanroom (`task clean:deep` + `task setup` + `task doctor` 42/5/0) + dogfood (103 main PASS + 1 Journey 03 focused re-run = 11 PASS = effective 114/0 functional FAIL) + chaos-test (`run-full.sh` v2 = 10/10 PASS / 0 FAIL / 0 INCONCLUSIVE in 52m12s post-fix of `loopzero-ops` binary path). Trio durable artifacts: KEDA cold-start retry in `httpGetWithHost` + `bin/loopzero-ops` auto-build for chaos verdict. **Resta re-rodar trio com `vaultEnabled: true`** em helmfile env value para validar integration vault-enabled end-to-end (operator window estimado ~3h).

**Arquivos canonicais para continuar:**
- Plano: `.claude/knowledge-base/plans/2026-06-02-openbao-adoption-plan.md` v1.5
- LIVE evidence: `.claude/knowledge-base/audits/2026-06-03-openbao-adoption-LIVE-evidence.md`
- Operator handoff: `.claude/knowledge-base/meetings/MEET-ASYNC-AMENDMENT-2026-06-03-002-openbao-adoption-engineering-closure.md`
- Runbooks: `docs/operations/openbao-multi-cluster-auth-runbook.md` (T5.1-T5.4) + `docs/operations/prod-pulumi-bootstrap.md` (T9.2-T9.5)
- Migrate script: `infra/scripts/openbao/migrate-pulumi-secrets-to-vault.sh` (default --dry-run)
- Vault state (post Phase A+B 2026-06-03): `k3d-loopzero-vault` cluster re-bootstrapped + Shamir 5/3 init + 3-shard unsealed + openbao-init Helm sync provisioned: 6 K8s auth mounts (k8s-mgmt/build/cell-default/cell-1/cell-2 + legacy kubernetes/), 3 K8s engine roles (loopzero-runtime-{mgmt,build,workload}), database engine (loopzero-api role TTL 1h/24h), transit engine, kv-v2 secrets engine. Phase B manual wire of `kubernetes/config` against `k3d-loopzero-mgmt` (vault-k8s-engine SA + loopzero-runtime SA + 24h JWT). **State DESTRUÍDO em próximo `task clean:deep`** — recipe + LIVE evidence em audit doc Phase A+B sections (reprodutível ~10min). Root token + 5 unseal shards `shred -uvz` post-tests (zero persistence em /tmp, commits, ou memory). `infra/scripts/openbao/init-ceremony.sh` permanece source-of-truth para PROD Shamir ceremony (RAMdisk custody Q13)
- LIVE artifacts ANTES desta sessão (já removidos em iter 34 cleanup): nenhum mais em loopzero-mgmt

**Para validar engineering side rapidamente em nova sessão:**
```bash
cd /home/paulo/Projetos/loopzero/loopzero
git pull --ff-only
task quality:gates 2>&1 | tail -20   # 18 gates openbao-adoption devem PASS
go test ./pkg/vault/... ./pkg/quality/{clusterrolenosecretsstar,nolegacysatoken,nopulumiinlinesecrets,noaesremaining,noenvvaultsecrets}/...
```

**Trio LIVE quando recursos OK:**
```bash
# Pré-requisitos:
#   - Fechar Claude sessions extras (libera ~2GB RAM)
#   - Opcional: reboot host (libera swap pressure)
#   - Disk ≥ 50GB free (verificar via df -h /)
task clean:deep         # ~15GB recovered + page cache drop + 4 k3d clusters destroyed
task setup              # ~30min, 4 k3d clusters + 32 helmfile releases
task doctor             # gate 38/0
tilt up                 # em outra terminal/sessão
/dogfood full           # ~45min — validar journeys end-to-end
/chaos-test full        # ~60min — validar SLO holds under chaos
/contra-prova           # ~10-15min — verificação independente (byte-grep + LIVE deploy + Prova de Vida/Morte)
```

**Memory checkpoint:** Para reabrir contexto rapidamente, ler estas 3 seções desta CLAUDE.md (status + Next session + 9 pending items) + ler frontmatter do plano v1.5 + abrir LIVE evidence audit.

---

**VAULT-INT SUBSTANTIVELY COMPLETE 2026-06-03 (plan v1.9)** per Ralph loop session iter 9-26 + sponsor ratification MEET-ASYNC-AMENDMENT-2026-06-03-001 (ACCEPT). Migrou loopzero-api/cloud/tenant-operator de env-var-as-secret para OpenBao Vault como canonical Source of Truth via ESO v0.14.3 D11. Artefatos: (a) **6 canonical operator scripts** em `infra/scripts/openbao/` (vault-bootstrap-eso.sh + seed-loopzero-secrets.sh + vault-sync-check.sh + rotate-secret.sh + enable-vault-backup.sh + restore-vault-snapshot.sh, ~1450 LOC) cobrindo Runbook P1-P5; (b) **9 hermetic test suites = 68 PASS aggregate** (6 script tests + 3 invariant gates: rotation-table-parity, creation-policy regression, chart-sanity 8 invariants); (c) **chart `loopzero-external-secrets`** com 13 ExternalSecret CRs inventory canonical + vaultBinaryOverride per-key cross-binary D6 shared paths; (d) **4 helmfile envs wired** (standalone-multi + dev + drill + prod) com `vaultEnabled` opt-in flag default false; (e) **Phase 9 T9.1 LIVE comprehensive** validation (iter 18-22): full inventory 13/13 ExternalSecret CRs SecretSynced=True + cross-binary D6 + drift recovery <1s + idempotency KV-v2 + rollback drill preserves 13/13 Secrets + rotation HAPPY PATH 4-steps-GREEN + rotation PARTIAL semantics + Vault outage graceful degradation (K8s preserved + Error status). **P0 cascade-delete bug found+fixed via LIVE iter 20**: `creationPolicy: Owner` set `blockOwnerDeletion=true` ownerReference → K8s GC cascade-deleted ALL 13 Secrets on `helm uninstall` BEFORE ESO could honor `deletionPolicy: Retain` → fix `Owner → Orphan` (decoupled lifecycle, no ownerReference); regression test prevents future Owner reintroduction. **Operator handoff** doc `docs/operations/vault-INT-operator-handoff.md` + runbook `docs/operations/vault-secret-migration-runbook.md` P1-P6 procedures (P1+P2+P4+P5 sections refactored com canonical scripts). LIVE evidence: `.claude/knowledge-base/audits/2026-06-03-vault-INT-LIVE-evidence.md`. **T9.2 DOKS drill + T9.3 prod-like smoke deferred to operator-time** (cost authorization gate ~$0.51/run) per sponsor amendment ACCEPT — plan substantively complete pending operator-time DOKS execution when budget available.


**Pricing (per MEET-2026-05-12 D4 = B):** Starter (Free, 3 projects) and Pro ($29/mo, 10 projects) are **public at GA**. Team ($99/mo, unlimited projects + RBAC + progressive delivery config) is **Early Access** (manual provisioning, no public signup until Sprint 10). Enterprise (custom, multi-cell + SSO/SAML + SLA) is **"Contact Us" only** — no public pricing, no signup flow, sales-led with explicit SSO/SAML "post-launch" disclosure (SSO/SAML deferred to v1.x). Go-to-market: bottom-up (validate with indie devs, learn, scale to enterprise). Indie DX is the funnel for enterprise adoption.

**Completed:** deploy happy path (full E2E dogfood validated), **progressive delivery (Argo Rollouts canary)** with TraefikService weighted routing + per-plan steps + AnalysisTemplate hooks + auto-abort on readiness failure (`progressDeadlineSeconds=300`), scaling+HPA targeting Rollout (Starter=2/Pro=5/Team=10), promote staging→prod, rollback, secrets via ESO, custom domains v2 (CF Zone Mgmt, model Vercel), GitHub App auto-deploy (~4min), monorepo validator (30 frameworks), cosign image signing + syft SBOM (CycloneDX) + per-build SBOM download (`GET /builds/{id}/sbom`) + attestation endpoint (`GET /builds/{id}/attestation`), supply-chain admission (policy-controller deny-by-default + warn-mode ClusterImagePolicy + digest enforcement in manifests), plan enforcement (4 enforcement middlewares: project/build/env/seat caps, HTTP 402 on exceed), audit log (filterable by action/resource_type/since, paginated), RSA-only JWT auth, two-host gateway, concurrent deploy rejection (1×202+1×409), JSON contracts (8/8), observability (112 loopzero_* metrics unique-registered including 8 cell metrics + Grafana + SLO alerts + 7 multi-cell alerts; measured 2026-05-26 via grep `Name: "loopzero_` em api/+operators/+pkg/+cloud/), KEDA scale-to-zero (targets Rollout via `Kind=Rollout` ScaleTargetRef + `transfer-hpa-ownership` annotation), cell architecture (Phase 0+1+1.5 + cells table + admin pin), **cell-2 ACTIVATED + LIVE-validated** (3/3 cells connected; namespace cell-aware reconcile; rollback SLO P50=648ms / P95=705ms <<300s budget; 12 ADR gates persistente), operator multi-binary split (4 binaries), layered bootstrap (Pulumi L1 + Helmfile L2 + ArgoCD L3), `loopzero admin tenants pin/evict` + bulk-delete, tenant soft-delete with restore (`DELETE /tenants/{id}` + `POST /tenants/{id}/restore`), **PG circuit breaker** (rejects mutations at pool saturation >90%, HTTP 503), **application reaper** for orphan CRs (`ApplicationCleaned` terminal state via `ApplicationOrphaned` event), **release reaper observability** (`ReleaseStore.AppendSideEffect` writes reaper context to `release.side_effects`), **release ARCHIVED status** (migration 000008, partial index excludes archived from active queries).

**Deferred to v1.x (per MEET-2026-05-12 D2/D4):** SSO/SAML (Enterprise tier blocker — explicit "post-launch" disclosure required on sales-led conversations); cosign **keyless** verification with Fulcio+Rekor (Keyed v1.0 ships with 5 mandatory mitigations per W1.2 of MEET-2026-05-12; Keyless v1.1 ADR committed with 90-day cutover deadline post-v1.0 GA); Loki cross-cluster streaming (infra gap, not code).

**Not built (no commitment yet):** Sprint 8 Phase 1+3+4+5+6 (require new T3 meeting), production canary stages 4-5 for cell-2 (1 customer 7-day → 10/50/100% — gates G4/G6 in MEET-2026-05-12 phased rollout).

**`loopzero scale` — fully functional E2E** (B40 fixed 2026-05-06, tag `scale-b40-converged-20260506`): chain DB→deploy→K8s closed via `ScalingResolver` (`api/internal/services/scaling_resolver.go`) injected into `BuildOrchestratorDeps.ScalingResolver`. Submit calls `Resolve` BEFORE `enforceScaling` so plan-cap operates on the resolved config (closes MEET-2026-04-23-059 Constraint 6). Scale handler persists with project NAME (not UUID) per CLAUDE.md "Project name vs project ID" convention. Validated LIVE via journey-04 hardening with `kubectl get deploy -o jsonpath='{.spec.replicas}'` assertion + Plan-cap probe (`REPLICAS_EXCEED_PLAN` rejection). Cleanroom dogfood 12/12 GREEN end-to-end (52min) with B40 fix shipped.

**Path to maturity 4.0** (currently 3.49 per modular audit baseline 2026-05-11): (a) operator-run prod-identical drill GREEN (was a GitHub Sunday cron `drill-prod-identical-soak.yml` until GitHub Actions was removed 2026-05-23; the drill logic survives and runs via `task drill:l2-scale`) — **2026-05-16 reformulated:** workflow now provisions an ephemeral drill prod-identical 4-cluster stack instead of assuming a non-existent DEV cluster (per plan `2026-05-16-drill-prod-identical-multicluster-plan.md`, finding `concepts/operations/finding-2026-05-16-gate-a-cell2-soaks-multilayer-blocker.md`). Stage 1 (1h) + Stage 2 (24h with EXPIRES_IN=26h per EC-01) execute via drill stack with mandatory teardown + doctl validator fallback (T4.1.bis EC-05). **2026-05-16 L2-unlock plan engineering-side COMPLETE** (plan `2026-05-16-drill-l2-unlock-plan.md` v1.1, MEET-ASYNC-AMENDMENT-2026-05-16-002 ratification pending): 5 new scripts shipped (`drill-state-reset.sh` EC-01, `drill-helmfile-bootstrap.sh` + `run-migrations.sh` PG_SSLMODE=require EC-02, `drill-cancel-refresh.sh` D3, `drill-teardown-validator.sh` VPC retry-backoff D4, `drill-evidence-capture.sh` EC-03 fault-tolerant), 5 consumer files refactored to v2 flat named exports (D1 — replaces v1 object-literal `outputs={...}` that caused Finding #3 null kubeconfig), `check-drill-outputs-consumers.sh` grep audit extended to detect both v0+v1 legacy schemas, vitest 48/48 + 49/49 hermetic tests across 7 scripts. **2026-05-18 L2 PHASE 2 LIVE PASS** (plan `2026-05-16-drill-l2-unlock-phase2-plan.md`): 80+ cascade bugs caught + fixed across 3 sessions (commits prefix `drill-l2-phase2`), bootstrap script consolidated 11 LIVE patches (Bugs #56-#82) so `task drill:l2-scale` reproduces success from clean state. **L2 harness success_rate=100%, p95=251s/4.2min** (target ≥90% / ≤10min) LIVE on prod-identical 4-cluster drill iter 55. Full chain validated: build → cosign → manifests → ArgoCD sync → tenant pods Running 1/1 → HTTPS 200 with Let's Encrypt prod cert. Phase 7 teardown CLEAN (21 resources deleted). Cross-validation **APROVADO** (commit `53d939f2`). Hermetic test suite **143/143 GREEN** (71 drill + 72 vitest pulumi-drill). Audit doc `2026-05-18-drill-l2-pass-evidence.md`. Gate (a) closure now requires (i) D7' sponsor override active (DIGITALOCEAN_TOKEN single-token model per `finding-2026-05-16-D7-prime-sponsor-override.md`) + (ii) MEET-002 ratification + (iii) L2 unlock operator drill re-run (Phase 4 LIVE: provision 4-cluster + bootstrap LoopZero via `drill-helmfile-bootstrap.sh` + L0+L1+L2 harness + evidence capture pre-teardown — **PASSED iter 55 LIVE**) + (iv) `/dogfood full` on clean local k3d (still pending). AND (b) 1 prod customer day-trial with no incidents (Sprint 9 prod canary). Local 7-day GREEN streak DROPPED as a gate (D7 reformulated 2026-05-03 — tautological in static dev env without diurnal traffic; operator-run drill + real prod usage are the legitimate signals — the GitHub CI cron was removed 2026-05-23). **Audit-grind progress (2026-05-12 to 2026-05-13, full tally in `.claude/knowledge-base/audits/2026-05-12-grind-summary.md` + log.md entries):** 30 of the 27 §6 backlog findings closed inline (some closures addressed multiple sub-items), 4 RFCs/design docs landed for the T2/T3 items, 5 latent bugs captured during code-walks under audit context, 3 operator runbooks/templates/dashboards shipped for Wave 2 onboarding. **Sponsor-direct-approval pattern (2026-05-13):** §6.B.2 + §6.B.3 (orphan CRD deletion, 2 CRDs deleted symmetrically) AND §6.B.5 (service-auth Ed25519 dual-control adoption) both shipped via the new pattern documented in `meetings/MEET-2026-05-13-orphan-crd-deletion.md` + `meetings/MEET-2026-05-13-ed25519-sponsor-approval.md` — sponsor explicit override of the Change Governor meeting after RFC review. Audit gaps M03 #2/#3 + M10 G1 closed (engineering side). Score un-bumped because Rule 11 requires 3 evidence sources (T1 commit+test, T2 LIVE log, T3 CI run) and the customer-trial LIVE evidence is still pending. The two original maturity gates remain the bottleneck.

**Known issues:** Local k3d cert-manager letsencrypt-prod cannot issue for `*.loopzero.local` (no public DNS for ACME challenge) — self-signed CA issuer (`loopzero-local-ca-issuer`) fills local gap, production uses CF-managed TLS so no impact, local Stage 1 canary HTTP probes report HTTP 000 in `--smoke` mode (probes informational only). Local chaos-mesh installs CLEAN in k3d (the prior CrashLoopBackOff/`informer cache sync timeout` was a runtime-socket mismatch — k3s ships containerd at `/run/k3s/containerd/containerd.sock`, not the upstream default; fixed via `infra/helm/chaos-mesh/values-k3d.yaml` socketPath override). Validated 2026-05-23: chaos-mesh 2.7.0 6/6 pods Running on all 3 k3d clusters (mgmt+build+workload); `/chaos-test --smoke` 3/3 PASS (C01 pod-kill-api-leader, C07 gitea-outage, C09 pg-connection-storm) with SLO held + zero-orphan cleanup. Install: `helm upgrade --install chaos-mesh infra/helm/chaos-mesh --kube-context k3d-loopzero-mgmt -n chaos-mesh --create-namespace -f infra/helm/chaos-mesh/values.yaml -f infra/helm/chaos-mesh/values-k3d.yaml`. Loki cross-cluster URL was helmfile-hardcoded to k3d local IP (would silently fail cross-VPC in prod); fixed as of `ef7a7c16` — promtail values now `.gotmpl`-templated per environment, prod points at DOKS internal LB explicitly. ADR claim "auto-blocks via W1.14 hook" for cosign keyless cutover was stale (W1.14 only protects ADR deletion); real deadline ratchet shipped as `infra/scripts/check-cosign-cutover-deadline.sh` + weekly cron in `9249e207`.

---

## Voice and Tone

**Locked 2026-05-15** (strategic review — Option A: 5th and final aspirational-voice formalization in the monorepo). LoopZeroCloud has adopted the aspirational voice originally scoped to LoopZeroKit. The operational guide — three communication layers (HERO / BODY / DEEP DIVE), vocabulary translation, banned terms list, storytelling rules, before/after examples — lives in [`../loopzerokit/CLAUDE.md`](../loopzerokit/CLAUDE.md). Read it before writing any public copy for this project.

**Applies to:**

- `PITCH.md` — landing-page copy at project root
- `loopzero.example` site copy referencing LoopZeroCloud
- Sales-led conversations, launch material, blog posts

**Does NOT apply to (stays technical-direct):**

- `README.md` (the engineering-facing reference — already mixes audiences but leans technical)
- This `CLAUDE.md`, `CHANGELOG.md`, `GOVERNANCE.md`, `SECURITY.md`
- Internal docs (`docs/`, `.claude/knowledge-base/`, ADRs, meetings, plans, audits)
- Source code, comments, error messages, CLI internal output

**Cross-Project Rule 9 — Cloud abstraction (in force regardless of voice):**

LoopZeroCloud hides its infrastructure the same way Vercel hides Lambda/CloudFront. User-facing copy says `loopzero deploy`, `live URL`, `rollback`, `production`, `canary deploys`, `signed container images`, `audit log`. **Never** in user-facing copy: `Kubernetes`, `cluster`, `BuildKit`, `ArgoCD`, `Cosign`, `Argo Rollouts`, `KEDA`, `Gitea`, `MinIO`, `ESO`, `cell`, `Pulumi`, `Helmfile`, `Tilt`, `Ory`, `Traefik`. These terms are allowed **only** in: `README.md` "Architecture" / "Self-Hosted" sections, this `CLAUDE.md`, ADRs, `docs/`, internal runbooks, source code.

The PITCH.md follows CR-9 strictly — internals appear only in the "Architecture" DEEP DIVE block, deliberately abstracted ("the runtime is hidden behind `loopzero deploy`", "GitOps under the hood", "supply-chain by default") so the document remains usable on `loopzero.example` without further redaction.

**Locked Narrative (governed by root `CLAUDE.md`, not this file; refined 2026-05-19 strategic review #2):**

- HERO (root, EN): *"Build AI Agents and Apps faster with LoopZero."*
- Hero tagline (root): *"Chat. Build. Deploy. From idea to live agent in minutes."*
- Sub-headline (root, EN): *"The complete ecosystem to ship AI agents — open-source AI chat, frameworks, and a managed runtime where your agent actually runs. ~4 minutes from start to live. Your code, your repo."*
- LoopZeroCloud-specific HERO (this product, retained as Deploy-layer founding hero): *"Where AI agents actually run."* + *"Type an idea. Get a real backend."* as secondary anchor
- LoopZeroCloud sub-headline (this product): *"The Deploy layer of the LoopZero ecosystem. We build it, ship it, and hand you a live URL — in ~4 minutes."*
- Proof point: *"~4 min deploy · ~3s rollback · 30 frameworks · signed images by default · multi-cell isolation."*
- BODY anchor: *"Chat with LoopZeroCode. Build with LoopZeroCreate + LoopZeroKit. Deploy on LoopZero."*
- Hero pattern: Variant A
- Demo interaction: Demo 1
- Positioning mix: "Vercel + Mastra + Claude Code" (full cycle in one ecosystem; the verbs are Chat / Build / Deploy)

If a piece of LoopZeroCloud copy contradicts the Locked Narrative in [`../CLAUDE.md`](../CLAUDE.md), or the operational rules in [`../loopzerokit/CLAUDE.md`](../loopzerokit/CLAUDE.md), or Cross-Project Rule 9, the root rules win — surface the conflict before publishing.

---

## Development Commands

```bash
task setup               # ONE-SHOT cold start: 4 k3d clusters + Helmfile sync (15-25min)
tilt up                  # DEV LOOP: hot reload of API/operators/CLI (canonical day-to-day)
task dev:up              # Alias: invokes setup + tilt-up reminder (legacy)
# Per T3.2 of plan 2026-05-22: `task setup` is the platform installer.
# `tilt up` is the canonical dev loop. See docs/operations/dev-loop.md.
task doctor              # Environment health check

task test                # All tests
task test:unit           # Unit tests
task test:e2e            # E2E (Ginkgo)
task lint                # golangci-lint + ESLint
task validate:manifests  # kubeconform --strict

cd cli && npm test       # CLI (Jest, 1,260 test cases — 88 suites)
cd api && go test ./...  # API (41 packages)
cd operators && go test ./...  # Operators (Ginkgo + envtest)
cd pkg && go test ./...  # Shared libs

task db:migrate          # Forward-only migrations
task db:reset            # Drop + recreate + migrate + seed

cd infra/pulumi && npx pulumi up --stack dev      # Deploy to DO
cd infra/pulumi && npx pulumi preview --stack dev # Preview
```

**Prerequisites:** Go 1.25+, Node.js 20+, Docker, k3d, Tilt, Task, Helm v3.14+, **helm-diff plugin** (`helm plugin install https://github.com/databus23/helm-diff`), **Helmfile v1.0+** (CHG-2026-04-28-LAYERED-BOOTSTRAP T0.3 — orchestrator L2), kubectl, kubeconform (`go install github.com/yannh/kubeconform/cmd/kubeconform@latest`), golangci-lint v2.11+, cosign (optional, image signing)

---

## The Rules (Non-Negotiable)

1. **Tests before code** — No production code without a failing test. Write the test, run it, confirm it fails, then implement.
2. **Code is authoritative** — Read `CHANGELOG.md`, relevant ADRs, and the codebase itself. Don't invent behavior not present in the code.
3. **No internal terms in user-facing output** — Forbidden in CLI output, error messages, `--json`: `ArgoCD`, `argocd`, `GitOps`, `gitops-push`, `CRD`, `tenant_id`, `advisory lock`, `pg_advisory`, `Gitea`, `MinIO`, `kubebuilder`, `envtest`, `Kaniko`. Use product language: deploy, team, account, service, revert, build.
4. **Exit codes are permanent** — `0` success, `1` usage error, `2` build failure, `3` deploy failure, `4` auth error, `5` partial failure. No others.
5. **`--json` is a stability contract** — Field names cannot change without major version bump. New fields can be added.
6. **Every mutation uses the environment lock** — `withEnvironmentLock` for all `(tenant, project, environment)` mutations. Concurrent-build check MUST be inside the lock (race condition otherwise).
7. **`pg_advisory_xact_lock` only** — Never session-scoped (`pg_advisory_lock`) — breaks with connection pools.
8. **State machines are strict** — Use state machine functions, never direct status updates. Invalid transitions panic (programming error).
9. **CLI is the primary interface** — Every feature lives in CLI before dashboard. Dashboard-only features are product bugs.
10. **Local equals production** — Same Helm charts, same operators, same build pipeline. No Docker Compose shortcuts. No "dev mode" with different behavior.
11. **Claims need evidence** — Any claim affecting maturity score requires **3 evidence sources** (T1 commit+test, T2 LIVE log, T3 CI run). Maturity-claim confidence cap **90-95%** (single-model family correlation residual; originally ratified as ADR-0051 in the governance suite removed 2026-05-11, but the cap remains binding). Note: this cap applies to maturity claims only — the 95%+ implementation confidence rule (`.claude/rules/workflow.md`) is a separate, stricter gate for all code changes. TTL applies: 30d operational / 60d arch / 90d strategic. Latest baseline: `.claude/knowledge-base/audits/2026-05-27/SUMMARY.md` (3.65/4.0; M07 incremental over 2026-05-26 aggregate 3.62/4.0 of 12 module audits). Anteriores: `.claude/knowledge-base/audits/2026-05-26/SUMMARY.md` (3.62/4.0) e `.claude/knowledge-base/audits/2026-05-11/SUMMARY.md` (3.49/4.0). The previous `docs/evidence/{dim}/` per-claim tree was consolidated into the audits folder during the same cleanup.
12. **Tool adoption claims need grep, not memory** — Before classifying any external tool/library as "adopted" or "gap" (comparing against CNCF landscape, kubetools, awesome-lists, or evaluating a new dependency), **grep the repo FIRST**: `grep -ril "<tool>" infra/ charts/ taskfiles/`. Authoritative inventory lives in (a) `infra/helm/<chart>/` (bootstrap charts), (b) `charts/<chart>/` (OCI-published), (c) `infra/helmfile/environments/*.yaml`, (d) `taskfiles/*.yml` (gate/tooling runners — GitHub workflows removed 2026-05-23). **This file is a curated summary, NOT an authoritative tool inventory** — classifying from memory produces false positives. Codified 2026-05-22 after false-positive incident (Velero/Trivy/Polaris/kubeconform/helm-chartsnap/chart-testing/Renovate/TruffleHog misclassified as "gap" when all already adopted). See § Tool Inventory below.

---

## Stack

> **Inventory verified 2026-05-22 via grep** (`infra/helm/` + `charts/` + `infra/helmfile/` + `taskfiles/`; `.github/workflows/` removed 2026-05-23). When CLAUDE.md and the file system disagree, the file system wins — see Rule 12.

### Application Layers

| Layer | Technology |
|-------|-----------|
| CLI | TypeScript + Commander (`cli/`) |
| API | Go 1.25 + chi + pgx (`api/`) |
| Operators | Go 1.25 + kubebuilder + controller-runtime (`operators/`) — 4 split binaries (tenant/app/preview/platform) |
| Shared libs | Go (`pkg/`: errors, errors/http, logging, id, naming, conditions, statemachine, jwt, enforcement, circuitbreaker, healthcheck) |
| Database | PostgreSQL 16 (managed DO prod; chart `infra/helm/postgresql/` self-hosted) |
| Migrations | golang-migrate (forward-only `.up.sql`) |
| Auth | Ory Kratos + Ory Hydra + Redis |

### Platform Charts (`infra/helm/`, 35 dirs — bootstrap via Helmfile L2; measured 2026-05-26)

| Domain | Charts |
|--------|--------|
| Identity/Auth | `ory-kratos`, `ory-hydra`, `redis` |
| Build | `argo-workflows`, `harbor` (auxiliary registry; prod uses DOCR) |
| GitOps | `argocd`, `gitea` |
| Progressive delivery | `argo-rollouts` (Traefik v3 traffic router `traefik.io/v1alpha1`, dashboard disabled, PSA restricted), `keda`, `keda-http-addon` |
| Ingress/TLS | `traefik`, `cert-manager`, `cluster-issuer`, `cloudflared` |
| Storage | `minio`, `nfs-server-provisioner` (with ADR exception) |
| Admission | `admission-policies` (VAPs custom), `policy-controller` (sigstore) |
| Network | `network-policies` (deny-all baseline) |
| Observability | `monitoring` (Prometheus + Grafana), `loki`, `promtail`, `tempo`, `otel`, `blackbox-exporter` |
| Resilience | `chaos-mesh`, `velero` |
| Testing | `k6-operator`, `smoke-test` |
| LoopZero platform | `loopzero-api`, `loopzero-cloud`, `static-assets`, `external-secrets` |

### Published Charts (`charts/`, 13 dirs — OCI registry, ArgoCD L3 + loopzero-common library)

`loopzero-admission-policies`, `loopzero-api`, `loopzero-app-operator`, `loopzero-build-cwts`, `loopzero-cloud`, `loopzero-common`, `loopzero-crds`, `loopzero-dashboard`, `loopzero-monitoring`, `loopzero-platform`, `loopzero-platform-operator`, `loopzero-preview-operator`, `loopzero-tenant-operator`

### IaC + Local Dev

| Layer | Tooling |
|-------|---------|
| L1 (cloud primitives) | Pulumi (TS) — 5 projects: `pulumi/` (DO+CF), `pulumi-infra/` (multi-cluster topology), `pulumi-platform/` (32 Helm releases), `pulumi-drill/` (DR prod-identical 4-cluster stack), `pulumi-shared/` (contracts). `pulumi-control-plane/` deleted 2026-05-15 per close-all-gaps T2.1. Bipartite SoT per MEET-2026-05-12 D3. |
| L2 (platform workloads) | Helmfile v1.0+ — orchestrator de 32 Helm releases. **SoT real desses workloads**, não Pulumi. |
| L3 (tenant Day-2 GitOps) | ArgoCD 2.13.3 (manual sync, from Gitea) |
| Local clusters | k3d × 4 (mgmt, build, workload, runtime-2) |
| Dev loop | Tilt + Taskfile (2929 lines across 7 files) |
| Drill cost | ~$0.32/hr active; auto-destroy watchdog cron every 6h |

### Edge + Registry

| Layer | Tooling |
|-------|---------|
| Edge proxy | Cloudflare Tunnel + Worker `loopzero-static-delivery` + KV `loopzero-routing` + Zone Mgmt |
| Container registry (prod) | DigitalOcean Container Registry (`registry.digitalocean.com/loopzero-system`) |
| Static delivery | DO Spaces + CDN |

### Supply Chain Security

> **NOTE (2026-05-23):** GitHub Actions was removed. The `*.yml` workflow names in the "Where" columns below (build-images.yml, ci.yml, etc.) **no longer exist** — they document the historical tool→runner mapping. The tool configs (cosign keys, syft, Trivy, TruffleHog, policy-controller) remain; re-wire their execution into the real CI or `task quality:*` as needed.

| Tool | Where | What |
|------|-------|------|
| **cosign** | `build-images.yml` + `cosign-rotation-drill.yml` + `cosign-cutover-deadline.yml` | Image signing (keyed; keyless v1.1 with Fulcio committed for cutover) |
| **syft** | `build-images.yml` | SBOM generation (CycloneDX) |
| **Trivy** | `ci.yml` (fs scan) + `build-images.yml` (image scan + **auto-delete vulnerable images from DOCR** on failure post-push) | CVE scanning with `severity=CRITICAL,HIGH`, `exit-code: 1` |
| **TruffleHog** | `ci.yml` | Secret scanner (`--only-verified`) |
| **sigstore policy-controller** | `infra/helm/policy-controller/` | Admission deny-by-default; `loopzero-images-warn` warn-mode ClusterImagePolicy (B16) |

### Quality + Validation

> **NOTE (2026-05-23):** the `*.yml` workflow names in the "Where" column below are historical (GitHub Actions removed). The `pkg/quality/*` gates run via `task quality:*` (`taskfiles/quality.yml`) + `cmd/loopzero-ops quality <gate>`. The validators (kubeconform, Polaris, golangci-lint, ESLint, helm-diff) run via `task` targets locally/operator.

| Tool | Where | What |
|------|-------|------|
| **kubeconform** | `task validate:manifests` + `helm-charts.yml` | K8s manifest schema validation (`--strict`) |
| **Polaris (Fairwinds)** | `scripts/run-polaris.sh --severity=danger` | K8s best-practices linter (DANGER bloqueia merge) |
| **chart-testing (ct)** | `helm-charts.yml` matrix multi-K8s-version | Helm chart lint + install test |
| **helm-chartsnap** | `helm-charts.yml` | Snapshot testing to detect chart output drift |
| **helm-diff** | `infra/scripts/helm-diff-structural.sh` + helmfile | Pre-sync diff (mandatory plugin) |
| **golangci-lint v2.11+** | `ci.yml` + `task lint` | Go lint |
| **ESLint** | `cli/` package.json | TypeScript lint |
| **`pkg/quality/*` gates** | `task quality:*` (`taskfiles/quality.yml`) + `cmd/loopzero-ops quality <gate>` | 40+ pure-logic gates (unhomed-logic, no-new-bash, dangling-refs/data, prod-image-tags, no-migrate-force, etc.) |
| **Renovate** | `.github/renovate.json` | Dependency mgmt with `pinDigests: true` (INF-G18 SHA pinning), weekly grouped PRs |

### CI/CD

| Layer | Tooling |
|-------|---------|
| Pipeline | **None via GitHub** — GitHub Actions removed 2026-05-23 (operator decision: not the CI/CD platform). Quality gates run via `task quality:*` (`taskfiles/quality.yml`) + `cmd/loopzero-ops quality <gate>`, locally/operator-run or callable by the real CI when chosen. |
| Build engine | Argo Workflows + BuildKit (rootless single-container) + Docker buildx |

### Known Gaps (CONSCIOUS NO — see § Tool Inventory and audit 2026-05-22)

- ~~Cost attribution per tenant (OpenCost/Kubecost)~~ **ADOPTED 2026-05-22 (plan T7.2)** — wrapper at `infra/helm/opencost/` + helmfile release with `needs: monitoring/prometheus` + ServiceMonitor at `infra/helm/monitoring/manifests/servicemonitor-opencost.yaml` + runbook `docs/operations/cost-attribution.md`. LIVE validation (pod Running + Prometheus scrape) operator HARD STOP.
- Pluto (deprecated API detector) — **enforcement REVERTED 2026-05-23**: the `pluto-deprecated-apis.yml` workflow + `pkg/quality/plutogate` were deleted with the GitHub Actions removal (the gate only validated that workflow). Only the runbook `docs/operations/k8s-upgrade-runbook.md` survives. Re-home as a `task quality:*` check before the next DOKS minor upgrade if still wanted.
- Slack alerting destination (Botkube/Robusta) — Prometheus alerts exist; routing missing
- K8sGPT, Falco/Tetragon, Kubescape — deferred to v1.x

---

## Repository Structure (Brief)

```
loopzero/
├── docs/                  ← audits/, plans/, strategy/. Removed in 2026-05-11: cli-reference.md, STATUS.md, evidence/, architecture/ (empty placeholder), meetings/ (empty placeholder), runbooks/ (empty placeholder). Spec is the code itself.
├── pkg/                   ← shared Go: statemachine, errors, errors/http, jwt, naming, conditions, id, logging, enforcement, circuitbreaker, healthcheck
├── cli/                   ← @loopzero/cli — TS + Commander, 66 registered commands across 40 command files, 1,260 test cases (88 suites; measured 2026-05-26)
├── api/internal/          ← 33 subpackages (measured 2026-05-26): adapters, admission, archive, argo, argocd, cellmgr, cfkv, cfzone, config, correlation, db, domain, gitclient, github, gitops, health, k8s, leader, lock, loki, manifests, metrics, middleware, placement, processing, routes, s3, server, services, shellgen, source, sse, tracing, validation, workqueue
├── operators/             ← 8 controllers (Tenant, Environment, Application, Exposure, PlatformBootstrap, GitRepositoryBinding, ClusterCredential, Preview), 8 CRDs (all wired). The 2 historical orphan CRDs (AppSecret + ReconciliationReport) deleted 2026-05-13 per ADR `decisions/2026-05-13-orphan-crd-deletion.md`.
├── infra/
│   ├── pulumi/            ← IaC (TS), providers: digitalocean/, cloudflare/, aws/(stub)
│   ├── helm/              ← 16 custom charts + 49 value files across 34 chart dirs (custom + upstream overrides)
│   ├── k3d/               ← 4 cluster configs (mgmt, build, workload, runtime-2)
│   ├── cloudflare/        ← CF Worker source (worker.mjs)
│   └── tests/             ← e2e/ (Ginkgo), chaos/, unit/, load/
└── .github/             ← NO workflows. All 51 workflows + 4 composite actions deleted 2026-05-23 (operator: GitHub Actions is not the CI/CD platform). Remaining files are non-CI: CODEOWNERS, PULL_REQUEST_TEMPLATE.md, renovate.json, branch-protection-config.yml. Quality gates re-homed to `taskfiles/quality.yml` (`task quality:*`).
```

Use `Glob` / `git ls-files` for current details.

---

## Tool Inventory (Authoritative — for Rule 12 verification)

This project consciously chose to adopt OR reject specific tools. The **stack inventory above is the source of truth** for "what's installed"; this section documents the **rejected/deferred** set so we don't re-evaluate the same decisions every sprint.

### Rejected by design (do NOT propose adoption without ADR)

| Category | Rejected | Reason |
|----------|----------|--------|
| Multi-tenancy | vCluster, Kamaji, KubeZoo, KubeStellar | LoopZero uses namespace + cell isolation (ADR-0008/0021); sticky placement, no auto-rebalance |
| Service Mesh | Istio, Linkerd, Kong, Gloo Mesh, OSM, APISIX | KISS: Traefik standalone; apps comunicate via public API; mesh adds latency without gain |
| CNI advanced | Cilium, Calico advanced features | NetworkPolicy basics suffice for cell isolation; eBPF maturity bar too high |
| FaaS | Knative, OpenFaaS, Fission, Kubeless | LoopZero is PaaS for containers, not FaaS |
| ML/AI workloads | Kubeflow, Volcano, Kuberay | Out of scope for LoopZeroCloud GA |
| IaC alternatives | Crossplane, Terraform direct | Pulumi (TS) chosen — familiarity advantage |
| GitOps alternatives | Flux, PipeCD, Tekton | ArgoCD chosen — UI + multi-cluster + manual sync model |
| Edge | KubeEdge, OpenYurt, SuperEdge | Regional cells in DO, no edge computing |
| Federation | Karmada, Kubeadmiral | DynamicRouter owns placement; federation auto-rebalance forbidden |
| Secret encryption | Sealed Secrets | ESO scales better for multi-tenant secret sync |
| Node autoscaling | Karpenter | DOKS managed nodepool; Karpenter is AWS-first |
| Policy engine | Kyverno | sigstore policy-controller + VAPs + Polaris suffice; revisit only if >3 policies accumulate |
| DB operator | KubeBlocks, CNPG, Crunchy | DOKS managed PG in prod; standalone Helm chart locally |

### Deferred to v1.x (revisit post-GA)

| Tool | When to revisit |
|------|------------------|
| ~~**OpenCost**~~ | **ADOPTED 2026-05-22** via plan T7.2 — see `infra/helm/opencost/` + `docs/operations/cost-attribution.md` |
| **Pluto** | Enforcement REVERTED 2026-05-23 (workflow + `plutogate` deleted with GitHub Actions). Re-home as `task quality:*` before next DOKS minor upgrade. |
| Falco / Tetragon | Enterprise tier compliance ask |
| Kubescape | After kube-bench wins evaluated |
| HolmesGPT | After K8sGPT validates value |
| Karpor / Pixie / DeepFlow / Alaz (eBPF obs) | v2.x when cells > 10 |
| Kueue | If batch/HPC workloads added |
| Litmus | Not adopted — Chaos Mesh covers chaos engineering |

### How to use this section

- **Before proposing a new tool:** check this table. If rejected, the burden of proof is on you (write ADR explaining why constraints changed).
- **Before classifying a tool as "gap":** grep the repo per Rule 12. If the grep returns matches in `infra/helm/`, `charts/`, or `taskfiles/`, it's already adopted — refresh this file.

---

## Knowledge Base (`.claude/knowledge-base/`)

Persistent wiki that stores **WHY** — decisions, trade-offs, rationale, and architectural patterns that cannot be derived by reading code alone. The code answers "what does it do?"; the knowledge-base answers "why was it built this way?".

### Structure

```
knowledge-base/
├── schema.md              ← Contract: page types, frontmatter, operations
├── index.md               ← Catalog of all pages (read first)
├── log.md                 ← Chronological record (append-only)
├── overview.md            ← Current project state summary
├── architecture/          ← C4 diagrams (Four Planes)
├── concepts/              ← SOTA research by domain (15 domains)
│   ├── multi-cell/        ← Placement, routing, DynamicRouter
│   ├── progressive-delivery/ ← Canary, Argo Rollouts, TraefikService
│   ├── state-machines/    ← 11 machines, framework design
│   └── ... (12 more)
├── audits/                ← Modular audits by date
├── decisions/             ← Formal ADRs
├── plans/                 ← Implementation plans
├── strategy/              ← Strategic analyses (CAP, pricing, resilience)
├── reviews/               ← Dogfood and cross-validation results
└── meetings/              ← Technical meeting records
```

### When to Consult

- **Before implementing**: read relevant `concepts/` page to understand prior decisions and rejected alternatives
- **Before changing state machines, multi-cell, or progressive delivery**: these have SOTA research pages with documented trade-offs
- **When asked "why?"**: check `decisions/` for ADRs and `concepts/` for rationale

### When to Update

- **After completing a task** that changes architecture (not cosmetics) — triggered by `[DOC-DRIFT]` hook warnings
- **Max 3 pages per task** (anti-thrashing rule)
- **Never auto-generate** as side effect of coding — doc updates are explicit, conscious decisions
- Full rules: `.claude/rules/auto-docs.md`

### Page Types

| Type | Purpose | Example |
|------|---------|---------|
| Entity | Named component/service/chart | DynamicRouter, LoopZeroApplication CRD |
| Concept | Pattern or technique | Placement Once Route Many |
| Comparison | A vs B | Deployment vs Rollout |
| Decision | Formal ADR | T3-6 State Machines |
| Synthesis | Cross-source analysis | CAP LoopZerorem Analysis |
| Audit | Modular audit result | 01-cli.md (3.5/4.0) |

Every page requires YAML frontmatter (`type`, `created_at`, `updated_at`, `updated_by`). Evidence must be classified: `[MEASURED]`, `[REPORTED]`, or `[ESTIMATED]`. Full schema: `.claude/knowledge-base/schema.md`.

---

## Agents

Claude operates as a team of specialists. Sequence work across agents for multi-component tasks.

- **orchestrator** — entry for multi-component tasks. Reads spec, breaks into subtasks, delegates, validates.
- **agent:cli** — owns `cli/`. Bar: Vercel CLI quality. Required: `--json` schema, exit codes, CI mode (`CI=true`), no internal terms, error messages suggest next action, unit tests cover all paths.
- **agent:api** — owns `api/`. Required: `withEnvironmentLock` on mutations, `Idempotency-Key` middleware, typed errors (`LoopZeroError`), `Correlation-ID` forwarded, component test for success + 409 + idempotency replay + concurrent rejection.
- **agent:operator** — owns `operators/`. Required: Ginkgo + envtest spec written first, idempotent reconcile, crash-mid-reconcile resumes correctly, state via state machine only, leader election enabled.
- **agent:infra** — owns `infra/`. Standards: idempotent Taskfile, `kubeconform --strict` passes, resource limits + readiness/liveness probes + non-root, no `latest` image tags.
- **agent:test** — writes specs before implementation. Critical tests that MUST exist: concurrent deploys (1×202+1×409), idempotency replay, lock release on crash, invalid transition panic, exit codes, JSON shape, no internal terms.
- **agent:pipeline-reviewer (Helena Duarte)** — Argo Workflows / CI/CD. Reviews: determinism (same commit = same artifact), build/release separation, scale (100 concurrent), observability, recovery (idempotent retries).
- **Meeting Review Board** — 9 Staff/Principal review agents + orchestrator for critical decisions. Never write code. Security/SRE have absolute veto. Min 3 agents per meeting.

Behavioral and governance rules for all agents: see `.claude/rules/` (workflow, TDD, CLI output, design principles, pre-release governance, LoopZero design).

Skills/slash commands (subset): `/sprint`, `/implement`, `/test`, `/review`, `/design-check`, `/failure-check`, `/schema-check`, `/state-machine`, `/dogfood`, `/sre-guardian`, `/pipeline-review`, `/meeting`, `/start-loop`, `/cancel-loop`. Full list injected by harness.

---

## Pre-Completion Checklist

```
[ ] task test:unit passes
[ ] task lint passes
[ ] task validate:manifests passes
[ ] No internal terms in CLI output (Rule 3)
[ ] Mutating API routes have withEnvironmentLock
[ ] Mutating API routes handle Idempotency-Key
[ ] CLI --json matches spec schema
[ ] Exit codes are 0/1/2/3/4/5 only
[ ] State machine changes match spec
```

**Bash blocked:** `kubectl delete` (without dry-run), `k3d cluster delete`, `DROP TABLE/COLUMN`, `git push --force` to main, `helm uninstall` on postgresql/gitea/minio.

---

## Sprint Plan

| Sprint | Focus | Status |
|--------|-------|--------|
| 0 | Foundation | ✅ |
| 1 | Login + Init + TenantController | ✅ |
| 2 | Deploy happy path | ✅ (E2E on DO) |
| 3 | Logs + Status + Secrets | ✅ (H6) |
| 4 | Rollback + Promote + Cancel | ✅ (E2E 2026-04-23) |
| 5 | Preview environments | ✅ ALL 14 tasks done. E2E validated live (12 specs pass against k3d). |
| 6 | Security + Supply chain | ✅ cosign + syft SBOM + policy-controller installed (CHG-2026-04-26-SPRINT6-IMPL). policy-controller is **deny-by-default**; warn-mode `loopzero-images-warn` ClusterImagePolicy is required (B16). As of CHG-2026-04-28-LAYERED-BOOTSTRAP T0.1 the policy is shipped as a Helm chart in `infra/helm/admission-policies/` (templates/cluster-image-policy.yaml). Deny-flip + cosign keyless verification pending Sprint 9 with Fulcio. |
| 7 | Observability | ✅ (H4) |
| 8 | Hardening + Chaos | 🟡 Phase -1 + Phase 0 + Phase 2 GREEN (CHG-001 + CHG-002a, MEET-060 + MEET-061, ADR-0015 + ADR-0016). Phase 1+3+4+5+6 → CHG-002b/c new meeting required |
| 9 | Polish + Launch | 🟡 Gates pending: (a) operator-run prod-identical drill GREEN (GitHub cron removed 2026-05-23 — runs via `task drill:l2-scale`), (b) 1 prod customer day-trial. Preparatory work (dogfood fixes, progressive delivery, build mode dispatch) complete in prior sprints. |
| 10 | Static Frontend Delivery | ✅ |
| 11 | Auth + Billing | ✅ |
| H1-H7 | Hardening themes (runtime, TLS, PG HA, observability, pipeline, ops, workload security). Note: H12-H18 referenced in Multi-Cell section are cleanroom heal entries, not hardening sprints. | ✅ all 7 themes complete |
| Cleanroom Dogfood Loop | Real LoopZeroCloud test: destroy infra + setup + 12 journeys end-to-end | ✅ 12/12 GREEN (2026-05-05, tag `dogfood-cleanroom-converged-20260505`). 39 bugs B1-B39 fixed canonically. |
| Argo Rollouts Progressive Delivery | Canary deploys (per-plan steps + Traefik weighted routing + AnalysisTemplate + KEDA targets Rollout) | ✅ 2026-05-07 — 15 plan tasks (T0.1-T5.2) complete. Warm-infra journey **80/80 GREEN in 41m41s**, fresh-cleanroom journey **71/72 GREEN in 42m49s** (1 unrelated ESO timing fail). 6 fixes: F-17 (fixture), F-18 (progressDeadlineSeconds=300), F-19 (S1 sync_client.go accepts Rollout), F-20 (S2 e2e scaffolding), F-21 (Taskfile bootstrap selector includes argo-rollouts), F-22 (S1 ClusterRole + argoproj.io RBAC). |
| Build Mode Dispatch + User Dockerfile Security | Strategy-pattern dispatch for `build: dockerfile` opt-in + dockerfile-lint security barrier | ✅ 2026-05-08 — 4 phases complete (T0-T4). Restored `LanguageStrategy.DockerfileStep` (was removed in MEET-2026-03-29-017); new `UserDockerfileStrategy` distinct from `CustomStrategy`; `ResolveStrategyForApp(AppSpec)` dispatches `build: dockerfile` apps to UserDockerfileStrategy. New `loopzero-dockerfile-lint` binary (Go, `tools/loopzero-dockerfile-lint/`) enforces hard rejection rules: FROM allow-list (docker.io/library/*, ghcr.io/*, DOCR, k3d-loopzero-registry, public.ecr.aws/, gcr.io/, mcr.microsoft.com/, quay.io/, opt-in via LOOPZERO_DOCKERFILE_ALLOWED_REGISTRIES), --privileged/--security=insecure RUN flags, absolute/escaping bind mounts, reserved env keys (LOOPZERO_*/KUBERNETES_*/AWS_*/DO_*), seccomp/apparmor unconfined. Soft warnings: USER root in final stage, ADD remote URL, apt without --no-install-recommends, HEALTHCHECK without CMD. loopzero-packs v0.4.0 canonical lenient contract published. loopzero-api uses `dockerfileStepTemplateName(strategy)` to wire DAG correctly (no hardcoded "generate-dockerfile"). LIVE: 25/25 lint unit tests + 17 strategy/dispatch tests + e2e `journey_user_dockerfile_test.go` 3/3 GREEN (T4.3 happy + T4.4 rejection). Full journey suite 74/75 PASS in 45m45s (1 J03 ESO timing flake unrelated). |
| Dogfood 2026-05-08 Fixes | F-1..F-7 closed: golang-migrate canonical, ESO ExternalSecret instrumentation+fix, build_events dead code, Application phase fast-path Healthy, OTel local off, seed cell_id, test scaffolding cleanup | ✅ 2026-05-08 — 14 plan tasks across 5 phases. Plan v1.1 with edge-case review (12 edges, 5 MUST FIX folded into tasks). Root causes identified LIVE via instrumentation (e.g., F-2 was runtime.kubeconfig context name drift between F-11 and B34). 9 commits. F-2 ExternalSecret CR Synced=True LIVE; F-6 Application phase Progressing→Serving LIVE; F-1 schema_migrations populated version=10 dirty=false. Heal H17 (schema_migrations backfill) + H18 (stale gitea-credentials-template) codified. Final journey 81/82 PASS (98.8%, single fail = pre-existing T4.3 deploy race outside plan scope). LP-009 added to local-prod divergence matrix. |
| ArgoCD Sync Cleanroom Resilience | F-01 (S0) + F-02 (S2) from /dogfood full 2026-05-08: ArgoCD repo-server timeout overrun + duplicate Gitea credentials | ✅ 2026-05-08 — Plan v1.1 with T2.1 NO-OP amendment (CLI deploy.sync_failed exit 3 path already implemented). T1.1: `ARGOCD_APPLICATION_CONTROLLER_REPO_SERVER_TIMEOUT_SECONDS=60` in bootstrap chart (4× default 15s) — production chart UNCHANGED. T1.2: heal H18 extended to delete `argocd-repo-gitea-internal` Helm-stub conflicting with canonical `argocd-creds-gitea`. **Cleanroom journey LIVE 83/83 PASS in 44m27s** (vs pre-fix 25/35 with 10 FAIL — full warm-infra baseline restored). Zero `PLATFORM_DEGRADED` + zero `application refresh deadline exceeded` post-fix. |

**E2E Validation (2026-04-23):** 25/30 pass (83%). Confidence: deploy 99%, scaling 95%, promote 95%, rollback 99%, concurrent rejection 99%, JSON contracts 99%, internal terms (Rule 3) 99%. Log streaming 60% (infra gap, not code).

**Maturity:** Latest snapshot **3.65/4.0** (M07 incremental re-audit 2026-05-27 via plan derivado M1 done; aggregate baseline 12 módulos 3.62/4.0 from 2026-05-26). Active plans: `docs/plans/completed/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md` v1.1 (target ≥3.95; ceiling 3.983 per ADR D3 — M11 #25 string-aliases cap pkg+state-machines at 3.9) + `docs/plans/2026-05-27-M07-tenant-build-pipeline-to-3-85-cap-plan.md` v1.1 (M1 done 3.65; M2 → 3.85 gated por TUF ceremony + Sigstore activation LOCAL k3d, operator HARD STOP). Engineering side 19/27 tasks done autonomously across 4 Ralph iterations (2026-05-27). Path to 4.0: (a) operator-run prod-identical drill GREEN, (b) 1 prod customer day-trial. Snapshot anterior: `.claude/knowledge-base/audits/2026-05-11/SUMMARY.md` (3.49/4.0). **M07 module-specific:** **3.85/4.0** (LIVE keyless GREEN registered 2026-05-28, audit `.claude/knowledge-base/audits/2026-05-28-keyless-GREEN/07-build-pipeline.md`). Plan `docs/plans/2026-05-28-meet-001-engineering-closure-plan.md` v1.1 (MEET-001 engineering closure) COMPLETED 2026-05-29: 18 tasks T0.1→T4.6 + 27 commits durável, all 11 CRITICAL findings (CR-1..CR-11) closed engineering-side, plus T1.3 follow-up regression caught + fixed mid-Final-Phase. Final Phase Dogfood QA on cleanroom-fresh 4-cluster k3d fully validated: dogfood **103 RUNNABLE specs PASS / 0 functional FAIL** (102 main + 1 focused TIMEDOUT re-run / 29 precondition-skipped — audit `.claude/knowledge-base/audits/2026-05-29-dogfood-cleanroom-fresh-evidence.md`) + chaos-test **--full 10/10 PASS in 52min** SLO held entire chaos window (audit `.claude/knowledge-base/audits/2026-05-29-chaos-test-full-cleanroom-fresh-evidence.md`). M07 prod cutover BLOCKED per MEET-2026-05-28-001 (sponsor accepted); operator HARD STOPs for 3.90+: TUF ceremony + Pulumi sigstore.ts prod stack up + drill iter ≥56 + 1 customer day-trial.

---


## Deep Review Roadmap (2026-06-01)

> **Source**: Deep Review Loop completo `/home/paulo/loopzero/api/review-output/final_report.md` — 83 findings (2 critical + 9 high + 36 medium + 36 low), 10 threat models, 7 invariants validated, C4 composite **3.15 / 4.0**. Ordem por impacto×esforço×blast-radius. Cada sprint termina com trio FULL re-validation (cleanroom + dogfood --full + chaos-test --full) — padrão V9 L17-L22 + L22-FAANG `098b6846`. Memory: `reference_deep_review_roadmap_2026_06_01.md`.

### ✅ Sprint A — P1 HOTFIXES — CLOSED 2026-06-03 (todos 4 itens já LIVE-shipped antes desta sessão; verified via git log + unit tests PASS)

Fecha 3 dos CR ainda abertos de MEET-2026-05-28-001 (CR-1/CR-6/CR-7) + 1 CRITICAL ativo.

| # | Finding | Esforço | File |
|---|---------|---------|------|
| ~~A1~~ | ✅ `comp-001` Wire `SecretEnvLocker` em setup_server.go — **SHIPPED commit `5e777c39`** (test: TestSecretSet_AcquiresEnvLock PASS) | 1 linha | `api/cmd/setup_server.go:82` + `api/internal/server/router.go:298` |
| ~~A2~~ | ✅ `sec-008` LOOPZERO_ENV allowlist em `Validate()` — **SHIPPED commit `6f35bdcb`** (rejects `Production`/`prod`/case-variants AT STARTUP via `isValidEnv()`; IsProduction() pode manter strict equality porque Validate() é EARLY gate; tests PASS) | — | `api/internal/config/config.go:462+482` |
| ~~A3~~ | ✅ `sec-009` BEFORE UPDATE trigger em `image_attestation_rekor_url` write-once + AFTER INSERT audit trigger — **SHIPPED commit `87ab3de4`** (migration 000020, NOT 000022 as roadmap predicted; 6 integration tests covering raise EXCEPTION + idempotent retry + first-set-from-null + audit_log + tx rollback) | migration 000020 | `api/internal/db/migrations/000020_attestation_rekor_url_write_once.up.sql` |
| ~~A4~~ | ✅ `sec-010` Per-tenant Fulcio SA audience `sigstore-<tenantID>` em production/staging (legacy `sigstore` em dev) + fail-closed sentinel `sigstore-MISSING-TENANT` — **SHIPPED commit `3bcad85f`** (audience_test.go PASS, wired at workflow_builder.go:550) | L effort | `api/internal/argo/workflow_builder.go:575` (`audienceFor`) |

### 🟠 Sprint B — P2 ARCH/CODE (~3-5 dias)

| # | Finding | Esforço | File |
|---|---------|---------|------|
| B1 | `arch-016` Add `ForceReconcile/GetSyncRevision/TerminateOperation/GetAppStatus` à `SyncClient` interface formal (L22 root-cause class — interface under-spec causa silent type assertion failures) | M | `api/internal/services/build_orchestrator.go:154` |
| B2 | `code-001` Promote orchestrator Lock2 re-verify (mirror rollback pattern, linha 210-219) | M | `api/internal/services/promotion_orchestrator.go:214-236` |
| B3 | `code-005` Env-name regex tighten (proibir hyphens via validation, não comentário) | S | `api/internal/routes/builds.go` |
| B4 | `val-002` Correlation-ID header canonical (`X-Correlation-ID` consistente em 3 routes) | S | `routes/builds.go`, `routes/promote.go`, `routes/rollback.go` |

### 🟡 Sprint C — INFRA/CI (~1 semana)

| # | Finding | Esforço | File |
|---|---------|---------|------|
| C1 | `infra-001` Selecionar plataforma CI (Gitea Actions natural — self-hosted, already in stack). T2 meeting required. | L | T2 meeting + ADR |
| C2 | `infra-006` RLS owner bypass — policy `loopzero_service_role` para background workers | M | new migration |
| C3 | `infra-007` Partial unique index `(tenant_id, project_id, environment) WHERE status='ACTIVE'` em releases | S | new migration |
| C4 | `infra-003` Anonymous-auth gate cobrir `infra/k3d/*.yaml` (atualmente só checa Helm/Helmfile) | S | `pkg/quality/anonymousauth/check.go` |

### 🟢 Sprint D — TEST DEBT + SECRETS HARDENING

| # | Finding | Esforço | File |
|---|---------|---------|------|
| D1 | L19 regression test (secret PK collision em envs) — fix está lá mas sem test | S | `api/internal/db/secret_wrapper_test.go` |
| D2 | L20 regression test (secret resync on deploy) — fix está lá mas sem test | S | `api/internal/services/secret_resyncer_test.go` |
| D3 | `sec-001` AES-GCM AAD binding migration (não-breaking via dual-decrypt window) — cross-tenant ciphertext transplant possível sem AAD | XL | `api/internal/db/secret_wrapper.go:59` |

### Anti-padrões inquebráveis (lições V9 + L22)

1. **NÃO fazer Sprint A+B+C juntos** — L22 5-layer composition (`098b6846`) foi difícil de revisar; commits pequenos sequenciais melhor.
2. **NÃO atacar 36 LOW primeiro** — backlog. Sprints A-D removem maior parte do risco real.
3. **NÃO pular trio entre sprints** — dogfood/chaos pristine 2× foi a confiança que liberou push do `098b6846`. Mesma disciplina aqui.
4. **NÃO promise compositoria** — emitir promise apenas quando trio FULL GREEN entre cada sprint.

### Path to Sprint 9 (Polish + Launch)

- ✅ Sprint A FECHADO 2026-06-03 — fechou CR-1/CR-6/CR-7 (3 dos 11 originais de MEET-2026-05-28-001). Verified via git log + unit tests this session. Final_report.md (Jun 1) está stale relativo ao código LIVE — todos os 4 itens já foram shipped por iterations anteriores do Plano A P1 security hardening (commits `5e777c39` + `6f35bdcb` + `87ab3de4` + `3bcad85f`).
- Pós-Sprint A: **8/11 CRs fechados**; remaining 3 são operator HARD STOPs (TUF ceremony + Sigstore Pulumi stack + customer trial).
- C4 Health pós-Sprint D estimado **3.65+** (alinhado com aggregate baseline 2026-05-27).
- Trio re-run mandatório entre Sprint A→B→C→D.


## Architecture Overview

### Four Planes

```
DEVELOPER PLANE   — CLI (TypeScript) ↔ API (Go)
CONTROL PLANE     — mgmt cluster: 8 operators · Gitea · PG · MinIO · ArgoCD · Traefik · cert-manager · Ory Kratos · Ory Hydra · Redis
BUILD PLANE       — build cluster: Argo Workflows + BuildKit + cosign signing
WORKLOAD PLANE    — runtime cluster: user apps via ArgoCD · Traefik + cert-manager + KEDA + Argo Rollouts (canary)
```

### Deploy Flow (Happy Path)

```
loopzero deploy → CLI validates → upload bundle → API locks env → submit Argo Workflow
  → build (BuildKit) → cosign sign → push image → generate manifests
    (Rollout + 2 Services + TraefikService + AnalysisTemplate when ProgressiveDelivery=true,
     legacy Deployment when LOOPZERO_PROGRESSIVE_DELIVERY=false)
  → push to Gitea (gitops) → ArgoCD sync → Argo Rollouts orchestrates canary steps
    (per-plan: Starter setWeight 50→100, Pro 20→Analysis→50→100, Team/Enterprise 10→Analysis→30→60→100)
  → SSE rollout.step events stream during canary; rollout.aborted (exit 3) on Degraded/Aborted
  → app Healthy → SSE deploy.completed → CLI shows URL
  (static apps: build → upload S3 → write CF KV → CF Worker serves from DO Spaces)
```

### CRDs (8 total, all wired)

Wired (✅): Tenant, Environment, LoopZeroApplication, ExposedRoute, PlatformBootstrap, GitRepositoryBinding, ClusterCredential.
Wired (✅): PreviewEnvironment — namespace + NetworkPolicy (deny-all/allow-ingress/allow-egress) + ResourceQuota + LimitRange + TTL + condition-wait flow (BuildSubmitted→BuildCompleted→WorkloadHealthy with grace period) + Failed transition + cleanup retry/CLEANUP_FAILED + finalizer + canonical naming via `naming.PreviewNamespace`. 12 envtest specs covering full lifecycle. E2E live validated against k3d.

The 2 historical orphan CRDs (AppSecret + ReconciliationReport) were deleted 2026-05-13 per ADR `decisions/2026-05-13-orphan-crd-deletion.md` — sponsor-approved T2, replacing the 2 PROPOSED RFCs `2026-05-12-appsecret-rfc.md` + `2026-05-12-reconciliationreport-rfc.md`. Both CRDs had been intentionally unregistered in the scheme (no controller, zero production touchpoints); the secret-sync path goes through ESO directly; drift-detection is provided ad-hoc via ArgoCD sync status + manual `kubectl diff` (no production gap surfaced).

### State Machines (11 in `pkg/statemachine/`)

All states shown below — not just happy path. Terminal states in **bold**. Code is authoritative (`pkg/statemachine/*.go`).

| Machine | All States | Happy Path | Failure/Recovery States | Timeouts |
|---------|-----------|------------|------------------------|----------|
| Build | QUEUED, RUNNING, RELEASING, **SUCCEEDED**, **FAILED**, CANCELLING, **CANCELLED** | QUEUED→RUNNING→RELEASING→SUCCEEDED | RUNNING→FAILED, CANCELLING→CANCELLED | QUEUED>5m, RUNNING>30m, RELEASING>10m, CANCELLING>60s |
| Release | READY, PENDING, ACTIVE, GIT_MIRROR_FAILED, SYNC_FAILED, DRIFT_ALERT, SUPERSEDED, ROLLING_BACK, ROLLED_BACK, ROLLBACK_FAILED, **CANCELLED** | READY→PENDING→ACTIVE→SUPERSEDED | GIT_MIRROR_FAILED→PENDING (retry), SYNC_FAILED→DRIFT_ALERT→ACTIVE (recovery), ACTIVE→ROLLING_BACK→ROLLED_BACK, ROLLBACK_FAILED→ACTIVE (re-attempt) | READY>5m, PENDING>10m, ROLLING_BACK>2m |
| Tenant | (empty), PROVISIONING, ACTIVE, SUSPENDED, PENDING_DELETION, **DELETING** | PROVISIONING→ACTIVE | ACTIVE→SUSPENDED→ACTIVE (reactivate), ACTIVE→PENDING_DELETION→DELETING (soft delete grace) | — |
| PlatformBootstrap | (empty), PENDING, CONFIGURING_GITEA, CONFIGURING_ARGOCD, WAITING_FOR_API, **BOOTSTRAP_COMPLETE**, **FAILED** | PENDING→CONFIGURING_GITEA→CONFIGURING_ARGOCD→WAITING_FOR_API→BOOTSTRAP_COMPLETE | Any step→FAILED (retry exhausted) | — |
| Preview | CREATING, ACTIVE, EXPIRING, **DELETED**, FAILED, DELETING, CLEANUP_FAILED | CREATING→ACTIVE→EXPIRING→DELETED | CREATING→FAILED→DELETING→DELETED, EXPIRING→CLEANUP_FAILED→DELETED | TTL-based |
| Promotion | VALIDATING, COMMITTING, SYNCING, **COMPLETED**, **REJECTED**, **COMMIT_FAILED**, **SYNC_FAILED** | VALIDATING→COMMITTING→SYNCING→COMPLETED | VALIDATING→REJECTED, COMMITTING→COMMIT_FAILED, SYNCING→SYNC_FAILED | — |
| Rollback | VALIDATING, REVERTING, SYNCING, **COMPLETED**, **REJECTED**, **BLOCKED**, **REVERT_FAILED**, **SYNC_FAILED** | VALIDATING→REVERTING→SYNCING→COMPLETED | VALIDATING→REJECTED/BLOCKED (NoPreviousRelease), REVERTING→REVERT_FAILED, SYNCING→SYNC_FAILED | — |
| Domain | PENDING_VERIFICATION, VERIFIED, PENDING_NS, NS_ACTIVE, PROVISIONING, ACTIVE, DEGRADED, FAILED, **REMOVING** | v1: PENDING_VERIFICATION→VERIFIED→PROVISIONING→ACTIVE. v2: PENDING_NS→NS_ACTIVE→PROVISIONING→ACTIVE | PROVISIONING→FAILED→VERIFIED/NS_ACTIVE (retry), ACTIVE→DEGRADED→ACTIVE (recovery), DEGRADED→FAILED | — |
| Environment | (empty), PENDING, ACTIVE, FAILED, **DELETING** | PENDING→ACTIVE→DELETING | PENDING→FAILED→PENDING (retry) | — |
| Application | (empty), PROGRESSING, SERVING, REACHABLE, DEGRADED, DOWN, **CLEANED** | PROGRESSING→SERVING→REACHABLE | REACHABLE↔DEGRADED (cyclic), DOWN→PROGRESSING (recovery), any→CLEANED (Orphaned event) | — |
| ExposedRoute | (empty), PENDING, CERT_ISSUED, REACHABLE, FAILED | PENDING→CERT_ISSUED→REACHABLE | CERT_ISSUED/REACHABLE→FAILED→PENDING (retry). No terminal state (GC via ownerRef). | — |

### Helm Chart Dual System

Two chart systems coexist with distinct purposes (ADR D1, bootstrap-integrity-fixes-plan):

| System | Path | Purpose | Used by |
|--------|------|---------|---------|
| **Bootstrap charts** | `infra/helm/` | Lightweight, inline values, direct install | Helmfile L2 (`infra/helmfile/`) |
| **Published charts** | `charts/` | OCI registry, loopzero-common library, full RBAC/monitoring | ArgoCD L3 (Day-2 GitOps), CI (`helm-charts.yml`) |

- `infra/helm/` is canonical for `helmfile sync` (bootstrap). Charts are simple, standalone, with hardcoded defaults.
- `charts/` is canonical for OCI publishing and ArgoCD. Charts use `loopzero-common` library for consistent labels and security contexts.
- **3 charts overlap functionally** (loopzero-api, loopzero-cloud, loopzero-admission-policies) — when patching one, apply the same fix to the other. `monitoring` / `loopzero-monitoring` are complementary layers (infra vs config), NOT overlapping.
- **admission-policies**: Both systems use `.Values.vaps.*` for VAP toggles and `.Values.registries.*` for registry prefixes (unified 2026-05-06, helm-chart-drift-alignment-plan).
- **loopzero-api / loopzero-cloud**: Both systems must share `runAsUser: 1000` (matching Dockerfile) and identical resource limits.
- The operators are deployed as **4 split binaries** (ADR-0023, plan `operators-sota-faang-grade-plan.md` Phase 1, 2026-05-04 LOCAL): `loopzero-tenant-operator`, `loopzero-app-operator`, `loopzero-preview-operator`, `loopzero-platform-operator`. Each has its own `operators/cmd/<name>/main.go`, Dockerfile, Helm chart in `charts/`, ServiceAccount + ClusterRole + leader-election Lease, and only mounts the env vars its controllers need. The legacy monolith chart `infra/helm/tenant-controller` and the redundant `infra/helm/loopzero-controller` were deleted in T1.5; production rollout is governed by separate `CHG-NNNN-OPERATOR-SPLIT-PROD` per ADR-0023's 4-stage cutover.

### Production Infrastructure (DigitalOcean)

3 DOKS clusters (v1.32.13, 2 nodes, SFO3) · PostgreSQL 16 managed · DO Container Registry (`registry.digitalocean.com/loopzero-infra-registry`) · CF Tunnel (zero public IPs, all runtime via CF edge) · CF Worker `loopzero-static-delivery` · CF KV `loopzero-routing` · CF Zone Mgmt for custom domains · GitHub App `LoopZero Platform` (ID 3319955) · DO Spaces `loopzero-static-assets` + CDN · State backend `s3://loopzero-infra-pulumi-state`.

### Multi-Cell Routing

Tenant workloads run on independent K8s clusters called **cells** (`cell-default`, `cell-1`, `cell-2`, ...). Routing is **placement once, route many** — not load-balanced. Local k3d state validated 2026-05-01: 3/3 ClusterCredentials connected; DynamicRouter `count=3`.

**Lifecycle of a deploy:**

1. **`loopzero bootstrap` (NEW tenant) — placement decision (one-time).** `RoundRobinEngine.AssignAndExecute` (`api/internal/placement/engine.go:179`) runs inside a SERIALIZABLE transaction with `pg_advisory_xact_lock`. Algorithm: bin-packing — pick the cell with FEWEST tenants AMONG those not at `maxPerCell` capacity (default 500). Writes `tenants.cell_id` (canonical SoT) + Tenant CR `spec.cellID`. Sticky thereafter.
2. **`loopzero deploy` / `promote` / `rollback` / `logs` / `status` — routing (every call).** Reads `tenants.cell_id` from PG. `DynamicRouter.Resolve(cellID)` (`api/internal/placement/dynamic_router.go:133`) is pure hash lookup — zero decision. Returns `CellConfig {kubeconfig, ArgoCD URL/token, base domain}`. Used by `cellmgr/client_manager.go:85` (kubectl) and `argocd/sync_client_factory.go:72` (sync).
3. **`loopzero admin tenants pin --tenant X --cell Y` — operator override (only path to move tenant).** `CellPinner.Pin` (`api/internal/services/cell_pinner.go`): DB UPDATE with capacity gate + Tenant CR MergePatch atomically. Operator reconcile re-emits ArgoCD App+AppProject with `destination.name=Y`. ArgoCD resolves via cluster Secret label.

**Key SoT separation:**
- `tenants.cell_id` PG column — canonical placement decision
- `cells` SQL table — operational metadata (capacity, status active/draining/maintenance/retired, region, audit)
- `ClusterCredential` CRD on mgmt cluster — connectivity (kubeconfig Secret + spec.cellID) — `K8sCellStore.LoadCells()` polls every 60s and **looks up by `spec.cellID`, NOT CR name** (per MEET-053 fix B9)

**Self-healing (`task k3d:heal` codifies):**
- **H12** (matrix entry 153) — cell-1 ClusterCredential missing despite cells SQL active: bootstraps via `clustercredential-create.sh cell-1 workload <scoped-kc>`. Cluster-name=`workload` matches `k3d-loopzero-workload-server-0` docker container (NOT `runtime` — that container doesn't exist locally; B82 fallback would render unreachable).
- **H13** (matrix entry 154) — cell-default SA token rotated/invalidated (401 even with exp claim valid): `kubectl create token loopzero-system/default --duration=24h` + patches Secret `data.kubeconfig` preserving CA + in-cluster server URL.
- Both pinned via vitest in `infra/pulumi/src/__tests__/heal-script-H12.test.ts` (19 invariants).

**Scoped-kubeconfig regen (`task k3d:regen-scoped` codifies — CORRECTIONS.txt item N, plan T2.1):** After ANY topology change that may invalidate scoped SA tokens (node-pool resize via doctl, manual SA recreate, post-rotation drift) — `task k3d:regen-scoped` regenerates the scoped SA + token for every ClusterCredential EXCEPT `cell-default` (which is bootstrap-managed by `bootstrap-cell-default-cc.sh` per H13). Implementation: `infra/scripts/local/regen-all-scoped-kubeconfigs.sh` (495 LOC), flock-protected (`/tmp/loopzero-regen-scoped-kubeconfig.lock` per EC-3 concurrent-regen race protection), Python filter excludes `spec.cellID == "cell-default"` per EC-1, optimistic-concurrency Secret patch with `--resource-version`, creates Secret if absent per EC-6. Pass `cluster_name` BARE (e.g., "workload"), NOT cell_id and NOT loopzero-prefixed — lib `scoped-kubeconfig.sh` internally prepends `k3d-loopzero-` per B81/B82. `DRY_RUN=1 task k3d:regen-scoped` previews without modification. Idempotent. 9/9 hermetic tests in `infra/tests/check-regen-scoped-kubeconfigs_test.sh`.

**Why no automatic rebalancing** (ADR-0008 + ADR-0021 D3):
- DNS continuity (endpoints `*.{tenant}.loopzero.local` cravado em cluster origem)
- State affinity (Secrets/ESO + S3 bundles + ArgoCD App+AppProj)
- Hot-spot signal = upgrade tier signal, NOT migration signal
- Auditability (`loopzero admin tenants pin` writes audit_log; auto-rebalance would be opaque)

Cell drain (`cells.status='draining'`) is operator-driven — `loopzero admin tenants pin --to cell-novo` per tenant.

---

## Known Systemic Patterns

| Pattern | Mitigation |
|---------|-----------|
| "Implemented but not wired" | Verify call site after implementation; integration test required |
| "Correct code in wrong place" | Single source of truth per behavior; grep duplicates before merge |
| "Project name vs project ID" | CLI sends project name as `project_id` in builds. Use name (not UUID) for build/release queries. Only `projects` table uses UUID. |
| "ArgoCD `notifiers` not `services`" | argo-cd Helm v7.x: use `notifications.notifiers`, `services` silently ignored |
| "Single ArgoCD App per tenant" | One App per environment. Any Degraded resource blocks ALL notifications. `gitops.Remove` failure is FATAL. |
| "Implemented but not documented" | Code is source of truth. Verify before claiming missing. |
| "Custom domains = CF Zone per domain" | v2: CF Zone Mgmt, NS change by user, no cert-manager Certificate (CF handles TLS). Full state machine: see § State Machines (Domain row) — includes v1 (TXT verification) backward-compat flow + degraded/failed/removing states. |
| "CF scan imports conflicting apex" | v2 create flow deletes conflicting A/AAAA/CNAME at apex BEFORE creating routing CNAME |
| "Live vs snapshot credentials" | Prefer fetching the live source at reconcile time over snapshotting at bootstrap. Snapshots go stale; live sources self-heal. See `kube-root-ca.crt` pattern for cluster CAs (`operators/internal/platform/ca_fetcher.go`, plan 2026-05-25-live-ca-fetch-clustercredential). |
| "ESO ExternalSecret cascade-delete" | `creationPolicy: Owner` sets ownerReference with `blockOwnerDeletion: true` → K8s GC cascade-deletes the K8s Secret on CR delete BEFORE ESO can honor `deletionPolicy: Retain`. Use `creationPolicy: Orphan` for any chart that needs rollback-safe lifecycle (helm uninstall preserves Secrets). VAULT-INT iter 20 P0 bug — discovered LIVE via rollback drill. Regression test: `check-loopzero-external-secrets-creation-policy_test.sh`. |
| "Bootstrap circular dependency (X-fetches-X)" | A credential fetcher MUST NOT use the credential it fetches. Example: fetcher reading `kube-root-ca.crt` to refresh stale CA must connect with `Insecure: true`, NOT with the stale CA itself — otherwise TLS handshake fails first and fetch never runs (Op B EC-13 trap). |
| "Validation procedure proves nothing" | Procedures that "test" CA rotation by restarting k3s container do NOT actually rotate the CA (it's persisted on volume). Real rotation requires `rm /var/lib/rancher/k3s/server/tls/server-ca.{crt,key}` before restart. Always assert pre/post hash differ to prove the failure mode was actually triggered. |
| "GitHub tarball ≠ git bundle" | autodeploy.go: tarball → extract → git init/add/commit → bundle → S3 |
| "Auto-deploy source = commit SHA" | Key `{tenant}/source/{commitSHA}.bundle`. CommitSHA from webhook must match exactly. |

---

## Common Mistakes — Read Before Coding

| Mistake | Fix |
|---------|-----|
| `pg_advisory_lock` (session-scoped) | Use `pg_advisory_xact_lock` |
| Concurrent build check OUTSIDE lock | Move inside `withEnvironmentLock` |
| `errors.New("...")` | Use `LoopZeroError` with typed code |
| `release.Status.State = "active"` | Use `release.Transition(event)` |
| `kubectl` in user-facing flow | Proxy through API |
| Mark release ACTIVE before ArgoCD confirms | Wait for sync confirmation |
| Internal terms in CLI output | Product language (Rule 3) |
| `notifications.services` in ArgoCD Helm | Use `notifications.notifiers` |
| `project.ID` (UUID) for build/release queries | Use project name from `loopzero.yaml` |
| `gitops.Remove` error treated non-fatal | FATAL — delete must fail |
| cert-manager Certificate for CF-managed domains | Skip Certificate CR (use `CFManaged`) |
| Forward-only migration violations (`DROP`) | Explicit sign-off required |
| Writing prod code before failing test | Test first, always |
| Multi-cell ClusterCredential CR name = cellID assumption | Wrong — `K8sCellStore` lookup is by `spec.cellID`, not metadata.name. CR named `workload` with `spec.cellID=cell-1` works fine (per H12). |
| `wire-clusters.sh` with `cluster-name=runtime` for k3d | Wrong — local cluster is `loopzero-workload`. Use `cluster-name=workload` so `scoped-kubeconfig.sh` resolves `k3d-loopzero-workload-server-0` docker container (B82 + H12). |
| Cached SA token in ClusterCredential Secret stops working before exp claim | SA UID changed (recreated during rollout). Mint fresh: `kubectl create token loopzero-system/default --duration=24h` + patch Secret data.kubeconfig (per H13). |
| ArgoCD `InvalidSpecError: project X does not exist` (but project EXISTS) | Repo-server cached `app path does not exist` for ~5min. `SyncClient.TriggerSync` calls `?refresh=hard` BEFORE POST /sync (B32). |
| VAP `failurePolicy: Fail` deadlocks namespace deletion | Add `matchConditions` excluding `system:serviceaccount:kube-system:namespace-controller`, `system:kube-controller-manager`, `system:masters` group (B33). |
| `secret K8s manager not available — ESO sync disabled` in api logs | `RUNTIME_KUBECONFIG` not mounted. Mount `loopzero-cluster-kubeconfigs` Secret (key=`runtime.kubeconfig`) + export `RUNTIME_KUBE_CONTEXT=in-cluster` (B34). |
| `seed-local-user.sh failed during regen` mid-journey | `HYDRA_NAMESPACE=loopzero-auth` (prod default) ≠ `loopzero-system` (k3d standalone). Set `SEED_HYDRA_NS=loopzero-system` in `_common.sh` (B35). |
| `releases_status_check` constraint violation on rollback | DB CHECK missing `ROLLING_BACK`/`ROLLED_BACK`. Migration `000010_release_rollback_states.up.sql` adds them. State machine declares it but DB CHECK was never updated (B37). |
| Admin endpoints return 404 `Endpoint not available` despite valid X-Service-Auth | `LOOPZERO_ADMIN_API=true` env var missing (defense-in-depth flag). Production sets via Pulumi; standalone Tilt YAML must include it (B38). |
| `kubectl rollout restart` is non-blocking — race with downstream operations | Always pair with `kubectl rollout status` if subsequent commands trigger reconciles in old/new pod. Old pod (env stale) reconciles BEFORE termination, sobrescreve writes (B39). |
| Convergence loop / health checker filters `res.Kind != "Deployment"` only | Wrong — workload primitive is now Deployment OR Rollout (default-on progressive delivery). Accept BOTH (`res.Kind != "Deployment" && res.Kind != "Rollout"`) or every Rollout-backed release stays PENDING forever (F-19). |
| `kubectl get deploy` in test scaffolding to verify workload exists | Wrong post-canary plan — query `deployments.apps,rollouts.argoproj.io` together. ProveDead helpers must include both kinds + TraefikService (F-20). |
| Adding a workload-cluster Helm release without updating `task k3d:bootstrap-workload` selector | The standalone `task setup` only syncs charts named in the explicit `--selector name=X` list. New runtime-cluster releases (e.g., argo-rollouts) MUST be added to that list, otherwise fresh cleanroom never deploys them (F-21). |
| New tenant-side CRD requires applying it via ArgoCD without granting `loopzero-ctrl-runtime` RBAC | The scoped kubeconfig's ClusterRole `loopzero-ctrl-runtime-rbac-admin` (`infra/helmfile/scripts/lib/scoped-kubeconfig.sh`) lists framework CRD apiGroups; missing entries cause silent `forbidden` failures during ArgoCD sync. Add the new apiGroup explicitly (F-22 added `argoproj.io`). |
| Argo Rollouts 1.7.x defaults Traefik traffic-router queries to legacy `traefik.containo.us` | LoopZero runs Traefik v3 on `traefik.io/v1alpha1`. Set `controller.extraArgs: [--traefik-api-group=traefik.io, --traefik-api-version=v1alpha1]` in `infra/helm/argo-rollouts/values.yaml` (F-16). |
| `Rollout` annotation `argocd.argoproj.io/sync-wave: "-1"` on first deploy | Wave -1 makes ArgoCD apply Rollout BEFORE the stable+canary Services. Rollout immediately Degraded (`InvalidSpec — stableService not found`) and ArgoCD halts. Use default wave 0 for ALL canary resources (F-15). |
| Test fixture `loopzero.yaml project: <name>` must match journey's `project` constant | Fixture gets named via loopzero.yaml; deployed Rollout = `<project>-<app>`. If they diverge, `kubectl get rollout <project>-<app>` returns NotFound and the assertion fails. Create dedicated fixture or align constant (F-17). |
| Renderer omits `Rollout.spec.progressDeadlineSeconds` | Argo Rollouts default 600s exceeds the 8min E2E budget for readiness-driven aborts. Renderer sets `progressDeadlineSeconds: 300` for ALL Rollouts so canary failures (e.g., bad `/health` probe) transition to Degraded within budget (F-18). |
| `if app.Build == "dockerfile"` in workflow_builder orchestration layer | Wrong layer — strategy decisions belong in `LanguageStrategy.DockerfileStep`, not in the orchestrator. Use `ResolveStrategyForApp(AppSpec)` + `g.Strategy.DockerfileStep(...)`. UserDockerfileStrategy emits `dockerfile-lint`; language strategies emit `generate-dockerfile`. Build mode dispatch plan T1.6. |
| DAG hardcodes `"generate-dockerfile"` as build-image dependency | Wrong post-build-mode-dispatch plan — UserDockerfileStrategy emits `dockerfile-lint` instead. Use `dockerfileStepTemplateName(strategy)` helper to capture the actual emitted name. Hardcoded literal causes `invalid spec: template name 'generate-dockerfile' undefined` for build:dockerfile workflows. |
| loopzero-packs v0.3.x stale image rejects user Dockerfile | Pin `imageLoopZeroPacks = ":v0.4.0"` (canonical lenient contract). v0.4.0 copies user Dockerfile verbatim to `--output`, exits 0. Cleanroom heal H16 ensures registry has v0.4.0 via `infra/scripts/build/build-and-push-loopzero-packs.sh`. |
| New tenant CRD apiGroup added without RBAC update | `loopzero-ctrl-runtime-rbac-admin` ClusterRole's framework-CRD apiGroups list MUST include the new group. F-22 added `argoproj.io`; build-mode-dispatch added pattern (no new apiGroup, but Strategy refactor reminded us). Defense-in-depth + governance-gate registers scope. |
| Dockerfile linter rejection in user-facing message uses internal terms | Use product language: "Dockerfile validation" not "BuildKit lint", "LoopZero's allow-list" not "ClusterImagePolicy registries". Hard rejection rule names (FROM_REGISTRY_NOT_ALLOWED etc.) are stable contracts but messages must be plain English. |
| Embedding CA snapshot in ClusterCredential reconcile (instead of live fetch) | Use `caFetcher.FetchClusterCA(ctx, restConfig)` (`operators/internal/platform/ca_fetcher.go`) to read `default/kube-root-ca.crt` on every reconcile. Snapshot becomes stale on CA rotation (k3s restart, prod incidents). When fetching, MUST clone `restConfig` with `TLSClientConfig{Insecure: true}` — otherwise stale snapshot blocks the very call that would refresh it. See plan 2026-05-25-live-ca-fetch-clustercredential. |
| `creationPolicy: Owner` em chart com ExternalSecret CRs | Wrong para qualquer chart que precisa rollback safety. Owner sets `blockOwnerDeletion: true` ownerReference → K8s GC cascade-deletes Secret on `helm uninstall` BEFORE ESO honors `deletionPolicy: Retain` → pods CrashLoop on next restart (envFrom secrets missing) → outage. Use `creationPolicy: Orphan`. VAULT-INT iter 20 P0 fix; regression test em `infra/tests/check-loopzero-external-secrets-creation-policy_test.sh`. |
| Rolling out new VAULT-INT operator script sem hermetic + LIVE testing | Script handling secrets requer 2 layers: (a) hermetic test com mock bao/kubectl + `test_dry_run_no_raw_secret_leak` (security invariant — value NEVER em stdout/stderr); (b) LIVE test against dev-mode Vault (catches K8s GC semantics que mocks não simulam). VAULT-INT iter 18-22 LIVE pegou P0 cascade-delete bug que 68 hermetic PASS não captou. Pattern: 6 scripts + 9 hermetic suites + LIVE eval em standalone-multi level. |
