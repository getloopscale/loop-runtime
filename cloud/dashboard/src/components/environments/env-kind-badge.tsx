/**
 * env-kind-badge — small visual tag indicating whether an environment
 * row is a permanent env (production / staging / custom) or the
 * single-slot preview row.
 *
 * D7 of plan 2026-06-07-preview-as-category-single-slot-plan.md.
 *
 * Strict design: only TWO values per the canonical model. There is no
 * "draft" / "unknown" because the env_targets.kind CHECK constraint
 * limits values to {'permanent', 'preview'} at the DB layer.
 */
import { Globe, GitBranch } from 'lucide-react';

export type EnvKind = 'permanent' | 'preview';

export interface EnvKindBadgeProps {
  kind: EnvKind;
}

export function EnvKindBadge({ kind }: EnvKindBadgeProps) {
  if (kind === 'preview') {
    return (
      <span
        className="inline-flex items-center gap-1 rounded-md bg-accent/10 px-2 py-0.5 text-label text-accent"
        aria-label="Preview environment"
      >
        <GitBranch className="size-3" aria-hidden="true" />
        Preview
      </span>
    );
  }
  return (
    <span
      className="inline-flex items-center gap-1 rounded-md bg-muted px-2 py-0.5 text-label text-muted-foreground"
      aria-label="Permanent environment"
    >
      <Globe className="size-3" aria-hidden="true" />
      Permanent
    </span>
  );
}
