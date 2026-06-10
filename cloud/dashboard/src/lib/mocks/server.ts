// Mock HTTP server for /api/v1/* routes — Plan v1.0 T0.2.
//
// Extends the dev-mocks.ts pattern (window.fetch wrap) to cover the full
// LoopZero backend API surface (55+ endpoints). Per ADR D1, NOT MSW — adds zero
// bundle weight and matches the project's existing interception convention.
//
// Pattern:
//   1. Each domain handler file (lib/mocks/handlers/{domain}.ts) imports
//      `registerRoute` from here and calls it for each endpoint it owns.
//   2. `installMocks()` is called once at boot from main.tsx (only when
//      VITE_USE_MOCKS=true). It wraps window.fetch with a router that matches
//      registered routes and falls through to the original fetch otherwise.
//   3. Path params use `{name}` placeholder syntax — extracted to params
//      object passed to the handler.
//
// Invariants:
//   - Idempotent install (safe to call multiple times).
//   - External-origin requests (Stripe, GitHub OAuth) NEVER intercepted.
//   - Unmatched API routes fall through to original fetch (so backend can
//     respond if running).
//   - Handlers return real Response objects (status, headers, body) — no
//     custom envelope abstraction.

export type MockHandler = (
  req: Request,
  params: Record<string, string>,
) => Promise<Response>;

export type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';

interface Route {
  method: HttpMethod;
  pattern: RegExp;
  paramNames: string[];
  handler: MockHandler;
}

const routes: Route[] = [];

/** Convert a path template like `/api/v1/projects/{name}` to a regex that
 * matches and extracts the `{name}` segment. The regex anchors on `^` and `$`
 * so partial matches don't fire. */
function compilePattern(pathPattern: string): { regex: RegExp; paramNames: string[] } {
  const paramNames: string[] = [];
  const regexStr = pathPattern.replace(/\{(\w+)\}/g, (_, name) => {
    paramNames.push(name);
    return '([^/?]+)';
  });
  return { regex: new RegExp(`^${regexStr}$`), paramNames };
}

/** Register a mock handler for a given HTTP method + path pattern.
 * Called from domain handler files at module-load time.
 *
 * @example
 *   registerRoute('GET', '/api/v1/projects/{name}', async (_req, { name }) => {
 *     return Response.json({ name, apps: [] });
 *   });
 */
export function registerRoute(
  method: HttpMethod,
  pathPattern: string,
  handler: MockHandler,
): void {
  const { regex, paramNames } = compilePattern(pathPattern);
  routes.push({ method, pattern: regex, paramNames, handler });
}

/** Reset all registered routes. Used in tests to isolate suites. Production
 * code never calls this — handlers register at module load and stay forever. */
export function resetRoutes(): void {
  routes.length = 0;
}

/** Get count of registered routes — useful for tests asserting install
 * completeness without exposing the internal `routes` array. */
export function getRegisteredRouteCount(): number {
  return routes.length;
}

/** Single source of truth for "are API mocks active?". Mirrors mocksEnabled()
 * from dev-mocks.ts but keeps the modules decoupled. */
export function isMockingEnabled(): boolean {
  if (typeof window === 'undefined') return false;
  if (typeof import.meta === 'undefined') return false;
  return import.meta.env?.VITE_USE_MOCKS === 'true';
}

let originalFetch: typeof fetch | null = null;

/** Find a route matching the given method + pathname. Returns the route +
 * extracted params, or null if no match. Exported for testing — production
 * callers use installMocks() which calls this internally. */
export function matchRoute(
  method: HttpMethod,
  pathname: string,
): { route: Route; params: Record<string, string> } | null {
  for (const route of routes) {
    if (route.method !== method) continue;
    const match = pathname.match(route.pattern);
    if (!match) continue;
    const params: Record<string, string> = {};
    route.paramNames.forEach((name, i) => {
      params[name] = decodeURIComponent(match[i + 1]);
    });
    return { route, params };
  }
  return null;
}

/** Install the fetch interceptor. Safe to call multiple times (idempotent).
 * Only takes effect when VITE_USE_MOCKS=true. */
export function installMocks(): void {
  if (!isMockingEnabled()) return;
  if (typeof window === 'undefined') return;
  if (originalFetch !== null) return; // already installed

  originalFetch = window.fetch.bind(window);

  window.fetch = async (
    input: RequestInfo | URL,
    init?: RequestInit,
  ): Promise<Response> => {
    const url =
      typeof input === 'string'
        ? input
        : input instanceof URL
          ? input.href
          : input.url;

    // Parse URL — bail to original fetch on parse failure (malformed input)
    let parsed: URL;
    try {
      parsed = new URL(url, window.location.origin);
    } catch {
      return originalFetch!(input, init);
    }

    // Skip external origins — Stripe portal, GitHub OAuth, CDNs, etc.
    if (parsed.origin !== window.location.origin) {
      return originalFetch!(input, init);
    }

    const method = ((init?.method ?? 'GET').toUpperCase()) as HttpMethod;
    const pathname = parsed.pathname;

    // Strip /engine prefix that engineFetch prepends — backend endpoints
    // register without it; engineFetch is a dashboard convention for routing
    // to the LoopZero engine through a proxy.
    const normalized = pathname.replace(/^\/engine/, '');

    const matched = matchRoute(method, normalized);
    if (!matched) {
      return originalFetch!(input, init);
    }

    // Build a Request the handler can introspect
    const req = new Request(url, init);
    try {
      return await matched.route.handler(req, matched.params);
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Mock handler crashed';
      return Response.json(
        { code: 'INTERNAL_ERROR', message: msg },
        { status: 500 },
      );
    }
  };
}

/** Test-only: tear down the interceptor and restore original fetch. */
export function uninstallMocks(): void {
  if (typeof window === 'undefined') return;
  if (originalFetch === null) return;
  window.fetch = originalFetch;
  originalFetch = null;
}
