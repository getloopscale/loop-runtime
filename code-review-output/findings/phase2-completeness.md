# Phase 2 — Completeness Audit (FAANG-level)

Auditor: `completeness-auditor`
Date: 2026-06-11
Scope: 9 delivered dashboard journeys (J-04, J-05, J-07, J-08, J-09, J-10, J-17, J-22, J-23)
Files inventoried: 41
Files inspected: 37

---

## Mandate (sponsor verbatim 2026-06-09 + 2026-06-11)

> **FORBID hardcoded configuration. Every env var, secret, URL, namespace, port, image tag, timeout, retry count, feature flag MUST be parameterized via env / Helm values / valueFrom secretRef / Pulumi outputs / ConfigMap-mounted file.**

Primary focus of this audit: hardcode patterns (memory `feedback_no_hardcode_gate_2026_06_09`, `feedback_secret_canonical_no_hardcode`, `feedback_drift_3layer_canonical_pattern`).

---

## Statistics

| Severity | Count |
|---|---:|
| critical | 0 |
| high     | 3 |
| medium   | 4 |
| low      | 3 |
| info     | 3 |
| **TOTAL**| **13** |

Within report budget (≤ 30 findings). No `critical` blocker for production cutover surfaced — all `high` are silent-bug / hardcode-drift risks documented below.

---

## Findings by category

| Category | Count |
|---|---:|
| `hardcoded_literal` | 7 |
| `promise_vs_impl` | 2 |
| `dead_code` / `disconnected_wiring` | 1 |
| `no_issues` (info) | 3 |

---

## Top findings (sorted by severity then journey impact)

### HIGH — H1. `dashboardURL` stored in `DeviceHandler` but never read; wire passes `BillingReturnURL`

- **File**: `cloud/internal/routes/device.go:17`
- **Journey**: J-05 Device Flow
- **Evidence**: `device.go:17,21,22` declare/set field. Grep across `cloud/internal/routes/device.go` shows ZERO reads of `h.dashboardURL`. `cloud/cmd/main.go:155` passes `cfg.BillingReturnURL` (a billing concern) into a device-flow handler parameter named `dashboardURL`.
- **Why high**: Either dead state (KISS violation) OR the device-success redirect was intended to use the dashboard URL but the implementation was never completed. Test fixtures (device_handler_test.go) pass `"http://localhost:3000"` — the wiring contract is ambiguous.
- **Fix**: Remove the dead field + parameter (KISS) OR wire `cfg.DashboardURL` and consume it in the device-success redirect path.

### HIGH — H2. `"production"` string literal duplicated across 6 sites without a canonical constant

- **File**: `api/internal/routes/projects.go:269` (canonical breaker) + 5 other sites
- **Journey**: J-07, J-08, J-10, J-22
- **Evidence**: `environments.go:32` (reserved), `:490` (delete protection), `:558` (stub list), `:579` (stub detail); `projects.go:269` (auto-create); `secrets.tsx:25` (ENV_OPTIONS frontend tuple), `:52` (`defaultEnvs`).
- **Why high**: Sponsor mandate `feedback_no_hardcode_gate_2026_06_09` explicitly forbids bare-string drift. Changing the canonical permanent-env name (e.g., `production` → `prod`) requires editing 6 places; any miss is a silent J-10 / J-22 regression.
- **Fix**: Introduce `pkg/naming.DefaultPermanentEnv = "production"` (or `domain.DefaultEnvironmentName`). Replace all 6 sites. Frontend should fetch env list from `GET /api/v1/environments` (already returns the list).

### HIGH — H3. J-22 Secrets page hardcodes env filter `[all, production, staging]` — breaks J-10 custom envs

