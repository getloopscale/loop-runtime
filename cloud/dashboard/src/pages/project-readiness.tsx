import { useParams, useSearchParams } from 'react-router-dom';
import { ShieldCheck } from 'lucide-react';
import { EmptyState, PageShell } from '@loopzero/ui';
import { useRequireAuth } from '@/hooks/use-auth';
import { useDocumentTitle } from '@/hooks/use-page-meta';
import { useProjects } from '@/hooks/use-projects';
import { useReadinessSignals } from '@/hooks/use-readiness-signals';
import { ReadinessScoreCard } from '@/components/readiness/readiness-score-card';
import { ReadinessChecklist } from '@/components/readiness/readiness-checklist';
import { ReadinessFAQ } from '@/components/readiness/readiness-faq';
import type { SignalName } from '@/lib/readiness/signals';

/**
 * J-21 Readiness Score plan v1.1 T3.1 + T3.2 — ProjectReadinessPage.
 *
 * Vercel-style production readiness checklist. Composes:
 *   - useReadinessSignals (T1.1) — tristate signals + weighted 0-100 score
 *   - ReadinessScoreCard (T2.1) — KPI big-number + delta vs last render
 *   - ReadinessChecklist (T2.2) — 8 rows with status icon + Fix-it link
 *   - ReadinessFAQ (T2.3) — collapse panel explaining each signal
 *
 * Deep-link (T3.2): `?signal=<name>` expands the matching FAQ row on
 * first render (e.g., from a "What's this?" CTA elsewhere in the app).
 *
 * EC-7 absorption: Fix-it links route through the KNOWN_ROUTES allowlist
 * — unimplemented J-XX routes render "Coming soon" tooltip instead of
 * broken Link.
 */
export function ProjectReadinessPage() {
  useRequireAuth();
  const { name = '' } = useParams<{ name: string }>();
  const [searchParams] = useSearchParams();
  useDocumentTitle(name ? `${name} · Readiness` : 'Readiness');

  const { projects, loading: projectsLoading } = useProjects();
  const projectExists = projects.some((p) => p.name === name);
  const { signals, score, loading: signalsLoading } = useReadinessSignals(
    projectExists ? name : undefined,
  );

  // EC-07 from v0: project not found → explicit empty state. Only render
  // once useProjects has settled (avoid flashing "not found" during load).
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

  // T3.2 deep-link drawer: `?signal=hpa_configured` opens the matching FAQ row.
  const deepLinkSignal = (searchParams.get('signal') ?? undefined) as SignalName | undefined;

  return (
    <PageShell
      title="Production readiness"
      description="Signals that indicate how production-ready this project is."
      loading={projectsLoading || signalsLoading}
    >
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[1fr_2fr]">
        <ReadinessScoreCard score={score} projectName={name} />
        <ReadinessChecklist signals={signals} projectName={name} />
      </div>
      <div className="mt-6">
        <ReadinessFAQ defaultOpen={deepLinkSignal} />
      </div>
    </PageShell>
  );
}
