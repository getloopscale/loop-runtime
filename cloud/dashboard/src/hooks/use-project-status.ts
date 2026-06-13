import { useQuery } from '@tanstack/react-query';
import type { ProjectStatusDetail } from '@/lib/api/types';
import { engineFetch } from '@/lib/api';
import { queryKeys } from '@/lib/api/query-keys';

/**
 * use-project-status (T1.2): TanStack Query migration with refetchInterval.
 *
 * EC-09 fold: refetchIntervalInBackground: false — pausa polling quando
 * tab está hidden (battery drain em laptops + mobile/PWA).
 */
export interface UseProjectStatusOptions {
  intervalMs?: number;
  env?: string;
}

export function useProjectStatus(
  projectName: string | undefined,
  options: UseProjectStatusOptions = {},
): {
  status: ProjectStatusDetail | null;
  loading: boolean;
  error: boolean;
} {
  // Backend (api/internal/routes/status.go) defaults env to "production"
  // when the query param is absent — clients only need to pass it when
  // querying a non-prod environment (preview, staging).
  const { intervalMs = 5000, env } = options;
  const path = env
    ? `/api/v1/projects/${encodeURIComponent(projectName!)}/status?env=${encodeURIComponent(env)}`
    : `/api/v1/projects/${encodeURIComponent(projectName!)}/status`;
  const query = useQuery({
    queryKey: [...queryKeys.projectStatus.detail(projectName), env ?? 'production'],
    queryFn: () => engineFetch<ProjectStatusDetail>(path),
    enabled: !!projectName,
    refetchInterval: intervalMs,
    refetchIntervalInBackground: false,
  });
  return {
    status: query.data ?? null,
    loading: query.isLoading,
    error: query.isError || (query.isFetched && query.data === null),
  };
}
