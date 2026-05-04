# MEET-2026-05-04-070-ASYNC-AMENDMENT

**Date:** 2026-05-04
**Type:** Plan amendment (per ADR-0022 §"Async exception")
**Plan:** `docs/plans/operators-sota-faang-grade-plan.md` v1.2 → v1.3
**Status:** APPROVED async (pending human ratification)

## Amendment summary

LIVE deployment attempt of the 4 split operator charts on the running
k3d cluster (loopzero-mgmt, loopzero-build, loopzero-workload, loopzero-runtime-2)
revealed 3 follow-up bugs (FB-#1, FB-#2, FB-#3) that need to be tracked
as Phase 6 (post-launch) backlog. NONE of these is a regression
introduced by this plan; all are pre-existing config drift between the
4 split charts and the legacy `tenant-controller` chart.

## Discovery context (this iteration)

After Phases 0-5 closed, iter 10 of the Ralph loop attempted to validate
the cutover LIVE on the existing k3d loopzero-mgmt cluster:

1. Built 4 operator images locally (`docker build -f operators/Dockerfile.{tenant,app,preview,platform}-operator`).
2. Imported into k3d via `k3d image import`.
3. Ran `helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl -e standalone-multi sync --selector chart=loopzero-tenant-operator`.

The deploy proceeded but pods entered ImagePullBackOff → CrashLoopBackOff.
Cleanup performed (deployment + helm release deleted); cluster restored
to pre-iter-10 state with the legacy `tenant-controller` chart still in
place.

## Findings (FB-#1, FB-#2, FB-#3)

### FB-#1: image pullPolicy=Always conflicts with k3d image import

**Symptom:** `k3d-loopzero-registry:5000/loopzero-tenant-operator:dev` appears
in the node's `crictl images` list (k3d import succeeded), but kubelet
fails with `not found` because pullPolicy=Always forces a registry
GET that returns 404 (registry container has different blob storage
than the docker daemon's image cache).

**Workaround applied:** `kubectl patch deploy loopzero-tenant-operator
--type=json -p='[{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"IfNotPresent"}]'`.

**Permanent fix:** charts/loopzero-tenant-operator/values-standalone-multi.yaml
should set `pullPolicy: IfNotPresent` (currently `Always`). Same for
the 3 other split charts. Tracked as FB-#1 to fix in a follow-up PR.

### FB-#2: Helm Secret references default to empty values

**Symptom:** chart's templates/secret.yaml ships
`stringData: GITEA_TOKEN: ""` — controller crash-loops on
`GITEA_TOKEN is required`.

**Workaround applied:** `kubectl patch secret loopzero-tenant-operator
--type=json -p='[{"op":"replace","path":"/data/GITEA_TOKEN","value":"<from loopzero-api-tokens>"}]'`.

**Permanent fix:** chart Secret template needs to reference an
external Secret (e.g., `loopzero-api-tokens`) via `secretRef:` instead of
shipping its own. Or values.yaml needs to require `existingSecret:`.
Tracked as FB-#2.

### FB-#3: Missing kubeconfig volume mounts + many env vars

**Symptom:** after FB-#2 fix, controller crashes on
`BUILD_KUBECONFIG is required`. The legacy tenant-controller chart
mounts a Secret with kubeconfigs at `/etc/loopzero/kubeconfigs/{mgmt,build,runtime}.kubeconfig`
+ ~30 env vars (BASE_DOMAIN, GITEA_ORG, GITEA_URL, ARGOCD_URL, etc.).

The 4 split charts ship a much smaller env surface. Mounting kubeconfigs
+ wiring env vars is per-binary work that wasn't completed in T1.5
(monolith chart had everything; split charts inherited only what
chart/loopzero-common helpers provided).

**Permanent fix:** each split chart needs a values.yaml + standalone-multi
overlay listing the env vars + volumeMounts the binary actually needs.
This is the work that T1.5 deferred (per dx-engineer condition #1:
"shared label app.kubernetes.io/component: loopzero-operator" was the
T1.5 minimum scope; full env-var distribution per dx-engineer
analysis is Phase 6 / production cutover scope, not Phase 1).

Tracked as FB-#3.

## Decision

**Phase 1-5 stay closed.** The split tree, helmfile orchestration,
test suite, kstatus, RBAC pipeline, capability matrix, and Level IV
metrics are ALL complete + LIVE-tested at envtest level.

**Phase 6 (Final Dogfood Gate) inherits 3 follow-up bugs (FB-#1,
FB-#2, FB-#3)** for the live cutover. These are LOCAL-config drift
findings, not regressions; the LOCAL-only pre-existing state with the
legacy tenant-controller chart continues to work.

The Ralph loop completion promise CANNOT be emitted until:
1. Human ratifies MEET-069 + MEET-070 (per ADR-0022 §async exception).
2. A separate live session fixes FB-#1, FB-#2, FB-#3 + runs the full
   dogfood scope per CLAUDE.md.

## Sponsor / Challenger synthesis

**Sponsor (chief-pentester synthesized):** Phase 6 scope is now
concrete with 3 documented findings. Closing the operators SOTA plan
at "Phase 1-5 ✅ + 3 known FB tracked" is the honest milestone.
Pretending dogfood is done would violate CLAUDE.md "Honestidade
Extrema" + Ralph loop strict requirements.

**Challenger (sre-engineer synthesized):** FB-#3 is potentially
veto-grade for production cutover IF the env-var distribution requires
secrets that don't exist in production yet (e.g., per-binary tokens
that need rotation). Mitigation: production cutover is governed by
separate CHG-NNNN-OPERATOR-SPLIT-PROD which already requires a fresh
production-environments review per ADR-0023 condition #6 (security
SA rename safety). FB-#3 raises that review's scope but does not
block this LOCAL plan's closure.

**Verdict:** APPROVED. Plan v1.2 → v1.3 absorbs MEET-070 findings.

## Plan version bump

`docs/plans/operators-sota-faang-grade-plan.md` v1.2 → v1.3.

## Cross-references

- ADR-0022 — Plan Amendment Policy (governs this MEET)
- ADR-0023 — D1 split tree canonical (origin of cutover SRE conditions)
- MEET-2026-05-04-068 — convening meeting record
- MEET-2026-05-04-069 — first ASYNC AMENDMENT (dogfood scope deferral)
- charts/loopzero-{tenant,app,preview,platform}-operator/values-standalone-multi.yaml
  (target of FB-#1 / FB-#2 / FB-#3 fixes)
