import { ExternalLink } from 'lucide-react';

/**
 * DeploymentVisitButton — T2.1 of plan 2026-06-12-dashboard-journey-j25-live-url-discoverability v1.2.
 *
 * Renders the "Visit" affordance in the Deployment Detail header — but ONLY when:
 *   (a) build.status === 'succeeded'
 *   (b) this build IS the currently-serving (active release) build
 *   (c) status.url is non-null
 *
 * Anti-trap UX (the whole reason this component exists):
 *   Visit on a PREVIOUS deploy would open the CURRENT live URL — the URL is
 *   per-environment, not per-build. The user, expecting historical-build
 *   content, would land on the current production. To prevent the trap,
 *   Visit is hidden on every build except the active release.
 *
 * Per ADR D2 of the plan + Vercel canonical UX (Vercel labels only the
 * currently-rolled-out deploy as "Production"; older deploys are "Previous"
 * with NO Visit link). The button is rendered as a primary anchor since
 * the most likely action pós-promote is to open and inspect.
 */

interface DeploymentVisitButtonProps {
  buildStatus: string;
  isActiveRelease: boolean;
  url?: string;
}

export function DeploymentVisitButton({
  buildStatus,
  isActiveRelease,
  url,
}: DeploymentVisitButtonProps) {
  if (buildStatus !== 'succeeded' || !isActiveRelease || !url) {
    return null;
  }
  return (
    <a
      href={url}
      target="_blank"
      rel="noopener noreferrer"
      className="inline-flex items-center gap-2 rounded-md bg-primary px-3 py-2 text-label font-medium text-primary-foreground hover:bg-primary/90"
    >
      <ExternalLink className="size-4" aria-hidden="true" />
      Visit
    </a>
  );
}
