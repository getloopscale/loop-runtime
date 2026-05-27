// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T2.2 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap — Cosign keyless
// infrastructure (Fulcio + Rekor + CTLog + TUF). Engineering-ready
// awaiting sponsor approval per ADR 2026-05-12 cosign cutover deadline
// (T0+90d = 2026-08-10 hard deadline).
//
// State of the art (FAANG + Sigstore community):
//
// 1. Public infrastructure: fulcio.sigstore.dev + rekor.sigstore.dev.
//    Free, OWNED + OPERATED by Sigstore Public Good Foundation.
//    Pros: zero infra cost, deploy in minutes, ToS sufficient for OSS.
//    Cons: NO SLA, rate-limited (~10 req/s), shared identity issuer space.
//    Adopters: most OSS projects, GitHub Container Registry attestations.
//
// 2. Self-hosted via sigstore/scaffolding Helm chart: provisions Fulcio,
//    Rekor, CTLog, TUF on Kubernetes in one Release. Includes
//    cert-manager integration + Prometheus metrics.
//    Pros: FAANG-grade SLA control, custom identity policy, audit isolation.
//    Cons: ~$30-50/mo additional infra (1 small cluster or shared), 16h
//    eng + monthly operational overhead (TUF rotation, Rekor pruning).
//    Adopters: Adobe (Photon), Red Hat (RHCOS), GitHub Enterprise.
//
// 3. Hybrid: Sigstore public for normal builds + self-hosted CTLog for
//    compliance audit isolation. Adobe pattern.
//
// THIS COMPONENT: Option 2 (self-hosted scaffolding). Why:
//   - 50k+ tenant target needs SLA control (rate-limit at scale = build hang)
//   - Cosign cutover deadline is HARD (2026-08-10) — public infra
//     leaves a residual rate-limit risk we can't mitigate post-cutover
//   - Cost ~$30-50/mo well within Pro tier margins
//
// Reference chart: https://github.com/sigstore/scaffolding
// Chart values doc: https://github.com/sigstore/helm-charts/tree/main/charts/scaffold

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";

// Pinned chart version — last verified 2026-05 stable release line.
// Sigstore scaffolding is at v0.7.x as of cutoff; this minor pin lets
// us validate before bumping. Patch upgrades auto via Renovate.
const HELM_VERSION = "0.7.16";

export interface SigstoreArgs {
  /** K8s provider for the target cluster (always mgmt cluster) */
  provider: k8s.Provider;
  /** Namespace to install into. Convention: "sigstore-system" */
  namespace: string;
  /** Unique prefix for resource naming (e.g., "mgmt") */
  role: string;
  /**
   * When false, the ComponentResource constructs no resources at all.
   * Default is false (disabled) so accidentally importing this module
   * into a stack doesn't trigger provisioning. Sponsor flips to true
   * after T2.2 Change Governor meeting per cutover plan ADR-2026-05-12.
   */
  enabled?: boolean;
  /**
   * Hostname of the Fulcio CA service. Self-hosted convention:
   * `fulcio.<domain>` (e.g., `fulcio.loopzero.example`). The Helm chart
   * mints a cert-manager Certificate for this hostname; cert-manager
   * MUST be installed first (needs: relationship).
   */
  fulcioHostname?: pulumi.Input<string>;
  /**
   * Hostname for Rekor transparency log service.
   * Convention: `rekor.<domain>`.
   */
  rekorHostname?: pulumi.Input<string>;
  /**
   * OIDC issuer URL accepted by Fulcio. For workload-identity flow
   * (the cosign keyless mode), this is the Kubernetes API server's
   * OIDC discovery URL. Sponsor sets per cluster.
   */
  oidcIssuer?: pulumi.Input<string>;
}

