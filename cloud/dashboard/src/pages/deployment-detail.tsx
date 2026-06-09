import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import {
  ArrowLeft,
  FileText,
  GitCommit,
  Loader2,
  Rocket,
  ScrollText,
  ShieldCheck,
  Undo2,
  XCircle,
} from 'lucide-react';
import {
  Badge,
  Button,
  Card,
  EmptyState,
  PageShell,
  StatusDot,
  type StatusKind,
} from '@loopzerokit/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useDeployment } from '@/hooks/use-deployment';
import { useCancelBuild } from '@/hooks/mutations/use-cancel-build';
import { BuildTimeline } from '@/components/deploy/build-timeline';
import { RollbackDialog } from '@/components/dialogs/rollback-dialog';
import { PromoteDialog } from '@/components/dialogs/promote-dialog';
import { formatDate } from '@/lib/format';
import type { BuildStatus } from '@/lib/api/types';

const STATUS_MAP: Record<BuildStatus, StatusKind> = {
  succeeded: 'live',
  running: 'building',
  queued: 'building',
  failed: 'failed',
  cancelled: 'idle',
  cancelling: 'idle',
};

const STATUS_LABEL: Record<BuildStatus, string> = {
  succeeded: 'Succeeded',
  running: 'Running',
  queued: 'Queued',
  failed: 'Failed',
  cancelled: 'Cancelled',
  cancelling: 'Cancelling',
};

function formatDuration(seconds?: number | null): string {
  if (!seconds && seconds !== 0) return '—';
  if (seconds < 60) return `${seconds}s`;
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m}m ${s}s`;
}

/**
 * DeploymentDetailPage — Plan v1.0 T1.2.
 *
 * Two-column layout: left = build metadata + actions, right = BuildTimeline.
 * Actions row contains View Logs (→ T1.3), Cancel (only running/queued),
 * View SBOM, View Attestation (→ T1.4/T1.5 placeholders for now).
 */
export function DeploymentDetailPage() {
  useRequireAuth();
  const { name = '', id = '' } = useParams<{ name: string; id: string }>();
  useSetPageTitle(`${name} · Deployment ${id.slice(0, 7)}`);
  const { build, loading, error } = useDeployment(id);
  const [rollbackOpen, setRollbackOpen] = useState(false);
  const [promoteOpen, setPromoteOpen] = useState(false);
  const cancelBuild = useCancelBuild();
  const cancelling = cancelBuild.isPending;

  // T1.3: useMutation onSuccess invalidates the build detail cache so the
  // status badge re-renders to CANCELLED without window.location.reload.
  const handleCancel = () => {
    if (!build) return;
    cancelBuild.mutate(build.build_id);
  };

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
  const canCancel = build.status === 'running' || build.status === 'queued';

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

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[1.5fr_1fr]">
        <div className="space-y-6">
          <Card>
            <Card.Header>
              <div className="flex items-center justify-between gap-4">
                <Card.Title>Build</Card.Title>
                <StatusDot
                  status={STATUS_MAP[build.status]}
                  label={STATUS_LABEL[build.status]}
                />
              </div>
              <Card.Description>{build.environment}</Card.Description>
            </Card.Header>
            <Card.Body>
              <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2 text-body-sm">
                <div>
                  <dt className="text-label uppercase text-muted-foreground">
                    Commit
                  </dt>
                  <dd className="mt-1 inline-flex items-center gap-2 font-mono text-foreground">
                    <GitCommit className="size-3.5" aria-hidden="true" />
                    {sha}
                  </dd>
                </div>
                <div>
                  <dt className="text-label uppercase text-muted-foreground">
                    Author
                  </dt>
                  <dd className="mt-1 text-foreground">{build.created_by}</dd>
                </div>
                <div>
                  <dt className="text-label uppercase text-muted-foreground">
                    Started
                  </dt>
                  <dd className="mt-1 tabular-nums text-foreground">
                    {build.started_at
                      ? formatDate(build.started_at, 'relative')
                      : '—'}
                  </dd>
                </div>
                <div>
                  <dt className="text-label uppercase text-muted-foreground">
                    Duration
                  </dt>
                  <dd className="mt-1 tabular-nums text-foreground">
                    {formatDuration(build.duration_seconds)}
                  </dd>
                </div>
              </dl>

              {build.error && (
                <div className="mt-4 rounded-md border border-destructive/40 bg-destructive/10 p-3">
                  <p className="text-label uppercase text-destructive">Error</p>
                  <p className="mt-1 font-mono text-label text-destructive">
                    {build.error}
                  </p>
                </div>
              )}

              {build.apps && build.apps.length > 0 && (
                <div className="mt-4">
                  <p className="text-label uppercase text-muted-foreground mb-2">
                    Apps deployed
                  </p>
                  <ul className="space-y-1.5">
                    {build.apps.map((a) => (
                      <li
                        key={a.name}
                        className="flex items-center justify-between text-body-sm"
                      >
                        <span className="font-medium text-foreground">
                          {a.name}
                        </span>
                        <Badge
                          variant="outline"
                          className="font-mono text-label"
                        >
                          {a.image_digest.slice(0, 19)}…
                        </Badge>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </Card.Body>
          </Card>

          <Card>
            <Card.Header>
              <Card.Title>Actions</Card.Title>
            </Card.Header>
            <Card.Body className="flex flex-wrap gap-2">
              <Button asChild variant="secondary" size="sm">
                <Link
                  to={`/projects/${encodeURIComponent(name)}/deployments/${id}/logs`}
                  className="inline-flex items-center gap-2"
                >
                  <ScrollText className="size-4" aria-hidden="true" />
                  View logs
                </Link>
              </Button>
              {canCancel && (
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={handleCancel}
                  disabled={cancelling}
                >
                  {cancelling ? (
                    <Loader2 className="size-4 animate-spin" aria-hidden="true" />
                  ) : (
                    <XCircle className="size-4" aria-hidden="true" />
                  )}
                  Cancel build
                </Button>
              )}
              {build.status === 'succeeded' && (
                <>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => setRollbackOpen(true)}
                  >
                    <Undo2 className="size-4 text-warning" aria-hidden="true" />
                    Rollback to here
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => setPromoteOpen(true)}
                  >
                    <Rocket className="size-4 text-primary" aria-hidden="true" />
                    Promote
                  </Button>
                </>
              )}
              <Button asChild variant="ghost" size="sm">
                <Link
                  to={`/projects/${encodeURIComponent(name)}/deployments/${id}/sbom`}
                  className="inline-flex items-center gap-2"
                >
                  <FileText className="size-4" aria-hidden="true" />
                  View SBOM
                </Link>
              </Button>
              <Button asChild variant="ghost" size="sm">
                <Link
                  to={`/projects/${encodeURIComponent(name)}/deployments/${id}/attestation`}
                  className="inline-flex items-center gap-2"
                >
                  <ShieldCheck className="size-4" aria-hidden="true" />
                  Attestation
                </Link>
              </Button>
            </Card.Body>
          </Card>
        </div>

        <Card>
          <Card.Header>
            <Card.Title>Timeline</Card.Title>
            <Card.Description>Pipeline phase progress.</Card.Description>
          </Card.Header>
          <Card.Body>
            <BuildTimeline status={build.status} />
          </Card.Body>
        </Card>
      </div>

      <RollbackDialog
        open={rollbackOpen}
        onOpenChange={setRollbackOpen}
        projectName={name}
        targetBuild={build}
      />
      <PromoteDialog
        open={promoteOpen}
        onOpenChange={setPromoteOpen}
        projectName={name}
      />
    </PageShell>
  );
}
