# Plan: Helm Chart Drift Alignment — Eliminate Bootstrap ↔ Published Divergence

> **Version 1.0** — Align the 4 overlapping Helm charts between `infra/helm/` (bootstrap/Helmfile L2) and `charts/` (published/ArgoCD L3) to eliminate security, configuration, and template drift. Includes critical B33 bugfix propagation to published chart.

## Context

The LoopZero platform uses a dual Helm chart system (ADR D1, bootstrap-integrity-fixes-plan):
- **`infra/helm/`** — 32 bootstrap charts, standalone, used by Helmfile L2 (`helmfile sync`)
- **`charts/`** — 13 published charts, use `loopzero-common` library, used by ArgoCD L3 (Day-2 GitOps)

A drift audit (2026-05-06) found **4 charts with overlap** between the two systems. Of these, 3 have functional drift:

| Chart pair | Drift severity | Finding |
|-----------|---------------|---------|
| `loopzero-api` | **Alta** — security context UID mismatch (65532 vs 1000) + resource limits 4x | ✅ Already fixed this session |
| `loopzero-cloud` | **Média** — resource limits 4x | ✅ Already fixed this session |
| `loopzero-admission-policies` | **Alta** — B33 matchConditions MISSING in published + dual values schema + registry logic divergence | **This plan** |
| `monitoring` / `loopzero-monitoring` | **Nenhuma** — complementary layers, not overlapping | No action needed |

### The admission-policies problem in detail

**Finding 1 — B33 bugfix not propagated (CRITICAL):**
The bootstrap `vap-protect-platform-networkpolicies.yaml` was patched on 2026-05-05 (CHG-2026-05-05-DOGFOOD-B33) to add `matchConditions` excluding Kubernetes internal GC actors. Without this, the VAP rejects the namespace garbage collector → namespaces deadlock in `Terminating` state forever. The published chart (dated 2026-04-23) does NOT have this fix. In production, ArgoCD L3 deploys the published chart — meaning **production is vulnerable to namespace deadlock**.

**Finding 2 — Dual values schema (technical debt):**
6 older VAP templates in the published chart read `.Values.policies.<name>.enabled`, while 2 newer ones read `.Values.vaps.<name>`. The bootstrap chart uses `.Values.vaps.*` consistently. The published `values.yaml` has both sections with a "compatibility shim" comment. This creates confusion and makes it impossible to toggle a VAP from a single source.

**Finding 3 — Registry logic divergence (moderate):**
Bootstrap `vap-restrict-registries.yaml` builds CEL expressions dynamically from `.Values.registries.{docrPrefix,harborPrefix,additionalPrefixes}` with `printf "%q"` (safe quoting). Published version reads `.Values.policies.restrictRegistries.allowedRegistries` with raw `trimSuffix "/*"` interpolation — less safe, different config path.

### What already exists

- Bootstrap chart (source of truth for correct behavior): `infra/helm/admission-policies/`
- Published chart (needs alignment): `charts/loopzero-admission-policies/`
- `loopzero-common` library (v0.3.0): `charts/loopzero-common/` — NOT used by admission-policies (no Deployments, just CRDs)
- helm-unittest tests in `charts/loopzero-admission-policies/tests/`: 4 test files (`vap_test.yaml`, `alerts_test.yaml`, `cluster-image-policy_test.yaml`, `multi-namespace_test.yaml`) — all reference `.Values.policies.*`

## Objective

**Done =** All 11 VAP templates in `charts/loopzero-admission-policies/` match their bootstrap counterparts in `infra/helm/admission-policies/` for: (a) values guard condition, (b) template content, (c) values schema. The `policies:` compatibility shim is removed. All helm-unittest tests pass against the unified `.Values.vaps.*` schema. `task validate:manifests` passes.

Specific goals:
1. B33 `matchConditions` propagated to published chart (CRITICAL — can be cherry-picked independently)
2. All 6 legacy templates migrated from `.Values.policies.*` to `.Values.vaps.*`
3. `vap-restrict-registries.yaml` aligned with bootstrap's CEL builder approach using `.Values.registries.*`
4. `policies:` block removed from `values.yaml`
5. All 4 helm-unittest test files updated for new values schema
6. `values.schema.json` updated (if it references `policies`)

## ADRs

### D1 — Bootstrap templates are source of truth for VAP content

