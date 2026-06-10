import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

vi.mock('@/lib/config', () => ({
  API_URL: '',
  AUTH_URL: '',
  ENGINE_URL: '',
  ACCOUNTS_URL: '',
}));

const mockEngineFetch = vi.fn();
vi.mock('@/lib/api', () => ({
  engineFetch: (...args: unknown[]) => mockEngineFetch(...args),
}));

import { useSwitchTeam } from './use-switch-team';

function makeClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
}

describe('useSwitchTeam (EC-02)', () => {
  beforeEach(() => {
    mockEngineFetch.mockReset();
  });

  it('test_clears_ALL_queries_on_success — not just whoami', async () => {
    mockEngineFetch.mockResolvedValueOnce({ ok: true });
    const client = makeClient();
    // Pre-populate caches that should be nuked after team switch
    client.setQueryData(['projects', 'list', {}], { projects: [{ name: 'p1' }] });
    client.setQueryData(['secrets', 'list', {}], { secrets: [{ name: 's1' }] });
    client.setQueryData(['whoami'], { team_id: 'old' });

    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );

    const clearSpy = vi.spyOn(client, 'clear');
    const { result } = renderHook(() => useSwitchTeam(), { wrapper });
    result.current.mutate('new-team-id');

    await waitFor(() => {
      expect(clearSpy).toHaveBeenCalledTimes(1);
    });

    // After clear, every pre-populated cache must be gone (not just whoami)
    expect(client.getQueryData(['projects', 'list', {}])).toBeUndefined();
    expect(client.getQueryData(['secrets', 'list', {}])).toBeUndefined();
    expect(client.getQueryData(['whoami'])).toBeUndefined();
  });

  it('test_posts_to_team_switch_endpoint_with_team_id', async () => {
    mockEngineFetch.mockResolvedValueOnce({ ok: true });
    const client = makeClient();
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useSwitchTeam(), { wrapper });
    result.current.mutate('team-xyz');

    await waitFor(() => {
      expect(mockEngineFetch).toHaveBeenCalledTimes(1);
    });
    const [path, options] = mockEngineFetch.mock.calls[0];
    expect(path).toBe('/api/v1/auth/team/switch');
    expect((options as RequestInit).method).toBe('POST');
    expect(((options as RequestInit).body as string)).toContain('team-xyz');
  });
});
