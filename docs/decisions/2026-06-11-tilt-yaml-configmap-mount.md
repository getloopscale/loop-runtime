# ADR D1 — Tilt-owned YAML → Helm ConfigMap + Secret mount canonical pattern

**Status:** Accepted
**Date:** 2026-06-11
**Plan:** `.claude/knowledge-base/plans/2026-06-11-code-review-remediation-147-findings-plan.md` v1.2
**Task:** Phase 0 / T0.2 (draft ADR for T1.1 implementation)
**Tier classification:** T2 (CI/CD changes touch dev-loop) per `.claude/rules/pre-release-governance.md` § Automatic T2/T3 Triggers

## Context

The 2026-06-11 code review (`code-review-output/final_report.md`) found **28 hardcoded literals concentrated in `infra/tilt/{api,cloud}-deployment.yaml`** — 19% of all 147 findings. The maturity-detective specialist surfaced this as the single highest-collapse-ratio architectural fix: ONE pattern change closes 28 findings simultaneously.

The Tilt-owned-YAML drift trap has been documented FOUR previous times in memory (`feedback_tilt_owned_deployment_yaml_drift`):
- Caught LIVE 2026-06-08 T6.1 sponsor walkthrough (4 distinct drifts in one session)
- Caught LIVE 2026-06-04 dev-public canonicalization
- Caught LIVE 2026-06-09 M2 operator canonical fix
- Caught LIVE 2026-06-10 J-22 ESO Sync chain

Each LIVE catch produced an ad-hoc fix in the diverged YAML — none architecturally closed the drift channel. The remediation plan's R1 task (T1.1) finally addresses the root cause via this ADR.

**Why Tilt-owned YAML exists at all:** LoopZero's dual chart system (per `rules/loopzero-design.md § Helm Chart Dual System`) has TWO Helm pipelines:
- `infra/helm/` — bootstrap charts consumed by Helmfile L2 (`task setup` cleanroom canonical)
- `charts/` — published OCI charts consumed by ArgoCD L3 (production GitOps)

Tilt-owned `Deployment` manifests under `infra/tilt/` exist for the dev-loop hot-reload experience — they reference live source paths via `live_update` that ArgoCD's production manifests cannot. Removing them entirely (Alternative (a) below) breaks the dev loop.

## Decision

Introduce paired Kubernetes resources rendered from the SAME helm chart that production uses:

1. **`infra/helm/loopzero-{api,cloud}/templates/configmap-runtime.yaml`** (NEW)
   - Non-sensitive runtime config (URLs, feature flags, namespaces, port numbers, base domains)
   - Rendered via `helm template -s templates/configmap-runtime.yaml` from `values-dev.yaml`

2. **`infra/helm/loopzero-{api,cloud}/templates/secret-runtime.yaml`** (NEW)
   - Sensitive values (DATABASE_URL with credentials, OAuth client_secret, API keys, JWT signing keys)
   - Rendered via `helm template -s templates/secret-runtime.yaml`
   - Hermetic test (`infra/tests/helm/tilt-secret-keys-classification_test.sh` NEW) asserts known-sensitive keys appear ONLY in Secret template (never in ConfigMap)

3. **Tilt-owned `Deployment` mount pattern (canonical):**
   ```yaml
   spec:
     template:
       metadata:
         annotations:
           # Triggers Pod rollout on data change — Kubernetes does NOT auto-restart on ConfigMap/Secret change
           checksum/config: {{ include "loopzero-api.configChecksum" . | quote }}
       spec:
         containers:
         - name: loopzero-api
           envFrom:
           - configMapRef:
               name: loopzero-api-runtime-config
           - secretRef:
               name: loopzero-api-runtime-secret
   ```

4. **Tiltfile orchestration:** renders ConfigMap + Secret BEFORE applying Deployment (helm-template step exits with clear error on missing required value); checksum annotation triggers rollout on Helm value edit.

5. **Edge cases absorbed from edge-case review:**
   - EC-1 (rollout trigger): `checksum/config` annotation computed via `sha256sum` Helm function
   - EC-2 (secret leak): split into `configmap-runtime.yaml` + `secret-runtime.yaml`; hermetic test classifies keys

## Alternatives

### (a) Delete Tilt-owned YAML; use `helmfile sync` for dev-loop reload — REJECTED

