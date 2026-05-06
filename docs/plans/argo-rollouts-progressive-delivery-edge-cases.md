# Edge Case Review — argo-rollouts-progressive-delivery

Data: 2026-05-06
Plano: docs/plans/argo-rollouts-progressive-delivery-plan.md
Tasks analisadas: 14 (T0.1, T0.2, T1.1, T1.2, T1.3, T1.4, T2.1, T3.1, T4.1, T4.2, T4.3, T5.1, T5.2)
Edge cases encontrados: 14 (MUST FIX: 5, SHOULD TEST: 5, DOCUMENT: 4)

---

## MUST FIX

### EC-1: KEDA scale-to-zero + Rollout: HTTPScaledObject routes to Service, but now there are 2 Services (stable + canary)

- **Task afetada:** T1.3, T1.4, T3.1
- **Familia:** Resource
- **Cenario:** Today, `buildHTTPScaledObject()` (renderer_deployment.go:265) creates an HTTPScaledObject with `scaleTargetRef.service = resName` pointing to the single Service. The plan creates 2 Services (`{resName}-stable` and `{resName}-canary`) and REMOVES the original `{resName}` Service. The HTTPScaledObject still references `{resName}` which no longer exists. Additionally, the KEDA HTTP interceptor proxy (`keda-add-ons-http-interceptor-proxy`) routes to the Service named in `scaleTargetRef.service` — this Service is gone.
- **Impacto:** Scale-to-zero apps will never wake up. The KEDA interceptor holds requests but has no valid backend Service to proxy to. All traffic to scale-to-zero apps returns 502 or hangs indefinitely.
- **Fix sugerido:** When progressive delivery is active, `HTTPScaledObject.scaleTargetRef.service` must point to `{resName}-stable` (the stable Service). The IngressRoute for scale-to-zero apps currently points to the KEDA interceptor (not the app Service), so this path is independent of the TraefikService — BUT the interceptor needs to know which backend to proxy to, and that must be the stable Service. Also: the `kedaHTTPScaleTargetRef.Name` (which is the Deployment/Rollout name) must match, and `kedaHTTPScaleTargetRef.Service` must be `{resName}-stable`.

### EC-2: IngressRoute `services[].kind` field missing — cannot point to TraefikService

- **Task afetada:** T1.4
- **Familia:** Format
- **Cenario:** The plan says IngressRoute will point to TraefikService instead of Service. But the current `traefikRouteService` struct (renderer_types.go:214) has fields `Name`, `Namespace`, `Port`, `Scheme` — no `Kind` field. Traefik IngressRoute differentiates between a Kubernetes Service and a TraefikService via the `kind` field in the route services block: `kind: TraefikService` vs default `kind: Service`. Without this field, Traefik always resolves to a Kubernetes Service. The plan says "Traefik resolve TraefikService antes de Service" — this is incorrect. Traefik requires explicit `kind: TraefikService` in the IngressRoute route service reference.
- **Impacto:** IngressRoute will try to resolve `{resName}` as a Kubernetes Service (which no longer exists — only `{resName}-stable` and `{resName}-canary` exist). Traffic returns 503 for all non-scale-to-zero apps.
- **Fix sugerido:** Add `Kind string json:"kind,omitempty"` to `traefikRouteService` struct. When progressive delivery is active, set `Kind: "TraefikService"` and omit `Port` (TraefikService already knows the ports from its weighted services definition). The TraefikService name must match the IngressRoute service name.

### EC-3: First deploy ever — Rollout has no stable ReplicaSet, canary analysis has no baseline traffic

