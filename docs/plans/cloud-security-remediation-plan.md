# Plan: Cloud Control Plane — Security Remediation (38 findings)

> **Version 1.0** — This plan remediates all 38 findings from the deep security review of the `cloud/` module (SaaS control plane: billing + identity + React dashboard), captured in `review-output/final_report.md` and `review-output/review.db`. The review verdict was **NOT READY for launch** (C4 health 1.95/4.0) driven by 2 critical, 10 high, 16 medium, and 10 low findings. The plan sequences fixes so the **CI quality gate is restored first** (it is currently absent for `cloud/`, so no fix can be verified automatically until it exists), then closes the **privilege-escalation chain** (consent client_id + network isolation + RBAC), the **billing/metering integrity chain** (atomic webhook + revenue leakage), and finally the hardening backlog. Expected outcome: every launch-blocker (P1) closed with a failing-test-first proof, and a green CI gate that prevents regression.

## Context

The `cloud/` module is a separate Go module (`github.com/getloopscale/loopzero/cloud`, chi + pgx + otel) plus a React/Vite dashboard. It was **not covered by the repo-root `CLAUDE.md`** and, critically, is **absent from the CI quality gate** — `.github/workflows/ci.yml` lints/tests only `[pkg, operators, api]`. Seven unit-test files already exist under `cloud/tests/unit/` but never execute in CI. Signed (cosign) images with SBOMs are produced from source that no automated gate validates.

Evidence base (all traceable to `review-output/review.db`):
- **Privilege-escalation chain (CHAIN-1):** `find_infra_003` (no default-deny NetworkPolicy in `ory` namespace → Hydra admin :4445 reachable cluster-wide) + `find_sec_002`/`find_infra_002` (`consent/handler.go:251` auto-approves *any* `client_id` and injects `tenant_id`) + `find_sec_003` (`RequireRole` wired on zero routes, `UserContext.Role` never populated) → forged JWT with arbitrary `tenant_id` reaches billing endpoints.
- **Financial chain (CHAIN-2):** `find_data_006` (`webhook.go:150` `plan:="pro"` before payment confirmed) + `find_data_003` (non-atomic `ProcessEvent`) + `find_data_004` (unchecked `RowsAffected`) → unpaid Pro entitlement / orphaned plan. Plus `find_data_001` (metering sends `sub_xxx` where Stripe needs `si_xxx` → **usage revenue is currently zero**) and `find_data_002` (no `FOR UPDATE SKIP LOCKED` → duplicate billing under HA).
- **Token-theft chain (CHAIN-3):** `find_infra_012` (no CSP) + `find_sec_004` (JWT broadcast over `BroadcastChannel`) → any same-origin XSS exfiltrates a tenant JWT.
- **Cleared (no action, credited):** `find_sec_001` (RSA→HMAC fallthrough is algorithm-segregated, NOT exploitable, `pkg/jwt/validator.go:79,200`); SQLi none; tenant isolation in SQL solid; IDOR/SSRF/React-XSS-sinks/hardcoded-secrets none.

## Objective

**Done = every P1 launch-blocker closed with a regression test, and `cloud/` (Go + dashboard) gated by CI; all 38 findings either fixed or explicitly accepted with a guard test.**

Measurable goals:
1. `cloud/` Go + dashboard run lint + unit tests + `govulncheck` in CI on every PR.
2. CHAIN-1 broken at ≥2 links (network isolation + consent client_id + RBAC enforced).
3. CHAIN-2 broken: webhook processing atomic and idempotent; no entitlement before payment; metered usage actually reported to Stripe exactly once.
4. CHAIN-3 broken: JWT no longer broadcast; CSP present on dashboard.
5. `golangci-lint run` green for `cloud/`; `go test ./...` green; dashboard `vitest`/`eslint` green; `kubeconform --strict` green on new manifests.

## ADRs

- **D1 — CI gate is the prerequisite, not an afterthought.** The CI quality gate for `cloud/` is built **first** (Phase 0).
  - *Rationale:* Per CLAUDE.md Rule 1 ("Tests before code"), no fix can be proven without a runner. Fixing application code while the gate is absent means every subsequent fix is unverified. The gate is also the single CRITICAL `find_cicd_001`.
  - *Consequences:* Enables RED-GREEN proof for all later phases; adds ~CI minutes per PR; surfaces any currently-failing `cloud/` test immediately.

- **D2 — Reuse existing configuration and middleware; do not add new abstractions.** Consent fixes use the already-present `cfg.KratosAdminURL`/`cfg.HydraAdminURL`/`cfg.HydraClientID`; RBAC uses the already-complete `auth.RequireRole`/`HasMinRole`.
  - *Rationale:* KISS + YAGNI. `config.go` and `roles.go` already model the correct design; the bugs are *non-wiring*, not missing capability.
  - *Consequences:* Small, surgical diffs; lower regression risk; no new config surface.

- **D3 — Role is injected as a JWT claim at issuance, resolved in middleware.** The authoritative role lives in `tenant_members` and is stamped into the token at consent (CLI) and session-mint (dashboard), then read by `RequireJWT`.
  - *Rationale:* Avoids a per-request DB lookup on every API call (the `tenant_members` role is already resolved at login — `EnsureTenant` returns it, currently discarded at `consent/handler.go:213`). Mirrors how `tenant_id` is already injected.
  - *Consequences:* Role changes take effect on next token mint (acceptable for owner/admin/member); requires the session-token issuer and Hydra consent to carry a `role` claim.

- **D4 — Webhook processing is wrapped in a single DB transaction; plan is set only by `subscription.updated`.** Business mutation + `webhook_events` upsert commit atomically; `checkout.session.completed` only links the subscription id.
  - *Rationale:* Stripe delivers at-least-once; D4 makes retries safe and removes the unpaid-entitlement window (CHAIN-2).
  - *Consequences:* Handlers receive a `*sql.Tx`; a crash mid-handler rolls back and Stripe safely retries.

- **D5 — Schema changes go through the API migrations directory (forward-only), classified T2 governance.** The new `stripe_subscription_item_id` column and the `plan_limits` migration fix are authored as new forward-only `.up.sql` files; cloud does not run migrations.
  - *Rationale:* Single source of truth for schema is the API migration set (`config.go` documents "cloud service does NOT run migrations"). Per `.claude/rules/pre-release-governance.md`, migrations are automatic T2 → require Change Governor review + ADR before coding.
  - *Consequences:* Phases touching migrations (T4.1, T7.1, T7.2) carry a governance gate; they must not be merged without the meeting record.

- **D6 — Network isolation is the highest-leverage single fix and ships independently.** The `ory`-namespace default-deny NetworkPolicy (Phase 2) breaks CHAIN-1 at its origin with zero application code.
  - *Rationale:* Defense-in-depth that reduces attacker prerequisite from "any pod" to "explicitly allowed pod" even if consent/RBAC fixes slip.
  - *Consequences:* Requires validating that legitimate callers (loopzero-cloud, loopzero-api, tenant-controller) are explicitly allowed to :4444/:4445 before applying default-deny, or auth breaks.

- **D7 — Governance classification per phase.** T0/T1/T6 application+CI changes are mostly T1 (single-component); NetworkPolicy (T2.1), migrations (T4.1, T7.1), and secret-handling (T2.2, T1.6) are **T2** → diagnosis + Change Governor review before implementation. This plan is the diagnosis artifact for those.

## Dependency Graph

```
Phase 0 (CI gate) ──▶ everything (provides RED/GREEN proof)
   │
   ├─▶ Phase 1 (Identity/AuthZ) ─┐
   ├─▶ Phase 2 (Network) [T2 gov]─┼─▶ breaks CHAIN-1
   │                              │
   ├─▶ Phase 3 (Webhook integrity)┐
   ├─▶ Phase 4 (Metering) [T2 gov]┼─▶ breaks CHAIN-2
   │                              │
   ├─▶ Phase 6 (Frontend/Container)─▶ breaks CHAIN-3 (T6.1)
   │
   ├─▶ Phase 5 (Supply chain)  ── parallel after Phase 0
   ├─▶ Phase 7 (Migration safety) [T2 gov] ── parallel after Phase 0
   └─▶ Phase 8 (Regression guard) ── parallel after Phase 0
```

- **Sequential blocker:** Phase 0 precedes all (provides the test runner).
- **Parallel after Phase 0:** Phases 1, 2, 3, 5, 6, 7, 8 are independent of each other. Phase 4 shares the billing schema with Phase 3 → run T4 after T3 to avoid migration/handler churn.
- **Governance-gated (T2, need Change Governor + ADR before code):** T2.1, T2.2, T1.6, T4.1, T7.1, T7.2.

---

## Phase 0: CI Quality Gate (P1 — prerequisite)

**Objective:** Make `cloud/` (Go module + dashboard) a first-class CI citizen so every later fix is verifiable, and harden the image pipeline.

### T0.1 — Add cloud Go module to the CI quality gate

