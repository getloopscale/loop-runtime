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

  constructor(message: string, exitCode: number, statusCode: number) {
    super(message);
    this.name = 'ApiError';
    this.exitCode = exitCode;
    this.statusCode = statusCode;
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
    body: Record<string, unknown>
  ): Promise<T> {
    return this.request<T>('POST', path, body);
  }

  async del<T = unknown>(path: string): Promise<T> {
    return this.request<T>('DELETE', path);
  }

  private async request<T>(
    method: string,
    path: string,
    body?: Record<string, unknown>
  ): Promise<T> {
    const headers: Record<string, string> = {
      'Correlation-ID': randomUUID(),
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
      throw this.mapError(res.status);
    }

    if (res.status === 204) {
      return undefined as T;
    }

    return (await res.json()) as T;
  }

  private mapError(status: number): ApiError {
    switch (status) {
      case 401:
        return new ApiError(
          'Not authenticated. Run: loopzero login',
          4,
          status
        );
      case 403:
        return new ApiError(
          'Permission denied. Check your account access.',
          4,
          status
        );
      case 409:
        return new ApiError(
          'Operation already in progress. Wait for it to complete or cancel it.',
          3,
          status
        );
      case 422:
        return new ApiError(
          'Invalid request. Check your command arguments.',
          1,
          status
        );
      default:
        if (status >= 500) {
          return new ApiError(
            'Service unavailable. Try again in a moment.',
            3,
            status
          );
        }
        return new ApiError(
          `Request failed (HTTP ${status}).`,
          3,
          status
        );
    }
  }
}
