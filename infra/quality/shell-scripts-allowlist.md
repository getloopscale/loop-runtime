<!--
RESTAURADO DE `.claude/rules/shell-scripts-allowlist.md` EM 2026-09-16, e movido de
proposito para um caminho RASTREADO.

`3d4075ca3` (2026-09-08) removeu a arvore de ferramental instalada do rastreamento e
levou este registro junto — dano colateral, nao decisao: `.claude/` e' dependencia
instalada e gitignored, e este arquivo e' governanca DESTE repositorio. Medido
2026-09-16: seis gates o leem e quatro deles ficaram vermelhos para todo clone, todo
runner e todo contribuidor, porque o arquivo existia apenas na maquina de quem tinha
o kit instalado.

`infra/quality/` e' onde dados de gate vivem rastreados — `chart-pairs.txt` ja' estava
la'. O conteudo abaixo e' o do commit anterior aquela remocao, byte por byte.
-->

# Shell Scripts Allowlist

**Authoritative source-of-truth for `loopzero-ops quality no-new-bash` CI gate.**
Per **D3** of `docs/plans/completed/2026-05-22-shell-script-strategic-plan.md` v1.3:
**creating new `.sh` files in the repo is FORBIDDEN by default**.

## How this gate works

1. CI workflow runs `loopzero-ops quality no-new-bash --base-ref origin/main --head-ref HEAD`
2. Gate parses this file extracting all paths matching the markdown-table pattern `| <path>.sh | ... |`
3. Gate runs `git diff --diff-filter=AR <base>...<head> -- '*.sh'` to find ADDED + RENAMED-TO scripts
4. If any new script is NOT in this allowlist → CI fails
5. CI does **NOT** block: modified (M) or deleted (D) scripts

## How to add a NEW script (rare; requires justification)

If you genuinely need a new `.sh` file:

1. **Check first:** can the function go in:
   - `infra/helmfile/` (declarative install) — see D2
   - `pkg/quality/<gate>/` (CI gate as pure-logic Go) — see D5 + Anti-Pattern #8
   - `infra/tests/e2e/` (Ginkgo test orchestrator)
   - `go:generate` directive (generator)?
2. **Only if NONE fit:** add an entry to one of the tables below + open ADR explaining why D4 criteria are satisfied (Necessary + Simple + Operator-time + Allowlisted)
3. Reference the ADR in the Justificativa column

## Allowlist format

Each section corresponds to a directory grouping. Tables MUST have exactly the column structure shown:

```
| <script-path-relative-to-repo-root>.sh | <LOC> | <one-line-justificativa> |
```

The CI gate parses ONLY rows starting with `|` whose first column ends in `.sh`. Header separator rows (`|---|---|`) and other markdown are silently ignored.

---

## Baseline grandfathered (2026-05-22)

Per Ralph iter 2 of plan execution, these 257 scripts are **grandfathered** into
the allowlist as the **starting point** of D3 enforcement. As Phases 1-5 of the
plan execute (delete dead, delegate to helmfile, migrate to Ginkgo/Go), entries
will be removed from this file proportional to script deletions. **Final
target: ≤ 30 scripts.**

Grandfathering is NOT a permanent endorsement. Each entry below is also
present in `docs/scripts-audit/2026-05-22.csv` with a proposed disposition;
operator review (`docs/scripts-audit/REVIEWED.md`) classifies each as either
KEEP-OPERATOR (truly justified per D4) or candidate for downstream phase action.

### .claude/

