import { useEffect, useState } from 'react';
import { cloudFetch } from '@/lib/api';

interface Project {
  name: string;
  apps?: { name: string; type: string; framework: string; status: string }[];
  last_deploy?: string;
  environment?: string;
}

interface ProjectsResponse {
  projects: Project[];
}

/**
 * use-projects — singleton SWR-style cache for /api/v1/projects.
 * Avoids duplicate fetches between TopBar (project selector) + Overview
 * (project count) + Projects page (full list).
 *
 * Per D-3 v1.1 EC-3: TopBar renders on every dashboard page, Overview
 * also fetches projects → naive impl = 2× chamadas em cada page load.
 * This hook dedups via module-level cache with 60s TTL.
 *
 * Cache key: none (single global resource). Returns null while initial
 * fetch in-flight, then populated array.
 */

const TTL_MS = 60_000;

let cache: { data: ProjectsResponse | null; timestamp: number } = {
  data: null,
  timestamp: 0,
};
let inflight: Promise<ProjectsResponse | null> | null = null;

async function fetchAndCache(): Promise<ProjectsResponse | null> {
  if (inflight) return inflight;
  inflight = cloudFetch<ProjectsResponse>('/engine/api/v1/projects').then((data) => {
    if (data) {
      cache = { data, timestamp: Date.now() };
    }
    inflight = null;
    return data;
  });
  return inflight;
}

export function useProjects(): { projects: Project[]; loading: boolean } {
  const fresh = Date.now() - cache.timestamp < TTL_MS && cache.data !== null;
  const [data, setData] = useState<ProjectsResponse | null>(fresh ? cache.data : null);
  const [loading, setLoading] = useState(!fresh);

  useEffect(() => {
    if (fresh) return;
    let cancelled = false;
    fetchAndCache().then((result) => {
      if (cancelled) return;
      setData(result);
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [fresh]);

  return { projects: data?.projects ?? [], loading };
}
