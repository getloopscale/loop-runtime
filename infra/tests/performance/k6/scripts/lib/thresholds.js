// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// SLO thresholds for k6 load tests.
//
// These numbers come from cell-budget and the SLO
// targets in each roadmap item. Changing a threshold here must be
// justified against the budget doc — k6 will fail the test (non-zero
// exit code) when any threshold is breached, which feeds directly
// into CI pass/fail.
//
// The thresholds are split into three tiers:
//
//   1. base — universal SLOs every load level must meet (L1 through L5).
//      e.g., "any 5xx is a bug, p99 must stay under the deploy budget."
//
//   2. perLevel — level-specific tightenings. L5 demands tighter
//      latency than L2 because the whole point of the roadmap is to
//      prove linear scalability.
//
//   3. custom metrics — references to the Rate/Trend metrics defined
//      in lib/loop-runtime-api.js. Scripts can opt-in per test.

export const base = {
  // HTTP layer — measured by k6 automatically.
  'http_req_failed':   ['rate<0.01'],      // < 1% non-2xx at HTTP level
  'http_req_duration': [
    'p(50)<2000',                           // p50 < 2s
    'p(95)<10000',                          // p95 < 10s
    'p(99)<30000',                          // p99 < 30s (API only, not deploy)
  ],

  // Generator health — under `ramping-arrival-rate`, dropped iterations
  // mean the k6 pod (not the platform) was the bottleneck: VUs hit the
  // `maxVUs` ceiling and k6 dropped queued load → the run SILENTLY
  // under-loaded the platform, making any SLO claim from it false.
  // Fail when more than a handful drop. N=100 is a starting absolute
  // (warm-up tolerance); calibrate against the first LIVE operator run
  // per cell-budget. `dropped_iterations` is a Counter
  // → use count< (a rate< form would be a silent no-op).
  'dropped_iterations': ['count<100'],

  // Custom LoopRuntime metrics.
  'loop_runtime_build_submit_success':   ['rate>0.99'],  // ≥ 99% submissions accepted
  'loop_runtime_project_created':        ['rate>0.99'],  // ≥ 99% project creations succeed
  'loop_runtime_build_succeeded':        ['rate>0.99'],  // ≥ 99% builds reach SUCCEEDED
  'loop_runtime_app_verified':           ['rate>0.99'],  // ≥ 99% apps verified live (HTTP 200)
  'loop_runtime_upload_duration_ms':     ['p(95)<10000'], // upload < 10s
  'loop_runtime_build_submit_duration_ms': ['p(95)<2000'], // build submit < 2s
  'loop_runtime_deploy_duration_ms':     ['p(95)<600000'], // deploy < 600s (CLAUDE.md budget)
  'loop_runtime_app_verify_duration_ms': ['p(95)<120000'], // app live within 120s of build SUCCEEDED
};

// Per-level tightenings. Merge into base to get the final thresholds.
export const perLevel = {
  l1: {
    'loop_runtime_deploy_duration_ms': ['p(99)<600000'], // L1: p99 < 10min
  },
  l2: {
    'loop_runtime_deploy_duration_ms': ['p(99)<600000'], // L2: p99 < 10min
    'loop_runtime_build_succeeded':    ['rate>0.99'],    // >= 99%
  },
  l3: {
    'loop_runtime_deploy_duration_ms': ['p(99)<900000'], // L3: p99 < 15min (more contention)
    'loop_runtime_build_succeeded':    ['rate>0.995'],   // >= 99.5% per roadmap 08
  },
  l4: {
    'loop_runtime_deploy_duration_ms': ['p(99)<1200000'], // L4: p99 < 20min
    'loop_runtime_build_succeeded':    ['rate>0.99'],     // >= 99% per roadmap 08
  },
  l5: {
    'loop_runtime_deploy_duration_ms': ['p(99)<1500000'], // L5: p99 < 25min
    'loop_runtime_build_succeeded':    ['rate>0.99'],     // >= 99% per roadmap 08
  },
};

/**
 * Combines base thresholds with a per-level override. Use in your
 * script's options.thresholds field.
 *
 * @param {keyof perLevel} level
 * @returns {object} merged threshold map
 */
export function thresholdsFor(level) {
  const overrides = perLevel[level] || {};
  return Object.assign({}, base, overrides);
}