**Decision:** When content differs between bootstrap and published, the bootstrap version wins. The published chart is aligned TO bootstrap, not the other way.
**Rationale:** Bootstrap charts are patched during live dogfood iterations (B33, CHG-DOGFOOD, etc.) and validated against real k3d clusters. Published charts lag because ArgoCD L3 deployment is a separate step. The bootstrap template has always been the working, tested version.
**Consequences:** Published templates may gain comments and matchConditions blocks they didn't have before. This is correct — they were missing operational fixes.

### D2 — Unified `.Values.vaps.*` schema, no backward compatibility shim

**Decision:** Remove the `policies:` block entirely from `values.yaml`. All VAP templates read from `.Values.vaps.*` (boolean toggles). No compatibility shim, no dual-read.
**Rationale:** Pre-release governance (Rule: "Backward compatibility is NOT required. Refactoring and redesign are preferred when they produce a cleaner system."). The published chart is only used by ArgoCD L3 in the LoopZero platform — there are no external consumers. A clean break is safer than maintaining two config paths.
**Consequences:** Anyone with custom ArgoCD values overrides referencing `.policies.*` must migrate to `.Values.vaps.*`. Since there are no external users, this affects only the LoopZero team's own overlays.

### D3 — Registry config centralized in `.Values.registries.*`

**Decision:** The published `vap-restrict-registries.yaml` adopts the bootstrap's approach: read from `.Values.registries.{docrPrefix, harborPrefix, additionalPrefixes}` and build CEL dynamically with `printf "%q"` quoting.
**Rationale:** (a) Centralized registry config is used by `cluster-image-policy.yaml` already (both charts). (b) `printf "%q"` is injection-safe; raw interpolation is not. (c) Single source for registry prefixes eliminates the risk of `allowedRegistries` and `registries.*` diverging.
**Consequences:** `.Values.policies.restrictRegistries.allowedRegistries` is removed. Registry config comes from `.Values.registries.*` only.

## Dependency Graph

```
Phase 0 (B33 hotfix) ──▶ Phase 1 (Template migration) ──▶ Phase 2 (Tests + validation)
```

Phase 0 is a standalone hotfix that can be cherry-picked to production immediately. Phases 1-2 are the full alignment.

---

## Phase 0: B33 Hotfix — Propagate matchConditions to Published Chart

**Objective:** Eliminate the namespace deadlock vulnerability in the published chart. This is the highest-priority fix and can be deployed independently.

### T0.1 — Add matchConditions to published vap-protect-platform-networkpolicies.yaml

#### Objective
Copy the `matchConditions` block from the bootstrap template to the published template, preventing Kubernetes namespace GC from being rejected by the VAP.

#### Evidence
- Bootstrap: `infra/helm/admission-policies/templates/vap-protect-platform-networkpolicies.yaml:31-36` has `matchConditions`
- Published: `charts/loopzero-admission-policies/templates/vap-protect-platform-networkpolicies.yaml` — MISSING
- B33 documented in CLAUDE.md Common Mistakes table: "VAP `failurePolicy: Fail` deadlocks namespace deletion"
- The published chart is what ArgoCD L3 deploys in production

#### Files to edit
```
charts/loopzero-admission-policies/templates/vap-protect-platform-networkpolicies.yaml — add matchConditions block after namespaceSelector
```

#### Deep file dependency analysis
- Template is standalone — no other template references it. Adding `matchConditions` is purely additive to the VAP spec. Downstream: ArgoCD syncs the updated template on next sync.
- The `matchConditions` field is GA in K8s 1.30+ (ValidatingAdmissionPolicy GA). Production runs 1.32.13 — safe.

#### Tasks
1. Add `matchConditions` block between `namespaceSelector` and `validations` (lines 16-17 of current published template)
2. Verify template renders correctly via `helm template`

#### TDD
```
RED:     helm-unittest: assert matchConditions exists on protect-platform-networkpolicies VAP
GREEN:   Add the matchConditions block
VERIFY:  helm template charts/loopzero-admission-policies | grep -A5 matchConditions
```

#### Acceptance Criteria
- [ ] `matchConditions` block present with 3 exclusion expressions (namespace-controller, kube-controller-manager, system:masters)
- [ ] `helm template` renders without errors
- [ ] `kubeconform --strict` passes

#### DoD
- [ ] matchConditions block identical to bootstrap version
- [ ] Existing helm-unittest tests still pass
- [ ] `task validate:manifests` passes

---

## Phase 1: Template Migration — Unify Values Schema

**Objective:** Migrate all 6 legacy VAP templates from `.Values.policies.*` to `.Values.vaps.*` and align `vap-restrict-registries.yaml` with bootstrap's CEL builder.

