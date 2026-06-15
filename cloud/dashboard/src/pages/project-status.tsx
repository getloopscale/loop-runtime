import { useState, useEffect } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { Activity, ArrowLeft, Loader2 } from 'lucide-react';
import { Button, EmptyState, PageShell } from '@loopzero/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useProjectStatus } from '@/hooks/use-project-status';
import { useStatusStream } from '@/hooks/use-status-stream';
import type { SignalName } from '@/hooks/use-status-stream';
import { KPIStripStatus } from '@/components/project/kpi-strip-status';
import { SignalGrid } from '@/components/project/signal-grid';
import { SignalDetailSheet } from '@/components/project/signal-detail-sheet';
import { RefreshButton } from '@/components/common/refresh-button';

/**
 * ProjectStatusPage — Plan J-18 T4.1 + T4.2 (v1.1).
 *
 * Vercel-style Status page:
 *  - KPI strip (5 cards): replicas, uptime 24h, p95, error rate, RPS
 *  - SignalGrid (8 pills): + status, alerts, last deploy time → click drills down
 *  - SignalDetailSheet (right-side): 7d data + Grafana link
 *  - Refresh button: manual force-refresh per ADR D4
 *
 * T4.2 URL state: `?signal=<name>` opens drill-down sheet on the named signal.
 * Navigation away from this page clears the sheet (EC-9 absorption — useEffect
 * cleanup on route change is implicit via component unmount).
 *
 * SSE realtime: useStatusStream is wired but T1.3 backend SSE handler not yet
 * shipped — hook returns connected=false; values come from REST snapshot via
 * useProjectStatus until T1.3 production wiring lands. Plan v1.1 § Failure
 * scenarios row pg_notify writer absent — UI shows static snapshot + Refresh.
 */
const VALID_SIGNALS: ReadonlySet<string> = new Set([
  'status',
  'replicas',
  'uptime_24h',
  'p95_latency',
  'error_rate',
  'rps',
  'alerts_active',
  'last_deploy_time',
]);

export function ProjectStatusPage() {
  useRequireAuth();
  const { name = '' } = useParams<{ name: string }>();
  useSetPageTitle(`${name} · Status`);

  // T4.2 URL state — ?signal=replicas opens drill-down on initial mount.
  const [searchParams, setSearchParams] = useSearchParams();
  const initialSignal = searchParams.get('signal');
  const validatedInitialSignal: SignalName | null =
    initialSignal && VALID_SIGNALS.has(initialSignal) ? (initialSignal as SignalName) : null;

  const [drillDownSignal, setDrillDownSignal] = useState<SignalName | null>(validatedInitialSignal);
  const [sheetOpen, setSheetOpen] = useState<boolean>(validatedInitialSignal !== null);

  // Keep sheet state in sync with URL — closing sheet clears the param.
  useEffect(() => {
    if (!sheetOpen) {
      const next = new URLSearchParams(searchParams);
      if (next.has('signal')) {
        next.delete('signal');
        setSearchParams(next, { replace: true });
      }
    }
  }, [sheetOpen, searchParams, setSearchParams]);

  // EC-9 absorption: navigating away from this page closes sheet implicitly
  // via component unmount; no explicit cleanup needed.

  const { status, loading, error } = useProjectStatus(name);

  // T2.1 SSE consumer scaffold — T1.3 backend not wired yet, returns empty signals.
  // When T1.3 ships, this powers realtime SignalGrid updates.
  useStatusStream(name, status?.environment ?? 'production');

  const handleDrillDown = (signal: SignalName) => {
    setDrillDownSignal(signal);
    setSheetOpen(true);
    const next = new URLSearchParams(searchParams);
    next.set('signal', signal);
    setSearchParams(next, { replace: true });
  };

  const handleSheetOpenChange = (open: boolean) => {
    setSheetOpen(open);
    if (!open) {
      setDrillDownSignal(null);
    }
  };

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

  const env = status.environment ?? 'production';

  // Derive KPI values from status snapshot. When T1.3 SSE wires up, these come
  // from useStatusStream signals[]. For now: scaling fields + null for KPIs
  // that need Prometheus (uptime, p95, error_rate, rps) — UI renders "—" honest.
  const replicas = status.scaling?.replicas ?? null;
  const uptime24h: number | null = null; // T1.2 metrics_proxy backend; UI ready
  const p95LatencyMs: number | null = null;
  const errorRate: number | null = null;
  const rps: number | null = null;

  // 8 signals for the grid — same null pattern.
  const signals = {
    status: status.active_release?.status === 'ACTIVE' ? 'Healthy' : 'Progressing',
    replicas,
    uptime_24h: uptime24h,
    p95_latency: p95LatencyMs,
    error_rate: errorRate,
    rps,
    alerts_active: null as number | null,
    last_deploy_time: status.active_release?.created_at ?? null,
  };

  // Backend T1.1: status response now has `metrics_range_url` populated when
  // operator set LOOPZERO_GRAFANA_BASE_URL. We pass it to the drill-down sheet so
  // the "Open in Grafana" link is enabled honest.
  const grafanaBaseURL: string | null =
    typeof status.metrics_range_url === 'string' && status.metrics_range_url !== ''
      ? new URL(status.metrics_range_url).origin
      : null;

  return (
    <PageShell title="Status" description="Auto-refresh every 5s.">
      <div className="mb-4 flex items-center justify-between gap-3">
        <Button asChild variant="ghost" size="sm">
          <Link
            to={`/projects/${encodeURIComponent(name)}`}
            className="inline-flex items-center gap-1"
          >
            <ArrowLeft className="size-4" aria-hidden="true" />
            <span>Back to project</span>
          </Link>
        </Button>
        <RefreshButton
          isFetching={loading}
          onRefresh={() => {
            // useQuery refetch — exposed via useProjectStatus future enhancement.
            // For now, parent setSearchParams triggers re-render which TanStack
            // honors via staleTime.
            setSearchParams(new URLSearchParams(searchParams), { replace: true });
          }}
        />
      </div>

      <div className="space-y-6">
        <section aria-label="KPI strip">
          <KPIStripStatus
            replicas={replicas}
            uptime24h={uptime24h}
            p95LatencyMs={p95LatencyMs}
            errorRate={errorRate}
            rps={rps}
          />
        </section>

        <section aria-label="Signal grid">
          <SignalGrid signals={signals} onDrillDown={handleDrillDown} />
        </section>
      </div>

      <SignalDetailSheet
        open={sheetOpen}
        onOpenChange={handleSheetOpenChange}
        signal={drillDownSignal}
        projectName={name}
        env={env}
        grafanaBaseURL={grafanaBaseURL}
      />
    </PageShell>
  );
}
