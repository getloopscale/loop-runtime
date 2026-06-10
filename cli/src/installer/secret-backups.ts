// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-30-INFRA-PARITY / T3.2 / ADR D2 — auto-detect Secret backups.
//
// `loopzero platform install` Step 3.5 (post-infra, pre-helmfile) needs to know
// if the operator has a Secret backup available so destroy + install can be
// fully automated. Without this, every install on a recreated cluster needs
// a manual `loopzero platform secrets restore` step.
//
// We deliberately DO NOT auto-restore silently — a cluster created in CI
// might belong to a different tenant. The CLI exposes:
//   --restore-secrets <file>     explicitly use this file
//   --no-restore-secrets         skip restore even if backup exists
// In CI mode (CI=true), one of the two must be present (or no backup exists)
// or the install fails with exit 1. In interactive mode, the operator is
// prompted.

import * as fs from 'fs';
import * as path from 'path';

export interface BackupInfo {
  path: string;
  mtime: Date;
  sizeBytes: number;
}

function backupDir(): string {
  return path.join(process.env.HOME || '/root', '.loopzero', 'backups');
}

/**
 * Returns the most recent secrets-*.yaml file under ~/.loopzero/backups/, or
 * null if the dir doesn't exist or has no secret backups. HOME is read
 * lazily so tests that mutate process.env.HOME work correctly.
 *
 * Naming convention written by generate-secrets.sh:
 *   secrets-manual-{ts}.yaml      manual via `loopzero platform secrets backup`
 *   build-secrets-{env}-{ts}.yaml automatic via generate-build-secrets.sh
 *   runtime-secrets-{role}-{env}-{ts}.yaml
 *   mgmt-secrets-{ts}.yaml
 */
export function findLatestBackup(): BackupInfo | null {
  const dir = backupDir();
  if (!fs.existsSync(dir)) {
    return null;
  }
  const candidates = fs
    .readdirSync(dir)
    .filter((f) => /-secrets-.*\.ya?ml$|secrets-manual-.*\.ya?ml$/.test(f))
    .map((f) => {
      const fp = path.join(dir, f);
      const st = fs.statSync(fp);
      return { path: fp, mtime: st.mtime, sizeBytes: st.size } as BackupInfo;
    });
  if (candidates.length === 0) return null;
  candidates.sort((a, b) => b.mtime.getTime() - a.mtime.getTime());
  return candidates[0];
}

export interface RestoreDecisionContext {
  isCI: boolean;
  /** From CLI: --restore-secrets <file> */
  restoreFile?: string;
  /** From CLI: --no-restore-secrets */
  skip?: boolean;
}

export type RestoreDecision =
  | { action: 'restore'; file: string }
  | { action: 'skip'; reason: string }
  | { action: 'prompt'; latest: BackupInfo }
  | { action: 'fail'; error: string };

/**
 * Decides what to do given CLI flags + environment + backup detection.
 * Pure function — no I/O beyond findLatestBackup() above. Caller (platform
 * install) handles the actual restore (kubectl apply) or prompt.
 *
 * Decision matrix:
 *   --restore-secrets <file>  → restore
 *   --no-restore-secrets       → skip
 *   no flag, no backup         → skip (nothing to restore)
 *   no flag, backup, CI=true   → fail (must opt in or out explicitly)
 *   no flag, backup, !CI       → prompt
 */
export function decideRestore(ctx: RestoreDecisionContext): RestoreDecision {
  if (ctx.restoreFile) {
    if (!fs.existsSync(ctx.restoreFile)) {
      return { action: 'fail', error: `Backup file not found: ${ctx.restoreFile}` };
    }
    return { action: 'restore', file: ctx.restoreFile };
  }
  if (ctx.skip) {
    return { action: 'skip', reason: 'explicit --no-restore-secrets' };
  }
  const latest = findLatestBackup();
  if (!latest) {
    return { action: 'skip', reason: 'no backup found in ~/.loopzero/backups' };
  }
  if (ctx.isCI) {
    return {
      action: 'fail',
      error:
        `CI mode requires explicit decision when Secret backups exist. ` +
        `Found: ${latest.path}. Use --restore-secrets <file> or --no-restore-secrets.`,
    };
  }
  return { action: 'prompt', latest };
}
