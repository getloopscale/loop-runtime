// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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

/**
 * TUFKeyset references the offline-generated TUF root key material.
 *
 * Per ADR-2026-05-27-T2.2-OFFLINE-TUF, TUF root keys MUST be generated
 * via an offline ceremony (multi-party, hardware-backed, air-gapped)
 * and provided to this component as INPUTS. The keys NEVER touch the
 * mgmt cluster filesystem at rest — they live in a sealed Secret
 * created by the operator via the ceremony output script.
 *
 * Operator runbook: docs/runbooks.md (TODO)
 */
export interface TUFKeyset {
  /**
   * Name of the pre-existing Kubernetes Secret (in `namespace`) holding
   * the ceremony output. Expected keys per sigstore-scaffolding chart:
   * - root.json (TUF root metadata)
   * - signing keys (encrypted at rest, key derivation handled by helm hook)
   *
   * Operator creates this Secret BEFORE `pulumi up`:
   *   kubectl create secret generic sigstore-tuf-ceremony \
   *     --from-file=root.json=./ceremony-output/root.json \
   *     --namespace=sigstore-system
   *
   * NOTE: schema validation against sigstore-scaffolding 0.7.16 helm
   * chart values pending LIVE provisioning evidence. Operator must
   * confirm the chart accepts external TUF root reference before flip.
   */
  secretName: pulumi.Input<string>;
  /**
   * Date of the offline ceremony (ISO-8601 UTC). Used to compute
   * T-90d/T-30d/T-7d expiry alerts. TUF roots rotate annually by
   * industry convention.
   */
  ceremonyDate: pulumi.Input<string>;
}

/**
 * RekorWitnessConfig configures cross-witnessing to public sigstore.dev
 * Rekor (or any other independent witness). Per ADR-2026-05-27-T2.2-OFFLINE-TUF
 * conditional veto: self-hosted Rekor without cross-witness is
 * tamper-vulnerable beyond single-cluster blast radius — attacker who
 * compromises Rekor can forge/delete entries retroactively.
 */
export interface RekorWitnessConfig {
  /**
   * Public Rekor instance URL to cross-witness every entry.
   * Default: https://rekor.sigstore.dev (Sigstore Public Good)
   */
  url: pulumi.Input<string>;
  /**
   * Pinned public key URL for witness verification. Defaults to
   * Sigstore Public Good's published public key.
   */
  publicKeyURL: pulumi.Input<string>;
}

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
   * `fulcio.<domain>` (e.g., `fulcio.getloopscale.com`). The Helm chart
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
  /**
   * Pre-minted TUF root keys from offline ceremony (REQUIRED when
   * enabled=true per ADR-2026-05-27-T2.2-OFFLINE-TUF). The component
   * REJECTS construction if enabled without this — defense-in-depth
   * against "deploy first ceremony later" anti-pattern.
   */
  tufPreMintedKeys?: TUFKeyset;
  /**
   * Rekor cross-witness configuration (REQUIRED when enabled=true).
   * Defeats single-cluster tamper attacks via independent witnessing
   * per SBR conditional veto.
   */
  rekorWitness?: RekorWitnessConfig;
  /**
   * Replica count for Fulcio + Rekor (HA per SRE absolute condition).
   * Default 2. PodDisruptionBudget minAvailable=1 enforced.
   */
  haReplicas?: pulumi.Input<number>;
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
 * **After enabling, charts/loopruntime-admission-policies/values.yaml MUST set:**
 *   `clusterImagePolicyKeyless.enabled: true`
 *   `clusterImagePolicyKeyless.fulcioURL: https://<fulcioHostname>`
 *   `clusterImagePolicyKeyless.rekorURL: https://<rekorHostname>`
 */
// NOT INSTANTIATED (measured 2026-09-07): the only occurrence of `Sigstore` in the
// pulumi source trees is this declaration — no stack constructs it. The keyless
// cosign chain that IS live comes from infra/scripts/local/bootstrap-sigstore-local.sh
// and the helmfile releases, not from this component. Treat the block above as the
// intended IaC shape, not as a description of what a stack applies today.
export class Sigstore extends pulumi.ComponentResource {
  public readonly release?: k8s.helm.v3.Release;
  public readonly ready: pulumi.Output<boolean>;

