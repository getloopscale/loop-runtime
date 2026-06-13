import { useEffect, useState } from 'react';
import { Copy, ExternalLink } from 'lucide-react';
import { Badge, Card, Skeleton } from '@loopzero/ui';
import type { ProjectStatusDetail } from '@/lib/api/types';

/**
 * LiveUrlHero — T1.1 of plan 2026-06-12-dashboard-journey-j25-live-url-discoverability v1.2.
 *
 * Renders the production deploy URL prominently at the top of the Project
 * Overview page. Three states:
 *   - loading: Skeleton
 *   - status.url populated: hero card with Visit link (new tab, noopener) + Copy button
 *   - status.url absent: gray "URL pending" badge (per ADR D3 — never silent hide)
 *
 * Canonical data path:
 *   useProjectStatus(name).status.url is emitted by `api/internal/routes/status.go:252`
 *   as `https://<project>-<env>.<baseDomain>` (e.g., https://teste-production.loopzero.example
 *   for dev-public). Per ADR D1, this is the single source of truth — zero backend change.
 *
 * Copy affordance uses navigator.clipboard.writeText + a transient ARIA-live region
 * ("Copied" message announced to AT for 2s). Avoids introducing a Toaster scaffolding
 * dependency for test setup; accessibility canonical.
 */

interface LiveUrlHeroProps {
  status: ProjectStatusDetail | null;
  loading: boolean;
}

export function LiveUrlHero({ status, loading }: LiveUrlHeroProps) {
  const [copyMessage, setCopyMessage] = useState<string>('');

  useEffect(() => {
    if (!copyMessage) return;
    const t = setTimeout(() => setCopyMessage(''), 2000);
    return () => clearTimeout(t);
  }, [copyMessage]);

  if (loading) {
    return (
      <Card>
        <Card.Body className="py-4">
          <Skeleton data-testid="live-url-hero-skeleton" className="h-8 w-2/3" />
        </Card.Body>
      </Card>
    );
  }

  const url = status?.url;

  if (!url) {
    return (
      <Card>
        <Card.Body className="flex items-center justify-between py-4">
          <div>
            <div className="text-label text-muted-foreground uppercase">Live URL</div>
            <div className="mt-1 text-body-sm text-muted-foreground">
              No production deploy yet — URL appears after the first successful deploy.
            </div>
          </div>
          <Badge variant="outline">URL pending</Badge>
        </Card.Body>
      </Card>
    );
  }

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(url);
      setCopyMessage('Copied');
    } catch {
      setCopyMessage('Copy failed');
    }
  };

  return (
    <Card>
      <Card.Body className="flex items-center justify-between gap-4 py-4">
        <div className="min-w-0 flex-1">
          <div className="text-label text-muted-foreground uppercase">Live URL</div>
          <div className="mt-1 truncate text-body font-mono text-foreground">{url}</div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <button
            type="button"
            onClick={handleCopy}
            aria-label="Copy live URL"
            title="Copy"
            className="inline-flex items-center justify-center rounded-md p-2 text-muted-foreground hover:bg-surface-2 hover:text-foreground"
          >
            <Copy className="size-4" aria-hidden="true" />
          </button>
          <a
            href={url}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-2 rounded-md bg-primary px-3 py-2 text-label font-medium text-primary-foreground hover:bg-primary/90"
          >
            <ExternalLink className="size-4" aria-hidden="true" />
            Visit
          </a>
        </div>
        <span role="status" aria-live="polite" className="sr-only">
          {copyMessage}
        </span>
      </Card.Body>
    </Card>
  );
}