- **Task afetada:** T1.3, T5.1
- **Familia:** State
- **Cenario:** On the first deploy of a new app, there is no existing Deployment or Rollout. Argo Rollouts creates the Rollout and immediately starts the canary strategy. But there are zero stable pods (no previous revision exists). The AnalysisTemplate queries `traefik_service_requests_total` for success rate, but with zero traffic to the stable Service, the Prometheus query returns `NaN` (0/0 division). `successCondition: "result[0] >= 0.95"` evaluates `NaN >= 0.95` as false. The analysis fails, triggering auto-abort on the very first deploy.
- **Impacto:** Every new app's first deploy is auto-aborted. The app never becomes healthy.
- **Fix sugerido:** Add `failureCondition: "result[0] < 0.90"` instead of (or alongside) `successCondition`, and set `inconclusive: "isNaN(result[0])"` on the metric. When inconclusive, Argo Rollouts treats the step as passed (auto-promotes). Alternatively, use `count: 0` (dry-run mode) for Starter plan to skip analysis entirely on first deploys, and detect first-deploy in the renderer by checking if Rollout already exists (but this complicates manifest generation — the `inconclusive` approach is simpler and handles sparse-traffic apps too).

### EC-4: Feature flag flip (Deployment to Rollout): ArgoCD sees delete + create, NOT in-place migration

- **Task afetada:** T1.4, T2.1
- **Familia:** State
- **Cenario:** When `LOOPZERO_PROGRESSIVE_DELIVERY` flips from `false` to `true` (or on first enable for existing apps), the Git repo changes from containing a `Deployment` to containing a `Rollout`. ArgoCD sees this as: delete `apps/v1/Deployment/{resName}` + create `argoproj.io/v1alpha1/Rollout/{resName}`. The delete happens first. During the window between delete and create, there are ZERO pods running. The app has a full outage. For apps with scale-to-zero (replicas=0), the delete may not cause visible impact, but for all others this is a hard outage.
- **Impacto:** Every existing app experiences a full outage (duration depends on ArgoCD sync wave ordering and Rollout startup time — typically 30-60 seconds minimum). For a PaaS with multiple tenants, this is a coordinated outage across ALL tenant apps when the flag flips.
- **Fix sugerido:** The plan must define a migration strategy. Option A (recommended): gradual per-tenant rollout — add `Plan` field to control progressive delivery per tenant, enable for new tenants first, then migrate existing tenants one at a time with a maintenance window. Option B: use ArgoCD sync waves — annotate the Rollout with `argocd.argoproj.io/sync-wave: "-1"` so it is created BEFORE the Deployment is deleted, and let Argo Rollouts adopt the existing pods via matching labels. Option B requires that the Rollout's `spec.selector` matches the Deployment's selector exactly (which it does in this plan — both use `app: {resName}`). Argo Rollouts documentation confirms it can adopt existing ReplicaSets from Deployments. Add this as a documented migration procedure in the plan.

### EC-5: KEDA ScaledObject + Rollout interaction — scale-to-zero means zero stable AND zero canary pods

- **Task afetada:** T3.1, T1.3
- **Familia:** State
- **Cenario:** When KEDA scales to zero (all pods terminated), a new deploy triggers a Rollout update. Argo Rollouts starts the canary strategy but finds zero stable pods. It creates canary pods per the `maxSurge` setting. But with `replicas: 0` in the Rollout spec (KEDA manages), the Rollout controller sees desired=0 and may not create canary pods at all, OR creates them and KEDA immediately scales them back to 0. The interaction between KEDA's replica management and Argo Rollouts' canary strategy when `replicas: 0` is undefined in either project's documentation.
- **Impacto:** Canary rollout may hang indefinitely (waiting for pods that KEDA keeps killing) or skip canary entirely and promote immediately (no safety benefit).
- **Fix sugerido:** When `isScaleToZero` is true AND progressive delivery is active, the Rollout should use `replicas: 1` (not 0) and let KEDA handle the scale-down AFTER the rollout completes. Add annotation `scaledobject.keda.sh/transfer-hpa-ownership: "true"` and configure `spec.strategy.canary.minPodsPerReplicaSet: 1` to ensure at least 1 pod exists during canary. After the Rollout completes (stable), KEDA can scale back to 0. This requires testing — add to T5.1 acceptance criteria.