#### Objective
Run lint, `go mod tidy` check, unit tests, and `govulncheck` for `cloud/` on every PR that touches it.

#### Evidence
`find_cicd_001` (CRITICAL): `.github/workflows/ci.yml` matrix is `[pkg, operators, api]` (line ~96); tidy loop is `for dir in pkg api operators infra/tests infra/scripts` (line ~145); no `cloud` govulncheck step (lines ~468-478). Seven test files in `cloud/tests/unit/` never execute.

#### Files to edit
```
.github/workflows/ci.yml — add 'cloud' to lint matrix, tidy loop, test job, and govulncheck step; add a path filter for cloud/**
```

#### Deep file dependency analysis
- **ci.yml** today gates only three Go modules. Adding `cloud` to the `matrix.component` list reuses the existing `golangci-lint` + `setup-go` steps. The `changes` job (path filters) must gain a `go-cloud` output so the lint/test jobs trigger on `cloud/**`. Downstream: `build-images.yml` runs `on: workflow_run: workflows: ["CI"]` — once CI validates `cloud`, the existing build gate becomes meaningful for cloud images (no change needed there for this task).

#### Deep Dives
- Path filter: add `go-cloud` to the `dorny/paths-filter` (or equivalent) step matching `cloud/**` and `pkg/**` (cloud depends on `pkg`).
- The matrix lint job `cd`s into the component dir; `cloud/go.mod` exists so `golangci-lint run` and `go test ./...` work unchanged.
- govulncheck: replicate the existing api step for `cloud/` (`cd cloud && govulncheck ./...`). This will flag `find_infra_004` (x/net) until Phase 5 — acceptable; mark non-blocking initially OR fix x/net in the same PR (preferred — see T5.1 dependency).
- Edge case: if any existing `cloud/tests/unit/*_test.go` currently fails, the PR surfaces it — triage before merge (do not skip).

#### Tasks
1. Add `go-cloud` path filter output (`cloud/**`, `pkg/**`).
2. Add `cloud` to the `matrix.component` list of the lint job and gate it on `go-cloud == 'true'`.
3. Add `cloud` (and `cloud/dashboard` excluded — JS handled in T0.2) to the `tidy-go` dir loop.
4. Add a `cloud` unit-test step: `cd cloud && go test ./... ./tests/...`.
5. Add a `govulncheck` step for `cloud`.

#### TDD
```
RED:     CI run on a branch that deliberately breaks a cloud unit test (e.g., flip an assertion in cloud/tests/unit/config_test.go) MUST fail the pipeline. Confirm the job executes and goes red.
RED:     CI run with an un-tidy cloud/go.mod MUST fail the tidy job.
GREEN:   Add matrix/loop/steps so a clean cloud/ passes; revert the deliberate break.
REFACTOR: De-duplicate the per-module steps via the existing matrix (no copy-paste job).
VERIFY:  Open a draft PR touching cloud/ and confirm lint+test+govulncheck jobs run and pass.
```

#### Acceptance Criteria
- [ ] A failing `cloud/` test fails CI (proven on a scratch branch).
- [ ] `go mod tidy` drift in `cloud/` fails CI.
- [ ] `govulncheck ./...` runs for `cloud/`.
- [ ] Jobs only trigger when `cloud/**`/`pkg/**` change.
- [ ] Pass: lint check (zero `golangci-lint` warnings for cloud).

#### DoD
- [ ] Workflow YAML validates (`actionlint` if available; otherwise PR CI self-check).
- [ ] All `cloud/tests/unit/*` pass in CI.
- [ ] Reviewer confirms jobs ran in the PR checks tab.

### T0.2 — Add dashboard (React) to CI

#### Objective
Run `eslint` and `vitest` for `cloud/dashboard/` on every PR touching it.

#### Evidence
`find_cicd_007`: dashboard has `"lint": "eslint ."` and `"test": "vitest run"` configured but no CI job runs them; reaches DOCR unvalidated.

#### Files to edit
```
.github/workflows/ci.yml — add a dashboard job (node setup, npm ci, npm run lint, npm test)
```

#### Deep file dependency analysis
- `cloud/dashboard/package.json` declares `lint`/`test` scripts and `vitest ^4.1.2`. A new job mirrors the repo's existing `cli/` Node job pattern (setup-node + `npm ci`). Downstream: prevents broken dashboard reaching the image build.

#### Deep Dives
- Use `npm ci` (not `npm install`) to honor the lockfile — also mitigates `find_cicd_007`'s caret-range concern (lockfile is authoritative under `ci`).
- Cache `~/.npm` keyed on `package-lock.json`.

#### Tasks
1. Add `js-dashboard` path filter (`cloud/dashboard/**`).
2. Add a job: `setup-node@<sha>`, `cd cloud/dashboard && npm ci && npm run lint && npm test`.

#### TDD
```
RED:     A failing dashboard vitest (flip cloud/dashboard/src/lib/auth.test.ts assertion) MUST fail CI.
RED:     An eslint violation MUST fail CI.
GREEN:   Job added; clean dashboard passes; revert deliberate break.
REFACTOR: None expected.
VERIFY:  Draft PR touching cloud/dashboard/ shows the job running.
```

#### Acceptance Criteria
- [ ] `vitest` runs in CI and a failing test goes red.
- [ ] `eslint` runs in CI.
- [ ] `npm ci` (lockfile-pinned) used, not `npm install`.

#### DoD
- [ ] Dashboard job green on a clean branch.
- [ ] Triggers only on `cloud/dashboard/**`.

### T0.3 — SHA-pin all actions in ci.yml

