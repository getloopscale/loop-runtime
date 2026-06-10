import { useMutation, useQueryClient } from '@tanstack/react-query';
import { engineFetch } from '@/lib/api';

/**
 * useSwitchTeam (T1.3 + EC-02): POST /api/v1/auth/team/switch and nuke
 * ALL caches via queryClient.clear() — not just whoami invalidate.
 *
 * Why clear() not invalidateQueries(['whoami']):
 * EVERY tenant-scoped resource (projects, secrets, deployments, audit,
 * billing, env) is invalid after team switch. invalidating one-by-one
 * leaks stale data into the new team context for any cache that consumer
 * forgets to invalidate. clear() is the only safe choice.
 *
 * Settings.tsx had hardcoded "team-acme" bug before — wrong team mutations
 * shipped. EC-02 closes the systemic gap.
 */
export function useSwitchTeam() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (teamId: string) =>
      engineFetch('/api/v1/auth/team/switch', {
        method: 'POST',
        body: JSON.stringify({ team_id: teamId }),
      }),
    onSuccess: () => {
      queryClient.clear();
    },
  });
}
