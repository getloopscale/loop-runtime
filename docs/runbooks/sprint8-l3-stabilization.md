# L3 Stabilization Runbook (Sprint 8 / CHG-002b)

**Status (2026-04-28 01:15 UTC):** L3 / L3-realistic stabilization in progress on DigitalOcean. Multiple root causes resolved through code + config fixes; pipeline now runs full-stack `dogfood-monorepo` builds end-to-end.

## Bugs Resolved (with code + config fixes)

| # | Bug | Fix | Where |
|---|---|---|---|
| 1 | `cosign-signing-key` Secret held literal placeholder string `PLACEHOLDER_RUN_PULUMI_CONFIG_SET_COSIGN` | Generate real cosign keypair, persist via `pulumi config set --secret`, recreate Secret | Operational + Pulumi config |
| 2 | `local-path` StorageClass aliased to `dobs.csi.digitalocean.com`, hitting DO API rate limit (429) under L3 concurrency | Install Rancher `local-path-provisioner` on build cluster, replace StorageClass to `provisioner: rancher.io/local-path`, `volumeBindingMode: WaitForFirstConsumer` | `infra/pulumi/src/components/build-argo.ts` (added `installLocalPathProvisioner` helper) |
| 3 | `tenant-controller` `MaxConcurrentReconciles=5` starved L3 (6 Active tenants monopolized budget) | Bump via `LOOPZERO_TENANT_MAX_CONCURRENT=20` env var | `infra/pulumi/src/components/mgmt-tenant-controller.ts` |
| 4 | `GITEA_TOKEN` Secret had trailing `\n` from `kubectl create secret --from-file`, breaking HTTP `Authorization: token <hash>\n` | Re-patch Secret with `printf '%s' \| base64 -w0`; document procedure | `docs/runbooks/gitea-token-rotation.md` |
| 5 | Loki saturated at default 5000 streams, blocking Promtail with 429s under L3 churn | Raise `max_global_streams_per_user: 100000`, `chunk_idle_period: 15m`, drop high-cardinality `step` + `pod` Promtail labels | `infra/helm/loki/values.yaml`, `infra/helm/promtail/values.yaml`, `infra/pulumi/src/components/build-loki.ts` |
| 6 | `clustercredential-controller` emitted raw PEM in ArgoCD cluster Secret `tlsClientConfig.caData`; ArgoCD requires base64-encoded PEM | Encode via `base64.StdEncoding.EncodeToString(restConfig.CAData)` | `operators/internal/controller/clustercredential_controller.go` (+ regression test) |
| 7 | ArgoCD application-controller SA lacked cluster-scoped read on `persistentvolumes` / `controllerrevisions` etc., blocking sync cache primer | Add catch-all read RBAC on `*/*` for `loopzero-ctrl-runtime-rbac-admin` ClusterRole | `infra/pulumi/src/components/runtime-bootstrap.ts` |
| 8 | `loopzero-packs` Node provider generated `npm run build` per app, ignoring monorepo dependency graph; cross-package TS imports failed at `tsc` with `Cannot find module '@dogfood/shared-utils'` | `loopzeropacks-generate` detects workspace at root; new `workspaceBuildCommand()` chooses `npx turbo run build --filter=<pkg>...` / `pnpm --filter <pkg>... run build` / `npm run build --workspaces --if-present`; resolve `appName` via member `package.json#name` so turbo's filter matches the registered package, not the directory | `loopzero-packs/cmd/loopzeropacks-generate/main.go`, `loopzero-packs/core/providers/node/node.go` (+ workspace_build_test.go) |
| 9 | LoopZero build pipeline ran `npm ci` + `npm run build` per-app dir, breaking workspaces (no hoisting, no dep resolution); duplicated work later done by BuildKit Dockerfile | Detect workspace at runtime in `scriptInstallNode` / `scriptBuildNode`; install + build at root with proper PM/turbo flags; standalone path preserved | `api/internal/argo/scripts.go` |
| 10 | L3 harness hardcoded `apps: [{api,node,3000}]` in `submitBuild`, dropping 3 of 4 apps for `dogfood-monorepo` | Parse `loopzero.yaml` at harness construction, build payload from real apps; verify URL parameterized via `LOOPZERO_VERIFY_APP_NAME` | `infra/tests/load/real_deploy_harness.go`, `infra/tests/load/real_deploy_test.go` |
| 11 | `gitops` repo archived on tenant delete; `EnsureRepo` returned URL without unarchiving, leaving `git-receive-pack` 403 on recreated tenant | Operational mitigation: batch unarchive of all gitops repos before each L3. Code fix queued in CHG-002b: `Archived bool` field on `RepoInfo` + `UnarchiveRepo()` API | Code fix pending; runbook covers operational fix |

