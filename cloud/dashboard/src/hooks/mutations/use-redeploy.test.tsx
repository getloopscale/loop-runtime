import { act, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactElement, ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => ({
  engineFetch: vi.fn(),
}));

import { useRedeploy } from './use-redeploy';
import { engineFetch } from '@/lib/api';

const mockedEngineFetch = vi.mocked(engineFetch);

function makeWrapper(): {
  wrapper: (props: { children: ReactNode }) => ReactElement;
  client: QueryClient;
} {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: 0 } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return { wrapper, client };
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('useRedeploy', () => {
  it('test_posts_to_builds_endpoint_with_redeploy_of', async () => {
    mockedEngineFetch.mockResolvedValue({
      build_id: 'b-new',
      stream_url: '/api/v1/builds/b-new/stream',
    });

    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useRedeploy(), { wrapper });

    await act(async () => {
      await result.current.mutateAsync('b-source');
    });

    expect(mockedEngineFetch).toHaveBeenCalledOnce();
    const [path, options] = mockedEngineFetch.mock.calls[0];
    expect(path).toBe('/api/v1/builds');
    expect(options?.method).toBe('POST');
    expect(JSON.parse(options?.body as string)).toEqual({ redeploy_of: 'b-source' });
  });

  it('test_returns_build_create_response', async () => {
    mockedEngineFetch.mockResolvedValue({
      build_id: 'b-new',
      stream_url: '/api/v1/builds/b-new/stream',
    });

    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useRedeploy(), { wrapper });

    await act(async () => {
      const resp = await result.current.mutateAsync('b-source');
      expect(resp?.build_id).toBe('b-new');
    });
  });

  it('test_invalidates_builds_cache_on_success', async () => {
    mockedEngineFetch.mockResolvedValue({
      build_id: 'b-new',
      stream_url: '/api/v1/builds/b-new/stream',
    });

    const { wrapper, client } = makeWrapper();
    const invalidateSpy = vi.spyOn(client, 'invalidateQueries');

    const { result } = renderHook(() => useRedeploy(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync('b-source');
    });

    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalled();
    });
  });

  it('test_surfaces_typed_error_on_409', async () => {
    mockedEngineFetch.mockRejectedValue(new Error('BUILD_NOT_TERMINAL'));

    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useRedeploy(), { wrapper });

    await act(async () => {
      try {
        await result.current.mutateAsync('b-running');
      } catch {
        // expected
      }
    });

    await waitFor(() => {
      expect(result.current.error).toBeInstanceOf(Error);
    });
    expect((result.current.error as Error).message).toBe('BUILD_NOT_TERMINAL');
  });
});
