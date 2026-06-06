import { useEffect } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { BookOpen, Brain, Code2, Database, FlaskConical, RefreshCw, Settings as SettingsIcon, Sparkles, Zap } from 'lucide-react';
import {
  Alert,
  Button,
  Card,
  PageShell,
  StatTile,
  TokenUsageChart,
  type TokenUsagePoint,
} from '@loopzero/ui';
import { ContentGrid } from '@/components/layout';
import { usePageMeta } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { cloudFetch } from '@/lib/api';

/**
 * Plan 1 of dashboard-memory-section-roadmap.md, T1.6.
 *
 * /memory landing page — the first customer-visible Memory surface. Renders
 * 4 KPIs from GET /v1/memory/metrics + 2 trend charts + a Refresh button.
 *
 * Per ADR D3 of the plan: empty pgvector returns 0 from every numeric field
 * (never null, never NaN). This page renders that 0 cleanly via StatTile —
 * the first-time user sees four "0" cards explaining what's coming, not
 * broken NaN tiles.
 *
 * Trend chart points are derived from the backend metrics for now (one
 * data point per refresh; multi-day trend wired by P2 once we have a
 * usage-aggregator background job).
 */

interface MemoryMetrics {
  total_memories: number;
  retrieval_api_usage: number;
  add_events: number;
  entity_count: number;
}

export function MemoryPage() {
  const { setPageTitle } = usePageMeta();
  useEffect(() => setPageTitle('Memory'), [setPageTitle]);
  useRequireAuth();
  const queryClient = useQueryClient();

  const metricsQuery = useQuery({
    queryKey: ['cloud', '/v1/memory/metrics'] as const,
    queryFn: () => cloudFetch<MemoryMetrics>('/v1/memory/metrics'),
  });

  // D3: even if the backend hiccups and returns null, fall back to zeros
  // so the dashboard never renders NaN.
  const metrics: MemoryMetrics = metricsQuery.data ?? {
    total_memories: 0,
    retrieval_api_usage: 0,
    add_events: 0,
    entity_count: 0,
  };

  const onRefresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['cloud', '/v1/memory/metrics'] });
  };

  // Placeholder trend until a P2-era usage aggregator ships. Plotting
  // today's snapshot once is honest "you haven't been tracking yet" UX.
  const trendPoints: TokenUsagePoint[] = [
    { label: 'Today', input: metrics.add_events, output: metrics.retrieval_api_usage },
  ];
  const entityTrendPoints: TokenUsagePoint[] = [
    { label: 'Today', input: metrics.entity_count, output: metrics.total_memories },
  ];

  return (
    <PageShell
      title="Memory"
      description="Your agent's persistent memory — what it knows, who it remembers, and how often."
      primaryAction={{
        label: 'Refresh',
        icon: RefreshCw,
        onClick: onRefresh,
        loading: metricsQuery.isFetching,
      }}
    >
      {metricsQuery.isError ? (
        <Alert intent="warning" title="Could not load memory metrics">
          The Memory service is not reachable right now. Try refreshing in a moment.
        </Alert>
      ) : null}

      {/* KPI strip — 4 tiles. ContentGrid only supports up-to-3 columns,
          so the KPI strip uses a direct Tailwind grid (2 cols on mobile,
          4 on desktop) — matches the existing billing.tsx pattern for
          dense KPI rows. */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <StatTile
          value={metrics.total_memories}
          label="Total memories"
          icon={Brain}
        />
        <StatTile
          value={metrics.retrieval_api_usage}
          label="Retrieval API calls"
          icon={Zap}
        />
        <StatTile
          value={metrics.add_events}
          label="Add events"
          icon={Sparkles}
        />
        <StatTile
          value={metrics.entity_count}
          label="Distinct entities"
          icon={Database}
        />
      </div>

      <ContentGrid columns="2-equal">
        <Card>
          <TokenUsageChart
            title="Usage trend"
            points={trendPoints}
            height={180}
          />
        </Card>
        <Card>
          <TokenUsageChart
            title="Entities trend"
            points={entityTrendPoints}
            height={180}
          />
        </Card>
      </ContentGrid>

      <Card>
        <div className="p-4 space-y-2">
          <h2 className="text-base font-semibold">Getting started</h2>
          <p className="text-sm text-muted-foreground">
            Use the SDK to remember facts from your agent. Anything you write
            here is queryable via <code>loopzero.memory.user(&apos;u&apos;).recall()</code>.
          </p>
          <Button variant="ghost" onClick={() => (window.location.href = '/memory/memories')}>
            Browse memories →
          </Button>
        </div>
      </Card>

      {/* Plan 2 of dashboard-memory-section-roadmap.md, T2.4 — Explore grid.
          4 next-step CTAs surfaced once the KPIs are visible. Keeps the
          first-run customer engaged with concrete next actions, not just
          a wall of zeros. */}
      <h2 className="text-base font-semibold pt-2">Explore</h2>
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        <ExploreCard
          icon={SettingsIcon}
          title="Customize"
          description="Tune memory categories and lifecycle rules."
          href="/settings"
        />
        <ExploreCard
          icon={Code2}
          title="Integration examples"
          description="Drop-in SDK snippets for your stack."
          href="/api-keys"
        />
        <ExploreCard
          icon={FlaskConical}
          title="Try Playground"
          description="Remember and recall live, no code required."
          href="/memory/memories"
        />
        <ExploreCard
          icon={BookOpen}
          title="Documentation"
          description="Read the architecture + every endpoint."
          href="/memory/entities"
        />
      </div>
    </PageShell>
  );
}

interface ExploreCardProps {
  icon: typeof Brain;
  title: string;
  description: string;
  href: string;
}

/**
 * Single Explore card. Renders an icon + title + 1-line description + a
 * destination button. Kept inline here (NOT extracted to a shared component)
 * because no other dashboard surface currently shows Explore cards in this
 * exact shape — Rule of 3 / YAGNI.
 */
function ExploreCard({ icon: Icon, title, description, href }: ExploreCardProps) {
  return (
    <Card>
      <div className="p-4 space-y-2">
        <Icon className="size-5 text-primary" aria-hidden="true" />
        <h3 className="text-sm font-semibold">{title}</h3>
        <p className="text-xs text-muted-foreground">{description}</p>
        <Button variant="ghost" size="sm" onClick={() => (window.location.href = href)}>
          Open →
        </Button>
      </div>
    </Card>
  );
}
