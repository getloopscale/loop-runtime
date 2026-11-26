// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// L3 — 100 tenants sustained load.
//
// This is where the cell capacity budget gets tested against reality.
// The cell-budget.md doc says 100 tenants/cell at L3 should work with
// ArgoCD at 128 apps and PG < 80% pool utilization.
//
// Scenario: ramp to 3 deploys/min sustained for 60 min = 180 total.
// Using 100 distinct tenant IDs with rotation (each tenant gets ~2
// builds over the test window to simulate re-deploys).
//
// Thresholds: ≥99.5% success (roadmap 08), p99 deploy < 15 min.

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
} from './lib/loopruntime-api.js';
import { thresholdsFor } from './lib/thresholds.js';
import { loadBundleBytes } from '../fixtures/bundle-builder.js';

const TENANT_COUNT = 100;

export const options = {
  scenarios: {
    l3_sustained: {
      executor: 'ramping-arrival-rate',
      startRate: 1,
      timeUnit: '1m',
      preAllocatedVUs: 40,
      maxVUs: 200,
      stages: [
        { target: 1, duration: '3m'  },  // warm up
        { target: 3, duration: '5m'  },  // ramp
        { target: 3, duration: '55m' },  // sustained
        { target: 0, duration: '5m'  },  // drain
      ],
      gracefulStop: '30m',
    },
  },
  thresholds: thresholdsFor('l3'),
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

export function setup() {
  if (!healthCheck()) {
    throw new Error('API health check failed — aborting L3 test');
  }
  return { bundle: loadBundleBytes() };
}

export default function (data) {
  // Round-robin across TENANT_COUNT tenants. Each iteration picks the
  // next tenant in the ring so load spreads evenly (no hot-spotting
  // one tenant — that would hit the per-tenant admission quota).
  const iterID = exec.scenario.iterationInTest;
  const tenantIdx = iterID % TENANT_COUNT;
  const buildNum = Math.floor(iterID / TENANT_COUNT);

  const tenantID = `k6-l3-tn-${tenantIdx}`;
  const userID = `k6-l3-user-${tenantIdx}`;
  const email = `k6-l3-${tenantIdx}@loopruntime.test`;
  const projectName = `k6-l3-app-${tenantIdx}`;

  let commitSHA, buildID;

  // Only create the project on the first visit to this tenant.
  // Subsequent visits skip creation and go straight to deploy.
  if (buildNum === 0) {
    group('create project', function () {
      createProject(tenantID, projectName, userID, email);
    });
    sleep(8);
  }

  group('upload source', function () {
    const r = uploadSource(tenantID, projectName, data.bundle, userID, email);
    if (r.ok) commitSHA = r.commitSHA;
  });
  if (!commitSHA) return;

  group('submit build', function () {
    const idempKey = `k6-l3-${tenantIdx}-${buildNum}-${Date.now()}`;
    const r = submitBuild(tenantID, projectName, commitSHA, idempKey, userID, email);
    if (r.ok) buildID = r.buildID;
  });
  if (!buildID) return;

  let buildOK = false;
  group('poll until done', function () {
    const r = pollBuildUntilDone(tenantID, buildID, userID, email, 20 * 60 * 1000, 30 * 1000);
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
  const succeeded = (m.loopruntime_build_succeeded || {}).values || {};
  const verified = (m.loopruntime_app_verified || {}).values || {};
  const deploy = (m.loopruntime_deploy_duration_ms || {}).values || {};
  const verify = (m.loopruntime_app_verify_duration_ms || {}).values || {};
  const text = `
═════════════════════════════════════════════════════════
  L3 — 100 tenants sustained
═════════════════════════════════════════════════════════
  Success rate:        ${(succeeded.rate * 100 || 0).toFixed(2)}%   (target ≥ 99.5%)
  Apps verified live:  ${(verified.rate * 100 || 0).toFixed(2)}%   (target ≥ 99.5%)
  Total builds:        ${succeeded.count || 0}
  Deploy p50:          ${Math.round((deploy.med || 0) / 1000)}s
  Deploy p95:          ${Math.round((deploy['p(95)'] || 0) / 1000)}s
  Deploy p99:          ${Math.round((deploy['p(99)'] || 0) / 1000)}s   (target < 15 min)
  App verify p95:      ${Math.round((verify['p(95)'] || 0) / 1000)}s
═════════════════════════════════════════════════════════
`;
  return {
    stdout: text,
    '/tmp/k6-l3-summary.json': JSON.stringify(data, null, 2),
  };
}
