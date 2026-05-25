import { useState } from 'react';
import { Link } from 'react-router-dom';
import { ExternalLink, FolderKanban, Plus } from 'lucide-react';
import {
  Badge,
  Button,
  DataTable,
  EmptyState,
  StatusDot,
  type DataTableColumn,
} from '@loopzero/ui';
import { ActionBar } from '@/components/layout';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useProjects } from '@/hooks/use-projects';
import { formatDate } from '@/lib/format';

interface ProjectRow {
  name: string;
  framework?: string;
  apps_count: number;
  last_deploy?: string;
  environment?: string;
}

/**
 * ProjectsPage — entity list rendered via DataTable (post Brief #5 migration 3/4).
 *
 * Pre: 3-col ContentGrid of <ProjectCard> (~140px per card, sparse for entity
 * list with 5+ rows). Closes Deep Review § 2.2 P1.
 *
 * Post: DataTable with sortable columns + sticky header + Open action per row.
 * Information density doubled — same 5 projects fit in ~280px vs ~600px.
 */
export function ProjectsPage() {
  useRequireAuth();
  useSetPageTitle('Projects');
  const { projects, loading } = useProjects();
  const [searchQuery, setSearchQuery] = useState('');

  const filteredProjects = projects.filter((p) =>
    p.name.toLowerCase().includes(searchQuery.toLowerCase()),
  );

  // Adapter: backend Project shape → table row shape.
  const rows: ProjectRow[] = filteredProjects.map((p) => ({
    name: p.name,
    framework: p.apps?.[0]?.framework,
    apps_count: p.apps?.length ?? 0,
    last_deploy: p.last_deploy,
    environment: p.environment,
  }));

  const columns: DataTableColumn<ProjectRow>[] = [
    {
      key: 'name',
      label: 'Project',
      sortable: true,
      render: (r) => <span className="font-medium text-foreground">{r.name}</span>,
    },
    {
      key: 'framework',
      label: 'Framework',
      sortable: true,
      className: 'hidden md:table-cell',
      render: (r) =>
        r.framework ? (
          <Badge variant="outline" className="text-label uppercase">{r.framework}</Badge>
        ) : (
          <span className="text-body-sm text-muted-foreground">—</span>
        ),
    },
    {
      key: 'apps_count',
      label: 'Apps',
      sortable: true,
      align: 'right',
      className: 'hidden md:table-cell',
      render: (r) => (
        <span className="text-body-sm text-muted-foreground tabular-nums">
          {r.apps_count}
        </span>
      ),
    },
    {
      key: 'environment',
      label: 'Environment',
      className: 'hidden md:table-cell',
      render: (r) => (
        <span className="text-body-sm text-muted-foreground">{r.environment ?? '—'}</span>
      ),
    },
    {
      key: 'status',
      label: 'Status',
      render: () => <StatusDot status="live" label="Live" />,
    },
    {
      key: 'last_deploy',
      label: 'Last deploy',
      sortable: true,
      render: (r) => (
        <span className="text-body-sm text-muted-foreground tabular-nums">
          {r.last_deploy ? formatDate(r.last_deploy, 'relative') : '—'}
        </span>
      ),
    },
    {
      key: '__open',
      label: '',
      align: 'right',
      render: (r) => (
        <Button variant="ghost" size="sm" asChild>
          <Link to={`/projects/${r.name}`} aria-label={`Open ${r.name}`}>
            Open <ExternalLink className="size-3" aria-hidden="true" />
          </Link>
        </Button>
      ),
    },
  ];

  return (
    <>
      <ActionBar
        searchPlaceholder="Search projects..."
        searchValue={searchQuery}
        onSearchChange={setSearchQuery}
        primaryAction={{
          label: 'New Project',
          icon: Plus,
          onClick: () => {
            window.open('https://docs.loopzero.example/cli/init', '_blank');
          },
        }}
      />

      <DataTable<ProjectRow>
        columns={columns}
        data={rows}
        rowKey={(r) => r.name}
        stickyHeader
        loading={loading}
        defaultSort={{ key: 'last_deploy', direction: 'desc' }}
        emptyState={
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
                  to create your first project.
                </>
              )
            }
          />
        }
      />
    </>
  );
}
