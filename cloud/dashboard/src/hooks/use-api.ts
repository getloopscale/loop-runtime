import { useEffect, useState } from 'react';
import { cloudFetch, engineFetch } from '@/lib/api';

interface UseApiResult<T> {
  data: T | null;
  loading: boolean;
  error: boolean;
}

// Fetch from cloud/api service
export function useCloudApi<T>(path: string): UseApiResult<T> {
  return useApiFetch<T>(() => cloudFetch<T>(path), path);
}

// Fetch from engine API
export function useEngineApi<T>(path: string): UseApiResult<T> {
  return useApiFetch<T>(() => engineFetch<T>(path), path);
}

function useApiFetch<T>(fetcher: () => Promise<T | null>, key: string): UseApiResult<T> {
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(false);

    fetcher().then(result => {
      if (cancelled) return;
      if (result === null) {
        setError(true);
      } else {
        setData(result);
      }
      setLoading(false);
    });

    return () => { cancelled = true; };
  }, [key]);

  return { data, loading, error };
}
