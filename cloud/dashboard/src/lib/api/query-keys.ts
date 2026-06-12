// T1.2: Centralized queryKey factories for TanStack Query.
//
// EC-05 fold: list factories accept optional filter object so that URL
// state changes (e.g., status filter in T2.3) produce distinct cache keys
// = automatic refetch. Without the filter param, cache hit would return
// stale data when filters change.
//
// Naming convention: hierarchical tuples enable invalidate(['projects'])
// to nuke all project-related queries (list, detail, sub-resources).

export const queryKeys = {
  projects: {
    all: ['projects'] as const,
    list: (filters?: Record<string, unknown>) =>
      ['projects', 'list', filters ?? {}] as const,
    detail: (name: string) => ['projects', name] as const,
  },
  builds: {
    all: ['builds'] as const,
    list: (project: string | undefined, filters?: Record<string, unknown>) =>
      ['builds', 'list', project, filters ?? {}] as const,
    detail: (id: string) => ['builds', id] as const,
  },
  secrets: {
    all: ['secrets'] as const,
    list: (filters?: Record<string, unknown>) =>
      ['secrets', 'list', filters ?? {}] as const,
  },
  previews: {
    all: ['previews'] as const,
    list: (project: string | undefined, filters?: Record<string, unknown>) =>
      ['previews', 'list', project, filters ?? {}] as const,
  },
  projectStatus: {
    all: ['project-status'] as const,
    detail: (project: string | undefined) => ['project-status', project] as const,
  },
  whoami: {
    all: ['whoami'] as const,
  },
  audit: {
    all: ['audit'] as const,
    list: (filters?: Record<string, unknown>) =>
      ['audit', 'list', filters ?? {}] as const,
  },
  // Plan J-10 v1.1 — environments queryKey canonical. Replaces the legacy
  // local ENVS_KEY = ['environments'] used in use-environments.ts so
  // invalidation by canonical key works across hooks.
  environments: {
    all: ['environments'] as const,
    list: (filters?: Record<string, unknown>) =>
      ['environments', 'list', filters ?? {}] as const,
    cascadePreview: (envName: string) =>
      ['environments', 'cascade-preview', envName] as const,
  },
} as const;