---

## SHOULD TEST

### EC-6: PDB selector matches Rollout pods — but Argo Rollouts creates pods with rollout-specific labels

- **Task afetada:** T1.4
- **Familia:** Resource
- **Cenario:** `buildPDB()` (renderer_deployment.go:310) creates a PDB with selector `{app: resName, loopzero.io/tenant: X, loopzero.io/project: Y}`. Argo Rollouts creates pods with the labels from `spec.template.metadata.labels` PLUS its own labels (`rollouts-pod-template-hash`). The PDB selector uses `matchLabels` which requires ALL labels to match — since the PDB selector is a subset of the pod labels, this should work. BUT verify that Argo Rollouts does not REMOVE any labels from the template during canary (it should not, but this is a critical assumption).
- **Impacto:** If PDB does not match Rollout pods, voluntary disruptions (node drain) could kill all pods simultaneously.
- **Fix sugerido:** Add E2E assertion in T5.1: after Rollout completes, verify `kubectl get pdb {resName}-pdb -o jsonpath='{.status.currentHealthy}'` shows expected count > 0.

### EC-7: Prometheus unreachable during analysis — AnalysisRun hangs or fails

- **Task afetada:** T1.3
- **Familia:** Boundary
- **Cenario:** The AnalysisTemplate uses a Prometheus provider with `address: {prometheusAddress}`. If Prometheus is unreachable (network partition, pod crash, DNS failure), the Prometheus provider returns an error. Argo Rollouts' default behavior on provider error depends on `failureLimit` — each failed query counts toward the limit. With `failureLimit: 3` and `interval: 30s`, after 90 seconds of Prometheus downtime the analysis fails and the Rollout is aborted. This means a Prometheus outage causes ALL in-flight deploys to abort, even if the apps are healthy.
- **Impacto:** Prometheus infrastructure outage cascades into deploy failures across all tenants.
- **Fix sugerido:** Add `errorCondition` handling or increase `failureLimit` for infrastructure errors. Better: add `consecutiveErrorLimit: 4` (separate from `failureLimit`) to the metric spec — this tells Argo Rollouts to only fail on provider errors after 4 consecutive failures, independently of measurement failures. Also add this as a DOCUMENT risk: Prometheus availability is now on the critical path for deploys.

### EC-8: Worker Rollout with single pause step — auto-promote or hang?

- **Task afetada:** T1.3
- **Familia:** State
- **Cenario:** The plan defines worker canary steps as `[{Pause: {Duration: "30s"}}]`. After the pause, Argo Rollouts reaches the end of the steps list and auto-promotes to 100% stable. This is correct behavior. BUT: if the pause has no `duration` field (just `{Pause: {}}` — indefinite pause), the Rollout hangs forever waiting for manual promotion. The plan correctly specifies `Duration: "30s"`, but verify the struct `k8sCanaryPause` has the `Duration` field and it serializes correctly (not omitted by `omitempty` when empty string).
- **Impacto:** Worker deploys hang forever if Duration field is accidentally omitted.
- **Fix sugerido:** Add unit test: `test_worker_rollout_pause_has_duration()` — verify the rendered YAML contains `duration: 30s` in the pause step. Ensure `k8sCanaryPause.Duration` is `json:"duration"` (not `json:"duration,omitempty"`).

### EC-9: Custom domains IngressRoute — must ALSO point to TraefikService

- **Task afetada:** T1.4
- **Familia:** Resource
- **Cenario:** `buildIngressResources()` generates additional IngressRoutes for custom domains (renderer_ingress.go:106-177). The plan only mentions updating the primary IngressRoute to point to TraefikService. Custom domain IngressRoutes currently point to the same `serviceTarget` (either direct Service or KEDA interceptor). When progressive delivery is active, these must ALSO point to the TraefikService (with `kind: TraefikService`), otherwise custom domain traffic bypasses canary and goes directly to... nothing (the original Service no longer exists).
- **Impacto:** Custom domain traffic returns 503 during and after deploy.
- **Fix sugerido:** Update the custom domain loop in `buildIngressResources()` to use the same conditional logic as the primary IngressRoute. Add test: `test_custom_domain_ingress_points_to_traefik_service()`.

