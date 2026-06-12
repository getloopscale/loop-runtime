import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  ArrowRight,
  FolderKanban,
  Globe,
  Loader2,
  Plus,
  Rocket,
  Users,
} from 'lucide-react';
import {
  Badge,
  Card,
  EmptyState,
  PageShell,
  ProjectCard,
  UsageMeter,
} from '@loopzero/ui';
import { ContentGrid } from '@/components/layout';
import { usePageMeta } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useProjects } from '@/hooks/use-projects';
import { useCloudApi } from '@/hooks/use-api';
import { formatNumber } from '@/lib/format';

interface UsageMetricShape {
  used: number;
  limit: number | null;
}

interface BillingResponse {
  plan: string;
  team: string;
  usage: Record<string, UsageMetricShape>;
  period_start?: string;
  period_end?: string;
}

/**
 * OverviewPage — mockup-aligned literal implementation per
 * cloud/dashboard/design/loopzero_overview_dashboard_mockup.html (Pattern D-1
 * of plan v1.2). 2-asymmetric grid (1.2fr:1fr) with Usage + Alerts on
 * left, Projects + Recent Previews on right.
 *
 * Usage data sourced from /v1/billing (BillingResponse.usage map) per T-A.2
 * of plan v1.1. Per ADR-D1.bis: limit:null renders "Unlimited" (Enterprise
 * tier). Per EC-6: used>limit renders actual numeric (over-quota visual).
 */
