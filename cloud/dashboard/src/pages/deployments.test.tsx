import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { NuqsAdapter } from 'nuqs/adapters/react-router/v6';
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

// Skip the auth gate.
vi.mock('@/hooks/use-auth', () => ({ useRequireAuth: () => undefined }));

import { DeploymentsPage } from './deployments';
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
    commit_sha: 'abc1234567890',
    created_by: 'paulo@loopzero.test',
    created_at: '2026-06-06T00:00:00Z',
    ...overrides,
  };
}

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: 0 } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/projects/teste/deployments']}>
        <NuqsAdapter>
          <Routes>
            <Route path="/projects/:name/deployments" element={<DeploymentsPage />} />
          </Routes>
        </NuqsAdapter>
      </MemoryRouter>
    </QueryClientProvider>,
  );
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

describe('DeploymentsPage', () => {
  it('test_initial_load_from_rest', async () => {
    mockedEngineFetch.mockResolvedValue({
      builds: [makeBuild('b-1', { commit_sha: '1234567890' })],
      total: 1,
    } as BuildListResponse);

    renderPage();

    await waitFor(() => expect(screen.queryByText(/no deployments/i)).not.toBeInTheDocument());
    expect(screen.getByText('1234567')).toBeInTheDocument();
  });

  it('test_new_build_appends_in_realtime', async () => {
    let callCount = 0;
    mockedEngineFetch.mockImplementation(async () => {
      callCount++;
      if (callCount === 1) {
        return {
          builds: [makeBuild('b-1', { commit_sha: 'aaaaaaa' })],
          total: 1,
        } as BuildListResponse;
      }
      return {
        builds: [
          makeBuild('b-1', { commit_sha: 'aaaaaaa' }),
          makeBuild('b-2', {
            commit_sha: 'bbbbbbb',
            status: 'running',
            created_at: '2026-06-06T01:00:00Z',
          }),
        ],
        total: 2,
      } as BuildListResponse;
    });

    renderPage();

    await waitFor(() => expect(screen.getByText('aaaaaaa')).toBeInTheDocument());

    // Emit SSE event for unknown build_id → triggers refetch.
    const es = MockEventSource.instances[0];
    expect(es).toBeDefined();
    await act(async () => {
      es.emit('build.started', { build_id: 'b-2', status: 'running' });
    });

    await waitFor(() => expect(screen.getByText('bbbbbbb')).toBeInTheDocument());
  });

  it('test_existing_build_updates_status_in_place', async () => {
    mockedEngineFetch.mockResolvedValue({
      builds: [makeBuild('b-1', { commit_sha: 'aaaaaaa', status: 'running' })],
      total: 1,
    } as BuildListResponse);

    renderPage();

    // Multiple "Running" matches exist (filter button + StatusDot). We
    // target the row by its commit SHA cell, then look inside for the
    // status label that lives there.
    await waitFor(() => expect(screen.getByText('aaaaaaa')).toBeInTheDocument());
    const row = screen.getByText('aaaaaaa').closest('tr');
    expect(row).not.toBeNull();
    expect(within(row as HTMLElement).getByText(/running/i)).toBeInTheDocument();

    const es = MockEventSource.instances[0];
    await act(async () => {
      es.emit('build.succeeded', { build_id: 'b-1', status: 'succeeded' });
    });

    await waitFor(() => {
      const newRow = screen.getByText('aaaaaaa').closest('tr');
      expect(within(newRow as HTMLElement).getByText(/succeeded/i)).toBeInTheDocument();
    });
    // SSE patches in place, no refetch of the BUILDS LIST. (useProjectStatus
    // refetches its own /status endpoint on a separate interval; we only
    // assert the builds list query was hit once.)
    const buildsCalls = mockedEngineFetch.mock.calls.filter(([p]) =>
      typeof p === 'string' && p.includes('/builds'),
    );
    expect(buildsCalls.length).toBe(1);
  });

  it('test_status_filter_chips_work', async () => {
    mockedEngineFetch.mockResolvedValue({
      builds: [makeBuild('b-1', { commit_sha: 'aaaaaaa' })],
      total: 1,
    } as BuildListResponse);

    renderPage();
    await waitFor(() => expect(screen.getByText('aaaaaaa')).toBeInTheDocument());

    const group = screen.getByRole('group', { name: /filter by status/i });
    await userEvent.click(within(group).getByRole('button', { name: /^running$/i }));

    // Filter pushes status= into URL → triggers refetch with new queryKey.
    await waitFor(() => {
      const calls = mockedEngineFetch.mock.calls;
      const lastCall = calls[calls.length - 1];
      expect(lastCall?.[0]).toContain('?status=running');
    });
  });
});
