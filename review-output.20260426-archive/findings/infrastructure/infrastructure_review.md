# Infrastructure Review — Phase 5
**Scope:** operators module infrastructure surface
**Date:** 2026-04-26
**Reviewer:** Chief Reviewer (infrastructure-reviewer + cicd-reviewer + dependency-analyzer roles)
**Findings registered:** 9 (INFRA-000 through INFRA-008)

---

## Tool Availability

| Tool | Status |
|------|--------|
| kubeconform | not installed — manual CRD inspection performed |
| govulncheck | not installed — CI workflow audited instead; go.mod versions inspected |
| yq | not installed — grep-based YAML inspection used |
| go | available (1.25+) |

---

## 5a — CI/CD Pipeline (Operator Image Build)

### Pipeline: `.github/workflows/build-images.yml`

**Trigger:** Push to `develop`, `homolog`, `main` (paths: `operators/**`, `pkg/**`, `go.work`, `**/Dockerfile`)

**Jobs:**
1. `resolve` — determines build targets from changed files
2. `build-operator` — builds and pushes `loopzero-tenant-controller:dev/sha`
3. `sign-and-attest` (matrix, `if: always()`) — Trivy scan → SBOM (syft) → cosign sign → cosign attest → verify

**Positives:**
- Provenance and SBOM flags: `--provenance=true --sbom=true` on every buildx invocation
- `SOURCE_DATE_EPOCH` set for reproducible builds
- Registry cache (`type=registry,mode=max`) for deterministic layer reuse
- cosign signing with keyless Fulcio/Rekor — no key material in workflow
- SBOM generated per component with syft, uploaded as artifact (90-day retention)
- Trivy scan with `exit-code: 1` and `severity: CRITICAL,HIGH`
- Signature verification smoke test after signing
- SHA-pinned: `actions/checkout@11bd71901bbe` and `docker/setup-buildx-action@b5ca514318bd`

**Findings:**

### INFRA-000 (HIGH): Build image pipeline runs concurrently with CI test gate

`build-images.yml` has no dependency on `ci.yml` completing. Both trigger independently on the same push. The `build-operator` job can succeed and push a broken image to DOCR even while `test-operators` is failing in a concurrent `ci.yml` run.

```
push to develop
  ├── ci.yml          (test-operators may FAIL)
  └── build-images.yml (build-operator PUSHES image regardless)
```

**Fix:** Gate `build-images.yml` on `ci-gate` success via `workflow_run`. Alternatively, inline `cd operators && go test ./... -short` as a step inside `build-operator` before `docker buildx build`.

### INFRA-003 (MEDIUM): Security-critical actions not SHA-pinned

In the `sign-and-attest` job:
- `sigstore/cosign-installer@v4.1.1` — mutable tag
- `anchore/sbom-action/download-syft@v0.24.0` — mutable tag
- `aquasecurity/trivy-action@v0.35.0` — mutable tag
- `digitalocean/action-doctl@v2.5.2` — mutable tag (used in every build job)
- `actions/upload-artifact@v4` — mutable tag

By contrast, `actions/checkout` and `docker/setup-buildx-action` are correctly SHA-pinned. The unpinned actions run in the pipeline that performs image signing — a compromised action maintainer could silently bypass or manipulate the Trivy scan, cosign signing, or push credentials.

**Fix:** Pin all third-party actions by commit SHA. Use Dependabot `github-actions` ecosystem to keep pins current.

### INFRA-005 (MEDIUM): Trivy scan runs POST-push — vulnerable image in registry before scan

The `build-operator` job pushes the image to DOCR first, then the separate `sign-and-attest` job runs Trivy. The vulnerable image sits in DOCR with the `:dev`/`:prod` tag written for ~10 minutes before any scan result is available. Additionally, `sign-and-attest` uses `fail-fast: false` in its matrix, so a Trivy failure in `loopzero-api` does not prevent the `loopzero-tenant-controller` matrix job from completing (and vice versa). The workflow-level success depends on all matrix jobs, but the window of exposure is real.

**Fix:** Move Trivy scan to before the push: build to local tar (`--output type=tar`), scan the tar, then push only if clean. Alternatively enable DOCR built-in vulnerability scanning with block-on-critical policy.

---

## 5b — Container and Deployment

### Dockerfile Analysis: `operators/Dockerfile`

