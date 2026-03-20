import { ApiClient, ApiError } from '../../../src/api/client';

function fakeFetch(
  status: number,
  body: Record<string, unknown> = {},
  headers: Record<string, string> = {}
): ((url: string, init?: RequestInit) => Promise<Response>) & {
  lastUrl?: string;
  lastInit?: RequestInit;
} {
  const fn = async (url: string, init?: RequestInit) => {
    fn.lastUrl = url;
    fn.lastInit = init;
    return {
      ok: status >= 200 && status < 300,
      status,
      headers: new Headers(headers),
      json: async () => body,
      text: async () => JSON.stringify(body),
    } as Response;
  };
  fn.lastUrl = undefined as string | undefined;
  fn.lastInit = undefined as RequestInit | undefined;
  return fn;
}

describe('ApiClient', () => {
  const baseURL = 'http://localhost:3001';

  describe('Authorization header', () => {
    it('should inject Bearer token in Authorization header', async () => {
      const fetchFn = fakeFetch(200, { ok: true });
      const client = new ApiClient({
        baseURL,
        token: 'ghp_test123',
        fetchFn,
      });

      await client.get('/health');
      const init = fetchFn.lastInit!;
      const headers = init.headers as Record<string, string>;
      expect(headers['Authorization']).toBe('Bearer ghp_test123');
    });

    it('should omit Authorization header when no token', async () => {
      const fetchFn = fakeFetch(200, { ok: true });
      const client = new ApiClient({
        baseURL,
        token: null,
        fetchFn,
      });

      await client.get('/health');
      const init = fetchFn.lastInit!;
      const headers = init.headers as Record<string, string>;
      expect(headers['Authorization']).toBeUndefined();
    });
  });

  describe('Correlation-ID header', () => {
    it('should inject Correlation-ID as UUID v4', async () => {
      const fetchFn = fakeFetch(200, { ok: true });
      const client = new ApiClient({
        baseURL,
        token: null,
        fetchFn,
      });

      await client.get('/health');
      const init = fetchFn.lastInit!;
      const headers = init.headers as Record<string, string>;
      const correlationId = headers['Correlation-ID'];
      expect(correlationId).toBeDefined();
      expect(correlationId).toMatch(
        /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
      );
    });
  });

  describe('base URL', () => {
    it('should use provided baseURL', async () => {
      const fetchFn = fakeFetch(200, {});
      const client = new ApiClient({
        baseURL: 'http://api.example.com',
        token: null,
        fetchFn,
      });

      await client.get('/tenants');
      expect(fetchFn.lastUrl).toBe('http://api.example.com/tenants');
    });

    it('should default to LOOPZERO_API_URL env var', () => {
      const original = process.env.LOOPZERO_API_URL;
      process.env.LOOPZERO_API_URL = 'http://custom-api:8080';

      const client = new ApiClient({ fetchFn: fakeFetch(200) });
      expect((client as any).baseURL).toBe('http://custom-api:8080');

      if (original !== undefined) {
        process.env.LOOPZERO_API_URL = original;
      } else {
        delete process.env.LOOPZERO_API_URL;
      }
    });

    it('should fallback to localhost:13001 when no env var', () => {
      const original = process.env.LOOPZERO_API_URL;
      delete process.env.LOOPZERO_API_URL;

      const client = new ApiClient({ fetchFn: fakeFetch(200) });
      expect((client as any).baseURL).toBe('http://localhost:13001');

      if (original !== undefined) {
        process.env.LOOPZERO_API_URL = original;
      }
    });
  });

  describe('HTTP methods', () => {
    it('get() should use GET method', async () => {
      const fetchFn = fakeFetch(200, { data: 'test' });
      const client = new ApiClient({ baseURL, token: null, fetchFn });

      const result = await client.get('/tenants');
      expect(fetchFn.lastInit!.method).toBe('GET');
      expect(result).toEqual({ data: 'test' });
    });

    it('post() should use POST method with body', async () => {
      const fetchFn = fakeFetch(201, { id: 't-1234' });
      const client = new ApiClient({ baseURL, token: null, fetchFn });

      const result = await client.post('/tenants', { name: 'acme' });
      const init = fetchFn.lastInit!;
      const headers = init.headers as Record<string, string>;
      expect(init.method).toBe('POST');
      expect(headers['Content-Type']).toBe('application/json');
      expect(JSON.parse(init.body as string)).toEqual({ name: 'acme' });
      expect(result).toEqual({ id: 't-1234' });
    });

    it('del() should use DELETE method', async () => {
      const fetchFn = fakeFetch(204);
      const client = new ApiClient({ baseURL, token: null, fetchFn });

      await client.del('/tenants/t-1234');
      expect(fetchFn.lastInit!.method).toBe('DELETE');
    });
  });

  describe('HTTP error → exit code mapping', () => {
    it('401 → exit code 4 with auth message', async () => {
      const fetchFn = fakeFetch(401, { error: 'unauthorized' });
      const client = new ApiClient({ baseURL, token: null, fetchFn });

      try {
        await client.get('/tenants');
        fail('should have thrown');
      } catch (err) {
        const apiErr = err as ApiError;
        expect(apiErr.exitCode).toBe(4);
        expect(apiErr.message).toContain('loopzero login');
      }
    });

    it('409 with server error body → uses server message with actionable hint', async () => {
      const fetchFn = fakeFetch(409, {
        code: 'DEPLOY_IN_PROGRESS',
        message: 'A deploy is already in progress for this environment.',
      });
      const client = new ApiClient({ baseURL, token: null, fetchFn });

      try {
        await client.post('/deploys', {});
        fail('should have thrown');
      } catch (err) {
        const apiErr = err as ApiError;
        expect(apiErr.exitCode).toBe(3);
        expect(apiErr.statusCode).toBe(409);
        expect(apiErr.message).toContain(
          'A deploy is already in progress for this environment.'
        );
        expect(apiErr.message).toContain('try again later');
      }
    });

    it('409 without parseable body → falls back to default message', async () => {
      const fn = async (_url: string, _init?: RequestInit) => {
        return {
          ok: false,
          status: 409,
          headers: new Headers(),
          json: async () => {
            throw new SyntaxError('Unexpected token');
          },
          text: async () => 'not json',
        } as unknown as Response;
      };
      const client = new ApiClient({ baseURL, token: null, fetchFn: fn });

      try {
        await client.post('/deploys', {});
        fail('should have thrown');
      } catch (err) {
        const apiErr = err as ApiError;
        expect(apiErr.exitCode).toBe(3);
        expect(apiErr.statusCode).toBe(409);
        expect(apiErr.message).toContain('already in progress');
        expect(apiErr.message).toContain('try again later');
      }
    });

    it('422 → message about invalid request', async () => {
      const fetchFn = fakeFetch(422, { error: 'validation failed' });
      const client = new ApiClient({ baseURL, token: null, fetchFn });

      try {
        await client.post('/tenants', {});
        fail('should have thrown');
      } catch (err) {
        const apiErr = err as ApiError;
        expect(apiErr.exitCode).toBe(1);
        expect(apiErr.message).toContain('Invalid request');
      }
    });

    it('500 → message about service unavailable', async () => {
      const fetchFn = fakeFetch(500, { error: 'internal' });
      const client = new ApiClient({ baseURL, token: null, fetchFn });

      try {
        await client.get('/health');
        fail('should have thrown');
      } catch (err) {
        const apiErr = err as ApiError;
        expect(apiErr.exitCode).toBe(3);
        expect(apiErr.message).toContain('unavailable');
      }
    });

    it('error messages should not contain internal terms', async () => {
      const forbidden = [
        'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD',
        'tenant_id', 'advisory lock', 'pg_advisory', 'Gitea',
        'MinIO', 'kubebuilder', 'envtest', 'Kaniko',
      ];

      const statuses = [401, 409, 422, 500];
      for (const status of statuses) {
        const fetchFn = fakeFetch(status, { error: 'err' });
        const client = new ApiClient({ baseURL, token: null, fetchFn });

        try {
          await client.get('/test');
        } catch (err) {
          const msg = (err as ApiError).message;
          for (const term of forbidden) {
            expect(msg).not.toContain(term);
          }
        }
      }
    });
  });

  describe('F-04/F-06: AUTH_SERVICE_UNAVAILABLE maps to exit code 4', () => {
    it('should map 503 with AUTH_SERVICE_UNAVAILABLE code to exit code 4', async () => {
      const fetchFn = fakeFetch(503, {
        code: 'AUTH_SERVICE_UNAVAILABLE',
        message: 'Authentication service is temporarily unavailable',
      });
      const client = new ApiClient({ baseURL, token: 'tok', fetchFn, maxRetries: 0 });

      try {
        await client.get('/test');
        fail('Expected ApiError');
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError);
        expect((err as ApiError).exitCode).toBe(4);
        expect((err as ApiError).code).toBe('AUTH_SERVICE_UNAVAILABLE');
      }
    });

    it('should map generic 503 to exit code 3 (not auth)', async () => {
      const fetchFn = fakeFetch(503, {
        code: 'SERVICE_UNAVAILABLE',
        message: 'Service temporarily unavailable',
      });
      const client = new ApiClient({ baseURL, token: 'tok', fetchFn, maxRetries: 0 });

      try {
        await client.get('/test');
        fail('Expected ApiError');
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError);
        expect((err as ApiError).exitCode).toBe(3);
      }
    });
  });

  describe('retry on transient errors', () => {
    it('should retry on 503 and succeed on second attempt', async () => {
      let callCount = 0;
      const fetchFn = async (_url: string, _init?: RequestInit): Promise<Response> => {
        callCount++;
        if (callCount === 1) {
          return { ok: false, status: 503, headers: new Headers(), json: async () => ({ code: 'SERVICE_UNAVAILABLE' }) } as Response;
        }
        return { ok: true, status: 200, headers: new Headers(), json: async () => ({ result: 'ok' }) } as Response;
      };
      const client = new ApiClient({ baseURL, token: null, fetchFn, maxRetries: 1 });

      const result = await client.get<{ result: string }>('/test');
      expect(result.result).toBe('ok');
      expect(callCount).toBe(2);
    });

    it('should not retry on 4xx errors', async () => {
      let callCount = 0;
      const fetchFn = async (_url: string, _init?: RequestInit): Promise<Response> => {
        callCount++;
        return { ok: false, status: 400, headers: new Headers(), json: async () => ({}) } as Response;
      };
      const client = new ApiClient({ baseURL, token: null, fetchFn, maxRetries: 3 });

      try {
        await client.get('/test');
        fail('should have thrown');
      } catch (err) {
        expect((err as ApiError).statusCode).toBe(400);
      }
      expect(callCount).toBe(1);
    });
  });
});