#### Objective
Pin every `uses:` in `ci.yml` to a commit SHA (matching `build-images.yml`'s posture).

#### Evidence
`find_cicd_003` (HIGH): `ci.yml` uses tag refs (`@v4`, `@v5`, `@v7`) while `build-images.yml` is SHA-pinned — the gate is less hardened than the pipeline it guards.

#### Files to edit
```
.github/workflows/ci.yml — replace tag refs with pinned SHAs (+ comment with the version)
```

#### Deep file dependency analysis
- Each `actions/checkout@v4` etc. resolves to a mutable tag; pinning to SHA removes the supply-chain mutation risk. No behavioral change.

#### Deep Dives
- Resolve each action tag to its current commit SHA; annotate `# v4.x.y`.
- Keep a follow-up note for Renovate/Dependabot to bump SHAs (the repo already uses Renovate per memory).

#### Tasks
1. Enumerate all `uses:` in `ci.yml`.
2. Replace each with `owner/repo@<sha> # vX.Y.Z`.

#### TDD
```
RED:     grep for 'uses:.*@v[0-9]' in ci.yml returns matches (current state) — assert it must become zero.
GREEN:   Pin all; the grep returns zero matches.
REFACTOR: None.
VERIFY:  CI still green; `grep -E 'uses:.*@v[0-9]+($|[^.])' .github/workflows/ci.yml` returns nothing.
```

#### Acceptance Criteria
- [ ] Zero tag-only `uses:` refs remain in `ci.yml`.
- [ ] CI passes unchanged after pinning.

#### DoD
- [ ] grep guard returns empty.
- [ ] PR checks green.

### T0.4 — Harden build-images supply-chain pipeline

#### Objective
SHA-pin remaining actions, scan before push, and unify SBOM format.

#### Evidence
`find_cicd_005` (MED): `anchore/sbom-action/download-syft@v0.24.0` + `actions/upload-artifact@v4` tag-pinned. `find_cicd_006` (MED): Trivy scans *after* push (`|| true` silent delete). `find_cicd_008` (LOW): `sign-and-attest` emits SPDX-JSON while Sprint-6 pipeline emits CycloneDX.

#### Files to edit
```
.github/workflows/build-images.yml — SHA-pin syft+upload-artifact (lines ~495, ~574); move Trivy scan before push or fail-closed on scan failure (lines ~513-525); standardize SBOM to CycloneDX (lines ~527-535)
```

#### Deep file dependency analysis
- `build-images.yml` builds, scans, signs, attests cloud + other images. Reordering scan-before-push requires building to a local/staged ref first, scanning, then pushing on pass. Downstream: cosign attestation should reference the same SBOM format used elsewhere (CycloneDX) so `policy-controller` verification is consistent.

#### Deep Dives
- Scan-before-push: `docker build` to local tag → `trivy image --exit-code 1 --severity HIGH,CRITICAL <local>` → push only if exit 0. Remove the `|| true` on the delete fallback (no longer needed if never pushed).
- SBOM: set syft output to `cyclonedx-json`; align the cosign `attest --type cyclonedx`.

#### Tasks
1. SHA-pin `download-syft` and `upload-artifact`.
2. Restructure job: build → scan → push-on-pass.
3. Switch SBOM format to CycloneDX; update attest type.

#### TDD
```
RED:     A workflow-lint/grep asserting no tag-only uses in build-images.yml fails today for the two actions.
RED:     (Behavioral) An image with a seeded HIGH CVE MUST NOT be pushed (verify via a scratch run with a known-vulnerable base) — currently it would be pushed then deleted.
GREEN:   Pin actions; reorder scan-before-push; unify SBOM.
REFACTOR: None.
VERIFY:  Scratch run shows scan gate blocks push; SBOM artifact is CycloneDX.
```

#### Acceptance Criteria
- [ ] No tag-only `uses:` in `build-images.yml`.
- [ ] Vulnerable image never reaches DOCR (scan precedes push).
- [ ] SBOM is CycloneDX, consistent with Sprint-6 pipeline.

#### DoD
- [ ] Scratch run demonstrates the scan gate.
- [ ] cosign attest succeeds with CycloneDX.

---

## Phase 1: Identity & Authorization (P1 — breaks CHAIN-1)

**Objective:** Stop OAuth client impersonation, enforce RBAC, and fix identity-claim issuance.

### T1.1 — Enforce OAuth client allowlist in consent handler

#### Objective
Reject consent challenges from any `client_id` not in the trusted allowlist before auto-approval.

#### Evidence
`find_sec_002` (CRITICAL) / `find_infra_002` (HIGH): `consent/handler.go:251` calls `AcceptConsent` with no check of `consentReq.ClientID`; `tenant_id` is injected into the resulting token. Origin link of CHAIN-1.

#### Files to edit
```
cloud/internal/consent/handler.go — add trustedClientID field + client_id check at start of Consent()
cloud/cmd/main.go — pass cfg.HydraClientID into consent.NewHandler
cloud/internal/config/config.go — (no change; HydraClientID already present at :52)
```

#### Deep file dependency analysis
- `Handler` (handler.go:27) has no client-id field today. `NewHandler` (:38) signature gains `trustedClientID string`. `cmd/main.go` constructs the handler — must pass `cfg.HydraClientID`. `Consent()` (:232) gains an early `403` when `consentReq.ClientID != h.trustedClientID`. No other caller of `NewHandler`.

#### Deep Dives
- Allowlist: a single trusted client (`cfg.HydraClientID`, default `loopzero-cli`). If future dashboards register a second first-party client, make it a comma-separated set — but YAGNI for now (single value).
- Invariant before: any registered Hydra client routed here gets full scope. Invariant after: only `trustedClientID` is auto-approved; all others get `403 Unauthorized client`.
- Edge case: empty `consentReq.ClientID` → reject.

#### Tasks
1. Add `trustedClientID string` to `Handler`; set it in `NewHandler`.
2. In `Consent()`, after fetching `consentReq`, `if consentReq.ClientID != h.trustedClientID { http.Error(w, "Unauthorized client", 403); return }`.
3. Wire `cfg.HydraClientID` in `cmd/main.go`.

#### TDD
```
RED:     Test_Consent_rejects_untrusted_client() — given consentReq.ClientID="evil-client", Consent() returns 403 and does NOT call AcceptConsent. (Fails today — auto-approves.)
RED:     Test_Consent_allows_trusted_client() — given ClientID==cfg.HydraClientID, AcceptConsent is called.
GREEN:   Implement the guard.
REFACTOR: None expected.
VERIFY:  cd cloud && go test ./internal/consent/... ./tests/...
```

#### Acceptance Criteria
- [ ] Untrusted `client_id` → 403, no token issued.
- [ ] Trusted `client_id` → unchanged behavior.
- [ ] Pass: lint, complexity ≤ 10, coverage on `Consent()` ≥ 90%.

#### DoD
- [ ] New tests RED before, GREEN after.
- [ ] `go test ./...` green; `golangci-lint` clean.

### T1.2 — Use configured KratosAdminURL (drop string-replace heuristic)

#### Objective
Resolve the Kratos admin URL from `cfg.KratosAdminURL` instead of `strings.Replace(...)`.

#### Evidence
`find_infra_001` (HIGH): `consent/handler.go:171-174` derives admin URL by replacing `"kratos-public"`→`"kratos-admin"`; if the internal URL lacks that substring the call hits the public API (no `/admin/identities`) → JWT issued without `tenant_id` → middleware rejects with "Account setup required". `cfg.KratosAdminURL` already exists (config.go:40).

#### Files to edit
```
cloud/internal/consent/handler.go — add kratosAdminURL field; use it in fetchIdentityClaims()
cloud/cmd/main.go — pass cfg.KratosAdminURL into consent.NewHandler
```

#### Deep file dependency analysis
- `fetchIdentityClaims` (:166) currently computes `kratosAdmin` via string-replace. Replace with `h.kratosAdminURL`. `NewHandler` gains a `kratosAdminURL string` param (combine with T1.1 signature change in one edit). `cmd/main.go` passes `cfg.KratosAdminURL`.

#### Deep Dives
- Invariant after: identity fetch always targets the configured admin endpoint regardless of service naming.
- Edge case: empty `kratosAdminURL` → default already provided by config (`http://ory-kratos-admin.loopzero-system:80`); add a `Validate()` check (see T1.6).

#### Tasks
1. Add `kratosAdminURL` to `Handler` + `NewHandler`.
2. Replace lines 171-174 with `kratosAdmin := h.kratosAdminURL`.
3. Wire in `cmd/main.go`.

#### TDD
```
RED:     Test_fetchIdentityClaims_uses_configured_admin_url() — with kratosInternalURL not containing "kratos-public", identity fetch still hits the configured admin URL (mock server asserts path /admin/identities/{sub}). Fails today.
GREEN:   Use h.kratosAdminURL.
REFACTOR: Remove now-unused strings import if applicable.
VERIFY:  cd cloud && go test ./internal/consent/...
```

#### Acceptance Criteria
- [ ] Admin calls target `cfg.KratosAdminURL` independent of public URL naming.
- [ ] `tenant_id` claim populated when identity resolves.

#### DoD
- [ ] Test RED→GREEN; lint clean.

### T1.3 — Populate Role and enforce RequireRole on sensitive routes

#### Objective
Stamp `role` into issued tokens, populate `UserContext.Role` in middleware, and gate billing/account-mutation routes with `RequireRole`.

#### Evidence
`find_sec_003` (HIGH): `middleware.go:74-79` and `:96-101` never set `Role`; `roles.go` `RequireRole` is wired on zero routes (`cmd/main.go`); any authenticated user can call `POST /v1/billing/checkout`/`portal`. `EnsureTenant` already returns the role (discarded at `consent/handler.go:213`).

#### Files to edit
```
cloud/internal/consent/handler.go — capture the role from EnsureTenant; add to tokenClaims["role"]
cloud/internal/auth/middleware.go — populate UserContext.Role from claims (RSA Ext.Role + session Role)
cloud/internal/routes/billing.go — (no change; route wiring is in main)
cloud/cmd/main.go — apply auth.RequireRole("owner") to billing checkout/portal (and any account/member-management mutations)
pkg/jwt/... — ensure the claims structs expose a Role/Ext.Role field (verify; add if absent)
```

#### Deep file dependency analysis
- `EnsureTenant(ctx, subject, email) (tenantID, role, err)` — second return is currently `_`. Capture it → `tokenClaims["role"]`. The session-token issuer (dashboard path) must likewise embed `role` when minting. `middleware.go` reads `claims.Role`/`claims.Ext.Role` (RSA) and session `claims.Role`, sets `UserContext.Role`. `cmd/main.go` composes route middleware — add `RequireRole("owner")` after `RequireJWT` on billing mutation routes. Downstream: `RequireRole` (roles.go:30) already returns 403 on insufficient role — no change there.

#### Deep Dives
- D3 governs role source. If `pkg/jwt` claims lack a `Role` field, add it (additive — JSON, backward-compatible per Rule 5 for `--json`/token shape).
- Default role: if a token predates this change and carries no role, `HasMinRole("", "owner")` returns false → 403. That is the safe default (deny). Document that existing dashboard sessions must re-mint (acceptable; tokens are short-lived).
- Which routes require which role (per roles.go §5.1 matrix): billing checkout/portal → `owner`; member management → `owner`; deploy/secrets → `admin`; read → `member`. This plan wires the billing/owner gate (the proven exposure); broader gating is a follow-up if other mutation routes exist in cloud.

#### Tasks
1. Verify/add `Role` to `pkg/jwt` RSA + session claims.
2. Capture role in `consent/handler.go`; add `tokenClaims["role"]`.
3. Embed `role` in session-token issuance (dashboard mint path).
4. Populate `UserContext.Role` in both middleware branches.
5. Apply `RequireRole("owner")` to billing checkout/portal in `cmd/main.go`.

#### TDD
```
RED:     Test_RequireRole_blocks_member_on_billing() — a token with role="member" → POST /v1/billing/checkout returns 403. Fails today (no role, route ungated).
RED:     Test_RequireJWT_populates_role() — UserContext.Role == claims role after validation. Fails today (always "").
RED:     Test_owner_can_checkout() — role="owner" → reaches handler.
GREEN:   Implement claim stamping + population + route gating.
REFACTOR: None expected.
VERIFY:  cd cloud && go test ./internal/auth/... ./tests/...
```

#### Acceptance Criteria
- [ ] `UserContext.Role` reflects the token role.
- [ ] member/admin → 403 on billing mutations; owner → allowed.
- [ ] Token shape adds `role` without renaming existing fields (Rule 5).

#### DoD
- [ ] Three tests RED→GREEN; lint clean; `go test ./...` green.

### T1.4 — Redact PII in consent logs

#### Objective
Stop logging full `tokenClaims` (email, tenant_id) at INFO.

#### Evidence
`find_sec_008` (MED): `consent/handler.go:258-263` logs `"claims", tokenClaims` (email + tenant_id) on every approval.

#### Files to edit
```
cloud/internal/consent/handler.go — replace "claims", tokenClaims with non-PII fields (client, subject, scope count)
```

#### Deep file dependency analysis
- The `h.logger.Info("consent auto-approved", ...)` call at :258 is the only PII sink here. Replace `"claims", tokenClaims` with `"has_tenant", tokenClaims["tenant_id"] != nil` and `"scope_count", len(...)`. No downstream consumer of the log shape.

#### Deep Dives
- Keep `subject` (opaque Kratos ID) — not PII like email. Drop `email`/`tenant_id` values.

#### Tasks
1. Replace the `claims` log field with redacted metadata.

#### TDD
```
RED:     Test_consent_log_omits_email() — capture slog output; assert it does NOT contain the email/tenant_id value. Fails today.
GREEN:   Redact.
REFACTOR: None.
VERIFY:  cd cloud && go test ./internal/consent/...
```

#### Acceptance Criteria
- [ ] No email/tenant_id values in consent INFO logs.

#### DoD
- [ ] Test RED→GREEN; lint clean.

### T1.5 — Validate device-verify returnTo (open-redirect guard)

#### Objective
Constrain `returnTo` in `DeviceVerify` to known-safe paths before building the Kratos login redirect.

#### Evidence
`find_sec_009` (LOW): `consent/handler.go:283-286` builds `returnTo := r.URL.String()` from attacker-controlled request URL, used in a redirect.

#### Files to edit
```
cloud/internal/consent/handler.go — validate/normalize returnTo (relative path only; strip host)
```

#### Deep file dependency analysis
- `DeviceVerify` (:271) uses `r.URL.String()` then passes to Kratos login `return_to`. Kratos itself enforces a `return_to` allowlist, so impact is low, but defense-in-depth: normalize to the path+query only (drop scheme/host) and ensure it begins with `/auth/device`.

#### Deep Dives
- Construct `returnTo` from a fixed prefix + the validated `device_challenge`/`user_code` query params rather than echoing the full URL.

#### Tasks
1. Rebuild `returnTo` from a fixed path + sanitized query params.

#### TDD
```
RED:     Test_DeviceVerify_returnTo_is_path_only() — given a crafted r.URL with an absolute external URL, the redirect's return_to is a relative /auth/device path. Fails today.
GREEN:   Normalize.
REFACTOR: None.
VERIFY:  cd cloud && go test ./internal/consent/...
```

#### Acceptance Criteria
- [ ] `return_to` is always a relative `/auth/device...` path.

#### DoD
- [ ] Test RED→GREEN; lint clean.

### T1.6 — Harden config.Validate() for production [T2 governance]

#### Objective
Require admin URLs and an explicit `ServiceAuthSecret` in production.

#### Evidence
`find_infra_013`: `config.go:124-146` checks `SessionTokenSecret` but not `ServiceAuthSecret` (auto-generated per-pod → non-deterministic engine auth at replicas>1), nor `KratosAdminURL`/`HydraAdminURL`.

#### Files to edit
```
cloud/internal/config/config.go — add prod checks for KratosAdminURL, HydraAdminURL, and ServiceAuthSecret length + explicit-set
```

#### Deep file dependency analysis
- `Load()` auto-generates `ServiceAuthSecret` via `generateDevSecret()` when env unset (:119). In prod, that produces a different 64-char hex per pod, which currently *passes* (no check). Fix: in prod, require `LOOPZERO_SERVICE_AUTH_SECRET` explicitly set (track via a separate `os.LookupEnv` or a sentinel) and `len >= 32`. Add non-empty checks for the admin URLs (consumed by T1.2/T1.1).

#### Deep Dives
- Distinguish "explicitly set" from "auto-generated": in prod, do NOT auto-generate `ServiceAuthSecret`; instead leave empty and let `Validate()` fail. Same consideration for `SessionTokenSecret` (already len-checked, but a 64-char dev secret would pass — apply the same explicit-set rule).
- Governance: this changes startup behavior of a security-critical secret → T2; route through Change Governor (D7).

#### Tasks
1. In `Load()`, only auto-generate dev secrets when `Environment != "prod"`.
2. In `Validate()`, require `ServiceAuthSecret` len ≥ 32, `KratosAdminURL != ""`, `HydraAdminURL != ""` in prod.

#### TDD
```
RED:     Test_Validate_requires_service_auth_secret_in_prod() — prod config with empty ServiceAuthSecret → error. Fails today (no check).
RED:     Test_Validate_requires_admin_urls_in_prod().
RED:     Test_Load_does_not_autogen_secrets_in_prod().
GREEN:   Implement.
REFACTOR: None.
VERIFY:  cd cloud && go test ./internal/config/... ./tests/...
```

#### Acceptance Criteria
- [ ] Prod start fails fast when `ServiceAuthSecret`/admin URLs unset.
- [ ] Dev behavior unchanged (auto-gen still works).

#### DoD
- [ ] Change Governor record exists (T2).
- [ ] Tests RED→GREEN; lint clean.

---

## Phase 2: Network Isolation (P1 — breaks CHAIN-1 at origin) [T2 governance]

**Objective:** Make the Hydra/Kratos admin APIs reachable only by explicitly authorized workloads.

### T2.1 — Default-deny + scoped allow NetworkPolicy for the ory namespace

#### Objective
Add `default-deny-ingress` to the `ory` namespace plus explicit allow rules for loopzero-cloud/loopzero-api/tenant-controller to :4444/:4445.

#### Evidence
`find_infra_003` (MED, H9 validated): no NetworkPolicy exists for the `ory` namespace; Hydra admin :4445 reachable from any routable pod. Highest-leverage CHAIN-1 break (D6).

#### Files to edit
```
infra/helm/network-policies/ory.yaml — (NEW) default-deny-ingress + allow-hydra-admin/allow-kratos-admin from loopzero-cloud/loopzero-api/tenant-controller via namespaceSelector+podSelector
```

#### Deep file dependency analysis
- `infra/helm/network-policies/loopzero-system.yaml:325-378` is the template to mirror (it already restricts within `loopzero-system`). The new `ory.yaml` targets the `ory` namespace where Hydra/Kratos run. Downstream: legitimate callers MUST be allowlisted first or auth breaks cluster-wide.

#### Deep Dives
- Identify the exact caller selectors: loopzero-cloud (consent/hydra client), loopzero-api, tenant-controller. Allow ingress to Hydra admin :4445 and public :4444, Kratos admin :80 and public :80 only from those pods.
- Invariant after: a pod outside the allowlist gets connection refused to :4445.
- Edge case: bootstrap/migration jobs that touch Hydra/Kratos admin must be allowlisted too.

#### Tasks
1. Author `ory.yaml` default-deny-ingress.
2. Add allow rules per caller/port.
3. Add to the network-policies Helm release values.

#### TDD
```
RED:     A kubeconform/policy test (or an envtest/e2e netpol assertion) that an unlabeled pod cannot reach hydra-admin:4445 — fails today (no policy).
RED:     Allowed caller CAN reach :4445.
GREEN:   Apply policies.
REFACTOR: None.
VERIFY:  task validate:manifests (kubeconform --strict) on ory.yaml; live netpol check in drill cluster.
```

#### Acceptance Criteria
- [ ] Unauthorized pod denied to :4445/:4444.
- [ ] loopzero-cloud/loopzero-api/tenant-controller still authenticate (no regression).
- [ ] Pass: `kubeconform --strict`.

#### DoD
- [ ] Change Governor record (T2) + ADR.
- [ ] Manifests validate; live drill confirms allow+deny.

### T2.2 — Move admin URLs from ConfigMap to Secret

#### Objective
Relocate `HYDRA_ADMIN_URL`/`KRATOS_ADMIN_URL` into the Secret (defense-in-depth).

#### Evidence
`find_infra_007` (MED): `charts/loopzero-cloud/templates/configmap.yaml:10-12` exposes admin URLs in a ConfigMap.

#### Files to edit
```
charts/loopzero-cloud/templates/configmap.yaml — remove admin URL keys
charts/loopzero-cloud/templates/secret.yaml — add admin URL keys (or envFrom secret)
charts/loopzero-cloud/values.yaml — relocate values
```

#### Deep file dependency analysis
- The Deployment's `envFrom`/`env` must source the admin URLs from the Secret. No app code change (env var names unchanged).

#### Deep Dives
- These are URLs (not credentials), but map to high-privilege endpoints; with T2.1 this removes an enumeration shortcut.

#### Tasks
1. Move the two keys to the Secret template.
2. Update Deployment env source.

#### TDD
```
RED:     helm template assertion: admin URLs no longer appear in the ConfigMap output — fails today.
GREEN:   Relocate.
REFACTOR: None.
VERIFY:  helm template + kubeconform --strict; pod boots with admin URLs from Secret.
```

#### Acceptance Criteria
- [ ] Admin URLs absent from ConfigMap, present in Secret.
- [ ] Pod still resolves admin URLs at runtime.

#### DoD
- [ ] Change Governor record (T2).
- [ ] `helm template` + kubeconform clean.

---

## Phase 3: Billing & Webhook Integrity (P1 — breaks CHAIN-2)

**Objective:** Make Stripe webhook processing atomic, idempotent, and entitlement-correct.

### T3.1 — Wrap ProcessEvent in a single transaction

#### Objective
Execute idempotency check + business mutation + `webhook_events` upsert atomically.

#### Evidence
`find_data_003` (HIGH): `webhook.go:49-118` runs three autocommit ops; a crash between mutation and event-record makes Stripe retries re-apply state.

#### Files to edit
```
cloud/internal/billing/webhook.go — open db.BeginTx in ProcessEvent; thread *sql.Tx into handle* methods; commit/rollback
```

#### Deep file dependency analysis
- `ProcessEvent` (:49) currently uses `h.db.QueryRow`/`h.db.Exec`. Change handlers (`handleCheckoutCompleted`, `handleSubscriptionUpdated`, `handleSubscriptionDeleted`, `handlePaymentFailed`) to accept `tx *sql.Tx` (or a `dbExecer` interface) and use it. The final `webhook_events` upsert (:100) joins the same tx. Commit on success; rollback on `processErr`. Downstream: `NewWebhookHandler` keeps `*sql.DB`; only internal call shape changes.

#### Deep Dives
- Use `database/sql` `BeginTx(ctx, nil)`. On `processErr`, still record `status='failed'` — but per D4 that must commit (so retries are tracked) while the business mutation rolls back. Resolve by: run business mutation in tx; if it fails, rollback business tx, then record the failure in a *separate* statement (autocommit) so the failure audit persists. If it succeeds, record `processed` in the same tx and commit. (This keeps the success path atomic and the failure path auditable.)
- Invariant after: subscription state and `status='processed'` are committed together or not at all.

#### Tasks
1. Add `ctx` + `BeginTx`.
2. Thread tx into handlers.
3. Commit on success (mutation + event record together); on failure, rollback + autocommit failure record.

#### TDD
```
RED:     Test_ProcessEvent_atomic_on_crash() — simulate event-record failure after mutation; assert the mutation is rolled back (subscription unchanged) so a retry is safe. Fails today.
RED:     Test_ProcessEvent_idempotent_replay() — same event id processed twice → mutation applied once.
GREEN:   Implement tx.
REFACTOR: Extract a small dbExecer interface.
VERIFY:  cd cloud && go test ./internal/billing/... ./tests/...
```

#### Acceptance Criteria
- [ ] Success path commits mutation+record atomically.
- [ ] Failure path leaves no partial mutation; failure is audited.
- [ ] Replay applies mutation once.

#### DoD
- [ ] Tests RED→GREEN; lint clean.

### T3.2 — Remove plan='pro' default + check RowsAffected on checkout

#### Objective
On `checkout.session.completed`, only link the subscription id; never set `plan`; error if no row updated.

#### Evidence
`find_data_006` (MED): `webhook.go:150` `plan:="pro"` before payment confirmed. `find_data_004` (MED): `webhook.go:152-160` UPDATE never checks `RowsAffected` → silent no-op orphans paid customers.

#### Files to edit
```
cloud/internal/billing/webhook.go — handleCheckoutCompleted: drop plan column from UPDATE; check RowsAffected
```

#### Deep file dependency analysis
- `handleCheckoutCompleted` (:127) UPDATE sets `plan=$2`. Remove `plan` from the SET list (only `stripe_subscription_id`, `status`). `handleSubscriptionUpdated` (:170) remains the authoritative plan setter via `PlanForPriceID`. Capture `res.RowsAffected()`; return error if 0 (so the event stays retryable per T3.1).

#### Deep Dives
- Invariant after: a tenant's `plan` is only ever set by a price-mapped `subscription.updated`. No entitlement before Stripe confirms the price.
- Edge case: if `subscription.updated` arrives before `checkout.session.completed`, the row already has the correct plan; checkout only links the sub id (idempotent).

#### Tasks
1. Remove `plan` from the checkout UPDATE.
2. Check `RowsAffected`; return error if 0.

#### TDD
```
RED:     Test_checkout_does_not_set_pro() — after checkout.session.completed, plan is unchanged (not forced to 'pro'). Fails today.
RED:     Test_checkout_errors_when_no_subscription_row() — UPDATE affecting 0 rows returns error (event retryable). Fails today.
GREEN:   Implement.
REFACTOR: None.
VERIFY:  cd cloud && go test ./internal/billing/...
```

#### Acceptance Criteria
- [ ] No plan granted on checkout; plan set only by subscription.updated.
- [ ] 0-row UPDATE → error (retryable).

#### DoD
- [ ] Tests RED→GREEN; lint clean.

### T3.3 — Audit-record unhandled/skipped webhook events

#### Objective
Record events of unhandled types as `status='skipped'` before returning.

#### Evidence
`find_data_007` (LOW): `webhook.go:86-89` `default: return nil` exits before the `webhook_events` INSERT — no audit trail.

#### Files to edit
```
cloud/internal/billing/webhook.go — in the default case, record status='skipped' then return nil
```

#### Deep file dependency analysis
- Move the event-record write so it also runs for the `default` branch (or fall through to the recording block with `status='skipped'`). Reuse the existing upsert.

#### Tasks
1. Record skipped events before returning from `default`.

#### TDD
```
RED:     Test_unhandled_event_is_recorded_skipped() — an event of unknown type creates a webhook_events row with status='skipped'. Fails today.
GREEN:   Implement.
REFACTOR: None.
VERIFY:  cd cloud && go test ./internal/billing/...
```

#### Acceptance Criteria
- [ ] Every received event has a `webhook_events` row.

#### DoD
- [ ] Test RED→GREEN; lint clean.

### T3.4 — Constant-time HMAC comparison on raw bytes

#### Objective
Decode hex signatures to raw bytes before `hmac.Equal`.

#### Evidence
`find_sec_005` (MED): `webhook.go:324` compares hex strings via `hmac.Equal([]byte(sig), []byte(expected))`; non-idiomatic and length-leaking on malformed sigs.

#### Files to edit
```
cloud/internal/billing/webhook.go — VerifySignature: hex.DecodeString(sig) and compare against mac.Sum(nil) raw bytes
```

#### Deep file dependency analysis
- `VerifySignature` (:275) builds `expected = hex.EncodeToString(mac.Sum(nil))`. Change to compare raw: `sigBytes, err := hex.DecodeString(sig)` and `hmac.Equal(sigBytes, mac.Sum(nil))`. Skip malformed sigs (continue). No caller change.

#### Deep Dives
- `hmac.Equal` is constant-time for equal-length slices; raw-byte comparison avoids any hex-length artifact and is the documented Stripe pattern.

#### Tasks
1. Decode each `v1` signature; `hmac.Equal` on raw bytes.

#### TDD
```
RED:     Test_VerifySignature_rejects_malformed_hex() — a non-hex v1 value is rejected, not crashed/length-leaked.
RED:     Test_VerifySignature_valid()/Test_VerifySignature_tampered() — pass/fail correctly with raw-byte compare.
GREEN:   Implement.
REFACTOR: None.
VERIFY:  cd cloud && go test ./internal/billing/...
```

#### Acceptance Criteria
- [ ] Valid sigs accepted, tampered/malformed rejected, raw-byte constant-time compare.

#### DoD
- [ ] Tests RED→GREEN; lint clean.

### T3.5 — Idempotency-Key on Stripe POST mutations

#### Objective
Send an `Idempotency-Key` header on Stripe POSTs (customer/subscription mutations, usage reporting).

#### Evidence
`find_sec_006` (MED): `stripe.go:145-153` `ReportUsage` (and other POSTs) send no `Idempotency-Key` → retries can double-apply.

#### Files to edit
```
cloud/internal/billing/stripe.go — add Idempotency-Key header (deterministic per logical operation) to POST requests
```

#### Deep file dependency analysis
- The hand-rolled client's POST path builds `http.Request`. Add `req.Header.Set("Idempotency-Key", key)` where `key` is a deterministic id per operation (e.g., usage record id `usage-{tenant}-{metric}-{period}`, already generated per data-reviewer). Downstream: Stripe dedupes server-side.

#### Deep Dives
- Key must be stable across retries of the *same* logical op but unique across distinct ops. Reuse the deterministic record id.

#### Tasks
1. Thread an idempotency key into POST helpers; set the header.

#### TDD
```
RED:     Test_ReportUsage_sends_idempotency_key() — mock Stripe server asserts header present and stable across a retry. Fails today.
GREEN:   Implement.
REFACTOR: None.
VERIFY:  cd cloud && go test ./internal/billing/...
```

#### Acceptance Criteria
- [ ] POST mutations carry a stable `Idempotency-Key`.

#### DoD
- [ ] Test RED→GREEN; lint clean.

---

## Phase 4: Metering Revenue Integrity (P1) [T2 governance — migration]

**Objective:** Actually report metered usage to Stripe, exactly once, under HA.

### T4.1 — Persist and use stripe_subscription_item_id

#### Objective
Add a `stripe_subscription_item_id` column, populate it from `subscription.updated`, and use it in the reporter.

#### Evidence
`find_data_001` (HIGH): `reporter.go:36-60` passes `stripe_subscription_id` (`sub_xxx`) to `stripe.go:152` which builds `/subscription_items/{id}/usage_records` requiring `si_xxx` → every report 404s silently → metered revenue is zero.

#### Files to edit
```
api/internal/db/migrations/000XXX_add_subscription_item_id.up.sql — (NEW) ALTER TABLE subscriptions ADD COLUMN stripe_subscription_item_id TEXT
cloud/internal/billing/webhook.go — handleSubscriptionUpdated: persist sub.Items.Data[0].ID
cloud/internal/metering/reporter.go — select + pass stripe_subscription_item_id to ReportUsage
cloud/internal/billing/stripe.go — ReportUsage param is the subscription item id (verify path)
```

#### Deep file dependency analysis
- `handleSubscriptionUpdated` (:170) already unmarshals `Items.Data[].Price.ID`; extend the struct to capture `Items.Data[0].ID` (the `si_xxx`) and persist it. `reporter.go` (:36) currently selects `stripe_subscription_id`; change to select the new column. `stripe.go:152` path is correct once given `si_xxx`. Migration is forward-only in the API migrations set (D5).

#### Deep Dives
- Backfill: existing subscriptions lack `si_xxx` until the next `subscription.updated`. Add a one-time reconcile or rely on the next Stripe event; document the gap. New column nullable; reporter skips rows where it is null (and logs at WARN so the gap is visible — not silent).
- Governance: migration = T2 (D5/D7).

#### Tasks
1. Author forward-only migration adding the column.
2. Persist `si_xxx` in `handleSubscriptionUpdated`.
3. Reporter selects + sends `si_xxx`; skip+WARN when null.

#### TDD
```
RED:     Test_subscription_updated_persists_item_id() — after the event, the column holds si_xxx. Fails today.
RED:     Test_reporter_sends_subscription_item_id() — ReportUsage receives si_xxx, not sub_xxx. Fails today.
RED:     Test_reporter_skips_and_warns_when_item_id_null().
GREEN:   Implement.
REFACTOR: None.
VERIFY:  cd cloud && go test ./internal/billing/... ./internal/metering/...; task db:migrate against a real PG.
```

#### Acceptance Criteria
- [ ] Reporter calls Stripe with `si_xxx`; usage is accepted.
- [ ] Null item id → skip + WARN (no silent loss).
- [ ] Migration is forward-only, no DROP.

#### DoD
- [ ] Change Governor record (T2) + ADR.
- [ ] Tests RED→GREEN; migration applies cleanly on existing DB.

### T4.2 — Lock pending usage rows with FOR UPDATE SKIP LOCKED

#### Objective
Prevent two replicas from reporting the same usage rows.

#### Evidence
`find_data_002` (HIGH): `reporter.go:36-75` selects pending rows without a row lock → concurrent replicas double-report → Stripe overcharge.

#### Files to edit
```
cloud/internal/metering/reporter.go — add FOR UPDATE SKIP LOCKED to the pending SELECT, inside a transaction
```

#### Deep file dependency analysis
- The pending SELECT (`WHERE reported_to_stripe=false LIMIT 100`) must run inside a tx with `FOR UPDATE SKIP LOCKED`; the subsequent `UPDATE reported_to_stripe=true` commits in the same tx. Combined with T3.5 idempotency keys this is belt-and-suspenders.

#### Deep Dives
- Invariant after: a usage row is claimed by exactly one reporter run.
- Edge case: report-to-Stripe failure → rollback so the row stays claimable.

#### Tasks
1. Wrap select+update in a tx with `FOR UPDATE SKIP LOCKED`.

#### TDD
```
RED:     Test_reporter_no_double_claim() — two concurrent ReportPending() runs against the same rows report each row once. Fails today.
GREEN:   Add lock.
REFACTOR: None.
VERIFY:  cd cloud && go test ./internal/metering/... (with a real/temp PG for the concurrency test).
```

#### Acceptance Criteria
- [ ] Each pending row reported at most once across concurrent runs.

#### DoD
- [ ] Test RED→GREEN; lint clean.

---

## Phase 5: Supply Chain & Dependencies (P2)

**Objective:** Patch the x/net CVE and remove the maintenance-mode driver.

### T5.1 — Bump golang.org/x/net (+ toolchain) in cloud and pkg

#### Objective
Raise `golang.org/x/net` to ≥ v0.53.0 and the Go toolchain to the patched release.

#### Evidence
`find_infra_004` (MED, H8): `cloud/go.mod:35` pins `x/net v0.52.0` (< v0.53.0 patched for GO-2026-4918 HTTP/2 loop); toolchain `go1.25.9` misses html/template XSS (GO-2026-4980/4982) + crypto/tls/x509 DoS.

#### Files to edit
```
cloud/go.mod, cloud/go.sum — x/net bump + toolchain
pkg/go.mod, pkg/go.sum — mirror for consistency
```

#### Deep file dependency analysis
- Cloud uses outbound gRPC (OTel exporter) over HTTP/2 via x/net. `go get golang.org/x/net@latest && go mod tidy`. Mirror in `pkg` (cloud depends on pkg). govulncheck (T0.1) becomes green.

#### Tasks
1. `cd cloud && go get golang.org/x/net@latest && go mod tidy`; bump toolchain.
2. Mirror in `pkg`.

#### TDD
```
RED:     govulncheck ./... in cloud reports GO-2026-4918 (current). Assert it must report clean after bump.
GREEN:   Bump.
REFACTOR: None.
VERIFY:  cd cloud && govulncheck ./... && go test ./...
```

#### Acceptance Criteria
- [ ] govulncheck clean for cloud (and pkg).
- [ ] `go test ./...` green post-bump.

#### DoD
- [ ] govulncheck green in CI (ties to T0.1).

### T5.2 — Migrate cloud from lib/pq to pgx

#### Objective
Replace the maintenance-mode `lib/pq` driver with `pgx` (matching `api/`).

#### Evidence
`find_cicd_004` (HIGH): `cloud/go.mod:42-43` uses `lib/pq v1.12.3` (maintenance-only). `find_cicd_002` (HIGH): hand-rolled paths have no regression net — addressed by tests under the now-active CI (T0.1).

#### Files to edit
```
cloud/go.mod — drop lib/pq, add pgx stdlib driver
cloud/cmd/main.go + any sql.Open call site — use pgx driver name
cloud/internal/{billing,metering,auth}/*.go — verify database/sql usage is driver-agnostic (placeholders already $N)
```

#### Deep file dependency analysis
- All queries use `$1` placeholders (pgx-compatible). The change is the `database/sql` driver registration (`_ "github.com/jackc/pgx/v5/stdlib"`) and the `sql.Open("pgx", dsn)` call. Low blast radius given parameterized SQL everywhere (confirmed by data-reviewer).

#### Tasks
1. Swap driver import + `sql.Open` name.
2. Run the full cloud test suite against a real PG.

#### TDD
```
RED:     Existing cloud DB tests (billing/metering/tenant) run under pgx — assert they pass (they exercise the driver). If any lib/pq-specific behavior leaks, it fails.
GREEN:   Switch driver; fix any leak.
REFACTOR: Remove lib/pq from go.mod.
VERIFY:  cd cloud && go test ./... ./tests/... against a live PG.
```

#### Acceptance Criteria
- [ ] `lib/pq` absent from `cloud/go.mod`.
- [ ] All DB tests green under pgx.

#### DoD
- [ ] Tests green; govulncheck/lint clean.

---

## Phase 6: Frontend & Container Hardening (P2/P3 — breaks CHAIN-3)

**Objective:** Stop JWT broadcast, add CSP, and harden containers/deployment.

### T6.1 — Stop broadcasting the JWT; add CSP

#### Objective
Replace the token in `BroadcastChannel` with a signal-only message and add a Content-Security-Policy.

#### Evidence
`find_sec_004` (HIGH): `dashboard/src/lib/api.ts:67-71` broadcasts the raw JWT to all same-origin tabs (XSS amplification). `find_infra_012` (LOW): `dashboard/Dockerfile:22` nginx config has no CSP/security headers.

#### Files to edit
```
cloud/dashboard/src/lib/api.ts — broadcast a "token-changed" signal; each tab re-reads its own token source
cloud/dashboard/Dockerfile (or NEW cloud/dashboard/nginx.conf) — add CSP, X-Frame-Options, X-Content-Type-Options, Referrer-Policy
```

#### Deep file dependency analysis
- `api.ts:67-71` posts `{token: accessToken}`. Change to post `{type:'auth-changed'}`; listeners re-fetch the token from the canonical store (memory/storage) rather than receiving it over the channel. Downstream: `auth.ts`/`use-auth.ts` listeners must handle the signal. The nginx config (currently inline `echo` per find_infra_012) becomes a committed `nginx.conf` with a strict CSP (`default-src 'self'`, connect-src to API/auth origins, no `unsafe-inline` for scripts).

#### Deep Dives
- CSP must allow the dashboard's API/auth/engine origins (the runtime-injected `__config.js` URLs) — generate `connect-src` from those or use `'self'` + known origins. Validate it doesn't break Vite-built assets (hashed, `'self'`).
- Invariant after: no JWT ever transits BroadcastChannel; XSS cannot harvest tokens from other tabs via the channel.

#### Tasks
1. Change broadcast payload to a signal; update listeners.
2. Add committed `nginx.conf` with CSP + headers; reference it in the Dockerfile.

#### TDD
```
RED:     auth.test.ts: broadcasting auth change does NOT include the token string in the message payload. Fails today.
RED:     A test asserting listeners re-read token on signal.
GREEN:   Implement signal-only + listener.
REFACTOR: None.
VERIFY:  cd cloud/dashboard && npm test && npm run lint; curl -I the served dashboard shows CSP header.
```

#### Acceptance Criteria
- [ ] No JWT in any BroadcastChannel message.
- [ ] CSP + security headers present on responses.

#### DoD
- [ ] vitest + eslint green (under T0.2 CI); header check passes.

### T6.2 — Sanitize docker-entrypoint env injection

#### Objective
Validate runtime env vars before writing them into client-served `__config.js`.

#### Evidence
`find_infra_005` (MED): `dashboard/docker-entrypoint.sh:1-14` injects `VITE_*` into `__config.js` via heredoc with no escaping → injection if a value is malicious/misconfigured.

#### Files to edit
```
cloud/dashboard/docker-entrypoint.sh — validate each VITE_* matches ^https?://... before writing; fail closed
```

#### Deep file dependency analysis
- The heredoc expands env vars directly into a JS object literal. Add a per-var regex guard; exit non-zero on invalid. No app code change.

#### Tasks
1. Add validation per `VITE_*` var; `exit 1` on failure.

#### TDD
```
RED:     A shell test (infra/tests or a bats-style test) feeding a malicious VITE_API_URL causes entrypoint to exit non-zero. Fails today (writes it verbatim).
RED:     A valid URL passes.
GREEN:   Add guards.
REFACTOR: None.
VERIFY:  run docker-entrypoint.sh with good/bad env in a test harness.
```

#### Acceptance Criteria
- [ ] Malicious env value → container fails to start (fail-closed).
- [ ] Valid values → unchanged.

#### DoD
- [ ] Shell test RED→GREEN.

### T6.3 — Rate-limit device and billing endpoints

#### Objective
Apply rate limiting beyond `/v1/auth/session/token`.

#### Evidence
`find_sec_007` (MED): `cmd/main.go:209-235` rate-limits only session token; `device/authorize`, `device/token`, and billing endpoints are unthrottled. (Note `find_infra` ratelimit keying on RemoteAddr is per-proxy — see Deep Dives.)

#### Files to edit
```
cloud/cmd/main.go — apply the ratelimit middleware to device + billing routes
cloud/internal/ratelimit/limiter.go — key on a trusted client identifier (X-Forwarded-For first hop validated, or authenticated subject) instead of raw RemoteAddr
```

#### Deep file dependency analysis
- The limiter (`limiter.go:51`) keys on `r.RemoteAddr` (per-proxy behind Traefik/CF). For authenticated routes (billing), key on `UserContext.Subject`; for device (pre-auth), key on a validated forwarded IP. Apply the middleware to the additional route groups in `main.go`.

#### Deep Dives
- Behind CF Tunnel/Traefik, `RemoteAddr` is the proxy; use the leftmost trusted `X-Forwarded-For` (validated against a trusted-proxy list) or the authenticated subject. Document the trusted-proxy assumption.
- This is in-memory per-replica; note the known limitation (a shared store like Redis is a larger follow-up — YAGNI for launch if per-replica limits are acceptable, but document it).

#### Tasks
1. Key the limiter on subject (authed) / validated XFF (pre-auth).
2. Apply middleware to device + billing routes.

#### TDD
```
RED:     Test_billing_rate_limited() — N+1 rapid requests from one subject → 429. Fails today.
RED:     Test_device_token_rate_limited().
GREEN:   Implement keying + wiring.
REFACTOR: None.
VERIFY:  cd cloud && go test ./internal/ratelimit/... ./tests/...
```

#### Acceptance Criteria
- [ ] device + billing endpoints throttled.
- [ ] Keying not trivially bypassed behind the proxy.

#### DoD
- [ ] Tests RED→GREEN; lint clean.

### T6.4 — Container & deployment hardening bundle

#### Objective
Add readOnlyRootFilesystem, .dockerignore, distroless base, immutable image tags, and a single-replica-safe PDB.

#### Evidence
`find_infra_006` (MED): Pulumi prod missing `readOnlyRootFilesystem`. `find_infra_008` (LOW): no `cloud/.dockerignore`. `find_infra_009` (LOW): alpine vs distroless. `find_infra_010` (LOW): default image tag `dev`. `find_infra_011` (LOW): PDB only active at replicas>1.

#### Files to edit
```
infra/pulumi/src/components/mgmt-cloud-service.ts + cp-cloud-service.ts — readOnlyRootFilesystem: true
cloud/.dockerignore — (NEW) whitelist pkg/ + cloud/
cloud/Dockerfile — distroless static base (gcr.io/distroless/static-debian12:nonroot)
charts/loopzero-cloud/values.yaml + charts/loopzero-dashboard/values.yaml — reject tag 'dev' in prod (validation) / require digest
charts/loopzero-cloud/templates/pdb.yaml — set replicaCount/minReplicas 2 in prod (HPA) so PDB is meaningful
```

#### Deep file dependency analysis
- Independent low-risk changes. `readOnlyRootFilesystem` requires verifying the binary writes nothing to `/` (static binary — should be fine; mount an emptyDir for `/tmp` if needed). Distroless requires the static `CGO_ENABLED=0` binary (already so). Image-tag validation: add a Pulumi guard rejecting `tag=='dev'` when `LOOPZERO_ENV=='prod'`. PDB: bump prod replicas to 2 + topology spread.

#### Tasks
1. Add `readOnlyRootFilesystem` to both Pulumi components (+ `/tmp` emptyDir if required).
2. Create `cloud/.dockerignore`.
3. Switch runtime base to distroless.
4. Add prod tag validation; default to digest.
5. Set prod replicaCount/minReplicas 2 + topology spread.

#### TDD
```
RED:     kubeconform/helm-template assertions: prod manifests have readOnlyRootFilesystem:true and replicas>=2; Pulumi unit asserts tag!='dev' in prod. Fail today.
RED:     Image builds and runs on distroless (smoke).
GREEN:   Apply.
REFACTOR: None.
VERIFY:  task validate:manifests; docker build + run smoke; pulumi preview.
```

#### Acceptance Criteria
- [ ] Prod container readOnlyRootFilesystem + non-root + distroless.
- [ ] `cloud/.dockerignore` present.
- [ ] Prod rejects `tag=dev`; PDB meaningful at ≥2 replicas.

#### DoD
- [ ] kubeconform clean; image smoke green; pulumi preview clean.

---

## Phase 7: Data Migration Safety (P3) [T2 governance]

**Objective:** Make destructive/data migrations safe and rollback-aware.

### T7.1 — Make plan_limits migration idempotent (ON CONFLICT)

#### Objective
Replace `DELETE FROM plan_limits; INSERT ...` with upserts.

#### Evidence
`find_data_005` (MED): `migrations-archive/000023_update_plan_limits_tiers.up.sql:12-61` does a bare `DELETE FROM plan_limits;` then INSERT without ON CONFLICT — INSERT failure leaves enforcement fail-open.

#### Files to edit
```
api/internal/db/migrations/000YYY_plan_limits_upsert.up.sql — (NEW forward-only) INSERT ... ON CONFLICT (plan, metric) DO UPDATE
```

#### Deep file dependency analysis
- 000023 is in `migrations-archive` (already applied historically). Per forward-only policy, author a NEW migration that converts the data to the desired state via upsert, and document that future plan-limit changes use ON CONFLICT, not DELETE+INSERT. Downstream: plan enforcement reads `plan_limits`.

#### Deep Dives
- Governance: migration = T2 (D5/D7). No DROP. Idempotent re-run safe.

#### Tasks
1. Author the upsert migration.
2. Document the pattern in the migrations README/runbook.

#### TDD
```
RED:     migration_test.go: applying the new migration twice yields identical plan_limits (idempotent); a simulated mid-INSERT failure does not wipe limits. Fails for the DELETE+INSERT pattern.
GREEN:   Upsert migration.
REFACTOR: None.
VERIFY:  task db:migrate on a real PG with existing plan_limits; re-run is a no-op.
```

#### Acceptance Criteria
- [ ] Plan limits never wiped to empty mid-migration.
- [ ] Re-running is idempotent; no DROP.

#### DoD
- [ ] Change Governor record (T2).
- [ ] Migration applies cleanly twice on existing DB.

### T7.2 — Pre-migration backup gate (PITR) in CI

#### Objective
Require a verified PITR snapshot before destructive migrations and document rollback RTO.

#### Evidence
`find_data_008` (LOW): no down-migrations exist; 000015/000018/000023 drop constraints/columns/data with no rollback path.

#### Files to edit
```
.github/workflows/<migration-or-release>.yml — add a gate that confirms a recent PITR snapshot before applying migrations to prod
docs/runbooks/migration-rollback.md — (NEW) document forward-only policy + PITR-based rollback RTO
```

#### Deep file dependency analysis
- Forward-only is policy (CLAUDE.md). The mitigation is operational: a CI/release gate that a PITR snapshot exists + a runbook. No schema change.

#### Tasks
1. Add the snapshot-verification gate to the migration/release workflow.
2. Write the rollback runbook.

#### TDD
```
RED:     A migration job WITHOUT a confirmed snapshot is blocked (gate fails) — assert via a dry-run.
GREEN:   Add gate.
REFACTOR: None.
VERIFY:  dry-run shows the gate blocks unsnapshotted prod migrations.
```

#### Acceptance Criteria
- [ ] Prod migrations gated on a verified snapshot.
- [ ] Rollback runbook exists with RTO.

#### DoD
- [ ] Change Governor record (T2).
- [ ] Gate demonstrated in a dry-run.

---

## Phase 8: Regression Guard (P4)

**Objective:** Lock in the verified-safe behaviors so future refactors cannot reintroduce them.

### T8.1 — JWT algorithm-segregation regression test

#### Objective
Add a test proving the RSA validator rejects HMAC tokens and vice-versa, guarding the (currently safe) middleware fallthrough.

#### Evidence
`find_sec_001` (LOW, RESOLVED NOT-EXPLOITABLE): `pkg/jwt/validator.go:79,200` enforce algorithm type assertions; `middleware.go:52-105` fallthrough is safe. No fix needed — but a guard test prevents a future refactor from introducing alg-confusion.

#### Files to edit
```
pkg/jwt/validator_test.go (or cloud/internal/auth middleware test) — add segregation tests
```

#### Deep file dependency analysis
- The validators enforce `token.Method.(*SigningMethodRSA)` / `(*SigningMethodHMAC)`. Tests assert an HS256 token fails the RSA validator and an RS256 token fails the HMAC validator.

#### Tasks
1. Add the two segregation tests.

#### TDD
```
RED:     (Guard) Tests pass against current code; to prove they're meaningful, temporarily relax a validator and confirm they go red, then restore.
GREEN:   Tests committed against the correct code.
REFACTOR: None.
VERIFY:  cd pkg && go test ./jwt/...
```

#### Acceptance Criteria
- [ ] RSA validator rejects HMAC tokens; HMAC validator rejects RSA tokens — asserted by tests.

#### DoD
- [ ] Tests committed and green; demonstrated meaningful (red when relaxed).

---

## Coverage Matrix

| # | Finding (severity) | Task(s) | Resolution |
|---|---|---|---|
| 1 | find_cicd_001 (critical) | T0.1 | cloud added to CI lint/test/tidy/govulncheck |
| 2 | find_sec_002 (critical) | T1.1 | consent client_id allowlist (403 untrusted) |
| 3 | find_infra_002 (high) | T1.1 | same fix as find_sec_002 |
| 4 | find_cicd_002 (high) | T0.1, T5.2, T3.* | cloud tests now run in CI; pgx + billing tests give regression net |
| 5 | find_cicd_003 (high) | T0.3 | SHA-pin ci.yml actions |
| 6 | find_cicd_004 (high) | T5.2 | migrate lib/pq → pgx |
| 7 | find_data_001 (high) | T4.1 | persist+use stripe_subscription_item_id |
| 8 | find_data_002 (high) | T4.2 | FOR UPDATE SKIP LOCKED |
| 9 | find_data_003 (high) | T3.1 | atomic ProcessEvent transaction |
| 10 | find_infra_001 (high) | T1.2 | use cfg.KratosAdminURL |
| 11 | find_sec_003 (high) | T1.3 | populate Role + wire RequireRole |
| 12 | find_sec_004 (high) | T6.1 | stop JWT broadcast + CSP |
| 13 | find_cicd_005 (medium) | T0.4 | SHA-pin syft + upload-artifact |
| 14 | find_cicd_006 (medium) | T0.4 | scan before push |
| 15 | find_cicd_007 (medium) | T0.2 | dashboard CI (vitest+eslint, npm ci) |
| 16 | find_data_004 (medium) | T3.2 | check RowsAffected on checkout |
| 17 | find_data_005 (medium) | T7.1 | idempotent plan_limits upsert migration |
| 18 | find_data_006 (medium) | T3.2 | remove plan='pro' default |
| 19 | find_infra_003 (medium) | T2.1 | ory default-deny NetworkPolicy |
| 20 | find_infra_004 (medium) | T5.1 | bump x/net + toolchain |
| 21 | find_infra_005 (medium) | T6.2 | sanitize entrypoint env injection |
| 22 | find_infra_006 (medium) | T6.4 | readOnlyRootFilesystem in Pulumi |
| 23 | find_infra_007 (medium) | T2.2 | admin URLs ConfigMap → Secret |
| 24 | find_infra_008 (medium) | T6.4 | cloud/.dockerignore |
| 25 | find_sec_005 (medium) | T3.4 | constant-time HMAC on raw bytes |
| 26 | find_sec_006 (medium) | T3.5 | Idempotency-Key on Stripe POSTs |
| 27 | find_sec_007 (medium) | T6.3 | rate-limit device + billing |
| 28 | find_sec_008 (medium) | T1.4 | redact PII in consent logs |
| 29 | find_cicd_008 (low) | T0.4 | SBOM CycloneDX consistency |
| 30 | find_data_007 (low) | T3.3 | record skipped events |
| 31 | find_data_008 (low) | T7.2 | PITR pre-migration gate + runbook |
| 32 | find_infra_009 (low) | T6.4 | distroless runtime base |
| 33 | find_infra_010 (low) | T6.4 | reject image tag 'dev' in prod |
| 34 | find_infra_011 (low) | T6.4 | PDB meaningful at ≥2 replicas |
| 35 | find_infra_012 (low) | T6.1 | nginx CSP + security headers |
| 36 | find_infra_013 (low) | T1.6 | config.Validate admin URLs + ServiceAuthSecret |
| 37 | find_sec_001 (low) | T8.1 | regression guard test (no fix needed) |
| 38 | find_sec_009 (low) | T1.5 | device returnTo open-redirect guard |

**Coverage: 38/38 findings covered (100%)**

## Global Definition of Done

- [ ] All 8 phases completed; all tasks RED→GREEN.
- [ ] `cd cloud && go test ./... ./tests/...` green; `cd cloud && golangci-lint run` zero warnings; `cd cloud && govulncheck ./...` clean.
- [ ] `cd cloud/dashboard && npm test && npm run lint` green.
- [ ] `task validate:manifests` (`kubeconform --strict`) green for new NetworkPolicy + chart changes.
- [ ] CI gates `cloud/` (Go + dashboard) on every PR (T0.1/T0.2 proven on a scratch branch).
- [ ] CHAIN-1 broken: ory default-deny applied + consent client_id enforced + RBAC on billing.
- [ ] CHAIN-2 broken: webhook atomic+idempotent, no plan before payment, metered usage reported once.
- [ ] CHAIN-3 broken: no JWT broadcast + CSP present.
- [ ] T2 tasks (T2.1, T2.2, T1.6, T4.1, T7.1, T7.2) have Change Governor records + ADRs before implementation.
- [ ] No internal terms leak into any new user-facing CLI/dashboard/API output (CLAUDE.md Rule 3).
- [ ] Forward-only migrations only; no DROP without explicit sign-off.
- [ ] `review-output/review.db` findings re-queried after fixes; each closed finding marked resolved.
