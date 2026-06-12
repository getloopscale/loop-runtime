import { useMutation, useQueryClient } from '@tanstack/react-query';
import { engineFetch } from '@/lib/api';
import { queryKeys } from '@/lib/api/query-keys';
import type { Secret } from '@/lib/api/types';

/**
 * J-22 T2.2 — Secret mutations (Vercel-canonical Add / Rotate / Delete / Bulk).
 *
 * Each mutation invalidates `queryKeys.secrets.all` on success so the
 * /secrets table refreshes immediately.
 */

export interface CreateSecretInput {
  name: string;
  value: string;
  environments: string[];
  project?: string;
}

/**
 * Create a single secret across one or more environments.
 *
 * Routes via POST /api/v1/secrets/bulk (T1.1) with a 1-key payload so the
 * server-side per-key fan-out handles multi-env atomically (audit per env
 * via T1.2; plan-limit gate via T1.3). The single-secret POST endpoint
 * expects `environment` (singular string) which the UI Sheet cannot
 * forward without losing multi-env support; using /bulk eliminates the
 * schema mismatch class.
 */
export function useCreateSecret() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateSecretInput) =>
      engineFetch('/api/v1/secrets/bulk', {
        method: 'POST',
        body: JSON.stringify({
          project: input.project,
          secrets: [
            { name: input.name, value: input.value, envs: input.environments },
          ],
        }),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.secrets.all });
    },
  });
}

export interface RotateSecretInput {
  name: string;
  newValue: string;
  /** EC-13 — server rejects 409 STALE_VERSION when expected_version doesn't match. */
  expectedVersion: number;
  environment: string;
  project?: string;
}

/** PUT /api/v1/secrets/{name} — rotate value with optimistic concurrency. */
export function useRotateSecret() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: RotateSecretInput) =>
      engineFetch<Secret>(`/api/v1/secrets/${encodeURIComponent(input.name)}`, {
        method: 'PUT',
        body: JSON.stringify({
          value: input.newValue,
          environment: input.environment,
          project: input.project,
          expected_version: input.expectedVersion,
        }),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.secrets.all });
    },
  });
}

export interface DeleteSecretInput {
  name: string;
  environment: string;
  project?: string;
}

/** DELETE /api/v1/secrets/{name}?env=&project= — remove from one env. */
export function useDeleteSecret() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: DeleteSecretInput) => {
      const params = new URLSearchParams({ environment: input.environment });
      if (input.project) params.set('project', input.project);
      return engineFetch(
        `/api/v1/secrets/${encodeURIComponent(input.name)}?${params.toString()}`,
        { method: 'DELETE' },
      );
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.secrets.all });
    },
  });
}

export interface BulkUploadInput {
  project: string;
  secrets: Array<{ name: string; value: string; envs: string[] }>;
}

export interface BulkUploadResult {
  results: Array<{
    name: string;
    status: 'created' | 'failed';
    error?: string;
    sync_status: 'pending' | 'synced' | 'failed';
  }>;
}

/** POST /api/v1/secrets/bulk — atomic batch from .env upload (T1.1 endpoint). */
export function useBulkUploadSecrets() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: BulkUploadInput) =>
      engineFetch<BulkUploadResult>('/api/v1/secrets/bulk', {
        method: 'POST',
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.secrets.all });
    },
  });
}
