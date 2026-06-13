import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { Secret } from '@/lib/api/types';
import { engineFetch } from '@/lib/api';
import { queryKeys } from '@/lib/api/query-keys';

/**
 * Raw shape returned by GET /api/v1/secrets — backend emits one row per
 * (key × env) tuple with `environment` SINGULAR. The UI groups by key so
 * each row shows all envs the secret is scoped to. LIVE 2026-06-10 catch
 * after `Secret` type lied about `environments: string[]`.
 */
interface RawSecretRow {
  name: string;
  environment: string;
  app: string;
}

interface RawSecretListResponse {
  secrets: RawSecretRow[];
}

const DEFAULT_ENV_FANOUT = ['production', 'staging'];

/**
 * use-secrets — TanStack Query.
 *
 * J-22 T2.1 + 2026-06-10 LIVE fixes:
 *   - Backend requires `project` + `env` to return non-empty results;
 *     queries fan out across known envs when the user picks "all".
 *   - Backend emits one row per (key × env); we group by `name` so the
 *     UI shows multi-env badges on a single row.
 */
export function useSecrets(opts: { project: string; env?: string }): {
  secrets: Secret[];
  loading: boolean;
  error: boolean;
  refetch: () => void;
} {
  const { project, env } = opts;
  const queryClient = useQueryClient();
  const envsToFetch = env ? [env] : DEFAULT_ENV_FANOUT;
  const query = useQuery({
    queryKey: [...queryKeys.secrets.list(), { project, env: env ?? 'all' }],
    enabled: project.length > 0,
    queryFn: async () => {
      const rows: RawSecretRow[] = [];
      for (const e of envsToFetch) {
        const params = new URLSearchParams({ project, env: e });
        const r = await engineFetch<RawSecretListResponse>(
          `/api/v1/secrets?${params.toString()}`,
        );
        if (r && Array.isArray(r.secrets)) rows.push(...r.secrets);
      }
      // Group by `name` so the UI shows one row per key with the union of envs.
      const byName = new Map<string, Secret>();
      for (const row of rows) {
        const existing = byName.get(row.name);
        if (existing) {
          if (!existing.environments.includes(row.environment)) {
            existing.environments.push(row.environment);
          }
        } else {
          byName.set(row.name, {
            name: row.name,
            environments: [row.environment],
          });
        }
      }
      return Array.from(byName.values());
    },
  });
  return {
    secrets: query.data ?? [],
    loading: query.isLoading,
    error: query.isError || (query.isFetched && query.data === null),
    refetch: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.secrets.all });
    },
  };
}
