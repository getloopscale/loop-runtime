import { useQuery } from '@tanstack/react-query';
import { cloudFetch } from '@/lib/api';
import { queryKeys } from '@/lib/api/query-keys';

interface Project {
  name: string;
  apps?: { name: string; type: string; framework: string; status: string }[];
  // last_deploy = ISO8601 timestamp of the latest build. Re-introduced
  // 2026-06-08 as additive completion of T2.1 after T5.3 LIVE-bug surfaced
  // the /projects table rendering "—" for every project's Last deploy
  // column. Backend now populates from latest_build.created_at LATERAL JOIN.
  last_deploy?: string;
  // legacy singular (pre-M2); kept for backward compat
  environment?: string;
  // T2.1 + ADR D2 (plan 2026-06-08-dashboard-screens-review-fixes):
  // server-derived per-project total env count and 5-value status enum.
  // Backend additive — legacy CLI consumers ignore unknown fields.
  environment_count?: number;
  last_deploy_status?: 'live' | 'building' | 'failed' | 'idle' | 'queued';
  // T5.3 LIVE-bug fix 2026-06-08 — additive completion of T2.1's summary
  // contract (sponsor walkthrough caught list page rendering 0/— in Apps
  // and Framework columns).
  // apps_count = jsonb_array_length(latest_build.apps_spec) — replaces
  // legacy `apps?.length ?? 0` which is always 0 since T2.1 dropped the
  // apps[] array from the listing.
  apps_count?: number;
  // latest_framework = latest_build.apps_spec[0].type — closest semantic
  // to "framework" indicator (loopzero.yaml apps schema has `type`, not
  // `framework`). Empty string when no builds OR apps_spec missing.
  latest_framework?: string;
}

interface ProjectsResponse {
  projects: Project[];
  // T2.3 pagination scaffold: unpaginated total carried in response so
  // the dashboard renders "Showing N of M" + Load more affordances.
  total?: number;
}

/**
 * use-projects (T1.2 + T4.5): TanStack Query migration. Drops manual TTL cache
 * (queryClient handles staleTime 60s) + inflight dedup (built-in) +
 * effect/cancellation boilerplate.
 *
 * T4.5 (plan 2026-06-08-dashboard-screens-review-fixes finding C.4):
 * exposes `total` from the response so /projects can render the J-17
 * "Showing N of M" + "Load more" affordance. Accepts optional `limit`
 * to bump the page window (default 50, cap 100 per T2.3 ADR D8).
 */
export function useProjects(
  options?: { limit?: number },
): { projects: Project[]; total: number; loading: boolean } {
  const limit = options?.limit ?? 50;
  const query = useQuery({
    queryKey: [...queryKeys.projects.list(), { limit }] as const,
    queryFn: () =>
      cloudFetch<ProjectsResponse>(`/engine/api/v1/projects?limit=${limit}`),
  });
  return {
    projects: query.data?.projects ?? [],
    total: query.data?.total ?? query.data?.projects?.length ?? 0,
    loading: query.isLoading,
  };
}
