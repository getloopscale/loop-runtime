import { useQuery } from '@tanstack/react-query';
import type { Build, BuildListResponse } from '@/lib/api/types';
import { engineFetch } from '@/lib/api';
import { queryKeys } from '@/lib/api/query-keys';

/**
 * use-deployments (T1.2): TanStack Query migration.
 *
 * EC-05 fold: accepts optional filters so that T2.3 URL state changes
 * (status filter via nuqs) generate distinct queryKey = automatic refetch.
 *
 * Public signature unchanged: { builds, loading, error }.
 */
export function useDeployments(
  projectName: string | undefined,
  filters?: { status?: string },
): {
  builds: Build[];
  loading: boolean;
  error: boolean;
} {
  const query = useQuery({
    queryKey: queryKeys.builds.list(projectName, filters),
    queryFn: () => {
      const qs = filters?.status ? `?status=${encodeURIComponent(filters.status)}` : '';
      return engineFetch<BuildListResponse>(
        `/api/v1/projects/${encodeURIComponent(projectName!)}/builds${qs}`,
      );
    },
    enabled: !!projectName,
  });
  return {
    builds: query.data?.builds ?? [],
    loading: query.isLoading,
    // null body from engineFetch = treat as error (matches legacy behavior)
    error: query.isError || (query.isFetched && query.data === null),
  };
}
