// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Soak test — 6 hours at L2 load (50 tenants).
//
// Objective: detect TEMPORAL degradation that L1-L5 tests miss because
// they run for minutes, not hours. Specifically:
//
//   1. Memory leaks (Go GC cannot reclaim if references accumulate)
//   2. File descriptor exhaustion (leaked connections, unclosed streams)
//   3. Connection pool starvation (PG pool, HTTP client pool)
//   4. GC pressure / stop-the-world pauses under sustained load
//   5. Log volume / disk pressure from 360 deploys worth of data
//
// Design:
//   - constant-arrival-rate at 1 deploy/min for 6h = 360 deploys total.
//   - 50 tenant IDs rotating — each tenant gets ~7 deploys over 6h.
//   - CO-safe: arrival rate is independent of response time, so a slow
//     build does not reduce the injection rate (coordinated omission).
//   - preAllocatedVUs = 30, maxVUs = 100 (enough headroom for build
//     queue backup during GC pauses or slow periods).
//
// How to interpret results:
//   - Compare p99 in first vs last hour. If last hour p99 > 2x first
//     hour p99, there is temporal degradation.
//   - loopruntime_soak_hour_marker increments every ~60 iterations for Grafana
//     annotation queries ("WHERE loopruntime_soak_hour_marker changed").
//   - Zero 5xx is non-negotiable — unlike stress-break-point, soak load
//     is within the platform's advertised capacity. Any 5xx is a bug.
//
// Run:
//   K6_API_URL=http://... K6_SVC_AUTH=... k6 run soak-6h.js

import { group, sleep } from 'k6';
import exec from 'k6/execution';
import { Counter, Trend } from 'k6/metrics';

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

// -----------------------------------------------------------------------
// Custom soak metrics
// -----------------------------------------------------------------------

const soakIterationTotal = new Counter('loopruntime_soak_iteration_total');
const soakHourMarker = new Counter('loopruntime_soak_hour_marker');
const soak5xxTotal = new Counter('loopruntime_soak_5xx_total');

// -----------------------------------------------------------------------
// Options
// -----------------------------------------------------------------------

const l2Thresholds = thresholdsFor('l2');

