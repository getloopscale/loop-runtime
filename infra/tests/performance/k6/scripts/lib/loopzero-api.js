// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Shared LoopZero API client for k6 load tests.
//
// Why a shared lib:
//   - Every script goes through the same endpoints (create project,
//     upload source, submit build, poll status). Duplicating the
//     request shape per script drifts quickly.
//   - Error classification is non-trivial (429 = admission reject,
//     409 = deploy in progress, 402 = plan limit). Each caller needs
//     the same mapping or the thresholds misfire.
//   - Headers follow the service-auth bypass pattern documented in
//     infra/tests/load/run-l1.sh. Centralizing them prevents typos.
//
// Contract:
//   - Every exported function returns `{ ok, status, body, error }`
//     where ok is a boolean and body is the decoded JSON (or null).
//     Callers should NOT throw on non-2xx — they should track the
//     result via k6 Counter / Trend metrics.
//
// Usage:
//   import { createProject, submitBuild, getBuildStatus } from './lib/loopzero-api.js';
//   const proj = createProject('tenant-1', 'app-1');
//   if (!proj.ok) { buildFailures.add(1); return; }

import http from 'k6/http';
import { check } from 'k6';
import { Counter, Trend, Rate } from 'k6/metrics';

/* ------------------------------------------------------------------ */
/* Configuration                                                       */
/* ------------------------------------------------------------------ */

// All config comes from env vars so the same script runs locally
// against api.dev.getloopscale.com and in-cluster against the service DNS.
export const API_URL = __ENV.K6_API_URL || 'http://loopzero-api.platform-system.svc.cluster.local:8080';
export const SVC_AUTH = __ENV.K6_SVC_AUTH || 'dev-service-auth-secret-default';
export const APP_DOMAIN = __ENV.K6_APP_DOMAIN || 'dev.getloopscale.com';

/* ------------------------------------------------------------------ */
/* Custom metrics                                                      */
/* ------------------------------------------------------------------ */

// These metrics are referenced in the thresholds block of each script.
// Keep the names stable across scripts so Grafana queries work.

export const buildSubmitSuccess = new Rate('loopzero_build_submit_success');
export const buildSucceeded = new Rate('loopzero_build_succeeded');
export const projectCreated = new Rate('loopzero_project_created');

export const uploadDuration = new Trend('loopzero_upload_duration_ms', true);
export const buildSubmitDuration = new Trend('loopzero_build_submit_duration_ms', true);
export const deployDuration = new Trend('loopzero_deploy_duration_ms', true);

export const appVerified = new Rate('loopzero_app_verified');
export const appVerifyDuration = new Trend('loopzero_app_verify_duration_ms', true);

export const admissionReject = new Counter('loopzero_admission_reject_total');
export const planLimitReject = new Counter('loopzero_plan_limit_reject_total');
export const lockConflict = new Counter('loopzero_lock_conflict_total');

/* ------------------------------------------------------------------ */
/* Headers                                                             */
/* ------------------------------------------------------------------ */

function authHeaders(tenantID, userID, email) {
  return {
    'X-Service-Auth': SVC_AUTH,
    'X-LoopZero-User-ID': userID,
    'X-LoopZero-Tenant': tenantID,
    'X-LoopZero-Email': email,
    'Content-Type': 'application/json',
  };
}

/* ------------------------------------------------------------------ */
/* Project lifecycle                                                   */
/* ------------------------------------------------------------------ */

export function createProject(tenantID, projectName, userID, email) {
  const body = JSON.stringify({ name: projectName });
  const res = http.post(`${API_URL}/api/v1/projects`, body, {
    headers: authHeaders(tenantID, userID, email),
    tags: { endpoint: 'create_project' },
  });

  const ok = res.status === 201 || res.status === 200;
  projectCreated.add(ok ? 1 : 0);

  if (res.status === 402) {
    planLimitReject.add(1);
  }

  return {
    ok,
    status: res.status,
    body: safeJSON(res.body),
    error: ok ? null : `HTTP ${res.status}: ${res.body}`,
  };
}

/* ------------------------------------------------------------------ */
/* Source upload + build submission                                    */
/* ------------------------------------------------------------------ */

export function uploadSource(tenantID, projectName, bundleBytes, userID, email) {
  const loopzeroYaml = `version: 2
project: ${projectName}
apps:
  api:
    build: buildpack
    framework: node
    render: server
    port: 8080
`;

  // k6's multipart helper.
  const body = {
    bundle: http.file(bundleBytes, 'loadtest.bundle', 'application/octet-stream'),
    loopzero_yaml: loopzeroYaml,
  };

  const headers = authHeaders(tenantID, userID, email);
  delete headers['Content-Type']; // let k6 set the multipart boundary

  const start = Date.now();
  const res = http.post(`${API_URL}/api/v1/source/upload`, body, {
    headers,
    tags: { endpoint: 'source_upload' },
  });
  uploadDuration.add(Date.now() - start);

  const parsed = safeJSON(res.body);
  const ok = res.status === 200 && parsed && parsed.commit_sha;
  return {
    ok,
    status: res.status,
    body: parsed,
    commitSHA: ok ? parsed.commit_sha : null,
    error: ok ? null : `HTTP ${res.status}: ${res.body}`,
  };
}

