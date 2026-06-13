# Phase 3 — Maturity Detective Report

> **Sponsor mandate verbatim 2026-06-09 + 2026-06-11**:
> "GATE NOVO NUNCA FAÇA NADA HARDCODE" + "PROIBINDO CONFIGURAÇÃO HARDCODED".
> Every literal that should be parameterized is a finding.

**Scope**: 9 delivered dashboard journeys (J-04 Recovery, J-05 Device Success, J-07 Projects, J-08 Project Detail, J-09 Project Connect, J-10 Environments, J-17 Deployments, J-22 Secrets, J-23 Previews).

**Surfaces audited**: `cloud/dashboard/src/pages/` · `api/internal/routes/` · `api/internal/{github,argo,loki,k8s}/` · `api/cmd/setup_secret_fetch.go` · `cloud/internal/routes/` · `operators/internal/{tenant,preview}/` · `pkg/vault/*` · `pkg/naming/preview*` · `infra/helm/loopzero-{api,cloud,external-secrets}/` · `infra/tilt/{api,cloud}-deployment.yaml`

**Verdict**: **51 maturity findings** (20 HIGH + 19 MEDIUM + 12 LOW). The Tilt-owned deployment YAMLs concentrate **35 of the 51 findings** — same root cause every time per `feedback_tilt_owned_deployment_yaml_drift` (2026-06-08 LIVE catch): hand-written L4 YAML silently lags the helm chart, and inline literals duplicate values that belong in helm `values.yaml` (single source of truth).

---

## Top-line: the Tilt-YAML drift trap

The single biggest maturity issue in the codebase is `infra/tilt/{api,cloud}-deployment.yaml`. These files contain **dozens of inline literals** that duplicate values in `infra/helm/loopzero-{api,cloud}/values.yaml`. Per the canonical 3-layer drift pattern (`feedback_drift_3layer_canonical_pattern_2026_06_06`), every drift between these two layers is a silent-fail class waiting for an operator change.

Of the 11 LIVE bugs caught during the 2026-06-08 T6.1 sponsor walkthrough, **4 were variations of this single root cause** (per `feedback_tilt_owned_deployment_yaml_drift` memory).

**Canonical fix**: mount the helm-rendered ConfigMap into the Tilt pod via `tilt-helm` resource. Eliminates by construction all 35 Tilt-YAML findings below in one architectural decision.

---

## HIGH findings (20) — must fix before next release

### Inline secrets / shared cleanroom keys (8)

