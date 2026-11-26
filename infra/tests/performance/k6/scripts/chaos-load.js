// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Chaos + Load — combined resilience test.
//
// This script validates platform behavior DURING active chaos experiments.
// It does NOT inject chaos itself — Chaos Mesh (or equivalent) runs
// independently, applying pod kills, network delays, PG disconnects,
// and partition faults. This script measures the IMPACT of those failures.
//
// Three phases:
//
//   1. BASELINE (5 min)  — no chaos active. Establishes normal latency
//      and success rates for comparison.
//   2. CHAOS (15 min)    — chaos experiments are active. The operator
//      applies failures while this script sustains load. We measure
//      graceful degradation: retries, 429/503, and unexpected 5xx.
//   3. RECOVERY (5 min)  — chaos stopped. The platform should return
//      to baseline success rates within this window. If it doesn't,
//      recovery is broken.
//
// How to use:
//
//   1. Start this script: k6 run chaos-load.js
//   2. At t=5m, apply chaos experiments (manually or via CronChaos):
//      kubectl apply -f infra/tests/chaos/pod-kill.yaml
//      kubectl apply -f infra/tests/chaos/network-delay.yaml
//   3. At t=20m, remove chaos experiments:
//      kubectl delete -f infra/tests/chaos/
//   4. Script completes at t=25m with per-phase breakdown.
//
// Key metrics to watch:
//   - loopruntime_chaos_unexpected_5xx: bugs exposed by chaos (should be < 3)
//   - loopruntime_chaos_graceful_reject: expected 429/503 (proves backpressure works)
//   - loopruntime_chaos_retry_success: retries working (proves retry logic correct)
//   - loopruntime_build_succeeded: overall > 85% (relaxed because chaos is active)
//
// Roadmap coverage:
//   - Sprint 8 Phase 2: graceful degradation under fault injection
//   - Hardening H6: build pipeline survives partial infra failure

import { group, sleep } from 'k6';
import { Counter, Rate, Gauge, Trend } from 'k6/metrics';
import exec from 'k6/execution';

import {
  createProject,
  uploadSource,
  submitBuild,
  pollBuildUntilDone,
  healthCheck,
  appURL,
  verifyAppLive,
} from './lib/loopruntime-api.js';
import { loadBundleBytes } from '../fixtures/bundle-builder.js';

// -----------------------------------------------------------------------
// Custom Metrics
// -----------------------------------------------------------------------

// Phase tracking — emitted as Counter per phase for Grafana time-series.
// Value: 0=baseline, 1=chaos, 2=recovery.
const chaosPhaseGauge = new Gauge('loopruntime_chaos_phase');

// Builds that failed once but succeeded on retry — proves retry logic works.
const chaosRetrySuccess = new Rate('loopruntime_chaos_retry_success');

// Expected responses during chaos: 429 (admission reject) and 503 (circuit breaker).
// These are GOOD — they prove backpressure/circuit-breaking work.
const chaosGracefulReject = new Counter('loopruntime_chaos_graceful_reject');

// Unexpected 500 responses — these are BUGS exposed by chaos.
const chaosUnexpected5xx = new Counter('loopruntime_chaos_unexpected_5xx');

// Build success rate (reuses loopruntime_build_succeeded from lib).
const chaosBuildSuccess = new Rate('loopruntime_chaos_build_success');

// Per-phase success tracking for handleSummary breakdown.
const chaosBaselineSuccess = new Rate('loopruntime_chaos_baseline_success');
const chaosPhaseChaosSuccess = new Rate('loopruntime_chaos_phase_chaos_success');
const chaosRecoverySuccess = new Rate('loopruntime_chaos_recovery_success');

// Deploy duration during chaos (separate from baseline for comparison).
const chaosDeployDuration = new Trend('loopruntime_chaos_deploy_duration_ms', true);

// -----------------------------------------------------------------------
// Options
// -----------------------------------------------------------------------

