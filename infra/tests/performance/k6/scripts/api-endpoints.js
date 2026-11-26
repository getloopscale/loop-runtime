// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// API Endpoints — pure HTTP load test.
//
// Unlike the deploy pipeline tests (L1-L5) that exercise the full build
// lifecycle, this script tests the API's HTTP endpoints directly with
// high concurrency. No builds, no waiting for ArgoCD — pure
// request/response latency under sustained RPS.
//
// Purpose:
//   - Validate API performance for read-heavy operations (list projects,
//     get build status, list environments).
//   - Detect lock contention on the write path (POST /projects).
//   - Establish latency baselines per endpoint for SLO enforcement.
//   - Find performance regressions before they hit deploy-time paths.
//
// Target: 100 requests/second for 5 minutes = 30,000 total requests.
// With 50 pre-allocated VUs and burst to 200, this validates the API
// can handle high concurrency without connection pool exhaustion or
// PG advisory lock starvation.
//
// Pre-requisites:
//   - Tenant `k6-api-tn-0` must exist in the DB with a pre-seeded
//     project and at least one build (for GET /builds/{id}).
//   - Seed SQL in the k6 Job template handles this.
//
// Roadmap coverage:
//   - Sprint 8: API latency under sustained load
//   - Hardening H4: observability baseline per endpoint

import { group, sleep } from 'k6';
import { Trend, Counter } from 'k6/metrics';
import http from 'k6/http';

import { API_URL, SVC_AUTH, healthCheck } from './lib/loop-runtime-api.js';

// -----------------------------------------------------------------------
// Custom Metrics — one Trend per endpoint for independent SLO tracking
// -----------------------------------------------------------------------

const healthDuration = new Trend('loop_runtime_api_health_duration', true);
const projectsListDuration = new Trend('loop_runtime_api_projects_list_duration', true);
const buildsGetDuration = new Trend('loop_runtime_api_builds_get_duration', true);
const envsListDuration = new Trend('loop_runtime_api_envs_list_duration', true);
const projectsCreateDuration = new Trend('loop_runtime_api_projects_create_duration', true);

// Error counters per endpoint for diagnostics.
const healthErrors = new Counter('loop_runtime_api_health_errors');
const projectsListErrors = new Counter('loop_runtime_api_projects_list_errors');
const buildsGetErrors = new Counter('loop_runtime_api_builds_get_errors');
const envsListErrors = new Counter('loop_runtime_api_envs_list_errors');
const projectsCreateErrors = new Counter('loop_runtime_api_projects_create_errors');

// -----------------------------------------------------------------------
// Options
// -----------------------------------------------------------------------