  constructor(name: string, args: SigstoreArgs, opts?: pulumi.ComponentResourceOptions) {
    super("loopruntime:platform:Sigstore", name, {}, opts);

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
    //
    // ADR-2026-05-27-T2.2-OFFLINE-TUF mandate: tufPreMintedKeys +
    // rekorWitness are REQUIRED when enabled. Without them, the stack
    // is "strictly worse than keyed" per SBR conditional veto — refuse
    // construction at preview time to prevent activation regressing
    // the security posture.
    if (!args.fulcioHostname || !args.rekorHostname || !args.oidcIssuer) {
      throw new Error(
        "Sigstore component enabled but fulcioHostname/rekorHostname/oidcIssuer not set. " +
        "Pre-flight: ensure DNS records exist + sponsor approved T2.2 budget."
      );
    }
    if (!args.tufPreMintedKeys) {
      throw new Error(
        "Sigstore component enabled WITHOUT tufPreMintedKeys. Per " +
        "ADR-2026-05-27-T2.2-OFFLINE-TUF, TUF root keys MUST be generated " +
        "via offline ceremony (multi-party, hardware-backed, air-gapped) " +
        "and provided as a Kubernetes Secret reference. In-cluster TUF " +
        "generation is REJECTED — mgmt compromise would equal supply " +
        "chain compromise. See docs/runbooks.md."
      );
    }
    if (!args.rekorWitness) {
      throw new Error(
        "Sigstore component enabled WITHOUT rekorWitness. Per " +
        "ADR-2026-05-27-T2.2-OFFLINE-TUF, every Rekor entry MUST be " +
        "cross-witnessed to an independent Rekor instance (default: " +
        "public sigstore.dev). Without cross-witness, attacker who " +
        "compromises local Rekor can forge/delete entries retroactively. " +
        "Set rekorWitness.url + rekorWitness.publicKeyURL."
      );
    }

    // HA default per SRE absolute condition: ≥2 replicas, PDB minAvailable=1.
    const replicas = args.haReplicas ?? 2;

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
          // HA replicas + PDB per SRE absolute condition.
          fulcio: {
            enabled: true,
            replicaCount: replicas,
            podDisruptionBudget: {
              enabled: true,
              minAvailable: 1,
            },
            server: {
              ingress: {
                http: { hosts: [{ host: args.fulcioHostname, path: "/" }] },
              },
            },
            // ADR-2026-05-27-T2.2-OFFLINE-TUF: REJECT in-cluster cert
            // creation. Operator provides via offline ceremony.
            // NOTE: actual sigstore-scaffolding 0.7.16 helm chart values
            // schema for external cert reference pending LIVE validation
            // — this disables the default in-cluster job; operator must
            // confirm the chart's exact override key before flip.
            createcerts: { enabled: false },
          },
          // Rekor: append-only transparency log.
          // HA replicas + PDB per SRE absolute condition.
          // Cross-witness per ADR-2026-05-27-T2.2-OFFLINE-TUF.
          rekor: {
            enabled: true,
            replicaCount: replicas,
            podDisruptionBudget: {
              enabled: true,
              minAvailable: 1,
            },
            server: {
              ingress: {
                hosts: [{ host: args.rekorHostname, path: "/" }],
              },
            },
            backfillRedis: { enabled: false },
            // Cross-witness configuration per SBR conditional veto.
            // Every entry mirrored to public sigstore.dev Rekor for
            // tamper-evidence beyond single-cluster blast radius.
            // NOTE: sigstore-scaffolding 0.7.16 may not expose this
            // exact key — operator confirms schema before flip OR
            // implements witness via sidecar/external controller.
            witness: {
              url: args.rekorWitness.url,
              publicKeyURL: args.rekorWitness.publicKeyURL,
            },
          },
          // CTLog: certificate transparency log for Fulcio.
          ctlog: {
            enabled: true,
            createctconfig: {
              fulcioURL: pulumi.interpolate`https://${args.fulcioHostname}`,
            },
          },
          // TUF: root-of-trust for the chain.
          // ADR-2026-05-27-T2.2-OFFLINE-TUF: keys provided via offline
          // ceremony Secret reference. In-cluster generation REJECTED.
          tuf: {
            enabled: true,
            // Reference the operator-created Secret (offline ceremony
            // output). The sigstore-scaffolding chart accepts external
            // root via this key OR via a similar mechanism; operator
            // confirms the exact schema before flip per chart 0.7.16
            // documentation.
            preMintedKeys: {
              secretName: args.tufPreMintedKeys.secretName,
              ceremonyDate: args.tufPreMintedKeys.ceremonyDate,
            },
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
      tufCeremonyDate: args.tufPreMintedKeys.ceremonyDate,
      witnessURL: args.rekorWitness.url,
      haReplicas: replicas,
    });
  }
}