```dockerfile
FROM golang:1.25.9-alpine3.22 AS builder   # pinned builder version
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"  # correct secure build flags
FROM alpine:3.22                            # runtime base
RUN apk add --no-cache ca-certificates && adduser -D -u 1000 loopzero
USER 1000
COPY --from=builder /loopzero-tenant-controller /usr/local/bin/loopzero-tenant-controller
EXPOSE 8080 8081
HEALTHCHECK --interval=30s ...             # present
ENTRYPOINT ["loopzero-tenant-controller"]
CMD ["--leader-elect=true"]                # secure default
```

**Positives:**
- Multi-stage build (builder + runtime)
- CGO_ENABLED=0, -trimpath, -ldflags="-s -w" (stripped, small, reproducible)
- Non-root user (uid 1000) created and applied
- ca-certificates only (no extra packages)
- HEALTHCHECK present at Dockerfile level
- `--leader-elect=true` as CMD default (production-safe default)
- Go workspace mode disabled (`GOWORK=off`) — deterministic module resolution

**Findings:**

### INFRA-007 (LOW): Runtime base is alpine:3.22, not distroless

Alpine includes `ash`, `apk`, and system utilities unnecessary for a compiled Go binary. The binary is statically linked with CGO_ENABLED=0 and only needs ca-certificates (included in distroless). If an attacker gains RCE in the operator, alpine provides a functional shell environment.

**Fix:** Use `gcr.io/distroless/static-debian12:nonroot` as runtime stage. Remove the Dockerfile HEALTHCHECK (distroless has no wget) — K8s liveness/readiness probes already handle health in the deployment manifest.

### Tilt Development Deployment: `infra/tilt/operator-deployment.yaml`

**Positives:**
- ServiceAccount created
- `runAsNonRoot: true`, `runAsUser: 1000`, `fsGroup: 1000` on pod spec
- `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true` on container
- readinessProbe (`/readyz:8081`) and livenessProbe (`/healthz:8081`) present
- Resource limits set: CPU 500m/100m, Memory 256Mi/64Mi
- emptyDir for `/tmp`

**Gaps vs Production:**

### INFRA-006 (LOW): Missing capabilities.drop=[ALL] and seccompProfile in dev deployment

Dev deployment (`infra/tilt/operator-deployment.yaml`) is missing:
```yaml
securityContext:            # container level
  capabilities:
    drop: ["ALL"]
securityContext:            # pod level
  seccompProfile:
    type: RuntimeDefault
```

Production (`mgmt-tenant-controller.ts`) correctly includes both. This means dev runs with more Linux capabilities than production.

### INFRA-002 (MEDIUM): Kubeconfigs mounted from ConfigMaps in dev deployment

Dev deployment mounts kubeconfigs from `configMap.name: loopzero-build-kubeconfig`. ConfigMaps are plaintext and unencrypted. Production correctly uses `k8s.core.v1.Secret` (see `mgmt-tenant-controller.ts` lines 109-124). Any pod that can `kubectl get configmap` in `loopzero-system` can read the kubeconfig.

Note: `OPERATOR_INSECURE_SKIP_TLS=true` is set in dev deployment with an explicit SEC-001 comment. Production omits this variable (defaults to false). This is correctly documented.

### Production Deployment: `infra/pulumi/src/components/mgmt-tenant-controller.ts`

**Positives:**
- kubeconfigs mounted from `k8s.core.v1.Secret` (correct)
- `imagePullPolicy: Always` (always fetches latest signed image)
- `seccompProfile: RuntimeDefault` on pod spec
- `capabilities: drop: ["ALL"]` on container
- `readonlyRootFilesystem: true`, `allowPrivilegeEscalation: false`
- `imagePullSecrets` for DOCR private registry
- `--leader-elect=true` in args
- All sensitive values from `secretKeyRef` (GITEA_TOKEN, SPACES keys, DO token)

---

## 5c — CRD Distribution and Versioning

### 10 CRDs in `operators/config/crd/bases/`

| CRD | API Version | Served | Storage | Conversion |
|-----|-------------|--------|---------|------------|
| tenants.loopzero.io | apiextensions.k8s.io/v1 | true | v1 | none |
| environments.loopzero.io | apiextensions.k8s.io/v1 | true | v1 | none |
| loopzeroapplications.loopzero.io | apiextensions.k8s.io/v1 | true | v1 | none |
| exposedroutes.loopzero.io | apiextensions.k8s.io/v1 | true | v1 | none |
| clustercredentials.loopzero.io | apiextensions.k8s.io/v1 | true | v1 | none |
| gitrepositorybindings.loopzero.io | apiextensions.k8s.io/v1 | true | v1 | none |
| platformbootstraps.loopzero.io | apiextensions.k8s.io/v1 | true | v1 | none |
| previewenvironments.loopzero.io | apiextensions.k8s.io/v1 | true | v1 | none |
| appsecrets.loopzero.io | apiextensions.k8s.io/v1 | true | v1 | none |
| reconciliationreports.loopzero.io | apiextensions.k8s.io/v1 | true | v1 | none |

