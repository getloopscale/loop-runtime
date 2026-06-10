// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { randomUUID } from 'crypto';
import { getDefaultApiUrl, API_REQUEST_TIMEOUT_MS } from '../config/constants';
import { injectTraceContext } from '../tracing/propagation';

type FetchFn = (url: string, init?: RequestInit) => Promise<Response>;

/**
 * HTTP status codes that are safe to retry (server-side transient errors).
 * T-06: Added 429 (CELL_AT_CAPACITY from admission gate) — the platform is
 * advertising backpressure, not rejecting permanently. CLI should retry with
 * exponential backoff instead of exiting 3.
 */
const RETRYABLE_STATUS_CODES = new Set([429, 502, 503, 504]);

/**
 * Max retry attempts for transient errors. T-06: bumped 3→5 so backoff ends
 * at 16s before giving up (covers full admission gate queue drain cycle).
 */
const MAX_RETRIES = 5;

/** Base delay between retries in ms (doubles each attempt: 1s, 2s, 4s, 8s, 16s). */
const RETRY_BASE_DELAY_MS = 1000;

/** Jitter ±10% added to backoff to avoid thundering-herd retry storms. */
const RETRY_JITTER_PCT = 0.1;

/** CLI version for User-Agent header. */
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { version: CLI_VERSION } = require('../../package.json');

export interface ApiClientOptions {
  baseURL?: string;
  token?: string | null;
  fetchFn?: FetchFn;
  /** Max retry attempts for transient errors. Default: 3. Set to 0 to disable. */
  maxRetries?: number;
  /**
   * Called when a 401 is received. Should attempt token refresh and return
   * a new access token, or null if refresh fails. The request will be
   * retried once with the new token.
   */
  onUnauthorized?: () => Promise<string | null>;
}

export class ApiError extends Error {
  public readonly exitCode: number;
  public readonly statusCode: number;
  public readonly code: string;

  /**
   * Plan v1.2 T5.1 (M4) — preserve the underlying error chain so verbose
   * mode (`LOOPZERO_DEBUG=1`) can render the root cause without losing
   * stack/context. Mirrors the Error.cause shape introduced in Node 16.9.
   */
  constructor(message: string, exitCode: number, statusCode: number, code?: string, cause?: unknown) {
    super(message, cause !== undefined ? { cause } : undefined);
    this.name = 'ApiError';
    this.exitCode = exitCode;
    this.statusCode = statusCode;
    this.code = code ?? 'API_ERROR';
  }

  /**
   * Single source of truth for HTTP status → exit code + message mapping.
   * Used by both ApiClient and source-upload adapter to avoid divergence.
   */
  static fromHttpStatus(status: number, serverMessage?: string | null, serverCode?: string): ApiError {
    if (status === 401) {
      return new ApiError(
        serverMessage || 'Not authenticated. Run: loopzero login',
        4, status, serverCode ?? 'AUTH_REQUIRED',
      );
    }
    if (status === 403) {
      return new ApiError(
        serverMessage || 'Permission denied. Check your account access.',
        4, status, serverCode ?? 'PERMISSION_DENIED',
      );
    }
    if (status === 402) {
      return new ApiError(
        serverMessage || 'Plan limit reached. Upgrade your plan to continue.',
        3, status, serverCode ?? 'PLAN_LIMIT_EXCEEDED',
      );
    }
    if (status === 409) {
      return new ApiError(
        serverMessage
          ? `${serverMessage} Wait for it to complete, or try again later.`
          : 'A deploy is already in progress for this environment. Wait for it to complete, or try again later.',
        3, status, serverCode ?? 'DEPLOY_IN_PROGRESS',
      );
    }
    if (status === 422) {
      return new ApiError(
        serverMessage || 'Invalid request. Check your command arguments.',
        1, status, serverCode ?? 'VALIDATION_FAILED',
      );
    }
    if (status >= 500) {
      if (serverCode === 'AUTH_SERVICE_UNAVAILABLE') {
        return new ApiError(
          serverMessage || 'Authentication service is temporarily unavailable. Try again in a moment.',
          4, status, 'AUTH_SERVICE_UNAVAILABLE',
        );
      }
      // Sprint 8 v3.2 / CHG-002a / D-7 — explicit MAINTENANCE_MODE handling.
      // Distinct from generic 503 because the CLI should NOT silently retry
      // (operator-initiated, can last minutes for PG resize). Show the
      // operator's message verbatim so users understand the planned downtime.
      if (serverCode === 'MAINTENANCE_MODE') {
        return new ApiError(
          serverMessage || 'LoopZero is in maintenance mode. Mutations are temporarily disabled.',
          3, status, 'MAINTENANCE_MODE',
        );
      }
      return new ApiError(
        serverMessage || 'Service unavailable. Try again in a moment.',
        3, status, serverCode ?? 'SERVICE_UNAVAILABLE',
      );
    }
    if (status >= 400 && status < 500) {
      return new ApiError(
        serverMessage || `Request failed (HTTP ${status}). Check your command arguments.`,
        1, status, serverCode ?? 'API_ERROR',
      );
    }
    return new ApiError(
      serverMessage || `Request failed (HTTP ${status}).`,
      3, status, serverCode ?? 'API_ERROR',
    );
  }
}

