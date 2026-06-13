import { useParams } from 'react-router-dom';
import { ShieldCheck } from 'lucide-react';
import { EmptyState, PageShell } from '@loopzero/ui';
import { useRequireAuth } from '@/hooks/use-auth';
import { useDocumentTitle } from '@/hooks/use-page-meta';
import { useProjects } from '@/hooks/use-projects';
import { useReadinessSignals } from '@/hooks/use-readiness-signals';
import { ReadinessChecklist } from '@/components/project/readiness-checklist';

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
          title="Project not found"
          description={`No project named "${name}" exists in this team.`}
        />
      </PageShell>
    );
  }

  return (
    <PageShell
      title="Production readiness"
      description="Signals that indicate how production-ready this project is."
      loading={projectsLoading || signalsLoading}
    >
      <ReadinessChecklist signals={signals} />
    </PageShell>
  );
}
