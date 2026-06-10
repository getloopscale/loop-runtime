import { useEngineApi } from '@/hooks/use-api';
import { useDeployments } from '@/hooks/use-deployments';
import { useProjects } from '@/hooks/use-projects';
import { useProjectStatus } from '@/hooks/use-project-status';
import type { ReadinessSignal } from '@/components/project/readiness-checklist';

interface DomainSummary {
  domain: string;
  project: string;
  status: string;
}
interface DomainsResponse { domains: DomainSummary[] }
interface SecretsResponse { secrets: { name: string }[] }

/**
 * useReadinessSignals (Sprint 1 T1.3) — composes 5 existing TanStack queries
 * into 8 production-fitness signals. Per ADR D4, NO new backend endpoints.
 *
 * EC-01: signal 6 (last build) uses null-safe access. builds=[] returns
 * 'unknown' instead of throwing on builds[0].status.
 *
 * EC-08: domain signals filter by project name to avoid showing global
 * domain status as project-specific.
 *
 * Returns { signals, loading } — loading=true while any sub-query pending.
 */
export function useReadinessSignals(projectName: string | undefined): {
  signals: ReadinessSignal[];
  loading: boolean;
} {
  const { projects, loading: projectsLoading } = useProjects();
  const { data: domainsData, loading: domainsLoading } =
    useEngineApi<DomainsResponse>('/api/v1/domains');
  const { data: secretsData, loading: secretsLoading } =
    useEngineApi<SecretsResponse>('/api/v1/secrets');
  const { builds, loading: buildsLoading } = useDeployments(projectName);
  const { status, loading: statusLoading } = useProjectStatus(projectName);

  const loading =
    projectsLoading || domainsLoading || secretsLoading || buildsLoading || statusLoading;

  // EC-08: filter domains by project name
  const projectDomains = (domainsData?.domains ?? []).filter(
    (d) => d.project === projectName,
  );
  const verifiedDomains = projectDomains.filter(
    (d) => d.status === 'active' || d.status === 'verified',
  );
  const projectExists = !!projects.find((p) => p.name === projectName);

  // EC-01: null-safe access — projects without builds don't crash signal 6.
  const lastBuild = builds[0];

  const signals: ReadinessSignal[] = [
    {
      id: 'project-exists',
      title: 'Project registered',
      description: 'Project record exists in this team workspace.',
      status: projectExists ? 'ok' : 'fail',
    },
    {
      id: 'domain-attached',
      title: 'Custom domain attached',
      description: 'At least one custom domain is associated with this project.',
      status: projectDomains.length > 0 ? 'ok' : 'warning',
      fixHref: projectDomains.length === 0 ? '/domains' : undefined,
      fixLabel: projectDomains.length === 0 ? 'Add domain' : undefined,
    },
    {
      id: 'domain-verified',
      title: 'Custom domain verified',
      description: 'At least one domain has completed DNS verification.',
      status: projectDomains.length === 0
        ? 'unknown'
        : verifiedDomains.length > 0
          ? 'ok'
          : 'warning',
    },
    {
      id: 'https-enforced',
      title: 'HTTPS enforced',
      description: 'Traffic to your project is encrypted by default.',
      status: 'ok',
    },
    {
      id: 'secrets-defined',
      title: 'Secrets defined',
      description: 'Runtime secrets are available to the project workload.',
      status: (secretsData?.secrets?.length ?? 0) > 0 ? 'ok' : 'warning',
      fixHref: (secretsData?.secrets?.length ?? 0) === 0 ? `/projects/${projectName}/secrets` : undefined,
      fixLabel: (secretsData?.secrets?.length ?? 0) === 0 ? 'Add secret' : undefined,
    },
    {
      id: 'last-build',
      title: 'Last build succeeded',
      description: 'The most recent build for this project completed without errors.',
      // EC-01 fix: null-safe access
      status: !lastBuild ? 'unknown' : lastBuild.status === 'succeeded' ? 'ok' : 'fail',
      fixHref: lastBuild && lastBuild.status !== 'succeeded'
        ? `/projects/${projectName}/deployments`
        : undefined,
      fixLabel: lastBuild && lastBuild.status !== 'succeeded' ? 'View build' : undefined,
    },
    {
      id: 'deployment-healthy',
      title: 'Deployment healthy',
      description: 'All requested replicas are ready and serving traffic.',
      status: !status
        ? 'unknown'
        : status.replicas_desired === 0
          ? 'unknown' // KEDA scaled to zero — informational
          : status.replicas_ready === status.replicas_desired
            ? 'ok'
            : status.replicas_ready === 0
              ? 'fail'
              : 'warning',
      fixHref: status && status.replicas_ready === 0 && status.replicas_desired > 0
        ? `/projects/${projectName}/status`
        : undefined,
      fixLabel: status && status.replicas_ready === 0 && status.replicas_desired > 0
        ? 'View status'
        : undefined,
    },
    {
      id: 'auto-scaling',
      title: 'Auto-scaling configured',
      description: 'More than one replica desired — protects against single-pod failure.',
      status: !status || status.replicas_desired === undefined
        ? 'unknown'
        : status.replicas_desired > 1
          ? 'ok'
          : 'warning',
      fixHref: status && status.replicas_desired === 1
        ? `/projects/${projectName}/scale`
        : undefined,
      fixLabel: status && status.replicas_desired === 1 ? 'Scale up' : undefined,
    },
  ];

  return { signals, loading };
}
