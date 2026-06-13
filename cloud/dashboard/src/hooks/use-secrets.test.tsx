import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => ({
  engineFetch: vi.fn(),
}));

vi.mock('@/lib/config', () => ({ API_URL: '' }));

import { useSecrets } from './use-secrets';
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
  mockedEngineFetch.mockResolvedValue({ secrets: [] });
});

describe('useSecrets — J-22 T2.1 + 2026-06-10 LIVE shape fixes', () => {
  it('test_passes_project_and_single_env_when_filter_set', async () => {
    const { result } = renderHook(
      () => useSecrets({ project: 'teste', env: 'production' }),
      { wrapper: wrapper() },
    );
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(mockedEngineFetch).toHaveBeenCalledWith(
      '/api/v1/secrets?project=teste&env=production',
    );
    expect(mockedEngineFetch).toHaveBeenCalledTimes(1);
  });

  it('test_fans_out_across_default_envs_when_filter_absent', async () => {
    const { result } = renderHook(
      () => useSecrets({ project: 'teste' }),
      { wrapper: wrapper() },
    );
    await waitFor(() => expect(result.current.loading).toBe(false));
    const calls = mockedEngineFetch.mock.calls.map((c) => c[0]);
    expect(calls).toContain('/api/v1/secrets?project=teste&env=production');
    expect(calls).toContain('/api/v1/secrets?project=teste&env=staging');
  });

  it('test_groups_rows_by_name_so_multi_env_secrets_collapse_to_one_row', async () => {
    // Backend emits one row per (key × env); UI groups by name.
    mockedEngineFetch.mockResolvedValueOnce({
      secrets: [
        { name: 'DATABASE_URL', environment: 'production', app: '' },
        { name: 'API_KEY', environment: 'production', app: '' },
      ],
    });
    mockedEngineFetch.mockResolvedValueOnce({
      secrets: [{ name: 'API_KEY', environment: 'staging', app: '' }],
    });
    const { result } = renderHook(
      () => useSecrets({ project: 'teste' }),
      { wrapper: wrapper() },
    );
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.secrets).toHaveLength(2);
    const dbUrl = result.current.secrets.find((s) => s.name === 'DATABASE_URL');
    const apiKey = result.current.secrets.find((s) => s.name === 'API_KEY');
    expect(dbUrl?.environments).toEqual(['production']);
    expect(apiKey?.environments.sort()).toEqual(['production', 'staging']);
  });
});
