import { randomUUID } from 'crypto';
import { getDefaultApiUrl, API_REQUEST_TIMEOUT_MS } from '../config/constants';

type FetchFn = (url: string, init?: RequestInit) => Promise<Response>;

/** HTTP status codes that are safe to retry (server-side transient errors). */
const RETRYABLE_STATUS_CODES = new Set([502, 503, 504]);

/** Max retry attempts for transient errors. */
const MAX_RETRIES = 3;

/** Base delay between retries in ms (doubles each attempt: 1s, 2s, 4s). */
const RETRY_BASE_DELAY_MS = 1000;

/** CLI version for User-Agent header. */
const CLI_VERSION = '0.1.0';

export interface ApiClientOptions {
  baseURL?: string;
  token?: string | null;
  fetchFn?: FetchFn;
  /** Max retry attempts for transient errors. Default: 3. Set to 0 to disable. */
  maxRetries?: number;
}

export class ApiError extends Error {
  public readonly exitCode: number;
  public readonly statusCode: number;
  public readonly code: string;

  constructor(message: string, exitCode: number, statusCode: number, code?: string) {
    super(message);
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

export class ApiClient {
  private readonly baseURL: string;
  private readonly token: string | null;
  private readonly fetchFn: FetchFn;
  private readonly maxRetries: number;

  constructor(opts: ApiClientOptions = {}) {
    this.baseURL = opts.baseURL ?? getDefaultApiUrl();
    this.token = opts.token ?? null;
    this.fetchFn = opts.fetchFn ?? globalThis.fetch;
    this.maxRetries = opts.maxRetries ?? MAX_RETRIES;
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

  async del<T = unknown>(path: string): Promise<T> {
    return this.request<T>('DELETE', path);
  }

  private async request<T>(
    method: string,
    path: string,
    body?: Record<string, unknown>,
    extraHeaders?: Record<string, string>
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

    const url = `${this.baseURL}${path}`;
    const bodyStr = body !== undefined ? JSON.stringify(body) : undefined;

    let lastError: ApiError | Error | undefined;

    for (let attempt = 0; attempt <= this.maxRetries; attempt++) {
      if (attempt > 0) {
        const delay = RETRY_BASE_DELAY_MS * Math.pow(2, attempt - 1);
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

          const apiError = this.mapError(res.status, errorBody);

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
    body?: { code?: string; message?: string } | null,
  ): ApiError {
    return ApiError.fromHttpStatus(status, body?.message, body?.code);
  }
}
