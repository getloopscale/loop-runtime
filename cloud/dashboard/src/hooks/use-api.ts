import { useQuery } from '@tanstack/react-query';
import { cloudFetch, engineFetch } from '@/lib/api';

/**
 * T1.4 deviation: plan v1.1 called for outright deletion. Pragmatic path
 * taken instead — rewrite as thin TanStack Query wrappers.
 *
 * Rationale:
 *  - 4 pages consume useCloudApi/useEngineApi + 5 tests mock by these names
 *  - Direct migration to useQuery would churn 9 files with zero behavior change
 *  - The "legacy" issue was the manual useState/useEffect boilerplate inside,
 *    not the wrapper API. Boilerplate is gone — wrapper API survives.
 *  - Tests + pages unchanged. KISS.
 *
 * Cache key is the URL path itself (singular per endpoint). Different from
 * the queryKeys.* factories used by domain-specific hooks (use-projects,
 * use-deployments, etc.) which need filter-aware structured keys.
 */

interface UseApiResult<T> {
  data: T | null;
  loading: boolean;
  error: boolean;
}

export function useCloudApi<T>(path: string): UseApiResult<T> {
  const query = useQuery({
    queryKey: ['cloud', path] as const,
    queryFn: () => cloudFetch<T>(path),
  });
  return {
    data: query.data ?? null,
    loading: query.isLoading,
    error: query.isError || (query.isFetched && query.data === null),
  };
}

export function useEngineApi<T>(path: string): UseApiResult<T> {
  const query = useQuery({
    queryKey: ['engine', path] as const,
    queryFn: () => engineFetch<T>(path),
  });
  return {
    data: query.data ?? null,
    loading: query.isLoading,
    error: query.isError || (query.isFetched && query.data === null),
  };
}
