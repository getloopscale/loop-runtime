// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// =============================================================================
// CONTROL-PLANE BIPARTITE SoT — INTENTIONAL EMPTY PROGRAM
// =============================================================================
//
// This file was a STUB declaring 10 TODOs for control-plane service
// installation (installLoopZeroAPI, installCloudService, installTenantController,
// installOryKratos, installOryHydra, etc.).
//
// MEET-2026-05-12 D3 ratified the BIPARTITE SOURCE OF TRUTH design:
//
//   L1 (Pulumi)   — Cloud primitives ONLY: DOKS, networking, DO Spaces, DOCR,
//                   CF Tunnel/Worker/KV/Zone, managed PG16, VPC, IAM baseline.
//   L2 (Helmfile) — Platform Helm releases (loopzero-api, loopzero-cloud, Tenant
//                   Controller, Ory Kratos, Ory Hydra, ArgoCD, Gitea, MinIO,
//                   etc.) — owned by `infra/helmfile/`.
//   L3 (ArgoCD)   — Tenant workloads from Gitea.
//
// Control-plane services therefore live in L2 (Helmfile), NOT in this project.
//
// The 10 TODO functions previously declared here would have unified L2 into
// Pulumi — that approach was explicitly REJECTED by SRE absolute veto + GitOps
// + Principal Architect at MEET-2026-05-12 (D3 chosen option A: bipartite
// documented, NOT option B unify Pulumi).
//
// The 8 sibling implementation files (cp-api-service.ts, cp-cloud-service.ts,
// cp-ingress.ts, cp-network.ts, cp-ory-hydra.ts, cp-ory-kratos.ts,
// cp-tenant-controller.ts, mgmt-control-plane-services.ts) were ORPHANED
// implementations of the rejected unified-Pulumi path and were deleted in
// T1.8 of plan v1.3 (1,538 LOC removed). They had zero external consumers.
//
// This project directory is RETAINED ONLY because the following scripts
// reference it for orchestration:
//   - infra/scripts/pulumi-archive.sh        (archive/backup)
//   - infra/scripts/pulumi-orphan-rm.sh      (orphan resource cleanup)
//   - infra/scripts/provision-homolog.sh     (homolog stack provisioning)
//
// DO NOT implement the deleted TODOs here. The decision to NOT manage
// control-plane services via Pulumi is binding per MEET-2026-05-12 D3.
//
// To extend control-plane services, edit `infra/helmfile/mgmt.helmfile.yaml.gotmpl`
// instead. To unwind this decision, file a new T3 meeting per pre-release
// governance and supersede ADR-2026-05-12-launch-readiness D3.
//
// References:
//   - .claude/knowledge-base/decisions/2026-05-12-launch-readiness.md § D3
//   - .claude/knowledge-base/plans/2026-05-12-launch-readiness-waves-plan.md § T1.8
//   - .claude/knowledge-base/architecture/infrastructure/system-context.md
//   - .claude/rules/pre-release-governance.md § Plan Amendment Policy
//
// =============================================================================

import * as pulumi from "@pulumi/pulumi";

// Empty Pulumi program — intentional per bipartite SoT.
// Stack outputs preserved for backward-compat with consumers that may read them.
export const controlPlane = pulumi.output({
  sot: "L2-helmfile",
  reason: "MEET-2026-05-12 D3 — bipartite SoT",
  rejected_path: "L2-pulumi-unified (option B)",
  binding_until: "next T3 meeting supersedes D3",
});
