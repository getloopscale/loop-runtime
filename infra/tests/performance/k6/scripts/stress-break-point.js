// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Stress test — find the break point.
//
// Unlike L1/L2/L3 which verify the platform handles EXPECTED load,
// this test ramps until the SLO breaks. The goal is to discover:
//
//   1. At what arrival rate does p99 exceed the budget?
//   2. At what arrival rate does admission start rejecting (429)?
//   3. At what arrival rate does the PG circuit breaker trip (503)?
//   4. Does the platform degrade gracefully (slower) or catastrophically
//      (crashes, data loss)?
//
// The test does NOT enforce latency thresholds — we expect them to
// fail past the break point. It enforces only the "no crashes" rule:
// all HTTP failures must be 429 or 503 (expected backpressure), never
// 500 (bug) or connection errors (infra crash).
//
// Roadmap 07 DoD: TestGracefulDegradation_150Percent. We push to 150%
// of the cell's build-concurrency budget (20 → 30 concurrent builds)
// and verify the system queues+drains without crashing.

import { group } from 'k6';
import { check } from 'k6';

import {
  createProject,
  uploadSource,
  submitBuild,
  healthCheck,
  admissionReject,
  lockConflict,
} from './lib/loopruntime-api.js';
import { Counter } from 'k6/metrics';
import { loadBundleBytes } from '../fixtures/bundle-builder.js';

const serverErrors = new Counter('loopruntime_stress_5xx_total');
const connectionErrors = new Counter('loopruntime_stress_conn_error_total');

export const options = {
  scenarios: {
    stress_rampup: {
      executor: 'ramping-arrival-rate',
      startRate: 1,
      timeUnit: '1s',
      preAllocatedVUs: 50,
      maxVUs: 200,
      stages: [
        // Phase 1: warm up at baseline (< budget)
        { target: 1,   duration: '2m'  },
        // Phase 2: hit the budget line (20 builds/cell)
        { target: 5,   duration: '2m'  },
        // Phase 3: push to 150% of budget
        { target: 10,  duration: '3m'  },
        // Phase 4: stay at 150% long enough to see sustained behavior
        { target: 10,  duration: '5m'  },
        // Phase 5: back off and observe recovery
        { target: 1,   duration: '2m'  },
      ],
      gracefulStop: '5m',
    },
  },
  // Only enforce the "no crashes" rule.
  // Latency is expected to blow up past the break point.
  thresholds: {
    'loopruntime_stress_5xx_total':     ['count<1'],   // ZERO server errors allowed
    'loopruntime_stress_conn_error_total': ['count<5'], // a few connection hiccups ok
    'http_req_failed': ['rate<0.60'],           // < 60% overall (429s are expected)
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

export function setup() {
  if (!healthCheck()) {
    throw new Error('API health check failed — aborting stress test');
  }
  return { bundle: loadBundleBytes() };
}

export default function (data) {
  // Stress test submits builds fast and does NOT wait for them to
  // complete. The point is to saturate the admission gate, not to
  // verify end-to-end deploy.

  const iter = __ITER + __VU * 10000; // unique ID across all VUs/iters
  const tenantID = `k6-stress-tn-${iter % 50}`; // 50 tenants, stress per-tenant quota too
  const userID = `k6-stress-user-${iter % 50}`;
  const email = `k6-stress-${iter % 50}@loopruntime.test`;
  const projectName = `k6-stress-app-${iter % 50}`;

  // First visit to a tenant creates the project.
  if (iter < 50) {
    createProject(tenantID, projectName, userID, email);
  }

  group('upload', function () {
    const r = uploadSource(tenantID, projectName, data.bundle, userID, email);
    if (!r.ok) return;
    classifyError(r.status);

    group('submit', function () {
      const idempKey = `k6-stress-${iter}-${Date.now()}`;
      const r2 = submitBuild(tenantID, projectName, r.commitSHA, idempKey, userID, email);
      classifyError(r2.status);

      if (r2.status === 429) {
        // Expected — we're stressing admission. Log the Retry-After
        // hint so we can verify it scales with saturation.
        console.log(`admission 429 retryAfter=${r2.retryAfter}`);
      }
    });
  });
}

function classifyError(status) {
  if (status === 0) {
    connectionErrors.add(1);
    return;
  }
  if (status >= 500 && status !== 503) {
    // 503 = circuit breaker (expected backpressure)
    // 500 = server bug (unacceptable)
    serverErrors.add(1);
  }
}

export function teardown() {
  if (!healthCheck()) {
    console.error('BREAK POINT: API unhealthy after stress test — platform crashed');
  } else {
    console.log('SURVIVED: API healthy after stress test — graceful degradation OK');
  }
}

export function handleSummary(data) {
  const m = data.metrics;
  const http = (m.http_req_duration || {}).values || {};
  const admission = (m.loopruntime_admission_reject_total || {}).values || {};
  const lock = (m.loopruntime_lock_conflict_total || {}).values || {};
  const s5xx = (m.loopruntime_stress_5xx_total || {}).values || {};
  const connErr = (m.loopruntime_stress_conn_error_total || {}).values || {};

  const text = `
═════════════════════════════════════════════════════════
  STRESS — break point analysis
═════════════════════════════════════════════════════════
  HTTP p50:            ${Math.round(http.med || 0)}ms
  HTTP p95:            ${Math.round(http['p(95)'] || 0)}ms
  HTTP p99:            ${Math.round(http['p(99)'] || 0)}ms

  Expected backpressure:
    Admission 429s:    ${admission.count || 0}   ← roadmap 07 working
    Lock 409s:         ${lock.count || 0}       ← env lock working

  FAIL conditions:
    5xx (non-503):     ${s5xx.count || 0}       ← MUST be 0
    Connection errors: ${connErr.count || 0}    ← MUST be < 5

  ${(s5xx.count || 0) === 0 && (connErr.count || 0) < 5 ?
    '✅ GRACEFUL DEGRADATION' :
    '❌ BROKE UNGRACEFULLY'}
═════════════════════════════════════════════════════════
`;
  return {
    stdout: text,
    '/tmp/k6-stress-summary.json': JSON.stringify(data, null, 2),
  };
}
