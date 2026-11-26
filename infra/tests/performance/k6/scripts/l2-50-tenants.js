// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// L2 — 50 tenants sustained load.
//
// Scenario differs from L1: we use the ARRIVAL-RATE executor instead
// of per-vu-iterations. This is important because:
//
//   - per-vu-iterations schedules work based on response time (a slow
//     build holds its VU and blocks the next one — coordinated omission).
//   - constant-arrival-rate fires a fixed number of iterations per unit
//     time REGARDLESS of how long they take. This is the only way to
//     measure what the platform does at sustained N tenant/min.
//
// Target: 50 tenants deployed over 30 minutes = 1.66 tenant/min = 100/hour.
// This is the roadmap 04 L2 budget line (50 tenants cell capacity).
//
// Thresholds: 100% build success, p99 deploy < 10min, PG pool < 80%.

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

export const options = {
  scenarios: {
    l2_sustained: {
      executor: 'ramping-arrival-rate',
      // Target: ~50 deploys over 30 min = ~1.66 req/min ≈ 100/hour
      startRate: 1,
      timeUnit: '1m',
      preAllocatedVUs: 20,     // start pool
      maxVUs: 80,              // cap so runaway does not overload k6
      stages: [
        { target: 2,  duration: '5m'  }, // ramp up to 2/min
        { target: 2,  duration: '25m' }, // sustained
        { target: 0,  duration: '2m'  }, // drain
      ],
      gracefulStop: '30m', // let in-flight builds finish
    },
  },
  thresholds: thresholdsFor('l2'),
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

export function setup() {
  if (!healthCheck()) {
    throw new Error('API health check failed — aborting L2 test');
  }
  return { bundle: loadBundleBytes() };
}

export default function (data) {
  // Use a monotonically increasing tenant ID so each iteration uses
  // a different tenant. Shared across VUs via the iteration counter.
  const iterID = exec.scenario.iterationInTest;
  const tenantID = `k6-l2-tn-${iterID}`;
  const userID = `k6-l2-user-${iterID}`;
  const email = `k6-l2-${iterID}@loopruntime.test`;
  const projectName = `k6-l2-app-${iterID}`;

  let commitSHA, buildID;

  group('create project', function () {
    const r = createProject(tenantID, projectName, userID, email);
    if (!r.ok && r.status !== 402) {
      return;
    }
  });

  // Shorter reconcile wait at L2 — TenantController is warm.
  sleep(10);

  group('upload source', function () {
    const r = uploadSource(tenantID, projectName, data.bundle, userID, email);
    if (r.ok) commitSHA = r.commitSHA;
  });
  if (!commitSHA) return;

  group('submit build', function () {
    const idempKey = `k6-l2-${iterID}-${Date.now()}`;
    const r = submitBuild(tenantID, projectName, commitSHA, idempKey, userID, email);
    if (r.ok) buildID = r.buildID;
  });
  if (!buildID) return;

  let buildOK = false;
  group('poll until done', function () {
    const r = pollBuildUntilDone(tenantID, buildID, userID, email, 15 * 60 * 1000, 20 * 1000);
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
  return {
    stdout: buildReport('L2 — 50 tenants sustained', data),
    '/tmp/k6-l2-summary.json': JSON.stringify(data, null, 2),
  };
}

function buildReport(title, data) {
  const m = data.metrics;
  const succeeded = (m.loopruntime_build_succeeded || {}).values || {};
  const verified = (m.loopruntime_app_verified || {}).values || {};
  const deploy = (m.loopruntime_deploy_duration_ms || {}).values || {};
  const verify = (m.loopruntime_app_verify_duration_ms || {}).values || {};
  const admissionRej = (m.loopruntime_admission_reject_total || {}).values || {};
  return `
═════════════════════════════════════════════════════════
  ${title}
═════════════════════════════════════════════════════════
  Build success rate:  ${(succeeded.rate * 100 || 0).toFixed(2)}%
  Apps verified live:  ${(verified.rate * 100 || 0).toFixed(2)}%
  Total builds:        ${succeeded.count || 0}
  Deploy p50:          ${Math.round((deploy.med || 0) / 1000)}s
  Deploy p95:          ${Math.round((deploy['p(95)'] || 0) / 1000)}s
  Deploy p99:          ${Math.round((deploy['p(99)'] || 0) / 1000)}s
  App verify p95:      ${Math.round((verify['p(95)'] || 0) / 1000)}s
  Admission rejects:   ${admissionRej.count || 0}
═════════════════════════════════════════════════════════
`;
}