| Script | LOC | Justificativa |
|---|---:|---|
| .github/scripts/check-workflow-hygiene.sh | 88 | KEEP-HARNESS-HOOK 2026-08-06 — guarda de higiene de workflows (#233/#250) — alvo e YAML do .github/, nao codigo Go; invocada por task quality:gates. Chegaram com o sync do cycle-kit e nunca foram allowlistados; mesma lacuna ja documentada para validate-command.sh. |
| .claude/hooks/delivery-gate.sh | 66 | KEEP-HARNESS-HOOK 2026-08-06 — PreToolUse(Bash) que intercepta `git push` e roda `task quality:all`, bloqueando (exit 2) se reprovar. (Rodava `quality:gates` ate 2026-08-13, cujas deps sao `[build]` e nada mais — os testes que provam que um gate reprova o que deve reprovar ficavam de fora do unico checkpoint obrigatorio. B-088 bala 2.) Existe porque o pre-push do git NAO e versionado: so existe onde alguem rodou install-git-hooks.sh, entao nao vale para o agente nem para um clone novo. Escopo estreito de proposito (so `git push`, pula `--dry-run`) e sem flag de bypass — e isso passou a ser medido, nao afirmado: uma auditoria achou quatro payloads que chegavam ao remoto sem o portao decidir nada (`git -C <path> push`, e `--dry-run` em qualquer outro ponto de um comando composto), os dois fechados e presos por teste. Mesma categoria harness-hook dos irmaos acima. RESTAURADO 2026-09-05 (B-187) a partir de `53c1dc1ea` depois da terceira delecao; a LOC conta LINHAS DE CODIGO (66 de 214 totais; a linha anterior, 181, era um `wc -l` de um arquivo majoritariamente comentario, ao lado de irmaos que trazem numeros de codigo). Subiu de 35 porque o cabecalho passou a registrar o que o arquivo nao cobre (a promocao) e as tres falhas fail-open que a versao anterior tinha. |

### cloud/

| Script | LOC | Justificativa |
|---|---:|---|

### infra/helmfile/scripts/

| Script | LOC | Justificativa |
|---|---:|---|
| infra/helmfile/scripts/bootstrap-cell-cc.sh | 234 | grandfathered 2026-05-22 — Phase 2 candidate (delegate to helmfile) |
| infra/helmfile/scripts/bootstrap-eso-cluster-secret-store.sh | 73 | grandfathered 2026-05-22 — Phase 2 candidate |
| infra/helmfile/scripts/bootstrap-oauth-clients.sh | 233 | grandfathered 2026-05-22 — Phase 2 candidate |

### Baseline (auto-imported)

> **Note for operators:** the full grandfathered list (252 remaining entries) is
> auto-imported below. Each is provisional — review and reclassify during Phase
> 1-5 execution. Use the `docs/scripts-audit/2026-05-22.csv` `disposition`
> column as the authoritative classification source for action.

<!-- BASELINE_AUTO_START — do not edit manually; regenerate via `loopzero-ops quality scripts-audit` + script -->

| Script | LOC | Justificativa |
|---|---:|---|
| infra/helmfile/scripts/bootstrap-patch-argocd-gitea-creds.sh | 45 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/helmfile/scripts/bootstrap-patch-argocd-repo-server-restart.sh | 29 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/helmfile/scripts/create-wiring-configmap.sh | 85 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/helmfile/scripts/generate-build-secrets.sh | 120 | grandfathered 2026-05-22 — MIGRATE-GENERATOR |
| infra/helmfile/scripts/generate-runtime-secrets.sh | 140 | grandfathered 2026-05-22 — MIGRATE-GENERATOR |
| infra/helmfile/scripts/generate-secrets.sh | 370 | grandfathered 2026-05-22 — MIGRATE-GENERATOR |
| infra/helmfile/scripts/lib/argocd-register.sh | 99 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/helmfile/scripts/lib/clustercredential-create.sh | 159 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/helmfile/scripts/lib/hydra-mint.sh | 171 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/helmfile/scripts/lib/pulumi-output.sh | 127 | KEEP-OPERATOR 2026-07-10 (M18 T2.2 reclassify per plan ADR-D2) — original MIGRATE-GO-SDK classification voided: empirical Q5 discovered `pulumi/sdk-v3` NOT present in any `go.mod` across the repo (cmd/loopzero-ops/go.mod, api/go.mod, pkg/go.mod, operators/go.mod). ADR-M15-D5 shim-first pre-req unsatisfied without adding a new Go dep, which contradicts Parsimony ladder rung 4. Path A canonical: `pulumi-output.sh` is Anti-Pattern #8 legitimate operator orchestration (subprocess `pulumi` CLI call) — same class as `check-cosign-cutover-deadline.sh` KEEP. Refactor to Go SDK requires new ADR at M19 plan-time. |
| infra/helmfile/scripts/lib/scoped-kubeconfig.sh | 183 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/helmfile/scripts/regression-checks.sh | 200 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/helmfile/scripts/seed-local-user.sh | 187 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/helmfile/scripts/test_helpers/run-all.sh | 37 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/helmfile/scripts/test_helpers/test-create-wiring-configmap.sh | 68 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/helmfile/scripts/test_helpers/test-generate-runtime-secrets.sh | 51 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/helmfile/scripts/test_helpers/test-generate-secrets.sh | 83 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/helmfile/scripts/test_helpers/test-idempotency.sh | 202 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/helmfile/scripts/wire-clusters.sh | 84 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/kubeconform-schemas/validate-helmfile-templates.sh | 74 | KEEP-CI-ORCHESTRATOR 2026-05-22 — pipes `helmfile template` into `kubeconform` (two external CLIs). NOT pure-logic; Go would be a shell-wrap (D5). Shell deliberately avoids Taskfile Go-template vs kubeconform {{.ResourceKind}} collision. Reclassified iter 27 (iter-25 heuristic false-negative: \bhelm\b missed "helmfile"). |
| infra/scripts/local/bootstrap-seed-tenant-cr.sh | 58 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/scripts/production/break-glass.sh | 145 | KEEP-OPERATOR 2026-05-23 — live-cluster operator tool, sponsor-ratified Phase-5 ledger |
| infra/scripts/test/baseline.sh | 212 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/scripts/test/cleanup.sh | 113 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/scripts/test/report.sh | 197 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/scripts/test/run-full.sh | 173 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/scripts/test/run-scenario.sh | 289 | grandfathered 2026-05-22 — DELEGATE-HELMFILE |
| infra/scripts/production/check-prod-vap-readiness.sh | 129 | KEEP-OPERATOR 2026-05-22 — live prod kubectl+cosign+argocd VAP-flip pre-flight (D4 operator-time + Anti-Pattern 8). NOT pure-logic; reclassified iter 25 after read-verification. |
| infra/scripts/security/check-cosign-cutover-deadline.sh | 158 | KEEP-OPERATOR 2026-05-27 — T3.3 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap. Enforces ADR 2026-05-12 cosign keyed→keyless hard deadline (T0+90d). Reads ConfigMap loopzero-system/loopzero-cosign-config; exit 1 if past deadline + mode==keyed; --emergency-override flag writes audit_log (EC-7 of edge-case-review). Live-cluster kubectl + psql, irreducible. |
| infra/scripts/local/clean-incremental.sh | 61 | KEEP-OPERATOR 2026-05-23 — live-cluster operator tool, sponsor-ratified Phase-5 ledger |
| infra/scripts/drill/dr-drill.sh | 297 | grandfathered 2026-05-22 — DELEGATE-ARGOCD |
| infra/scripts/drill/cancel-refresh.sh | 142 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/scripts/drill/cost-report.sh | 159 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/scripts/drill/evidence-capture.sh | 298 | KEEP-OPERATOR 2026-05-24 — orchestration (21 kubectl/doctl/pulumi gather calls vs 4 jq lines; no separable shaper). MEET-ASYNC-AMENDMENT-2026-05-24-002 (ratified). |
| infra/scripts/drill/jwt-diagnose.sh | 372 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/scripts/drill/preflight.sh | 381 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/scripts/drill/state-reset.sh | 340 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/scripts/drill/teardown-validator.sh | 283 | KEEP-OPERATOR 2026-05-24 — orchestration (doctl list/delete-woven; only a trivial 3-way verdict is pure, not worth a Go round-trip leaving the I/O untested). MEET-ASYNC-AMENDMENT-2026-05-24-002 (ratified). |
| infra/scripts/drill/watchdog.sh | 149 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/scripts/drill/drill-helmfile-bootstrap.sh | 1126 | grandfathered 2026-05-22 — DELEGATE-HELMFILE |
| infra/scripts/drill/drill-mint-jwt.sh | 300 | KEEP-OPERATOR 2026-05-24 — orchestration (hydra port-forward+curl mint; TTL assert embedded in lib/hydra-mint.sh:mint_jwt, not separable). MEET-ASYNC-AMENDMENT-2026-05-24-002 (ratified). |
| infra/scripts/ci/ensure-project.sh | 49 | KEEP-CI-ORCHESTRATOR 2026-05-23 — wraps gh/npm/go (anti-pattern-8), sponsor-ratified Phase-5 ledger |
| infra/scripts/local/k3d-cross-cluster-heal.sh | 980 | KEEP-OPERATOR 2026-05-23 — live-cluster operator tool, sponsor-ratified Phase-5 ledger |
| infra/scripts/lib/bootstrap-cell-cc-common.sh | 161 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/scripts/lib/bootstrap-cell-cc-drill-common.sh | 166 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/scripts/lib/drill-jwt-gate.sh | 68 | KEEP-OPERATOR 2026-05-24 — orchestration (verify_jwt_auth_e2e: mints + LIVE curls loopzero-api /projects, not a pure TTL decode). MEET-ASYNC-AMENDMENT-2026-05-24-002 (ratified). |
| infra/scripts/lib/drill-skill-common.sh | 464 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/scripts/lib/dry-run.sh | 86 | KEEP-OPERATOR 2026-05-23 — live-cluster operator tool, sponsor-ratified Phase-5 ledger |
| infra/scripts/lib/sops.sh | 126 | KEEP-OPERATOR 2026-05-23 — live-cluster operator tool, sponsor-ratified Phase-5 ledger |
| infra/scripts/production/migrate-tenant-namespace.sh | 229 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/scripts/test/pkg-soak-smoke.sh | 161 | KEEP-OPERATOR 2026-05-23 — live-cluster operator tool, sponsor-ratified Phase-5 ledger |
| infra/scripts/production/provision-api-tokens.sh | 357 | KEEP-OPERATOR 2026-05-23 — live-cluster operator tool, sponsor-ratified Phase-5 ledger |
| infra/scripts/ci/branch-protection-update.sh | 84 | KEEP-CI-ORCHESTRATOR 2026-05-23 — wraps gh/npm/go (anti-pattern-8), sponsor-ratified Phase-5 ledger |
| infra/scripts/ci/coverage-go.sh | 120 | KEEP-CI-ORCHESTRATOR 2026-05-23 — wraps gh/npm/go (anti-pattern-8), sponsor-ratified Phase-5 ledger |
| infra/scripts/local/regen-all-scoped-kubeconfigs.sh | 220 | KEEP-OPERATOR 2026-05-23 — live-cluster operator tool, sponsor-ratified Phase-5 ledger |
| infra/scripts/local/bootstrap-sigstore-local.sh | 122 | KEEP-OPERATOR 2026-05-28 — Ralph iter 5 of plan 2026-05-28-M07-fulcio-cacert-keyless-LIVE v1.6. Idempotent LOCAL Sigstore stack bootstrap: helm install scaffold + Fetch K3s CA + Patch Fulcio OIDCIssuer.CACert + Install sigstore-extras Helm chart (NetworkPolicies kube-router fix) + ClusterRoleBinding RBAC. Wired into task bootstrap:standalone (opt-out via SKIP_SIGSTORE_LOCAL=1). Irreducible: live-cluster orchestration. Reproduces M07 LIVE keyless GREEN from cleanroom (validated 2026-05-28). |
| infra/scripts/local/fulcio-oidc-diagnose.sh | 163 | KEEP-OPERATOR 2026-05-28 — T0.1 of plan 2026-05-28-M07-fulcio-oidc-trust-to-3-85. Live-cluster diagnostic for Fulcio K8s OIDC trust chain (M07 keyless 3.85 cap): kubectl exec sa-token-probe + net-probe + Fulcio debug log capture + cleanup trap. Irreducible per D4 strategic plan — depends on live cluster state. |
| infra/scripts/local/lib/fulcio-probes.sh | 173 | KEEP-OPERATOR 2026-05-28 — T0.1 lib sourced by fulcio-oidc-diagnose.sh (probe_sa_token_iss, probe_network_discovery, probe_fulcio_logs, cleanup). Live-cluster, irreducible. |
| infra/scripts/production/run-migrations.sh | 261 | KEEP-OPERATOR 2026-05-23 — live-cluster operator tool, sponsor-ratified Phase-5 ledger |
| infra/scripts/local/setup-cross-cluster-networking.sh | 86 | KEEP-OPERATOR 2026-05-23 — live-cluster operator tool, sponsor-ratified Phase-5 ledger |
| infra/scripts/local/setup-host-inotify-limits.sh | 96 | KEEP-OPERATOR 2026-05-23 — live-cluster operator tool, sponsor-ratified Phase-5 ledger |
| infra/scripts/local/update-cross-cluster-ips.sh | 171 | KEEP-OPERATOR 2026-05-23 — live-cluster operator tool, sponsor-ratified Phase-5 ledger |
| infra/scripts/local/validate-op-b-soak.sh | 128 | KEEP-OPERATOR 2026-05-25 — T5.0 pre-flight gate for Op B heal cleanup (plan 2026-05-25-live-ca-fetch-clustercredential). Queries Prometheus for loopzero_clustercredential_ca_fetch_total{result="fallback"} over N-day window; blocks T5.1 deletion of H19/H19a if any fallback events occurred. Honors --dry-run for hermetic test mode. |
| infra/scripts/drill/velero-dr-drill.sh | 413 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/tests/livetest/dogfood/preflight.sh | 484 | grandfathered 2026-05-22 — DELEGATE-HELMFILE |
| infra/tests/e2e/local-install-deploy.sh | 398 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/tests/performance/load/run-tests.sh | 106 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/tests/runner/run-chaos.sh | 133 | grandfathered 2026-05-22 — DELEGATE-HELMFILE |
| infra/tests/runner/run-k6.sh | 83 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/tests/runner/safety.sh | 56 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| tools/admin-evict-tenants.sh | 126 | KEEP-OPERATOR 2026-05-23 — live-cluster operator tool, sponsor-ratified Phase-5 ledger |

<!-- BASELINE_AUTO_END -->

### Post-baseline additions (plan 2026-05-29-dashboard-public-integration-canonicalization)

| Script | LOC | Justificativa |
|---|---:|---|
| infra/scripts/local/recover-mgmt-degraded.sh | TBD | KEEP-OPERATOR 2026-05-29 — live-cluster recovery for stuck Terminating pods + degraded Deployments with --request-timeout=10s + exponential backoff. T0.2 of plan. |
| infra/scripts/local/seed-demo-data.sh | TBD | KEEP-OPERATOR 2026-05-29 — codified demo data seed (2 projects + 8 builds + 5 releases + audit). Live PG inserts; idempotent via FK-respecting DELETE order. T6.1 of plan. |
| infra/scripts/local/port-forward-public-dashboard.sh | TBD | KEEP-OPERATOR 2026-05-29 — persistent 4-pf orchestrator with PID file + signal trap + exponential backoff (2s→30s caps). T7.3 of plan. |

### Post-baseline additions (plan 2026-06-04-dev-public-domain-canonicalization)

| Script | LOC | Justificativa |
|---|---:|---|
| infra/scripts/local/setup-dev-public-dns.sh | ~120 | KEEP-OPERATOR 2026-06-04 — T0.1 of plan 2026-06-04-dev-public-domain-canonicalization. Idempotent upsert of 3 A records (auth-dev/app-dev/cloud-dev.loopzero.example) via Cloudflare API curl. --dry-run + --verify flags. Pulumi codification deferred pending governance scope expansion for infra/pulumi/src/cloudflare/. Live-cluster orchestration (CF API), irreducible. |
| infra/scripts/local/caddy-up-dev-public.sh | ~120 | KEEP-OPERATOR 2026-06-04 — T1.1 of plan. Syncs canonical infra/local/Caddyfile to runtime .local/Caddyfile + starts/reloads Caddy. --verify validates TLS Let's Encrypt certs issued for all 6 hostnames via openssl s_client probe + curl HTTP code (accepts 200/404/502 — 502 = TLS OK upstream absent). Idempotent. |
| infra/tests/e2e/live/dev-public-oidc-redirect_test.sh | ~150 | KEEP-OPERATOR 2026-06-04 — T3+T4 LIVE e2e test. Validates Kratos OIDC flow against https://auth-dev.loopzero.example: browser flow → POST method=oidc → HTTP 303 with Location to accounts.google.com / github.com, redirect_uri equals https://auth-dev.loopzero.example/self-service/methods/oidc/callback/{provider}. Uses curl + python3 (no kubectl). Runs after `task dev-public:bootstrap`. 8/8 PASS LIVE 2026-06-04. |
| infra/scripts/local/check-infra.sh | ~250 | KEEP-OPERATOR 2026-06-04 — FAANG-grade post-cleanroom validation (plan 2026-06-04-dev-public-docr-fix-forward T6+, sponsor request). Detects component placement bugs + Secret/ConfigMap placeholder drift that `task doctor` misses: (a) loopzero-cluster-kubeconfigs slot pointing at mgmt cluster instead of build/runtime, (b) gitea-admin.token holding 32-char placeholder, (c) MinIO rootPassword out of sync with minio-credentials.secretkey, (d) operator NetworkPolicy egress targeting wrong namespace. 6 check categories: C1 placement / C2 anti-placement (CRD must NOT be where it shouldn't) / C3 content + LIVE auth probes / C4 NetPol egress / C5 cells connected / C6 deep health 200. --json mode for CI gate. Live-cluster, irreducible (kubectl get + base64 decode + Pod curl probes). |
| infra/tests/e2e/live/dev-public-billing-portal_test.sh | ~135 | KEEP-OPERATOR 2026-06-04 — T4 LIVE e2e test for /v1/billing/portal regression. Regression-protection for the 2026-06-04 bug where LOOPZERO_STRIPE_SECRET_KEY was sk_test_*SX4hL (rotated) while canonical is rk_test_*TZGhm. Full chain: login → JWT exchange → /v1/billing/portal returns billing.stripe.com URL. 4 steps + 5 assertions. Live-cluster, irreducible. 5/5 PASS LIVE 2026-06-04. |
| infra/scripts/local/trio-dev-public.sh | ~140 | KEEP-OPERATOR 2026-06-04 — T8.1 of plan 2026-06-04. Non-destructive trio LIVE for dev-public env: cleanroom-soft (config drift assertion), chaos-soft (6/6 TLS LE), dogfood-soft (billing portal e2e), live-proof (3 checks × 3 layers). Soft-equivalent of destructive /cleanroom + /chaos-test + /dogfood + /live-proof. --bail-on-fail + --json flags. Live-cluster orchestration, irreducible. 4/4 PASS LIVE 2026-06-04. |
| infra/scripts/local/test-all-dev-public.sh | ~155 | KEEP-OPERATOR 2026-06-04 — T8.1+ aggregate runner for all 9 dev-public test suites (5 hermetic + 4 LIVE). Reports per-suite PASS/FAIL with assert count, total aggregate. --hermetic-only (CI gate, ~3s), --live-only (operational gate, ~30s), --bail-on-fail, --json. 9/9 suites GREEN / 121/0 assertions PASS aggregate LIVE 2026-06-04. |

### Post-baseline additions (plan 2026-06-05-dashboard-journey-j04-recovery)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan 2026-06-05-dashboard-journey-j05-device-success)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan 2026-06-05-dashboard-journey-j09-project-connect)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan 2026-06-06-dashboard-journey-j17-deployments-realtime)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan 2026-06-06-dashboard-journey-j10-environments)

| Script | LOC | Justificativa |
|---|---:|---|
| infra/tests/e2e/live/dev-public-production-deploy_test.sh | 336 | KEEP-OPERATOR 2026-06-09 — T6.1 of plan 2026-06-09-m2-operator-canonical-fix v1.4. LIVE 13-step production deploy chain test against dev-public: (1) preflight gh+kubectl+reachability, (2) reset-for-e2e.sh, (3) seed-admin-loopzero-dev.sh, (4) POST /api/v1/projects (T4.1 auto-creates production env), (5) connect-canonical-fixture.sh, (6) gh api PUT contents triggers real commit, (7) assert release ACTIVE in 10min, (8) assert M2 namespace exists, (9) assert AppProject destinations include M2 ns, (10) assert SA loopzero-app in M2 ns, (11) assert pods Running in 20min, (12) assert ArgoCD Synced+Healthy, (13) anti-workaround self-grep: ZERO `kubectl patch appproject` / `kubectl create namespace loopzero-*` / SA copy / `kubectl patch app sync revision` calls. Wall-clock budget 20min. Flags: --skip-live (hermetic-only) + --json + --skip-reset. Per ADR D6 fail-fast. The goal metric of the M2 plan — when this passes LIVE, the M2 drift is fixed. |
| infra/scripts/local/caddy-build-with-cloudflare.sh | ~95 | KEEP-OPERATOR 2026-06-07 — J-23 #68 of 4-task batch (sponsor verbatim "TLS para *.preview-dev.loopzero.example"). Builds Caddy v2.11.3 + caddy-dns/cloudflare via xcaddy v0.4.6 (both pinned, not floating). Idempotent: skips if $CADDY_OUTPUT exists AND already bundles dns.providers.cloudflare (verified via `caddy list-modules`). CADDY_FORCE_REBUILD=1 overrides. Wires the DNS-01 challenge so the `*.preview-dev.loopzero.example` wildcard cert is issued by LE without per-PR rate-limit consumption. Operator orchestration (go build + sudo install), irreducible. |

### Post-baseline additions (plan 2026-06-08-dashboard-screens-review-fixes v1.2)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan 2026-06-08-pre-release-test-rigor)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan 2026-06-08-pre-release-test-rigor — T1.1)

| Script | LOC | Justificativa |
|---|---:|---|
| infra/scripts/local/baseline-capture.sh | ~210 | KEEP-OPERATOR 2026-06-08 — T1.1 of plan 2026-06-08-pre-release-test-rigor v1.2. Captures 4-layer steady-state snapshot per `.claude/rules/steady-state-baseline.md` into structured JSON for machine comparison. Production mode queries kubectl+helm+curl+psql; FIXTURE mode (BASELINE_FIXTURE_DIR env) reads pre-canned JSON for hermetic tests; DETERMINISTIC mode zeroes captured_at for byte-identical idempotent captures. Volatile field normalization (EC-7 absorption) strips metadata.resourceVersion + generation + managedFields + creationTimestamp + uid + status.conditions[].lastTransitionTime before serialization. Wired in T1.2 cleanroom skill as pre+post-rebuild hook. Pure orchestration: kubectl + helm + curl + psql + python3 jq alternative; irreducible per D4 (bash). |
| infra/scripts/local/baseline-diff.sh | ~130 | KEEP-OPERATOR 2026-06-08 — T1.1 of plan 2026-06-08-pre-release-test-rigor v1.2. Compares 2 snapshots from baseline-capture.sh; classifies deltas as additive (OK)/missing (FAIL)/mismatch (FAIL)/identical (OK). Recursive structural diff with array-element pairing by stable identity key (name/id/kratos_identity_id). Volatile field normalization applied defensively as second pass. Exit 0 on OK verdict / 1 on FAIL. Optional --markdown flag emits human report to stderr. Pure-python3 JSON diff under bash wrapper; irreducible per D4 (bash entrypoint convention; logic lives in inline python3 heredoc — same canonical pattern as M2 plan T8.3). |

### Post-baseline additions (plan 2026-06-08-pre-release-test-rigor — T1.2)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan 2026-06-08-pre-release-test-rigor — T2.1)

| Script | LOC | Justificativa |
|---|---:|---|
| infra/scripts/local/connect-canonical-fixture.sh | ~190 | KEEP-OPERATOR 2026-06-08 — T2.1 of plan 2026-06-08-pre-release-test-rigor v1.2. Connects canonical fixture repo (getloopscale/loopzero-repotest) + creates canonical project (teste) in admin@loopzero.example tenant via LoopZero CLI. Idempotent — detects existing installation + project via /engine/api/v1/projects + suspended_at field on GitHub App installation (EC-10). EC-12 preflight: gh auth status + rate_limit headroom (require remaining ≥ 100) BEFORE any API call. --verify mode queries PG audit_log for project.connected event. --dry-run shows planned actions without mutation. Pure orchestration: gh CLI + curl + LoopZero CLI + kubectl + psql; irreducible per D4 (bash). |

### Post-baseline additions (plan 2026-06-08-pre-release-test-rigor — T2.2)

| Script | LOC | Justificativa |
|---|---:|---|
| infra/tests/e2e/live/dev-public-webhook-chain_test.sh | ~340 | KEEP-OPERATOR 2026-06-08 — T2.2 of plan 2026-06-08-pre-release-test-rigor v1.2 (Cenário 1). LIVE 9-link chain assertion: webhook → autodeploy → HTTPS 200 in < 20 min wall-clock per ADR D8. 6 EC absorbed: EC-1/D7 rate-limit fallback to push-event; EC-2 ArgoCD refresh=hard between link 7 and link 8 (repo-server cache); EC-5 diagnostic dump with kubectl get events --sort-by=lastTimestamp; EC-8 LE staging fallback when "too many certificates"; EC-9 401 retry via seed-admin --refresh-only (max once); EC-12 preflight gh auth + rate_limit + kubectl context. Fail-fast per ADR D6 (no silent retry on chain links). --skip-live + --json + --use-push-event flags. Pure orchestration: gh CLI + curl + kubectl + psql; irreducible per D4 (bash). |

### Post-baseline additions (plan 2026-06-08-pre-release-test-rigor — T4.1 + T4.2)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan 2026-06-08-pre-release-test-rigor — T5.1)

| Script | LOC | Justificativa |
|---|---:|---|

### TEMPORARY — DELETE AFTER T6.1 UMBRELLA PASS (sponsor verbatim 2026-06-08) — CLOSED 2026-07-10

> Closed 2026-07-10 by M15 Wave A. `reset-for-e2e.sh` deleted per sponsor mandate;
> canonical replacement is Cleanroom TOTAL (`task clean:all && task setup`) +
> `seed-post-cleanroom-all.sh --verify` per `.claude/rules/cleanroom-up-live-test-mandate.md`.
> No table entries remain in this section.

### Post-baseline additions (plan 2026-06-06-dashboard-journey-j22-secrets-crud — T5.3)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan 2026-06-10-j22-openbao-transit-eso-sync — T0.1)

| Script | LOC | Justificativa |
|---|---:|---|
| infra/tests/e2e/live/dev-public-secrets-zero-leak_test.sh | ~280 | KEEP-OPERATOR 2026-06-10 — T4.1 of plan 2026-06-10-j22-openbao-transit-eso-sync v1.2. LIVE 6-scenario zero-leak proof per D5 ADR. Scenarios: A PG encrypted_value vault:v1: prefix, B workload K8s Secret plaintext, C pod printenv plaintext, D kubectl logs no sentinel leak, E kubectl describe pods no plaintext value leak, F sentinel ONLY in tenant ns Secret. Ephemeral UUID sentinel makes greps precise. EC-14 cleanup trap EXIT INT TERM. Flags --skip-live (hermetic CI mode) + --json + --scenario= + --help. OPERATOR HARD STOP — requires real OpenBao + ESO controller + loopzero-api wired with transit chain. |










| infra/scripts/local/vault-bootstrap-dev.sh | ~155 | KEEP-OPERATOR 2026-06-11 — T2.1 of plan 2026-06-11-cleanroom-canonical-5-defense-layers v1.2. Aggregates 10 canonical OpenBao Transit bootstrap manual steps (init + Shamir unseal × 3 + root token Secret + helmfile sync openbao-init + approle + loopzero-api policy with transit/keys/* wildcard + role_id + secret_id + loopzero-openbao-approle Secret + SPKI pin extraction) into one idempotent task. Closes catch #5 from T6.2 session. Stores shards + root_token at ~/.loopzero/cleanroom-unseal.json mode 0600. Live-cluster orchestration (kubectl exec bao + openssl SPKI extraction), irreducible per D4. |








### Post-baseline additions (plan 2026-06-12-dashboard-journey-j25-live-url-discoverability)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan 2026-06-06-dashboard-journey-j21-readiness)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan 2026-06-06-dashboard-journey-j11-domains)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan 2026-06-15-code-review-49-findings-remediation — T1.1+T1.2+T1.3)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan 2026-06-15-100pct-dashboard-coverage-roadmap)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (post-Cleanroom GitHub App canonical seed 2026-06-15)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (post-Cleanroom canonical chain expansion 2026-06-15)

| Script | LOC | Justificativa |
|---|---:|---|
| infra/scripts/local/seed-post-cleanroom-all.sh | ~120 | KEEP-OPERATOR 2026-06-15 (expanded 2026-06-16) — canonical POST-CLEANROOM master orchestrator. Aggregates the 12-step canonical chain (9 original + 3 NEW discovered during 2026-06-16 Cleanroom validation 12/12 PASS): (1) vault-bootstrap-dev / (2) seed-minio-canonical / (3) seed-admin-loopzero-dev / (4) patch-kratos-dev-public / (4b NEW) seed-hydra-urls-canonical / (5) seed-github-app-dev / (6) seed-gitea-token-canonical / (6b NEW) seed-argocd-gitea-creds-canonical / (7) seed-broker-canonical / (7b NEW) seed-vault-role-id-sync / (8) seed-loki-cross-cluster / (9) seed-tenant-fixture-canonical. Step order matters (deps): vault → minio → kratos+hydra → github → gitea+argocd → broker+vault-role → loki → fixtures. --verify cascades; --skip-vault for partial reruns; --dry-run prints chain; --json machine-readable verdict. Closes the canonical Cleanroom-survivable gap — without this, every `task clean:all && task setup` destroys 70% of operational state. Per cleanroom-up-live-test-mandate.md INQUEBRÁVEL. |

### Post-baseline additions (3 NEW canonical seeds discovered during Cleanroom validation 2026-06-16)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan 2026-06-16-m0-j01-login-polish — M0 Plan A v0.19.0)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan prepush-hook-and-api-crd-typed-schema — M8 1.5)

| Script | LOC | Justificativa |
|---|---:|---|
| infra/scripts/local/install-git-hooks.sh | ~15 | KEEP-OPERATOR 2026-06-17 — M8 1.5 idempotent installer copying in-repo git hooks into .git/hooks/ (hooks aren't versioned by git). Cheap CI substitute. Pure file copy via `install -m 0755`; irreducible (git-toplevel resolution). |

### Post-baseline additions (plan deploy-timeline-canonical-5-phases — T6.1+T6.2+T6.3)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan m1-commit-canonical-30-fixes — T1.1+T1.2+T1.3+T1.4)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (canonical fixture repo guard — 2026-06-24)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan m2-promote-11-canonical-seeds — Phase 1+3 — 2026-06-23)

| Script | LOC | Justificativa |
|---|---:|---|
| infra/scripts/local/seed-chaos-mesh-canonical.sh | ~65 | KEEP-OPERATOR 2026-06-23 — T1.1 of plan m2-promote-11-canonical-seeds v1.1 SHIPPABLE 95.2. Canonical chaos-mesh helm install em k3d-loopzero-core cluster com values-k3d.yaml socketPath override (per CLAUDE.md § Known issues). Idempotent: pre-check via `helm list` + helm upgrade --install upgrade-no-op semantic. `--verify` probes 6/6 pods Running. Per qa-readiness-roadmap M2 seed #1. Wired no master script Step 12. |

### Post-baseline additions (plan k6-load-test-rigor-fixes — 2026-06-23)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan stripe-l2-wiring-canonical-fix — 2026-06-24)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan qa-sprint5-cleanup-2026-06-25 — 2026-06-25)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan qa-sprint7-cleanup-2026-06-25 — 2026-06-25)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan qa-sprint8-cleanup-2026-06-25 — 2026-06-25)

> Sprint 8 batch retroactively closes the canonical 3-script triplet debt for
> Sprint 6 (#34 — discriminated BUILD_SUBMIT_FAILED reasons) and Sprint 7
> (#35 info-disclosure scrub + #36 build RBAC). Per the INQUEBRÁVEL feedback
> `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` (sponsor verbatim
> 2026-06-25): every issue closure now requires hermetic syntax + LIVE bash
> + dogfood wrapper triplet — not just Go-side tests.

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan m37-tenant-cr-project-sync-canonical-fix — 2026-06-25)

> Plan m37 closes the second root cause of loopzero-cloud#37 (cenário 4 BUILD → RELEASE → URL chain) that Sprint 8 missed. Sprint 8 fixed the engine watcher namespace-scoped LIST; m37 fixes the EnvironmentLister project-agnostic shallow check that was silently swallowing new-project deploys against existing tenants → Tenant CR stale → AppProject sync fail. Per the INQUEBRÁVEL feedback_issue_fix_requires_failing_e2e_test_2026_06_25 every issue closure now requires hermetic syntax + LIVE bash + dogfood wrapper triplet — the closure-with-LIVE-PROOF is the contract.

| Script | LOC | Justificativa |
|---|---:|---|
| infra/tests/e2e/live/dev-public-deploy-live-url_test.sh | ~280 | KEEP-OPERATOR 2026-06-25 — T3.1 of plan m37-tenant-cr-project-sync-canonical-fix v1.0 (closes loopzero-cloud#37 cenário 4). 4 LIVE scenarios A-D for the BUILD → RELEASE → URL chain: A asserts Tenant CR `spec.environments[]` gains the new (project, env) tuple after POST /api/v1/builds for a NEW project on existing tenant; B asserts ArgoCD AppProject `<tenant>.spec.destinations[].namespace` extended to `loopzero-<tenant>-<project>-<env>` within 60s; C asserts release reaches ACTIVE with non-empty url within 30min CLI ceiling; D asserts HTTP probe of Live URL returns 200..399. Wait budget configurable via `LOOPZERO_DEPLOY_LIVE_URL_WAIT_BUDGET` (default 1800s). Unique probe project per run (`m37-probe-<epoch>` default; `LOOPZERO_DEPLOY_LIVE_URL_PROJECT` override). OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md INQUEBRÁVEL. |

### Post-baseline additions (plan caddyfile-csp-vite-dev-vs-prod-bundle — 2026-06-26)

> Originally shipped as `infra/tests/check-caddyfile-app-dev-no-csp_test.sh` (T1.1 RED→GREEN). Migrated to Go gate `pkg/quality/caddyfilecspgate/` per `unhomed-logic` gate canonical pattern (the project rule `2026-05-23-operational-logic-capability-architecture` forbids increasing un-homed test logic). Wired via `loopzero-ops quality caddyfile-csp` + `taskfiles/quality.yml`. The shell test was deleted in the same commit set as the Go gate landing. Allowlist no longer carries an entry for this gate (Go-homed, not bash).

### Post-baseline additions (Red Team #65 closure — 2026-06-26)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team #64 closure — 2026-06-26)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan kratos-self-service-rate-limit-systemic — 2026-06-27)

| Script | LOC | Justificativa |
|---|---:|---|
| infra/scripts/local/caddy-build-canonical.sh | ~140 | KEEP-OPERATOR 2026-06-27 — T1.1 of plan kratos-self-service-rate-limit-systemic v1.1 (EC-1 + EC-6 absorbed). Canonical xcaddy builder producing a SINGLE Caddy binary that bundles BOTH `caddy-dns/cloudflare` (DNS-01 for wildcard cert J-23 #68) AND `mholt/caddy-ratelimit@5625512` (edge rate-limit for Red Team #68/#89/#43). EC-1: a separate single-module builder would OVERWRITE the existing canonical binary and DROP cloudflare DNS support → wildcard cert renewal would break silently. EC-6: caddy-ratelimit pinned to explicit commit SHA `5625512f24f6f59d6f64fb3aafe5eecff0b286db` (HEAD verified on-disk at .claude/knowledge-base/references/caddy-ratelimit/, dated 2026-06-12) — upstream has not yet shipped a semver tag; commit SHA is canonical pin. Idempotent skip-if-BOTH-modules-bundled, `CADDY_FORCE_REBUILD=1` opt-out. Live-cluster orchestration (xcaddy build + go install + sudo install), irreducible per D4. |
| infra/scripts/local/caddy-build-with-cloudflare.sh | 18 | KEEP-OPERATOR 2026-06-27 — replaces grandfathered J-23 #68 entry; bumped from 106 LoC standalone to 18-LoC deprecation shim per T1.1 EC-1 of plan kratos-self-service-rate-limit-systemic v1.1. Shim preserves the existing operator path (referenced by allowlist + memory + runbooks) by transparently `exec`ing the canonical builder. Old standalone form (caddy-build-with-cloudflare.sh bundling ONLY cloudflare module) is superseded — calling it now invokes the canonical bundler so cloudflare + ratelimit are both bundled. NO behavior break for operators running `bash infra/scripts/local/caddy-build-with-cloudflare.sh`. |
| infra/tests/e2e/live/dev-public-login-rate-limit_test.sh | ~135 | KEEP-OPERATOR 2026-06-27 — T1.4 of plan kratos-self-service-rate-limit-systemic v1.1 closes Red Team loopzero-cloud#68 login rate-limit gap. Reproduces #68 verbatim via the canonical 2-step pattern (EC-5 absorption): GET /self-service/login/api → flow_id; burst 30 POSTs to /self-service/login/methods/password?flow=<id>; asserts ≥ 5 of 30 return 429 + Retry-After header. EC-8 pre-flight JWT seed check + actionable BLOCKED hint. EC-12 doc-comment: safe to run in parallel with dev-public-recovery-rate-limit_test.sh (distinct bucket zones). --skip-live (hermetic CI) + --json + --scenario=A\|B + --help flags. Live-cluster orchestration (curl + jq); irreducible per D4. |
| infra/tests/e2e/live/dev-public-recovery-rate-limit_test.sh | ~155 | KEEP-OPERATOR 2026-06-27 — T1.5 of plan kratos-self-service-rate-limit-systemic v1.1 closes Red Team loopzero-cloud#89 recovery email-bomb + brute-force code guess. Asserts BOTH recovery buckets engage: Scenario A (recovery_flow_create_throttle 5/5min) — 6 sequential GETs to /self-service/recovery/browser → 6th MUST be 429; Scenario B (recovery_throttle 3/1min) — GET /recovery/api → flow_id, 4 POSTs to /recovery/methods/code → 4th MUST be 429 + Retry-After. EC-5 canonical endpoint pattern + EC-8 JWT seed pre-flight + EC-12 parallel-safe doc. --skip-live + --json + --scenario=A\|B + --help flags. |
| infra/tests/e2e/live/dev-public-redteam-canonical-paths_test.sh | ~205 | KEEP-OPERATOR 2026-06-27 — Red Team loopzero-cloud#68 + #43 post-fix-verification regression-protector. Reproduces EXACT attack vector the Red Team used after the first-round fixes only covered `/self-service/{login,registration}/methods/*` and missed the canonical browser-flow submit paths `/self-service/{login,registration}` (no `/methods/` suffix — the `ui.action` URL emitted by `GET /self-service/{login,registration}/browser`). Scenario A: 12 wrong-pwd POSTs to canonical `/self-service/login` → asserts ≥ 2 × 429 (login_throttle engages on canonical path). Scenario B: browser flow new vs existing email POST to `/self-service/registration` with cookie-jar + CSRF → asserts byte-identical universal 200 (BFF unifier active on canonical path; no enumeration). OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md INQUEBRÁVEL. --skip-live + --json + --scenario= + --help. |

### Post-baseline additions (Red Team #112 closure — 2026-06-28)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan red-team-77-107-closure — 2026-06-28)

| Script | LOC | Justificativa |
|---|---:|---|
| infra/tests/e2e/live/dev-public-auth-email-delivery_test.sh | ~270 | KEEP-OPERATOR 2026-06-29 — Red Team loopzero-cloud#117 closure regression-protector. 3 LIVE scenarios (A anti-enumeration mute for unknown address / B known admin@loopzero.example recovery enqueues + Resend delivered / C fresh signup enqueues + Resend sent) prove the Kratos→Resend pipeline survives Cleanroom in dev-public. Probes Kratos public API + courier_messages PG table + Resend list endpoint (`api.resend.com/emails`). RESEND_API_KEY derived from `~/.loopzero/dev-credentials.json` OR live ConfigMap Bearer token. OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md — requires Cleanroom TOTAL + seed-admin-loopzero-dev.sh + patch-kratos-dev-public.sh. Flags `--skip-live` (hermetic CI mode) + `--scenario=A|B|C` + `--json` + `--help`. Pure orchestration: curl + kubectl + psql + python3. |

### Post-baseline additions (plan seed-post-cleanroom-tenant-cr-provisioning — 2026-06-29)

| Script | LOC | Justificativa |
|---|---:|---|
| infra/tests/e2e/live/dev-public-seed-post-cleanroom-tenant-cr_test.sh | ~180 | KEEP-OPERATOR 2026-06-29 — closes Red Team loopzero-cloud#118 (QA Red Team identity loga em dev-public mas Tenant CR ausente em k3d-loopzero-core → deploys falham). 2 LIVE scenarios: A admin@loopzero.example Tenant CR (tn-78d5c2d4f678b774) Active + B paulohenrique.java@gmail.com Tenant CR (tn-79611071399a2e5e) Active (THE BUG — pre-fix this scenario returns NotFound). Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL: LIVE bash que reproduz o issue verbatim BEFORE the fix é o canonical contract. Pre-flight: kubectl + context loopzero-core + tenants.loopzero.io CRD installed. Flags `--skip-live` (hermetic CI mode) + `--scenario=A\|B` + `--json` + `--help`. OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md. Pure orchestration: kubectl + bash; irreducible. |

### Post-baseline additions (plan loopzero-promote-no-op-fail-loud-p0 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#121 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#124 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#123 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#122 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#98 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan c13-valid-destructive-config-chaos — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|
### Post-baseline additions (Red Team loopzero-cloud#102 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#96 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#97 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#71 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#73 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#74 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#75 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#76 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#78 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#79 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#80 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#81 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#82 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#83 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#104 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#51 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#56 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#55 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#50 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#42 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#40 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#111 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#126 — 2026-06-30)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan dashboard-badge-data-plane-poller — Red Team loopzero-cloud#128 — 2026-07-01)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#127 — 2026-07-01)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero/loopzero#154 — 2026-07-01)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero/loopzero#155 — 2026-07-01)

| Script | LOC | Justificativa |
|---|---:|---|
| infra/tests/e2e/live/dev-public-billing-stripe-price-ids-seed-canonical_test.sh | ~220 | KEEP-OPERATOR 2026-07-01 — closes Red Team loopzero/loopzero#155 (MAJOR: `POST /v1/billing/checkout {"plan":"pro"}` retorna `503 BILLING_NOT_CONFIGURED` porque STRIPE_PRICE_{STARTER,PRO,TEAM} env vars unset no loopzero-cloud; jornada plan_upgrade morta pós-#154). Follow-up de #154. Root cause: `internal/billing/plans.go::PriceIDForPlan` chama `os.Getenv("STRIPE_PRICE_<PLAN>")` — retorna "" → `internal/routes/billing.go:216` emite 503 tipado. Fix: `loopzero-ops seed stripe-api-keys` (M15 W1) lê `stripe.price_{starter,pro,team}` do dev-credentials.json e materializa como env vars no Secret `loopzero-stripe-api-keys` alongside STRIPE_API_KEY (canonical LoopZero Pro `price_1TJGIuA0ZwssSKrVEMKZsiMj` + LoopZero Team `price_1TJGJ5A0ZwssSKrVy3mjsal6` discovered via Stripe API test-mode listing; STARTER vazio por design — free tier). LIVE 3-scenario: A Secret tem `STRIPE_PRICE_PRO` não-vazio; B pod envFrom referencia loopzero-stripe-api-keys (distroless — sem printenv, verificação por config-level); C `POST /v1/billing/checkout {"plan":"pro"}` NÃO retorna `503 BILLING_NOT_CONFIGURED` (canonical: HTTP 200 com Stripe checkout URL). Flags `--skip-live` + `--scenario=A\|B\|C` + `--json` + `--help`. OPERATOR HARD STOP. |

### Post-baseline additions (Red Team loopzero-cloud#156 — 2026-07-01)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#157 — 2026-07-01)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#158 — 2026-07-01)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#159 — 2026-07-01)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#160 — 2026-07-01)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero-cloud#161 — 2026-07-01)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero/loopzero#162 — 2026-07-01)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (Red Team loopzero/loopzero#163 — 2026-07-01)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (M0 J-02 register journey triplet — 2026-07-03)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (M0 J-03 verification journey triplet — 2026-07-03)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (M0 J-01 patch-kratos read-only gate — 2026-07-03)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (plan 2026-06-06-dashboard-journey-j06-overview — 2026-07-03)

| Script | LOC | Justificativa |
|---|---:|---|

### Post-baseline additions (canonical post-Cleanroom GitHub webhook seed — 2026-07-04)

| Script | LOC | Justificativa |
|---|---:|---|

### infra/tests taxonomy refactor — secrets/ + platform/ domain homing (2026-07-11)

> Renames only (git mv, history preserved). The 15 root `infra/tests/check-*_test.sh`
> were homed into domain folders: 14 vault/secrets/eso/kubeconfig into
> `infra/tests/secrets/`, 1 namespace-migration into `infra/tests/platform/`.
> `taskfiles/quality.yml` invocations updated in lockstep. `unhomedlogic` gate
> unaffected (paths keep the `infra/tests/` prefix). Part of the infra/tests
> taxonomy refactor (review 2026-07-11). No behavior change.

| Script | LOC | Justificativa |
|---|---:|---|
| infra/tests/secrets/check-enable-vault-backup_test.sh | 232 | domain homing 2026-07-11 — moved from repo root; wired in quality:gates (OpenBao snapshot backup) |
| infra/tests/secrets/check-generate-secrets-vault-fallback_test.sh | 88 | domain homing 2026-07-11 — moved from root; cold-bootstrap Vault-skipped fallback |
| infra/tests/secrets/check-promtail-vault-empty-targets_test.sh | 92 | domain homing 2026-07-11 — moved from root; promtail vault-audit filter when NS absent |
| infra/tests/secrets/check-promtail-vault-scrape_test.sh | 102 | domain homing 2026-07-11 — moved from root; promtail secrets-system scrape + Grafana |
| infra/tests/secrets/check-restore-vault-snapshot_test.sh | 218 | domain homing 2026-07-11 — moved from root; OpenBao DR restore dry-run |
| infra/tests/secrets/check-rotate-secret_test.sh | 249 | domain homing 2026-07-11 — moved from root; cross-binary atomic rotation |
| infra/tests/secrets/check-rotation-table-parity_test.sh | 149 | domain homing 2026-07-11 — moved from root; ROTATION_TABLE vs ESO inventory parity |
| infra/tests/secrets/check-scoped-kubeconfig-cold-bootstrap_test.sh | 80 | domain homing 2026-07-11 — moved from root; cold-bootstrap legacy SA-token block |
| infra/tests/secrets/check-scoped-kubeconfig-vault_test.sh | 95 | domain homing 2026-07-11 — moved from root; Vault k8s engine SA token fetch |
| infra/tests/secrets/check-seed-loopzero-secrets_test.sh | 321 | domain homing 2026-07-11 — moved from root; idempotent Vault seed |
| infra/tests/secrets/check-loopzero-api-no-cluster-secrets_test.sh | 109 | domain homing 2026-07-11 — moved from root; legacyClusterWideSecrets flag |
| infra/tests/secrets/check-loopzero-api-vault-projected-token_test.sh | 164 | domain homing 2026-07-11 — moved from root; projected SA token audience=vault.loopzero.io |
| infra/tests/secrets/check-vault-bootstrap-eso_test.sh | 199 | domain homing 2026-07-11 — moved from root; ESO ClusterSecretStore bootstrap |
| infra/tests/secrets/check-vault-sync_test.sh | 230 | domain homing 2026-07-11 — moved from root; ESO ExternalSecret sync status |

### Post-baseline additions (DELEGATION-M7-rag-ui — T1+T2 durable RAG dashboard wiring — 2026-07-16)

| Script | LOC | Justificativa |
|---|---:|---|
| infra/tests/check-loopzero-cloud-rag-dashboard-wiring_test.sh | ~120 | KEEP-CI-ORCHESTRATOR 2026-07-16 — hermetic gate closing DELEGATION-M7-rag-ui.md T1+T2. Renders `helmfile -e dev-public template --selector name=loopzero-cloud` and YAML-parses via python3 stdlib (`yaml.safe_load_all`) to assert 5 wiring invariants on the loopzero-cloud Deployment + rendered ExternalSecret: (A1) env RAG_BASE_URL literal non-empty http(s):// URL; (A2) env RAG_PLATFORM_ADMIN_KEY sourced via valueFrom.secretKeyRef name=loopzero-rag-admin-key key=RAG_PLATFORM_ADMIN_KEY optional=true; (A3) chart-owned template file `charts/loopzero-cloud/templates/externalsecret-admin-keys.yaml` present; (A4) rendered ExternalSecret targets Secret loopzero-rag-admin-key from OpenBao path loopzero/cloud property rag_platform_admin_key; (A5) target.creationPolicy=Owner (anti-orphan). Guards regression to the volatile-`kubectl set env` state that caused `/v1/dashboard/rag/*` → 404 (breaks app-dev.loopzero.example/rag/analytics). Hermetic, no live cluster — pairs with LIVE `kubectl get externalsecret loopzero-rag-admin-key` post-sync + pod-env probe (operator gate). |
| infra/tests/check-loopzero-cloud-lens-dashboard-wiring_test.sh | ~130 | KEEP-CI-ORCHESTRATOR 2026-07-20 — hermetic gate for the loopzero-lens dev-public dashboard wiring, sibling of the RAG gate above (same class, same toolchain). Renders `helmfile -e dev-public template --selector name=loopzero-cloud` and `yq`-asserts the same 4 wiring invariants for lens: (A1) env LENS_BASE_URL non-empty http(s):// URL (dev-public → `http://host.k3d.internal:18084`, the loopzero-lens host process port); (A2) env LENS_PLATFORM_ADMIN_KEY via valueFrom.secretKeyRef name=loopzero-lens-admin-key key=LENS_PLATFORM_ADMIN_KEY optional=true; (A3) chart template `charts/loopzero-cloud/templates/externalsecret-admin-keys.yaml` present; (A4) rendered ExternalSecret targets Secret loopzero-lens-admin-key from OpenBao path loopzero/cloud property lens_platform_admin_key + creationPolicy=Owner. Without LENS_BASE_URL the loopzero-lens Model-B broker in cmd/main.go stays nil and the `/v1/dashboard/lens/*` (M41/M44) BFF is skipped (breaks app-dev.loopzero.example/lens). Hermetic, no live cluster. |

### Post-baseline additions (plan b033-prometheus-url-dev-public — 2026-08-31)

| Script | LOC | Justificativa |
|---|---:|---|
| infra/tests/check-loopzero-cloud-cost-aggregator-wiring_test.sh | 88 | KEEP-CI-ORCHESTRATOR 2026-08-31 — B-033 T1.1 hermetic gate. Renders `helmfile -e dev-public template --selector name=loopzero-cloud` + `loopzero-app-operator` and asserts A1/A2 (LOOPZERO_PROMETHEUS_URL present with canonical value `http://prometheus-server.monitoring.svc.cluster.local:80`) + A3 (parity byte-idêntica com loopzero-app-operator.yaml.gotmpl:28 — absorve EC-1 do edge-case-plan). Sem essa variável, o cost aggregator em loopzero-cloud/cmd/main.go boot-desliga com `logger.Info("cost aggregator disabled")`, `cost_counters` fica vazia, e o CostEnforcer no api/ degrada para pass-through — os tetos de build/egresso/pulls ficam sem enforcement no dev-public. Wire em `taskfiles/quality.yml § quality:gates` (T2.2 do plano). Hermetic, sem cluster. Pattern canonical dos irmãos `check-loopzero-cloud-{rag,lens}-dashboard-wiring_test.sh`. D4 criteria: Necessary (guarda regressão real da causa raiz de B-033) + Simple (88 LoC, pattern replicado) + Operator-time (hermético, sem cluster live) + Allowlisted (esta entrada). |

### Kit-sync gap closure (commit 63cc51c48 landed .sh files without allowlist rows — 2026-08-31)

| Script | LOC | Justificativa |
|---|---:|---|

### Kit harness scripts — caminho `mechanisms/` (allowlist alinhado 2026-09-07)

O kit renomeou `.claude/scripts/` para `.claude/mechanisms/` e este arquivo continuou apontando para os caminhos antigos: 23 linhas nomeavam `.sh` que nao existem mais (varias delas hooks que o kit migrou para `.py`), enquanto os 12 scripts reais nao tinham linha nenhuma e reprovavam o gate `no-new-bash`. As linhas mortas foram removidas e estas sao as vivas. Mesma categoria KEEP-HARNESS-HOOK dos irmaos acima: sao do plugin instalado, nao do produto.

| Script | LOC | Justificativa |
|---|---:|---|
| .claude/mechanisms/cycle/attest_plan.sh | 133 | atesta o SHA256 de um plano lockado antes do /implement; operator-time |
| .claude/mechanisms/cycle/run_gates.sh | 128 | roda a bateria de gates de uma slice durante o halt-loop; operator-time |
| .claude/mechanisms/cycle/run_slice_tests.sh | 126 | roda a suite de uma slice durante o halt-loop; operator-time |
| .claude/mechanisms/distribution/install.sh | 966 | instalador do kit no projeto consumidor; operator-time |
| .claude/mechanisms/distribution/patch_install.sh | 402 | atualizacao incremental do kit; operator-time |
| .claude/mechanisms/fleet/dispatch_to_lane.sh | 100 | despacha um item para uma lane da fleet; operator-time |
| .claude/mechanisms/fleet/fleet_status.sh | 168 | le o estado das lanes da fleet; leitura apenas |
| .claude/mechanisms/fleet/fleet_wall.sh | 116 | painel de acompanhamento das lanes; leitura apenas |
| .claude/mechanisms/fleet/run_remote.sh | 59 | executa uma lane numa maquina remota; operator-time |
| .claude/mechanisms/fleet/start_fleet.sh | 149 | sobe as sessoes da fleet; operator-time |
| .claude/mechanisms/fleet/start_lead_session.sh | 142 | sobe a sessao lead da fleet; operator-time |
| .claude/mechanisms/fleet/statusline.sh | 47 | statusline do Claude Code; puramente cosmetico |

### Scripts de infra sem linha no allowlist (inventario alinhado 2026-09-07)

O cabecalho deste arquivo diz que ele e a fonte parseada do gate; um script que existe e nao esta listado torna essa afirmacao falsa. O gate `no-new-bash` so bloqueia o que e ADICIONADO num diff, entao estes passavam sem aparecer.

Cada linha carrega o marcador de disposicao que a **medicao** sustenta, nao um carimbo em bloco: `unhomed-logic` le a coluna Justificativa linha a linha, e uma versao anterior desta secao afirmava aqui no cabecalho que "todos sao orquestracao live-cluster" — falso para `mock-kubectl.sh`, que e um stub que sai 0 e nunca fala com cluster nenhum, e para os tres que so envolvem CLI externo. `KEEP-OPERATOR` = invoca kubectl/bao/helm/aws contra ambiente vivo; `KEEP-CI-ORCHESTRATOR` = envolve gh/npm/go/node/curl/psql sem tocar cluster (anti-pattern-8); `KEEP-CI-WIRED` = o script E o proprio andaime hermetico, sem rede. Nenhum dos 19 tem heredoc Python (medido), entao nenhum e um programa Python vestido de bash.

| Script | LOC | Justificativa |
|---|---:|---|
| infra/helmfile/scripts/lib/vault-k8s-engine-token.sh | 79 | KEEP-OPERATOR — lib sourced pelo bootstrap do OpenBao k8s auth engine; invoca bao/kubectl contra cluster vivo, irredutivel per D4 |
| infra/helmfile/scripts/test_helpers/mock-kubectl.sh | 54 | KEEP-CI-WIRED — stub de kubectl para os testes hermeticos dos scripts de helmfile: registra a invocacao em log e sai 0, zero rede e zero cluster. Nao e ferramenta de operador |
| infra/scripts/local/bootstrap-themory-key.sh | 156 | KEEP-OPERATOR — provisiona a chave do loopzero-memory; kubectl/bao contra cluster vivo |
| infra/scripts/local/create-test-api-key.sh | 89 | KEEP-CI-ORCHESTRATOR — cria API key de teste via curl/psql; zero kubectl (anti-pattern-8) |
| infra/scripts/local/preflight-themory.sh | 91 | KEEP-OPERATOR — pre-flight do loopzero-memory antes do smoke; kubectl contra cluster vivo |
| infra/scripts/local/smoke-dashboard-memory-bff-e2e.sh | 181 | KEEP-OPERATOR — smoke e2e LIVE do BFF de memory no dashboard; kubectl + curl contra ambiente vivo |
| infra/scripts/local/smoke-themory-e2e.sh | 153 | KEEP-CI-ORCHESTRATOR — smoke e2e do loopzero-memory por curl; zero kubectl (anti-pattern-8) |
| infra/scripts/local/start-themory.sh | 142 | KEEP-CI-ORCHESTRATOR — sobe o processo local do loopzero-memory envolvendo go/node/npm; zero kubectl (anti-pattern-8) |
| infra/scripts/local/stop-themory.sh | 66 | KEEP-OPERATOR — encerra o processo local por PID file + SIGTERM/SIGKILL; sinais e PID file sao primitiva do shell (escada da parcimonia, degrau 3), nao logica reimplementavel em Go |
| infra/scripts/local/validate-themory-wireup.sh | 98 | KEEP-OPERATOR — valida o wiring do loopzero-memory; kubectl contra cluster vivo |
| infra/scripts/openbao/enable-vault-backup.sh | 249 | KEEP-OPERATOR — habilita snapshot backup do OpenBao; bao/kubectl contra cluster vivo, irredutivel |
| infra/scripts/openbao/init-ceremony.sh | 153 | KEEP-OPERATOR — cerimonia Shamir de init do OpenBao; sponsor-witnessed, bao contra cluster vivo, irredutivel |
| infra/scripts/openbao/migrate-pulumi-secrets-to-vault.sh | 130 | KEEP-OPERATOR — migracao one-shot de secrets Pulumi para o OpenBao; bao/pulumi contra ambiente vivo |
| infra/scripts/openbao/provision-aws-resources.sh | 470 | KEEP-OPERATOR — provisiona os recursos AWS de auto-unseal via CLI da AWS contra a conta real; operator-time, irredutivel |
| infra/scripts/openbao/restore-vault-snapshot.sh | 254 | KEEP-OPERATOR — restore DR de snapshot do OpenBao; bao/kubectl contra cluster vivo, irredutivel |
| infra/scripts/openbao/rotate-secret.sh | 317 | KEEP-OPERATOR — rotacao atomica cross-binary de um secret; bao/kubectl contra cluster vivo |
| infra/scripts/openbao/seed-loopzero-secrets.sh | 187 | KEEP-OPERATOR — seed idempotente do Vault; bao contra cluster vivo |
| infra/scripts/openbao/vault-bootstrap-eso.sh | 241 | KEEP-OPERATOR — bootstrap do ClusterSecretStore do ESO; bao/kubectl contra cluster vivo |
| infra/scripts/openbao/vault-sync-check.sh | 214 | KEEP-OPERATOR — checa o status de sync dos ExternalSecrets; kubectl contra cluster vivo |
