import { useMutation, useQueryClient } from '@tanstack/react-query';
import { engineFetch } from '@/lib/api';

/** Environments cache key — local to this module since queryKeys.environments
 *  was not declared upstream (env list lives in pages/environments.tsx via
 *  useEngineApi today; will move to queryKeys when use-api.ts is deleted in T1.4). */
const ENVS_KEY = ['environments'] as const;

export function useDeleteEnvironment() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (envId: string) =>
      engineFetch(`/api/v1/environments/${encodeURIComponent(envId)}`, {
        method: 'DELETE',
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ENVS_KEY });
    },
  });
}

export function useCreateEnvironment() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { name: string; project_id?: string }) =>
      engineFetch('/api/v1/environments', {
        method: 'POST',
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ENVS_KEY });
    },
  });
}