/**
 * ApiClient — low-level HTTP client used by all LoopZero CLI commands.
 *
 * Retry policy (T-06 from docs/plans/50k-immediate-focus.md):
 *   - HTTP 429 (CELL_AT_CAPACITY backpressure from admission gate)
 *   - HTTP 502 (bad gateway, transient)
 *   - HTTP 503 (SERVICE_UNAVAILABLE, circuit breaker)
 *   - HTTP 504 (gateway timeout)
 *   - Network errors: ECONNRESET, ECONNREFUSED, fetch failed, TimeoutError
 *
 * Backoff: 1s, 2s, 4s, 8s, 16s (exponential × 2) with ±10% jitter to avoid
 * thundering-herd retry storms. maxRetries=5 default (configurable via opts).
 *
 * The platform advertises backpressure with 429/503 precisely because these
 * are *recoverable* — the CLI should wait and retry, not exit. Only after
 * all 5 retries exhausted does the CLI exit with code 3 (deploy failure).
 *
 * For non-retryable client errors (400, 401, 403, 404, 409, 422), the error
 * is thrown immediately — no backoff. Exit codes follow spec mapping at
 * mapError() above.
 */
export class ApiClient {
  private readonly baseURL: string;
  private token: string | null;
  private readonly fetchFn: FetchFn;
  private readonly maxRetries: number;
  private readonly onUnauthorized?: () => Promise<string | null>;

  constructor(opts: ApiClientOptions = {}) {
    this.baseURL = opts.baseURL ?? getDefaultApiUrl();
    this.token = opts.token ?? null;
    this.fetchFn = opts.fetchFn ?? globalThis.fetch;
    this.maxRetries = opts.maxRetries ?? MAX_RETRIES;
    this.onUnauthorized = opts.onUnauthorized;
  }

  async get<T = unknown>(path: string): Promise<T> {
    return this.request<T>('GET', path);
  }

  async post<T = unknown>(
    path: string,
    body: Record<string, unknown>,
    extraHeaders?: Record<string, string>
  ): Promise<T> {
    return this.request<T>('POST', path, body, extraHeaders);
  }

  async patch<T = unknown>(
    path: string,
    body: Record<string, unknown>,
  ): Promise<T> {
    return this.request<T>('PATCH', path, body);
  }

  async del<T = unknown>(path: string): Promise<T> {
    return this.request<T>('DELETE', path);
  }

