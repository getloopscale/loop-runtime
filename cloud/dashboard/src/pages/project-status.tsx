import { Link, useParams } from 'react-router-dom';
import { Activity, ArrowLeft, Loader2 } from 'lucide-react';
import { Button, EmptyState, PageShell } from '@loopzerokit/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useProjectStatus } from '@/hooks/use-project-status';
import { ProjectHealthCard } from '@/components/project/project-health-card';
import { formatDate } from '@/lib/format';

/**
 * ProjectStatusPage — Plan v1.0 T2.2.
 *
 * Real-time view of project health: ProjectHealthCard with 4 signal pills,
 * last deploy timestamp, auto-refresh every 5s via useProjectStatus.
 */
export function ProjectStatusPage() {
  useRequireAuth();
  const { name = '' } = useParams<{ name: string }>();
  useSetPageTitle(`${name} · Status`);
  const { status, loading, error } = useProjectStatus(name);

  if (loading) {
    return (
      <PageShell title="Status">
        <div className="flex items-center justify-center py-12">
          <Loader2
            className="size-6 animate-spin text-muted-foreground"
            aria-label="Loading status"
          />
        </div>
      </PageShell>
    );
  }

  if (error || !status) {
    return (
      <PageShell title="Status unavailable">
        <EmptyState
          icon={Activity}
          title="Couldn't load project status"
          description="Status will recover when the project is reachable. Auto-refresh continues every 5s."
        />
      </PageShell>
    );
  }

  return (
    <PageShell title="Status" description={`Auto-refresh every 5s.`}>
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

      <div className="space-y-4">
        <ProjectHealthCard status={status} />
        {status.last_deploy_at && (
          <p className="text-body-sm text-muted-foreground">
            Last deploy: {formatDate(status.last_deploy_at, 'relative')}
          </p>
        )}
      </div>
    </PageShell>
  );
}