### T1.1 — Migrate 5 simple VAP templates to `.Values.vaps.*`

#### Objective
Change the guard condition on 5 templates from `.Values.policies.<name>.enabled` to `.Values.vaps.<name>`.

#### Evidence
- Templates affected:
  - `vap-disallow-latest-tag.yaml` — line 1: `{{- if .Values.policies.disallowLatestTag.enabled }}`
  - `vap-require-resource-limits.yaml` — line 1: `{{- if .Values.policies.requireResourceLimits.enabled }}`
  - `vap-require-image-signature.yaml` — line 1: `{{- if .Values.policies.requireImageSignature.enabled }}`
  - `vap-argocd-managed-by.yaml` — line 1: `{{- if .Values.policies.argocdManagedBy.enabled }}`
  - `vap-protect-platform-networkpolicies.yaml` — line 1: `{{- if .Values.policies.protectPlatformNetworkpolicies.enabled }}` (will be updated in T0.1 already)
- Bootstrap equivalents all use `.Values.vaps.<name>` (boolean)
- Content after the guard is identical (confirmed by diff audit) except for vap-protect-platform-networkpolicies which gains matchConditions in T0.1

#### Files to edit
```
charts/loopzero-admission-policies/templates/vap-disallow-latest-tag.yaml          — line 1 guard
charts/loopzero-admission-policies/templates/vap-require-resource-limits.yaml      — line 1 guard
charts/loopzero-admission-policies/templates/vap-require-image-signature.yaml      — line 1 guard
charts/loopzero-admission-policies/templates/vap-argocd-managed-by.yaml            — line 1 guard
charts/loopzero-admission-policies/templates/vap-protect-platform-networkpolicies.yaml — line 1 guard (if not already changed in T0.1)
```

#### Deep file dependency analysis
- Each template is self-contained (VAP + VAPBinding). No cross-template references.
- `values.yaml` still has both `vaps:` and `policies:` at this point — templates will read from `vaps:` which already has the same boolean values.
- Test files (`vap_test.yaml`, `multi-namespace_test.yaml`) reference `.Values.policies.*` — updated in Phase 2.

#### Tasks
1. Change line 1 of each template from `{{- if .Values.policies.<name>.enabled }}` to `{{- if .Values.vaps.<name> }}`
2. For vap-protect-platform-networkpolicies: confirm guard already changed after T0.1, or change it
3. Verify all 5 templates render correctly via `helm template`

#### TDD
```
RED:     helm template with policies.X.enabled=false + vaps.X=true → should render (currently won't because templates read policies)
GREEN:   Change guard conditions
VERIFY:  helm template charts/loopzero-admission-policies --set vaps.disallowLatestTag=true | grep loopzero-disallow-latest-tag
```

#### Acceptance Criteria
- [ ] All 5 templates use `{{- if .Values.vaps.<name> }}` guard
- [ ] `helm template` with default values renders all enabled VAPs
- [ ] No template references `.Values.policies` anymore (confirmed by grep)

#### DoD
- [ ] 5 templates migrated
- [ ] `helm template` renders correctly
- [ ] grep `.Values.policies` returns zero matches in templates/

---

### T1.2 — Align vap-restrict-registries.yaml with bootstrap CEL builder

#### Objective
Replace the published template's simple `allowedRegistries` iteration with the bootstrap's dynamic CEL builder that reads from `.Values.registries.*` with `printf "%q"` safe quoting.

#### Evidence
- Bootstrap (`infra/helm/admission-policies/templates/vap-restrict-registries.yaml`): 77 lines, builds CEL from `.Values.registries.{docrPrefix, harborPrefix, additionalPrefixes}` with `printf "%q"` quoting
- Published (`charts/loopzero-admission-policies/templates/vap-restrict-registries.yaml`): 53 lines, iterates `.Values.policies.restrictRegistries.allowedRegistries` with raw interpolation
- The bootstrap version is more robust (quoting prevents CEL injection) and more maintainable (centralized registry config)

#### Files to edit
```
charts/loopzero-admission-policies/templates/vap-restrict-registries.yaml — replace entire template with bootstrap version (adapted guard to .Values.vaps.restrictRegistries)
```

#### Deep file dependency analysis
- Template reads `.Values.registries.*` — this section already exists in published `values.yaml` (lines 9-15) but was unused by the legacy template. After this change, it becomes the sole source.
- `.Values.policies.restrictRegistries.allowedRegistries` will no longer be read — removed in T1.3.
- CEL expressions change from raw interpolation to `%q`-quoted. Functionally equivalent but safer.

