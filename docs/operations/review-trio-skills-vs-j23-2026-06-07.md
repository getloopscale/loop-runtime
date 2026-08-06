---
type: operational-runbook
title: "Review: cleanroom + dogfood full + chaos-test + live-proof vs J-23 2026-06-07 LIVE catches"
---

# Review: cleanroom + dogfood full + chaos-test + live-proof vs J-23 2026-06-07 LIVE catches

**Created:** 2026-06-07
**Sponsor mandate:** "quero que salve uma tasks ou em um lugar para revisr nosso cleanroom + dogfood full + chao-test + live-prof"
**Tracked task:** #72
**Status:** Open — awaiting review session
**Cross-refs:**
- `[[reference_j23_batch_2026_06_07]]` (memory) — the 8 commits + 9 bugs LIVE catched
- `[[feedback_swallowed_silent_errors_pattern]]` (memory) — the systemic class of all 4 LIVE catches

---

## The question

The J-23 batch landed `5a8132fc` → `347fdbc0` on 2026-06-07. Of the 9 bugs closed in that batch, **4 were caught only during the manual sponsor-guided regression test session AFTER the dogfood-preview wrapper said GREEN**:

| # | Bug | Why dogfood missed it |
|---|---|---|
| #65b | loopzero-api SA had ZERO RBAC for `previewenvironments` | Dogfood scenarios called the cluster as cluster-admin, not as loopzero-api SA |
| #70 | `tenant_controller.SetupWithManager` had no `Watches(PreviewEnvironment)` | Dogfood test forced reconcile via `kubectl annotate`, masking the watcher gap |
| #5 | `DeletePreviewCR` used synthetic name that never matched real CR name | Dogfood test never ran a real PR close → cleanup path |
| #71 | `tenant_gitops` had no prune pass for orphan ArgoCD Apps | Dogfood test created Apps but never observed them across multiple PR open/close cycles |

**Question to answer in the review session**: should the trio (cleanroom + dogfood full + chaos-test + live-proof) be extended to catch this class of bug? If yes, where exactly?

---

## Trio canonical sequence (current state)

| Phase | Skill | What it does | Time | Surface coverage |
|---|---|---|---|---|
| 1 | `/cleanroom` | Destroys + recreates infra k3d local (4 clusters, helmfile sync, tilt up, doctor 38/0) | ~30-45min | Infrastructure boot reproducibility |
| 2 | `/dogfood full` | Executes journeys CLI → API end-to-end (login, deploy, scale, rollback, delete) | ~60min | User-facing happy paths |
| 3 | `/chaos-test` | Injects failures (pod-kill, network-partition, IO-pressure) and validates SLO | ~60min | Resilience under controlled chaos |
| 4 | `/live-proof` | Byte-grep between commit content + LIVE state (env vars, ConfigMap, Helm values) | ~10s | Commit ↔ deployed state drift |

---

## Gaps detected by the J-23 regression test

### Gap 1 — Nothing runs a real PR webhook → preview → close cycle

**Symptom:** `/dogfood full` validates `loopzero deploy` but **does NOT** trigger a GitHub PR webhook against `getloopscale/loopzero-repotest` and walk the entire preview lifecycle (opened → synchronize → closed). The PR webhook path is what wired #65b + #5 + #70 + #71 together — without it, all four can fail in production and the dogfood is silent.

**Proposed fix:**

- Add to `/dogfood full` a new journey `PR preview lifecycle` that:
  1. Pushes a commit to `getloopscale/loopzero-repotest` on a fresh branch.
  2. Opens a PR via `gh pr create`.
  3. Asserts: webhook delivered, build submitted, PreviewEnvironment CR Active, namespace canonical exists, SA `loopzero-app` has automount=false, ArgoCD App `tn-<x>-preview-pr-N` Synced/Healthy, HTTPS via wildcard returns 200.
  4. Closes the PR via `gh pr close --comment` (or merge via `gh pr merge`).
  5. Asserts: webhook delivered, PreviewEnvironment CR deleted, namespace Terminating → GONE, ArgoCD App pruned within 1 reconcile.
- Equivalent of what we now have in `infra/tests/e2e/dev-public-preview-flow_test.sh` (5 scenarios A-E) BUT with the close-cycle scenarios F-G added.

### Gap 2 — Nothing checks operator watcher wiring

**Symptom:** `/cleanroom` boots the operators. `/dogfood full` exercises CLI flows that trigger reconciles. Neither **inspects** the `SetupWithManager` builder to assert that every cross-resource flow has a corresponding `Watches(...)` call. The #70 bug was: `tenant_gitops` reads PreviewEnvironment but had no Watches(PreviewEnvironment) — invisible to dogfood until a PreviewEnvironment was created without a concurrent Tenant CR change.

**Proposed fix:**

- Add a static-analysis Go test that asserts every `controllerruntime.NewControllerManagedBy(...)` chain in operators/ has:
  - `For(<primary>)` — required
  - `Owns(<owned>)` — for every owned resource (already checked)
  - `Watches(<sibling>, MapFunc)` — for every sibling CR type the reconcile READS (this is the new check)
- The sibling-read check is detectable: grep the reconcile body for `List(<Kind>)` or `Get(<Kind>)` of a different loopzero.io kind than the primary For type. If found, assert there's a matching Watches in SetupWithManager.

### Gap 3 — Nothing cross-validates create vs delete shape symmetry

