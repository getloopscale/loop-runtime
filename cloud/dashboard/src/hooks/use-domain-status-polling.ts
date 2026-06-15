import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { engineFetch } from '@/lib/api';
import type { Domain, DomainStatus } from '@/lib/api/types';

/**
 * J-11 Domains plan v1.1 T2.2 — useDomainStatusPolling
 *
 * TanStack `useQuery` with `refetchInterval=30s` while domain status is
 * non-terminal. Terminal set per ADR D2 (LOCKED):
 *
 *   - `active`    — CF zone healthy, polling stops
 *   - `failed`    — user must intervene; polling stops
 *   - `removing`  — domain being deleted; polling stops
 *
 * All OTHER statuses (`pending_verification`, `verified`, `provisioning`)
 * keep polling at 30s intervals.
 *
 * EC-6 absorption: polling pauses when `document.visibilityState === 'hidden'`
 * via the visibilitychange listener. Saves battery on long-Pending domains.
 *
 * The query key matches the canonical `useEngineApi` key for `/api/v1/domains/{name}`
 * so React Query cache stays consistent if both hooks consume the same data.
 */

const POLL_INTERVAL_MS = 30_000;

const TERMINAL_STATUSES: ReadonlySet<DomainStatus> = new Set<DomainStatus>([
  'active',
  'failed',
  'removing',
]);

export interface UseDomainStatusPollingResult {
  /** Most recent domain data — null while loading or unresolved. */
  data: Domain | null;
  /** True while initial fetch in flight. */
  loading: boolean;
  /** True if the query hit an error (network / 4xx / 5xx). */
  error: boolean;
  /**
   * True while polling is ACTIVE (status non-terminal AND document visible).
   * False when polling has stopped because the terminal state was reached
   * OR the tab is hidden. UI can render a small "Polling…" badge bound to
   * this signal.
   */
  isPolling: boolean;
}

export function useDomainStatusPolling(
  domainName: string | undefined,
): UseDomainStatusPollingResult {
  const visible = useDocumentVisible();
  const query = useQuery({
    queryKey: ['engine', `/api/v1/domains/${domainName}`] as const,
    queryFn: () => engineFetch<Domain>(`/api/v1/domains/${encodeURIComponent(domainName ?? '')}`),
    enabled: !!domainName,
    refetchInterval: (q) => {
      // Reach into the latest data via the query state.
      const latest = q.state.data as Domain | undefined;
      const status = latest?.status;
      const isTerminal = status !== undefined && TERMINAL_STATUSES.has(status);
      if (isTerminal) return false;
      if (!visible) return false;
      return POLL_INTERVAL_MS;
    },
    refetchIntervalInBackground: false,
  });

  const status = query.data?.status;
  const isTerminal = status !== undefined && TERMINAL_STATUSES.has(status);
  const isPolling = !isTerminal && visible && !!domainName;

  return {
    data: query.data ?? null,
    loading: query.isLoading,
    error: query.isError,
    isPolling,
  };
}

/**
 * EC-6 absorption — listens to visibilitychange so polling can pause when
 * the tab is hidden. Exported for hermetic test consumption.
 */
function useDocumentVisible(): boolean {
  const [visible, setVisible] = useState(() =>
    typeof document === 'undefined' ? true : document.visibilityState !== 'hidden',
  );
  useEffect(() => {
    if (typeof document === 'undefined') return;
    const onChange = () => setVisible(document.visibilityState !== 'hidden');
    document.addEventListener('visibilitychange', onChange);
    return () => document.removeEventListener('visibilitychange', onChange);
  }, []);
  return visible;
}

/** Exported for hermetic tests so the LOCKED terminal set is auditable. */
export const __test_TERMINAL_STATUSES = TERMINAL_STATUSES;
export const __test_POLL_INTERVAL_MS = POLL_INTERVAL_MS;
