// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.1 T2.1 (loopzero-cli-vercel-like-ux) — resolve the `loopzerokit` binary
// from local `node_modules/.bin` → system PATH → `LOOPZEROKIT_BIN` env var.
//
// v1.1 ADR D1 amendment (covers EC-1 + EC-6):
//   validateCandidate(path) = fs.statSync(path).isFile() && (mode & 0o111) !== 0.
//   Every candidate (walk-up hit, which/where fallback, env-var path) is
//   validated. Env-var path that fails validation throws immediately.

import { execSync } from 'child_process';
import * as fs from 'fs';
import * as path from 'path';

const WIN32 = process.platform === 'win32';
const BIN_NAME = WIN32 ? 'loopzerokit.cmd' : 'loopzerokit';

/**
 * Thrown when no usable `loopzerokit` binary can be resolved. Carries an install
 * hint covering both local and global flavors.
 */
export class LoopZerokitBinaryNotFoundError extends Error {
  public readonly code = 'LOOPZEROKIT_BINARY_NOT_FOUND';
  constructor(message?: string) {
    super(
      message ??
        'loopzerokit binary not found.\n\n' +
          'Install with:\n' +
          '  - In your project: npm install loopzerokit\n' +
          '  - Globally:        npm install -g loopzerokit\n\n' +
          'If installed but not detected, set LOOPZEROKIT_BIN to the absolute path.',
    );
    this.name = 'LoopZerokitBinaryNotFoundError';
  }
}

/**
 * Returns true when `candidate` is an existing regular file with at least one
 * executable bit set. False for missing paths, directories, and files without
 * any executable bit. EC-1 + EC-6 absorbed.
 */
export function validateCandidate(candidate: string): boolean {
  try {
    const stat = fs.statSync(candidate);
    if (!stat.isFile()) return false;
    return (stat.mode & 0o111) !== 0;
  } catch {
    return false;
  }
}

function walkUpForBin(cwd: string): string | null {
  let dir = cwd;
  while (true) {
    const candidate = path.join(dir, 'node_modules', '.bin', BIN_NAME);
    if (validateCandidate(candidate)) {
      return candidate;
    }
    const parent = path.dirname(dir);
    if (parent === dir) return null;
    dir = parent;
  }
}

function fallbackToWhich(): string | null {
  try {
    const cmd = WIN32 ? `where ${BIN_NAME}` : `which ${BIN_NAME}`;
    const output = execSync(cmd, { stdio: ['ignore', 'pipe', 'ignore'], encoding: 'utf-8' });
    const firstLine = output.split(/\r?\n/)[0]?.trim() ?? '';
    if (firstLine === '') return null;
    return validateCandidate(firstLine) ? firstLine : null;
  } catch {
    return null;
  }
}

/**
 * Resolve the path to the `loopzerokit` binary. Order (per ADR D1):
 *
 *   1. `LOOPZEROKIT_BIN` env-var override (validated, throws if invalid).
 *   2. Walk up from `cwd` looking for `node_modules/.bin/loopzerokit`.
 *   3. `which loopzerokit` (Unix) / `where loopzerokit` (Windows).
 *   4. Throw `LoopZerokitBinaryNotFoundError` with install hint.
 */
export function resolveLoopZerokitBin(cwd: string): string {
  const envOverride = process.env.LOOPZEROKIT_BIN;
  if (envOverride !== undefined && envOverride !== '') {
    if (!validateCandidate(envOverride)) {
      throw new LoopZerokitBinaryNotFoundError(
        `LOOPZEROKIT_BIN=${envOverride} is not an executable file; unset it or fix the path.`,
      );
    }
    return envOverride;
  }
  const walkUpHit = walkUpForBin(cwd);
  if (walkUpHit !== null) return walkUpHit;
  const whichHit = fallbackToWhich();
  if (whichHit !== null) return whichHit;
  throw new LoopZerokitBinaryNotFoundError();
}