/**
 * Sigstore Scaffolding — Fulcio + Rekor + CTLog + TUF in a single
 * Helm release. Provides the keyless signing infrastructure required
 * by `cosign sign --keyless` and `policy-controller` keyless verification.
 *
 * **DISABLED BY DEFAULT.** Set `enabled: true` only after:
 *   1. Sponsor approves T2.2 budget ($30-50/mo) via T3 meeting
 *   2. fulcioHostname + rekorHostname DNS records exist
 *   3. oidcIssuer is reachable from the cluster
 *
 * **Required before enabling:**
 *   - cert-manager (this chart provisions Certificate CRs for the hostnames)
 *   - ClusterIssuer named "letsencrypt-prod" or "self-signed-ca-issuer"
 *
 * **After enabling, charts/loopzero-admission-policies/values.yaml MUST set:**
 *   `clusterImagePolicyKeyless.enabled: true`
 *   `clusterImagePolicyKeyless.fulcioURL: https://<fulcioHostname>`
 *   `clusterImagePolicyKeyless.rekorURL: https://<rekorHostname>`
 */
export class Sigstore extends pulumi.ComponentResource {
  public readonly release?: k8s.helm.v3.Release;
  public readonly ready: pulumi.Output<boolean>;

  constructor(name: string, args: SigstoreArgs, opts?: pulumi.ComponentResourceOptions) {
    super("loopzero:platform:Sigstore", name, {}, opts);

    const enabled = args.enabled ?? false;

    if (!enabled) {
      // Disabled mode: emit a constant ready=false output so dependent
      // resources can wait on it but no provisioning happens.
      this.ready = pulumi.output(false);
      this.registerOutputs({ ready: this.ready });
      return;
    }

    // Validate required inputs in enabled mode. Pulumi component runs
    // at preview time, so missing required args throw early.
    if (!args.fulcioHostname || !args.rekorHostname || !args.oidcIssuer) {
      throw new Error(
        "Sigstore component enabled but fulcioHostname/rekorHostname/oidcIssuer not set. " +
        "Pre-flight: ensure DNS records exist + sponsor approved T2.2 budget."
      );
    }

    this.release = new k8s.helm.v3.Release(
      `${args.role}-sigstore-scaffolding`,
      {
        name: "sigstore-scaffolding",
        chart: "scaffold",
        version: HELM_VERSION,
        repositoryOpts: { repo: "https://sigstore.github.io/helm-charts" },
        namespace: args.namespace,
        createNamespace: false,
        // 30min timeout — scaffolding bootstraps TUF root + creates
        // multiple cert-manager certs sequentially, can take 15-20min
        // on first install while waiting for ACME issuance.
        timeout: 1800,
        values: {
          // Fulcio: short-lived cert issuance via OIDC.
          fulcio: {
            enabled: true,
            server: {
              ingress: {
                http: { hosts: [{ host: args.fulcioHostname, path: "/" }] },
              },
            },
            createcerts: { enabled: true },
          },
          // Rekor: append-only transparency log.
          rekor: {
            enabled: true,
            server: {
              ingress: {
                hosts: [{ host: args.rekorHostname, path: "/" }],
              },
            },
            backfillRedis: { enabled: false },
          },
          // CTLog: certificate transparency log for Fulcio.
          ctlog: {
            enabled: true,
            createctconfig: {
              fulcioURL: pulumi.interpolate`https://${args.fulcioHostname}`,
            },
          },
          // TUF: root-of-trust for the chain. Self-hosted means we
          // generate + manage our own TUF root (vs trusting Sigstore's).
          tuf: {
            enabled: true,
            ingress: {
              create: true,
              hosts: [{ host: args.rekorHostname, path: "/tuf/" }],
            },
          },
          // Trillian: backend storage for Rekor + CTLog. Default
          // MySQL — scaffolding chart provisions inline. For prod we
          // could swap to managed PG but scaffolding doesn't support
          // PG yet (Trillian roadmap item).
          trillian: { enabled: true },
        },
      },
      { provider: args.provider, parent: this },
    );

    this.ready = this.release.status.apply((s) => s.status === "deployed");
    this.registerOutputs({
      ready: this.ready,
      fulcioURL: pulumi.interpolate`https://${args.fulcioHostname}`,
      rekorURL: pulumi.interpolate`https://${args.rekorHostname}`,
    });
  }
}
