// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.1 T1.1 (loopzero-cli-vercel-like-ux): discovery of `.loopzero/services.json`
// for the deploy upload payload. Replaces the misdirected `findLoopZeroYaml`
// usage at the upload boundary (which was silently shipping YAML bytes inside
// the `services_json` form field per Plan B v3.1 cutover gap).
//
// EC-4 absorbed: --services-json <path> override for users with custom
// `distDir` in `loopzero.config.ts` (e.g. distDir: 'out' → out/services.json).

import * as fs from 'fs';
import * as path from 'path';

const SERVICES_JSON_RELATIVE = path.join('.loopzero', 'services.json');

/**
 * Thrown when `--services-json <path>` was supplied but the target file does
 * not exist. Distinct from "no services.json discovered" because the user
 * explicitly pointed at a path that must resolve.
 */
export class ServicesJsonOverrideError extends Error {
  public readonly code = 'SERVICES_JSON_OVERRIDE_INVALID';
  constructor(suppliedPath: string) {
    super(
      `--services-json ${suppliedPath}: file not found. ` +
        'Re-run `loopzerokit build --target loopzero-cloud` or fix the path.',
    );
    this.name = 'ServicesJsonOverrideError';
  }
}

/**
 * Walk up from `cwd` to `gitRoot` looking for `.loopzero/services.json`. Returns
 * the path to the first match or `null` when none found before the boundary.
 *
 * EC-4 absorbed: when `overridePath` is supplied, the walk-up is bypassed and
 * the supplied path is validated via `existsSync`. Missing target throws
 * `ServicesJsonOverrideError` (callers convert to exit code 1).
 */
export function findServicesJson(
  cwd: string,
  gitRoot: string,
  overridePath?: string,
): { path: string } | null {
  if (overridePath !== undefined && overridePath !== '') {
    const resolved = path.isAbsolute(overridePath)
      ? overridePath
      : path.join(cwd, overridePath);
    if (!fs.existsSync(resolved)) {
      throw new ServicesJsonOverrideError(overridePath);
    }
    return { path: resolved };
  }

  let dir = cwd;
  while (true) {
    const candidate = path.join(dir, SERVICES_JSON_RELATIVE);
    if (fs.existsSync(candidate)) {
      return { path: candidate };
    }
    if (dir === gitRoot || dir === path.dirname(dir)) {
      return null;
    }
    dir = path.dirname(dir);
  }
}
