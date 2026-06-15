import { act, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactElement, ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => ({
  engineFetch: vi.fn(),
}));

import { useScale } from './use-scale';
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

describe('useScale', () => {
  it('test_posts_scale_input_to_canonical_endpoint', async () => {
    mockedEngineFetch.mockResolvedValue({
      app: 'api',
      environment: 'production',
      replicas: 3,
      status: 'applied',
      monthly_cost_usd: 109.5,
      plan_tier: 'pro',
    });

    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useScale('my-api'), { wrapper });

    await act(async () => {
      await result.current.mutateAsync({
        app: 'api',
        environment: 'production',
        replicas: 3,
      });
    });

    expect(mockedEngineFetch).toHaveBeenCalledOnce();
    const [path, options] = mockedEngineFetch.mock.calls[0];
    expect(path).toBe('/api/v1/projects/my-api/scale');
    expect(options?.method).toBe('POST');
    expect(JSON.parse(options?.body as string)).toEqual({
      app: 'api',
      environment: 'production',
      replicas: 3,
    });
  });

  it('test_returns_server_authoritative_monthly_cost', async () => {
    mockedEngineFetch.mockResolvedValue({
      app: 'api',
      environment: 'production',
      replicas: 5,
      status: 'applied',
      monthly_cost_usd: 182.5,
      plan_tier: 'pro',
    });

    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useScale('my-api'), { wrapper });

    const resp = await act(async () =>
      result.current.mutateAsync({ app: 'api', replicas: 5 }),
    );

    expect(resp).toMatchObject({
      monthly_cost_usd: 182.5,
      plan_tier: 'pro',
      status: 'applied',
    });
  });

  it('test_propagates_402_plan_upgrade_required_error', async () => {
    const upgradeErr = new Error('PLAN_UPGRADE_REQUIRED');
    mockedEngineFetch.mockRejectedValue(upgradeErr);

    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useScale('my-api'), { wrapper });

    let caught: unknown = null;
    await act(async () => {
      try {
        await result.current.mutateAsync({ app: 'api', min: 0, max: 5 });
      } catch (e) {
        caught = e;
      }
    });

    expect(caught).toBe(upgradeErr);
    // useMutation flushes isError on the next render — wait for it.
    await waitFor(() => expect(result.current.isError).toBe(true));
  });
});
