import { useState } from 'react';
import { Link, NavLink, Outlet, useParams } from 'react-router-dom';
import { ArrowLeft, Loader2, XCircle } from 'lucide-react';
import { Button, EmptyState, PageShell } from '@loopzero/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useDeployment } from '@/hooks/use-deployment';
import { BuildHeader } from '@/components/deploy/build-header';
import { RollbackDialog } from '@/components/dialogs/rollback-dialog';
import { RedeployDialog } from '@/components/dialogs/redeploy-dialog';
import { CancelBuildDialog } from '@/components/dialogs/cancel-build-dialog';

/**
 * DeploymentDetailLayout — Plan J-17 T5.1 + T5.2.
 *
 * Becomes the canonical container for ALL deployment detail sub-routes:
 *   /projects/:name/deployments/:id            → TimelineTab (index)
 *   /projects/:name/deployments/:id/logs       → LogsTab
 *   /projects/:name/deployments/:id/sbom       → SBOMTab
 *   /projects/:name/deployments/:id/attestation → AttestationTab
 *
 * The layout owns:
 *   - build fetch (useDeployment) — cached by React Query so every tab
 *     consumes the SAME row without re-fetch
 *   - BuildHeader (T3.5) with onCancel / onRollback / onRedeploy callbacks
 *   - NavLink tab strip with active state preserved across navigations
 *   - <Outlet/> that renders the active tab content
 *
 * EC-15 risco aceito: each tab mounts its own SSE EventSource. Switching
 * tabs unmounts/remounts; Last-Event-ID resume guarantees zero loss.
 */

const TABS = [
  { to: '', label: 'Timeline', end: true },
  { to: 'logs', label: 'Logs', end: false },
  { to: 'sbom', label: 'SBOM', end: false },
  { to: 'attestation', label: 'Attestation', end: false },
] as const;

export function DeploymentDetailLayout() {
  useRequireAuth();
  const { name = '', id = '' } = useParams<{ name: string; id: string }>();
  useSetPageTitle(`${name} · Deployment ${id.slice(0, 7)}`);
  const { build, loading, error } = useDeployment(id);
  const [rollbackOpen, setRollbackOpen] = useState(false);
  const [redeployOpen, setRedeployOpen] = useState(false);
  const [cancelOpen, setCancelOpen] = useState(false);

  const handleCancel = () => setCancelOpen(true);
  const handleRollback = () => setRollbackOpen(true);
  const handleRedeploy = () => setRedeployOpen(true);

  if (loading) {
    return (
      <PageShell title="Deployment">
        <div className="flex items-center justify-center py-12">
          <Loader2
            className="size-6 animate-spin text-muted-foreground"
            aria-label="Loading deployment"
          />
        </div>
      </PageShell>
    );
  }

  if (error || !build) {
    return (
      <PageShell title="Deployment not found">
        <EmptyState
          icon={XCircle}
          title={`No build with id "${id}"`}
          description="It may have been pruned, or the URL is wrong."
          action={
            <Button asChild variant="secondary">
              <Link
                to={`/projects/${encodeURIComponent(name)}/deployments`}
                className="inline-flex items-center gap-2"
              >
                <ArrowLeft className="size-4" aria-hidden="true" />
                Back to deployments
              </Link>
            </Button>
          }
        />
      </PageShell>
    );
  }

  const sha = build.commit_sha.slice(0, 7);

  return (
    <PageShell title={sha} description={`Build ${build.build_id}`}>
      <div className="mb-4">
        <Button asChild variant="ghost" size="sm">
          <Link
            to={`/projects/${encodeURIComponent(name)}/deployments`}
            className="inline-flex items-center gap-1"
          >
            <ArrowLeft className="size-4" aria-hidden="true" />
            <span>Back to deployments</span>
          </Link>
        </Button>
      </div>

      <div className="mb-6 space-y-4">
        <BuildHeader
          build={build}
          onCancel={handleCancel}
          onRollback={handleRollback}
          onRedeploy={handleRedeploy}
        />
        <nav className="border-b border-border" aria-label="Deployment tabs">
          <ul className="flex gap-1 -mb-px">
            {TABS.map((t) => {
              const to =
                t.to === ''
                  ? `/projects/${encodeURIComponent(name)}/deployments/${encodeURIComponent(id)}`
                  : `/projects/${encodeURIComponent(name)}/deployments/${encodeURIComponent(id)}/${t.to}`;
              return (
                <li key={t.label}>
                  <NavLink
                    to={to}
                    end={t.end}
                    className={({ isActive }) =>
                      [
                        'inline-block px-3 py-2 text-body-sm border-b-2 -mb-px transition-colors',
                        isActive
                          ? 'border-primary text-foreground font-medium'
                          : 'border-transparent text-muted-foreground hover:text-foreground hover:border-border',
                      ].join(' ')
                    }
                  >
                    {t.label}
                  </NavLink>
                </li>
              );
            })}
          </ul>
        </nav>
      </div>

      <Outlet />

      <RollbackDialog
        open={rollbackOpen}
        onOpenChange={setRollbackOpen}
        projectName={name}
        targetBuild={build}
      />
      <RedeployDialog
        open={redeployOpen}
        onOpenChange={setRedeployOpen}
        projectName={name}
        sourceBuild={build}
      />
      <CancelBuildDialog
        open={cancelOpen}
        onOpenChange={setCancelOpen}
        build={build}
      />
    </PageShell>
  );
}
