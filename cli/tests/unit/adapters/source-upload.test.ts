// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { uploadSource } from '../../../src/adapters/source-upload';
import { ApiError } from '../../../src/api/client';
import * as fs from 'fs';

jest.mock('fs');

const mockFs = fs as jest.Mocked<typeof fs>;

function mockFsReads(): void {
  mockFs.readFileSync.mockImplementation((path: fs.PathOrFileDescriptor) => {
    const p = String(path);
    if (p.endsWith('.bundle')) return Buffer.from('bundle-data');
    if (p.endsWith('loopzero.yaml')) return 'project: my-saas\napps: {}';
    return '';
  });
}

function jsonResponse(status: number, body: Record<string, unknown>): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 422 ? 'Unprocessable Entity' : status === 500 ? 'Internal Server Error' : 'Error',
    json: () => Promise.resolve(body),
    text: () => Promise.resolve(JSON.stringify(body)),
    headers: new Headers(),
  } as unknown as Response;
}

function textResponse(status: number, text: string): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: 'Error',
    json: () => Promise.reject(new SyntaxError('Unexpected token')),
    text: () => Promise.resolve(text),
    headers: new Headers(),
  } as unknown as Response;
}

describe('uploadSource', () => {
  beforeEach(() => {
    jest.restoreAllMocks();
    mockFsReads();
  });

  describe('F-01: timeout when API unreachable', () => {
    it('should pass AbortSignal.timeout to fetch', async () => {
      const fetchSpy = jest.spyOn(globalThis, 'fetch').mockResolvedValue(
        jsonResponse(200, { commit_sha: 'abc123', apps: ['api'] })
      );

      await uploadSource('http://localhost:3001', 'token', '/tmp/b.bundle', '/proj/loopzero.yaml');

      const callArgs = fetchSpy.mock.calls[0];
      const init = callArgs[1] as RequestInit;
      expect(init.signal).toBeDefined();
    });

    it('should throw ApiError with exit code 3 on timeout', async () => {
      const timeoutError = new DOMException('The operation was aborted due to timeout', 'TimeoutError');
      jest.spyOn(globalThis, 'fetch').mockRejectedValue(timeoutError);

      await expect(
        uploadSource('http://localhost:3001', 'token', '/tmp/b.bundle', '/proj/loopzero.yaml')
      ).rejects.toThrow(ApiError);

      try {
        await uploadSource('http://localhost:3001', 'token', '/tmp/b.bundle', '/proj/loopzero.yaml');
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError);
        expect((err as ApiError).exitCode).toBe(3);
        expect((err as ApiError).message).toContain('timed out');
      }
    });

    it('should throw ApiError with exit code 3 on connection refused', async () => {
      const connError = new TypeError('fetch failed');
      (connError as unknown as Record<string, unknown>).cause = new Error('connect ECONNREFUSED 127.0.0.1:3001');
      jest.spyOn(globalThis, 'fetch').mockRejectedValue(connError);

      await expect(
        uploadSource('http://localhost:3001', 'token', '/tmp/b.bundle', '/proj/loopzero.yaml')
      ).rejects.toThrow(ApiError);

      try {
        await uploadSource('http://localhost:3001', 'token', '/tmp/b.bundle', '/proj/loopzero.yaml');
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError);
        expect((err as ApiError).exitCode).toBe(3);
        expect((err as ApiError).message).toMatch(/could not reach|connection/i);
      }
    });

    it('should throw ApiError with exit code 3 on generic network error', async () => {
      jest.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('network error'));

      await expect(
        uploadSource('http://localhost:3001', 'token', '/tmp/b.bundle', '/proj/loopzero.yaml')
      ).rejects.toThrow(ApiError);

      try {
        await uploadSource('http://localhost:3001', 'token', '/tmp/b.bundle', '/proj/loopzero.yaml');
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError);
        expect((err as ApiError).exitCode).toBe(3);
      }
    });
  });

  describe('F-02: error message parsing', () => {
    it('should extract message from 422 JSON error and throw ApiError with exit code 1', async () => {
      jest.spyOn(globalThis, 'fetch').mockResolvedValue(
        jsonResponse(422, {
          code: 'VALIDATION_FAILED',
          message: "app 'worker' must have a port",
        })
      );

      try {
        await uploadSource('http://localhost:3001', 'token', '/tmp/b.bundle', '/proj/loopzero.yaml');
        fail('Should have thrown');
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError);
        const apiErr = err as ApiError;
        expect(apiErr.exitCode).toBe(1);
        expect(apiErr.statusCode).toBe(422);
        expect(apiErr.message).toBe("app 'worker' must have a port");
        // Must NOT contain raw JSON
        expect(apiErr.message).not.toContain('{');
        expect(apiErr.message).not.toContain('}');
      }
    });

    it('should throw ApiError with exit code 1 for other 4xx errors', async () => {
      jest.spyOn(globalThis, 'fetch').mockResolvedValue(
        jsonResponse(400, {
          code: 'BAD_REQUEST',
          message: 'Missing required field: project',
        })
      );

      try {
        await uploadSource('http://localhost:3001', 'token', '/tmp/b.bundle', '/proj/loopzero.yaml');
        fail('Should have thrown');
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError);
        const apiErr = err as ApiError;
        expect(apiErr.exitCode).toBe(1);
        expect(apiErr.statusCode).toBe(400);
        expect(apiErr.message).toBe('Missing required field: project');
      }
    });

    it('should throw ApiError with exit code 4 for 401', async () => {
      jest.spyOn(globalThis, 'fetch').mockResolvedValue(
        jsonResponse(401, { message: 'Token expired' })
      );

      try {
        await uploadSource('http://localhost:3001', 'token', '/tmp/b.bundle', '/proj/loopzero.yaml');
        fail('Should have thrown');
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError);
        const apiErr = err as ApiError;
        expect(apiErr.exitCode).toBe(4);
        expect(apiErr.statusCode).toBe(401);
      }
    });

    it('should throw ApiError with exit code 3 for 500 server error', async () => {
      jest.spyOn(globalThis, 'fetch').mockResolvedValue(
        jsonResponse(500, {
          code: 'INTERNAL_ERROR',
          message: 'Internal server error',
        })
      );

      try {
        await uploadSource('http://localhost:3001', 'token', '/tmp/b.bundle', '/proj/loopzero.yaml');
        fail('Should have thrown');
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError);
        const apiErr = err as ApiError;
        expect(apiErr.exitCode).toBe(3);
        expect(apiErr.statusCode).toBe(500);
        expect(apiErr.message).toBe('Internal server error');
      }
    });

    it('should handle non-JSON error body gracefully', async () => {
      jest.spyOn(globalThis, 'fetch').mockResolvedValue(
        textResponse(502, '<html>Bad Gateway</html>')
      );

      try {
        await uploadSource('http://localhost:3001', 'token', '/tmp/b.bundle', '/proj/loopzero.yaml');
        fail('Should have thrown');
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError);
        const apiErr = err as ApiError;
        expect(apiErr.exitCode).toBe(3);
        expect(apiErr.statusCode).toBe(502);
        // Should not contain raw HTML
        expect(apiErr.message).not.toContain('<html>');
      }
    });

    it('should handle JSON error body without message field', async () => {
      jest.spyOn(globalThis, 'fetch').mockResolvedValue(
        jsonResponse(422, { code: 'VALIDATION_FAILED' })
      );

      try {
        await uploadSource('http://localhost:3001', 'token', '/tmp/b.bundle', '/proj/loopzero.yaml');
        fail('Should have thrown');
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError);
        const apiErr = err as ApiError;
        expect(apiErr.exitCode).toBe(1);
        expect(apiErr.statusCode).toBe(422);
        // Should have a fallback message, not raw JSON
        expect(apiErr.message).not.toContain('{');
      }
    });
  });

  describe('success path', () => {
    it('should return parsed response on success', async () => {
      jest.spyOn(globalThis, 'fetch').mockResolvedValue(
        jsonResponse(200, { commit_sha: 'abc123', apps: ['api', 'web'] })
      );

      const result = await uploadSource(
        'http://localhost:3001', 'token', '/tmp/b.bundle', '/proj/loopzero.yaml'
      );

      expect(result).toEqual({ commit_sha: 'abc123', apps: ['api', 'web'] });
    });
  });
});
