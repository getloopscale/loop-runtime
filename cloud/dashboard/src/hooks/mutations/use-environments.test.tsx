import { act, renderHook } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactElement, ReactNode } from 'react';
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';

vi.mock('@/lib/api', () => ({
  engineFetch: vi.fn(),
}));

import {
  useCreateEnvironment,
  useDeleteEnvironment,
  usePromoteEnvironment,
} from './use-environments';
import { queryKeys } from '@/lib/api/query-keys';
import { engineFetch } from '@/lib/api';

const mockedEngineFetch = vi.mocked(engineFetch);

function makeWrapper(): {
  wrapper: (props: { children: ReactNode }) => ReactElement;
  client: QueryClient;
} {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0, staleTime: 0 },
      mutations: { retry: false },
    },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return { wrapper, client };
}

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('useCreateEnvironment', () => {
  it('test_create_happy_path_invalidates_canonical_query_key', async () => {
    mockedEngineFetch.mockResolvedValue({ name: 'qa', status: 'active' });
    const { wrapper, client } = makeWrapper();
    const invalidateSpy = vi.spyOn(client, 'invalidateQueries');

    const { result } = renderHook(() => useCreateEnvironment(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync({ name: 'qa', project: 'web' });
    });

    expect(mockedEngineFetch).toHaveBeenCalledWith(
      '/api/v1/environments',
      expect.objectContaining({ method: 'POST' }),
    );
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: queryKeys.environments.all,
    });
  });
});

describe('useDeleteEnvironment', () => {
  it('test_delete_url_encodes_name', async () => {
    mockedEngineFetch.mockResolvedValue({ deleted: 'qa-eu#1' });
    const { wrapper } = makeWrapper();

    const { result } = renderHook(() => useDeleteEnvironment(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync('qa-eu#1');
    });

    // # must be URL-encoded; assert path contains %23 (no raw #).
    const path = mockedEngineFetch.mock.calls[0]?.[0] as string;
    expect(path).toBe('/api/v1/environments/qa-eu%231');
    expect(path).not.toContain('#');
  });

  it('test_delete_invalidates_canonical_query_key', async () => {
    mockedEngineFetch.mockResolvedValue({ deleted: 'qa' });
    const { wrapper, client } = makeWrapper();
    const invalidateSpy = vi.spyOn(client, 'invalidateQueries');

    const { result } = renderHook(() => useDeleteEnvironment(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync('qa');
    });

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: queryKeys.environments.all,
    });
  });
});

describe('usePromoteEnvironment', () => {
  it('test_promote_posts_canonical_from_to_body_at_project_scoped_path', async () => {
    mockedEngineFetch.mockResolvedValue({ from: 'staging', to: 'production' });
    const { wrapper } = makeWrapper();

    const { result } = renderHook(() => usePromoteEnvironment('teste'), { wrapper });
    await act(async () => {
      await result.current.mutateAsync({ from: 'staging', to: 'production' });
    });

    const [path, options] = mockedEngineFetch.mock.calls[0]!;
    expect(path).toBe('/api/v1/projects/teste/promote');
    expect(options).toMatchObject({ method: 'POST' });
    const body = JSON.parse(options!.body as string);
    expect(body).toEqual({ from: 'staging', to: 'production' });
  });

  it('test_promote_invalidates_environments_AND_builds_queries', async () => {
    mockedEngineFetch.mockResolvedValue({ from: 'staging', to: 'production' });
    const { wrapper, client } = makeWrapper();
    const invalidateSpy = vi.spyOn(client, 'invalidateQueries');

    const { result } = renderHook(() => usePromoteEnvironment('teste'), { wrapper });
    await act(async () => {
      await result.current.mutateAsync({ from: 'staging', to: 'production' });
    });

    // Both cache groups should be invalidated so KPI row + deployments grid
    // reflect the promote immediately.
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: queryKeys.environments.all,
    });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: queryKeys.builds.all,
    });
  });

  it('test_promote_url_encodes_project_name', async () => {
    mockedEngineFetch.mockResolvedValue({ from: 'a', to: 'b' });
    const { wrapper } = makeWrapper();

    const { result } = renderHook(() => usePromoteEnvironment('my project!'), {
      wrapper,
    });
    await act(async () => {
      await result.current.mutateAsync({ from: 'a', to: 'b' });
    });

    const path = mockedEngineFetch.mock.calls[0]?.[0] as string;
    expect(path).toBe('/api/v1/projects/my%20project!/promote');
  });
});

// LIVE catch 2026-06-06: pages today fetch via useEngineApi which uses
// queryKey `['engine', '/api/v1/environments']` (different tree from
// canonical queryKeys.environments). Without an extra `['engine']`
// prefix invalidation, the table stays stale after a mutation. These
// tests pin the dual-invalidation pattern so it can't regress.
describe('engine query tree invalidation (EC-15)', () => {
  it('test_delete_invalidates_engine_prefix', async () => {
    mockedEngineFetch.mockResolvedValue({ deleted: 'qa' });
    const { wrapper, client } = makeWrapper();
    const invalidateSpy = vi.spyOn(client, 'invalidateQueries');

    const { result } = renderHook(() => useDeleteEnvironment(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync('qa');
    });

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['engine'] });
  });

  it('test_create_invalidates_engine_prefix', async () => {
    mockedEngineFetch.mockResolvedValue({ name: 'qa', status: 'active' });
    const { wrapper, client } = makeWrapper();
    const invalidateSpy = vi.spyOn(client, 'invalidateQueries');

    const { result } = renderHook(() => useCreateEnvironment(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync({ name: 'qa', project: 'web' });
    });

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['engine'] });
  });

  it('test_promote_invalidates_engine_prefix', async () => {
    mockedEngineFetch.mockResolvedValue({ from: 'staging', to: 'production' });
    const { wrapper, client } = makeWrapper();
    const invalidateSpy = vi.spyOn(client, 'invalidateQueries');

    const { result } = renderHook(() => usePromoteEnvironment('teste'), { wrapper });
    await act(async () => {
      await result.current.mutateAsync({ from: 'staging', to: 'production' });
    });

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['engine'] });
  });
});
