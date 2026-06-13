// use-environments.ts
//
// Plan 2026-06-11-code-review-remediation T1.4. Closes the J-22↔J-10
// silent break: the secrets page hardcoded `['production', 'staging']`
// in ENV_OPTIONS so any tenant with a custom env (`staging-eu-1`,
// `preview-pr-42`) would silently drop secrets at filter time.
//
// Source of truth: the backend `/api/v1/environments` endpoint already
// returns the canonical list per tenant (and optionally per-project
// post-M2 v1.2). This hook is the read-side counterpart of the J-10
// mutations in `hooks/mutations/use-environments.ts` — same canonical
// query-key tree, so any create/delete invalidates this read.
//
// EC-5 absorption: a fetch error MUST NOT collapse the UI; the caller
// receives `[DEFAULT_PERMANENT_ENV]` as a safe fallback so the user can
// at least see + select the permanent env that every tenant has by
// construction. The `error` flag is also exposed so the page can render
// an inline warning toast.
import { useQuery } from '@tanstack/react-query';
import { engineFetch } from '@/lib/api';
import { queryKeys } from '@/lib/api/query-keys';
import { DEFAULT_PERMANENT_ENV } from '@/lib/domain';

interface EnvironmentRow {
  name: string;
  // M2 v1.2 T5.2 (2026-06-07): per-(project, env) tuple. Empty string
  // is the legacy production entry retained for back-compat (ADR D2).
  project?: string;
}

interface EnvironmentsResponse {
  environments: EnvironmentRow[];
  // 2026-06-07 per-project preview model — `permanent` mirrors the
  // permanent env names; we read it when present (canonical), else
  // derive from `environments[]`.
  permanent?: string[];
}

interface UseEnvironmentsResult {
  envs: string[];
  loading: boolean;
  error: boolean;
}

/**
 * useEnvironments — canonical read of the tenant's environments,
 * optionally narrowed to a single project. Returns string names ready
 * to feed into dropdowns and filter chips.
 *
 * Contract:
 *   - When `projectName` is omitted: returns ALL permanent envs of
 *     the tenant (cross-project).
 *   - When `projectName` is provided: filters to environments whose
 *     `project` field matches OR whose `project` field is empty/absent
 *     (legacy production entries — back-compat ADR D2).
 *   - On error or empty response: returns `[DEFAULT_PERMANENT_ENV]` so
 *     the UI never collapses (EC-5 absorption).
 */
export function useEnvironments(projectName?: string): UseEnvironmentsResult {
  const query = useQuery({
    queryKey: [...queryKeys.environments.all, 'list-names', projectName ?? ''] as const,
    queryFn: () => engineFetch<EnvironmentsResponse>('/api/v1/environments'),
  });

  // EC-5: any error path falls back to the canonical permanent env so
  // the caller's dropdown always has at least one valid choice.
  if (query.isError || (query.isFetched && query.data === null)) {
    return { envs: [DEFAULT_PERMANENT_ENV], loading: false, error: true };
  }

  const data = query.data;
  if (!data) {
    return { envs: [], loading: query.isLoading, error: false };
  }

  // Prefer `permanent[]` when the backend emits it (canonical post-2026-06-07);
  // else derive distinct names from `environments[]`.
  let candidates: string[] = [];
  if (Array.isArray(data.permanent) && data.permanent.length > 0) {
    candidates = data.permanent.slice();
  } else if (Array.isArray(data.environments)) {
    if (projectName) {
      // Filter to entries scoped to this project OR legacy entries with
      // empty `project` (ADR D2 back-compat).
      const seen = new Set<string>();
      for (const row of data.environments) {
        if (!row.project || row.project === projectName) {
          if (!seen.has(row.name)) {
            seen.add(row.name);
            candidates.push(row.name);
          }
        }
      }
    } else {
      const seen = new Set<string>();
      for (const row of data.environments) {
        if (!seen.has(row.name)) {
          seen.add(row.name);
          candidates.push(row.name);
        }
      }
    }
  }

  // EC-5 final safety: if filtering left the list empty (e.g., a fresh
  // project with no scoped envs yet), still surface the canonical
  // permanent env so the dropdown is never empty.
  if (candidates.length === 0) {
    candidates = [DEFAULT_PERMANENT_ENV];
  }

  return {
    envs: candidates,
    loading: query.isLoading,
    error: false,
  };
}
