// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// L1 — 10 tenants smoke test.
//
// Scenario: 10 virtual users ramp up over 30s, each deploys one app,
// monitors it to terminal state, then exits. Total budget: 15 min.
//
// Why this beats the old shell script:
//
//  1. Client-side latency measurement (old script measured server
//     response time only, missing TCP/TLS overhead).
//  2. Coordinated-omission-safe: each VU is independent, so a slow
//     build does not delay the next submission.
//  3. Thresholds ENFORCE the SLO — failing p99 fails the test with
//     exit code 99, which fails CI.
//  4. Prometheus remote-write: all metrics flow to Grafana's
//     cell-saturation dashboard in real time.
//
// Roadmap coverage:
//   - Item 01 DoD #1: L1 10/10 SUCCEEDED
//   - Item 03 DoD #4: cell-0 backward compat
//   - Item 07: admission gate must NOT reject any build at L1 (cell has capacity)

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

// -----------------------------------------------------------------------
// Options
// -----------------------------------------------------------------------

export const options = {
  scenarios: {
    l1_smoke: {
      executor: 'per-vu-iterations',
      vus: 10,
      iterations: 1,
      maxDuration: '15m',
      gracefulStop: '2m',
    },
  },
  thresholds: thresholdsFor('l1'),
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

// -----------------------------------------------------------------------
// Setup — runs once before all VUs
// -----------------------------------------------------------------------

export function setup() {
  // Verify the API is reachable before spawning VUs.
  if (!healthCheck()) {
    throw new Error('API health check failed — aborting test');
  }

  // Prime the bundle fixture (read once, share across VUs).
  const bundle = loadBundleBytes();
  return { bundle };
}

// -----------------------------------------------------------------------
// Per-VU work
// -----------------------------------------------------------------------

export default function (data) {
  // Each VU owns a unique tenant identified by its VU number.
  // __VU is k6's built-in VU index (1-indexed).
  const vuID = exec.vu.idInTest;
  const tenantID = `k6-l1-tn-${vuID}`;
  const userID = `k6-l1-user-${vuID}`;
  const email = `k6-l1-${vuID}@loop-runtime.test`;
  const projectName = `k6-l1-app-${vuID}`;

  let commitSHA;
  let buildID;

  // Note: tenants/users/memberships must already exist in the DB.
  // The k6 Job runs a pre-seed step before `k6 run`. See
  // job-template.yaml for the seed SQL.

  group('create project', function () {
    const r = createProject(tenantID, projectName, userID, email);
    if (!r.ok) {
      // 402 plan-limit means the project already exists from a prior
      // run — that's a retryable test-data condition, not a platform bug.
      if (r.status === 402) {
        console.warn(`VU ${vuID}: project exists, reusing`);
      } else {
        exec.test.abort(`create project failed: ${r.error}`);
      }
    }
  });

  sleep(20); // TenantController reconcile window

  group('upload source', function () {
    const r = uploadSource(tenantID, projectName, data.bundle, userID, email);
    if (!r.ok) {
      console.error(`VU ${vuID} upload failed: ${r.error}`);
      return;
    }
    commitSHA = r.commitSHA;
  });

  if (!commitSHA) {
    return;
  }

  group('submit build', function () {
    const idempKey = `k6-l1-${vuID}-${Date.now()}`;
    const r = submitBuild(tenantID, projectName, commitSHA, idempKey, userID, email);
    if (!r.ok) {
      console.error(`VU ${vuID} submit failed: ${r.error} (retryAfter=${r.retryAfter})`);
      return;
    }
    buildID = r.buildID;
  });

  if (!buildID) {
    return;
  }

  let buildOK = false;
  group('poll until done', function () {
    const r = pollBuildUntilDone(tenantID, buildID, userID, email, 15 * 60 * 1000, 20 * 1000);
    console.log(`VU ${vuID}: build ${buildID} → ${r.terminalState} in ${Math.round(r.durationMs / 1000)}s`);
    if (!r.ok) {
      console.error(`VU ${vuID}: build did not reach SUCCEEDED`);
    }
    buildOK = r.ok;
  });

  if (!buildOK) {
    return;
  }

  // scale-50k-plan requirement: every deploy verified live via HTTP 200.
  // This is the "dogfood guarantee" — if the URL doesn't return 200,
  // the deploy is not successful regardless of build state.
  group('verify app live', function () {
    const url = appURL(tenantID, projectName);
    const r = verifyAppLive(url, 120 * 1000);
    if (r.ok) {
      console.log(`VU ${vuID}: app LIVE at ${url} in ${Math.round(r.durationMs / 1000)}s`);
    } else {
      console.error(`VU ${vuID}: app NOT LIVE at ${url} (last status=${r.statusCode}, waited ${Math.round(r.durationMs / 1000)}s)`);
    }
  });
}

// -----------------------------------------------------------------------
// Teardown — runs once after all VUs complete
// -----------------------------------------------------------------------

export function teardown() {
  // Final health check — verify the platform survived the test.
  if (!healthCheck()) {
    console.error('API unhealthy after L1 test — investigate!');
  }
}

// -----------------------------------------------------------------------
// Report customization (runs after all metrics are collected)
// -----------------------------------------------------------------------

export function handleSummary(data) {
  // Emit both stdout summary and a JSON file for CI to parse.
  return {
    stdout: summaryText(data),
    '/tmp/k6-l1-summary.json': JSON.stringify(data, null, 2),
  };
}

function summaryText(data) {
  const m = data.metrics;
  const succeeded = (m.loop_runtime_build_succeeded || {}).values || {};
  const deploy = (m.loop_runtime_deploy_duration_ms || {}).values || {};
  const verified = (m.loop_runtime_app_verified || {}).values || {};
  const verify = (m.loop_runtime_app_verify_duration_ms || {}).values || {};
  return `
═════════════════════════════════════════════════════════
  L1 — 10 tenants (k6)
═════════════════════════════════════════════════════════
  Build success rate:  ${(succeeded.rate * 100 || 0).toFixed(1)}%
  Apps verified live:  ${(verified.rate * 100 || 0).toFixed(1)}%
  Deploy p50:          ${Math.round((deploy.med || 0) / 1000)}s
  Deploy p95:          ${Math.round((deploy['p(95)'] || 0) / 1000)}s
  Deploy p99:          ${Math.round((deploy['p(99)'] || 0) / 1000)}s
  App verify p50:      ${Math.round((verify.med || 0) / 1000)}s
  App verify p95:      ${Math.round((verify['p(95)'] || 0) / 1000)}s
  HTTP failures:       ${((m.http_req_failed || {}).values || {}).rate || 0}
═════════════════════════════════════════════════════════
`;
}
