# Polaris Baseline

## Status

- **Plan:** `docs/plans/infra-sota-hardening-plan.md` T1.2
- **Meeting:** MEET-2026-05-03-062 (MEET-A) condition #1: baseline-first; promote `severity=danger` after baseline review.
- **Config:** `.polaris.yaml`
- **Wrapper:** `scripts/run-polaris.sh`

## Why baseline-first

Per MEET-A condition #1: enabling Polaris in `enforce` mode without a baseline pass would block legitimate deploys (e.g., `promtail` requiring `hostPath`). Baseline-first allows engineering to triage warnings and either:

1. Fix the issue (preferred for issues with a clean fix).
2. Add chart to `.polaris.yaml` `exemptions` block with documented reason.
3. Promote check from `warning` to `danger` once all charts pass.

## Severity matrix (current)

| Check | Severity | Reason |
|---|---|---|
| `runAsRootAllowed` | warning | Phase 3.2 fixed via securityContext; promote to `danger` after iter 2 validation |
| `runAsPrivileged` | **danger** | No legitimate use in LoopZero platform |
| `notReadOnlyRootFilesystem` | warning | Some charts (StatefulSets writing to PVC) need partial RW |
| `privilegeEscalationAllowed` | **danger** | Plan ADR D7 standard |
| `dangerousCapabilities` | **danger** | Plan ADR D7 standard |
| `insecureCapabilities` | warning | NET_BIND_SERVICE may be needed for Traefik |
| `hostIPCSet` | **danger** | Cross-pod IPC is a tenant isolation violation |
| `hostPIDSet` | **danger** | Same |
| `hostNetworkSet` | warning | Required for promtail (per ADR-of-exception) |
| `hostPortSet` | warning | Used by Traefik LB; tracked separately |
| `cpuLimitsMissing` | ignore | KEDA scale-to-zero needs no limits |
| `cpuRequestsMissing` | warning | Required for HPA |
| `memoryLimitsMissing` | warning | Required for OOM protection |
| `memoryRequestsMissing` | warning | Required for HPA |
| `livenessProbeMissing` | **danger** | INF-G08 Secure Defaults |
| `readinessProbeMissing` | **danger** | INF-G08 Secure Defaults |
| `tagNotSpecified` | **danger** | INF-G19 Reproducibility (no `:latest`) |
| `pullPolicyNotAlways` | ignore | `IfNotPresent` preferred (cache-friendly) |
| `deploymentMissingReplicas` | warning | Default 1 is fine for non-HA charts |
| `priorityClassNotSet` | ignore | Set selectively per chart |

## Exemptions

| Chart | Exempted checks | Reason | ADR file |
|---|---|---|---|
| `promtail` | runAsRootAllowed, notReadOnlyRootFilesystem, hostNetworkSet | Log collection requires hostPath + hostNet | `infra/helm/promtail/ADR-securitycontext-exception.md` |
| `nfs-server-provisioner` | runAsRootAllowed, runAsPrivileged, notReadOnlyRootFilesystem, dangerousCapabilities, insecureCapabilities | NFS daemon requires kernel module + mount syscall | `infra/helm/nfs-server-provisioner/ADR-securitycontext-exception.md` |

## How to run

### Local

```bash
# Install Polaris (once)
curl -L https://github.com/FairwindsOps/polaris/releases/latest/download/polaris_linux_amd64.tar.gz | tar -xz
sudo install polaris /usr/local/bin/

# Run on all charts
bash scripts/run-polaris.sh

# Run on one chart
bash scripts/run-polaris.sh charts/loopzero-api

# Strict mode (CI)
bash scripts/run-polaris.sh --severity=danger
```

### CI

`.github/workflows/helm-charts.yml` runs Polaris baseline-first:

```yaml
- name: Polaris audit (baseline)
  continue-on-error: true   # warnings only during baseline phase
  run: bash scripts/run-polaris.sh --severity=warning
```

After Phase 3.3 GREEN + 1 month of clean baseline runs, promote to `--severity=danger` and remove `continue-on-error`.

## Iteration 1 baseline expectation

Charts in scope: 14 (loopzero-common exempt as library).

Expected initial counts (from `audit-security-context.sh` Phase 3.2):

- All loopzero-* charts have `runAsNonRoot: true`, `readOnlyRootFilesystem: true`, `drop: [ALL]`, `allowPrivilegeEscalation: false`.
- All loopzero-* charts have liveness + readiness probes (validated via helm-unittest).
- All loopzero-* charts have resource requests/limits (validated via helm lint --strict + Bitnami pattern).

So baseline should pass at `severity=warning` for all 14 charts, with possible warnings around:

- `cpuLimitsMissing` (ignored intentionally for KEDA scale-to-zero)
- `priorityClassNotSet` (set selectively)
- minor host* warnings on Traefik (LB hostPort)

If baseline shows `danger`-level findings on loopzero-* charts, that's a regression from Phase 3 work — investigate.

## Baseline run output

> Re-run `bash scripts/run-polaris.sh > polaris-baseline-$(date -u +%Y-%m-%d).log 2>&1` and append the summary here when first CI run completes.

```
Pending first CI run after this commit.
```