**Positives:**
- All CRDs use `apiextensions.k8s.io/v1` (not deprecated v1beta1)
- All have `served: true`, `storage: true` for a single version
- No conversion webhooks needed (single-version design — correct for pre-release)
- openAPIV3Schema present and non-trivial (with `required`, `pattern`, `minLength` markers observed in tenants CRD)
- `status` subresource enabled (`subresources.status: {}`) for proper controller-runtime status updates
- Helm chart uses `"helm.sh/resource-policy": keep` annotation on all 10 CRD templates (prevents accidental deletion on helm uninstall)
- additionalPrinterColumns configured for useful kubectl output (Phase, Plan, Age on Tenants)
- SEC-011 pattern constraint on displayName prevents injection in tenants CRD

**Finding:**

### INFRA-004 (MEDIUM): CRD content sync not validated in CI — only count parity checked

CI validates `SRC=$(ls operators/config/crd/bases/) count == DST=$(ls charts/loopzero-crds/templates/) count`. Schema content parity is not validated. Sync is manual (`task sync-crds`). A developer who runs `controller-gen` to add a required field can forget to run `task sync-crds`, and the production chart will deploy the outdated schema.

Kubeconform is used in CI but with `--skip CustomResourceDefinition` on `operators/config/`. The chart templates are validated with `kubeconform` separately, but the comparison between source and chart is only a file count.

---

## 5d — RBAC Permission Matrix

### ServiceAccount: `tenant-controller` in `loopzero-system`

**ClusterRole: `tenant-controller`**

| API Group | Resources | Verbs | Justification | Risk |
|-----------|-----------|-------|---------------|------|
| loopzero.io | all 10 CRDs | get/list/watch/create/update/patch/delete | Full reconcile access | Acceptable — operator's domain |
| loopzero.io | */status | get/update/patch | Status subresource | Acceptable |
| loopzero.io | tenants/finalizers, etc. | update | Finalizer management | Acceptable |
| core | namespaces | get/list/watch/create/delete | Tenant namespace management | High (cluster-wide namespace delete) |
| core | resourcequotas, limitranges | get/list/watch/create/update | Quota enforcement | Acceptable |
| rbac | roles, rolebindings | get/list/watch/create/update | Tenant RBAC creation | Medium (cluster-wide scope) |
| core | serviceaccounts | get/list/watch/create | SA provisioning | Acceptable |
| networking.k8s.io | networkpolicies, ingresses | get/list/watch/create/update/delete | Tenant network isolation | Acceptable |
| **core** | **secrets** | **get/list/watch/create/update/delete** | **ArgoCD cluster secrets** | **HIGH — cluster-wide secret access** |
| scheduling.k8s.io | priorityclasses | get/list/watch/create/update | Workload priority | Acceptable |
| core | configmaps, services | get/list/create/update/patch | RBAC escalation prevention | Acceptable |
| apps | deployments | get/list/create/update/patch | RBAC escalation prevention | Acceptable |
| **core** | **deployments** | **get/list/create/update/patch** | **Backward compat** | **Dead rule — Deployments not in core API** |
| **apps** | **configmaps, services** | **get/list/create/update/patch** | **Backward compat** | **Dead rule — ConfigMaps/Services not in apps** |
| argoproj.io | applications, appprojects | get/list/watch/create/update/patch/delete | ArgoCD management | Acceptable |
| core | events | create | Observability | Acceptable |
| coordination.k8s.io | leases | get/list/watch/create/update/patch/delete | Leader election | Acceptable |

### INFRA-001 (HIGH): Cluster-scoped Secret CRUD

The operator holds cluster-wide `delete` on Secrets. It can delete `kube-system/` secrets, other operators' credentials, and platform secrets (Gitea, ArgoCD, MinIO). The `platformbootstrap_role.yaml` demonstrates the correct pattern: namespace-scoped Roles per namespace that needs Secret access.

