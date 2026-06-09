import { Link, useParams } from 'react-router-dom';
import { ArrowLeft } from 'lucide-react';
import { Button, PageShell } from '@loopzerokit/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useProjectStatus } from '@/hooks/use-project-status';
import { ProjectHealthCard } from '@/components/project/project-health-card';
import { CardSkeleton } from '@/components/data/card-skeleton';
import { formatDate } from '@/lib/format';

/**
 * ProjectStatusPage — Plan v1.0 T2.2.
 *
 * Real-time view of project health: ProjectHealthCard with 4 signal pills,
 * last deploy timestamp, auto-refresh every 5s via useProjectStatus.
 *
 * DESIGN.md §14.2: loading replaced by layout-matching skeleton instead of
 * generic spinner. Error state surfaced via PageShell.error so retry +
 * standard chrome (Back button) remain available.
 */
export function ProjectStatusPage() {
  useRequireAuth();
  const { name = '' } = useParams<{ name: string }>();
  useSetPageTitle(`${name} · Status`);
  const { status, loading, error, refetch } = useProjectStatus(name);

  return (
    <PageShell
      title="Status"
      description="Auto-refresh every 5s."
      loading={loading}
      loadingNode={<CardSkeleton />}
      error={
        error || (!loading && !status)
          ? {
              message:
                'Status could not be fetched. The project may be unreachable or starting up — retries continue automatically every 5s.',
              onRetry: refetch,
            }
          : undefined
      }
    >
      <div className="mb-4">
        <Button asChild variant="ghost" size="sm">
          <Link
            to={`/projects/${encodeURIComponent(name)}`}
            className="inline-flex items-center gap-1"
          >
            <ArrowLeft className="size-4" aria-hidden="true" />
            <span>Back to project</span>
          </Link>
        </Button>
      </div>

      {status ? (
        <div className="flex flex-col gap-4">
          <ProjectHealthCard status={status} />
          {status.last_deploy_at && (
            <p className="text-body-sm text-muted-foreground">
              Last deploy: {formatDate(status.last_deploy_at, 'relative')}
            </p>
          )}
        </div>
      ) : null}
    </PageShell>
  );
}
