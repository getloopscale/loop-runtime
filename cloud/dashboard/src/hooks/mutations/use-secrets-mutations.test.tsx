import { act, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => ({
  engineFetch: vi.fn(),
}));

vi.mock('@/lib/config', () => ({ API_URL: '' }));

import {
  useCreateSecret,
  useRotateSecret,
  useDeleteSecret,
  useBulkUploadSecrets,
} from './use-secrets-mutations';
import { engineFetch } from '@/lib/api';

const mockedEngineFetch = vi.mocked(engineFetch);

function wrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  mockedEngineFetch.mockResolvedValue({});
});

describe('useCreateSecret — J-22 T2.2', () => {
  it('test_routes_via_bulk_endpoint_with_one_key_per_env', async () => {
    // LIVE bug 2026-06-10: single-secret POST /api/v1/secrets expects
    // singular `environment` while the Add sheet sends multi-env. Routing
    // through /bulk (T1.1) keeps the canonical shape AND inherits audit
    // (T1.2) + plan-limit gate (T1.3).
    const { result } = renderHook(() => useCreateSecret(), { wrapper: wrapper() });
    await act(async () => {
      result.current.mutate({
        name: 'DATABASE_URL',
        value: 'postgres://x',
        environments: ['production', 'staging'],
        project: 'teste',
      });
    });
    await waitFor(() => expect(mockedEngineFetch).toHaveBeenCalled());
    const [url, init] = mockedEngineFetch.mock.calls[0];
    expect(url).toBe('/api/v1/secrets/bulk');
    expect(init?.method).toBe('POST');
    const body = JSON.parse(init?.body as string);
    expect(body).toEqual({
      project: 'teste',
      secrets: [{ name: 'DATABASE_URL', value: 'postgres://x', envs: ['production', 'staging'] }],
    });
  });
});

describe('useRotateSecret — J-22 T2.2 (EC-13 expected_version)', () => {
  it('test_puts_to_named_secret_with_expected_version', async () => {
    const { result } = renderHook(() => useRotateSecret(), { wrapper: wrapper() });
    await act(async () => {
      result.current.mutate({
        name: 'API_KEY',
        newValue: 'new-secret',
        expectedVersion: 3,
        environment: 'production',
        project: 'teste',
      });
    });
    await waitFor(() => expect(mockedEngineFetch).toHaveBeenCalled());
    const [url, init] = mockedEngineFetch.mock.calls[0];
    expect(url).toBe('/api/v1/secrets/API_KEY');
    expect(init?.method).toBe('PUT');
    const body = JSON.parse(init?.body as string);
    expect(body.value).toBe('new-secret');
    expect(body.expected_version).toBe(3);
    expect(body.environment).toBe('production');
    expect(body.project).toBe('teste');
  });
});

describe('useDeleteSecret — J-22 T2.2', () => {
  it('test_deletes_with_env_and_project_query_params', async () => {
    const { result } = renderHook(() => useDeleteSecret(), { wrapper: wrapper() });
    await act(async () => {
      result.current.mutate({ name: 'DEBUG_FLAG', environment: 'staging', project: 'teste' });
    });
    await waitFor(() => expect(mockedEngineFetch).toHaveBeenCalled());
    const [url, init] = mockedEngineFetch.mock.calls[0];
    expect(url).toBe('/api/v1/secrets/DEBUG_FLAG?environment=staging&project=teste');
    expect(init?.method).toBe('DELETE');
  });
});

describe('useBulkUploadSecrets — J-22 T2.2 (T1.1 endpoint consumer)', () => {
  it('test_posts_bulk_payload_to_bulk_endpoint', async () => {
    const { result } = renderHook(() => useBulkUploadSecrets(), { wrapper: wrapper() });
    await act(async () => {
      result.current.mutate({
        project: 'teste',
        secrets: [
          { name: 'KEY_A', value: 'a', envs: ['production'] },
          { name: 'KEY_B', value: 'b', envs: ['production', 'staging'] },
        ],
      });
    });
    await waitFor(() => expect(mockedEngineFetch).toHaveBeenCalled());
    const [url, init] = mockedEngineFetch.mock.calls[0];
    expect(url).toBe('/api/v1/secrets/bulk');
    expect(init?.method).toBe('POST');
    const body = JSON.parse(init?.body as string);
    expect(body.project).toBe('teste');
    expect(body.secrets).toHaveLength(2);
    expect(body.secrets[1].envs).toEqual(['production', 'staging']);
  });
});
