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
}

export function useProjectStatus(
  projectName: string | undefined,
  options: UseProjectStatusOptions = {},
): {
  status: ProjectStatusDetail | null;
  loading: boolean;
  error: boolean;
} {
  const { intervalMs = 5000 } = options;
  const query = useQuery({
    queryKey: queryKeys.projectStatus.detail(projectName),
    queryFn: () =>
      engineFetch<ProjectStatusDetail>(
        `/api/v1/projects/${encodeURIComponent(projectName!)}/status`,
      ),
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
