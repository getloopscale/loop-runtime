/**
 * preview-row — single-slot preview row for the /environments grid.
 *
 * D7 of plan 2026-06-07-preview-as-category-single-slot-plan.md
 * (Netlify-style: one logical "Preview" entry per tenant; the active
 * deployment substitutes the previous one atomically when a new PR opens).
 *
 * Renders either:
 *   - Active state: PR number + branch + external URL link.
 *   - Empty state: "No active branch" (slot row exists but
 *     active_pr_number is NULL — the tenant had previews before but
 *     they all closed).
 */
import { ExternalLink, GitBranch } from 'lucide-react';
import { Table } from '@loopzero/ui';
import { EnvKindBadge } from './env-kind-badge';

export interface PreviewSlot {
  project_id: string;
  active_pr_number?: number | null;
  active_branch?: string | null;
  active_release_id?: string | null;
  url?: string | null;
  updated_at: string;
}

export interface PreviewRowProps {
  /** Slot row from the API. null = tenant has no slot at all (rendered as empty). */
  slot: PreviewSlot | null;
}

export function PreviewRow({ slot }: PreviewRowProps) {
  const isActive = slot?.active_pr_number != null;

  return (
    <Table.Row aria-label={`Preview environment row for project ${slot?.project_id ?? 'unknown'}`}>
      <Table.Cell>
        <div className="flex items-center gap-2">
          {/* 2026-06-07 per-project: name includes project_id so users
              with N projects see distinct rows. Format: "preview · <project>". */}
          <span className="font-mono text-body-sm text-foreground">
            preview · {slot?.project_id ?? 'unknown'}
          </span>
          <EnvKindBadge kind="preview" />
        </div>
      </Table.Cell>
      <Table.Cell>
        {isActive ? (
          <div className="flex items-center gap-2 text-body-sm">
            <GitBranch className="size-3 text-muted-foreground" aria-hidden="true" />
            <span className="font-mono text-foreground">
              PR #{slot!.active_pr_number} — {slot!.active_branch}
            </span>
          </div>
        ) : (
          <span className="text-muted-foreground text-label" aria-label="No active branch">
            No active branch
          </span>
        )}
      </Table.Cell>
      <Table.Cell className="hidden md:table-cell">
        {isActive && slot!.url ? (
          <a
            href={slot!.url}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 text-primary hover:underline"
          >
            Open
            <ExternalLink className="size-3" aria-hidden="true" />
          </a>
        ) : (
          <span className="text-muted-foreground text-label">—</span>
        )}
      </Table.Cell>
      <Table.Cell className="hidden md:table-cell">
        <span className="text-muted-foreground text-label">—</span>
      </Table.Cell>
      <Table.Cell align="right">
        <span className="text-muted-foreground text-label">—</span>
      </Table.Cell>
    </Table.Row>
  );
}
