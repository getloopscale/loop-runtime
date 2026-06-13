import { AlertCircle, ExternalLink } from 'lucide-react';
import { Link } from 'react-router-dom';
import type { ErrorClass } from '@/lib/api/sse-events';

/**
 * BuildErrorPill — Plan J-17 T3.4.
 *
 * Renders the error_class as a compact pill with a tooltip-style content
 * area beneath that surfaces the suggestion and a link to the build
 * logs tab (where the user can dig further).
 *
 * The pill always shows the human-readable label even when the class
 * is missing or unknown — UX should never expose raw OOM/NULL strings.
 */
export interface BuildErrorPillProps {
  errorClass: ErrorClass;
  errorMessage: string;
  suggestion: string;
  /** Used to construct the "Open logs" link. */
  projectName: string;
  buildId: string;
  className?: string;
}

const LABEL_BY_CLASS: Record<ErrorClass, string> = {
  OOM: 'Out of memory',
  BUILD_TIMEOUT: 'Build timed out',
  IMAGE_PULL_FAILED: 'Image pull failed',
  COSIGN_FAILED: 'Image signing failed',
  REGISTRY_AUTH: 'Registry authentication failed',
  DOCKERFILE_LINT: 'Dockerfile lint failed',
  LOOPZERO_YAML_INVALID: 'loopzero.yaml invalid',
  CANCELLED_BY_USER: 'Cancelled',
  WORKFLOW_DELETED: 'Build interrupted',
  UNKNOWN: 'Build failed',
};

export function BuildErrorPill({
  errorClass,
  errorMessage,
  suggestion,
  projectName,
  buildId,
  className,
}: BuildErrorPillProps) {
  const label = LABEL_BY_CLASS[errorClass] ?? LABEL_BY_CLASS.UNKNOWN;
  const logsHref = `/projects/${encodeURIComponent(
    projectName,
  )}/deployments/${encodeURIComponent(buildId)}/logs`;
  return (
    <div className={className} role="alert" aria-live="polite">
      <div className="inline-flex items-center gap-2 rounded-full border border-destructive/40 bg-destructive/10 px-3 py-1 text-label uppercase tracking-wide text-destructive">
        <AlertCircle className="size-3.5" aria-hidden="true" />
        <span>{label}</span>
      </div>
      {suggestion && (
        <p className="mt-2 max-w-xl text-body-sm text-muted-foreground">{suggestion}</p>
      )}
      {errorMessage && (
        <p className="mt-1 max-w-xl font-mono text-label text-muted-foreground/80">
          {errorMessage}
        </p>
      )}
      <Link
        to={logsHref}
        className="mt-2 inline-flex items-center gap-1 text-body-sm text-primary hover:underline"
      >
        <ExternalLink className="size-3.5" aria-hidden="true" />
        Open build logs
      </Link>
    </div>
  );
}
