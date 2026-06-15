import { useMutation, useQueryClient } from '@tanstack/react-query';
import { engineFetch } from '@/lib/api';
import { queryKeys } from '@/lib/api/query-keys';
import type { ScaleInput, ScaleResponse } from '@/lib/api/types';

/**
 * useScale — J-20 Scale plan v1.1 T2.1.
 *
 * Submits a scaling mutation against POST /api/v1/projects/{name}/scale.
 * The backend wraps the persistence step in withEnvironmentLock per
 * CLAUDE.md Concurrency Model — concurrent calls on the same
 * (tenant, project, env) serialize at the DB layer (EC-5 absorption).
 *
 * Typed error surfaces the dashboard handles:
 *
 *   - 402 PLAN_UPGRADE_REQUIRED — Starter tenant requested KEDA
 *     scale-to-zero (min=0). EC-3 — render upgrade CTA.
 *   - 400 HPA_TARGET_OUT_OF_BOUNDS — cpu/memory target outside
 *     [10,95] / [10,90]. EC-4 — surface field-specific inline error.
 *   - 400 INVALID_REQUEST — replicas + autoscaling fields mixed (ADR D2),
 *     or scale-to-zero requested while cluster KEDA is disabled.
 *   - 404 PROJECT_NOT_FOUND — typed when name is wrong or cross-tenant.
 *
 * On success the project status cache is invalidated so the dashboard's
 * status hero reflects the new replica count without waiting for the
 * SSE stream to repaint.
 */
export function useScale(projectName: string) {
  const queryClient = useQueryClient();
  return useMutation<ScaleResponse, Error, ScaleInput>({
    mutationFn: (input: ScaleInput) =>
      engineFetch<ScaleResponse>(`/api/v1/projects/${projectName}/scale`, {
        method: 'POST',
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      // Invalidate project status + scale entries so the UI repaints.
      queryClient.invalidateQueries({ queryKey: queryKeys.projectStatus.all });
      queryClient.invalidateQueries({ queryKey: queryKeys.scale.all });
    },
  });
}
