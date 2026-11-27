// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// L4 — 500 tenants sustained load (single cell limit).
//
// scale-50k-plan Phase 2.2: push one cell to its validated limit.
// This test ramps to 10 deploys/min sustained for 2 hours, covering
// 500 distinct tenants with re-deploy rotation.
//
// Key differences from L3:
//   - 5x more tenants = 5x more ArgoCD apps, Gitea repos, PG rows
//   - Tests admission control (per-tenant limit of 3 concurrent builds)
//   - Tests PG pool saturation (500 tenants polling builds concurrently)
//   - Tests ArgoCD sync latency at 500+ apps
//
// Thresholds: ≥99% success, p99 deploy < 20min, admission rejects < 5%.

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

const TENANT_COUNT = 500;

export const options = {
  scenarios: {
    l4_sustained: {
      executor: 'ramping-arrival-rate',
      startRate: 1,
      timeUnit: '1m',
      preAllocatedVUs: 100,
      maxVUs: 600,
      stages: [
        { target: 2,   duration: '5m'  },  // warm up
        { target: 5,   duration: '10m' },  // ramp
        { target: 10,  duration: '90m' },  // sustained (bulk of the test)
        { target: 5,   duration: '10m' },  // cool down
        { target: 0,   duration: '5m'  },  // drain
      ],
      gracefulStop: '30m',
    },
  },
  thresholds: Object.assign(thresholdsFor('l4'), {
    // L4-specific: admission rejects should be < 5% of total submissions
    'loop_runtime_admission_reject_total': ['count<25'],
  }),
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

export function setup() {
  if (!healthCheck()) {
    throw new Error('API health check failed — aborting L4 test');
  }
  return { bundle: loadBundleBytes() };
}

export default function (data) {
  const iterID = exec.scenario.iterationInTest;
  const tenantIdx = iterID % TENANT_COUNT;
  const buildNum = Math.floor(iterID / TENANT_COUNT);

  const tenantID = `k6-l4-tn-${tenantIdx}`;
  const userID = `k6-l4-user-${tenantIdx}`;
  const email = `k6-l4-${tenantIdx}@loop-runtime.test`;
  const projectName = `k6-l4-app-${tenantIdx}`;

  let commitSHA, buildID;

  // Only create project on first visit.
  if (buildNum === 0) {
    group('create project', function () {
      createProject(tenantID, projectName, userID, email);
    });
    sleep(5); // Brief reconcile window
  }

  group('upload source', function () {
    const r = uploadSource(tenantID, projectName, data.bundle, userID, email);
    if (r.ok) commitSHA = r.commitSHA;
  });
  if (!commitSHA) return;

  group('submit build', function () {
    const idempKey = `k6-l4-${tenantIdx}-${buildNum}-${Date.now()}`;
    const r = submitBuild(tenantID, projectName, commitSHA, idempKey, userID, email);
    if (r.ok) buildID = r.buildID;
    // Log admission rejects for visibility.
    if (r.status === 429) {
      console.warn(`VU iter-${iterID}: admission rejected (Retry-After: ${r.retryAfter})`);
    }
  });
  if (!buildID) return;

  let buildOK = false;
  group('poll until done', function () {
    const r = pollBuildUntilDone(tenantID, buildID, userID, email, 25 * 60 * 1000, 30 * 1000);
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
  const admRej = (m.loop_runtime_admission_reject_total || {}).values || {};
  const lockConf = (m.loop_runtime_lock_conflict_total || {}).values || {};

  const text = `
═════════════════════════════════════════════════════════
  L4 — 500 tenants sustained (single cell limit)
═════════════════════════════════════════════════════════
  Success rate:        ${(succeeded.rate * 100 || 0).toFixed(2)}%   (target ≥ 99%)
  Apps verified live:  ${(verified.rate * 100 || 0).toFixed(2)}%
  Total builds:        ${succeeded.count || 0}
  Deploy p50:          ${Math.round((deploy.med || 0) / 1000)}s
  Deploy p95:          ${Math.round((deploy['p(95)'] || 0) / 1000)}s
  Deploy p99:          ${Math.round((deploy['p(99)'] || 0) / 1000)}s   (target < 20 min)
  App verify p95:      ${Math.round((verify['p(95)'] || 0) / 1000)}s
  Admission rejects:   ${admRej.count || 0}
  Lock conflicts:      ${lockConf.count || 0}
═════════════════════════════════════════════════════════
`;
  return {
    stdout: text,
    '/tmp/k6-l4-summary.json': JSON.stringify(data, null, 2),
  };
}
