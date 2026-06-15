import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => ({ engineFetch: vi.fn() }));

import {
  useDomainStatusPolling,
  __test_TERMINAL_STATUSES,
  __test_POLL_INTERVAL_MS,
} from './use-domain-status-polling';
import { engineFetch } from '@/lib/api';

const mockedEngineFetch = vi.mocked(engineFetch);

function wrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: 0 } },
  });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('useDomainStatusPolling (J-11 v1.1 T2.2)', () => {
  it('test_terminal_set_locked_to_three_canonical_statuses (ADR D2)', () => {
    // Plan v1.1 ADR D2 LOCKS the terminal set. If a refactor expands or
    // contracts this set, this test forces an explicit conversation +
    // edge-case review.
    expect(__test_TERMINAL_STATUSES.has('active')).toBe(true);
    expect(__test_TERMINAL_STATUSES.has('failed')).toBe(true);
    expect(__test_TERMINAL_STATUSES.has('removing')).toBe(true);
    expect(__test_TERMINAL_STATUSES.size).toBe(3);
    // Non-terminal statuses must NOT appear in the set.
    expect(__test_TERMINAL_STATUSES.has('pending_verification' as any)).toBe(false);
    expect(__test_TERMINAL_STATUSES.has('verified' as any)).toBe(false);
    expect(__test_TERMINAL_STATUSES.has('provisioning' as any)).toBe(false);
  });

  it('test_poll_interval_canonical_30_seconds', () => {
    // ADR D2 + plan v1.1 lock interval at 30s.
    expect(__test_POLL_INTERVAL_MS).toBe(30_000);
  });

  it('test_use_domain_status_polling_initial_load_fires_engineFetch', async () => {
    mockedEngineFetch.mockResolvedValue({
      id: 'd1',
      domain: 'loopzero-test.example',
      status: 'pending_verification',
      nameservers: ['ns1.x.cloudflare.com', 'ns2.x.cloudflare.com'],
    });
    const { result } = renderHook(() => useDomainStatusPolling('loopzero-test.example'), {
      wrapper: wrapper(),
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(mockedEngineFetch).toHaveBeenCalledOnce();
    expect(result.current.data?.status).toBe('pending_verification');
    // Non-terminal status → isPolling = true
    expect(result.current.isPolling).toBe(true);
  });

  it('test_use_domain_status_polling_stops_on_terminal (EC-3)', async () => {
    // Domain in Active status → isPolling = false.
    mockedEngineFetch.mockResolvedValue({
      id: 'd1',
      domain: 'loopzero-test.example',
      status: 'active',
      nameservers: ['ns1.x.cloudflare.com'],
    });
    const { result } = renderHook(() => useDomainStatusPolling('loopzero-test.example'), {
      wrapper: wrapper(),
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.isPolling).toBe(false);
  });

  it('test_use_domain_status_polling_disabled_when_no_domain_name', () => {
    const { result } = renderHook(() => useDomainStatusPolling(undefined), {
      wrapper: wrapper(),
    });
    // No engineFetch ever called.
    expect(mockedEngineFetch).not.toHaveBeenCalled();
    expect(result.current.isPolling).toBe(false);
    expect(result.current.data).toBeNull();
  });
});
