# Promtail / pulumi-kubernetes upgrade tracking

**Status:** open
**Owner:** Infrastructure
**Roadmap:** Item 01 Correction M
**Origin:** `infra/tests/load/CORRECTIONS.txt` § M (2026-04-09)

---

## Context

Promtail runs on the build cluster to ship Argo Workflow build logs into
Loki. Before 2026-04-09 it was managed through Pulumi via the
`pulumi-kubernetes` provider. During an L1 load test the provider binary
crashed mid-operation with `grpc: client connection is closing` while
reconciling the Promtail Helm release, and kept crashing on retry.

To unblock the load test the Promtail release was removed from Pulumi
state management. Promtail itself continues to run as an unmanaged Helm
release on the build cluster — logs still flow into Loki — but the
Helm values and version are not enforced by Pulumi.

## Current state (2026-04-10)

| Thing | Status |
|---|---|
| Promtail runs on build cluster | ✅ yes (`kubectl -n monitoring get ds promtail`) |
| Promtail values enforced by Pulumi | ❌ no — removed from state |
| Build logs reaching Loki | ✅ yes (Loki canary + `promtail-8bmp7`/`promtail-tdql9` Running) |
| pulumi-kubernetes pinned version | 4.27.0 (4.28.0 had a separate crash) |

## Re-enablement criteria

Re-enable Promtail under Pulumi management once ALL of the following are true:

1. **A `pulumi-kubernetes` release ≥ 4.30 is published** that resolves
   the long-running-operation gRPC crash. Track the upstream bug report
   here before updating: https://github.com/pulumi/pulumi-kubernetes/issues
2. **Infra unit tests** cover applying a Helm release that takes >60 s
   without a provider crash, run against a k3d cluster in CI
   (`infra/tests/unit` or new e2e).
3. **Gradual rollout:** first re-add Promtail to the `dev` stack only,
   verify two consecutive `pulumi up` runs are clean, then promote to
   `staging` and `prod` stacks.

## Interim risk

Promtail's values (log level, scrape config, Loki push URL, TLS) are not
drift-controlled. If someone edits the Helm release by hand, there is no
Pulumi reconciliation to revert it. Mitigations:

- The release uses a pinned chart version and pinned image digest in its
  values ConfigMap — drift risk is limited to intentional manual edits.
- ArgoCD does not manage this release. It lives purely as a Helm chart.
- On cluster rebuild, the bootstrap Helmfile / Taskfile step re-installs
  Promtail fresh from its checked-in values, so cluster replacement
  recovers the intended state.

## Related corrections

- **N** (manual node pool changes break scoped kubeconfigs) — always
  change node pools via Pulumi.
- **O** (pipeline orphan-SA cleanup deletes Pulumi-managed SAs) — same
  class of problem: Pulumi state diverged from actual cluster state.

## Action items

- [ ] Subscribe to pulumi-kubernetes release notes; evaluate each minor
      for the long-running-operation fix.
- [ ] Write an infra e2e test that simulates a slow Helm release via a
      Helm post-install hook that sleeps 120 s, run under pulumi-kubernetes
      4.27.0 to reproduce the crash. Currently skipped until a candidate
      fix is available upstream.
- [ ] When the candidate fix lands: re-add Promtail to
      `infra/pulumi/src/components/bootstrap.ts` under a feature flag
      `enablePromtailPulumi: bool`, default `false`, flip to `true` on
      dev first.