## Bugs Pending (operational mitigation only)

| # | Bug | Severity | Operational mitigation |
|---|---|---|---|
| A | `gitea_pusher` wraps push errors as "failed after 3 attempts" without preserving stderr | Med | Currently log-diving; fix in `gitea_pusher.go` queued |
| B | `mirror-harbor` 401 on Harbor robot account race (best-effort `ContinueOn.Failed:true` masks it) | Low | None — non-blocking |
| C | Source bundle `git push` race on noisy-neighbor (`tn-0` 10 rapid deploys) | Low | Test gracefully accepts as expected |
| D | Build cluster autoscaler slow to expand 2→5 nodes under burst load | Med | `doctl kubernetes cluster node-pool update --count 5` pre-warm |
| E | DOCR + Docker Hub QPS limits cause `ImagePullBackOff` flakes mid-burst | Low | Self-recovers; cache hot images |

## L3-realistic Validation Procedure

Pre-conditions (verify before each run):

```bash
KCFG=/home/paulo/.kube/loopzero-loopzero-infra-dev-sfo3-mgmt.yaml
KCFG_BUILD=/home/paulo/.kube/loopzero-loopzero-infra-dev-sfo3-build.yaml

# 1. cosign Secret has real PEM
kubectl --kubeconfig=$KCFG_BUILD get secret -n argo cosign-signing-key \
  -o jsonpath='{.data.cosign\.key}' | base64 -d | head -1
# expected: -----BEGIN ENCRYPTED SIGSTORE PRIVATE KEY-----

# 2. local-path uses Rancher
kubectl --kubeconfig=$KCFG_BUILD get sc local-path \
  -o jsonpath='{.provisioner}'
# expected: rancher.io/local-path

# 3. tenant-controller concurrency
kubectl --kubeconfig=$KCFG get deploy tenant-controller -n loopzero-system \
  -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="LOOPZERO_TENANT_MAX_CONCURRENT")].value}'
# expected: 20

# 4. GITEA_TOKEN length 40, no trailing newline
kubectl --kubeconfig=$KCFG get secret -n loopzero-system loopzero-platform-tokens \
  -o jsonpath='{.data.GITEA_TOKEN}' | base64 -d | wc -c
# expected: 40

# 5. ArgoCD cluster Secrets have base64(PEM) caData
kubectl --kubeconfig=$KCFG get secret -n loopzero-system cluster-loopzero-infra-dev-sfo3-runtime \
  -o jsonpath='{.data.config}' | base64 -d \
  | python3 -c 'import json,sys;d=json.load(sys.stdin);ca=d.get("tlsClientConfig",{}).get("caData","");print("LS0t prefix:",ca.startswith("LS0t"))'
# expected: True

# 6. Build cluster pre-warmed to 5 nodes
doctl kubernetes cluster node-pool list loopzero-infra-dev-sfo3-build -o json \
  | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d[0]["count"])'
# expected: 5 (raise via `doctl kubernetes cluster node-pool update <cluster> <pool-id> --count 5`)
```

Reset between runs (DB + CRs):

```bash
# DB cleanup (use postgres:16-alpine pod with DATABASE_URL Secret)
# delete projects → tenant_members → subscriptions → tenants → users
# WHERE LIKE 'tn-lt-l3r-%' or 'github-lt-l3r-%'

# CR cleanup
kubectl --kubeconfig=$KCFG delete tenants.loopzero.io -A --all --wait=false
# Strip finalizers + delete: applications, loopzeroapplications, exposedroutes, appprojects (non-default)

# Workflow + PVC cleanup on build cluster
kubectl --kubeconfig=$KCFG_BUILD delete wf -n argo --all --wait=false
kubectl --kubeconfig=$KCFG_BUILD delete pvc -n argo --all --wait=false
```

Run:

```bash
SERVICE_AUTH=$(kubectl --kubeconfig=$KCFG get secret -n loopzero-system loopzero-api-secrets \
  -o jsonpath='{.data.LOOPZERO_SERVICE_AUTH_SECRET}' | base64 -d)

LOOPZERO_REAL_DEPLOY=1 \
LOOPZERO_LOAD_LEVEL=l3-realistic \
LOOPZERO_API_URL=https://api.dev.loopzero.example \
LOOPZERO_APP_DOMAIN=dev.loopzero.example \
LOOPZERO_SERVICE_AUTH="$SERVICE_AUTH" \
go test ./infra/tests/load/ -run L3Realistic -v -timeout 4h
```

