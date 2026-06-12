import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => ({
  engineFetch: vi.fn(),
}));

vi.mock('@/lib/config', () => ({ API_URL: '' }));

import { useEnvironments } from './use-environments';
import { engineFetch } from '@/lib/api';
import { DEFAULT_PERMANENT_ENV } from '@/lib/domain';

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
});

describe('useEnvironments — Plan 2026-06-11 T1.4 (closes J-22↔J-10)', () => {
  it('test_returns_permanent_names_when_backend_emits_permanent_field', async () => {
    // Canonical post-2026-06-07: backend emits `permanent[]` directly.
    mockedEngineFetch.mockResolvedValue({
      environments: [{ name: 'production' }, { name: 'staging-eu-1' }],
      permanent: ['production', 'staging-eu-1'],
    });
    const { result } = renderHook(() => useEnvironments(), { wrapper: wrapper() });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.envs).toEqual(['production', 'staging-eu-1']);
    expect(result.current.error).toBe(false);
  });

  it('test_returns_custom_env_so_hardcoded_production_staging_no_longer_drops_them', async () => {
    // The very J-22↔J-10 silent break: a tenant with `staging-eu-1` would
    // not see it appear in the secrets filter chips before this hook.
    mockedEngineFetch.mockResolvedValue({
      environments: [
        { name: 'production' },
        { name: 'staging-eu-1' },
        { name: 'preview-pr-42' },
      ],
    });
    const { result } = renderHook(() => useEnvironments(), { wrapper: wrapper() });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.envs).toContain('staging-eu-1');
    expect(result.current.envs).toContain('preview-pr-42');
  });

  it('test_filters_by_project_when_provided_keeping_legacy_empty_project_entries', async () => {
    // M2 v1.2: per-(project, env) tuple. Legacy entries with empty
    // `project` field MUST still surface (ADR D2 back-compat).
    mockedEngineFetch.mockResolvedValue({
      environments: [
        { name: 'production', project: '' },
        { name: 'staging', project: 'web' },
        { name: 'staging', project: 'api' },
        { name: 'qa', project: 'web' },
      ],
    });
    const { result } = renderHook(() => useEnvironments('web'), {
      wrapper: wrapper(),
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.envs.sort()).toEqual(['production', 'qa', 'staging']);
  });

  it('test_fallback_to_default_permanent_env_on_fetch_error_ec5', async () => {
    // EC-5 absorption: hook MUST NOT collapse the UI on a fetch error.
    // The caller (secrets.tsx dropdown) needs at least one valid choice.
    mockedEngineFetch.mockRejectedValue(new Error('network down'));
    const { result } = renderHook(() => useEnvironments('teste'), {
      wrapper: wrapper(),
    });
    await waitFor(() => expect(result.current.error).toBe(true));
    expect(result.current.envs).toEqual([DEFAULT_PERMANENT_ENV]);
  });

  it('test_fallback_when_filter_eliminates_all_entries', async () => {
    // EC-5 final safety: a fresh project with no scoped envs MUST still
    // surface DEFAULT_PERMANENT_ENV so the dropdown is not empty.
    mockedEngineFetch.mockResolvedValue({
      environments: [{ name: 'production', project: 'other-project' }],
    });
    const { result } = renderHook(() => useEnvironments('fresh-project'), {
      wrapper: wrapper(),
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.envs).toEqual([DEFAULT_PERMANENT_ENV]);
    expect(result.current.error).toBe(false);
  });
});
