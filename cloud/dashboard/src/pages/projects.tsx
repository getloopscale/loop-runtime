import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { FolderKanban, Loader2, Plus } from 'lucide-react';
import { Card, EmptyState, ProjectCard } from '@loopzero/ui';
import { ActionBar, ContentGrid } from '@/components/layout';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useProjects } from '@/hooks/use-projects';

/**
 * ProjectsPage — full project list with search + filter + new CTA + 3-col grid.
 * Per design language Pattern 4 (ActionBar) + Pattern 5 (ContentGrid 3-equal).
 *
 * Adapter EC-5 v1.1: backend Project shape → @loopzero/ui Project type with
 * sensible defaults (status='live', branch='main', etc.).
 */
export function ProjectsPage() {
  useRequireAuth();
  useSetPageTitle('Projects');
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
          label: 'New Project',
          icon: Plus,
          onClick: () => {
            /* TODO: open new project flow when CLI deep-link or modal exists */
            window.open('https://docs.loopzero.example/cli/init', '_blank');
          },
        }}
      />

      {loading ? (
        <Card>
          <Card.Body className="py-16 text-center">
            <Loader2
              className="mx-auto size-6 text-muted-foreground animate-spin"
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
                to create your first project.
              </>
            )
          }
        />
      ) : (
        <ContentGrid columns="3-equal" gap={4}>
          {filteredProjects.map((p) => (
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
              onClick={() => navigate(`/projects/${p.name}`)}
            />
          ))}
        </ContentGrid>
      )}
    </>
  );
}
