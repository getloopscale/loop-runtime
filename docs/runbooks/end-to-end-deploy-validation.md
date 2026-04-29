# End-to-End Deploy Validation — Runbook

> Companion to `docs/plans/end-to-end-deploy-validation-plan.md`. Contains the
> **executable procedure** to bootstrap mgmt+build+workload clusters in k3d and
> exercise `loopzero deploy` happy path locally.

## Purpose

Validate the full caminho usuário→CLI→API→Argo→ArgoCD→pod-running locally
before relying on DOKS production. Companion to Rule 10 (Local equals
production) — the goal is to discover bugs locally that would otherwise
break a fresh DOKS bootstrap.

## Prerequisites

| Tool | Version | Verify |
|---|---|---|
| docker | latest | `docker version` |
| k3d | v5.6+ | `k3d version` |
| kubectl | v1.28+ | `kubectl version --client` |
| helmfile | v1.0+ | `helmfile --version` |
| helm | v3.14+ | `helm version --short` |
| doctl | latest | `doctl version` |
| cosign | v2.4+ | `cosign version` (install: `go install github.com/sigstore/cosign/v2/cmd/cosign@v2.4.1`) |
| loopzero CLI | latest | `loopzero --version` |

Authentication:
- `doctl auth login` — DO Registry pull
- `loopzero login` — LoopZero Cloud (or local if Ory Kratos seeded)

## Memory Budget

| Component | Memory (approx) |
|---|---|
| Host base (OS + dev tools) | ~2GB |
| loopzero-mgmt cluster (1 server + 2 agents, 22 releases) | ~5GB |
| loopzero-build cluster (1 server + 1 agent, 3 releases) | ~2GB |
| loopzero-workload cluster (1 server + 1 agent, 13 releases) | ~3GB |
| **Total** | **~12GB** |

Recommended: 16GB RAM minimum. If <14GB, run phases serially with
`task cleanup:incremental` between each.

## Execution Sequence

### Phase 0: Pre-flight

```bash
task cleanup:incremental                # Free ~1GB
free -g | awk '/^Mem:/ {print $7}'      # Should be ≥ 7
task validate:layered-bootstrap          # Sanity: existing tests still pass
```

### Phase 1: Build cluster

```bash
bash infra/scripts/bootstrap-cluster.sh build
KUBECONFIG=$(k3d kubeconfig get loopzero-build) bash infra/helmfile/scripts/generate-build-secrets.sh standalone-multi build
helmfile -f infra/helmfile/build.helmfile.yaml.gotmpl -e standalone-multi --kube-context k3d-loopzero-build sync --concurrency 2
bash infra/helmfile/scripts/health-check.sh build
```

**Expected:** 3 helm releases (argo-workflows, loki, promtail) deployed; cosign-signing-key Secret with real keypair (not PLACEHOLDER).

### Phase 2: Workload/runtime cluster

```bash
bash infra/scripts/bootstrap-cluster.sh workload
bash infra/helmfile/scripts/generate-runtime-secrets.sh standalone-multi runtime
helmfile -f infra/helmfile/runtime.helmfile.yaml.gotmpl -e standalone-multi --kube-context k3d-loopzero-workload sync --concurrency 2
bash infra/helmfile/scripts/health-check.sh runtime
```

**Expected:** 13 helm releases deployed; cert-manager webhook Ready; Traefik routing; KEDA operator running; admission-policies in warn mode.

### Phase 3: Cross-cluster wiring

```bash
export LOOPZERO_MGMT_KUBECONFIG=$(k3d kubeconfig get loopzero-mgmt)
export LOOPZERO_BUILD_KUBECONFIG=$(k3d kubeconfig get loopzero-build)
export LOOPZERO_RUNTIME_KUBECONFIG=$(k3d kubeconfig get loopzero-workload)
bash infra/helmfile/scripts/wire-clusters.sh standalone-multi
bash infra/helmfile/scripts/create-wiring-configmap.sh standalone-multi
```

**Expected:** ArgoCD lists 3 clusters; loopzero-platform-runtime-rbac-admin ClusterRole has resources:["*"]; loopzero-platform-topology ConfigMap has cross-cluster URLs.

### Phase 4: LoopZero deploy E2E

```bash
LOOPZERO_E2E_DEPLOY=1 go test ./infra/tests/e2e/ -run TestE2EDeployHappyPath -v -timeout 30m
```

**Expected:** workload pod 1/1 Running; HTTP 200 from URL.

### Phase 5: Validation

```bash
LOOPZERO_REGRESSION_PROFILE=multi-cluster bash infra/helmfile/scripts/regression-checks.sh
go test ./infra/tests/load/ -run "TestB|TestRegression" -count=1
task validate:layered-bootstrap
```

**Expected:** all PASS (with new B59+ tests added during phases 1-4).

## Troubleshooting

### k3d cluster create fails: "Failed to find registry node 'k3d-loopzero-registry'"
Run `task registry:create` first (creates shared k3d registry).

### Build helm sync fails on argo-workflows: ImagePullBackOff
Verify pull secret in argo namespace: `kubectl --context k3d-loopzero-build -n argo get secret loopzero-registry-pull`. Re-create via `bash infra/scripts/bootstrap-cluster.sh build`.

### wire-clusters.sh fails caData encoding
B6 protection. If recurs, dump caData and validate PEM:
```bash
kubectl --context k3d-loopzero-mgmt -n argocd get secret <cluster-secret> -o jsonpath='{.data.config}' | base64 -d | jq .tlsClientConfig.caData | base64 -d
```
Must output `-----BEGIN CERTIFICATE-----`.

### Memory pressure mid-bootstrap
Run `task cleanup:incremental`. If still tight, scale down non-essential mgmt
pods (e.g., `kubectl scale deploy loopzero-controller --replicas=0`). NEVER delete
mgmt cluster mid-bootstrap.

### loopzero deploy hangs at "build queued"
Check Argo Workflows: `kubectl --context k3d-loopzero-build -n argo get workflows`.
Check loopzero-api logs: `kubectl --context k3d-loopzero-mgmt -n loopzero-system logs -l app=loopzero-api`.

## Cleanup (full teardown)

```bash
k3d cluster delete loopzero-mgmt loopzero-build loopzero-workload
k3d registry delete loopzero-registry
docker system prune -af --volumes
```

⚠️  This deletes ALL state. Use only when starting fresh.