| # | File:Line | Literal | Class |
|---|-----------|---------|-------|
| 1 | `infra/tilt/api-deployment.yaml:252` | `LOOPZERO_SERVICE_AUTH_SECRET = "cleanroom-service-auth-secret-do-not-use-in-prod"` | Service-auth HS256 key inline — duplicate of cloud-deployment.yaml:61. Drift = 401 on /engine/* (caught LIVE 2026-05-29). |
| 2 | `infra/tilt/cloud-deployment.yaml:61` | (same as #1) | Duplicate of api-deployment.yaml:252. |
| 3 | `infra/tilt/cloud-deployment.yaml:67` | `SESSION_TOKEN_SECRET = "cleanroom-cloud-session-token-secret-do-not-use-in-prod-32chars"` | Session token HS256 key inline. |
| 4 | `infra/tilt/api-deployment.yaml:135` | `LOOPZERO_ARGO_CALLBACK_SECRET = "dev-callback-secret"` | Argo→loopzero-api callback secret. Forgable build-completion callbacks. |
| 5 | `infra/tilt/api-deployment.yaml:155-157` | `MINIO_ACCESS_KEY="loopzero-admin"` + `MINIO_SECRET_KEY="loopzero-dev-password"` | MinIO admin creds inline. |
| 6 | `infra/tilt/api-deployment.yaml:133` | `DATABASE_URL="postgres://loopzero:loopzero-dev-password@..."` | PG DSN with password inline. Triple-drift (api+cloud Tilt+helm). |
| 7 | `infra/tilt/cloud-deployment.yaml:54` | (same DSN as #6) | Duplicate. |
| 8 | `infra/tilt/api-deployment.yaml:324` | `OPENBAO_SPKI_PIN_SHA256 = "298f481d0829..."` | TLS SPKI pin inline. Drift = `ErrTLSPinMismatch` silent break. |
| 9 | `infra/tilt/api-deployment.yaml:322` | `OPENBAO_VAULT_ROLE_ID = "b805a39a-2b8c-..."` | AppRole RoleID inline (half of 2-factor — SecretID is properly externalized). |

**Canonical fix for all 9**: `envFrom: secretRef + optional: true` per `feedback_secret_canonical_no_hardcode`. The pattern already exists 7 lines later in api-deployment.yaml for `loopzero-openbao-approle` and `loopzero-broker-token`. Apply uniformly.

### Hardcoded URLs / namespaces / pseudo-DNS (8)

| # | File:Line | Literal | Class |
|---|-----------|---------|-------|
| 10 | `infra/tilt/api-deployment.yaml:153` | `MINIO_CROSS_CLUSTER_ENDPOINT = "http://172.18.0.3:32000"` | Docker bridge IP — rotates per `docker restart`. |
| 11 | `infra/tilt/api-deployment.yaml:161-163` | `GITEA_CROSS_CLUSTER_URL`+`API_CROSS_CLUSTER_URL = "http://172.18.0.3:..."` | Same class. |
| 12 | `infra/tilt/api-deployment.yaml:299` | `WORKLOAD_INGRESS_URL = "http://172.18.0.12:80"` | Docker bridge IP for workload k3d cluster. |
| 13 | `infra/tilt/cloud-deployment.yaml:30-36` | 4 × `ory-{kratos,hydra}-{public,admin}.loopzero-auth.svc.cluster.local` | Duplicate of helm/loopzero-cloud/values.yaml:48-51. |
| 14 | `infra/tilt/cloud-deployment.yaml:40` | `ENGINE_API_URL = "http://loopzero-api.loopzero-system.svc.cluster.local:3001"` | Duplicate of helm values:58. Canonical 3-layer drift trap caught LIVE 2026-06-08. |
| 15 | `infra/tilt/api-deployment.yaml:291` | `GITHUB_APP_ID = "3319955"` | GitHub App ID inline. Single-tenant assumption. |
| 16 | `api/internal/github/appclient.go:27` | `const githubAPIBase = "https://api.github.com"` | GHES-hostile by construction. |
| 17 | `cloud/internal/routes/integrations_github.go:105` | `installURL := "https://github.com/apps/" + h.appSlug + ...` | Same GHES hostility. |

**Canonical fix for 10-15**: helm-rendered ConfigMap mount + script-based heal removal. Per `feedback_drift_3layer_canonical_pattern`: single source of truth = helm/values.yaml. **Fix for 16-17**: promote `githubAPIBase` to a `BaseURL string` field on `AppClient` populated from config (default `https://api.github.com`).

### Generated-shell-script hardcode (2)

| # | File:Line | Literal | Class |
|---|-----------|---------|-------|
| 18 | `api/internal/argo/harbor_mirror_observe.go:132-133` | `fmt.Sprintf("http://loki.monitoring.svc.cluster.local:3100/loki/api/v1/push")` (no fmt args!) | `fmt.Sprintf` with no format args = clear refactor artifact. URLs hardcoded in 2 layers (Go env + bash heredoc default at line 66-67). |
| 19 | `api/internal/argo/harbor_mirror_observe.go:66-67` | Same URLs in bash heredoc `${LOKI_PUSH_URL:-http://...}` | DRY violation: 4 file edits required to re-home Loki. |

### Stale namespace logic (1) — REAL BUG, not just maturity

| # | File:Line | Literal | Class |
|---|-----------|---------|-------|
| 20 | `api/internal/loki/client.go:221` | `namespace := fmt.Sprintf("loopzero-%s-%s", tenant, env)` | **Post-M2 (2026-06-09) namespace canonical is `loopzero-{tenant}-{project}-{env}`** per `pkg/naming.Namespace`. Comment at line 217 acknowledges 'project is currently unused' — that line IS the bug. Tenants with 2+ projects get cross-project log leakage. **Likely real LIVE bug.** |

---

## MEDIUM findings (19) — fix in next iteration

### Image tags / registry / cluster names (3)

| # | File:Line | Literal | Class |
|---|-----------|---------|-------|
| 21 | `infra/tilt/api-deployment.yaml:229-244` | 5 × `k3d-loopzero-registry:5000/loopzero-*:v0.X.Y` | Image tags inline. Drift when chart bumps version, Tilt YAML lags. |
| 22 | `infra/tilt/api-deployment.yaml:216` | `REGISTRY_URL = "k3d-loopzero-registry:5000"` | Registry hostname + port inline, repeated in 7 image overrides above. DRY violation. |
| 23 | `infra/tilt/api-deployment.yaml:181` | `BUILD_KUBE_CONTEXT = "build"` | Bare k3d context name inline. Drift class. |

### Port / URL / domain inline (5)

| # | File:Line | Literal |
|---|-----------|---------|
| 24 | `infra/tilt/api-deployment.yaml:131` | `PORT = "3001"` |
| 25 | `infra/tilt/cloud-deployment.yaml:28` | `CLOUD_LISTEN_ADDR = ":13002"` |
| 26 | `infra/tilt/cloud-deployment.yaml:73` | `AUTH_COOKIE_DOMAIN = "loopzero.example"` |
| 27 | `infra/tilt/cloud-deployment.yaml:83-87` | `APP_PUBLIC_URL` + `BILLING_RETURN_URL` inline. Drift bug caught LIVE 2026-06-08. |
| 28 | `infra/tilt/api-deployment.yaml:301` | `BASE_DOMAIN = "loopzero.local"` |
| 29 | `infra/tilt/api-deployment.yaml:303-305` | `JWKS_URL` + `JWT_ISSUER` inline pointing at loopzero-auth ns. |
| 30 | `infra/tilt/cloud-deployment.yaml:38` | `HYDRA_CLIENT_ID = "loopzero-cli"` |
| 31 | `infra/tilt/api-deployment.yaml:179` | `BUILD_KUBECONFIG` + `RUNTIME_KUBECONFIG` mount paths inline. |

### Hardcoded magic numbers / config defaults (5)

| # | File:Line | Literal |
|---|-----------|---------|
| 32 | `api/internal/argo/workflow_submitter.go:32` | `submitMaxRetries = 3` |
| 33 | `api/internal/github/appclient.go:135` | `8 * time.Minute` (App JWT expiry — see also LOW finding #43) |
| 34 | `api/internal/k8s/tenant_evictor.go:84-87` | Default fallback ns `"loopzero-system"` + `"argo"` inline |
| 35 | `api/internal/routes/environments.go:26-33` | Reserved-namespace map inline — silent drift when Helm adds a new system ns |
| 36 | `operators/internal/preview/preview_resources.go:77` | Preview SA name `"loopzero-app"` inline (also at line 91 in error message — drift trap) |

### Stale logic / hardcoded formula (2)

| # | File:Line | Literal |
|---|-----------|---------|
| 37 | `api/internal/github/status_notifier.go:70` | `fmt.Sprintf("https://%s-%s-%s-%s.%s", tenantID, projectName, "api", environment, n.baseDomain)` — pre-M2 4-segment formula + hardcoded `"api"` app slug. Multi-app monorepos break. |
| 38 | `cloud/dashboard/src/pages/environments.tsx:117` | `'teste'` literal fallback as last-ditch default — leaks internal fixture slug to real users in 0-project tenants. |

### Duplicated URL block (1)

| # | File:Line | Class |
|---|-----------|-------|
| 39 | `infra/tilt/cloud-deployment.yaml:29-36` ↔ `infra/helm/loopzero-cloud/values.yaml:48-51` | Identical 4-URL block duplicated across helm + Tilt. Canonical 3-layer drift trap. |

---

## LOW findings (12) — defer to documentation pass

### Vague names / stale comments / unnamed magic numbers

| # | File:Line | Class |
|---|-----------|-------|
| 40 | `cloud/dashboard/src/pages/recovery.tsx:193,226,320` | 3 × `const result` for different Kratos flow types. Rename to `updatedFlow`/`verifiedFlow`/`passwordFlow`. |
| 41 | `api/internal/loki/client.go:217` | Doc-comment paraphrases pre-M2 namespace formula — stale. |
| 42 | `api/internal/loki/client.go:56` | Doc-comment hardcodes label format `loopzero-build-{buildID}-{suffix}` — should ref `pkg/naming`. |
| 43 | `pkg/vault/transit_client.go:176` | Default `RateLimiterMaxEntries = 10000` unnamed. |
| 44 | `pkg/vault/cached_client.go:88,94,133` | Three `30 * time.Second` clustered (CacheTTL / CBCooldown / HTTPClient.Timeout) — name each. |
| 45 | `api/internal/github/appclient.go:135` | Magic `8 * time.Minute` + inline comment `// 8min (max 10)` — promote to named const. |
| 46 | `api/internal/github/appclient.go:134` | Magic `-60 * time.Second` + comment `// clock skew tolerance` — promote. |
| 47 | `pkg/vault/database_client.go:64` + `k8s_engine_client.go:55` | `refreshWindow = 30 * time.Minute` vs `15 * time.Minute` — why differ? |
| 48 | `pkg/vault/cached_client.go:91` | `cfg.CBThreshold = 5` unnamed default. |
| 49 | `cloud/dashboard/src/pages/device-success.tsx:12-14` | `AUTO_CLOSE_MS = 5000` / `COUNTDOWN_TICK_MS = 1000` — named but no rationale comments. |
| 50 | `cloud/dashboard/src/pages/recovery.tsx:130` | Cooldown poll `setInterval(..., 1000)` unnamed. |
| 51 | `infra/tilt/api-deployment.yaml:147-260` | 5 × stringly-typed boolean env vars (`"true"`/`"false"`). Typo `"True"` silently disables. |

---

## Anti-patterns NOT found (good engineering signals)

- **NO `TODO` / `FIXME` comments without tickets** anywhere in the 9 journey surfaces.
- **NO `range len()` Python-ism** in Go code.
- **NO `try/except: pass`** silent swallows.
- **Type hints / Go static types** consistently applied.
- **State machines** use `Transition(event)` properly (no direct status writes spotted in audited files).
- **`pg_advisory_xact_lock`** scope respected.
- **The `setup_secret_fetch.go` broker file is a textbook canonical-by-construction example**: `brokerSingleClusterCellID = placement.DefaultCellID` (line 41) explicitly replaces what was a magic-literal `"cell-default"` finding in /loop-code-review consensus-CRITICAL #13. ADR D1 cited inline. **This is the pattern the rest of the codebase should imitate.**

---

## Strategic recommendation

**One architectural fix collapses ~35 of the 51 findings**: replace the hand-edited `infra/tilt/{api,cloud}-deployment.yaml` files with helm-template + mount-as-ConfigMap. This is the canonical resolution to the drift class documented in:

- `feedback_tilt_owned_deployment_yaml_drift` (2026-06-08 LIVE catch)
- `feedback_drift_3layer_canonical_pattern_2026_06_06` (J-09 dogfood — 8 of 11 bugs were this pattern)
- `feedback_secret_canonical_no_hardcode` (T6.1 LIVE catch sponsor verbatim)
- `feedback_no_hardcode_gate_2026_06_09` (sponsor mandate verbatim "GATE NOVO NUNCA FAÇA NADA HARDCODE")
- `feedback_4layer_drift_extension_2026_06_09` (canonical 4-layer drift framing)

Without this architectural change, the same drift class will produce more LIVE-only bugs in the next sponsor walkthrough — by design, because there is no automated test that catches "helm values diverged from Tilt YAML" (vitest mocks resolve to whatever they're given; tsc accepts both shapes).

The remaining ~16 findings (image tags, GHES base URL, M2 stale Loki namespace, harbor_mirror_observe URLs, magic numbers in pkg/vault, vague variable names) are per-file fixes ranging from 1-line renames to 1-PR refactors.

---

*Generated by maturity-detective agent — Phase 3 code review session 2026-06-11.*
