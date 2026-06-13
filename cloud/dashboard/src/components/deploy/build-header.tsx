import { GitBranch, GitCommit, RefreshCw, Undo2, XCircle } from 'lucide-react';
import { Button, StatusDot, type StatusKind } from '@loopzero/ui';
import { normalizeBuildStatus, type Build, type BuildStatus } from '@/lib/api/types';

/**
 * BuildHeader — Plan J-17 T3.5.
 *
 * Header for the deploy detail page. Renders the canonical metadata
 * row (SHA, branch, total duration, status badge) and the action
 * cluster (cancel, rollback, redeploy).
 *
 * Action visibility:
 *   - cancel   : only when status is in-flight (queued/running/cancelling)
 *   - rollback : only when status is succeeded (immutable target needed)
 *   - redeploy : always rendered; disabled while in-flight so users
 *                see a stable affordance instead of a moving target
 *
 * Callbacks are received as props so the layout owns the mutation
 * lifecycle (react-query) and the header stays render-pure.
 */

export interface BuildHeaderProps {
  build: Build;
  branch?: string;
  onCancel: () => void;
  onRollback: () => void;
  onRedeploy: () => void;
}

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
  if (seconds === undefined || seconds === null) return '—';
  if (seconds < 60) return `${Math.round(seconds)}s`;
  const m = Math.floor(seconds / 60);
  const s = Math.round(seconds % 60);
  return s === 0 ? `${m}m` : `${m}m ${s}s`;
}

function isInFlight(status: BuildStatus): boolean {
  return status === 'queued' || status === 'running' || status === 'cancelling';
}

export function BuildHeader({
  build,
  branch,
  onCancel,
  onRollback,
  onRedeploy,
}: BuildHeaderProps) {
  const sha = build.commit_sha.slice(0, 7);
  // Backend returns CAPS statuses; normalize once so every comparison
  // and lookup below works uniformly.
  const ns = normalizeBuildStatus(build.status);
  const inFlight = isInFlight(ns);
  const showCancel = inFlight;
  const showRollback = ns === 'succeeded';

  return (
    <header className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-body-sm">
        <span className="inline-flex items-center gap-1.5 font-mono text-foreground">
          <GitCommit className="size-4 text-muted-foreground" aria-hidden="true" />
          {sha}
        </span>
        {branch && (
          <span className="inline-flex items-center gap-1.5 text-foreground">
            <GitBranch className="size-4 text-muted-foreground" aria-hidden="true" />
            {branch}
          </span>
        )}
        <span className="tabular-nums text-muted-foreground">
          {formatDuration(build.duration_seconds)}
        </span>
        <StatusDot status={STATUS_MAP[ns]} label={STATUS_LABEL[ns]} />
      </div>

      <div className="flex flex-wrap items-center gap-2">
        {showCancel && (
          <Button
            variant="destructive"
            size="sm"
            onClick={onCancel}
            aria-label="Cancel build"
          >
            <XCircle className="size-4" aria-hidden="true" />
            Cancel build
          </Button>
        )}
        {showRollback && (
          <Button
            variant="ghost"
            size="sm"
            onClick={onRollback}
            aria-label="Rollback to this build"
          >
            <Undo2 className="size-4 text-warning" aria-hidden="true" />
            Rollback
          </Button>
        )}
        <Button
          variant="secondary"
          size="sm"
          onClick={onRedeploy}
          disabled={inFlight}
          aria-label="Redeploy"
        >
          <RefreshCw className="size-4" aria-hidden="true" />
          Redeploy
        </Button>
      </div>
    </header>
  );
}
