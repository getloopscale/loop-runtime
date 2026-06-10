import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { Preview, PreviewListResponse } from '@/lib/api/types';
import { engineFetch } from '@/lib/api';
import { queryKeys } from '@/lib/api/query-keys';

/**
 * use-previews (T1.2): TanStack Query migration. Filter-aware queryKey
 * (EC-05) ready para futuro URL state (T2.3).
 */
export function usePreviews(
  projectName: string | undefined,
  filters?: { status?: string },
): {
  previews: Preview[];
  loading: boolean;
  error: boolean;
  refetch: () => void;
} {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: queryKeys.previews.list(projectName, filters),
    queryFn: () => {
      const qs = filters?.status ? `?status=${encodeURIComponent(filters.status)}` : '';
      return engineFetch<PreviewListResponse>(
        `/api/v1/projects/${encodeURIComponent(projectName!)}/previews${qs}`,
      );
    },
    enabled: !!projectName,
  });
  return {
    previews: query.data?.previews ?? [],
    loading: query.isLoading,
    error: query.isError || (query.isFetched && query.data === null),
    refetch: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.previews.all });
    },
  };
}