- **File**: `cloud/dashboard/src/pages/secrets.tsx:25` (ENV_OPTIONS) + `:52` (defaultEnvs)
- **Journey**: J-22 Secrets (silent regression of J-10 Environments custom env contract)
- **Evidence**: `secrets.tsx:25 const ENV_OPTIONS = ['all', 'production', 'staging'] as const;` — no `useEnvironments` / `useBuildEnvs` hook. Compare to `deployments.tsx:62` where comment says "env chips are no longer hardcoded" + dynamic hook + fallback. Secrets page never received this fix.
- **Why high**: Users who create a custom env on J-10 (e.g., `staging-eu-1`, `j-rollout-a`) cannot select it on the J-22 secrets dropdown — silent J-22 ↔ J-10 promise drift.
- **Fix**: Replace `ENV_OPTIONS` with dynamic `useEnvironments(name)` hook (mirror `deployments.tsx` pattern). Keep static `[all, production]` only as loading-state fallback. `defaultEnvs` should default from API.

### MEDIUM — M1. `loopzero-builds` S3 bucket name duplicated as fallback in 3 sites

- **File**: `api/internal/github/autodeploy.go:181` (push flow) + `:336` (PrepareSourceBundle) + `api/internal/routes/source.go:64`
- **Journey**: J-09 (autodeploy) + J-17 (build-time bundle upload)
- **Evidence**: All three default to literal `"loopzero-builds"`. Config flows via `cfg.SourceBucket = envOr("SOURCE_BUCKET", "loopzero-builds")` in `config.go:364` — the fallback in source.go is dead unless `WithSourceBucket` is forgotten, but autodeploy.go fallback is live (no setter pattern).
- **Fix**: Single `const defaultSourceBucket = "loopzero-builds"` in `api/internal/source/constants.go`; all three sites import. Or: refuse-boot if config is empty.

### MEDIUM — M2. `loopzero-broker` ClusterSecretStore name hardcoded in Go but customizable via Helm

- **File**: `api/internal/k8s/secret_k8s_manager.go:230`
- **Journey**: J-22 Secrets
- **Evidence**: Chart `infra/helm/loopzero-external-secrets/templates/clustersecretstore-loopzero-broker.yaml:46` renders from `{{ .Values.broker.name | default "loopzero-broker" }}` (operator-overridable). Go literal at line 230 hardcodes `"loopzero-broker"`. Two layers diverge silently — no test asserts they match.
- **Why medium**: Silent failure mode — operator overrides `broker.name`, ESO fails to resolve store, ExternalSecret CR stays in `SecretSyncedError`, tenant pods never get the secret (memory `feedback_swallowed_silent_errors_pattern`).
- **Fix**: Env var `LOOPZERO_BROKER_CSS_NAME` mounted from ConfigMap so Helm value flows through. Most secure: refuse boot when env is empty AND workload-side CSS not Found via discovery probe.

### MEDIUM — M3. BuildKit + base-language images const-only (no env override)

- **File**: `api/internal/argo/constants.go:17` (`imageBuildKit`) + lines 12-18 (Node, Golang, Python, Alpine, Ruby, AWS-CLI)
- **Journey**: J-17 Builds
- **Evidence**: loopzero-packs, dockerfile-lint, cosign, syft images ARE env-overridable via per-image helpers. BuildKit + base-language images are CONST-ONLY. Inconsistent contract: constants.go header docstring says "single source of truth, update one constant to bump version".
- **Why medium**: Dev-loop image substitution works for loopzero-* images but NOT for buildkit. Local k3d dev with non-default buildkit (e.g., custom rootless variant) requires code edit.
- **Fix**: Either add env-override helpers for all 7 images (consistent contract) OR ADR-document why buildkit is deliberately NOT operator-overridable (security pinning is canonical defense).

### MEDIUM — M4. Preview namespace ResourceQuota hardcoded (no per-plan / per-tenant override)

- **File**: `operators/internal/preview/preview_resources.go:35-37`
- **Journey**: J-23 Previews
- **Evidence**: `previewQuotaCPU = "2"`, `previewQuotaMemory = "4Gi"`, `previewQuotaStorage = "5Gi"`. CLAUDE.md claims per-plan caps (Starter/Pro/Team) but preview quota is plan-oblivious.
- **Fix**: Read quota from `PreviewEnvironment` CR spec (operator-set per tenant plan via existing plan-enforcement middleware). Operator decides at admission, controller honors.

### LOW — L1. `pkg/naming.PreviewNamespace` marked DEPRECATED but still called from `operators/internal/preview/preview_controller.go:229`

