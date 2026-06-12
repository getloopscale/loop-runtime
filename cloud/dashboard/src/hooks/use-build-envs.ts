// use-build-envs.ts
//
// Plan 2026-06-08-dashboard-screens-review-fixes T4.3 + ADR D3.
// Fetches the distinct env names the project has at least one build
// for, ordered by most-recently-used. Backend endpoint (T2.2) gates
// cross-tenant probes via the EC-2 SEPA-CRITICAL ProjectStore
// adapter, so cold/wrong-tenant lookups return 404 PROJECT_NOT_FOUND.

import { useQuery } from '@tanstack/react-query';
import { engineFetch } from '@/lib/api';

interface BuildEnvsResponse {
  envs: string[];
  total: number;
}

interface UseBuildEnvsResult {
  envs: string[];
  loading: boolean;
  error: boolean;
}

/**
 * useBuildEnvs — distinct envs the project has built into, ordered by
 * most-recent build per env DESC. Feeds the /deployments page env-
 * filter chips (replaces the hardcoded production/staging/preview
 * literals per ADR D3 — custom envs like `qa` or `staging-eu-1` now
 * appear in the filter chips).
 *
 * Returns empty `envs` for cold projects (Rule 5: slice not nil from
 * backend, useQuery preserves shape via `?? []`).
 */
export function useBuildEnvs(projectName: string): UseBuildEnvsResult {
  const query = useQuery({
    queryKey: ['build-envs', projectName] as const,
    queryFn: () =>
      engineFetch<BuildEnvsResponse>(
        `/api/v1/projects/${encodeURIComponent(projectName)}/build-envs`,
      ),
    enabled: Boolean(projectName),
  });
  return {
    envs: query.data?.envs ?? [],
    loading: query.isLoading,
    error: query.isError || (query.isFetched && query.data === null),
  };
}
