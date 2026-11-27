// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// T2.1 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md —
// Component contract tests for pulumi-platform services. Audit gap
// M05 #14 (MEDIUM): pulumi-platform had 1 source test for 4655 LOC.
// These tests lock chart sources + version pinning + cross-service
// dependsOn wiring without requiring a Pulumi stack to be provisioned
// (same source-grep approach as schema.test.ts).

import { describe, it, expect } from "vitest";
import * as fs from "fs";
import * as path from "path";

const SERVICES_DIR = path.resolve(__dirname, "../services");

function readSrc(file: string): string {
  return fs.readFileSync(path.join(SERVICES_DIR, file), "utf-8");
}

// ---------------------------------------------------------------------------
// cert-manager.ts
// ---------------------------------------------------------------------------

describe("cert-manager.ts — CertManager component", () => {
  const src = readSrc("cert-manager.ts");

  it("pins helm chart to a fixed version", () => {
    // Regression: floating version (e.g., "latest" or "^x.y.z") would
    // make pulumi up non-reproducible — same commit can produce different
    // K8s manifests across runs.
    expect(src).toMatch(/const HELM_VERSION = "v\d+\.\d+\.\d+"/);
  });

  it("uses official jetstack chart repository", () => {
    expect(src).toContain('"https://charts.jetstack.io"');
  });

  it("installs CRDs (required for ClusterIssuer)", () => {
    expect(src).toContain("installCRDs: true");
  });

  it("exports ComponentResource with k8s.helm.v3.Release + ready Output", () => {
    expect(src).toContain("extends pulumi.ComponentResource");
    expect(src).toContain("public readonly release: k8s.helm.v3.Release");
    expect(src).toContain("public readonly ready: pulumi.Output<boolean>");
  });

  it("declares role-prefixed resource name (mgmt vs runtime isolation)", () => {
    expect(src).toContain("${args.role}-cert-manager");
  });
});

// ---------------------------------------------------------------------------
// traefik.ts
// ---------------------------------------------------------------------------

describe("traefik.ts — Traefik component", () => {
  const src = readSrc("traefik.ts");

  it("pins helm chart to a fixed version", () => {
    expect(src).toMatch(/const HELM_VERSION = "\d+\.\d+\.\d+"/);
  });

  it("uses official traefik chart repository", () => {
    expect(src).toContain('"https://traefik.github.io/charts"');
  });

  it("declares cert-manager as required dependency (TLS chain)", () => {
    expect(src).toMatch(/certManager:\s*CertManagerLike/);
  });

  it("exports ingressEndpoint Output for downstream wiring", () => {
    expect(src).toContain("public readonly ingressEndpoint: pulumi.Output<string>");
  });

  it("uses role-prefixed Helm release name (multi-cluster isolation)", () => {
    expect(src).toContain("${args.role}-traefik");
  });
});

// ---------------------------------------------------------------------------
// argocd.ts
// ---------------------------------------------------------------------------

describe("argocd.ts — ArgoCD component", () => {
  const src = readSrc("argocd.ts");

  it("pins helm chart to a fixed version", () => {
    expect(src).toMatch(/HELM_VERSION = "[\d.]+"/);
  });

  it("uses official argo helm repository", () => {
    expect(src).toContain("argoproj.github.io/argo-helm");
  });

  it("exports ComponentResource", () => {
    expect(src).toContain("extends pulumi.ComponentResource");
  });
});

// ---------------------------------------------------------------------------
// gitea.ts
// ---------------------------------------------------------------------------

describe("gitea.ts — Gitea component", () => {
  const src = readSrc("gitea.ts");

  it("pins helm chart to a fixed version", () => {
    expect(src).toMatch(/HELM_VERSION = "[\d.]+"/);
  });

  it("references gitea charts repository", () => {
    // Either oci://ghcr or charts.gitea.com
    expect(src).toMatch(/gitea/i);
  });

  it("exports ComponentResource", () => {
    expect(src).toContain("extends pulumi.ComponentResource");
  });
});