**Rationale for rejection:** breaks hot-reload (Tilt's `live_update` requires Deployment manifests under `infra/tilt/` to wire live source paths) + violates `task setup` canonical cleanroom dev loop documented in CLAUDE.md § Development Commands. The dev-loop experience is non-negotiable per project goal "local equals production" — but local needs SUB-SECOND iteration that `helmfile sync` cannot deliver.

### (b) Single source `values.yaml.gotmpl` per env (operator copies values between dev + prod) — REJECTED

**Rationale for rejection:** doesn't help the dev-loop reload story (Tilt still needs the rendered Deployment manifest), and still requires literal env vars in the Tilt YAML — moves the drift surface without eliminating it. The 28 hardcoded literals would still exist; they would just be sourced from a different template, with the same review burden.

### (c) Tilt-managed env var injection via Tiltfile `k8s_resource(env={...})` — REJECTED

**Rationale for rejection:** still hardcoded literals — just moved from the YAML file into the Tiltfile. Tiltfile is Starlark not YAML, so review tooling (kubeconform, helm lint, the maturity-detective specialist) cannot catch drift via standard analysis. The 28-finding hardcode set would become 28-finding Tiltfile-embedded literal set, same problem with worse review surface.

### (d) ESO ExternalSecret CR pointing at OpenBao — REJECTED (for this ADR; orthogonal concern)

**Rationale for rejection:** ESO is the correct production pattern for secrets sourced from an external secret manager (OpenBao Transit), already documented in plan `2026-06-10-j22-openbao-transit-eso-sync-plan.md`. For dev-loop, where OpenBao is also running in cluster (verified at T0.1 baseline Layer 2), ESO would force the operator to populate OpenBao with dev fixture values, slowing iteration. The Secret-mount pattern (this ADR) keeps dev-loop fast; prod inherits the same Deployment template but the Secret is materialized via ESO instead of helm-rendered. The mount contract is identical; the source of truth differs — orthogonal concerns.

## Rationale

Cited rules:
- **`rules/loopzero-design.md` § Helm Chart Dual System** — establishes that `infra/helm/` and `charts/` coexist; the runtime ConfigMap+Secret pattern makes the Tilt overlay TYPE-COMPATIBLE with the chart (both consume the same `configmap-runtime.yaml`/`secret-runtime.yaml` template shape)
- **`rules/design-principles.md` § DRY** — canonical knowledge (the env var key list) has single source (`values.yaml` `runtimeConfig` + `runtimeSecrets` sections), not 28 duplicated literals
- **`rules/cleanroom-up-live-test-mandate.md`** — the dev-loop story is non-negotiable; this ADR preserves `task setup` + `tilt up` + `task doctor` 38/0 flow
- **`feedback_tilt_owned_deployment_yaml_drift`** (memory) — 4 prior LIVE catches establish the empirical case for an architectural fix vs ad-hoc patches
- **`feedback_secret_canonical_no_hardcode`** (memory) — secrets must use `valueFrom.secretRef` pattern, EC-2 absorbed via split

Cited principle: **Twelve-Factor App § III (Config in environment)** — config is strictly separated from code; rendered via composition (Helm template) not duplication (inline YAML).

## Consequences

### Positive

- **28 of 147 code-review findings closed simultaneously by T1.1** (highest collapse ratio of any single task)
- Tilt overlay becomes type-compatible with production chart — both pipelines consume identical `runtimeConfig`/`runtimeSecrets` value-shape
- Future env var additions: one entry in `values.yaml` → automatically wired in dev + prod (no Tilt YAML touch required)
- Hermetic CI gate (`tilt-secret-keys-classification_test.sh`) catches drift before merge — turns the 4× LIVE-catch pattern into a CI-time pre-merge catch
- Closes the `feedback_tilt_owned_deployment_yaml_drift` memory entry permanently

### Negative

- Initial change requires touching 4 Helm chart files + 2 Tilt YAML files + 1 Tiltfile + new hermetic test (T1.1 effort estimate 2-3 days per plan)
- Operators editing values must re-render Tilt-rendered ConfigMap manually (mitigated by Tiltfile auto-render via `local_resource` step with `deps=values-dev.yaml`)
- Adds one Pod rollout on every Helm value edit during dev iteration (mitigated by `checksum/config` annotation — Pod restart is bounded to ~5s on local k3d, acceptable for a config edit; behavior-only edits use Tilt's `live_update` which bypasses Deployment manifest entirely)

### Neutral

- Replaces 28-line inline-env-var Tilt YAML block with 4-line `envFrom: [configMapRef, secretRef]` block — net LoC reduction of ~50 across the 2 Tilt YAML files
- Production charts (`charts/loopzero-{api,cloud}/`) are unaffected by this ADR — they consume the same `configmap-runtime.yaml`/`secret-runtime.yaml` templates via OCI publishing, no chart-side change needed

## References

- Plan: `.claude/knowledge-base/plans/2026-06-11-code-review-remediation-147-findings-plan.md` v1.2 § ADR D1 + Task T1.1
- Edge case review: `.claude/knowledge-base/reviews/2026-06-11-code-review-remediation-147-findings-edge-cases-2026-06-11.md` (EC-1, EC-2)
- Memory: `feedback_tilt_owned_deployment_yaml_drift`, `feedback_secret_canonical_no_hardcode`, `feedback_no_hardcode_gate_2026_06_09`
- Sibling ADRs to draft in subsequent tasks: D2 (T0.3, EnvRef), D3 (T0.4, DefaultPermanentEnv), D4 (T0.5, Plan tier), D5 (T0.6, Pillar-b), D6 (T0.7, Complexity)