export const options = {
  scenarios: {
    api_load: {
      executor: 'constant-arrival-rate',
      rate: 100,
      timeUnit: '1s',
      duration: '5m',
      preAllocatedVUs: 50,
      maxVUs: 200,
    },
  },
  thresholds: {
    // Per-endpoint latency SLOs.
    'loop_runtime_api_health_duration': ['p(99)<100'],
    'loop_runtime_api_projects_list_duration': ['p(99)<500'],
    'loop_runtime_api_builds_get_duration': ['p(99)<200'],
    'loop_runtime_api_envs_list_duration': ['p(99)<500'],
    'loop_runtime_api_projects_create_duration': ['p(99)<1000'],
    // Overall HTTP failure rate.
    'http_req_failed': ['rate<0.01'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

// -----------------------------------------------------------------------
// Configuration
// -----------------------------------------------------------------------

// Pre-seeded test data references.
const TENANT_ID = 'k6-api-tn-0';
const USER_ID = 'k6-api-user-0';
const EMAIL = 'k6-api-0@loop-runtime.test';

// Pre-seeded build ID — populated in setup() by listing builds.
let SEED_BUILD_ID = null;

function authHeaders() {
  return {
    'X-Service-Auth': SVC_AUTH,
    'X-Loop-Runtime-User-ID': USER_ID,
    'X-Loop-Runtime-Tenant': TENANT_ID,
    'X-Loop-Runtime-Email': EMAIL,
    'Content-Type': 'application/json',
  };
}

// -----------------------------------------------------------------------
// Setup
// -----------------------------------------------------------------------

export function setup() {
  if (!healthCheck()) {
    throw new Error('API health check failed — aborting api-endpoints test');
  }

  // Discover a pre-seeded build ID for GET /builds/{id} tests.
  // If none exists, we'll use a placeholder that returns 404 (still tests latency).
  let buildID = 'seed-build-placeholder';
  const res = http.get(`${API_URL}/api/v1/builds?limit=1`, {
    headers: authHeaders(),
  });
  if (res.status === 200) {
    try {
      const body = JSON.parse(res.body);
      if (body && body.builds && body.builds.length > 0) {
        buildID = body.builds[0].id || body.builds[0].build_id;
      }
    } catch (_) {
      // Use placeholder — GET will return 404 but latency is still measured.
    }
  }

  return { buildID };
}

// -----------------------------------------------------------------------
// Per-iteration work — round-robin across endpoints
// -----------------------------------------------------------------------

export default function (data) {
  // Distribute requests across endpoints using weighted random selection.
  // Reads are 80% of traffic, writes 20% (simulates real usage).
  const roll = Math.random();

  if (roll < 0.10) {
    // 10% — Health check (baseline latency, no auth).
    testHealth();
  } else if (roll < 0.35) {
    // 25% — List projects (authenticated read).
    testProjectsList();
  } else if (roll < 0.55) {
    // 20% — Get build status (authenticated read, single resource).
    testBuildsGet(data.buildID);
  } else if (roll < 0.80) {
    // 25% — List environments (authenticated read).
    testEnvsList();
  } else {
    // 20% — Create project (authenticated write, tests lock contention).
    testProjectsCreate();
  }
}

// -----------------------------------------------------------------------
// Endpoint test functions
// -----------------------------------------------------------------------

function testHealth() {
  group('GET /health', function () {
    const start = Date.now();
    const res = http.get(`${API_URL}/api/v1/health`, {
      tags: { endpoint: 'health' },
    });
    healthDuration.add(Date.now() - start);

    if (res.status !== 200) {
      healthErrors.add(1);
    }
  });
}

function testProjectsList() {
  group('GET /projects', function () {
    const start = Date.now();
    const res = http.get(`${API_URL}/api/v1/projects`, {
      headers: authHeaders(),
      tags: { endpoint: 'projects_list' },
    });
    projectsListDuration.add(Date.now() - start);

    if (res.status !== 200) {
      projectsListErrors.add(1);
    }
  });
}

function testBuildsGet(buildID) {
  group('GET /builds/{id}', function () {
    const start = Date.now();
    const res = http.get(`${API_URL}/api/v1/builds/${buildID}`, {
      headers: authHeaders(),
      tags: { endpoint: 'builds_get' },
    });
    buildsGetDuration.add(Date.now() - start);

    // 200 or 404 are both valid (seed may not exist). Only 5xx is an error.
    if (res.status >= 500) {
      buildsGetErrors.add(1);
    }
  });
}

function testEnvsList() {
  group('GET /environments', function () {
    const start = Date.now();
    const res = http.get(`${API_URL}/api/v1/environments`, {
      headers: authHeaders(),
      tags: { endpoint: 'envs_list' },
    });
    envsListDuration.add(Date.now() - start);

    if (res.status !== 200) {
      envsListErrors.add(1);
    }
  });
}

function testProjectsCreate() {
  group('POST /projects', function () {
    // Use unique project name per iteration to avoid 409 conflicts piling up.
    // Format: k6-api-proj-{timestamp}-{random} (collision-free).
    const projectName = `k6-api-proj-${Date.now()}-${Math.floor(Math.random() * 10000)}`;
    const body = JSON.stringify({ name: projectName });

    const start = Date.now();
    const res = http.post(`${API_URL}/api/v1/projects`, body, {
      headers: authHeaders(),
      tags: { endpoint: 'projects_create' },
    });
    projectsCreateDuration.add(Date.now() - start);

    // 201, 200, 402 (plan limit) are all "working correctly".
    // Only 5xx indicates a real problem.
    if (res.status >= 500) {
      projectsCreateErrors.add(1);
    }
  });
}

// -----------------------------------------------------------------------
// Teardown
// -----------------------------------------------------------------------

export function teardown() {
  if (!healthCheck()) {
    console.error('API unhealthy after api-endpoints test — investigate!');
  }
}

// -----------------------------------------------------------------------
// Summary
// -----------------------------------------------------------------------

export function handleSummary(data) {
  return {
    stdout: endpointReport(data),
    '/tmp/k6-api-endpoints-summary.json': JSON.stringify(data, null, 2),
  };
}

function endpointReport(data) {
  const m = data.metrics;
  const trend = (name) => ((m[name] || {}).values || {});
  const cnt = (name) => ((m[name] || {}).values || {}).count || 0;

  const h = trend('loop_runtime_api_health_duration');
  const pl = trend('loop_runtime_api_projects_list_duration');
  const bg = trend('loop_runtime_api_builds_get_duration');
  const el = trend('loop_runtime_api_envs_list_duration');
  const pc = trend('loop_runtime_api_projects_create_duration');

  const fmtMs = (v) => v ? `${Math.round(v)}ms` : 'N/A';

  return `
\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550
  API ENDPOINTS — HTTP Load Test Results (100 rps x 5 min)
\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550

  ENDPOINT                  p50       p95       p99       ERRORS
  \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500
  GET /health               ${fmtMs(h.med).padEnd(10)}${fmtMs(h['p(95)']).padEnd(10)}${fmtMs(h['p(99)']).padEnd(10)}${cnt('loop_runtime_api_health_errors')}
  GET /projects             ${fmtMs(pl.med).padEnd(10)}${fmtMs(pl['p(95)']).padEnd(10)}${fmtMs(pl['p(99)']).padEnd(10)}${cnt('loop_runtime_api_projects_list_errors')}
  GET /builds/{id}          ${fmtMs(bg.med).padEnd(10)}${fmtMs(bg['p(95)']).padEnd(10)}${fmtMs(bg['p(99)']).padEnd(10)}${cnt('loop_runtime_api_builds_get_errors')}
  GET /environments         ${fmtMs(el.med).padEnd(10)}${fmtMs(el['p(95)']).padEnd(10)}${fmtMs(el['p(99)']).padEnd(10)}${cnt('loop_runtime_api_envs_list_errors')}
  POST /projects            ${fmtMs(pc.med).padEnd(10)}${fmtMs(pc['p(95)']).padEnd(10)}${fmtMs(pc['p(99)']).padEnd(10)}${cnt('loop_runtime_api_projects_create_errors')}

  THRESHOLDS:
  \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500
  GET /health        p99 < 100ms:  ${h['p(99)'] <= 100 ? 'PASS' : 'FAIL'} (actual: ${fmtMs(h['p(99)'])})
  GET /projects      p99 < 500ms:  ${pl['p(99)'] <= 500 ? 'PASS' : 'FAIL'} (actual: ${fmtMs(pl['p(99)'])})
  GET /builds/{id}   p99 < 200ms:  ${bg['p(99)'] <= 200 ? 'PASS' : 'FAIL'} (actual: ${fmtMs(bg['p(99)'])})
  GET /environments  p99 < 500ms:  ${el['p(99)'] <= 500 ? 'PASS' : 'FAIL'} (actual: ${fmtMs(el['p(99)'])})
  POST /projects     p99 < 1000ms: ${pc['p(99)'] <= 1000 ? 'PASS' : 'FAIL'} (actual: ${fmtMs(pc['p(99)'])})

  HTTP failure rate: ${((m.http_req_failed || {}).values || {}).rate ? ((m.http_req_failed.values.rate) * 100).toFixed(2) : '0.00'}%
  Total requests:    ${((m.http_reqs || {}).values || {}).count || 0}
\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550
`;
}