## Iteration 2026-04-28 03:00-03:50 UTC — Bugs Resolved Pre-Destroy

This block captures the next layer of bugs uncovered after fixes 1-11 above. Findings carried into the next infra rebuild via this runbook + governance scope file.

### B12 — `buildkit-registry-config` Secret oscillation between DOCR and Harbor auths

**Symptom:** `mirror-harbor-*` Argo steps returned 401 Unauthorized intermittently. The pipeline `harbor_mirror.go` template comments expect a unified `config.json` with both DOCR and Harbor auths, but the Secret oscillated between Harbor-only (`Type: kubernetes.io/dockerconfigjson`) and DOCR-only (`Type: Opaque`) on every reconcile.

**Root cause:** Two writers in `tenant-controller` competed on the same Secret name:
- `tenant_build.go::ensureBuildkitRegistrySecret` wrote `Opaque` + DOCR-only
- `tenant_harbor.go::ensureHarborRegistrySecret` deleted-and-recreated as `DockerConfigJson` + Harbor-only

The constants `SecretBuildKitRegistryConfig` and `SecretHarborRegistryConfig` literally point to the same string `"buildkit-registry-config"` — anti-pattern aliasing.

**Fix (FAANG-level):** Host-scoped merge. Each writer reads existing Secret, adds/replaces only its own host entry in `auths`, preserves the other host. Single Type=`Opaque`, single key=`config.json`.
- New file: `operators/internal/controller/registry_auth_merge.go` (helper + 7 unit tests including 2-writer convergence proof)
- Refactored `ensureBuildkitRegistrySecret` and `ensureHarborRegistrySecret` to use `mergeAuthsByHost(existing, incoming)`
- Pre-flight delete preserved for legacy Secrets with `DockerConfigJson` Type

### B13 — Self-heal heuristic missed Harbor entry absence

**Symptom:** After B12 merge, Secret had only DOCR. Harbor robot existed but `ensureHarborRegistrySecret` never ran because `EnsureRobotAccount` returns `Secret == ""` for existing robots (Harbor never returns the password).

**Root cause:** `ensureHarborRobotAccounts` checked only "Secret K8s exists" to decide rotation. After B12 merge, Secret existed (with DOCR alone) — so the check returned true, no rotation triggered, no Harbor secret returned, no merge.

**Fix:** Replaced `secretExists()` with `secretHasHostAuth(host=HarborRegistryHost)` which parses the `config.json` and checks for the Harbor host entry. False → force robot delete-and-recreate.

### B14 — `DeleteRobotAccount` used wrong endpoint

**Symptom:** "robot not found, nothing to delete" log was a lie. Robot existed; delete didn't find it; `EnsureRobotAccount` then found it via project scope, saw valid TTL, returned no Secret, perpetuating B13.

**Root cause:** `DeleteRobotAccount` used only `findRobot()` (global endpoint). Project-level robots in Harbor 2.x have name `robot$<project>+<name>` and the global filter does not match them reliably. `EnsureRobotAccount` was already using `findRobotInProject()` first; delete should mirror.

**Fix:** `DeleteRobotAccount` now tries `findRobotInProject` first, falls back to `findRobot`. `operators/internal/harbor/client.go`.

### B15 — Annotation collision between DOCR and Harbor writers

**Symptom:** Some tenants had Harbor-only Secret with annotation `loopzero.io/token-expires-at: "2262-11-17"` (Harbor robot maxint). DOCR writer's `isRegistryTokenFresh()` saw fresh annotation, skipped re-write, never re-merged DOCR.

**Root cause:** Both writers wrote to the same annotation `loopzero.io/token-expires-at` with different semantics (DOCR token expiry vs Harbor robot expiry).

**Fix:**
- New constant `AnnotationHarborTokenExpiresAt = "loopzero.io/harbor-token-expires-at"`
- Harbor writer uses Harbor-scoped annotation only
- `isRegistryTokenFresh()` strengthened: also checks DOCR auth presence in `config.json`, not only annotation freshness. Helper `containsDOCRHost(host)` matches `registry.digitalocean.com[/...]`.

### B16 — sigstore policy-controller "deny by default" blocks all tenant Deployments

**Symptom:** ArgoCD created Service+PDB+Certificate+IngressRoute in tenant namespaces but **no Deployments**. Dry-run revealed: `admission webhook "policy.sigstore.dev" denied the request: validation failed: no matching policies`.

