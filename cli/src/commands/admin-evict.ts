// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

/**
 * `loopzero admin tenants evict` — bulk tenant eviction (T-02).
 *
 * Plan: docs/plans/50k-immediate-focus.md
 *
 * Admin-only command. Requires LOOPZERO_SERVICE_AUTH env var (same as the
 * API uses internally). Wraps POST /api/v1/admin/tenants/evict.
 *
 * Usage:
 *   loopzero admin tenants evict --pattern 'tn-lt-%' --dry-run
 *   loopzero admin tenants evict --pattern 'tn-lt-l3-%' --yes
 *   loopzero admin tenants evict --ids tn-abc,tn-def --yes
 */

export interface AdminEvictInput {
  pattern?: string;
  ids?: string[];
  dry_run?: boolean;
}

export interface AdminEvictResponse {
  job_id: string;
  queued: number;
  matched?: string[];
  accepted: boolean;
}

export interface AdminEvictDeps {
  getServiceAuth: () => string | null;
  postEvict: (input: AdminEvictInput) => Promise<AdminEvictResponse>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
  prompt: (question: string) => Promise<string>;
}

export interface AdminEvictOptions {
  pattern?: string;
  ids?: string;
  dryRun?: boolean;
  yes?: boolean;
  json?: boolean;
}

/**
 * Returns exit code:
 *   0 — success or dry-run listed matches
 *   1 — usage error (missing flags)
 *   4 — auth error (LOOPZERO_SERVICE_AUTH missing)
 *   5 — partial failure (job accepted but some tenants failed)
 */
export async function runAdminEvict(
  opts: AdminEvictOptions,
  deps: AdminEvictDeps,
): Promise<number> {
  const auth = deps.getServiceAuth();
  if (!auth) {
    deps.writeError('LOOPZERO_SERVICE_AUTH env var required for admin commands.');
    return 4;
  }

  const pattern = opts.pattern?.trim();
  const ids = opts.ids?.split(',').map((s) => s.trim()).filter(Boolean) ?? [];

  if (!pattern && ids.length === 0) {
    deps.writeError('Either --pattern or --ids must be provided.');
    return 1;
  }

  if (pattern === '%' || pattern === 'tn-%') {
    deps.writeError("Pattern '%' or 'tn-%' is too broad — enumerate explicitly.");
    return 1;
  }

  // 1. Always dry-run first to show what would be evicted.
  const preview = await deps.postEvict({ pattern, ids, dry_run: true });
  if (opts.json) {
    deps.writeTo(JSON.stringify(preview, null, 2));
    return 0;
  }

  deps.writeTo(`Matched ${preview.queued} tenant(s):`);
  for (const id of preview.matched ?? []) {
    deps.writeTo(`  - ${id}`);
  }

  if (opts.dryRun) {
    return 0;
  }

  // 2. Confirm (unless --yes).
  if (!opts.yes) {
    const answer = await deps.prompt(`Evict ${preview.queued} tenant(s)? (yes/no) `);
    if (answer.trim().toLowerCase() !== 'yes') {
      deps.writeError('Aborted.');
      return 1;
    }
  }

  // 3. Submit eviction.
  const result = await deps.postEvict({ pattern, ids, dry_run: false });
  if (!result.accepted) {
    deps.writeError('Eviction request not accepted by API.');
    return 5;
  }

  deps.writeTo(`Eviction job queued: ${result.job_id} (${result.queued} tenants)`);
  return 0;
}
