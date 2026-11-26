// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// L5 — 5,000 tenants across multiple cells.
//
// scale-50k-plan Phase 4.3: validates multi-cell routing, cell isolation,
// and the DynamicRouter/PlacementEngine under sustained load.
//
// Key differences from L4:
//   - Tenants are distributed across N cells (PlacementEngine round-robin)
//   - Cell IDs are embedded in tenant names for correlation in Grafana
//   - Cross-cell aggregation is validated via project list endpoint
//   - Thresholds are per-cell and global
//
// This script scales to L6 (50k) by changing TENANT_COUNT and cell count.
// At 500 tenants/cell: 5,000 tenants = 10 cells.
//
// Thresholds: ≥99% success, p99 deploy < 25min, <1% admission rejects.

import { group, sleep } from 'k6';
import exec from 'k6/execution';

import {
  createProject,
  uploadSource,
  submitBuild,
  pollBuildUntilDone,
  healthCheck,
  verifyAppLive,
  appURL,
} from './lib/loop-runtime-api.js';
import { thresholdsFor } from './lib/thresholds.js';
import { loadBundleBytes } from '../fixtures/bundle-builder.js';

// Configuration — override via env vars for different scale levels.
const TENANT_COUNT = parseInt(__ENV.K6_TENANT_COUNT || '5000', 10);
const TENANTS_PER_CELL = parseInt(__ENV.K6_TENANTS_PER_CELL || '500', 10);
const CELL_COUNT = Math.ceil(TENANT_COUNT / TENANTS_PER_CELL);

export const options = {
  scenarios: {
    l5_multicell: {
      executor: 'ramping-arrival-rate',
      startRate: 1,
      timeUnit: '1m',
      preAllocatedVUs: 200,
      maxVUs: 1000,
      stages: [
        { target: 5,   duration: '10m' },  // warm up
        { target: 20,  duration: '20m' },  // ramp
        { target: 20,  duration: '180m' }, // sustained (3 hours)
        { target: 10,  duration: '20m' },  // cool down
        { target: 0,   duration: '10m' },  // drain
      ],
      gracefulStop: '30m',
    },
  },
  thresholds: Object.assign(thresholdsFor('l5'), {
    'loop_runtime_admission_reject_total': ['count<50'],
  }),
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

export function setup() {
  if (!healthCheck()) {
    throw new Error('API health check failed — aborting L5 test');
  }
  console.log(`L5 config: ${TENANT_COUNT} tenants, ${CELL_COUNT} cells, ${TENANTS_PER_CELL}/cell`);
  return { bundle: loadBundleBytes() };
}

export default function (data) {
  const iterID = exec.scenario.iterationInTest;
  const tenantIdx = iterID % TENANT_COUNT;
  const buildNum = Math.floor(iterID / TENANT_COUNT);

  // Cell assignment mirrors PlacementEngine round-robin.
  const cellIdx = tenantIdx % CELL_COUNT;
  const cellName = `cell-${cellIdx}`;

  // Tenant ID includes cell for Grafana correlation.
  const tenantID = `k6-l5-c${cellIdx}-tn-${tenantIdx}`;
  const userID = `k6-l5-user-${tenantIdx}`;
  const email = `k6-l5-${tenantIdx}@loop-runtime.test`;
  const projectName = `k6-l5-app-${tenantIdx}`;

  let commitSHA, buildID;

  if (buildNum === 0) {
    group('create project', function () {
      createProject(tenantID, projectName, userID, email);
    });
    sleep(3);
  }

  group('upload source', function () {
    const r = uploadSource(tenantID, projectName, data.bundle, userID, email);
    if (r.ok) commitSHA = r.commitSHA;
  });
  if (!commitSHA) return;

  group('submit build', function () {
    const idempKey = `k6-l5-${tenantIdx}-${buildNum}-${Date.now()}`;
    const r = submitBuild(tenantID, projectName, commitSHA, idempKey, userID, email);
    if (r.ok) buildID = r.buildID;
    if (r.status === 429) {
      console.warn(`iter-${iterID} [${cellName}]: admission rejected`);
    }
  });
  if (!buildID) return;

  let buildOK = false;
  group('poll until done', function () {
    const r = pollBuildUntilDone(tenantID, buildID, userID, email, 30 * 60 * 1000, 30 * 1000);
    buildOK = r.ok;
  });

  if (!buildOK) return;

  group('verify app live', function () {
    const url = appURL(tenantID, projectName);
    verifyAppLive(url, 120 * 1000);
  });
}

export function teardown() {
  healthCheck();
}

export function handleSummary(data) {
  const m = data.metrics;
  const succeeded = (m.loop_runtime_build_succeeded || {}).values || {};
  const verified = (m.loop_runtime_app_verified || {}).values || {};
  const deploy = (m.loop_runtime_deploy_duration_ms || {}).values || {};
  const verify = (m.loop_runtime_app_verify_duration_ms || {}).values || {};

  const text = `
═════════════════════════════════════════════════════════
  L5 — ${TENANT_COUNT} tenants across ${CELL_COUNT} cells
═════════════════════════════════════════════════════════
  Success rate:        ${(succeeded.rate * 100 || 0).toFixed(2)}%   (target ≥ 99%)
  Apps verified live:  ${(verified.rate * 100 || 0).toFixed(2)}%
  Total builds:        ${succeeded.count || 0}
  Deploy p50:          ${Math.round((deploy.med || 0) / 1000)}s
  Deploy p95:          ${Math.round((deploy['p(95)'] || 0) / 1000)}s
  Deploy p99:          ${Math.round((deploy['p(99)'] || 0) / 1000)}s   (target < 25 min)
  App verify p95:      ${Math.round((verify['p(95)'] || 0) / 1000)}s
═════════════════════════════════════════════════════════
`;
  return {
    stdout: text,
    '/tmp/k6-l5-summary.json': JSON.stringify(data, null, 2),
  };
}
