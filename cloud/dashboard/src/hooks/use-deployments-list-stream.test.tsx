import { act, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactElement, ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Build, BuildListResponse } from '@/lib/api/types';

vi.mock('@/lib/api', () => ({
  ensureSSEToken: vi.fn(async () => 'tok'),
  clearToken: vi.fn(),
  hasValidToken: vi.fn(() => true),
  engineFetch: vi.fn(),
  cloudFetch: vi.fn(),
}));

vi.mock('@/lib/config', () => ({ API_URL: '' }));

import { useDeploymentsListStream } from './use-deployments-list-stream';
import { engineFetch } from '@/lib/api';

const mockedEngineFetch = vi.mocked(engineFetch);

class MockEventSource {
  static instances: MockEventSource[] = [];
  url: string;
  listeners: Record<string, Array<(ev: unknown) => void>> = {};
  onopen: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: unknown) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  closed = false;

  constructor(url: string) {
    this.url = url;
    MockEventSource.instances.push(this);
    queueMicrotask(() => this.onopen?.({}));
  }
  addEventListener(type: string, l: (ev: unknown) => void) {
    (this.listeners[type] ??= []).push(l);
  }
  removeEventListener() {}
  emit(type: string, data: unknown, lastEventId = '') {
    const ev = { data: JSON.stringify(data), lastEventId };
    (this.listeners[type] ?? []).forEach((l) => l(ev));
  }
  close() {
    this.closed = true;
  }
}

function makeBuild(id: string, overrides: Partial<Build> = {}): Build {
  return {
    build_id: id,
    project_id: 'p-1',
    environment: 'production',
    status: 'succeeded',
    commit_sha: 'abc1234',
    created_by: 'paulo@loopzero.test',
    created_at: '2026-06-06T00:00:00Z',
    ...overrides,
  };
}

function makeWrapper(): { wrapper: (props: { children: ReactNode }) => ReactElement; client: QueryClient } {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0, staleTime: 0 },
    },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return { wrapper, client };
}

beforeEach(() => {
  MockEventSource.instances = [];
  // @ts-expect-error global stub
  globalThis.EventSource = MockEventSource;
  vi.clearAllMocks();
});

afterEach(() => {
  // @ts-expect-error restore
  delete globalThis.EventSource;
});

