import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

// T0.1 of plan 2026-05-27-backend-frontend-integration v1.1 — assert that the
// 3 mutations in use-team.ts hit the PLURAL canonical path /api/v1/teams/members
// matching backend router.go:342-345. Pre-T0.1 the code used SINGULAR
// (/api/v1/team/members), which mock SW silently dropped (returned null,
// optimistic UI hid the failure). Plain semantic tests can't catch this — only
// a string literal assertion on the path can.

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

import { useInviteMember, useRemoveMember, useUpdateMemberRole } from './use-team';

function makeClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
}

function wrap(client: QueryClient) {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe('use-team mutations — path canonicalization (T0.1)', () => {
  beforeEach(() => {
    mockEngineFetch.mockReset();
  });

  it('test_useInviteMember_posts_to_teams_members_plural_path', async () => {
    mockEngineFetch.mockResolvedValueOnce({ username: 'new-user', role: 'admin' });
    const client = makeClient();
    const { result } = renderHook(() => useInviteMember(), { wrapper: wrap(client) });

    result.current.mutate({ email: 'newbie@loopzero.dev', role: 'admin' });

    await waitFor(() => {
      expect(mockEngineFetch).toHaveBeenCalledTimes(1);
    });

    const [path, options] = mockEngineFetch.mock.calls[0];
    expect(path).toBe('/api/v1/teams/members');
    expect((options as RequestInit).method).toBe('POST');
    const body = JSON.parse((options as RequestInit).body as string);
    expect(body).toEqual({ email: 'newbie@loopzero.dev', role: 'admin' });
  });

  it('test_useRemoveMember_deletes_at_pluralized_path_with_encoded_id', async () => {
    mockEngineFetch.mockResolvedValueOnce({ ok: true });
    const client = makeClient();
    const { result } = renderHook(() => useRemoveMember(), { wrapper: wrap(client) });

    result.current.mutate('paulo');

    await waitFor(() => {
      expect(mockEngineFetch).toHaveBeenCalledTimes(1);
    });

    const [path, options] = mockEngineFetch.mock.calls[0];
    expect(path).toBe('/api/v1/teams/members/paulo');
    expect((options as RequestInit).method).toBe('DELETE');
  });

  it('test_useRemoveMember_url_encodes_special_chars_in_id', async () => {
    mockEngineFetch.mockResolvedValueOnce({ ok: true });
    const client = makeClient();
    const { result } = renderHook(() => useRemoveMember(), { wrapper: wrap(client) });

    // Edge: an id containing characters that demand percent-encoding.
    result.current.mutate('a b@c');

    await waitFor(() => {
      expect(mockEngineFetch).toHaveBeenCalledTimes(1);
    });

    const [path] = mockEngineFetch.mock.calls[0];
    // encodeURIComponent('a b@c') = 'a%20b%40c'
    expect(path).toBe('/api/v1/teams/members/a%20b%40c');
  });

  it('test_useUpdateMemberRole_patches_at_pluralized_path', async () => {
    mockEngineFetch.mockResolvedValueOnce({ username: 'carlos', role: 'admin' });
    const client = makeClient();
    const { result } = renderHook(() => useUpdateMemberRole(), { wrapper: wrap(client) });

    result.current.mutate({ memberId: 'carlos', role: 'admin' });

    await waitFor(() => {
      expect(mockEngineFetch).toHaveBeenCalledTimes(1);
    });

    const [path, options] = mockEngineFetch.mock.calls[0];
    expect(path).toBe('/api/v1/teams/members/carlos');
    expect((options as RequestInit).method).toBe('PATCH');
    const body = JSON.parse((options as RequestInit).body as string);
    expect(body).toEqual({ role: 'admin' });
  });

  it('test_no_call_site_uses_singular_team_members_path', () => {
    // Belt-and-suspenders: even if the hook's behavior tests pass, this
    // regression net catches a refactor that reintroduces singular paths.
    // Asserts the source file does not contain the singular literal.
    // Reads the file at test-time so the assertion is canonical.
    const useTeamModule = String.raw`${useInviteMember.toString()}${useRemoveMember.toString()}${useUpdateMemberRole.toString()}`;
    expect(useTeamModule).not.toContain('/api/v1/team/members');
    expect(useTeamModule).toContain('/api/v1/teams/members');
  });
});