**Symptom:** `EnsurePreviewCR` set `metadata.name = spec.Name` (UUID). `DeletePreviewCR` constructed a synthetic name `<tenant>-<project>-pr-<N>`. Both compiled, both unit-tested. The create-create-delete roundtrip was never exercised in a hermetic test that ASSERTS the deleted resource is GONE — only that Delete returned nil.

**Proposed fix:**

- For every K8s CR manager package, require a "creation roundtrip" integration test:
  1. Call Ensure / Create (capture the actual CR object via List)
  2. Call Delete via the public API
  3. List again — assert ZERO matches
- This test would have failed immediately for #5 because the Delete returned nil but the List still showed 1 match.

### Gap 4 — Nothing audits RBAC drift between Tilt YAML and published charts

**Symptom:** `charts/loopzero-api/templates/clusterrole.yaml` grants `loopzero.io/*` wildcard (covers `previewenvironments` automatically). `infra/tilt/api-deployment.yaml` enumerates each resource explicitly (`tenants`, `loopzeroapplications`, `environments`, `clustercredentials`) — and `previewenvironments` was missing. Prod was safe; dev k3d (which uses Tilt) was broken silently.

**Proposed fix:**

- Add a kubeconform-style gate: for every `apiGroups: ["loopzero.io"]` rule in the chart, assert the matching rule exists in the Tilt YAML (and vice versa). The published chart's wildcard hides the issue but the chart's `--set rbac.legacyClusterWideSecrets=false` will eventually flip — and at that point, drift between chart and Tilt becomes a prod regression source.

### Gap 5 — /live-proof doesn't probe RBAC

**Symptom:** `/live-proof` does byte-grep between commit + deployed state (ConfigMap values, env vars, Helm-rendered manifests). It does NOT execute `kubectl auth can-i ...` to verify SA permissions match the documented intent.

**Proposed fix:**

- Add a `live-proof:rbac` mode that:
  1. Parses every `kind: ClusterRole` in charts/ and infra/tilt/
  2. For each rule, runs `kubectl auth can-i <verb> <resource> --as=system:serviceaccount:<ns>:<sa>`
  3. Asserts the live-cluster answer matches the documented rule.
- Would have caught #65b in <10s on a fresh cleanroom.

---

## Skill-by-skill review checklist

To execute when the review session runs.

### /cleanroom

- [ ] Verify `task setup` completes 4 clusters + 38/0 doctor pass
- [ ] Verify `tilt up` deploys all operators and they reach Ready
- [ ] Verify all loopzero.io CRDs land with current schemas (including `Project` field on TenantEnvironmentEntry per #69)
- [ ] **NEW:** Verify Tilt RBAC matches chart RBAC for all loopzero.io types (Gap 4)
- [ ] **NEW:** Run `kubectl auth can-i` matrix for every SA × every loopzero.io verb against expected intent (Gap 5)

### /dogfood full

- [ ] Current scenarios: login, deploy, scale, rollback, delete via CLI
- [ ] **NEW:** Add `PR preview lifecycle` journey end-to-end (Gap 1)
  - opened → synchronize (commit push) → closed (via gh pr close + merge variants)
  - assert webhook delivery + CR lifecycle + namespace lifecycle + ArgoCD App lifecycle
- [ ] **NEW:** Add "creation roundtrip" assertion to every K8s CR manager package test (Gap 3)
- [ ] **NEW:** Add SetupWithManager static-analysis gate (Gap 2)

### /chaos-test

- [ ] Current scenarios: pod-kill (API leader), network-partition, IO-pressure, gitea-outage, PG-connection-storm
- [ ] **REVIEW:** Does any chaos scenario tear down PreviewEnvironment CRs while watcher is firing? (would catch #70 missing watcher under chaos conditions)
- [ ] **REVIEW:** Does any chaos scenario test orphan App accumulation? (e.g., 10 PRs open + close in burst — would catch #71 under load)

### /live-proof

- [ ] Current scenarios: env vars + ConfigMap byte-grep
- [ ] **NEW:** Add `live-proof:rbac` mode (Gap 5)
- [ ] **NEW:** Add `live-proof:watchers` mode that asserts operator SetupWithManager has Watches for every cross-CR dependency

---

## Review session deliverables

When this review is executed, the deliverables should be:

1. **Decision per gap:** Accept / Defer / Reject (with rationale)
2. **Spec for each accepted fix:** which file, which test, who runs it (CI vs operator)
3. **Updated trio sequence:** if any gap fix adds a new phase, document the order
4. **New baseline run:** after fixes land, re-run trio against dev k3d + capture clean baseline per `.claude/rules/steady-state-baseline.md`

---

## Anti-patterns to avoid in the review

1. **"We caught all 4 in regression so it's fine"** — the regression test was a 90min sponsor-guided session. That doesn't scale. The trio needs to catch this class within 60min unattended.
2. **"Just add hermetic tests"** — unit/hermetic tests catch logic bugs. The 4 J-23 bugs were CONTRACT bugs (RBAC, watcher, delete shape, prune missing) that only surface in a live multi-component chain. Hermetic alone is insufficient.
3. **"chaos-test will catch it"** — chaos-test injects failures into a baseline state. It assumes the steady state is correct. None of the 4 bugs was a chaos-induced failure; they were steady-state bugs.
4. **"The dogfood-preview wrapper is enough"** — that wrapper validates the J-23 chain in isolation. It does NOT cover the full trio sequence + does NOT validate other journeys (J-10, J-17) didn't regress.

---

## Next steps

1. Sponsor schedules a review session (1-2hr) to walk through this doc.
2. Accept/defer each gap, write specs for accepted ones.
3. Implement spec, re-run trio, capture baseline.
4. Close task #72.
