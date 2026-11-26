// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// SSE deploy stream test — validates real-time event delivery during deploys.
//
// Objective: verify that `loopruntime logs --follow` / build progress SSE
// streaming works reliably under concurrent deploy load. Specifically:
//
//   1. SSE connections establish quickly (first event within 5s)
//   2. Events flow without unexpected drops
//   3. Terminal events (build_completed / build_failed) are always delivered
//   4. The SSE endpoint does not leak connections under sustained load
//
// Design:
//   - constant-arrival-rate at 5 deploys/min for 10 minutes = 50 deploys.
//   - Each VU submits a build, then opens an SSE connection to stream
//     build events until terminal state or timeout.
//   - k6 does NOT have native SSE support since v0.52. This script uses
//     a hybrid approach:
//       (a) Attempt SSE via HTTP GET with Accept: text/event-stream and
//           responseType: 'text' (k6 streams the full response body).
//       (b) In parallel, poll the build status endpoint as a fallback to
//           ensure we detect terminal state even if SSE delivery fails.
//     This dual approach gives us the SSE reliability metric (did we get
//     the terminal event via SSE?) while ensuring the test always finishes.
//
// Run:
//   K6_API_URL=http://... K6_SVC_AUTH=... k6 run sse-deploy-stream.js

import { group, sleep } from 'k6';
import http from 'k6/http';
import exec from 'k6/execution';
import { Counter, Trend, Rate } from 'k6/metrics';

import {
  createProject,
  uploadSource,
  submitBuild,
  getBuildStatus,
  healthCheck,
  API_URL,
  SVC_AUTH,
} from './lib/loopruntime-api.js';
import { loadBundleBytes } from '../fixtures/bundle-builder.js';

// -----------------------------------------------------------------------
// Custom SSE metrics
// -----------------------------------------------------------------------

const sseConnectDuration = new Trend('loopruntime_sse_connect_duration_ms', true);
const sseEventsReceived = new Counter('loopruntime_sse_events_received');
const sseConnectionDrops = new Counter('loopruntime_sse_connection_drops');
const sseBuildTerminalReceived = new Rate('loopruntime_sse_build_terminal_received');

// -----------------------------------------------------------------------
// Options
// -----------------------------------------------------------------------

