# ADR-0020 — Infra Parity (CHG-2026-04-30): D1-D7

| | |
|---|---|
| **Status** | APPROVED |
| **Date** | 2026-04-30 |
| **Plan** | docs/plans/infra-parity-plan.md |
| **Discovery** | 2026-04-30 audit revealed 49/52 ArgoCD apps OutOfSync because runtime ≠ Git |
| **Reviewers** | platform-architect, production-design-reviewer, sre-engineer |
| **Supersedes** | parts of ADR-0010 #6 (capacity targets), ADR-0015 (adopt-or-create) for parity scope |

## Problem

The 2026-04-30 audit (`/review` against `infra/pulumi`, `infra/helm`, `cli/`, `operators/`) surfaced a systemic failure mode that broke the contract `loopzero platform install -f values.yaml` makes with operators:

1. **49/52 ArgoCD apps in `Sync=Unknown` / `OutOfSync`** because the team iterated through `kubectl set image` rather than `helmfile sync` — **runtime ≠ Git**.
2. **11 LoopZeroApplications**, **9 namespaces in Terminating**, **10 environments + 3 tenants in Deleting** — finalizer races piled up across iterations because `reconcileDelete` did not tolerate `NotFound`.
3. **Hot-patched image tags** (`loopzero-api:achado28`, `loopzero-api:f07-fix`) had no Git source; reproducible install was impossible.
4. **Dead code** in `cli/src/installer/{bootstrap,components}.ts` carried Rule 3 violations latent (would re-leak `Gitea`/`ArgoCD`/`MinIO` if re-imported).
5. **Self-hosting drift from Cloud** — Helmfile environments could silently diverge (release X declared in standalone, missing in prod) with no test catching it.

These are not isolated bugs. They are the symptoms of operating without a parity contract. This ADR defines that contract via 7 binding decisions (D1-D7) and groups them in one place so future contributors do not re-litigate them while reading the plan inline.

## Decisions

### D1 — Image tags: semver | sha-* | latest. No branch tags. No hot-patch.

**Decision:** Image tags MUST match the regex `^(v\d+\.\d+\.\d+(-[a-z0-9]+)?|sha-[a-f0-9]+|[a-f0-9]+|latest)$`. `latest` is accepted as the internal-development convention because every dev rebuild + `pullPolicy: Always` produces a fresh pull on rollout, which is observationally equivalent to a semver bump for the pre-release period. Branch-name tags (`achado28`, `f07-fix`, `REDACTED_SECRET_061`) are rejected at CI gate.

**Rationale:** Branch tags mask the Git ↔ runtime relationship. Semver/SHA/latest each have unambiguous source-of-truth semantics: semver pins to a release commit, SHA pins to a build commit, `latest` pins to "whatever the local registry has right now" (acceptable in dev where the dev loop produces it deterministically).

**Enforcement:** `infra/pulumi/src/__tests__/image-tag-format.test.ts` (13 cases — 6 values files × pattern × pullPolicy=Always rule). VAP `loopzero-disallow-latest-tag` blocks `:latest` in production tenant namespaces.

### D2 — `loopzero platform install` is the only install path; Secret restore is auto-detected.

**Decision:** There is exactly one supported way to install LoopZero: `loopzero platform install -f values.yaml [--provider <name>]`. Any other path (raw helmfile invocation, manual kubectl applies, custom shell scripts) is unsupported. After a `pulumi destroy`, a subsequent `loopzero platform install` MUST detect prior `~/.loopzero/backups/secrets-*.yaml` files and require an explicit decision (CI: `--restore-secrets <file>` OR `--no-restore-secrets`; interactive: prompt).

**Rationale:** The 2026-04-30 audit found that destroy → install lost tenant secrets silently → tenant pods entered `CreateContainerConfigError`. Without auto-detection, the operator must remember to manually re-apply backups, and CI pipelines have no idempotent path.

**Enforcement:** `cli/src/installer/secret-backups.ts` `decideRestore()` (7 cases pinning the decision matrix); Step 3.5 in `cli/src/commands/platform.ts`.

