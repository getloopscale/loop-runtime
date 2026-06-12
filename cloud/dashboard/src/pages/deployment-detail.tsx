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
} from '@loopzero/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useDeployment } from '@/hooks/use-deployment';
import { useProjectStatus } from '@/hooks/use-project-status';
import { DeploymentVisitButton } from '@/components/project/deployment-visit-button';
import { useEngineApi } from '@/hooks/use-api';
import { BuildTimelineLive } from '@/components/deploy/build-timeline-live';
import { RollbackDialog } from '@/components/dialogs/rollback-dialog';
import { PromoteDialog } from '@/components/dialogs/promote-dialog';
import { CancelBuildDialog } from '@/components/dialogs/cancel-build-dialog';
import { formatDate } from '@/lib/format';
import { formatBuildError } from '@/lib/format-build-error-patterns';
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
  // T2.1 J-25 — Visit button anti-trap UX: hide on previous deploys.
  // useProjectStatus delivers status.url + active_release.build_id used to
  // gate Visit visibility (only the currently-serving build renders Visit).
  const { status: projectStatus } = useProjectStatus(name);
  const [rollbackOpen, setRollbackOpen] = useState(false);
  const [promoteOpen, setPromoteOpen] = useState(false);
  // T4.4 finding 4.1 + EC-7 absorption: cancel build now goes through
  // CancelBuildDialog (J-17 T6.2 canonical pattern). Direct mutate from
  // the action button was the 2026-06-08 review finding "single-click
  // destructive". The dialog also surfaces 409 race ("build already
  // completed") as a toast instead of silently dropping.
  const [cancelDialogOpen, setCancelDialogOpen] = useState(false);
  // Plan J-10 v1.1 — PromoteDialog requires environments list for source/target
  // dropdowns. Fetched here so deployment detail's "Promote" button can open
  // the dialog with full env context.
  const { data: envsData } = useEngineApi<{
    environments: Array<{ name: string; status?: string }>;
  }>('/api/v1/environments');
  const environments = envsData?.environments ?? [];

  // T4.4 finding 4.4: STATUS_LABEL fallback. New BuildStatus values
  // added to BuildState in pkg/statemachine (e.g. RELEASING) would
  // previously render `undefined` here; fall back to the raw status
  // string OR "Unknown" as last resort.
  function safeStatusLabel(status: string): string {
    return STATUS_LABEL[status as BuildStatus] ?? status ?? 'Unknown';
  }
  function safeStatusKind(status: string): StatusKind {
    return STATUS_MAP[status as BuildStatus] ?? 'idle';
  }

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

      {/* T4.4 finding 4.8: timeline column joins at md breakpoint
          (was lg, leaving tablets in single-column with timeline at
          the bottom — usability gap). */}
      <div className="grid grid-cols-1 gap-6 md:grid-cols-[1.5fr_1fr]">
        <div className="space-y-6">
          <Card>
            <Card.Header>
              <div className="flex items-center justify-between gap-4">
                <Card.Title>Build</Card.Title>
                <StatusDot
                  status={safeStatusKind(build.status)}
                  label={safeStatusLabel(build.status)}
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
                <BuildErrorPanel raw={build.error} />
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
              {/* T4.4 finding 4.1: cancel button now opens
                  CancelBuildDialog (J-17 canonical confirm pattern)
                  instead of single-click mutate. EC-7 absorption:
                  dialog handles 409 race when build completes between
                  opening + confirming. */}
              {canCancel && (
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => setCancelDialogOpen(true)}
                >
                  <XCircle className="size-4" aria-hidden="true" />
                  Cancel build
                </Button>
              )}
              {build.status === 'succeeded' && (
                <>
                  {/* T2.1 J-25 — Visit comes BEFORE Rollback so the most
                      likely action pós-promote (open + inspect live URL) is
                      visually primary. Component itself enforces anti-trap
                      UX: rendered ONLY if this build is the active release. */}
                  <DeploymentVisitButton
                    buildStatus={build.status}
                    isActiveRelease={projectStatus?.active_release?.build_id === id}
                    url={projectStatus?.url}
                  />
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
              {/* T4.4 finding 4.7: SBOM/Attestation buttons are
                  meaningful only on succeeded builds — earlier states
                  have no signed image to inspect. Disabled (not
                  hidden) so the user discovers the affordance exists
                  but understands the gate. */}
              <Button
                asChild={build.status === 'succeeded'}
                variant="ghost"
                size="sm"
                disabled={build.status !== 'succeeded'}
                aria-disabled={build.status !== 'succeeded'}
              >
                {build.status === 'succeeded' ? (
                  <Link
                    to={`/projects/${encodeURIComponent(name)}/deployments/${id}/sbom`}
                    className="inline-flex items-center gap-2"
                  >
                    <FileText className="size-4" aria-hidden="true" />
                    View SBOM
                  </Link>
                ) : (
                  <span className="inline-flex items-center gap-2 opacity-50">
                    <FileText className="size-4" aria-hidden="true" />
                    View SBOM
                  </span>
                )}
              </Button>
              <Button
                asChild={build.status === 'succeeded'}
                variant="ghost"
                size="sm"
                disabled={build.status !== 'succeeded'}
                aria-disabled={build.status !== 'succeeded'}
              >
                {build.status === 'succeeded' ? (
                  <Link
                    to={`/projects/${encodeURIComponent(name)}/deployments/${id}/attestation`}
                    className="inline-flex items-center gap-2"
                  >
                    <ShieldCheck className="size-4" aria-hidden="true" />
                    Attestation
                  </Link>
                ) : (
                  <span className="inline-flex items-center gap-2 opacity-50">
                    <ShieldCheck className="size-4" aria-hidden="true" />
                    Attestation
                  </span>
                )}
              </Button>
            </Card.Body>
          </Card>
        </div>

        <Card>
          <Card.Header>
            <Card.Title>Timeline</Card.Title>
            <Card.Description>
              Live pipeline step-by-step progress (SSE).
            </Card.Description>
          </Card.Header>
          <Card.Body>
            {/* T5.3 — realtime swap. The hook resumes via Last-Event-ID
                so even mid-tab navigation never drops events. */}
            <BuildTimelineLive
              buildId={build.build_id}
              projectName={name}
            />
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
        environments={environments}
        initialFrom={build?.environment}
      />
      {/* T4.4 finding 4.1 + EC-7: cancel dialog mounted unconditionally
          so the open/close transition animates smoothly. Dialog itself
          gates open state via the prop. */}
      <CancelBuildDialog
        open={cancelDialogOpen}
        onOpenChange={setCancelDialogOpen}
        build={build}
      />
    </PageShell>
  );
}