export const options = {
  scenarios: {
    chaos_load: {
      executor: 'ramping-arrival-rate',
      startRate: 0,
      timeUnit: '1m',
      preAllocatedVUs: 30,
      maxVUs: 60,
      stages: [
        // Phase 1: BASELINE — ramp to 2 deploys/min over 2 min, hold for 3 min.
        { duration: '2m', target: 2 },
        { duration: '3m', target: 2 },
        // Phase 2: CHAOS — sustain 2 deploys/min for 15 min while chaos is active.
        { duration: '15m', target: 2 },
        // Phase 3: RECOVERY — sustain 2 deploys/min for 5 min after chaos removed.
        { duration: '5m', target: 2 },
      ],
    },
  },
  thresholds: {
    // During chaos: some tolerance for unexpected errors.
    'loopruntime_chaos_unexpected_5xx': ['count<3'],
    // Overall build success rate: relaxed to 85% because chaos is active.
    'loopruntime_chaos_build_success': ['rate>0.85'],
    // Recovery phase should show > 95% success.
    'loopruntime_chaos_recovery_success': ['rate>0.95'],
    // HTTP layer: relax to 15% failure (chaos causes expected failures).
    'http_req_failed': ['rate<0.15'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

// Phase time boundaries (relative to test start).
const BASELINE_END_MS = 5 * 60 * 1000;       // 5 min
const CHAOS_END_MS = 20 * 60 * 1000;         // 20 min
const RECOVERY_END_MS = 25 * 60 * 1000;      // 25 min

// -----------------------------------------------------------------------
// Setup
// -----------------------------------------------------------------------

export function setup() {
  if (!healthCheck()) {
    throw new Error('API health check failed — aborting chaos-load test');
  }

  const bundle = loadBundleBytes();
  return { bundle, startTime: Date.now() };
}

// -----------------------------------------------------------------------
// Per-iteration work
// -----------------------------------------------------------------------

export default function (data) {
  const vuID = exec.vu.idInTest;
  const iterID = exec.scenario.iterationInTest;
  const elapsed = Date.now() - data.startTime;

  // Determine current phase.
  let phase;
  if (elapsed < BASELINE_END_MS) {
    phase = 'baseline';
    chaosPhaseGauge.add(0);
  } else if (elapsed < CHAOS_END_MS) {
    phase = 'chaos';
    chaosPhaseGauge.add(1);
  } else {
    phase = 'recovery';
    chaosPhaseGauge.add(2);
  }

  // Rotate through 30 pre-seeded tenants.
  const tenantIdx = iterID % 30;
  const tenantID = `k6-chaos-tn-${tenantIdx}`;
  const userID = `k6-chaos-user-${tenantIdx}`;
  const email = `k6-chaos-${tenantIdx}@loopruntime.test`;
  const projectName = `k6-chaos-app-${tenantIdx}`;

  let commitSHA;
  let buildID;
  let retried = false;

  // Step 1: Ensure project exists.
  group('create project', function () {
    const r = createProject(tenantID, projectName, userID, email);
    if (!r.ok && r.status !== 402) {
      classifyResponse(r.status);
      if (r.status >= 500 && r.status < 600) {
        console.warn(`[${phase}] VU ${vuID}: project create got ${r.status}`);
      }
    }
  });

  sleep(5); // Brief wait for reconcile (shorter than L1 — project likely exists).

  // Step 2: Upload source bundle.
  group('upload source', function () {
    const r = uploadSource(tenantID, projectName, data.bundle, userID, email);
    if (!r.ok) {
      classifyResponse(r.status);
      console.warn(`[${phase}] VU ${vuID}: upload failed (${r.status})`);
      return;
    }
    commitSHA = r.commitSHA;
  });

  if (!commitSHA) {
    recordBuildResult(phase, false, false);
    return;
  }

  // Step 3: Submit build (with retry on transient failure).
  group('submit build', function () {
    const idempKey = `k6-chaos-${iterID}-${Date.now()}`;
    let r = submitBuild(tenantID, projectName, commitSHA, idempKey, userID, email);

    if (!r.ok && isTransient(r.status)) {
      // Retry once after backoff — validates retry logic works under chaos.
      classifyResponse(r.status);
      sleep(5);
      r = submitBuild(tenantID, projectName, commitSHA, `${idempKey}-retry`, userID, email);
      if (r.ok) {
        retried = true;
      }
    }

    if (!r.ok) {
      classifyResponse(r.status);
      console.warn(`[${phase}] VU ${vuID}: build submit failed (${r.status})`);
      return;
    }
    buildID = r.buildID;
  });

  if (!buildID) {
    recordBuildResult(phase, false, retried);
    return;
  }

  // Step 4: Poll build to terminal state.
  let buildOK = false;
  group('poll until done', function () {
    // Budget: 10 min during chaos (things are slower), 5 min baseline/recovery.
    const budget = phase === 'chaos' ? 10 * 60 * 1000 : 5 * 60 * 1000;
    const r = pollBuildUntilDone(tenantID, buildID, userID, email, budget, 15 * 1000);
    buildOK = r.ok;

    if (r.ok) {
      chaosDeployDuration.add(r.durationMs);
    }

    console.log(`[${phase}] VU ${vuID}: build ${buildID} → ${r.terminalState} (${Math.round(r.durationMs / 1000)}s)`);
  });

  recordBuildResult(phase, buildOK, retried);
}

// -----------------------------------------------------------------------
// Teardown
// -----------------------------------------------------------------------

export function teardown() {
  if (!healthCheck()) {
    console.error('API unhealthy after chaos-load test — platform may not have recovered!');
  }
}

// -----------------------------------------------------------------------
// Summary
// -----------------------------------------------------------------------

export function handleSummary(data) {
  return {
    stdout: chaosReport(data),
    '/tmp/k6-chaos-load-summary.json': JSON.stringify(data, null, 2),
  };
}

function chaosReport(data) {
  const m = data.metrics;
  const val = (name, field) => ((m[name] || {}).values || {})[field] || 0;

  return `
\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550
  CHAOS + LOAD — Resilience Test Results
\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550

  PHASE BREAKDOWN:
  \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500
  Baseline (0-5m):   ${(val('loopruntime_chaos_baseline_success', 'rate') * 100).toFixed(1)}% success
  Chaos (5-20m):     ${(val('loopruntime_chaos_phase_chaos_success', 'rate') * 100).toFixed(1)}% success
  Recovery (20-25m): ${(val('loopruntime_chaos_recovery_success', 'rate') * 100).toFixed(1)}% success

  RESILIENCE METRICS:
  \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500
  Unexpected 5xx:    ${val('loopruntime_chaos_unexpected_5xx', 'count')} (threshold: < 3)
  Graceful rejects:  ${val('loopruntime_chaos_graceful_reject', 'count')} (429/503 — expected)
  Retry successes:   ${(val('loopruntime_chaos_retry_success', 'rate') * 100).toFixed(1)}%
  Overall success:   ${(val('loopruntime_chaos_build_success', 'rate') * 100).toFixed(1)}% (threshold: > 85%)

  LATENCY (deploys during chaos):
  \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500
  Deploy p50:  ${Math.round(val('loopruntime_chaos_deploy_duration_ms', 'med') / 1000)}s
  Deploy p95:  ${Math.round(val('loopruntime_chaos_deploy_duration_ms', 'p(95)') / 1000)}s
  Deploy p99:  ${Math.round(val('loopruntime_chaos_deploy_duration_ms', 'p(99)') / 1000)}s

  HTTP failures: ${(val('http_req_failed', 'rate') * 100).toFixed(1)}%
\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550
`;
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

function classifyResponse(status) {
  if (status === 429 || status === 503) {
    chaosGracefulReject.add(1);
  } else if (status >= 500 && status < 600) {
    chaosUnexpected5xx.add(1);
  }
}

function isTransient(status) {
  return status === 429 || status === 503 || status === 502 || status === 504;
}

function recordBuildResult(phase, success, retried) {
  chaosBuildSuccess.add(success ? 1 : 0);

  if (retried) {
    chaosRetrySuccess.add(success ? 1 : 0);
  }

  switch (phase) {
    case 'baseline':
      chaosBaselineSuccess.add(success ? 1 : 0);
      break;
    case 'chaos':
      chaosPhaseChaosSuccess.add(success ? 1 : 0);
      break;
    case 'recovery':
      chaosRecoverySuccess.add(success ? 1 : 0);
      break;
  }
}
