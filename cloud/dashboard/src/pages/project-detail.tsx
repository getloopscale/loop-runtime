import { useParams, Link, useNavigate } from 'react-router-dom';
import { ArrowLeft, Box, FolderKanban, Loader2, Rocket } from 'lucide-react';
import { Badge, Button, Card, CodeBlock, ConfirmDialog, DangerZone, EmptyState, PageShell } from '@loopzero/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useProjects } from '@/hooks/use-projects';
import { useProjectStatus } from '@/hooks/use-project-status';
import { useDeployments } from '@/hooks/use-deployments';
import { LiveUrlHero } from '@/components/project/live-url-hero';
import { ProjectHealthCard } from '@/components/project/project-health-card';
import { engineFetch } from '@/lib/api';
import { normalizeBuildStatus } from '@/lib/api/types';
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
  const { status, loading: statusLoading } = useProjectStatus(name);
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

  const recentBuilds = builds.slice(0, 5);

  // T5.3 LIVE-bug fix (2026-06-08, plan 2026-06-08-dashboard-screens-review-fixes):
  // The T2.1 ListProjectsWithSummaryByTenant query (consumed via useProjects())
  // returns environment_count + last_deploy_status_raw but NO LONGER returns the
  // legacy `apps[]` + `last_deploy` timestamp + `environment` (singular) fields.
  // The Overview cards previously read from those legacy fields and silently
  // resolved to "—" / "0" + the misleading "No apps yet" message even when the
  // project had several successful deployments visible in "Recent deployments".
  //
  // Caught by sponsor verbatim during T5.3 LIVE walkthrough: /projects/teste
  // showed Environment=— + Last deploy=— + Apps=0 + "No apps yet" while 5
  // deploys were rendered below.
  //
  // Fix: derive Overview metrics from the builds[] array (already loaded via
  // useDeployments on this page) instead of the listing's project shape.
  //   - apps come from the latest successful build's apps[] snapshot (loopzero.yaml
  //     apps declared at that build time); fall back to latest build when no
  //     successful one exists yet
  //   - last deploy timestamp comes from the latest build's created_at
  //   - environment display uses environment_count (T2.1) so the value matches
  //     the "Live envs" column on /projects
  // BuildApp shape has `type` (not `framework` like the legacy Project.apps[]),
  // so framework display reads from app.type.
  const latestBuild = recentBuilds[0];
  // Canonical pattern: backend ships uppercase BuildState ('SUCCEEDED'); TS
  // type Build.status is the normalized lowercase variant. normalizeBuildStatus
  // bridges that gap (see cloud/dashboard/src/lib/api/types.ts:144).
  const latestSuccessful = recentBuilds.find((b) => normalizeBuildStatus(b.status) === 'succeeded');
  // T5.3 LIVE-bug iter 20 refinement (2026-06-08): canonical apps_count
  // source is now p.apps_count from the T2.1 ListProjectsWithSummaryByTenant
  // SQL summary (wired in commit 0d854f79). The earlier iter cd603c7b
  // derivation `appsFromBuild = latestSuccessful?.apps ?? latestBuild?.apps`
  // always resolved to [] because BuildListItem in api/internal/routes/
  // build_list.go does NOT serialize an `apps` field on builds list
  // entries — only the deprecated BuildCallback shape carries apps. So
  // counts from `recentBuilds[].apps.length` are always 0 even when the
  // project has a SUCCEEDED build with populated apps_spec in the DB.
  // The backend additive T2.1 fix already exposes apps_count + latest_framework
  // via the listing query — read from that as primary, fall back to the
  // builds-derived path for forward-compat with response shapes that
  // pre-date 0d854f79.
  const appsFromBuild = latestSuccessful?.apps ?? latestBuild?.apps ?? [];
  const appsCount = project.apps_count ?? appsFromBuild.length;
  const framework = project.latest_framework || appsFromBuild[0]?.type;
  const lastDeployAt = project.last_deploy ?? latestBuild?.created_at;
  const envCount = project.environment_count ?? (project.environment ? 1 : 0);

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

        {/* T1.1 of plan 2026-06-12-...j25...: Live URL hero card above health
            (Vercel-canonical hero slot per ADR D2; data path ADR D1 = useProjectStatus.url). */}
        <LiveUrlHero status={status} loading={statusLoading} />

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
                <div className="text-label text-muted-foreground uppercase">Live envs</div>
                <div className="text-foreground mt-1 tabular-nums">{envCount}</div>
              </div>
              <div>
                <div className="text-label text-muted-foreground uppercase">Last deploy</div>
                <div className="text-foreground mt-1 tabular-nums">
                  {lastDeployAt ? formatDate(lastDeployAt, 'relative') : '—'}
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
                {appsFromBuild.map((app) => (
                  // BuildApp.type is the canonical "framework" indicator after
                  // T2.1; legacy Project.apps[].framework is no longer returned.
                  <li key={app.name} className="flex items-center justify-between py-3">
                    <div className="flex items-center gap-3">
                      <Box className="size-4 text-muted-foreground" aria-hidden="true" />
                      <span className="font-medium">{app.name}</span>
                      {app.type && (
                        <Badge variant="outline" className="text-label uppercase">
                          {app.type}
                        </Badge>
                      )}
                    </div>
                    {app.image_digest && (
                      <span className="text-body-sm text-muted-foreground font-mono">
                        {app.image_digest.slice(0, 12)}
                      </span>
                    )}
                  </li>
                ))}
              </ul>
            </Card.Body>
          </Card>
        )}

        {/* T0.2 EC-06: empty state shown when builds load completed e zero. NÃO durante loading (would flash). */}
        {recentBuilds.length === 0 ? (
          <Card>
            <Card.Body className="py-10">
              <EmptyState
                icon={Rocket}
                title="No deployments yet"
                description="Run `loopzero deploy` from your project directory, or connect a Git repository to ship on every push."
                action={
                  <Button asChild variant="primary" size="sm">
                    <Link to={`/projects/${encodeURIComponent(project.name)}/connect`}>
                      Connect repository
                    </Link>
                  </Button>
                }
              />
            </Card.Body>
          </Card>
        ) : (
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
            <Card.Title className="text-body-sm">Open in your terminal</Card.Title>{/* T0.2 closes recent-deploys ternary */}
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
