import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { Secret, SecretListResponse } from '@/lib/api/types';
import { engineFetch } from '@/lib/api';
import { queryKeys } from '@/lib/api/query-keys';

/**
 * use-secrets (T1.2): TanStack Query migration. Manual version counter
 * substituído por invalidateQueries. refetch() preservado para compatibilidade
 * com pages que chamavam após mutations (até T1.3 migrar mutations para
 * useMutation + onSuccess invalidate).
 */
export function useSecrets(): {
  secrets: Secret[];
  loading: boolean;
  error: boolean;
  refetch: () => void;
} {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: queryKeys.secrets.list(),
    queryFn: () => engineFetch<SecretListResponse>('/api/v1/secrets'),
  });
  return {
    secrets: query.data?.secrets ?? [],
    loading: query.isLoading,
    error: query.isError || (query.isFetched && query.data === null),
    refetch: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.secrets.all });
    },
  };
}