describe('useDeploymentsListStream', () => {
  it('test_initial_state_returns_rest_response', async () => {
    const initial: BuildListResponse = {
      builds: [makeBuild('b-1'), makeBuild('b-2', { status: 'running' })],
      total: 2,
    };
    mockedEngineFetch.mockResolvedValue(initial);

    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useDeploymentsListStream('teste'), { wrapper });

    await waitFor(() => expect(result.current.builds.length).toBe(2));
    // Sort: created_at desc (both have same date; stable order from Map)
    expect(result.current.builds.map((b) => b.build_id).sort()).toEqual(['b-1', 'b-2']);
    expect(result.current.loading).toBe(false);
  });

  it('test_new_build_event_appends', async () => {
    // First REST call returns 1 build; SSE then announces a new build_id.
    // The hook should call engineFetch a second time (invalidate triggers refetch).
    let callCount = 0;
    mockedEngineFetch.mockImplementation(async () => {
      callCount++;
      if (callCount === 1) {
        return { builds: [makeBuild('b-1')], total: 1 } as BuildListResponse;
      }
      return {
        builds: [
          makeBuild('b-1'),
          makeBuild('b-2', { status: 'running', created_at: '2026-06-06T00:01:00Z' }),
        ],
        total: 2,
      } as BuildListResponse;
    });

    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useDeploymentsListStream('teste'), { wrapper });

    await waitFor(() => expect(result.current.builds.length).toBe(1));

    // Emit unknown-build event → triggers invalidate → refetch
    const es = MockEventSource.instances[0];
    expect(es).toBeDefined();
    await act(async () => {
      es.emit('build.started', { build_id: 'b-2', status: 'running' });
    });

    await waitFor(() => expect(callCount).toBeGreaterThanOrEqual(2));
    await waitFor(() => expect(result.current.builds.length).toBe(2));
    expect(result.current.builds.map((b) => b.build_id).sort()).toEqual(['b-1', 'b-2']);
  });

  it('test_replayed_started_does_not_regress_terminal_snapshot', async () => {
    // LIVE bug 2026-06-06 regression guard: when the SSE replays an
    // older `build.started` event whose paired `build.succeeded` has
    // already been evicted from the buffer FIFO cap, the reducer used
    // to override the snapshot's SUCCEEDED status with `running`,
    // making the Running pill jump onto a row that had finished
    // minutes ago. Terminal must be absorbing in the list reducer.
    const initial: BuildListResponse = {
      builds: [
        makeBuild('b-finished', { status: 'succeeded' }),
        makeBuild('b-also-finished', { status: 'failed' }),
      ],
      total: 2,
    };
    mockedEngineFetch.mockResolvedValue(initial);

    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useDeploymentsListStream('teste'), { wrapper });

    await waitFor(() => expect(result.current.builds.length).toBe(2));

    const es = MockEventSource.instances[0];
    expect(es).toBeDefined();
    // SSE replay sends started for builds the snapshot already shows as
    // terminal — this is the failure mode (the matching succeeded was
    // dropped from the FIFO before the client connected).
    await act(async () => {
      es.emit('build.started', { build_id: 'b-finished', status: 'running' });
      es.emit('build.started', { build_id: 'b-also-finished', status: 'running' });
    });

    // Status MUST remain at the snapshot terminal value; no row should
    // flicker to running.
    const byId = new Map(result.current.builds.map((b) => [b.build_id, b]));
    expect(byId.get('b-finished')?.status).toBe('succeeded');
    expect(byId.get('b-also-finished')?.status).toBe('failed');
  });

  it('test_limit_param_forwarded_and_total_surfaced', async () => {
    // Polish 2026-06-06: page accepts ?limit=N and exposes backend total
    // so the deployments page can show "Showing N of M" and a Load more
    // button. Verify the limit reaches the REST URL and that total is
    // bubbled up unchanged.
    mockedEngineFetch.mockResolvedValue({
      builds: [makeBuild('b-1'), makeBuild('b-2')],
      total: 137, // backend reports 137 total even though only 2 rows returned
    } as BuildListResponse);

    const { wrapper } = makeWrapper();
    const { result } = renderHook(
      () => useDeploymentsListStream('teste', { limit: 20 }),
      { wrapper },
    );

    await waitFor(() => expect(result.current.builds.length).toBe(2));
    expect(result.current.total).toBe(137);
    // The engineFetch path must carry ?limit=20
    const lastCallPath = mockedEngineFetch.mock.calls[0]?.[0] as string;
    expect(lastCallPath).toContain('limit=20');
  });

  it('test_env_filter_forwarded_as_query_param', async () => {
    // T4.2: env chip persists into the REST query string. Server-side
    // narrowing is the canonical path (client filter is just a safety
    // net); the hook MUST forward `env=staging` if the page requests it.
    mockedEngineFetch.mockResolvedValue({
      builds: [makeBuild('b-1', { environment: 'staging' })],
      total: 1,
    } as BuildListResponse);

    const { wrapper } = makeWrapper();
    renderHook(() => useDeploymentsListStream('teste', { env: 'staging' }), { wrapper });

    await waitFor(() => expect(mockedEngineFetch).toHaveBeenCalled());
    const path = mockedEngineFetch.mock.calls[0]?.[0] as string;
    expect(path).toContain('env=staging');
  });

  it('test_refetch_triggers_fresh_rest_call', async () => {
    // Polish 2026-06-06: explicit Refresh button. Calling refetch()
    // increments the engineFetch call count even when nothing else
    // changed (SSE keeps flowing, no event needed).
    mockedEngineFetch.mockResolvedValue({
      builds: [makeBuild('b-1')],
      total: 1,
    } as BuildListResponse);

    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useDeploymentsListStream('teste'), { wrapper });

    await waitFor(() => expect(result.current.builds.length).toBe(1));
    const before = mockedEngineFetch.mock.calls.length;

    await act(async () => {
      result.current.refetch();
    });

    await waitFor(() =>
      expect(mockedEngineFetch.mock.calls.length).toBeGreaterThan(before),
    );
  });

  it('test_status_change_event_patches_in_place', async () => {
    const initial: BuildListResponse = {
      builds: [makeBuild('b-1', { status: 'running' })],
      total: 1,
    };
    mockedEngineFetch.mockResolvedValue(initial);

    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useDeploymentsListStream('teste'), { wrapper });

    await waitFor(() => expect(result.current.builds[0]?.status).toBe('running'));

    const es = MockEventSource.instances[0];
    expect(es).toBeDefined();
    await act(async () => {
      es.emit('build.succeeded', { build_id: 'b-1', status: 'succeeded', duration_seconds: 42 });
    });

    await waitFor(() => expect(result.current.builds[0]?.status).toBe('succeeded'));
    expect(result.current.builds[0]?.duration_seconds).toBe(42);
    // No refetch needed for in-place patch — engineFetch called only once.
    expect(mockedEngineFetch).toHaveBeenCalledTimes(1);
  });
});
