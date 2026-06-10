import { useQuery } from '@tanstack/react-query';
import { cloudFetch } from '@/lib/api';
import { queryKeys } from '@/lib/api/query-keys';

interface Project {
  name: string;
  apps?: { name: string; type: string; framework: string; status: string }[];
  last_deploy?: string;
  environment?: string;
}

interface ProjectsResponse {
  projects: Project[];
}

/**
 * use-projects (T1.2): TanStack Query migration. Drops manual TTL cache
 * (queryClient handles staleTime 60s) + inflight dedup (built-in) +
 * effect/cancellation boilerplate.
 *
 * Public signature unchanged: { projects, loading } — zero breaking em consumers.
 */
export function useProjects(): { projects: Project[]; loading: boolean } {
  const query = useQuery({
    queryKey: queryKeys.projects.list(),
    queryFn: () => cloudFetch<ProjectsResponse>('/engine/api/v1/projects'),
  });
  return {
    projects: query.data?.projects ?? [],
    loading: query.isLoading,
  };
}
