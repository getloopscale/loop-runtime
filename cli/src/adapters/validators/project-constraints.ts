// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import type { LoopZeroYaml, ValidatorResult } from './types';

/**
 * MEET-2026-03-29-016 project-level constraints: at most 1 server + 1
 * worker per project. Frontends are always static (zero pods) and don't
 * count toward the limit.
 */
export function validateProjectConstraints(config: LoopZeroYaml): ValidatorResult {
  const errors: ValidatorResult['errors'] = [];

  const serverApps = Object.entries(config.apps).filter(
    ([, a]) => (a.type ?? 'server') === 'server',
  );
  const workerApps = Object.entries(config.apps).filter(([, a]) => a.type === 'worker');

  if (serverApps.length > 1) {
    errors.push({
      field: 'apps',
      message: `Project has ${serverApps.length} API services (${serverApps.map(([n]) => n).join(', ')}) but only 1 is allowed. Split into separate projects or consolidate into a single API.`,
    });
  }
  if (workerApps.length > 1) {
    errors.push({
      field: 'apps',
      message: `Project has ${workerApps.length} workers (${workerApps.map(([n]) => n).join(', ')}) but only 1 is allowed. Combine background tasks into a single worker.`,
    });
  }

  return { errors, warnings: [] };
}
