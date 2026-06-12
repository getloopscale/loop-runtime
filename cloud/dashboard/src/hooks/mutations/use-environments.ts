import { useMutation, useQueryClient } from '@tanstack/react-query';
import { engineFetch } from '@/lib/api';
import { queryKeys } from '@/lib/api/query-keys';

/**
 * Environments mutations — Plan J-10 v1.1.
 *
 * Backend endpoints (LIVE-verified T0.1 Recon 2026-06-06):
 *   - POST   /api/v1/environments              → 201 {name, status}
 *   - DELETE /api/v1/environments/{name}       → 200 {deleted}
 *   - POST   /api/v1/projects/{name}/promote   → 200 {from, to, ...}
 *
 * v1.1 changes:
 *   - All hooks invalidate `queryKeys.environments.all` (canonical key)
 *     instead of the legacy ad-hoc `['environments']`. v1.0 used a local
 *     ENVS_KEY because queryKeys.environments did not exist; v1.1 added
 *     it (see lib/api/query-keys.ts).
 *   - usePromoteEnvironment NEW — pattern de useRedeploy applied to the
 *     promote endpoint with {from, to} body.
 */

/**
 * useDeleteEnvironment — M2 v1.2 T5.3 (2026-06-07).
 *
 * Accepts both legacy string-only (back-compat for v1.0 callers) and the
 * canonical `{ name, project }` shape post-M2. When `project` is non-empty
 * the DELETE URL appends `?project=<slug>` so the backend filters by
 * (name, project) tuple — removing `staging+web` no longer touches
 * `staging+api`. Empty project preserves the production-legacy lookup
 * (ADR D2).
 */
export function useDeleteEnvironment() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: string | { name: string; project?: string }) => {
      const params = typeof input === 'string' ? { name: input } : input;
      const url = `/api/v1/environments/${encodeURIComponent(params.name)}`;
      const query = params.project ? `?project=${encodeURIComponent(params.project)}` : '';
      return engineFetch(`${url}${query}`, { method: 'DELETE' });
    },
    onSuccess: () => {
      // Canonical key tree for downstream consumers using queryKeys.environments.
      queryClient.invalidateQueries({ queryKey: queryKeys.environments.all });
      // LIVE catch 2026-06-06: pages today fetch via useEngineApi which uses
      // key `['engine', '/api/v1/environments']` (different tree). Without
      // a second invalidation, the table stays stale after delete/create.
      queryClient.invalidateQueries({ queryKey: ['engine'] });
    },
  });
}

/**
 * useCreateEnvironment — M2 v1.2 T5.1.
 *
 * Body shape (canonical post-M2): `{ name, project }` where project is the
 * project slug (NOT UUID). Backend returns 400 MISSING_FIELD when project
 * is missing — defense at the API layer; this hook is the second defence.
 */
export function useCreateEnvironment() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { name: string; project: string; protected?: boolean }) =>
      engineFetch('/api/v1/environments', {
        method: 'POST',
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      // Canonical key tree for downstream consumers using queryKeys.environments.
      queryClient.invalidateQueries({ queryKey: queryKeys.environments.all });
      // LIVE catch 2026-06-06: pages today fetch via useEngineApi which uses
      // key `['engine', '/api/v1/environments']` (different tree). Without
      // a second invalidation, the table stays stale after delete/create.
      queryClient.invalidateQueries({ queryKey: ['engine'] });
    },
  });
}

/**
 * usePromoteEnvironment — Plan J-10 v1.1 T1.3 (NEW).
 *
 * Promotes the active release of `from` env into `to` env (re-deploy with
 * same commit_sha, env-specific context). Backend handler is at
 * POST /api/v1/projects/{projectName}/promote with body {from, to}.
 *
 * The handler:
 *   - Verifies the source env has a terminal (ACTIVE) release.
 *   - Submits a new build cloning commit_sha + apps_spec from source.
 *   - On success the source release transitions to SUPERSEDED + the new
 *     release in target env transitions to ACTIVE atomically.
 *
 * Typed errors (UI maps to product-language banners per blueprint § 4.6):
 *   - BUILD_NOT_TERMINAL (409): source release still building
 *   - DEPLOY_IN_PROGRESS (409): another deploy in flight on target
 *   - PROJECT_NOT_FOUND  (404): wrong project name or cross-tenant
 *   - ENV_NOT_FOUND      (404): source or target env missing → UI surfaces
 *     "+ Create new..." sentinel per ADR D3 (create-on-the-fly)
 *
 * On success the environments list cache is invalidated so the KPI row
 * reflects the promoted release immediately.
 */
export function usePromoteEnvironment(projectName: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { from: string; to: string }) =>
      engineFetch(`/api/v1/projects/${encodeURIComponent(projectName)}/promote`, {
        method: 'POST',
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.environments.all });
      queryClient.invalidateQueries({ queryKey: queryKeys.builds.all });
      // LIVE catch 2026-06-06: useEngineApi key tree differs from canonical;
      // see useDeleteEnvironment comment for context.
      queryClient.invalidateQueries({ queryKey: ['engine'] });
    },
  });
}
