import { useQuery } from '@tanstack/react-query';
import type { Build } from '@/lib/api/types';
import { engineFetch } from '@/lib/api';
import { queryKeys } from '@/lib/api/query-keys';

/**
 * use-deployment (T1.2): TanStack Query migration. Public signature
 * unchanged: { build, loading, error }.
 */
export function useDeployment(buildId: string | undefined): {
  build: Build | null;
  loading: boolean;
  error: boolean;
} {
  const query = useQuery({
    queryKey: queryKeys.builds.detail(buildId ?? ''),
    queryFn: () => engineFetch<Build>(`/api/v1/builds/${encodeURIComponent(buildId!)}`),
    enabled: !!buildId,
  });
  return {
    build: query.data ?? null,
    loading: query.isLoading,
    error: query.isError || (query.isFetched && query.data === null),
  };
}
