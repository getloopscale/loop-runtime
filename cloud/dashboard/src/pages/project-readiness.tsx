import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, ShieldCheck } from 'lucide-react';
import { Button, EmptyState, PageShell } from '@loopzerokit/ui';
import { useRequireAuth } from '@/hooks/use-auth';
import { useDocumentTitle } from '@/hooks/use-page-meta';
import { useProjects } from '@/hooks/use-projects';
import { useReadinessSignals } from '@/hooks/use-readiness-signals';
import { ReadinessChecklist } from '@/components/project/readiness-checklist';
import { CardSkeleton } from '@/components/data/card-skeleton';

/**
 * ProjectReadinessPage (Sprint 1 T1.1) — production-fitness signals per project.
 *
 * Aggregates 8 signals from existing APIs into a readiness checklist with
 * status + actionable Fix CTAs. Backend has no new endpoints; hook composes
 * existing queries via TanStack Query (foundation from hardening Phase 1).
 *
 * EC-07: invalid projectName renders distinct empty state instead of 8
 * confusing "unknown" signals — UX clarity.
 */
export function ProjectReadinessPage() {
  useRequireAuth();
  const { name = '' } = useParams<{ name: string }>();
  useDocumentTitle(name ? `${name} · Readiness` : 'Readiness');

  const { projects, loading: projectsLoading } = useProjects();
  const projectExists = projects.some((p) => p.name === name);
  const { signals, loading: signalsLoading } = useReadinessSignals(
    projectExists ? name : undefined,
  );

  // EC-07: project not found → explicit empty state. Only render once
  // useProjects has settled (avoid flashing "not found" during load).
  if (!projectsLoading && !projectExists) {
    return (
      <PageShell title="Production readiness">
        <EmptyState
          icon={ShieldCheck}
          title={`No project named "${name}"`}
          description="The project may have been deleted or the URL is wrong. Other projects in your workspace are still available."
          action={
            <Button asChild variant="secondary">
              <Link to="/projects" className="inline-flex items-center gap-1">
                <ArrowLeft className="size-4" aria-hidden="true" />
                Back to projects
              </Link>
            </Button>
          }
        />
      </PageShell>
    );
  }

  return (
    <PageShell
      title="Production readiness"
      description="Signals that indicate how production-ready this project is."
      loading={projectsLoading || signalsLoading}
      loadingNode={
        <div className="flex flex-col gap-4">
          <CardSkeleton />
          <CardSkeleton />
          <CardSkeleton />
        </div>
      }
    >
      <ReadinessChecklist signals={signals} />
    </PageShell>
  );
}