// ---------------------------------------------------------------------------
// loki.ts
// ---------------------------------------------------------------------------

describe("loki.ts — Loki component", () => {
  const src = readSrc("loki.ts");

  it("pins helm chart to a fixed version", () => {
    expect(src).toMatch(/HELM_VERSION = "[\d.]+"/);
  });

  it("exports ComponentResource", () => {
    expect(src).toContain("extends pulumi.ComponentResource");
  });
});

// ---------------------------------------------------------------------------
// prometheus.ts
// ---------------------------------------------------------------------------

describe("prometheus.ts — Prometheus component", () => {
  const src = readSrc("prometheus.ts");

  it("pins helm chart to a fixed version", () => {
    expect(src).toMatch(/HELM_VERSION = "[\d.]+"/);
  });

  it("exports ComponentResource", () => {
    expect(src).toContain("extends pulumi.ComponentResource");
  });
});

// ---------------------------------------------------------------------------
// sigstore.ts — T2.2 cosign keyless infrastructure
// ---------------------------------------------------------------------------

describe("sigstore.ts — Sigstore component (T2.2 cosign keyless)", () => {
  const src = readSrc("sigstore.ts");

  it("pins helm chart to a fixed version (Renovate-friendly)", () => {
    expect(src).toMatch(/const HELM_VERSION = "\d+\.\d+\.\d+"/);
  });

  it("uses official sigstore helm chart repository", () => {
    expect(src).toContain('"https://sigstore.github.io/helm-charts"');
  });

  it("declares Fulcio + Rekor + CTLog + TUF in values (full keyless stack)", () => {
    expect(src).toContain("fulcio:");
    expect(src).toContain("rekor:");
    expect(src).toContain("ctlog:");
    expect(src).toContain("tuf:");
  });

  it("defaults to enabled=false (disabled-by-default safety)", () => {
    // The component runs only when sponsor explicitly enables. Accidental
    // import into a stack must NOT trigger provisioning ($30-50/mo cost).
    expect(src).toMatch(/enabled\?:\s*boolean/);
    expect(src).toMatch(/args\.enabled\s*\?\?\s*false/);
  });

  it("throws clear error if enabled without required hostnames", () => {
    // Pre-flight validation per CLAUDE.md fail-fast philosophy: missing
    // DNS records is a configuration error, not a runtime degradation.
    expect(src).toMatch(/throw new Error.*fulcioHostname.*rekorHostname.*oidcIssuer/s);
  });

  it("REJECTS enabled without tufPreMintedKeys (ADR-2026-05-27-T2.2-OFFLINE-TUF)", () => {
    // Per offline TUF ceremony mandate: refuse construction at preview
    // time if pre-minted keys not provided. In-cluster TUF generation
    // is "strictly worse than keyed" per SBR conditional veto.
    expect(src).toMatch(/if\s*\(\s*!args\.tufPreMintedKeys\s*\)/);
    expect(src).toContain("offline ceremony");
    expect(src).toContain("ADR-2026-05-27-T2.2-OFFLINE-TUF");
  });

  it("REJECTS enabled without rekorWitness (cross-witness mandatory)", () => {
    // Per SBR conditional veto: without cross-witness, attacker who
    // compromises local Rekor can forge/delete entries retroactively.
    expect(src).toMatch(/if\s*\(\s*!args\.rekorWitness\s*\)/);
    expect(src).toContain("cross-witnessed");
  });

  it("defaults HA to 2 replicas + PodDisruptionBudget minAvailable=1", () => {
    // Per SRE absolute condition on production-readiness.
    expect(src).toContain("haReplicas ?? 2");
    expect(src).toContain("minAvailable: 1");
    // Both Fulcio + Rekor must have PDB (separate components, separate failure modes).
    const pdbMatches = src.match(/podDisruptionBudget:\s*{\s*enabled:\s*true/g);
    expect(pdbMatches?.length, "must have PDB on BOTH Fulcio AND Rekor").toBeGreaterThanOrEqual(2);
  });

  it("disables in-cluster cert creation (createcerts: enabled: false)", () => {
    // Per ADR offline ceremony mandate: in-cluster cert creation
    // REJECTED. Operator provides via ceremony output Secret.
    expect(src).toMatch(/createcerts:\s*{\s*enabled:\s*false/);
  });

  it("exports TUF ceremony date + witness URL + HA replicas in outputs", () => {
    // Outputs surface operational metadata for observability + alerts
    // (T-90d/T-30d/T-7d TUF root expiry countdown per ADR).
    expect(src).toContain("tufCeremonyDate:");
    expect(src).toContain("witnessURL:");
    expect(src).toContain("haReplicas:");
  });

  it("uses role-prefix naming for multi-stack isolation", () => {
    // Sigstore on mgmt cluster only, but role-prefix still avoids collisions
    // if drill stack also imports this component.
    expect(src).toContain("${args.role}-sigstore-scaffolding");
  });

  it("documents ADR-2026-05-12 cutover deadline reference", () => {
    // The component header MUST cite the ADR so future readers find the
    // 2026-08-10 hard deadline context.
    expect(src).toContain("ADR 2026-05-12");
    expect(src).toContain("2026-08-10");
  });

  it("exports ComponentResource with optional release + ready Output", () => {
    expect(src).toContain("extends pulumi.ComponentResource");
    expect(src).toContain("public readonly release?: k8s.helm.v3.Release");
    expect(src).toContain("public readonly ready: pulumi.Output<boolean>");
  });

  it("sets long timeout for first-install TUF bootstrap (≥15min)", () => {
    // Scaffolding chart bootstraps TUF root + multiple cert-manager certs
    // sequentially; ACME issuance can hit 15-20min on first install.
    // Default 5min would fail every cold-start.
    expect(src).toMatch(/timeout:\s*\d+/);
    const timeoutMatch = src.match(/timeout:\s*(\d+)/);
    if (timeoutMatch) {
      const seconds = parseInt(timeoutMatch[1], 10);
      expect(seconds, "timeout must be ≥900s (15min) for first install").toBeGreaterThanOrEqual(900);
    }
  });
});

// ---------------------------------------------------------------------------
// Cross-service invariants
// ---------------------------------------------------------------------------

describe("services/ — cross-service invariants", () => {
  const allServices = fs.readdirSync(SERVICES_DIR).filter((f) => f.endsWith(".ts"));

  it("multi-cluster services (cert-manager, traefik) use the role-prefix pattern", () => {
    // Multi-cluster services install on BOTH mgmt + runtime (and drill cluster
    // for prod-identical stack). They MUST use ${args.role}-X for Helm release
    // naming to avoid collisions when 2+ stacks share the same K8s API server.
    // Single-instance services (argocd, gitea, grafana, loki, prometheus,
    // tempo) install only on mgmt cluster — no collision risk.
    const multiCluster = new Set(["cert-manager.ts", "traefik.ts"]);
    for (const f of allServices) {
      if (!multiCluster.has(f)) continue;
      const src = readSrc(f);
      const hasRolePrefix = /\$\{args\.role\}/.test(src);
      expect(hasRolePrefix, `${f} (multi-cluster) must use \${args.role}-X for resource naming`).toBe(true);
    }
  });

  it("every service file declares HELM_VERSION constant (no inline strings)", () => {
    for (const f of allServices) {
      const src = readSrc(f);
      // HELM_VERSION centralizes the version for grep auditing (e.g.,
      // Renovate PR-based bumps). Inline `version: "x.y.z"` would be drift.
      expect(src.includes("HELM_VERSION"), `${f} must declare HELM_VERSION`).toBe(true);
    }
  });

  it("every service file exports a ComponentResource subclass", () => {
    for (const f of allServices) {
      const src = readSrc(f);
      expect(src.includes("extends pulumi.ComponentResource"),
        `${f} must export a ComponentResource subclass`).toBe(true);
    }
  });
});
