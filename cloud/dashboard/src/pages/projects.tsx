import { useEffect, useState } from 'react';
import { Link, useSearchParams, useNavigate } from 'react-router-dom';
import { ExternalLink, FolderKanban, Plus } from 'lucide-react';
import { Badge, Button, Card, PageShell, StatusDot, Table } from '@loopzerokit/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useProjects } from '@/hooks/use-projects';
import { CreateProjectSheet } from "@/components/sheets/create-project-sheet";
import { EmptyProjectsHero } from '@/components/onboarding/empty-projects-hero';
import { TableSkeleton } from '@/components/data/table-skeleton';
import { formatDate } from '@/lib/format';

/**
 * ProjectsPage — entity list.
 *
 * Visual consistency pattern (per 2026-05-26 sponsor directive "all tables
 * must look like /environments"): PageShell + Card + Table primitive,
 * matching environments.tsx style. Was previously DataTable composite +
 * ActionBar; migrated to the simpler primitive so every list page in the
 * dashboard renders the same header chrome.
 *
 * Sorting was a DataTable freebie that the Table primitive doesn't provide;
 * accepted trade-off for visual consistency. Sortable columns can be
 * reintroduced later as an opt-in feature (likely upstream candidate).
 */
export function ProjectsPage() {
  useRequireAuth();
  useSetPageTitle('Projects');
  const { projects, loading } = useProjects();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [searchQuery, setSearchQuery] = useState(searchParams.get('q') ?? '');
  const [createOpen, setCreateOpen] = useState(false);

  // Sync ProjectsPage search input with `?q=` query param (sidebar search wire,
  // plan v1.2 T0.5). Updates on URL change so back/forward works.
  useEffect(() => {
    const q = searchParams.get('q') ?? '';
    setSearchQuery(q);
  }, [searchParams]);

  // EC-03 consumer wiring (Sprint 2 T2.3): CommandPalette "Create project"
  // navigates here with ?action=new. Open sheet + auto-clear param so re-renders
  // don't re-trigger.
  useEffect(() => {
    if (searchParams.get('action') === 'new') {
      setCreateOpen(true);
      const next = new URLSearchParams(searchParams);
      next.delete('action');
      setSearchParams(next, { replace: true });
    }
  }, [searchParams, setSearchParams]);

  const filteredProjects = projects.filter((p) =>
    p.name.toLowerCase().includes(searchQuery.toLowerCase()),
  );

  return (
    <PageShell
      title="Projects"
      search={{
        placeholder: 'Search projects...',
        value: searchQuery,
        onChange: setSearchQuery,
      }}
      primaryAction={{
        label: 'New Project',
        icon: Plus,
        // SG-01 CLOSED (plan v1.0 T4.1, 2026-05-26): real CreateProjectSheet
        // wired to POST /api/v1/projects. Full GitHub picker wizard lives in
        // a future iter — current dialog accepts name + creates the project.
        onClick: () => setCreateOpen(true),
      }}
      loading={loading}
      loadingNode={<Card><TableSkeleton rows={6} columns={5} /></Card>}
      empty={
        // Only PageShell empty state for SEARCH no-match. For "no projects at all"
        // (Sprint 3 T3.3), render EmptyProjectsHero inline below — empty-state-as-
        // onboarding per ADR D2.
        !loading && searchQuery && filteredProjects.length === 0
          ? {
              icon: FolderKanban,
              title: 'No projects match your search',
              description: `No project name contains "${searchQuery}". Try a different term or clear the search to see all projects.`,
              action: { label: 'Clear search', onClick: () => setSearchQuery('') },
            }
          : undefined
      }
    >
      {!loading && projects.length === 0 ? (
        // Sprint 3 T3.3: empty-state-as-onboarding per ADR D2.
        <EmptyProjectsHero onCreateFromDashboard={() => setCreateOpen(true)} />
      ) : (
      <Card>
        <Card.Body className="p-0">
          <Table>
            <Table.Header>
              <Table.Row>
                <Table.HeaderCell>Project</Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Framework
                </Table.HeaderCell>
                <Table.HeaderCell
                  align="right"
                  className="hidden md:table-cell"
                >
                  Apps
                </Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Environment
                </Table.HeaderCell>
                <Table.HeaderCell>Status</Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Last deploy
                </Table.HeaderCell>
                <Table.HeaderCell align="right">
                  <span className="sr-only">Open project</span>
                </Table.HeaderCell>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {filteredProjects.map((p) => {
                const framework = p.apps?.[0]?.framework;
                const appsCount = p.apps?.length ?? 0;
                const isLive = Boolean(p.last_deploy);
                return (
                  <Table.Row key={p.name}>
                    <Table.Cell>
                      <span className="font-medium text-body-sm text-foreground">
                        {p.name}
                      </span>
                    </Table.Cell>
                    <Table.Cell className="hidden md:table-cell">
                      {framework ? (
                        <Badge variant="outline" className="text-label uppercase">
                          {framework}
                        </Badge>
                      ) : (
                        <span className="text-body-sm text-muted-foreground">—</span>
                      )}
                    </Table.Cell>
                    <Table.Cell
                      align="right"
                      numeric
                      className="hidden md:table-cell"
                    >
                      {appsCount}
                    </Table.Cell>
                    <Table.Cell className="hidden md:table-cell">
                      <span className="font-mono text-label text-muted-foreground">
                        {p.environment ?? '—'}
                      </span>
                    </Table.Cell>
                    <Table.Cell>
                      <StatusDot
                        status={isLive ? 'live' : 'idle'}
                        label={isLive ? 'Live' : 'Idle'}
                      />
                    </Table.Cell>
                    <Table.Cell className="hidden md:table-cell">
                      <span className="text-body-sm text-muted-foreground tabular-nums">
                        {p.last_deploy ? formatDate(p.last_deploy, 'relative') : '—'}
                      </span>
                    </Table.Cell>
                    <Table.Cell align="right">
                      <Button variant="ghost" size="sm" asChild>
                        <Link
                          to={`/projects/${p.name}`}
                          aria-label={`Open ${p.name}`}
                        >
                          Open
                          <ExternalLink className="size-3" aria-hidden="true" />
                        </Link>
                      </Button>
                    </Table.Cell>
                  </Table.Row>
                );
              })}
            </Table.Body>
          </Table>
        </Card.Body>
      </Card>
      )}

      <CreateProjectSheet
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={(p) => navigate(`/projects/${encodeURIComponent(p.name)}`)}
      />
    </PageShell>
  );
}