#### Tasks
1. Copy bootstrap template content
2. Change line 1 guard from `.Values.vaps.restrictRegistries` (same — already correct in bootstrap)
3. Verify CEL renders correctly with default registries via `helm template`

#### TDD
```
RED:     helm template → verify CEL contains %q-quoted "registry.digitalocean.com/loopzero-system/" (currently uses raw single-quoted)
GREEN:   Replace template with bootstrap version
VERIFY:  helm template charts/loopzero-admission-policies --show-only templates/vap-restrict-registries.yaml | grep 'startsWith'
```

#### Acceptance Criteria
- [ ] Template uses `printf "%q"` for registry prefix quoting
- [ ] Reads from `.Values.registries.{docrPrefix, harborPrefix, additionalPrefixes}`
- [ ] CEL expressions cover containers, initContainers, ephemeralContainers
- [ ] Comments include MEET-048 + CHG-DOGFOOD iter 9 context

#### DoD
- [ ] Template content matches bootstrap (modulo guard condition format)
- [ ] `helm template` renders valid CEL
- [ ] `kubeconform --strict` passes

---

### T1.3 — Remove `policies:` compatibility shim from values.yaml

#### Objective
Remove the entire `policies:` block and its compatibility comment from `charts/loopzero-admission-policies/values.yaml`. Update `values.schema.json` if it references `policies`.

#### Evidence
- `values.yaml:63-82`: compatibility shim block with comment "legacy values.yaml had `policies.<name>.enabled` structure"
- After T1.1 and T1.2, no template reads from `.Values.policies.*`
- `.Values.registries.*` (lines 9-15) already exists and is now the sole registry config source
- `.Values.vaps.*` (lines 49-61) already has all boolean toggles

#### Files to edit
```
charts/loopzero-admission-policies/values.yaml         — remove policies: block (lines 63-82) + compatibility comment
charts/loopzero-admission-policies/values.schema.json   — remove policies property (if present)
```

#### Deep file dependency analysis
- `values.yaml`: removing `policies:` makes `.Values.vaps.*` and `.Values.registries.*` the only config paths. All templates already migrated in T1.1/T1.2.
- `values.schema.json`: if it defines `policies` as a required or optional property, it must be updated.
- ArgoCD overlays: any `values-*.yaml` referencing `policies.*` must migrate to `vaps.*` — but there are no external consumers (pre-release).

#### Tasks
1. Remove `policies:` block and its comment from `values.yaml`
2. Check and update `values.schema.json`
3. Verify `helm template` still renders all VAPs with default values

#### TDD
```
RED:     grep 'policies:' values.yaml → should find it (currently does)
GREEN:   Remove the block
VERIFY:  grep 'policies' values.yaml → zero matches; helm template renders all VAPs
```

#### Acceptance Criteria
- [ ] Zero references to `policies` in `values.yaml`
- [ ] Zero references to `policies` in `values.schema.json`
- [ ] `helm template` with default values renders all enabled VAPs
- [ ] `helm lint charts/loopzero-admission-policies` passes

#### DoD
- [ ] `policies:` block fully removed
- [ ] Schema updated
- [ ] No rendering regressions

---

## Phase 2: Test Migration + Validation

**Objective:** Update all helm-unittest test files to use `.Values.vaps.*` and run full validation.

### T2.1 — Update helm-unittest tests for unified values schema

#### Objective
Migrate all `set: policies.<name>.enabled: true` directives in test files to `set: vaps.<name>: true`.

#### Evidence
- `tests/vap_test.yaml`: 6 tests reference `policies.<name>.enabled` (lines 17, 27, 35, 44)
- `tests/multi-namespace_test.yaml`: 5 tests reference `policies.<name>.enabled` (lines 16, 27, 36, 50, 58)
- `tests/alerts_test.yaml`: does NOT reference `policies` — uses `alerts.enabled` (safe)
- `tests/cluster-image-policy_test.yaml`: does NOT reference `policies` — uses `clusterImagePolicy.*` (safe)

#### Files to edit
```
charts/loopzero-admission-policies/tests/vap_test.yaml            — all set: directives
charts/loopzero-admission-policies/tests/multi-namespace_test.yaml — all set: directives
```