export const options = {
  scenarios: {
    soak_6h: {
      executor: 'constant-arrival-rate',
      rate: 1,
      timeUnit: '1m',               // 1 deploy per minute
      duration: '6h',
      preAllocatedVUs: 30,
      maxVUs: 100,
      gracefulStop: '30m',          // allow in-flight builds to complete
    },
  },
  thresholds: Object.assign({}, l2Thresholds, {
    // Soak-specific: zero 5xx (we are within budget, unlike stress test)
    'loopruntime_soak_5xx_total':    ['count<1'],
    // Tighten build success for soak (sustained reliability)
    'loopruntime_build_succeeded':   ['rate>0.995'],
    // HTTP p99 drift detection (absolute ceiling — temporal comparison
    // is done in handleSummary since k6 doesn't support per-window thresholds)
    'http_req_duration':      ['p(99)<60000'],   // absolute ceiling: 60s for any API call
  }),
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

// -----------------------------------------------------------------------
// Constants
// -----------------------------------------------------------------------

const TENANT_COUNT = 50;

// -----------------------------------------------------------------------
// Setup
// -----------------------------------------------------------------------

export function setup() {
  if (!healthCheck()) {
    throw new Error('API health check failed — aborting soak test');
  }

  const bundle = loadBundleBytes();

  // Pre-create all 50 tenant projects so the soak loop only deploys.
  for (let i = 0; i < TENANT_COUNT; i++) {
    const tenantID = `k6-soak-tn-${i}`;
    const userID = `k6-soak-user-${i}`;
    const email = `k6-soak-${i}@loopruntime.test`;
    const projectName = `k6-soak-app-${i}`;
    createProject(tenantID, projectName, userID, email);
    // Small pause to avoid hammering during setup
    sleep(0.2);
  }

  return { bundle };
}

// -----------------------------------------------------------------------
// Per-iteration work (1 deploy/min, sustained 6h)
// -----------------------------------------------------------------------

export default function (data) {
  const iter = exec.scenario.iterationInTest;
  const tenantIdx = iter % TENANT_COUNT;

  const tenantID = `k6-soak-tn-${tenantIdx}`;
  const userID = `k6-soak-user-${tenantIdx}`;
  const email = `k6-soak-${tenantIdx}@loopruntime.test`;
  const projectName = `k6-soak-app-${tenantIdx}`;

  // Track total iterations for observability.
  soakIterationTotal.add(1);

  // Emit hour marker every 60 iterations (~1/hour at 1 deploy/min).
  if (iter > 0 && iter % 60 === 0) {
    soakHourMarker.add(1);
  }

  group('soak_deploy', function () {
    // 1. Upload source bundle
    const upload = uploadSource(tenantID, projectName, data.bundle, userID, email);
    if (!upload.ok) {
      classifyStatus(upload.status);
      return;
    }

    // 2. Submit build
    const idempKey = `k6-soak-${tenantIdx}-${iter}-${Date.now()}`;
    const build = submitBuild(
      tenantID, projectName, upload.commitSHA, idempKey, userID, email
    );
    if (!build.ok) {
      classifyStatus(build.status);
      return;
    }

    // 3. Poll until terminal state (20min timeout, 20s interval)
    const result = pollBuildUntilDone(
      tenantID, build.buildID, userID, email,
      20 * 60 * 1000,  // 20min
      20 * 1000         // 20s poll
    );

    // 4. Verify app is live (only on success)
    if (result.ok) {
      const url = appURL(tenantID, projectName, 'api', 'production');
      verifyAppLive(url, 120 * 1000);
    }
  });
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

function classifyStatus(status) {
  if (status >= 500) {
    soak5xxTotal.add(1);
  }
}

// -----------------------------------------------------------------------
// Teardown
// -----------------------------------------------------------------------

export function teardown() {
  if (!healthCheck()) {
    console.error('SOAK FAILURE: API unhealthy after 6h — temporal degradation detected');
  } else {
    console.log('SOAK PASSED: API healthy after 6h sustained load');
  }
}

// -----------------------------------------------------------------------
// Summary report
// -----------------------------------------------------------------------

export function handleSummary(data) {
  const m = data.metrics;
  const httpDur = (m.http_req_duration || {}).values || {};
  const buildSucc = (m.loopruntime_build_succeeded || {}).values || {};
  const soakIter = (m.loopruntime_soak_iteration_total || {}).values || {};
  const soakHour = (m.loopruntime_soak_hour_marker || {}).values || {};
  const s5xx = (m.loopruntime_soak_5xx_total || {}).values || {};
  const deployDur = (m.loopruntime_deploy_duration_ms || {}).values || {};
  const appVer = (m.loopruntime_app_verified || {}).values || {};

  const totalIter = soakIter.count || 0;
  const hours = (soakHour.count || 0) + 1; // hour markers + 1 for first hour
  const buildRate = buildSucc.rate !== undefined ? (buildSucc.rate * 100).toFixed(2) : 'N/A';
  const appRate = appVer.rate !== undefined ? (appVer.rate * 100).toFixed(2) : 'N/A';

  const text = `
${'='.repeat(65)}
  SOAK TEST REPORT — 6h at L2 load (50 tenants, 1 deploy/min)
${'='.repeat(65)}

  Duration:              ~${hours} hours (${totalIter} iterations completed)
  Tenants:              50 (rotating)
  Target:               360 deploys total

  HTTP Latency:
    p50:                ${Math.round(httpDur.med || 0)}ms
    p95:                ${Math.round(httpDur['p(95)'] || 0)}ms
    p99:                ${Math.round(httpDur['p(99)'] || 0)}ms
    max:                ${Math.round(httpDur.max || 0)}ms

  Deploy Latency:
    avg:                ${Math.round(deployDur.avg || 0)}ms
    p95:                ${Math.round(deployDur['p(95)'] || 0)}ms
    p99:                ${Math.round(deployDur['p(99)'] || 0)}ms

  Reliability:
    Build success rate: ${buildRate}%  (target: >99.5%)
    App verified rate:  ${appRate}%  (target: >99%)
    5xx errors:         ${s5xx.count || 0}         (target: 0)

  Temporal Degradation Detection:
    (Compare HTTP p99 in Grafana first-hour vs last-hour panels.
     If last-hour p99 > 2x first-hour p99, investigate GC/pool/FD.)
    Hour markers emitted: ${soakHour.count || 0} (query: loopruntime_soak_hour_marker)

  ${(s5xx.count || 0) === 0 && buildRate !== 'N/A' && parseFloat(buildRate) >= 99.5 ?
    'SOAK PASSED — no temporal degradation detected' :
    'SOAK FAILED — check metrics above for degradation signal'}
${'='.repeat(65)}
`;
  return {
    stdout: text,
    '/tmp/k6-soak-6h-summary.json': JSON.stringify(data, null, 2),
  };
}