**Root cause:** Sprint 6 declared "Audit mode active" but never created a `ClusterImagePolicy`. The policy-controller's deny-by-default kicks in BEFORE `mode: warn` evaluation: with zero policies matching, the request is rejected as "no matching policies", regardless of mode. The 2-week observation window promised in Sprint 6 only manifests after at least one policy with `mode: warn` exists.

**Fix:** Created `ClusterImagePolicy/loopzero-images-warn` covering `registry.digitalocean.com/loopzero-system/**` and `harbor.dev.loopzero.example/**` with `mode: warn` + `static.action: pass`. Documented as temporary fallback to be replaced in Sprint 9 with cosign keyless verification using Fulcio.

**Architectural lesson:** The "Audit mode" abstraction in `policy-controller` is a property of policies, not of the controller globally. CLAUDE.md status was therefore subtly inaccurate — needs update.

### B17 — ArgoCD repo-server cached "manifest generation error" indefinitely

**Symptom:** 79/100 Apps stuck in `sync: Unknown` with `"production: app path does not exist (cached)"` long after the gitops repo had `production/` populated.

**Root cause:** ArgoCD repo-server caches manifest generation errors. The "(cached)" suffix in the condition message is the only hint. Annotation `argocd.argoproj.io/refresh: hard` per-app does refresh but is slow when applied via `for app in $(...); kubectl annotate`. Restarting the deployment flushes the entire cache in one operation.

**Mitigation:** `kubectl rollout restart deployment/argocd-repo-server`. Document procedure: after a fresh fleet bootstrap, always restart repo-server once builds start writing to gitops.

### B18 — Runtime pool sized for steady state, not burst

**Symptom:** First 25 Deployments scheduled, next 9 went `Pending: Insufficient cpu`. DOKS autoscaler reacted (3rd node tainted-uninitialized) but slowly.

**Root cause:** Runtime pool was 3× `s-2vcpu-4gb` (12 vCPU, 24 GB total) — sized for the existing baseline pods. L3-realistic schedules ~400 tenant containers (4 apps × 100 tenants × ~125m CPU req each = 50 vCPU sustained).

**Mitigation:** Manual `doctl kubernetes cluster node-pool update --count 8 --auto-scale --min 4 --max 12`. Codified in this runbook for next bootstrap.

## Pulumi Destroy Learnings (2026-04-28 04:00-05:00 UTC)

User pediu `pulumi destroy` ao final da iteração. Operação consumiu **3 rodadas + state surgery**. Aprendizados:

### Pre-flight obrigatório
1. **`pulumi destroy --refresh` falha hard quando há resources órfãos no DO API.** Um CDN endpoint deletado externamente (404) abortou o refresh inteiro — zero recursos destruídos. Solução: `pulumi state delete <urn>` no recurso órfão **antes** de `--refresh`. Procedure futuro: `--refresh` é opcional; preferir destroy sem `--refresh` quando se sabe que há órfãos, ou pre-clean state.

2. **CRDs com finalizers travam destroy por 5+ minutos cada.** Cada CRD `.loopzero.io` em deletion fica "scheduled for deletion" enquanto há CRs com finalizers. Solução: antes do destroy, strip finalizers em batch:
   ```bash
   for crd in tenants.loopzero.io environments.loopzero.io loopzeroapplications.loopzero.io \
              exposedroutes.loopzero.io platformbootstraps.loopzero.io \
              gitrepositorybindings.loopzero.io clustercredentials.loopzero.io \
              previewenvironments.loopzero.io reconciliationreports.loopzero.io appsecrets.loopzero.io; do
     kubectl --context loopzero-mgmt patch crd "$crd" --type=merge -p '{"metadata":{"finalizers":[]}}'
   done
   # Idem para Application/AppProject ArgoCD
   ```

### O caminho do destroy (ordem real)
1. K8s leaf resources primeiro (Deployments, Services, Secrets, ConfigMaps, IngressRoutes)
2. Helm releases (algumas com `keep` policy: PVCs, CRDs)
3. Namespaces (esperam children — CRs com finalizer travam)
4. CRDs
5. K8s providers
6. DigitalOcean Kubernetes clusters (~60s cada, paralelizável)
7. DigitalOcean Database Cluster (Redis ~60s, PG `[retain]` recusa)
8. Spaces buckets
9. Container Registry (`[retain]` recusa)
10. Cloudflare Tunnel
11. VPC (recusa enquanto DB ou Registry estão lá)

