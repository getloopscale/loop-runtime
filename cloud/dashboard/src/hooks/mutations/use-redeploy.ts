import { useMutation, useQueryClient } from '@tanstack/react-query';
import { engineFetch } from '@/lib/api';
import { queryKeys } from '@/lib/api/query-keys';
import type { BuildCreateResponse } from '@/lib/api/types';

/**
 * useRedeploy — Plan J-17 T6.4.
 *
 * Submits a redeploy of a terminal source build. The backend looks up
 * the source row, verifies tenant ownership + terminal status, copies
 * commit_sha + environment + apps_spec, and submits a new build.
 *
 * On success the deployments list cache is invalidated so the new
 * build appears immediately (the SSE list stream will also push the
 * row, but the invalidate makes the UX feel snappy without waiting
 * for the round-trip).
 *
 * Wire-format note: the request shape mirrors the existing POST /builds
 * contract; only `redeploy_of` is set. The body's other fields are
 * intentionally absent — backend handler treats redeploy_of as the
 * source of truth.
 *
 * Error handling: the typed errors that surface are
 *   - BUILD_NOT_TERMINAL (409): source build still in flight
 *   - BUILD_NOT_FOUND (404): wrong id or cross-tenant
 *   - DEPLOY_IN_PROGRESS (409): another deploy is already running on
 *     the same environment (concurrent-build guard)
 */
export function useRedeploy() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (sourceBuildId: string) =>
      engineFetch<BuildCreateResponse>(`/api/v1/builds`, {
        method: 'POST',
        body: JSON.stringify({ redeploy_of: sourceBuildId }),
      }),
    onSuccess: () => {
      // Invalidate the active list query so the new build row appears
      // on the deployments page without waiting for the SSE stream.
      queryClient.invalidateQueries({ queryKey: queryKeys.builds.all });
    },
  });
}