### EC-10: Concurrent deploy + Rollout in-progress — Rollout handles this, but state machine must agree

- **Task afetada:** T4.1
- **Familia:** Concurrency
- **Cenario:** If a second deploy starts while the Rollout is mid-canary (e.g., at 20%), the Git push updates the Rollout spec with a new image. Argo Rollouts handles this natively: it aborts the current canary and restarts with the new image from step 0. This is correct. BUT: the API's `withEnvironmentLock` + concurrent-build check (which rejects the second deploy with 409) means this scenario should never happen for the SAME project+environment. However, for DIFFERENT projects in the same namespace (shared namespace `loopzero-{tenant}-{env}`), two Rollouts progressing simultaneously is normal and expected.
- **Impacto:** Low risk — the existing concurrency model handles this. But the SSE event stream for `rollout.step` must correctly identify WHICH project's rollout is reporting, not confuse events from different projects.
- **Fix sugerido:** Ensure `rollout.step` SSE event includes `project_id` and `app_name` fields. Add test: verify two concurrent deploys (different projects, same env) produce correctly scoped SSE events.

---

## DOCUMENT

### EC-11: ArgoCD sync timeout with multi-step canary — sync may timeout before canary completes

- **Task afetada:** T2.1
- **Familia:** Timing
- **Cenario:** ArgoCD Application sync has a timeout (typically 300s default). A Team plan canary with steps at 10%/30%/60% with 30s pauses + analysis takes at minimum 90s pauses + analysis time. For Pro plan: 60s pause + analysis. If analysis takes time (slow Prometheus, retry on inconclusive), the total canary duration may exceed the ArgoCD sync timeout. ArgoCD would mark the sync as "failed" even though the Rollout is progressing normally.
- **Impacto:** ArgoCD shows "Sync Failed" in dashboard, potentially triggering alerts. The Rollout continues independently (Argo Rollouts controller manages it, not ArgoCD). No data loss but confusing operational signals.
- **Fix sugerido:** Set ArgoCD Application `spec.syncPolicy.syncOptions: ["RespectIgnoreDifferences=true"]` and document that ArgoCD sync status for Rollout apps should show "OutOfSync" during canary (this is expected). Add `argocd.argoproj.io/compare-options: IgnoreExtraneous` if needed. Document this as expected behavior in runbooks.

### EC-12: Rollout revision history and `loopzero rollback` interaction

- **Task afetada:** (not explicitly covered)
- **Familia:** State
- **Cenario:** `loopzero rollback` currently works by reverting the Git commit in Gitea and triggering ArgoCD sync. With Rollouts, Argo Rollouts maintains its own revision history. A Git revert creates a new Rollout spec update (with the old image), which Argo Rollouts treats as a NEW rollout (starts canary from step 0 with the old image). This means rollback is NOT instant — it goes through the full canary process. The plan's claim "Rollback proativo (auto-abort em bad deploy)" is about aborting a BAD deploy, not about `loopzero rollback` being fast.
- **Impacto:** `loopzero rollback` goes from ~3s (current: Git revert + ArgoCD sync) to potentially 2-5 minutes (canary steps for the reverted image). Acceptable for safety but a UX regression.
- **Fix sugerido:** Document this behavior change. Optionally, `loopzero rollback` could use `kubectl argo rollouts undo` which reverts to the previous ReplicaSet instantly (like Deployment rollback) without going through canary. This is a product decision — document for Sprint 9.

### EC-13: Stable/canary Service selectors are identical — Argo Rollouts injects rollout-pod-template-hash

