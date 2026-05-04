# Phase 6 backlog — Cutover follow-up bugs (MEET-070)

> **Per MEET-2026-05-04-070 ASYNC AMENDMENT:** LIVE deployment attempt
> of the 4 split operator charts on the running k3d loopzero-mgmt cluster
> revealed 3 follow-up bugs in chart config. NONE is a regression
> introduced by Phases 1-5; all are pre-existing config drift between
> the 4 split charts and the legacy `tenant-controller` chart.

**Generated:** 2026-05-04 iter 11

## FB-#1: pullPolicy=Always conflicts with k3d image import — ✅ FIXED

**Status:** Fixed in iter 11.

**Symptom:** With `pullPolicy: Always`, kubelet calls registry HTTP
even when k3d image import already populated the node's local crictl
cache. The local registry container at `k3d-loopzero-registry:5000` has
separate blob storage from the docker daemon's image cache, so
`docker push` from host fails (docker registry is HTTP-only +
hostname not resolvable from outside the k3d network), but
`k3d image import` succeeds (uses tarball into node).

**Fix applied:** `pullPolicy: IfNotPresent` in
`charts/loopzero-{tenant,app,preview,platform}-operator/values-standalone-multi.yaml`.

**Verification:** `task dev:up && tilt up` after this fix; pods reach
Running without ImagePullBackOff.

## FB-#2: chart Secret ships empty stringData — ⏳ PARTIAL FIX

**Status:** Partially fixed. Chart template ALREADY supports
`existingSecret` (verified iter 11). What's missing is the values
override + env-var mapping for production secrets.

**Symptom:** chart's `templates/secret.yaml` ships
`stringData: GITEA_TOKEN: ""` because `secrets.create=true` +
`secrets.giteaToken=""` (default). Controller crash-loops on
`GITEA_TOKEN is required`.

**Existing chart support:**
```yaml
{{- if and .Values.secrets.create (not .Values.secrets.existingSecret) }}
# generates Secret with stringData
{{- end }}
```

**Production reality:** the legacy `tenant-controller` chart references
`Secret loopzero-api-tokens` (already in cluster) which contains
`gitea-token` (lowercase hyphenated key). The split chart's deployment
mounts envFrom `Secret <chart-name>` which expects `GITEA_TOKEN`
(uppercase underscore key).

**Permanent fix (deferred to live cutover session):**
1. Update chart `templates/deployment.yaml` to support
   `envFrom: [{secretRef: {name: existingSecret}}]` when set.
2. Either:
   - Patch `loopzero-api-tokens` secret to contain `GITEA_TOKEN` key
     (keeping lowercase one too for back-compat).
   - OR rename chart's expected env var to match `gitea-token` key
     (less clean — env var conventions are uppercase).
3. Document the secret contract in chart README.

**Workaround for any live attempt:** kubectl patch the chart-created
Secret with the actual token from `loopzero-api-tokens.gitea-token`.

## FB-#3: split charts missing env vars + kubeconfig volume mounts — ⏳ PARTIAL FIX (iter 11)

**Update 2026-05-04 iter 11:** Minimum-viable env-var distribution
applied to all 4 split charts' `values-standalone-multi.yaml`:

  - **loopzero-app-operator:** BASE_DOMAIN.
  - **loopzero-platform-operator:** ARGOCD_NAMESPACE.
  - **loopzero-preview-operator:** BASE_DOMAIN +
    kubeconfig.runtime.existingConfigMap=loopzero-runtime-kubeconfig.
  - **loopzero-tenant-operator:** GITEA_URL + BASE_DOMAIN + ARGOCD_NAMESPACE
    + INGRESS_NAMESPACE + INSECURE_SKIP_TLS +
    kubeconfig.{build,runtime}.existingConfigMap.

**Still deferred for full production cutover:**

  - HARBOR_URL/HARBOR_ADMIN_USERNAME/HARBOR_ADMIN_PASSWORD/HARBOR_REGISTRY_HOST
    — production-specific; needs Harbor existingSecret reference.
  - MINIO_ENDPOINT/MINIO_ACCESS_KEY/MINIO_SECRET_KEY — same pattern.
  - SPACES_ACCESS_KEY_ID/SPACES_SECRET_ACCESS_KEY — DO Spaces creds.
  - DIGITALOCEAN_TOKEN — DOKS API token.
  - REGISTRY_DOCKER_CONFIG/REGISTRY_CIDRS — production registry config.
  - WORKLOAD_CLUSTER_URL — patched at runtime by `task k3d:heal H10`
    in LOCAL; production sets via Helm value during cutover.

