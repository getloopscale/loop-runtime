import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Bell, Eye, FolderKanban, Loader2, Plus } from 'lucide-react';
import {
  Badge,
  Button,
  Card,
  EmptyState,
  ProjectCard,
  UsageMeter,
} from '@loopzero/ui';
import { ActionBar, ContentGrid } from '@/components/layout';
import { useSetPageTitle } from '@/hooks/use-page-meta';
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
  useSetPageTitle('Overview');
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

  return (
    <>
      <ActionBar
        searchPlaceholder="Search projects..."
        searchValue={searchQuery}
        onSearchChange={setSearchQuery}
        primaryAction={{
          label: 'Add New',
          icon: Plus,
          onClick: () => navigate('/projects'),
        }}
      />

      <ContentGrid columns="2-asymmetric" gap={6}>
        {/* Left column — Usage + Alerts */}
        <div className="space-y-6">
          <section>
            <p className="text-label-caps text-muted-foreground uppercase mb-3 tracking-wider">
              Usage
            </p>
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
              ]}
            />
          </section>

          <section>
            <p className="text-label-caps text-muted-foreground uppercase mb-3 tracking-wider">
              Alerts
            </p>
            <EmptyState
              icon={Bell}
              title="Get alerted for anomalies"
              description="Automatically monitor your projects for anomalies and get notified."
              action={<Button variant="primary">Upgrade to Pro</Button>}
            />
          </section>
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
                title={searchQuery ? 'No matching projects' : 'No projects yet'}
                description={
                  searchQuery ? (
                    <>Try a different search term.</>
                  ) : (
                    <>
                      Run{' '}
                      <code className="bg-muted px-2 py-0.5 rounded font-mono">
                        loopzero init
                      </code>{' '}
                      to create your first.
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
                      status: 'live',
                      lastDeployedAt: p.last_deploy ?? new Date().toISOString(),
                      framework: p.apps?.[0]?.framework,
                    }}
                    href={`/projects/${p.name}`}
                  />
                ))}
              </div>
            )}
          </section>

          <section>
            <p className="text-label-caps text-muted-foreground uppercase mb-3 tracking-wider">
              Recent Previews
            </p>
            <EmptyState
              icon={Eye}
              title="No recent previews"
              description="Preview deployments that you have recently visited or created will appear here."
            />
          </section>
        </div>
      </ContentGrid>
    </>
  );
}
