// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

/**
 * `loopzero admin release inspect <id>` — release attestation chain diagnostic.
 *
 * Plan: docs/plans/2026-05-28-meet-001-engineering-closure-plan.md (T4.4).
 * CR-10 of MEET-2026-05-28-001: on-call needs `loopzero admin release
 * inspect <release-id>` showing which signing path was taken without
 * source access.
 *
 * Admin-only. Requires LOOPZERO_SERVICE_AUTH env var. Wraps
 * GET /api/v1/admin/releases/{id}/inspect.
 *
 * Exit codes (CLAUDE.md Rule 4):
 *   0 — success
 *   1 — usage error (missing id) OR release not found
 *   4 — auth error (LOOPZERO_SERVICE_AUTH missing or rejected)
 *
 * Rule 3 (no internal terms): the rendered output uses only product
 * language ("signing path", "attestation", "audit log", "build"). The
 * API enforces the same contract for the underlying JSON payload.
 */

export interface AdminReleaseInspectResponse {
  release_id: string;
  build_id: string;
  tenant_id: string;
  project_id: string;
  environment: string;
  status: string;
  correlation_id: string;
  workflow_id?: string | null;
  image_digest?: string | null;
  image_signature?: string | null;
  image_signed_at?: string | null;
  image_attestation_rekor_url?: string | null;
  signing_path: 'keyless' | 'keyed' | 'unsigned' | 'unknown';
  attestation_path: 'present' | 'absent' | 'unknown';
  audit_log_entries: Array<{
    action: string;
    actor: string;
    resource_id: string;
    correlation_id?: string;
    metadata?: Record<string, unknown>;
    created_at: string;
  }>;
}

export interface AdminReleaseInspectDeps {
  getServiceAuth: () => string | null;
  getRelease: (releaseID: string) => Promise<{
    status: number;
    body: AdminReleaseInspectResponse | { error: string; code: string };
  }>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface AdminReleaseInspectOptions {
  releaseId: string;
  json?: boolean;
}

export async function runAdminReleaseInspect(
  opts: AdminReleaseInspectOptions,
  deps: AdminReleaseInspectDeps,
): Promise<number> {
  const emitJsonError = (
    message: string,
    code: string,
    exitCode: number,
  ): number => {
    if (opts.json) {
      deps.writeTo(JSON.stringify({ error: message, exit_code: exitCode, code }));
    } else {
      deps.writeError(message);
    }
    return exitCode;
  };

  if (!opts.releaseId || opts.releaseId.trim() === '') {
    return emitJsonError(
      'Release id is required. Usage: loopzero admin release inspect <id>',
      'MISSING_RELEASE_ID',
      1,
    );
  }

  const auth = deps.getServiceAuth();
  if (!auth) {
    return emitJsonError(
      'LOOPZERO_SERVICE_AUTH env var required for admin commands.',
      'AUTH_REQUIRED',
      4,
    );
  }

  const result = await deps.getRelease(opts.releaseId.trim());

  if (result.status === 403) {
    return emitJsonError(
      'Admin auth rejected. Check LOOPZERO_SERVICE_AUTH value.',
      'AUTH_REJECTED',
      4,
    );
  }
  if (result.status === 404) {
    return emitJsonError(
      `Release "${opts.releaseId}" not found.`,
      'RELEASE_NOT_FOUND',
      1,
    );
  }
  if (result.status !== 200) {
    return emitJsonError(
      `Release inspect failed with status ${result.status}.`,
      'INSPECT_FAILED',
      1,
    );
  }

  const body = result.body as AdminReleaseInspectResponse;

  if (opts.json) {
    deps.writeTo(JSON.stringify(body));
    return 0;
  }

  // Plain-text rendering. Order: identity → signing chain → audit trail.
  // Avoid internal terms (Rule 3): no "ArgoCD", no "tenant_id" (use
  // "Team" since admin-scope renders the canonical name).
  const lines: string[] = [];
  lines.push(`Release        ${body.release_id}`);
  lines.push(`Build          ${body.build_id}`);
  lines.push(`Team           ${body.tenant_id}`);
  lines.push(`Project        ${body.project_id}`);
  lines.push(`Environment    ${body.environment}`);
  lines.push(`Status         ${body.status}`);
  if (body.workflow_id) {
    lines.push(`Workflow       ${body.workflow_id}`);
  } else {
    lines.push(`Workflow       (not available; build artifact may have been pruned)`);
  }
  lines.push('');
  lines.push('--- Signing chain ---');
  lines.push(`Signing path   ${body.signing_path}`);
  lines.push(`Image digest   ${body.image_digest ?? '(none)'}`);
  lines.push(`Signature      ${body.image_signature ?? '(none)'}`);
  lines.push(`Signed at      ${body.image_signed_at ?? '(none)'}`);
  lines.push('');
  lines.push('--- Attestation chain ---');
  lines.push(`Attestation    ${body.attestation_path}`);
  lines.push(`Rekor entry    ${body.image_attestation_rekor_url ?? '(none)'}`);
  lines.push('');
  if (body.audit_log_entries.length > 0) {
    lines.push(`--- Audit log (${body.audit_log_entries.length} entries) ---`);
    for (const entry of body.audit_log_entries) {
      lines.push(`  ${entry.created_at}  ${entry.action}  by ${entry.actor}`);
    }
  } else {
    lines.push('--- Audit log ---');
    lines.push('  (no entries for this release)');
  }

  deps.writeTo(lines.join('\n'));
  return 0;
}
