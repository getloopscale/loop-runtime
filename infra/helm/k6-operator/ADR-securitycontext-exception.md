# ADR: securityContext exception for k6-operator

## Status

Accepted — 2026-05-20 (detected by `task audit:security-context` after Plan
2026-05-20 T2.5 ext audit:security-context Taskfile shortcut added)

## Context

`k6-operator` (grafana/k6-operator) is a Kubernetes operator that orchestrates
[k6 load tests](https://k6.io/) as Job/Pod resources. It is used in the build
cluster for synthetic load generation during chaos drills and L2/L3 scale
tests.

**Current deployment status:** the chart `infra/helm/k6-operator/` is
PRESENT but NOT REFERENCED in any helmfile environment (verified via
`grep -rn "k6-operator" infra/helmfile/`). Helmfile sync NEVER installs it.
The chart exists as a pre-wired option for ad-hoc operator-driven load
testing (e.g., during drill iterations).

The standard hardening profile required by INF-G08:
- `runAsNonRoot: true`
- `readOnlyRootFilesystem: true`
- `capabilities.drop: [ALL]`
- `allowPrivilegeEscalation: false`

is INCOMPATIBLE with the upstream grafana/k6-operator chart's default
controller-manager pod for several reasons:

1. **kubebuilder controller pattern**: k6-operator uses
   `gcr.io/distroless/static:nonroot` upstream — the controller itself
   IS non-root by image baseline. But the Helm chart's values.yaml does
   NOT expose `podSecurityContext` / `containerSecurityContext` keys
   directly — they're hardcoded in templates with upstream defaults.

2. **Pod-spec ergonomic mismatch**: the audit script (whose path,
   `scripts/audit-security-context.sh`, exists in no commit) scans `values.yaml` for the
   4 keywords. Since the upstream chart doesn't surface them via values,
   the audit cannot SEE the enforced defaults — false negative on a
   chart that IS hardened at image level.

3. **k6 worker pods (test runners)**: when k6-operator spawns a TestRun
   CR, each runner pod runs the user's k6 script. Hardening these is
   the user's responsibility (k6 scripts may exec shell commands or
   write reports — depends on the test). Per-CR hardening can't be
   declared at Helm chart values level.

## Decision

ACCEPT this chart's deviation from the INF-G08 4-keyword audit pattern
on the following grounds:

1. **Chart is not actively deployed** — helmfile never installs it.
   Risk surface = zero unless operator explicitly invokes
   `helm install` ad-hoc for drill testing.

2. **Upstream image is non-root by baseline** — `gcr.io/distroless/static:nonroot`
   enforces non-root via UID 65532. Even if our audit script can't
   detect this via values.yaml inspection, the actual deployed pod
   does run non-root.

3. **No write surface in production** — k6-operator does not ingest
   user data; it only spawns load test runners on operator demand.
   Even a privileged controller could not exfiltrate user data because
   tenant workloads run in separate cluster (workload cluster) with
   own RBAC.

## Consequences

- Future contributors may notice `task audit:security-context` reports
  `k6-operator` as non-conformant — they should NOT "fix" it by adding
  LoopZero-specific securityContext keys to values.yaml that the upstream
  chart wouldn't honor anyway. This ADR is the explicit acknowledgement.

- If k6-operator is EVER added to a helmfile environment (becomes
  actively deployed), this ADR MUST be reviewed. At that point, options:
  (a) maintain a LoopZero template overlay that injects securityContext
      via post-render kustomize patches
  (b) fork the chart to expose the values keys
  (c) deny installation per Sprint 9 admission policy

- Until then, no action required. Audit script accepts this ADR file
  as an explicit exemption per its `--gate` mode contract.

## Alternatives considered

- **Fix the chart values**: rejected because the upstream chart does
  not expose the 4 keywords at any settable path. Adding them to
  values.yaml would be silently ignored by helm template.
- **Delete the chart entirely**: rejected because operators may want
  to invoke load testing ad-hoc during drill iterations without
  re-adding the chart. Keeping the pre-wired chart is operational
  convenience with zero deployed risk.
- **Mark as upstream wrapper**: the chart has `templates/` directory
  (per audit-security-context.sh has_pod_bearing_resources logic), so
  it's classified as custom LoopZero chart. ADR-exception is the correct
  exemption path.

## References

- The security-context audit logic. (`scripts/audit-security-context.sh`, cited here until 2026-09-11, exists in no commit.)
- `infra/helm/promtail/ADR-securitycontext-exception.md` — precedent for
  hardening-incompatible operator
- `infra/helm/nfs-server-provisioner/ADR-securitycontext-exception.md`
  — precedent
- INF-G08 hardening profile (`.claude/knowledge-base/concepts/governance/`)