#### Tasks
1. In `vap_test.yaml`: replace `policies.<name>.enabled: true` with `vaps.<name>: true` for all 6 tests
2. In `multi-namespace_test.yaml`: replace `policies.<name>.enabled: true` with `vaps.<name>: true` for all 5 tests
3. For `restrict-registries` tests: ensure registries values are set if template now reads from `.Values.registries.*`
4. Add new test case: `vap-protect-platform-networkpolicies has matchConditions` (B33 regression guard)
5. Run helm-unittest suite

#### TDD
```
RED:     helm unittest charts/loopzero-admission-policies → tests fail (templates changed, tests use old values)
GREEN:   Update set: directives + add matchConditions test
VERIFY:  helm unittest charts/loopzero-admission-policies → all pass
```

#### Acceptance Criteria
- [ ] Zero references to `policies` in test files
- [ ] All existing tests pass with `.Values.vaps.*` directives
- [ ] New test verifies matchConditions presence on protect-platform-networkpolicies
- [ ] restrict-registries test sets `.Values.registries.*` (not `allowedRegistries`)

#### DoD
- [ ] `helm unittest charts/loopzero-admission-policies` all GREEN
- [ ] No reference to `policies` in any file under `charts/loopzero-admission-policies/`

---

### T2.2 — Full validation sweep

#### Objective
Run all validation tools to confirm no regressions.

#### Tasks
1. `helm lint charts/loopzero-admission-policies`
2. `helm template charts/loopzero-admission-policies` (verify all enabled VAPs render)
3. `kubeconform --strict` on rendered output
4. `task validate:manifests`
5. `grep -r '.Values.policies' charts/loopzero-admission-policies/` → zero matches
6. Verify `infra/helm/admission-policies/` templates are still untouched (bootstrap is not modified by this plan)

#### Acceptance Criteria
- [ ] `helm lint` passes
- [ ] `helm template` renders 8 VAPs (6 migrated + 2 already on vaps.*) + bindings
- [ ] `kubeconform --strict` passes
- [ ] `task validate:manifests` passes
- [ ] Zero `.Values.policies` references in entire `charts/loopzero-admission-policies/` directory
- [ ] Bootstrap chart (`infra/helm/admission-policies/`) unchanged

#### DoD
- [ ] All validation tools GREEN
- [ ] Final grep confirms zero drift in values paths

---

## Phase 3: Documentation + CLAUDE.md Update

### T3.1 — Update CLAUDE.md Helm Chart Dual System section

#### Objective
Replace the generic "Changes to shared templates must be propagated to both systems" line with specific guidance about which charts overlap and what to check.

#### Files to edit
```
CLAUDE.md — "Helm Chart Dual System" section (line 223-235)
```

#### Tasks
1. Add note about which 3 charts have functional overlap (loopzero-api, loopzero-cloud, loopzero-admission-policies)
2. Replace generic propagation note with: "When patching a VAP in infra/helm/admission-policies/, apply the same fix to charts/loopzero-admission-policies/. Both use .Values.vaps.* for toggles and .Values.registries.* for registry prefixes."
3. Note that monitoring is complementary (not overlapping)

#### DoD
- [ ] CLAUDE.md section updated with specific overlap inventory
- [ ] Generic propagation note replaced with actionable guidance

---

### T3.2 — CHANGELOG entry

#### Tasks
1. Add entry under `[Unreleased] > Fixed` for B33 propagation
2. Add entry under `[Unreleased] > Changed` for values schema unification

#### DoD
- [ ] CHANGELOG.md updated with 2 entries

---

## Risk Assessment

| Risk | Probability | Impact | Mitigation |
|------|------------|--------|-----------|
| B33 matchConditions breaks namespace creation | Very Low | Low | matchConditions only *excludes* actors — it doesn't add new restrictions. Tested in bootstrap since 2026-05-05. |
| Removing `policies:` breaks ArgoCD overlay | Low | Medium | No external consumers (pre-release). Grep all ArgoCD app-of-apps manifests for `policies` references before removing. |
| CEL builder change in restrict-registries renders invalid CEL | Low | High | Bootstrap version has been validated in 12/12 dogfood cleanroom. `helm template` + `kubeconform` are mandatory gates. |
| helm-unittest tests miss a case | Low | Low | Tests already existed — we're only changing the values path, not the assertion logic. |

## Execution Order

1. **T0.1** — Deploy immediately (B33 hotfix, production safety)
2. **T1.1** → **T1.2** → **T1.3** (sequential — each depends on prior)
3. **T2.1** → **T2.2** (sequential — tests first, then validation)
4. **T3.1** + **T3.2** (parallel, documentation only)
