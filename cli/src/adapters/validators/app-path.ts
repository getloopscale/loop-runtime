// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as fs from 'fs';
import * as path from 'path';
import type { AppName, AppSpec, ValidatorResult } from './types';

export interface PathValidationOutcome extends ValidatorResult {
  /**
   * When the path escapes the project root, downstream validators should
   * be skipped for this app (matches the `continue` behavior in the
   * pre-T2.1 monolithic implementation).
   */
  shouldSkipDownstream: boolean;
}

/**
 * SEC-10: Reject path traversal (defense-in-depth — the server has SEC-3
 * mirror). Then verify path existence (skipped when skipFs=true).
 */
export function validateAppPath(
  name: AppName,
  app: AppSpec,
  projectRoot: string,
  skipFs: boolean,
): PathValidationOutcome {
  const errors: ValidatorResult['errors'] = [];

  const appPath = path.resolve(projectRoot, app.path);
  if (!appPath.startsWith(path.resolve(projectRoot))) {
    errors.push({
      field: `apps.${name}.path`,
      message: `App '${name}': path '${app.path}' escapes project root. Use a relative path within the project.`,
    });
    return { errors, warnings: [], shouldSkipDownstream: true };
  }

  if (!skipFs && !fs.existsSync(appPath)) {
    errors.push({
      field: `apps.${name}.path`,
      message: `App '${name}': path '${app.path}' does not exist`,
    });
  }

  return { errors, warnings: [], shouldSkipDownstream: false };
}
