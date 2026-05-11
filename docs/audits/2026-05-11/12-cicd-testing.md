# Module 12 — CI/CD and Testing (`.github/workflows/` + `infra/tests/`)

**Audit Date:** 2026-05-11
**Auditor:** Claude Opus 4.6 (1M context), code-validated
**Confidence:** 90%
**Maturity:** **3.5 / 4.0**

## TL;DR

**35 workflows** (não 29 como CLAUDE.md afirma — 6 a mais) + **89 test files / ~24,729 LOC** em `infra/tests/`. Disciplina sólida: integration-tests promoted to PR gate, auto-issue creation em soak failure, immutable `:sha` tags (ADR-0049). Bloqueia 4.0 por **2 gates específicos pendentes**: `cell2-soaks.yml` first GREEN cron + 1 prod customer day-trial 24h. Ambos manuais/scheduled, fora do escopo de engineering.

---

## 1. Inventário Real

| Categoria | Workflows | Lista |
|---|---|---|
| CI & Quality Gates | 7 | ci.yml, integration-tests.yml, cli-commands.yml, status-pr-gate.yml, parity-validation.yml, helm-charts.yml, status-refresh.yml |
| Build & Images | 2 | build-images.yml, release.yml |
| Infrastructure | 8 | infra-e2e, infra-bootstrap, infra-deploy, infra-destroy, infra-drift, infra-restart, infra-state-reset, infra-gates |
| **Soaks (cron)** | 5 | **cell2-soaks.yml** (Sun), operator-split-soak (Sat), pkg-soak (Fri), load-test (Mon), load-test-pr |
| Health & Monitoring | 4 | platform-health-7day-watch, decay-scan, velero-backup-check, charts-drift-cron |
| Validation & Parity | 2 | cross-validation-weekly, adversarial-spotcheck |
| Reusable Actions | 3 | reusable-setup-go/helm/node |
| Pulumi | 2 | pulumi-validate, pulumi-archive |
| Multi-Cell | 1 | multi-cell.yml |
| 1 Cell Routing | 1 | (incluído acima) |
| **TOTAL** | **35** | **(CLAUDE.md diz 29 — drift de +6)** |

**Test infrastructure:**

| Path | Files | LOC | Purpose |
|---|---|---|---|
| `infra/tests/e2e/` | 63 | ~6,200 | 19 journey + 44 cli_* + edge + helpers |
| `infra/tests/load/` | 11 | ~3,700 | Cell scale (1080), regression (1742), k3d integration, real_deploy harness |
| `infra/tests/unit/` | 14 | ~1,400 | Helm values, ArgoCD, RBAC, operator cutover, split operator invariants |
| `infra/tests/chaos/` | 1 + 10 manifests | 388 | Chaos Mesh: pod kill, network partition, DB storm |
| **Total** | **89** | **~24,729** | |

**Taskfile:** 165 tasks (Taskfile.yml + 6 includes: test, infra, helm, dev, db, k3d).

---

## 2. Arquitetura Observada

**Cron schedules (UTC):**
- Sunday 02:00 → **`cell2-soaks.yml`** (Stage 1 1h synthetic + Stage 2 24h, 10% tenants) — **gate 4.0**
- Saturday 02:00 → operator-split-soak (4h soak + Chaos Mesh mid-deploy)
- Friday 02:00 → pkg-soak (4h, RFC 9457 + CB recovery + SLO P95<705ms)
- Monday 06:00 → infra-e2e (destroy→up→bootstrap→health, ~$2/run) + load-test (L0 baseline)
- Nightly 03:00 → integration-tests (API + PG invariants)
- Daily 04:00 → adversarial-spotcheck (selection-only, awaits manual operator)
- Hourly → platform-health-7day-watch (canary probes vs prod)
- Quarterly → pulumi-archive (AWS S3 backup)

**Soak workflow patterns:**
- Auto-issue creation em failure (`priority/critical`, `area/multi-cell` labels)
- Preflight dry-run em cada soak (kubeconfig connectivity, prerequisites)
- Runbook closure em sucesso (e.g., `cell2-soaks.yml:112-127` appende evidência a `docs/runbooks/multi-cell-canary.md`)

