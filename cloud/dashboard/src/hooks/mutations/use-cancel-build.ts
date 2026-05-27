import { useMutation, useQueryClient } from '@tanstack/react-query';
import { engineFetch } from '@/lib/api';
import { queryKeys } from '@/lib/api/query-keys';

/** Cancel a running build. Invalidates builds list + the specific build detail. */
export function useCancelBuild() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (buildId: string) =>
      engineFetch(`/api/v1/builds/${encodeURIComponent(buildId)}/cancel`, {
        method: 'POST',
      }),
    onSuccess: (_data, buildId) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.builds.all });
      queryClient.invalidateQueries({ queryKey: queryKeys.builds.detail(buildId) });
    },
  });
}
