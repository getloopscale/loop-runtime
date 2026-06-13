import { ExternalLink } from 'lucide-react';

/**
 * DeploymentRowVisitIcon — T3.1 of plan 2026-06-12-dashboard-journey-j25-live-url-discoverability v1.2.
 *
 * Small ExternalLink icon affordance rendered ONLY on the table row whose
 * build IS the currently-serving (active release) deploy AND when the live
 * URL is present. Same anti-trap UX invariant as T2.1 Visit button — Vercel
 * canonical pattern: only the active row gets the open-in-new-tab icon.
 *
 * Per ADR D2 (3 slots) + T3.1 acceptance: "Ícone ExternalLink renderizado
 * APENAS na row cujo build.id == active_release.build_id." Tooltip text
 * "Open live URL" satisfies a11y + matches plan v1.2.
 */

interface DeploymentRowVisitIconProps {
  isActive: boolean;
  url?: string;
}

export function DeploymentRowVisitIcon({
  isActive,
  url,
}: DeploymentRowVisitIconProps) {
  if (!isActive || !url) return null;
  return (
    <a
      href={url}
      target="_blank"
      rel="noopener noreferrer"
      aria-label="Open live URL"
      title="Open live URL"
      className="inline-flex items-center justify-center rounded-md p-1 text-muted-foreground hover:bg-surface-2 hover:text-foreground"
    >
      <ExternalLink className="size-4" aria-hidden="true" />
    </a>
  );
}