export function OverviewPage() {
  useRequireAuth();
  const { setPageTitle } = usePageMeta();
  const navigate = useNavigate();
  const { projects, loading } = useProjects();
  const { data: billing } = useCloudApi<BillingResponse>('/v1/billing');
  const usage = billing?.usage ?? {};
  const [searchQuery, setSearchQuery] = useState('');

  const filteredProjects = projects.filter((p) =>
    p.name.toLowerCase().includes(searchQuery.toLowerCase()),
  );

  // Pull metric (used, limit) from billing.usage with fallback for null billing.
  // EC: /v1/billing null → defaults to 0/limit per acceptance criteria.
  // EC-5: limit may be null (Enterprise unlimited) — propagate as-is.
  function pickMetric(key: string, defaultLimit: number): { value: number; max: number | null } {
    const m = usage[key];
    if (!m) return { value: 0, max: defaultLimit };
    return { value: m.used, max: m.limit };
  }

  // Formatter for UsageMeter metric row's value/max text.
  // Per ADR-D1.bis: limit===null renders "{used} / Unlimited".
  // UsageMeter primitive natively handles over-quota (value>max → text-warning + warning intent).
  // Uses formatNumber() from lib/format.ts per T-C.2 migration (en-US locale per ADR-D1).
  function fmtMetric(value: number, max: number | null, opts?: { compactMillions?: boolean }): string {
    if (max === null) return `${formatNumber(value, 'grouped')} / Unlimited`;
    if (opts?.compactMillions && max >= 1_000_000) {
      return `${formatNumber(value, 'grouped')} / ${formatNumber(max, 'compact')}`;
    }
    return `${formatNumber(value, 'grouped')} / ${formatNumber(max, 'grouped')}`;
  }

  const dataMetric = pickMetric('Fast Data Transfer', 100);
  const originMetric = pickMetric('Fast Origin Transfer', 10);
  const requestsMetric = pickMetric('Edge Requests', 1_000_000);
  const buildMinutesMetric = pickMetric('Build Minutes', 400);
  const concurrentBuildsMetric = pickMetric('Concurrent Builds', 4);

  return (
    <PageShell
      title="Overview"
      onTitleChange={setPageTitle}
      search={{
        placeholder: 'Search projects...',
        value: searchQuery,
        onChange: setSearchQuery,
      }}
      primaryAction={{
        label: 'Add New',
        icon: Plus,
        onClick: () => navigate('/projects'),
      }}
    >
      <ContentGrid columns="2-asymmetric" gap={6}>
        {/* Left column — Usage + Alerts */}
        <div className="space-y-6">
          <section>
            <p className="text-label-caps text-muted-foreground uppercase mb-3 tracking-wider">
              Usage
            </p>
            {/* T-DR-2.5 v1.2 (closes V-8 from Vercel benchmark): max-h
                + overflow-y-auto acomoda 5+ KPIs sem inflar página.
                Vercel pattern: panel scrolls internally rather than
                forcing page growth. */}
            <div className="max-h-[400px] overflow-y-auto">
              <UsageMeter
                title="Last 30 days"
                action={<Badge variant="outline">Upgrade</Badge>}
                metrics={[
                {
                  label: 'Fast Data Transfer',
                  value: dataMetric.value,
                  max: dataMetric.max ?? Number.MAX_SAFE_INTEGER,
                  unit: 'GB',
                  formatter: (v) => fmtMetric(v, dataMetric.max),
                },
                {
                  label: 'Fast Origin Transfer',
                  value: originMetric.value,
                  max: originMetric.max ?? Number.MAX_SAFE_INTEGER,
                  unit: 'GB',
                  formatter: (v) => fmtMetric(v, originMetric.max),
                },
                {
                  label: 'Edge Requests',
                  value: requestsMetric.value,
                  max: requestsMetric.max ?? Number.MAX_SAFE_INTEGER,
                  unit: 'req',
                  formatter: (v) => fmtMetric(v, requestsMetric.max, { compactMillions: true }),
                },
                {
                  label: 'Build Minutes',
                  value: buildMinutesMetric.value,
                  max: buildMinutesMetric.max ?? Number.MAX_SAFE_INTEGER,
                  unit: 'min',
                  formatter: (v) => fmtMetric(v, buildMinutesMetric.max),
                },
                {
                  label: 'Concurrent Builds',
                  value: concurrentBuildsMetric.value,
                  max: concurrentBuildsMetric.max ?? Number.MAX_SAFE_INTEGER,
                  unit: '',
                  formatter: (v) => fmtMetric(v, concurrentBuildsMetric.max),
                },
              ]}
              />
            </div>
          </section>

          {/* Alerts section removed v1.2 T-DR-1.1 (closes P1-1): feature
              not yet backend-shipped; oversized empty state (270px) violated
              CC-3 (max 1 empty state/page ≤200px). Re-introduce when
              backend alert engine ships, scoped as inline Alert banner. */}
        </div>

        {/* Right column — Projects + Recent Previews */}
        <div className="space-y-6">
          <section>
            <p className="text-label-caps text-muted-foreground uppercase mb-3 tracking-wider">
              Projects
            </p>
            {loading ? (
              <Card>
                <Card.Body className="py-8 text-center">
                  <Loader2
                    className="mx-auto size-5 text-muted-foreground animate-spin"
                    aria-label="Loading projects"
                  />
                </Card.Body>
              </Card>
            ) : filteredProjects.length === 0 ? (
              <EmptyState
                icon={FolderKanban}
                title={searchQuery ? 'No matching projects' : 'Deploy your first project'}
                description={
                  searchQuery ? (
                    <>Try a different search term.</>
                  ) : (
                    <>
                      Run{' '}
                      <code className="bg-muted px-2 py-0.5 rounded font-mono">
                        loopzero init
                      </code>{' '}
                      in your terminal to connect a repo and ship.
                    </>
                  )
                }
              />
            ) : (
              <div className="space-y-3">
                {filteredProjects.slice(0, 3).map((p) => (
                  <ProjectCard
                    key={p.name}
                    project={{
                      id: p.name,
                      name: p.name,
                      branch: 'main',
                      commitSha: '',
                      // Honest status: 'live' only when project has a deploy timestamp
                      // (plan v1.2 T0.6 — backend `last_deploy_status` audit pending).
                      // 'queued' as fallback because ProjectCard accepts DeploymentStatus
                      // (not 'idle'); 'queued' reads as "pending first deploy" — true.
                      status: p.last_deploy ? 'live' : 'queued',
                      lastDeployedAt: p.last_deploy ?? new Date().toISOString(),
                      framework: p.apps?.[0]?.framework,
                    }}
                    href={`/projects/${p.name}`}
                  />
                ))}
              </div>
            )}
          </section>

          {/* Quick actions — UI-only nav aids (replaces removed Recent Previews
              section v1.2 T0.7 fold). Reduz a sensação de "anêmico" do Overview
              sem inventar dados. Cada action é navegação real para surface
              existente; nenhuma depende de backend novo. */}
          <section>
            <p className="text-label-caps text-muted-foreground uppercase mb-3 tracking-wider">
              Quick actions
            </p>
            <Card>
              <ul className="divide-y divide-border">
                <li>
                  <button
                    type="button"
                    onClick={() => navigate('/projects')}
                    className="w-full flex items-center justify-between gap-3 px-4 py-3 text-left hover:bg-muted/40 transition-colors"
                  >
                    <span className="flex items-center gap-3">
                      <Rocket className="size-4 text-primary" aria-hidden="true" />
                      <span className="font-medium text-foreground">Deploy a project</span>
                    </span>
                    <ArrowRight className="size-4 text-muted-foreground" aria-hidden="true" />
                  </button>
                </li>
                <li>
                  <button
                    type="button"
                    onClick={() => navigate('/team')}
                    className="w-full flex items-center justify-between gap-3 px-4 py-3 text-left hover:bg-muted/40 transition-colors"
                  >
                    <span className="flex items-center gap-3">
                      <Users className="size-4 text-primary" aria-hidden="true" />
                      <span className="font-medium text-foreground">Invite teammates</span>
                    </span>
                    <ArrowRight className="size-4 text-muted-foreground" aria-hidden="true" />
                  </button>
                </li>
                <li>
                  <button
                    type="button"
                    onClick={() => navigate('/domains')}
                    className="w-full flex items-center justify-between gap-3 px-4 py-3 text-left hover:bg-muted/40 transition-colors"
                  >
                    <span className="flex items-center gap-3">
                      <Globe className="size-4 text-primary" aria-hidden="true" />
                      <span className="font-medium text-foreground">Add a custom domain</span>
                    </span>
                    <ArrowRight className="size-4 text-muted-foreground" aria-hidden="true" />
                  </button>
                </li>
              </ul>
            </Card>
          </section>
        </div>
      </ContentGrid>
    </PageShell>
  );
}
