# securityContext Audit — infra/helm/

**Generated:** 2026-05-03T19:08:33Z
**Charts inspected:** 33
**Auditor:** scripts/audit-security-context.sh

## Matrix

| Chart | runAsNonRoot | readOnlyRootFS | drop:[ALL] | allowPrivilegeEscalation:false | Status | ADR Exception |
|---|---|---|---|---|---|---|
| admission-policies | n/a | n/a | n/a | n/a | N/A (no pods) | — |
| argocd | n/a | n/a | n/a | n/a | N/A (no pods) | — |
| argo-workflows | n/a | n/a | n/a | n/a | N/A (no pods) | — |
| blackbox-exporter | n/a | n/a | n/a | n/a | N/A (no pods) | — |
| cert-manager | yes | yes | yes | yes | CONFORMANT | — |
| chaos-mesh | yes | yes | yes | yes | CONFORMANT | — |
| cloudflared | yes | yes | yes | yes | CONFORMANT | — |
| cluster-issuer | n/a | n/a | n/a | n/a | N/A (no pods) | — |
| external-secrets | yes | yes | yes | yes | CONFORMANT | — |
| gitea | yes | yes | yes | yes | CONFORMANT | — |
| harbor | yes | yes | yes | yes | CONFORMANT | — |
| keda | yes | yes | yes | yes | CONFORMANT | — |
| keda-http-addon | yes | yes | yes | yes | CONFORMANT | — |
| loki | n/a | n/a | n/a | n/a | N/A (no pods) | — |
| minio | yes | yes | yes | yes | CONFORMANT | — |
| monitoring | n/a | n/a | n/a | n/a | N/A (no pods) | — |
| network-policies | n/a | n/a | n/a | n/a | N/A (no pods) | — |
| nfs-server-provisioner | yes | yes | yes | yes | CONFORMANT | ✅ exists |
| ory-hydra | yes | yes | yes | yes | CONFORMANT | — |
| ory-kratos | yes | yes | yes | yes | CONFORMANT | — |
| otel | yes | yes | yes | yes | CONFORMANT | — |
| policy-controller | n/a | n/a | n/a | n/a | N/A (no pods) | — |
| postgresql | yes | yes | yes | yes | CONFORMANT | — |
| promtail | yes | yes | yes | no | PARTIAL | ✅ exists |
| redis | yes | yes | yes | yes | CONFORMANT | — |
| smoke-test | yes | yes | yes | yes | CONFORMANT | — |
| static-assets | yes | yes | yes | yes | CONFORMANT | — |
| tempo | yes | yes | yes | yes | CONFORMANT | — |
| tenant-controller | yes | yes | yes | yes | CONFORMANT | — |
| loopzero-api | yes | yes | yes | yes | CONFORMANT | — |
| loopzero-cloud | yes | yes | yes | yes | CONFORMANT | — |
| loopzero-controller | yes | yes | yes | yes | CONFORMANT | — |
| traefik | yes | yes | yes | yes | CONFORMANT | — |

## Summary

- **CONFORMANT:** 23/33
- **PARTIAL:** 1/33
- **NON-CONFORMANT:** 0/33
- **N/A (no pods):** 9/33
- **NON-CONFORMANT without ADR:** 0

## Next Action

All NON-CONFORMANT charts have ADR exception. Audit gate would PASS.

## Closure (2026-05-03)

All 13 originally NON-CONFORMANT charts resolved per Plan ADR D7:

### Fixed (11)

| Chart | Action |
|---|---|
| `cert-manager` | Added `containerSecurityContext` block with hardened defaults |
| `external-secrets` | Added `podSecurityContext` + `containerSecurityContext` + per-component (webhook + certController) overrides |
| `keda` | Added per-component `securityContext` (operator + metricServer + webhooks) |
| `keda-http-addon` | Added top-level `podSecurityContext` + `securityContext` |
| `ory-hydra` | Added `podSecurityContext` + `securityContext` + `deployment.containerSecurityContext` |
| `ory-kratos` | Same pattern as ory-hydra |
| `otel` | Added `podSecurityContext` + extended `securityContext` |
| `postgresql` | Bitnami pattern: `primary.podSecurityContext.enabled=true` + `primary.containerSecurityContext` with full hardening; data dir is PVC mount, root FS readonly compatible |
| `redis` | Bitnami pattern: `master.podSecurityContext.enabled=true` + `master.containerSecurityContext` |
| `tempo` | Top-level `podSecurityContext`/`securityContext` + nested `tempo.containerSecurityContext` |
| `traefik` | Top-level hardening — internal ports >1024 so NET_BIND_SERVICE not required |

### ADR-of-exception (2)

| Chart | ADR file |
|---|---|
| `nfs-server-provisioner` | `infra/helm/nfs-server-provisioner/ADR-securitycontext-exception.md` |
| `promtail` | `infra/helm/promtail/ADR-securitycontext-exception.md` |

### Validation

```
$ bash scripts/audit-security-context.sh --gate
✅ securityContext audit GREEN — 0 NON-CONFORMANT charts without ADR exception.
$ echo $?
0
```

### Follow-up tasks

1. Live validation in k3d (`task k3d:up && helmfile sync && kubectl get pods -A` — all pods Running with correct securityContext applied).
2. INF-G08 status moves PARTIAL → MET in `improvement-roadmap.md` after live validation.
3. Polaris audit (T1.2) will provide finer-grained per-pod check; complementary to this script.