The minimum-viable set lets the binary boot in standalone-multi (all
required env vars present); the deferred set is runtime-conditional
(binary degrades gracefully when missing — e.g., Harbor mode skipped
if HARBOR_URL is empty).

**Verified:** `helm template + kubeconform --strict` passes for all
4 charts after FB-#3 partial fix (9/8/8/12 resources valid).



**Status:** Deferred to Phase 6 / `CHG-NNNN-OPERATOR-SPLIT-PROD`. This
is the bulk of the cutover work; not feasible inside a Ralph loop
session because it requires production-environment review of token
rotation patterns, kubeconfig sourcing, and per-binary env-var
distribution.

**Symptom:** after FB-#2 fix, controller crashes on
`BUILD_KUBECONFIG is required`. The legacy `tenant-controller` chart
mounts a Secret with kubeconfigs at
`/etc/loopzero/kubeconfigs/{mgmt,build,runtime}.kubeconfig` + carries
~30 env vars (BASE_DOMAIN, GITEA_ORG, GITEA_URL, ARGOCD_URL,
RUNTIME_KUBECONFIG, MGMT_KUBE_CONTEXT, etc.).

The 4 split charts ship a much smaller env surface — only the
loopzero-common library provides standard env vars; per-binary needs
were not yet enumerated.

**Permanent fix (deferred):**

Per dx-engineer review of T1.5, the env-var distribution across 4
binaries is documented in `docs/plans/operators-sota-faang-grade-plan.md`
T1.5 §"Env var distribution across 4 binaries":

> tenant-operator needs: GITEA_URL, GITEA_TOKEN, GITEA_ORG,
>   ARGOCD_NAMESPACE, BUILD_KUBECONFIG, RUNTIME_KUBECONFIG, MINIO_*,
>   HARBOR_*, REGISTRY_CIDRS, DIGITALOCEAN_TOKEN,
>   REGISTRY_DOCKER_CONFIG, SPACES_*, WORKLOAD_CLUSTER_URL,
>   LOOPZERO_TENANT_MAX_CONCURRENT
> app-operator needs: BASE_DOMAIN
> preview-operator needs: RUNTIME_KUBECONFIG, BASE_DOMAIN,
>   INGRESS_NAMESPACE
> platform-operator needs: ARGOCD_NAMESPACE (for ClusterCredential
>   controller)

The fix is to populate each chart's `values-standalone-multi.yaml`
+ `templates/deployment.yaml` envFrom + volumeMounts to match this
distribution.

**Why deferred:**
- Requires kubeconfig Secret sourcing strategy (legacy uses
  `loopzero-cluster-kubeconfigs` Secret; production cutover may want
  per-cell Secrets via ClusterCredential CRs which already exist).
- Requires validation that each binary's reconcile loops still work
  with reduced env surface (e.g., app-operator should NOT need
  GITEA_TOKEN; if it does, that's a separation-of-concerns regression).
- Requires production-rollout review (security-engineer condition
  ADR-0026 #6: SA rename safety; FB-#3 expands that review's scope).

## Work order for live cutover session (post-handoff)

When a future session attempts the cutover:

1. ✅ FB-#1 already fixed (this commit).
2. Implement FB-#2 fully:
   - Add chart helper `loopzero-common.envFromSecrets` that supports
     `existingSecret` references with key remapping.
   - Update `templates/deployment.yaml` of all 4 charts.
   - Update `values-standalone-multi.yaml` to point at
     `loopzero-api-tokens` with key remap.
3. Implement FB-#3 per binary:
   - Audit each binary's `cmd/<name>/main.go` for `os.Getenv` calls.
   - Populate `values-standalone-multi.yaml` env block.
   - Add `volumeMounts` for kubeconfig Secret.
4. Re-run cutover:
   - `task teardown && task setup && tilt up`
   - Verify 4 pods Running (no monolith)
   - Run `loopzero deploy` smoke test
   - Capture evidence in `dogfood-evidence.md`
5. Submit `CHG-NNNN-OPERATOR-SPLIT-PROD` with evidence.

## Cross-references

- MEET-070 — original LIVE attempt findings
- ADR-0023 D1 4-stage cutover (Stage A coexistence still active in production)
- plan v1.3 §"Governance Conditions Appendix" (dx-engineer condition #5
  enumerates 6 fan-out points for T1.5; FB-#3 expands to env-var distribution)