### D3 — ArgoCD reconciliation: Git wins, always. Hot-patch is rejected at the API layer.

**Decision:** Any post-install mutation of `image:` on a LoopZero-managed Deployment via direct `kubectl set image` (or equivalent) is rejected by a ValidatingAdmissionPolicy. The only permitted mutation paths are:
- ArgoCD application controller (Helm-driven, exempt via SA)
- loopzero-controller (operator updates spec.template via owner-ref, exempt via SA)
- kube-controller-manager (replicaset rollouts, exempt via SA)

User access (kubectl with normal RBAC, including operators) is denied.

**Rationale:** The audit's headline finding (49/52 apps in `Unknown`) was caused entirely by operators reaching for `kubectl set image` to test fixes faster than the build → push → helmfile sync loop. With the VAP, the ONLY way to land an image change is through Git → build → helmfile → ArgoCD. This is non-negotiable.

**Enforcement:** `infra/helm/admission-policies/templates/vap-no-runtime-image-mutations.yaml`. `loopzero platform status` adds drift detection (exit 5 + JSON `sync.drift[]`) so the operator sees the regression in real-time during the first install after this ADR ships.

### D4 — Self-hosting === LoopZero Cloud, modulo escala e cert-issuer.

**Decision:** The set of Helmfile releases (chart names, versions, `needs:` ordering) MUST be identical across all four environments (`prod`, `dev`, `standalone-multi`, `standalone`). Differences are restricted to:
- Resource sizing (replicas, requests, limits)
- ClusterIssuer name (`letsencrypt-prod` vs `loopzero-ca-issuer` for self-signed local)
- A single explicit allowlist of intentional differences (today: `chaos-mesh` is prod-only)

Anything else is drift and breaks parity.

**Rationale:** Without this constraint, customers self-hosting LoopZero build an emergent "third platform" that nobody tests against LoopZero Cloud. Bug fixes shipped on Cloud silently miss self-hosting and vice-versa.

**Enforcement:** `infra/pulumi/src/__tests__/helmfile-environments-parity.test.ts` (4 cases — render each env via `helmfile list --output json`, assert release-name set equality modulo D4 exception list).

### D5 — L3 100-tenant smoke is a release gate, not optional.

**Decision:** No release ships without `loopzero platform install --validate-l3` GREEN. The flag triggers `TestRealDeploy_L3RealisticWithChurn` (100 tenants, 10% churn) against the freshly installed platform with SLO p95 ≤ 2min create-to-active, error rate ≤ 5%, all 100 HTTP 200. Failure → exit 5 (partial: install OK, capacity miss).

**Rationale:** Sprint 8 demonstrated that capacity issues are invisible until ~50+ tenants share the management plane (PG max_connections, Gitea HA quorum, Loki stream cardinality). Without an L3 gate, capacity regressions only surface at customer N+1.

**Enforcement:** `cli/src/commands/platform.ts` Step 6.5 + `cli/src/register/platform.ts` `--validate-l3` option. `infra/tests/load/l3_variants_test.go` `TestRealDeploy_L3RealisticWithChurn` is the gate. 4 unit tests pin the contract: default off, on+Ready spawns, on+fail→exit 5, --json emits `phase:'validate-l3'` events.

### D6 — Dead code with internal-term leaks is T1 cleanup, not tolerated.

**Decision:** `cli/src/installer/bootstrap.ts` and `cli/src/installer/components.ts` (and any successor with the same shape) are deleted whenever they (a) are dead and (b) contain internal terms (`Gitea`, `ArgoCD`, `MinIO`, `Kaniko`, `kubebuilder`, `envtest`) that violate Rule 3 if accidentally re-imported. `getPlatformStatus` is the only piece worth keeping; it lives in `cli/src/installer/status.ts` (neutral, product-language only).