export function submitBuild(tenantID, projectName, commitSHA, idempotencyKey, userID, email) {
  const body = JSON.stringify({
    project_id: projectName,
    environment: 'production',
    commit_sha: commitSHA,
    apps: [{
      name: 'api',
      build: 'buildpack',
      framework: 'node',
      render: 'server',
      port: 8080,
    }],
  });

  const headers = authHeaders(tenantID, userID, email);
  headers['Idempotency-Key'] = idempotencyKey;

  const start = Date.now();
  const res = http.post(`${API_URL}/api/v1/builds`, body, {
    headers,
    tags: { endpoint: 'submit_build' },
  });
  buildSubmitDuration.add(Date.now() - start);

  const parsed = safeJSON(res.body);
  const ok = res.status === 202 && parsed && parsed.build_id;
  buildSubmitSuccess.add(ok ? 1 : 0);

  if (res.status === 429) {
    admissionReject.add(1);
  } else if (res.status === 409) {
    lockConflict.add(1);
  }

  return {
    ok,
    status: res.status,
    body: parsed,
    buildID: ok ? parsed.build_id : null,
    retryAfter: res.headers['Retry-After'] || null,
    error: ok ? null : `HTTP ${res.status}: ${res.body}`,
  };
}

/* ------------------------------------------------------------------ */
/* Polling                                                             */
/* ------------------------------------------------------------------ */

export function getBuildStatus(tenantID, buildID, userID, email) {
  const res = http.get(`${API_URL}/api/v1/builds/${buildID}`, {
    headers: authHeaders(tenantID, userID, email),
    tags: { endpoint: 'get_build' },
  });

  const parsed = safeJSON(res.body);
  return {
    ok: res.status === 200,
    status: res.status,
    buildStatus: parsed ? parsed.status : 'UNKNOWN',
    duration: parsed ? parsed.duration_seconds : null,
  };
}

/**
 * Polls a build until terminal state (SUCCEEDED, FAILED, CANCELLED) or
 * the timeout is reached. Emits deployDuration on success.
 *
 * @param {number} timeoutMs - wall clock budget for this build
 * @param {number} pollIntervalMs - how often to poll (default 20s)
 * @returns {{ ok: boolean, terminalState: string, durationMs: number }}
 */
export function pollBuildUntilDone(tenantID, buildID, userID, email, timeoutMs, pollIntervalMs) {
  timeoutMs = timeoutMs || 20 * 60 * 1000; // 20min default
  pollIntervalMs = pollIntervalMs || 20 * 1000;

  const start = Date.now();
  let state = 'UNKNOWN';
  while (Date.now() - start < timeoutMs) {
    const r = getBuildStatus(tenantID, buildID, userID, email);
    state = r.buildStatus;
    if (state === 'SUCCEEDED' || state === 'FAILED' || state === 'CANCELLED') {
      break;
    }
    // Sleep — k6 provides `sleep` from k6 runtime.
    sleepSec(pollIntervalMs / 1000);
  }

  const elapsed = Date.now() - start;
  const ok = state === 'SUCCEEDED';
  buildSucceeded.add(ok ? 1 : 0);
  if (ok) {
    deployDuration.add(elapsed);
  }
  return { ok, terminalState: state, durationMs: elapsed };
}

/* ------------------------------------------------------------------ */
/* App verification (scale-50k-plan requirement)                      */
/* ------------------------------------------------------------------ */

/**
 * Constructs the expected app URL for a deployed tenant.
 * Pattern: https://{tenantID}-{projectName}-{appName}-{env}.{domain}
 */
export function appURL(tenantID, projectName, appName, env) {
  appName = appName || 'api';
  env = env || 'production';
  return `https://${tenantID}-${projectName}-${appName}-${env}.${APP_DOMAIN}`;
}

/**
 * Verifies a deployed app is reachable by polling its URL until HTTP 200
 * or timeout. This is the critical "dogfood guarantee" from scale-50k-plan:
 * if the app URL doesn't return 200, the deploy is NOT successful.
 *
 * Polls every 10s for up to 120s (ArgoCD sync + pod scheduling + TLS).
 *
 * @param {string} url - The full app URL to check
 * @param {number} [timeoutMs=120000] - Max time to wait
 * @returns {{ ok: boolean, statusCode: number, durationMs: number }}
 */
export function verifyAppLive(url, timeoutMs) {
  timeoutMs = timeoutMs || 120 * 1000;
  const start = Date.now();

  let lastStatus = 0;
  while (Date.now() - start < timeoutMs) {
    try {
      const res = http.get(url, {
        tags: { endpoint: 'app_verify' },
        timeout: '10s',
      });
      lastStatus = res.status;
      if (res.status === 200) {
        const elapsed = Date.now() - start;
        appVerified.add(1);
        appVerifyDuration.add(elapsed);
        return { ok: true, statusCode: 200, durationMs: elapsed };
      }
    } catch (_) {
      // Connection refused, DNS not ready, TLS not provisioned yet
    }
    sleepSec(10);
  }

  const elapsed = Date.now() - start;
  appVerified.add(0);
  appVerifyDuration.add(elapsed);
  return { ok: false, statusCode: lastStatus, durationMs: elapsed };
}

/* ------------------------------------------------------------------ */
/* Health / readiness                                                  */
/* ------------------------------------------------------------------ */

export function healthCheck() {
  const res = http.get(`${API_URL}/api/v1/health`, {
    tags: { endpoint: 'health' },
  });
  return check(res, { 'health 200': (r) => r.status === 200 });
}

/* ------------------------------------------------------------------ */
/* Internal helpers                                                    */
/* ------------------------------------------------------------------ */

function safeJSON(body) {
  try {
    return JSON.parse(body);
  } catch (_) {
    return null;
  }
}

// k6 exposes `sleep` only from the k6 module, which we can import
// locally in the caller. We re-export it through a wrapper so the
// poll loop is self-contained.
import { sleep as k6Sleep } from 'k6';
function sleepSec(s) { k6Sleep(s); }