- **File**: `pkg/naming/naming.go:151`
- **Journey**: J-23 Previews
- **Evidence**: naming.go:151 `// PreviewNamespace is DEPRECATED`; preview_controller.go:229 still calls it (the ONLY prod call site). Deprecation never propagated.
- **Fix**: Either migrate caller to `naming.Namespace(tenant, project, fmt.Sprintf("preview-pr-%d", n))` OR remove the Deprecated note.

### LOW — L2. `https://github.com` hardcoded in `project-connect.tsx` (no GHE / Gitea path)

- **File**: `cloud/dashboard/src/pages/project-connect.tsx:390` + `:509` + `:567`
- **Journey**: J-09 Project Connect
- **Fix**: Single `GITHUB_PUBLIC_HOST` runtime config or page-local const for DRY + future-proofing.

### LOW — L3. `support@loopzero.example` hardcoded in `recovery.tsx` error message

- **File**: `cloud/dashboard/src/pages/recovery.tsx:47`
- **Journey**: J-04 Recovery
- **Fix**: Flow through `window.__LOOPZERO_CONFIG__.SUPPORT_EMAIL` (mirror `API_URL`/`AUTH_URL` pattern).

### INFO — Files audited with no completeness issues

1. **`pkg/vault/transit_client.go`** — Constructor takes `TransitConfig`, SPKI pinning enforced, AppRole singleflight, LRU-bounded rate limiter. Tenant key prefix `loopzero-tenants-` is documented OpenBao single-segment convention (not a hardcode mistake).
2. **`cloud/internal/routes/integrations_github.go`** — All 4 config fields (appSlug, dashboardURL, loginURL, cookieDomain) flow from `cloud/cmd/main.go`. EC-2 / EC-3 / EC-15 absorptions present.
3. **`api/internal/k8s/secret_k8s_manager.go`** — DRY label helper, cross-tenant binding enforced, `loopzero-system` mgmt-namespace + `loopzero-broker` CSS-name caveats folded into M2 + a future SecretStorageConfig refactor.

---

## Hardcode density summary

| Hardcode type | Count | Severity tier |
|---|---:|---|
| Canonical env name `"production"` | 6 sites | HIGH |
| S3 bucket `"loopzero-builds"` | 3 sites | MEDIUM |
| K8s namespace `"loopzero-system"` | 2+ sites | (caveat — operator) |
| ClusterSecretStore `"loopzero-broker"` | Go + chart | MEDIUM |
| Base images (BuildKit, Node, etc.) | 7 const | MEDIUM |
| Preview quota | 3 const | MEDIUM |
| Frontend env list | 2 sites | HIGH |
| GitHub public host | 3 sites | LOW |
| Support email | 1 site | LOW |

---

## Recommendations (ranked by ROI)

1. **Ship `pkg/domain.DefaultPermanentEnv = "production"` constant** + replace 6 call sites (closes H2; ~30 LOC change; locked by test asserting the constant value).
2. **Migrate `secrets.tsx` to `useEnvironments` hook** + drop `ENV_OPTIONS` hardcode (closes H3; ~50 LOC; matches `deployments.tsx` pattern).
3. **Remove dead `dashboardURL` field from `DeviceHandler`** OR wire the correct config field + consume it (closes H1; clarifies intent).
4. **Centralize `loopzero-builds`** in `api/internal/source/constants.go` (closes M1).
5. **Wire `loopzero-broker` CSS name via env var** flowing from Helm `broker.name` value (closes M2; closes silent-failure mode).

Remaining (M3/M4/L1/L2/L3) are lower-priority polish — can ride a future hardening sprint without blocking M2 prod cutover.

---

## Cross-references

- DB findings persisted in `code-review-output/code-review.db` (table `findings`, phase=2, category='completeness').
- Memory anchors: `feedback_no_hardcode_gate_2026_06_09`, `feedback_secret_canonical_no_hardcode`, `feedback_drift_3layer_canonical_pattern`, `feedback_swallowed_silent_errors_pattern`, `feedback_security_first_mandate`.
- All findings emitted with `severity`, `category`, `file`, `line`, `evidence`, `remediation` per Phase 2 contract.