**Rationale:** Latent Rule 3 violations are a security-of-DX issue: future contributors re-importing dead helpers leak internal vocabulary into customer-facing output. The cost of keeping the code is non-zero (bundle size, audit overhead, potential re-adoption). The benefit is zero (it's dead).

**Enforcement:** `cli/tests/unit/bundle-rule3-clean.test.ts` (8 cases — grep `dist/` for forbidden terms outside the Rule 3 enforcement code itself).

### D7 — Cross-validation suite runs in CI before merge, not after.

**Decision:** The parity validation suite (helmfile env parity, image tag format, regression matrix integrity) is a PR gate, not a post-merge sanity check. PRs touching `infra/tests/parity/`, `cli/src/installer/`, `cli/src/commands/platform.ts`, or the regression matrix MUST pass `parity-validation.yml` before merge.

**Rationale:** Post-merge gates create a window where main is briefly broken. With offline-fast tests (parity preflight is <2s), there is no excuse for not running pre-merge.

**Enforcement:** `.github/workflows/parity-validation.yml` (3 jobs: parity preflight + CLI parity tests + Pulumi parity tests). `task parity:preflight` for local invocation.

## Consequences

**Positive:**
- Operators have one install command, one source of truth (Git), and one capacity gate. Cognitive load drops.
- Drift between self-hosting and LoopZero Cloud becomes mechanically impossible to merge undetected.
- Hot-patch as an anti-pattern is foreclosed at the admission layer; the dev loop is forced through helmfile sync, which is faster anyway when the build cache is warm.
- Regression matrix integrity is auditable mechanically (`bash infra/tests/parity/audit-regression-matrix.sh`), not by hand.

**Negative:**
- D3 (VAP block on `kubectl set image`) breaks `kubectl rollout` ergonomics for engineers used to it. Mitigation: rebuild + helmfile sync produces the same effect with `pullPolicy: Always`, and is the path that integrates with Git anyway.
- D5 adds ~30min to release rituals. Mitigation: this is precisely the cost of preventing capacity regressions, and the `--validate-l3` flag makes it explicit and skippable for non-release work.
- D7 means a flaky `parity-validation.yml` job blocks merges. Mitigation: parity preflight is offline + deterministic; it cannot be flaky unless an upstream `helmfile`/`vitest`/`shellcheck` is. We have not observed flakes in 6+ iterations.

**Open questions (post-ADR):**
- Should `latest` tag tolerance from D1 sunset once the platform reaches GA? Current decision: revisit at Sprint 9. Today it stays.
- Should D4's allowlist of intentional differences live in code (parity test) or in this ADR? Current decision: in code (test source-of-truth). This ADR is the rationale.

## Verification

| Decision | Test | Status |
|---|---|---|
| D1 | `image-tag-format.test.ts` (13 cases) | GREEN |
| D2 | `decideRestore()` 7 cases + Step 3.5 wired | GREEN |
| D3 | `vap-no-runtime-image-mutations.yaml` rendered + `platform-status-drift.test.ts` (6 cases) | GREEN |
| D4 | `helmfile-environments-parity.test.ts` (4 cases) | GREEN |
| D5 | `platform-install-validate-l3.test.ts` (4 cases) + `cli/src/register/platform.ts` flag wired | GREEN |
| D6 | `bundle-rule3-clean.test.ts` (8 cases) | GREEN |
| D7 | `.github/workflows/parity-validation.yml` (3 jobs) + `task parity:preflight` | GREEN |

| Live validation | Status |
|---|---|
| T5.1 — k3d virgin install | GREEN (run 6, 2026-04-30) |
| T6.1 — Droplet install | Cloud-pendente (~$0.50, ~30min) |
| T7.1 — DOKS install | Cloud-pendente (~$5, ~60min) |
| T8.1 — L3 100-tenant smoke | Cloud-pendente (~$10, ~30min) |

## References

- Plan: `docs/plans/infra-parity-plan.md`
- Regression matrix: `docs/runbooks/bootstrap-regression-matrix.md` (135 entries, 109/135 protected)
- Runbook: `docs/runbooks/parity-validation.md`
- 9 Achados captured during T5.1 live: matrix entries 127-135 (#29 postgres schema, #30 docker prune, #31 k3d Traefik, #32 helmfile gates, #33+#131 capacity, #34 label, #35 sentinel, #36 chart name, #37 phantom chart)