/**
 * BuildErrorPanel — T4.4 finding 4.2 + T3.1 + EC-3 + EC-5.
 *
 * Sanitizes `build.error` through formatBuildError before rendering.
 * The visible default is the product-language `summary`; raw operator
 * output lives behind a "Show details" toggle (CLAUDE.md Rule 3 — no
 * internal terms like ArgoCD/BuildKit/gyp/workspace paths leak to
 * customers in the default view).
 */
function BuildErrorPanel({ raw }: { raw: string }) {
  const [showDetails, setShowDetails] = useState(false);
  const { summary, detail } = formatBuildError(raw);
  return (
    <div className="mt-4 rounded-md border border-destructive/40 bg-destructive/10 p-3">
      <p className="text-label uppercase text-destructive">Error</p>
      <p className="mt-1 text-body-sm text-destructive">{summary}</p>
      {detail && detail !== summary && (
        <>
          <button
            type="button"
            className="mt-2 text-label text-destructive underline hover:no-underline"
            onClick={() => setShowDetails((v) => !v)}
            aria-expanded={showDetails}
          >
            {showDetails ? 'Hide details' : 'Show details'}
          </button>
          {showDetails && (
            <pre className="mt-2 max-h-64 overflow-auto whitespace-pre-wrap rounded bg-destructive/5 p-2 font-mono text-label text-destructive/90">
              {detail}
            </pre>
          )}
        </>
      )}
    </div>
  );
}
