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
 * 2026-06-06 polish: forwards `limit` to the backend (`?limit=N`,
 * server-capped at 50 per request) and surfaces the backend `total`
 * plus a manual `refetch` so the deployments page can offer a "Load
 * more" button + an explicit Refresh control. The limit value is part
 * of the queryKey so a bump produces a clean cache miss + refetch.
 */
export function useDeployments(
  projectName: string | undefined,
  filters?: { status?: string; env?: string; limit?: number },
): {
  builds: Build[];
  total: number;
  loading: boolean;
  error: boolean;
  refetch: () => void;
  fetching: boolean;
} {
  const query = useQuery({
    queryKey: queryKeys.builds.list(projectName, filters),
    queryFn: () => {
      const params = new URLSearchParams();
      if (filters?.status) params.set('status', filters.status);
      if (filters?.env) params.set('env', filters.env);
      if (filters?.limit && filters.limit > 0) params.set('limit', String(filters.limit));
      const qs = params.toString() ? `?${params.toString()}` : '';
      return engineFetch<BuildListResponse>(
        `/api/v1/projects/${encodeURIComponent(projectName!)}/builds${qs}`,
      );
    },
    enabled: !!projectName,
  });
  return {
    builds: query.data?.builds ?? [],
    total: query.data?.total ?? 0,
    loading: query.isLoading,
    // null body from engineFetch = treat as error (matches legacy behavior)
    error: query.isError || (query.isFetched && query.data === null),
    refetch: () => {
      void query.refetch();
    },
    fetching: query.isFetching,
  };
}
