import { randomUUID } from 'crypto';

type FetchFn = (url: string, init?: RequestInit) => Promise<Response>;

export interface ApiClientOptions {
  baseURL?: string;
  token?: string | null;
  fetchFn?: FetchFn;
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
}

export class ApiClient {
  private readonly baseURL: string;
  private readonly token: string | null;
  private readonly fetchFn: FetchFn;

  constructor(opts: ApiClientOptions = {}) {
    this.baseURL =
      opts.baseURL ??
      process.env.LOOPZERO_API_URL ??
      'http://localhost:3001';
    this.token = opts.token ?? null;
    this.fetchFn = opts.fetchFn ?? globalThis.fetch;
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
      ...extraHeaders,
    };

    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`;
    }

    if (body !== undefined) {
      headers['Content-Type'] = 'application/json';
    }

    const url = `${this.baseURL}${path}`;
    const init: RequestInit = {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
      signal: AbortSignal.timeout(30_000),
    };

    const res = await this.fetchFn(url, init);

    if (!res.ok) {
      let errorBody: { code?: string; message?: string } | null = null;
      try {
        errorBody = (await res.json()) as { code?: string; message?: string };
      } catch {
        /* ignore parse errors */
      }
      throw this.mapError(res.status, errorBody);
    }

    if (res.status === 204) {
      return undefined as T;
    }

    return (await res.json()) as T;
  }

  private mapError(
    status: number,
    body?: { code?: string; message?: string } | null,
  ): ApiError {
    const serverMessage = body?.message ?? null;
    const serverCode = body?.code ?? undefined;

    switch (status) {
      case 401:
        return new ApiError(
          serverMessage || 'Not authenticated. Run: loopzero login',
          4,
          status,
          serverCode ?? 'AUTH_REQUIRED',
        );
      case 403:
        return new ApiError(
          serverMessage || 'Permission denied. Check your account access.',
          4,
          status,
          serverCode ?? 'PERMISSION_DENIED',
        );
      case 409:
        // Exit code 3 (deploy failure) is used for concurrent rejection per spec §0.3.
        return new ApiError(
          (serverMessage
            ? `${serverMessage} Wait for it to complete, or try again later.`
            : 'A deploy is already in progress for this environment. Wait for it to complete, or try again later.'),
          3,
          status,
          serverCode ?? 'DEPLOY_IN_PROGRESS',
        );
      case 422:
        return new ApiError(
          serverMessage || 'Invalid request. Check your command arguments.',
          1,
          status,
          serverCode ?? 'VALIDATION_FAILED',
        );
      default:
        if (status >= 500) {
          // F-04/F-06: Auth service errors are exit code 4 (auth), not 3 (deploy failure)
          if (serverCode === 'AUTH_SERVICE_UNAVAILABLE') {
            return new ApiError(
              serverMessage || 'Authentication service is temporarily unavailable. Try again in a moment.',
              4,
              status,
              'AUTH_SERVICE_UNAVAILABLE',
            );
          }
          return new ApiError(
            serverMessage || 'Service unavailable. Try again in a moment.',
            3,
            status,
            serverCode ?? 'SERVICE_UNAVAILABLE',
          );
        }
        return new ApiError(
          serverMessage || `Request failed (HTTP ${status}).`,
          3,
          status,
          serverCode ?? 'API_ERROR',
        );
    }
  }
}
