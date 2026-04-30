// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-30-INFRA-NORMALIZE / Phase 4 T4.1 — in-process mock API
// server. Pure node http (no Express dep) so we don't bloat package.json.
// Tests register canned responses per (method, path) before spawning the
// CLI; the CLI hits the server over real loopback and the test asserts
// both the request shape and the CLI behaviour.
//
// Why not nock? nock intercepts inside the test process — but our spawn
// helper forks a SEPARATE node process for the CLI. nock can't intercept
// across processes. Real loopback HTTP is the only reliable mock pattern
// here.

import * as http from 'http';
import { AddressInfo } from 'net';

export interface MockResponse {
  /** HTTP status code. */
  status: number;
  /** Response body. Object → JSON.stringify; string → as-is. */
  body?: unknown;
  /** Optional response headers. Content-Type defaults to application/json. */
  headers?: Record<string, string>;
}

export interface MockRoute {
  method: string;
  path: string;
  /** Static response OR function that receives the request body and returns one. */
  response: MockResponse | ((reqBody: string) => MockResponse);
}

export interface CapturedRequest {
  method: string;
  url: string;
  headers: http.IncomingHttpHeaders;
  body: string;
  timestamp: number;
}

export interface MockApiServer {
  /** Base URL e.g. http://127.0.0.1:54321 */
  url: string;
  /** Captured requests (in arrival order). */
  requests: CapturedRequest[];
  /** Register or replace a route. Last registration wins. */
  on(route: MockRoute): void;
  /** Stop the server (idempotent). */
  close(): Promise<void>;
}

/**
 * Start a mock HTTP server on a random port. Returns the server handle
 * with `url`, `requests[]`, `on(route)`, and `close()`.
 *
 * Call sequence in tests:
 *   const server = await startMockApi();
 *   server.on({method:'GET', path:'/api/v1/users/me',
 *              response:{status:200, body:{user_id:'u1'}}});
 *   const result = spawnLoopZero({args:[...], env:{LOOPZERO_URL: server.url}});
 *   // assertions on result + server.requests
 *   await server.close();
 */
export function startMockApi(): Promise<MockApiServer> {
  return new Promise((resolve, reject) => {
    const routes: MockRoute[] = [];
    const requests: CapturedRequest[] = [];

    const server = http.createServer((req, res) => {
      let body = '';
      req.on('data', (chunk: Buffer) => {
        body += chunk.toString('utf-8');
      });
      req.on('end', () => {
        const captured: CapturedRequest = {
          method: req.method ?? 'UNKNOWN',
          url: req.url ?? '/',
          headers: req.headers,
          body,
          timestamp: Date.now(),
        };
        requests.push(captured);

        const route = findRoute(routes, req.method ?? '', req.url ?? '');
        if (!route) {
          res.statusCode = 404;
          res.setHeader('Content-Type', 'application/json');
          res.end(JSON.stringify({
            code: 'not_found',
            message: `mock api: no route for ${req.method} ${req.url}`,
          }));
          return;
        }

        const resp = typeof route.response === 'function'
          ? route.response(body)
          : route.response;

        res.statusCode = resp.status;
        const headers = resp.headers ?? {};
        if (!headers['Content-Type'] && !headers['content-type']) {
          headers['Content-Type'] = 'application/json';
        }
        for (const [k, v] of Object.entries(headers)) {
          res.setHeader(k, v);
        }
        const out = typeof resp.body === 'string'
          ? resp.body
          : JSON.stringify(resp.body ?? {});
        res.end(out);
      });
    });

    server.on('error', (err) => {
      reject(err);
    });

    // Bind to 127.0.0.1:0 (kernel picks free port) for true isolation.
    server.listen(0, '127.0.0.1', () => {
      const addr = server.address() as AddressInfo;
      const url = `http://127.0.0.1:${addr.port}`;
      resolve({
        url,
        requests,
        on: (route: MockRoute) => {
          // Last registration wins — replace any existing same-key route.
          const idx = routes.findIndex(
            (r) => r.method === route.method && r.path === route.path,
          );
          if (idx >= 0) routes[idx] = route;
          else routes.push(route);
        },
        close: () =>
          new Promise<void>((res) => {
            server.close(() => res());
          }),
      });
    });
  });
}

/**
 * Match by exact (method, path). Path is the URL path part (no query).
 * Wildcards are NOT supported by design — tests are explicit.
 */
function findRoute(
  routes: MockRoute[],
  method: string,
  url: string,
): MockRoute | null {
  const path = url.split('?')[0];
  for (const r of routes) {
    if (r.method.toUpperCase() === method.toUpperCase() && r.path === path) {
      return r;
    }
  }
  return null;
}
