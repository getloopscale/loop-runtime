import { useParams, Link, useNavigate } from 'react-router-dom';
import { ArrowLeft, Box, FolderKanban, Loader2 } from 'lucide-react';
import { Badge, Button, Card, CodeBlock, ConfirmDialog, DangerZone, EmptyState, PageShell } from '@loopzero/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useProjects } from '@/hooks/use-projects';
import { useProjectStatus } from '@/hooks/use-project-status';
import { useDeployments } from '@/hooks/use-deployments';
import { ProjectHealthCard } from '@/components/project/project-health-card';
import { engineFetch } from '@/lib/api';
import { formatDate } from '@/lib/format';
import { useState } from 'react';

/**
 * ProjectDetailPage — entry overview for a single project.
 *
 * After 2026-05-26 FAANG UX refactor: the per-project operations live in the
 * sidebar drill-down (Deployments, Status, Runtime logs, Scale, Secrets,
 * Previews, Repository). This page is the project's Overview screen:
 *   - ProjectHealthCard (4 signal pills via useProjectStatus)
 *   - Apps list
 *   - Recent deployments preview (top 5) + link to full list
 *   - CLI hint footer
 *
 * Operations card removed — sidebar IS the operations menu (single source
 * of nav truth). Pattern Vercel/Linear.
 */
export function ProjectDetailPage() {
  useRequireAuth();
  const { name = '' } = useParams<{ name: string }>();
  useSetPageTitle(name || 'Project');
  const { projects, loading } = useProjects();
  const { status } = useProjectStatus(name);
  const { builds } = useDeployments(name);
  const navigate = useNavigate();
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const handleDeleteProject = async () => {
    setDeleting(true);
    try {
      await engineFetch(`/api/v1/projects/${encodeURIComponent(name)}`, {
        method: 'DELETE',
      });
      navigate('/projects');
    } finally {
      setDeleting(false);
    }
  };

  if (loading) {
    return (
      <PageShell title={name}>
        <div className="flex items-center justify-center py-12">
          <Loader2 className="size-6 animate-spin text-muted-foreground" aria-label="Loading project" />
        </div>
      </PageShell>
    );
  }

  const project = projects.find((p) => p.name === name);

  if (!project) {
    return (
      <PageShell title="Project not found">
        <EmptyState
          icon={FolderKanban}
          title={`No project named "${name}"`}
          description="It may have been deleted, or the URL is wrong."
          action={
            <Button asChild variant="secondary">
              <Link to="/projects">
                <ArrowLeft className="size-4" aria-hidden="true" />
                Back to projects
              </Link>
            </Button>
          }
        />
      </PageShell>
    );
  }

  const appsCount = project.apps?.length ?? 0;
  const framework = project.apps?.[0]?.framework;
  const recentBuilds = builds.slice(0, 5);

  return (
    <PageShell title={project.name}>
      <div className="space-y-6">
        <div className="flex items-center gap-2 text-body-sm text-muted-foreground">
          <Link to="/projects" className="hover:text-foreground inline-flex items-center gap-1">
            <ArrowLeft className="size-3" aria-hidden="true" />
            Projects
          </Link>
          <span>/</span>
          <span className="text-foreground">{project.name}</span>
        </div>

        {status && <ProjectHealthCard status={status} />}

        <Card>
          <Card.Header>
            <Card.Title>Overview</Card.Title>
            <Card.Description>
              {appsCount === 0
                ? 'No apps yet. Apps appear after the first deploy.'
                : `${appsCount} ${appsCount === 1 ? 'app' : 'apps'}${framework ? ` · ${framework}` : ''}`}
            </Card.Description>
          </Card.Header>
          <Card.Body>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 text-body-sm">
              <div>
                <div className="text-label text-muted-foreground uppercase">Environment</div>
                <div className="text-foreground mt-1">{project.environment ?? '—'}</div>
              </div>
              <div>
                <div className="text-label text-muted-foreground uppercase">Last deploy</div>
                <div className="text-foreground mt-1 tabular-nums">
                  {project.last_deploy ? formatDate(project.last_deploy, 'relative') : '—'}
                </div>
              </div>
              <div>
                <div className="text-label text-muted-foreground uppercase">Apps</div>
                <div className="text-foreground mt-1 tabular-nums">{appsCount}</div>
              </div>
            </div>
          </Card.Body>
        </Card>

        {appsCount > 0 && (
          <Card>
            <Card.Header>
              <Card.Title>Apps</Card.Title>
            </Card.Header>
            <Card.Body>
              <ul className="divide-y divide-border">
                {project.apps!.map((app) => (
                  <li key={app.name} className="flex items-center justify-between py-3">
                    <div className="flex items-center gap-3">
                      <Box className="size-4 text-muted-foreground" aria-hidden="true" />
                      <span className="font-medium">{app.name}</span>
                      {app.framework && (
                        <Badge variant="outline" className="text-label uppercase">
                          {app.framework}
                        </Badge>
                      )}
                    </div>
                    <span className="text-body-sm text-muted-foreground">{app.type}</span>
                  </li>
                ))}
              </ul>
            </Card.Body>
          </Card>
        )}

        {recentBuilds.length > 0 && (
          <Card>
            <Card.Header>
              <div className="flex items-center justify-between">
                <Card.Title>Recent deployments</Card.Title>
                <Button asChild variant="ghost" size="sm">
                  <Link
                    to={`/projects/${encodeURIComponent(project.name)}/deployments`}
                  >
                    View all
                  </Link>
                </Button>
              </div>
            </Card.Header>
            <Card.Body>
              <ul className="divide-y divide-border">
                {recentBuilds.map((b) => (
                  <li
                    key={b.build_id}
                    className="flex items-center justify-between py-3"
                  >
                    <div className="flex items-center gap-3 min-w-0">
                      <span className="font-mono text-label text-foreground">
                        {b.commit_sha.slice(0, 7)}
                      </span>
                      <span className="text-body-sm text-muted-foreground truncate">
                        {b.created_by}
                      </span>
                    </div>
                    <span className="text-label text-muted-foreground tabular-nums">
                      {b.started_at
                        ? formatDate(b.started_at, 'relative')
                        : '—'}
                    </span>
                  </li>
                ))}
              </ul>
            </Card.Body>
          </Card>
        )}

        <Card>
          <Card.Header>
            <Card.Title className="text-body-sm">Open in your terminal</Card.Title>
            <Card.Description>Use the CLI for full control of this project.</Card.Description>
          </Card.Header>
          <Card.Body>
            <CodeBlock code={`loopzero status ${project.name}\nloopzero logs ${project.name}`} terminal copyable />
          </Card.Body>
        </Card>

        <DangerZone>
          <DangerZone.Action
            title="Delete project"
            description="Permanently delete this project and all its deploys. This cannot be undone."
            action={
              <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}>
                Delete project
              </Button>
            }
          />
        </DangerZone>

        <ConfirmDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          title={`Delete project "${project.name}"?`}
          description="This permanently removes the project, its environments, and all deploys. This cannot be undone."
          confirmationPhrase={project.name}
          confirmLabel="Delete project"
          loading={deleting}
          onConfirm={handleDeleteProject}
        />
      </div>
    </PageShell>
  );
}