### Provider K8s + cluster destruído = retry infinito
Se um Helm release ou Namespace é registrado no Pulumi state mas o cluster que ele vivia já foi destruído, o provider K8s entra em loop infinito tentando alcançar o endpoint que não existe. **Sintoma:** "destroying........." sem progresso por 10+ minutos; logs sem novas linhas. **Cura:** `pulumi cancel`, depois `pulumi state delete --target-dependents <urn-do-namespace>`.

### `[PROTECT]` vs `[RETAIN]`
- `[PROTECT]`: `pulumi destroy` recusa deletar; precisa `pulumi state unprotect` antes.
- `[RETAIN]` (`retainOnDelete: true`): destroy remove do state mas NÃO da DO; o recurso sobrevive.
- Os dois combinados (PG + ContainerRegistry no nosso caso): preservados deliberadamente, sobrevivem a 100 destroys.

### Async tear-down de workers DOKS
`pulumi destroy` reporta `KubernetesCluster deleted` em ~1s. Mas os ~8 worker droplets continuam aparecendo em `doctl droplet list` por 5-10 min — DO faz cleanup assíncrono. Para parada IMEDIATA de billing: `doctl compute droplet delete <id>` direto. Para uso normal, ignorar.

### Recursos que sobreviveram (resultado final 2026-04-28 ~05:00 UTC)
- ContainerRegistry `loopzero-system` `[PROTECT][RETAIN]` — design
- DatabaseCluster `loopzero-infra-dev-sfo3-db` PG 16 `[PROTECT][RETAIN]` — design
- VPC `loopzero-infra-dev-sfo3-net` — preservada porque DB está dentro
- 2 droplets fora do IaC: `loopzero-code-vm` (142.93.254.245) e `loopzero-e2e-runner` (165.227.121.20)

### Recursos destruídos (~285)
- 4 DOKS clusters (build, mgmt, runtime, runtime-2)
- Redis HA database
- 5 Spaces buckets (artifacts, logs, builds, static-assets, container-images)
- CF Tunnel + Zero-Trust config
- ~262 K8s resources (Helm releases, Secrets, ConfigMaps, CRDs, RBAC, IngressRoutes, etc)

## Architectural Patterns Learned

1. **Aliased constants are a code smell.** `SecretBuildKitRegistryConfig` and `SecretHarborRegistryConfig` having the same string value lulled both writers into thinking they had unique ownership. Rename or unify with a single import.
2. **Self-heal heuristics must check semantic state, not container existence.** "Secret K8s exists" ≠ "Secret K8s contains the auth I'm responsible for". Always parse the contents you depend on.
3. **Merge primitives must be idempotent under interleaving.** Test cases must include both `[A,B,A,B]` and `[B,A,B,A]` orderings to prove convergence.
4. **Annotations are scoped resources.** When two controllers share a Secret, each should own a distinct annotation key. Sharing one annotation across writers leads to silent mutual stomp.
5. **Webhook "Audit mode" is a property of CRs, not the webhook config.** Bootstrap procedures must include "create at least one warning-mode policy" as a Day-0 step.
6. **Cache invalidation needs a fleet-level button.** Per-resource hard-refresh is too slow at 100+ Apps. `rollout restart` of repo-server is the right tool; should be standard runbook step.

## Open Follow-ups (CHG-002b parts 2+)

- Eliminar `install-deps` + `build-apps` Argo Workflow steps (delegate fully to BuildKit Dockerfile via `loopzero-packs`). Current pipeline duplicates install/build between native steps and Dockerfile multi-stage. **FAANG end-state**: 5 steps (clone, validate, scan, generate-dockerfile, build-image+sign+sbom+mirror) instead of 9.
- `gitea_pusher.go` error wrapping: surface underlying git stderr in wrapped error.
- `EnsureRepo` Unarchive: add `Archived` field to `RepoInfo`, wire `UnarchiveRepo` through CB provisioner, ensure tenant recreation flow.
- `BuildQueueDepth` Gauge accounting: every `Inc()` must be paired with `Dec()` on every terminal state — current monotonic growth observed by SRE Guardian indicates orphan increments on failure paths.
- `validate-workspace` step should detect monorepo mode and emit a marker file consumed by downstream steps (eliminates per-step grep).
- `cosignPrivateKeyB64` fail-fast: `infra/pulumi/src/components/build-secrets.ts` should `throw` (not `?? "PLACEHOLDER..."`) when the cosign config is missing in non-dev stacks.
