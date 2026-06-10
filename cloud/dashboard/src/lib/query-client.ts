// T1.1: TanStack Query singleton — server state library.
// Substitui 7 hooks domain-specific (use-projects, use-deployments, etc.) +
// manual TTL cache em use-projects.ts. Cache + dedup + refetch on focus
// out-of-the-box.
//
// Defaults per ADR D2 (plan v1.1):
//   - staleTime 60s — matches existing use-projects.ts TTL
//   - gcTime 5min — cache survives 5min after last consumer unmounts
//   - refetchOnWindowFocus — FAANG default (tab switch = re-validate)
//   - retry 1 com exponential backoff
//   - mutations: retry 0 (never auto-retry user actions)

import { QueryClient } from '@tanstack/react-query';

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 60_000,
      gcTime: 5 * 60_000,
      refetchOnWindowFocus: true,
      refetchOnReconnect: true,
      retry: 1,
      retryDelay: (n) => Math.min(1000 * 2 ** n, 30_000),
    },
    mutations: {
      retry: 0,
    },
  },
});

/** Invalidate all queries whose key starts with the given prefix. */
export function invalidate(prefix: readonly unknown[]): Promise<void> {
  return queryClient.invalidateQueries({ queryKey: prefix });
}