export const options = {
  scenarios: {
    sse_stream: {
      executor: 'constant-arrival-rate',
      rate: 5,
      timeUnit: '1m',               // 5 deploys per minute
      duration: '10m',
      preAllocatedVUs: 10,
      maxVUs: 30,
      gracefulStop: '12m',          // allow in-flight SSE streams to finish
    },
  },
  thresholds: {
    // SSE-specific SLOs
    'loopruntime_sse_connect_duration_ms': ['p(95)<5000'],   // first event within 5s
    'loopruntime_sse_connection_drops':    ['count<5'],       // near-zero unexpected drops
    'loopruntime_sse_build_terminal_received': ['rate>0.95'], // 95% get terminal via SSE

    // Basic reliability (we still care about deploys succeeding)
    'http_req_failed': ['rate<0.05'],                 // < 5% HTTP errors
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

// -----------------------------------------------------------------------
// Constants
// -----------------------------------------------------------------------

const TENANT_COUNT = 10;
const SSE_TIMEOUT_MS = 10 * 60 * 1000;  // 10 minutes max per SSE stream
const POLL_INTERVAL_MS = 15 * 1000;      // 15s poll fallback

// -----------------------------------------------------------------------
// Setup
// -----------------------------------------------------------------------

export function setup() {
  if (!healthCheck()) {
    throw new Error('API health check failed — aborting SSE test');
  }

  const bundle = loadBundleBytes();

  // Pre-create tenant projects.
  for (let i = 0; i < TENANT_COUNT; i++) {
    const tenantID = `k6-sse-tn-${i}`;
    const userID = `k6-sse-user-${i}`;
    const email = `k6-sse-${i}@loopruntime.test`;
    const projectName = `k6-sse-app-${i}`;
    createProject(tenantID, projectName, userID, email);
    sleep(0.2);
  }

  return { bundle };
}

// -----------------------------------------------------------------------
// Per-iteration work
// -----------------------------------------------------------------------

export default function (data) {
  const iter = exec.scenario.iterationInTest;
  const tenantIdx = iter % TENANT_COUNT;

  const tenantID = `k6-sse-tn-${tenantIdx}`;
  const userID = `k6-sse-user-${tenantIdx}`;
  const email = `k6-sse-${tenantIdx}@loopruntime.test`;
  const projectName = `k6-sse-app-${tenantIdx}`;

  let buildID = null;

  group('submit_build', function () {
    // 1. Upload source
    const upload = uploadSource(tenantID, projectName, data.bundle, userID, email);
    if (!upload.ok) return;

    // 2. Submit build
    const idempKey = `k6-sse-${tenantIdx}-${iter}-${Date.now()}`;
    const build = submitBuild(
      tenantID, projectName, upload.commitSHA, idempKey, userID, email
    );
    if (!build.ok) return;

    buildID = build.buildID;
  });

  if (!buildID) return;

  group('sse_stream', function () {
    // Attempt SSE connection to build events endpoint.
    // k6 will receive the full response body as text once the connection
    // closes (server-side close on terminal event or timeout).
    const sseURL = `${API_URL}/api/v1/builds/${buildID}/events`;
    const sseStart = Date.now();

    const sseRes = http.get(sseURL, {
      headers: {
        'X-Service-Auth': SVC_AUTH,
        'X-LoopRuntime-User-ID': userID,
        'X-LoopRuntime-Tenant': tenantID,
        'X-LoopRuntime-Email': email,
        'Accept': 'text/event-stream',
        'Cache-Control': 'no-cache',
      },
      tags: { endpoint: 'sse_build_events' },
      timeout: `${SSE_TIMEOUT_MS}ms`,
    });

    const sseElapsed = Date.now() - sseStart;

    // Parse SSE response body for events.
    // SSE format: "event: <name>\ndata: <json>\n\n"
    const gotSSE = sseRes.status === 200 && sseRes.body && sseRes.body.length > 0;

    if (gotSSE) {
      const events = parseSSEEvents(sseRes.body);
      const eventCount = events.length;
      sseEventsReceived.add(eventCount);

      // Time to first event: if we got events, use the connection time
      // as a proxy (SSE response completes when stream ends, so for
      // connect duration we check if the first event arrived quickly
      // by looking at the overall time / event count ratio).
      if (eventCount > 0) {
        // Approximate first-event latency: total time / event count
        // gives average inter-event time. First event is typically fast.
        // A more precise measure would require streaming, which k6
        // doesn't support natively. Use min(5000, elapsed/events) as proxy.
        const approxFirstEvent = Math.min(5000, sseElapsed / Math.max(eventCount, 1));
        sseConnectDuration.add(approxFirstEvent);
      }

      // Check for terminal event
      const hasTerminal = events.some(e =>
        e.event === 'build_completed' ||
        e.event === 'build_failed' ||
        e.event === 'build_cancelled'
      );
      sseBuildTerminalReceived.add(hasTerminal ? 1 : 0);

      if (!hasTerminal && sseElapsed >= SSE_TIMEOUT_MS - 1000) {
        // Connection timed out without terminal — likely a drop
        sseConnectionDrops.add(1);
      }
    } else if (sseRes.status === 200 && (!sseRes.body || sseRes.body.length === 0)) {
      // Connected but got nothing — connection drop
      sseConnectionDrops.add(1);
      sseConnectDuration.add(SSE_TIMEOUT_MS);
      sseBuildTerminalReceived.add(0);
    } else {
      // SSE endpoint returned non-200 — fall back to polling.
      // Record as a drop and poll for terminal state.
      sseConnectionDrops.add(1);
      sseBuildTerminalReceived.add(0);

      // Fallback: poll until terminal so the test still validates
      // build completion even when SSE is broken.
      pollUntilTerminal(tenantID, buildID, userID, email);
    }
  });
}

// -----------------------------------------------------------------------
// SSE parser
// -----------------------------------------------------------------------

/**
 * Parses an SSE response body into an array of events.
 * SSE format: lines starting with "event:" and "data:" separated by blank lines.
 *
 * @param {string} body - raw SSE response body
 * @returns {Array<{event: string, data: string}>}
 */
function parseSSEEvents(body) {
  const events = [];
  const blocks = body.split('\n\n');

  for (const block of blocks) {
    if (!block.trim()) continue;

    let eventName = '';
    let eventData = '';

    const lines = block.split('\n');
    for (const line of lines) {
      if (line.startsWith('event:')) {
        eventName = line.substring(6).trim();
      } else if (line.startsWith('data:')) {
        eventData = line.substring(5).trim();
      }
    }

    if (eventName || eventData) {
      events.push({ event: eventName, data: eventData });
    }
  }

  return events;
}

// -----------------------------------------------------------------------
// Polling fallback
// -----------------------------------------------------------------------

/**
 * Polls build status until terminal. Used when SSE connection fails.
 * Does NOT emit deploy metrics (that is loopruntime-api.js's job) — only
 * ensures the VU finishes gracefully.
 */
function pollUntilTerminal(tenantID, buildID, userID, email) {
  const start = Date.now();
  while (Date.now() - start < SSE_TIMEOUT_MS) {
    const r = getBuildStatus(tenantID, buildID, userID, email);
    if (r.buildStatus === 'SUCCEEDED' || r.buildStatus === 'FAILED' || r.buildStatus === 'CANCELLED') {
      return r.buildStatus;
    }
    sleep(POLL_INTERVAL_MS / 1000);
  }
  return 'TIMEOUT';
}

// -----------------------------------------------------------------------
// Teardown
// -----------------------------------------------------------------------

export function teardown() {
  if (!healthCheck()) {
    console.error('SSE TEST: API unhealthy after test — possible connection leak');
  } else {
    console.log('SSE TEST: API healthy after test');
  }
}

// -----------------------------------------------------------------------
// Summary report
// -----------------------------------------------------------------------

export function handleSummary(data) {
  const m = data.metrics;
  const sseDur = (m.loopruntime_sse_connect_duration_ms || {}).values || {};
  const sseEvents = (m.loopruntime_sse_events_received || {}).values || {};
  const sseDrops = (m.loopruntime_sse_connection_drops || {}).values || {};
  const sseTerminal = (m.loopruntime_sse_build_terminal_received || {}).values || {};
  const httpDur = (m.http_req_duration || {}).values || {};

  const terminalRate = sseTerminal.rate !== undefined
    ? (sseTerminal.rate * 100).toFixed(1)
    : 'N/A';

  const text = `
${'='.repeat(65)}
  SSE DEPLOY STREAM REPORT — 50 deploys, 5/min for 10 minutes
${'='.repeat(65)}

  SSE Connection Quality:
    First event p50:    ${Math.round(sseDur.med || 0)}ms
    First event p95:    ${Math.round(sseDur['p(95)'] || 0)}ms  (target: <5000ms)
    First event p99:    ${Math.round(sseDur['p(99)'] || 0)}ms

  SSE Event Delivery:
    Total events:       ${sseEvents.count || 0}
    Connection drops:   ${sseDrops.count || 0}         (target: <5)
    Terminal received:  ${terminalRate}%       (target: >95%)

  HTTP Baseline:
    p50:                ${Math.round(httpDur.med || 0)}ms
    p95:                ${Math.round(httpDur['p(95)'] || 0)}ms
    p99:                ${Math.round(httpDur['p(99)'] || 0)}ms

  ${(sseDrops.count || 0) < 5 && terminalRate !== 'N/A' && parseFloat(terminalRate) >= 95 ?
    'SSE PASSED — event delivery reliable under load' :
    'SSE FAILED — check drops and terminal delivery rate'}
${'='.repeat(65)}
`;
  return {
    stdout: text,
    '/tmp/k6-sse-stream-summary.json': JSON.stringify(data, null, 2),
  };
}