**Correct pattern (from platformbootstrap_role.yaml):**
- Role in `loopzero-system`: get/list/create/update (no delete, no cluster-scope)
- Role in `argocd`: get/create/update (no delete, scoped to argocd namespace)
- ClusterRole: watch-only for CRD list/watch

**INFRA-008 (LOW): Stale backward-compat RBAC rules**

Lines 82-90 in `operator-deployment.yaml` add rules for `core/v1 Deployments` and `apps/v1 ConfigMaps/Services`. These API group + resource combinations do not exist in Kubernetes (Deployments are apps/v1, ConfigMaps/Services are core/v1). Kubernetes silently ignores them, but they pollute the RBAC manifest.

---

## 5e — Supply Chain

### govulncheck
Not installed locally. CI workflow (`ci.yml`, security job) runs `govulncheck ./...` for operators when `go-operators` files change. This is a correct and sufficient gate.

### Go Dependency Audit (`operators/go.mod`)

| Dependency | Version | Assessment |
|-----------|---------|------------|
| k8s.io/api | v0.35.2 | K8s 1.35.x — very recent |
| sigs.k8s.io/controller-runtime | v0.23.3 | Current for K8s 0.35.x |
| golang.org/x/crypto | v0.49.0 | Recent (Go security team releases) |
| golang.org/x/net | v0.52.0 | Recent |
| github.com/minio/minio-go/v7 | v7.0.90 | Recent |
| github.com/minio/madmin-go/v3 | v3.0.110 | Recent |
| go.opentelemetry.io/otel | v1.43.0 | Current stable |
| github.com/onsi/ginkgo/v2 | v2.28.1 | Current |
| go.yaml.in/yaml/v2 | v2.4.3 | Modern fork (not deprecated gopkg.in/yaml.v2) |

No `gopkg.in/yaml.v2` direct dependency (the notorious CVE-2022-28948 package) — the module uses the modern `go.yaml.in/yaml/v2` fork.

**Supply chain verdict:** Dependencies are current. No obviously stale or known-vulnerable packages visible in go.mod. govulncheck runs in CI as the definitive scan.

### Image Signing

Platform operator image (`loopzero-tenant-controller`) is included in the `sign-and-attest` matrix:
- Trivy scan (CRITICAL+HIGH, exit-code: 1)
- syft SBOM (SPDX-JSON format)
- cosign sign (keyless — Fulcio + Rekor)
- cosign attest (SBOM predicate)
- Signature verification smoke test

Note: This is keyless signing, unlike the tenant build images which use key-based cosign. Keyless provides GitHub OIDC identity anchoring but requires Rekor transparency log access for verification. The certificate identity is `https://github.com/loopzero/*`.

### License Audit
Go module licenses not audited with go-licenses (tool not installed). All observable major dependencies use Apache 2.0, MIT, or BSD licenses. No GPL dependencies identified in go.mod manual inspection.

---

## Summary

### Findings by Severity

| ID | Severity | Title |
|----|----------|-------|
| INFRA-000 | HIGH | Build image runs concurrently with CI — no test gate on push |
| INFRA-001 | HIGH | ClusterRole cluster-wide Secret CRUD including delete |
| INFRA-002 | MEDIUM | Dev deployment uses ConfigMap for kubeconfigs (vs Secret in prod) |
| INFRA-003 | MEDIUM | Security-critical actions not SHA-pinned in build-images.yml |
| INFRA-004 | MEDIUM | CRD schema drift risk — only count parity validated in CI |
| INFRA-005 | MEDIUM | Trivy scan runs after push — vulnerable image window in DOCR |
| INFRA-006 | LOW | Dev deployment missing capabilities.drop=[ALL] and seccompProfile |
| INFRA-007 | LOW | Runtime base alpine:3.22 instead of distroless |
| INFRA-008 | LOW | Stale backward-compat RBAC rules with invalid API groups |

### Strengths
- Cosign signing with provenance and SBOM for all platform images including operator
- Reproducible builds via SOURCE_DATE_EPOCH
- Production deployment correctly uses Secrets, seccompProfile, capabilities.drop
- Leader election enabled in production (not in dev — intentional for local dev)
- CRDs use apiextensions/v1, all have status subresource, single-version (no conversion webhooks)
- helm.sh/resource-policy: keep on all CRD chart templates
- govulncheck runs in CI for the operators module
- Build flags correct: CGO_ENABLED=0, -trimpath, -ldflags="-s -w"
