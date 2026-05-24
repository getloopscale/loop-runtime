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

/**
 * OverviewPage — mockup-aligned literal implementation per
 * cloud/dashboard/design/loopzero_overview_dashboard_mockup.html (Pattern D-1
 * of plan v1.2). 2-asymmetric grid (1.2fr:1fr) with Usage + Alerts on
 * left, Projects + Recent Previews on right.
 *
 * Usage metrics: TODO(EC-4) — currently placeholder zeros. /v1/billing
 * GetBilling handler doesn't expose usage metrics yet; cost_counters
 * table needs dedicated endpoint /v1/billing/usage or /v1/usage.
 * Mockup spec accepts placeholders zerados (mockup has "0 / 100 GB" literal).
 */
export function OverviewPage() {
  useRequireAuth();
  useSetPageTitle('Overview');
  const navigate = useNavigate();
  const { projects, loading } = useProjects();
  const [searchQuery, setSearchQuery] = useState('');

  const filteredProjects = projects.filter((p) =>
    p.name.toLowerCase().includes(searchQuery.toLowerCase()),
  );

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
                  value: 0,
                  max: 100,
                  unit: 'GB',
                },
                {
                  label: 'Fast Origin Transfer',
                  value: 0,
                  max: 10,
                  unit: 'GB',
                },
                {
                  label: 'Edge Requests',
                  value: 0,
                  max: 1_000_000,
                  unit: 'req',
                  formatter: (v, m) =>
                    `${v.toLocaleString()} / ${m >= 1_000_000 ? `${m / 1_000_000}M` : m}`,
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