- **Task afetada:** T1.3
- **Familia:** Resource
- **Cenario:** The plan shows both stable and canary Services with `selector: {app: resName}`. This is correct because Argo Rollouts injects `rollouts-pod-template-hash` into the Service selectors at runtime to distinguish stable from canary pods. The rendered manifests will have identical selectors, and Argo Rollouts patches them. This is expected Argo Rollouts behavior, but ArgoCD will see this as drift (the selector changed from what is in Git).
- **Impacto:** ArgoCD shows OutOfSync on the stable/canary Services during canary. This is cosmetic but may trigger alerts.
- **Fix sugerido:** Add to `ignoreDifferences` in T2.1: `{group: "", kind: "Service", jsonPointers: ["/spec/selector/rollouts-pod-template-hash"]}`. Document that Argo Rollouts patches Service selectors at runtime.

### EC-14: AnalysisTemplate Prometheus query uses Traefik service metric name format

- **Task afetada:** T1.3
- **Familia:** Format
- **Cenario:** The AnalysisTemplate query is `traefik_service_requests_total{service=~"{{args.service-name}}.*"}`. Traefik's metric label `service` uses the format `{namespace}-{serviceName}@kubernetesprovider` (e.g., `loopzero-acme-staging-acme-api-stable@kubernetesprovider`). The `args.service-name` value must match this format. If the plan passes just `{resName}` as the arg, the regex `{resName}.*` will match both stable and canary services (since both start with the same resName prefix), defeating the purpose of canary-specific analysis.
- **Impacto:** Analysis measures combined stable+canary success rate instead of canary-only. A bad canary deploy's errors are diluted by healthy stable traffic, making the analysis less sensitive. Auto-abort may not trigger.
- **Fix sugerido:** Pass `{resName}-canary` as the service-name arg to measure canary traffic only. Or use the full Traefik service name format: `{namespace}-{resName}-canary@kubernetesprovider`. Document the metric format and add a test that verifies the rendered AnalysisTemplate arg matches the canary Service name.

---

## Resumo

| Task | Edges encontrados | MUST FIX | SHOULD TEST | DOCUMENT |
|------|-------------------|----------|-------------|----------|
| T0.1 | 0 | 0 | 0 | 0 |
| T0.2 | 0 | 0 | 0 | 0 |
| T1.1 | 0 | 0 | 0 | 0 |
| T1.2 | 0 | 0 | 0 | 0 |
| T1.3 | 5 | 2 (EC-3, EC-5) | 2 (EC-7, EC-8) | 1 (EC-14) |
| T1.4 | 4 | 2 (EC-2, EC-4) | 2 (EC-6, EC-9) | 0 |
| T2.1 | 1 | 0 | 0 | 1 (EC-11) |
| T3.1 | 1 | 1 (EC-1) | 0 | 0 |
| T4.1 | 1 | 0 | 1 (EC-10) | 0 |
| T4.2 | 0 | 0 | 0 | 0 |
| T4.3 | 0 | 0 | 0 | 0 |
| T5.1 | 0 | 0 | 0 | 0 |
| T5.2 | 0 | 0 | 0 | 0 |
| (cross) | 2 | 0 | 0 | 2 (EC-12, EC-13) |

**Veredicto: PLANO PRECISA DE AJUSTE**

5 MUST FIX issues identified. The most critical are:

1. **EC-1** (KEDA + Rollout): Scale-to-zero apps will break completely. HTTPScaledObject references a Service that no longer exists.
2. **EC-2** (IngressRoute kind field): ALL non-scale-to-zero traffic will 503 because IngressRoute cannot reference a TraefikService without a `kind` field in the struct.
3. **EC-3** (First deploy NaN): Every new app's first deploy will auto-abort due to NaN in Prometheus query.
4. **EC-4** (Migration outage): Flipping the feature flag causes coordinated outage across all existing apps.
5. **EC-5** (KEDA + Rollout replicas=0): Scale-to-zero + canary is an undefined interaction.

EC-1 and EC-2 are the highest priority — without fixing these, zero traffic will be served after enabling progressive delivery.
