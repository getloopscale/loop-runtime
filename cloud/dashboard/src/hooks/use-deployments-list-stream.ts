import { useEffect, useMemo, useRef } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useDeployments } from './use-deployments';
import { useSSEStream } from './use-sse-stream';
import type { Build, BuildStatus } from '@/lib/api/types';
import { normalizeBuildStatus } from '@/lib/api/types';
import { queryKeys } from '@/lib/api/query-keys';

// Terminal statuses are absorbing — once a build hits one of these, the
// list reducer must never let an in-buffer SSE event regress it.
//
// LIVE bug caught 2026-06-06: sponsor saw `Running` appear on rows of
// builds that had been SUCCEEDED for minutes. Root cause: useSSEStream
// keeps a FIFO buffer of up to 200 events; on SSE replay/resume the
// server can retransmit `build.started{X}` events whose paired
// `build.succeeded{X}` was already evicted from the buffer cap. The
// reducer then applied `started_X` again and overwrote the snapshot's
// SUCCEEDED status with `running` — visually moving the Running pill
// onto a finished row, on every fresh event arrival.
const TERMINAL_STATUSES = new Set<BuildStatus>([
  'succeeded',
  'failed',
  'cancelled',
]);

/**
 * useDeploymentsListStream — Plan J-17 T2.3.
 *
 * Replaces interval polling on the deployments list page. Architecture:
 *
 *   1. Initial REST snapshot via useDeployments() — Build[] with full
 *      metadata (commit_sha, environment, created_at, ...).
 *   2. SSE subscription to /engine/api/v1/projects/{name}/builds/stream — receives
 *      build.started / build.succeeded / build.failed events.
 *   3. Reducer merges events into the snapshot Map:
 *        - unknown build_id on build.started => trigger React Query
 *          invalidation so a fresh snapshot pulls the new Build row.
 *          (SSE payload is intentionally minimal per EC-2 pg_notify 8KB
 *          limit; the row arrives via REST refetch.)
 *        - known build_id on build.succeeded / build.failed => patch
 *          status + duration in place, no refetch needed.
 *
 * The hook is the polling killer — once mounted, the list never asks
 * the API on a timer. New deploys + status transitions arrive over SSE.
 */

interface BuildLifecyclePayload {
  build_id?: string;
  status?: string;
  error?: string;
  duration_seconds?: number;
}

export interface UseDeploymentsListStreamResult {
  builds: Build[];
  /** Backend total (independent of the current page size). */
  total: number;
  loading: boolean;
  error: boolean;
  connected: boolean;
  lastEventID: string | null;
  /**
   * Whether the REST snapshot is currently fetching (initial OR refetch).
   * Distinct from `loading` which is only true on the first fetch.
   */
  fetching: boolean;
  /**
   * Force a fresh REST snapshot. Sponsor UX: explicit Refresh button.
   * Does NOT close or reopen the SSE — live events keep flowing while
   * the snapshot reloads, and the SSE buffer merges with the new
   * snapshot on the next render cycle.
   */
  refetch: () => void;
}

const STREAM_EVENT_TYPES = [
  'build.started',
  'build.succeeded',
  'build.failed',
];

export function useDeploymentsListStream(
  projectName: string | null | undefined,
  filters?: { status?: string; env?: string; limit?: number },
): UseDeploymentsListStreamResult {
  const queryClient = useQueryClient();
  const initial = useDeployments(projectName ?? undefined, filters);

  // LIVE bug caught 2026-06-06 J-17 dogfood: same /engine/ prefix
  // requirement as use-deployment-stream. Caddy + cloud BFF only routes
  // /engine/* to loopzero-api.
  const streamPath = projectName
    ? `/engine/api/v1/projects/${encodeURIComponent(projectName)}/builds/stream`
    : null;

  const stream = useSSEStream(streamPath, {
    eventTypes: STREAM_EVENT_TYPES,
    // List view fan-in is small: 14 step events * N builds is the cap on
    // build-level streams; list-level only sees lifecycle (3 per build).
    // 200 covers ~60 builds before FIFO begins to drop, comfortably above
    // the page size.
    maxEvents: 200,
  });

  // Track which build_ids we've already triggered a refetch for to avoid
  // a refetch storm when the snapshot is still loading and many events
  // arrive in burst.
  const refetchedFor = useRef<Set<string>>(new Set());

  // Side-effect: any unknown-build-id event triggers exactly one query
  // invalidation per build_id. Subsequent events for the same id are
  // patched in place by the reducer below.
  useEffect(() => {
    if (!projectName || stream.events.length === 0) return;
    const known = new Set(initial.builds.map((b) => b.build_id));
    let needsRefetch = false;
    for (const ev of stream.events) {
      const data = ev.data as BuildLifecyclePayload;
      const id = data.build_id;
      if (!id) continue;
      if (ev.type === 'build.started' && !known.has(id) && !refetchedFor.current.has(id)) {
        refetchedFor.current.add(id);
        needsRefetch = true;
      }
    }
    if (needsRefetch) {
      void queryClient.invalidateQueries({
        queryKey: queryKeys.builds.list(projectName, filters),
      });
    }
  }, [stream.events, projectName, filters, initial.builds, queryClient]);

  const builds = useMemo(() => {
    const map = new Map<string, Build>();
    for (const b of initial.builds) {
      map.set(b.build_id, b);
    }
    for (const ev of stream.events) {
      const data = ev.data as BuildLifecyclePayload;
      const id = data.build_id;
      if (!id) continue;
      const prior = map.get(id);
      // Status mapping — backend canonical statuses are 'queued', 'running',
      // 'succeeded', 'failed', 'cancelled' (Build.status type union).
      let nextStatus: Build['status'] | undefined;
      if (ev.type === 'build.started') {
        nextStatus = 'running';
      } else if (ev.type === 'build.succeeded') {
        nextStatus = 'succeeded';
      } else if (ev.type === 'build.failed') {
        nextStatus = 'failed';
      }
      if (!prior) {
        // Unknown build_id and snapshot hasn't caught up yet. We DON'T
        // synthesize a partial Build — the refetch effect above will
        // resolve this. Skip until snapshot returns.
        continue;
      }
      // Absorbing terminal guard: don't let a replayed `build.started`
      // regress a build that the snapshot (REST) already shows as
      // terminal. See the TERMINAL_STATUSES comment at the top of the
      // file for the LIVE failure mode this guards against.
      const priorNs = normalizeBuildStatus(prior.status);
      if (
        TERMINAL_STATUSES.has(priorNs) &&
        nextStatus !== undefined &&
        !TERMINAL_STATUSES.has(nextStatus)
      ) {
        continue;
      }
      map.set(id, {
        ...prior,
        status: nextStatus ?? prior.status,
        error: data.error ?? prior.error,
        duration_seconds:
          data.duration_seconds !== undefined ? data.duration_seconds : prior.duration_seconds,
      });
    }
    return Array.from(map.values()).sort(
      (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
    );
  }, [initial.builds, stream.events]);

  return {
    builds,
    total: initial.total,
    loading: initial.loading,
    error: initial.error,
    connected: stream.connected,
    lastEventID: stream.lastEventID,
    fetching: initial.fetching,
    refetch: initial.refetch,
  };
}