  private async request<T>(
    method: string,
    path: string,
    body?: Record<string, unknown>,
    extraHeaders?: Record<string, string>,
    isRetryAfterRefresh = false,
  ): Promise<T> {
    const headers: Record<string, string> = {
      'Correlation-ID': randomUUID(),
      'User-Agent': `loopzero-cli/${CLI_VERSION}`,
      ...extraHeaders,
    };

    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`;
    }

    if (body !== undefined) {
      headers['Content-Type'] = 'application/json';
    }

    // Inject W3C TraceContext so CLI spans become parent of API spans.
    injectTraceContext(headers);

    const url = `${this.baseURL}${path}`;
    const bodyStr = body !== undefined ? JSON.stringify(body) : undefined;

    let lastError: ApiError | Error | undefined;

    for (let attempt = 0; attempt <= this.maxRetries; attempt++) {
      if (attempt > 0) {
        // T-06: exponential backoff with ±10% jitter to avoid thundering-herd.
        // Attempts 1..5 → 1s, 2s, 4s, 8s, 16s (±10%).
        const base = RETRY_BASE_DELAY_MS * Math.pow(2, attempt - 1);
        const jitter = base * RETRY_JITTER_PCT * (Math.random() * 2 - 1);
        const delay = Math.floor(base + jitter);
        await new Promise((resolve) => setTimeout(resolve, delay));
      }

      try {
        const init: RequestInit = {
          method,
          headers,
          body: bodyStr,
          signal: AbortSignal.timeout(API_REQUEST_TIMEOUT_MS),
        };

        const res = await this.fetchFn(url, init);

        if (!res.ok) {
          let errorBody: { code?: string; message?: string } | null = null;
          try {
            errorBody = (await res.json()) as { code?: string; message?: string };
          } catch {
            /* ignore parse errors */
          }

          // 401 + onUnauthorized callback → attempt token refresh once
          if (res.status === 401 && this.onUnauthorized && !isRetryAfterRefresh) {
            const newToken = await this.onUnauthorized();
            if (newToken) {
              this.token = newToken;
              return this.request<T>(method, path, body, extraHeaders, true);
            }
          }

          const apiError = this.mapError(res.status, errorBody);

          // Sprint 8 v3.2 / CHG-002a / D-7: MAINTENANCE_MODE is operator-initiated
          // and can persist for minutes during PG resize / mgmt rolling upgrade.
          // Skip the retry loop — surface the message immediately so the user
          // understands the planned downtime instead of seeing 5 silent retries.
          if (apiError.code === 'MAINTENANCE_MODE') {
            throw apiError;
          }

          // Only retry on transient server errors (502, 503, 504)
          if (RETRYABLE_STATUS_CODES.has(res.status) && attempt < this.maxRetries) {
            lastError = apiError;
            continue;
          }

          throw apiError;
        }

        if (res.status === 204) {
          return undefined as T;
        }

        return (await res.json()) as T;
      } catch (err) {
        if (err instanceof ApiError) {
          // Already mapped — if not retryable, throw immediately
          if (!RETRYABLE_STATUS_CODES.has(err.statusCode)) {
            throw err;
          }
          lastError = err;
          if (attempt >= this.maxRetries) {
            throw err;
          }
          continue;
        }

        // Network errors (ECONNRESET, ECONNREFUSED, etc.) — retry
        const isNetworkError = err instanceof Error && (
          err.message.includes('ECONNRESET') ||
          err.message.includes('ECONNREFUSED') ||
          err.message.includes('fetch failed') ||
          err.name === 'TimeoutError'
        );

        if (isNetworkError && attempt < this.maxRetries) {
          lastError = err as Error;
          continue;
        }

        throw err;
      }
    }

    // Should not reach here, but safety net
    throw lastError ?? new ApiError('Request failed after retries.', 3, 0);
  }

  private mapError(
    status: number,
    body?: { code?: string; message?: string; error?: string } | null,
  ): ApiError {
    // Some legacy routes (e.g. /api/v1/domains) respond with {"error": "..."}
    // instead of the canonical {"code", "message"} shape. We accept either so
    // the CLI surfaces the specific server-side message instead of falling
    // back to "Request failed (HTTP 400). Check your command arguments." —
    // a Rule 8 (fail-clear) violation observed during dogfood iter 13.
    const message = body?.message ?? body?.error;
    return ApiError.fromHttpStatus(status, message, body?.code);
  }
}