**CI gate aggregation** (`ci.yml`):
- `ci-gate` job = aggregate de lint-go + lint-cli + tidy-go + test-pkg/api/operators/infra/cli/pulumi + validate-manifests + lint-charts + migration-safety + security
- PR merge requer ci-gate GREEN

**Integration-tests T4.8 promoção:** Era nightly-only; agora PR gate em `api/**` ou `pkg/**` changes (deep-review-remediation T4.8).

**Build promotion (ADR-0049):**
- Mutable tags: `:dev`, `:staging`, `:prod`
- **Immutable**: `:${SHA7}` (audit trail; production deploys MUST reference `:sha`)
- build-images.yml gated em CI success (workflow_run)

**Helm publishing** (cross-cut M06): 9 blocking + 4 advisory gates, matrix K8s 1.30/1.31/1.32, dual cosign signing + SBOM via syft.

**E2E Ginkgo journey suite (LIVE):**
- Latest runs: 76/76 GREEN (2026-05-06), 80/80 GREEN canary (2026-05-07), 74/75 user-dockerfile (2026-05-08), 77/78 cleanroom (2026-05-09), 83/83 ArgoCD sync resilience (2026-05-08)

---

## 3. Contratos Públicos

**Required PR merge checks:**
1. ci-gate (12 sub-jobs aggregated)
2. integration-tests (api/** or pkg/** changes)
3. cli-commands (cli/** changes, mock API)
4. parity-validation (infra/tests/parity/** changes)
5. helm-charts (charts/** changes)

**GitHub Secrets:**
- `DIGITALOCEAN_TOKEN` (infra-deploy, build-images, cell2-soaks)
- `CLOUDFLARE_API_TOKEN` (infra-e2e, infra-deploy)
- `KUBECONFIG_DEV`, `KUBECONFIG_DEV_MGMT` (soaks)
- `PULUMI_CONFIG_PASSPHRASE`, `PULUMI_ACCESS_TOKEN` (infra-e2e, infra-deploy)
- `SPACES_ACCESS_KEY_ID`, `SPACES_SECRET_ACCESS_KEY` (Pulumi S3 backend)
- `LOOPZERO_API_URL_DEV`, `PROM_URL_DEV`, `LOOPZERO_ADMIN_SERVICE_AUTH_DEV` (soaks)
- `COSIGN_KEY`, `COSIGN_PASSWORD` (release.yml, future)
- `SLACK_WEBHOOK_URL` (alerting)
- `GITHUB_TOKEN` (implicit)

**Release flow:** `git tag v*.*.*` → release.yml → extract CHANGELOG section → GitHub Release → trigger build-images.yml → push DOCR `:prod` + `:${SHA7}`.

---

## 4. Cobertura de Testes

**E2E journey suite (19 journeys):**
admin, autodeploy, connect, custom_domain, failures, logs, multi_server, platform_upgrade, preview, preview_ttl, promote, rollback, rollout, scale, secrets, solo_deploy, team_add, user_dockerfile, velero_dr.

**CLI tests (44 cli_*):** audit, billing, build inspect/list, cancel, connect, domain ops, env CRUD, login/logout, logs, platform ops, project CRUD, promote, rollback, secret ops, status, team ops, whoami, validate, preview, helpers.

**Chaos scenarios (10 Chaos Mesh manifests):** pod kills (api-leader, controller, simultaneous), network partitions (build cluster), DB storms (connection exhaustion, latency), service outages (ArgoCD, Gitea), infrastructure (nodes).

**Load test levels:**
- L0: single deploy baseline (~20min)
- L1: 10 tenants concurrent + webhook burst
- L2: 50 tenants sustained
- L3: 100 tenants concurrent (milestone)
- L4: lock stress (50 concurrent same project)

**LIVE evidence:**
- 76/76 journey GREEN (2026-05-06)
- 80/80 canary GREEN (2026-05-07)
- 12/12 cleanroom GREEN (2026-05-05)
- 77/78 cleanroom post-remediation (98.7%, 2026-05-09)
- 83/83 ArgoCD sync resilience (2026-05-08)

---

## 5. Gaps e Dívida Técnica

| Item | Severidade | Locação/Evidência |
|---|---|---|
| **`cell2-soaks.yml` FIRST GREEN pendente** | **HIGH (4.0 GATE)** | Workflow deployed, awaiting first successful Sunday cron + artifacts |
| **1 prod customer day-trial pendente** | **HIGH (4.0 GATE)** | Manual process; no workflow automation |
| **CLAUDE.md drift** (29 → 35 workflows) | BAIXA | Documentation |
| Mutable `:prod` tag risk | MÉDIA | ADR-0049 — convention only, sem CI gate enforcement |
| Sem flake tracking dashboard | MÉDIA | Manual RCA required em journey_rollout, journey_velero_dr |
| 6 TODOs Renovate (action SHA pinning) | BAIXA | ci.yml:54,120,365,554 (T4.1 deep-review) |
| Sem state backup automation | BAIXA | pulumi-archive é manual (quarterly) |
| Sem explicit secret rotation policy | BAIXA | Workflows |
| Sem CRD drift automatizado | BAIXA | crd-drift job em PR-trigger only |
| Local k3d cert-manager letsencrypt-prod gap | BAIXA (local only) | Production CF-managed, sem impact |
| HTTP 000 probes em `--smoke` mode | BAIXA (informational) | Stage 1 canary local |

---

## 6. Avaliação de Maturidade

**Score: 3.5 / 4.0** (confidence 90%)

### Evidências

1. **T1 (commit+test)** — 35 workflows em `.github/workflows/`; 89 test files em `infra/tests/` (~24.7K LOC); `integration-tests.yml` T4.8 promoted to PR gate.
2. **T2 (LIVE log)** — Journey 76/76 GREEN (2026-05-06); canary 80/80 GREEN (2026-05-07); cleanroom 12/12 GREEN (2026-05-05); ArgoCD sync resilience 83/83 GREEN (2026-05-08); cleanroom post-remediation 77/78 (2026-05-09).
3. **T3 (CI run)** — CI workflow blocking gates aggregated em `ci-gate` job; soaks scheduled (Fri/Sat/Sun + Mon baseline); auto-issue creation on soak failure.

### Raciocínio

**Sustenta 3.0-3.5:**
- 35 workflows + 89 test files cobrindo CI/CD/soak/chaos/load comprehensively
- Integration-tests promoted to PR gate (T4.8)
- Multi-cell preflight em PR + live job dispatch-only
- Auto-issue em soak failure + preflight dry-runs + runbook closure
- Immutable `:sha` tags (ADR-0049)
- SHA pinning de actions (com 6 TODOs Renovate)
- Pulumi state integrity checks (idempotency + duplicate URNs detection)
- Helm chart publishing com 9 blocking gates (cross-cut M06)
- 165 Taskfile tasks layered (test, infra, helm, dev, db, k3d)
- LIVE journey runs persistently GREEN

**Bloqueia 4.0:**
- **`cell2-soaks.yml` FIRST GREEN pendente** — Sunday cron deployed mas aguardando primeira execução sucesso com artifacts
- **1 prod customer day-trial 24h pendente** — manual gate, requires operator action
- Mutable `:prod` tag risk (operador pode referenciar errado)
- Sem flake tracking dashboard automatizado
- 6 TODOs Renovate action SHA pinning não-resolvidos
- Sem state backup automation (quarterly manual)
- Sem CRD drift detection cron (apenas em PR trigger)

### Caminho para 4.0

1. **[HIGH]** Executar primeiro Sunday cron `cell2-soaks.yml` GREEN com artifacts publicados — **gate explícito 4.0**
2. **[HIGH]** Identificar primeiro customer day-trial + 24h soak sem incidentes — **gate explícito 4.0**
3. CI gate enforcement: production deploys MUST reference `:${SHA7}` (não `:prod`)
4. Dashboard de flake tracking automatizado (e.g., test-result aggregation em Grafana)
5. Resolver 6 TODOs Renovate (action SHA pinning)
6. Automatizar pulumi-archive (event-driven em vez de quarterly cron)
7. CRD drift detection em cron diário (não apenas em PR)
8. Atualizar CLAUDE.md: 35 workflows (não 29)

---

**Próximas auditorias relacionadas:** Multi-Cell (M09, cell2-soaks gate cross-cut), Helm (M06, helm-charts gates).
